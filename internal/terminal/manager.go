package terminal

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
	"velin-webssh/internal/security"
	"velin-webssh/internal/store"
)

var (
	ErrHostKeyUnknown     = errors.New("host key is not trusted")
	ErrHostKeyChanged     = errors.New("host key changed")
	ErrNotController      = errors.New("terminal is controlled by another client")
	ErrNormalSessionEnded = errors.New("normal SSH sessions cannot be restored after the Velin service restarts")
)

type HostKeyError struct{ Kind, Fingerprint, PublicKey, HostName, Address string }

func (e *HostKeyError) Error() string { return e.Kind + ": " + e.Fingerprint }

type CreateRequest struct {
	Host               store.Host
	Credential         store.Credential
	Secret, Passphrase string
	TrustFingerprint   string
	Name               string
	SessionMode        string
}
type TestResult struct {
	LatencyMS    int64  `json:"latencyMs"`
	TmuxVersion  string `json:"tmuxVersion"`
	SessionMode  string `json:"sessionMode"`
	Platform     string `json:"platform"`
	Distribution string `json:"distribution"`
	Fingerprint  string `json:"fingerprint,omitempty"`
}

type Manager struct {
	store        *store.Store
	vault        *security.Vault
	deploymentID string
	mu           sync.RWMutex
	sessions     map[string]*Session
}

func (m *Manager) DialSaved(ctx context.Context, userID, hostID string) (*ssh.Client, store.Host, error) {
	host, err := m.store.Host(userID, hostID)
	if err != nil {
		return nil, host, err
	}
	if host.CredentialID == "" {
		return nil, host, errors.New("saved credential required")
	}
	credential, err := m.store.Credential(userID, host.CredentialID)
	if err != nil {
		return nil, host, err
	}
	client, err := m.dial(ctx, userID, host, credential, "", "", "")
	return client, host, err
}

type Session struct {
	meta           store.TerminalSession
	manager        *Manager
	mu             sync.RWMutex
	sshClient      *ssh.Client
	sshSession     *ssh.Session
	stdin          io.WriteCloser
	subs           map[string]chan Event
	controller     string
	controllerSeen time.Time
	pendingControl string
	buffer         *ringBuffer
	closed         bool
}

type Event struct {
	Type       string `json:"type"`
	Data       string `json:"data,omitempty"`
	Status     string `json:"status,omitempty"`
	Message    string `json:"message,omitempty"`
	ClientID   string `json:"clientID,omitempty"`
	Controller string `json:"controller,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
	Requester  string `json:"requester,omitempty"`
}

func NewManager(s *store.Store, vault *security.Vault, deploymentID string) *Manager {
	_ = s.EndStaleNormalSessions(ErrNormalSessionEnded.Error())
	return &Manager{store: s, vault: vault, deploymentID: deploymentID, sessions: make(map[string]*Session)}
}

func normalizeSessionMode(preferred, fallback string) string {
	if preferred == "normal" || (preferred == "" && fallback == "normal") {
		return "normal"
	}
	return "tmux"
}

func (m *Manager) ActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

func (m *Manager) Create(ctx context.Context, userID string, req CreateRequest) (store.TerminalSession, error) {
	id := uuid.NewString()
	mode := normalizeSessionMode(req.SessionMode, req.Host.SessionMode)
	meta := store.TerminalSession{ID: id, UserID: userID, HostID: req.Host.ID, CredentialID: req.Credential.ID, Name: req.Name, RemoteUser: req.Host.Username, SessionMode: mode, TmuxSocket: "velin-webssh-" + m.deploymentID, TmuxName: "ws_" + strings.ReplaceAll(id, "-", ""), OwnerMarker: m.deploymentID + ":" + userID + ":" + id, Status: "creating"}
	if meta.Name == "" {
		meta.Name = req.Host.Name
	}
	if err := m.store.SaveTerminal(meta); err != nil {
		return meta, err
	}
	sess := &Session{meta: meta, manager: m, subs: make(map[string]chan Event), buffer: newRingBuffer(2 << 20)}
	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()
	if err := sess.connect(ctx, req.Host, req.Credential, req.Secret, req.Passphrase, req.TrustFingerprint, true); err != nil {
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
		_ = m.store.DeleteTerminal(userID, id)
		return meta, err
	}
	return sess.Meta(), nil
}

func (m *Manager) Test(ctx context.Context, userID string, host store.Host, credential store.Credential, secret, passphrase, trust string) (TestResult, error) {
	started := time.Now()
	client, err := m.dial(ctx, userID, host, credential, secret, passphrase, trust)
	if err != nil {
		return TestResult{}, err
	}
	defer client.Close()
	platform, distribution := detectHostSystem(client)
	if platform != "" {
		_ = m.store.UpdateHostSystem(userID, host.ID, platform, distribution)
	}
	mode := normalizeSessionMode("", host.SessionMode)
	if mode == "normal" {
		return TestResult{LatencyMS: time.Since(started).Milliseconds(), SessionMode: mode, Platform: platform, Distribution: distribution}, nil
	}
	out, err := run(client, "command -v tmux >/dev/null 2>&1 && tmux -V")
	if err != nil {
		return TestResult{}, fmt.Errorf("tmux is required on the remote host: %s", cleanOutput(out, err))
	}
	return TestResult{LatencyMS: time.Since(started).Milliseconds(), TmuxVersion: strings.TrimSpace(string(out)), SessionMode: mode, Platform: platform, Distribution: distribution}, nil
}

func (m *Manager) Restore(ctx context.Context, userID, id, secret, passphrase, trust string) (*Session, error) {
	m.mu.RLock()
	existing := m.sessions[id]
	m.mu.RUnlock()
	if existing != nil {
		return existing, nil
	}
	meta, err := m.store.Terminal(userID, id)
	if err != nil {
		return nil, err
	}
	if meta.Status == "ended" {
		return nil, errors.New("terminal session has ended")
	}
	if meta.SessionMode == "normal" {
		_ = m.store.UpdateTerminalStatus(userID, id, "ended", ErrNormalSessionEnded.Error())
		return nil, ErrNormalSessionEnded
	}
	host, err := m.store.Host(userID, meta.HostID)
	if err != nil {
		return nil, err
	}
	var cred store.Credential
	if meta.CredentialID != "" {
		cred, err = m.store.Credential(userID, meta.CredentialID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	sess := &Session{meta: meta, manager: m, subs: make(map[string]chan Event), buffer: newRingBuffer(2 << 20)}
	m.mu.Lock()
	if prior := m.sessions[id]; prior != nil {
		m.mu.Unlock()
		return prior, nil
	}
	m.sessions[id] = sess
	m.mu.Unlock()
	if err = sess.connect(ctx, host, cred, secret, passphrase, trust, false); err != nil {
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
		status := "unreachable"
		if secret == "" && cred.ID == "" {
			status = "auth_required"
		}
		_ = m.store.UpdateTerminalStatus(userID, id, status, err.Error())
		return nil, err
	}
	return sess, nil
}

func (m *Manager) Get(userID, id string) (*Session, error) {
	m.mu.RLock()
	s := m.sessions[id]
	m.mu.RUnlock()
	if s == nil || s.meta.UserID != userID {
		return nil, sql.ErrNoRows
	}
	return s, nil
}

func (m *Manager) Rename(userID, id, name string) error {
	if err := m.store.RenameTerminal(userID, id, name); err != nil {
		return err
	}
	m.mu.RLock()
	s := m.sessions[id]
	m.mu.RUnlock()
	if s != nil && s.meta.UserID == userID {
		s.mu.Lock()
		s.meta.Name = name
		s.mu.Unlock()
	}
	return nil
}

func (m *Manager) Terminate(ctx context.Context, userID, id string, secret, passphrase string) error {
	meta, err := m.store.Terminal(userID, id)
	if err != nil {
		return err
	}
	s, err := m.Get(userID, id)
	if err == nil {
		if err = s.killRemote(ctx); err != nil {
			return err
		}
		s.close("ended", "Terminated by user")
		return nil
	}
	if meta.SessionMode == "normal" {
		return m.store.UpdateTerminalStatus(userID, id, "ended", "Terminated by user")
	}
	host, err := m.store.Host(userID, meta.HostID)
	if err != nil {
		return err
	}
	var cred store.Credential
	if meta.CredentialID != "" {
		cred, _ = m.store.Credential(userID, meta.CredentialID)
	}
	client, err := m.dial(ctx, userID, host, cred, secret, passphrase, "")
	if err != nil {
		return err
	}
	defer client.Close()
	if err = validateOwnership(client, meta); err != nil {
		return err
	}
	cmd := fmt.Sprintf("tmux -L %s kill-session -t %s", meta.TmuxSocket, meta.TmuxName)
	if out, e := run(client, cmd); e != nil && !strings.Contains(string(out), "can't find session") {
		return fmt.Errorf("terminate tmux: %s", cleanOutput(out, e))
	}
	return m.store.UpdateTerminalStatus(userID, id, "ended", "Terminated by user")
}

func (s *Session) Meta() store.TerminalSession { s.mu.RLock(); defer s.mu.RUnlock(); return s.meta }

func (s *Session) SSHClient() *ssh.Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sshClient
}

func (s *Session) CurrentDirectory() (string, error) {
	s.mu.RLock()
	client, meta := s.sshClient, s.meta
	s.mu.RUnlock()
	if client == nil {
		return "", errors.New("terminal connection is not available")
	}
	if meta.SessionMode == "normal" {
		return "", errors.New("current directory is unavailable in normal SSH mode")
	}
	out, err := run(client, fmt.Sprintf("tmux -L %s display-message -p -t %s '#{pane_current_path}'", meta.TmuxSocket, meta.TmuxName))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (s *Session) connect(ctx context.Context, host store.Host, cred store.Credential, secret, passphrase, trust string, create bool) error {
	client, err := s.manager.dial(ctx, s.meta.UserID, host, cred, secret, passphrase, trust)
	if err != nil {
		return err
	}
	platform, distribution := detectHostSystem(client)
	if platform != "" {
		_ = s.manager.store.UpdateHostSystem(s.meta.UserID, host.ID, platform, distribution)
	}
	tmuxMode := s.meta.SessionMode != "normal"
	if tmuxMode {
		if out, err := run(client, "command -v tmux >/dev/null 2>&1 && tmux -V"); err != nil {
			client.Close()
			return fmt.Errorf("tmux is required on the remote host: %s", cleanOutput(out, err))
		}
		if create {
			startDir := ""
			if strings.TrimSpace(host.InitialDir) != "" {
				startDir = " -c " + shellQuote(strings.TrimSpace(host.InitialDir))
			}
			cmd := fmt.Sprintf("tmux -L %s new-session -d -s %s%s \\; set-option -t %s @velin_owner %s \\; set-option -t %s history-limit 50000 \\; set-option -t %s status off", s.meta.TmuxSocket, s.meta.TmuxName, startDir, s.meta.TmuxName, s.meta.OwnerMarker, s.meta.TmuxName, s.meta.TmuxName)
			if out, err := run(client, cmd); err != nil {
				client.Close()
				return fmt.Errorf("create tmux session: %s", cleanOutput(out, err))
			}
		} else if err := validateOwnership(client, s.meta); err != nil {
			client.Close()
			return err
		}
		if out, err := run(client, fmt.Sprintf("tmux -L %s set-option -t %s status off", s.meta.TmuxSocket, s.meta.TmuxName)); err != nil {
			client.Close()
			return fmt.Errorf("configure tmux session: %s", cleanOutput(out, err))
		}
		// Keep browser xterm.js on its normal buffer so scrollback and its viewport
		// scrollbar remain available while tmux still manages alternate-screen apps.
		if out, err := run(client, fmt.Sprintf("tmux -L %s set-option -as terminal-overrides ',xterm-256color:smcup@:rmcup@'", s.meta.TmuxSocket)); err != nil {
			client.Close()
			return fmt.Errorf("configure tmux terminal: %s", cleanOutput(out, err))
		}
	}
	sshSess, err := client.NewSession()
	if err != nil {
		client.Close()
		return err
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}
	terminalType := host.TerminalType
	if terminalType == "" {
		terminalType = "xterm-256color"
	}
	if err = sshSess.RequestPty(terminalType, 30, 120, modes); err != nil {
		sshSess.Close()
		client.Close()
		return err
	}
	stdin, err := sshSess.StdinPipe()
	if err != nil {
		sshSess.Close()
		client.Close()
		return err
	}
	stdout, err := sshSess.StdoutPipe()
	if err != nil {
		sshSess.Close()
		client.Close()
		return err
	}
	sshSess.Stderr = sshSess.Stdout
	if tmuxMode {
		attach := fmt.Sprintf("exec tmux -L %s attach-session -t %s", s.meta.TmuxSocket, s.meta.TmuxName)
		err = sshSess.Start(attach)
	} else if strings.TrimSpace(host.InitialDir) != "" && platform != "windows" {
		start := fmt.Sprintf("cd %s && exec ${SHELL:-/bin/sh} -l", shellQuote(strings.TrimSpace(host.InitialDir)))
		err = sshSess.Start(start)
	} else {
		err = sshSess.Shell()
	}
	if err != nil {
		sshSess.Close()
		client.Close()
		return err
	}
	s.mu.Lock()
	s.sshClient = client
	s.sshSession = sshSess
	s.stdin = stdin
	s.closed = false
	s.meta.Status = "attached"
	s.meta.LastError = ""
	s.mu.Unlock()
	_ = s.manager.store.UpdateTerminalStatus(s.meta.UserID, s.meta.ID, "attached", "")
	go s.readLoop(stdout)
	if host.KeepaliveInterval > 0 {
		go keepalive(client, time.Duration(host.KeepaliveInterval)*time.Second)
	}
	return nil
}

func (m *Manager) dial(ctx context.Context, userID string, host store.Host, cred store.Credential, secret, passphrase, trust string) (*ssh.Client, error) {
	return m.dialHost(ctx, userID, host, cred, secret, passphrase, trust, make(map[string]bool), 0)
}

func (m *Manager) dialHost(ctx context.Context, userID string, host store.Host, cred store.Credential, secret, passphrase, trust string, visited map[string]bool, depth int) (*ssh.Client, error) {
	if depth > 8 {
		return nil, errors.New("跳板机链路不能超过 8 层")
	}
	hostKey := host.ID
	if hostKey == "" {
		hostKey = net.JoinHostPort(host.Address, fmt.Sprint(host.Port))
	}
	if visited[hostKey] {
		return nil, errors.New("检测到跳板机循环引用")
	}
	visited[hostKey] = true
	defer delete(visited, hostKey)

	if cred.ID != "" {
		var err error
		secret, err = m.vault.Decrypt(cred.Secret)
		if err != nil {
			return nil, err
		}
		if cred.Passphrase != "" {
			passphrase, err = m.vault.Decrypt(cred.Passphrase)
			if err != nil {
				return nil, err
			}
		}
	}
	if secret == "" {
		return nil, errors.New("SSH credential required")
	}
	var auth ssh.AuthMethod
	if cred.Kind == "key" || strings.Contains(secret, "PRIVATE KEY") {
		signer, err := parseKey([]byte(secret), []byte(passphrase))
		if err != nil {
			return nil, fmt.Errorf("invalid private key: %w", err)
		}
		auth = ssh.PublicKeys(signer)
	} else {
		auth = ssh.Password(secret)
	}
	callback := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		fp := ssh.FingerprintSHA256(key)
		_, stored, err := m.store.KnownHost(userID, host.Address, host.Port)
		if errors.Is(err, sql.ErrNoRows) {
			if trust != fp {
				return &HostKeyError{Kind: "unknown_host_key", Fingerprint: fp, PublicKey: base64.StdEncoding.EncodeToString(key.Marshal()), HostName: host.Name, Address: net.JoinHostPort(host.Address, fmt.Sprint(host.Port))}
			}
			return m.store.TrustHost(userID, host.Address, host.Port, fp, base64.StdEncoding.EncodeToString(key.Marshal()))
		}
		if err != nil {
			return err
		}
		if stored != base64.StdEncoding.EncodeToString(key.Marshal()) {
			if trust == fp {
				return m.store.TrustHost(userID, host.Address, host.Port, fp, base64.StdEncoding.EncodeToString(key.Marshal()))
			}
			return &HostKeyError{Kind: "host_key_changed", Fingerprint: fp, PublicKey: base64.StdEncoding.EncodeToString(key.Marshal()), HostName: host.Name, Address: net.JoinHostPort(host.Address, fmt.Sprint(host.Port))}
		}
		return nil
	}
	timeout := time.Duration(host.ConnectTimeout) * time.Second
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	config := &ssh.ClientConfig{User: host.Username, Auth: []ssh.AuthMethod{auth}, HostKeyCallback: callback, Timeout: timeout}
	addr := net.JoinHostPort(host.Address, fmt.Sprint(host.Port))
	var (
		conn       net.Conn
		jumpClient *ssh.Client
		err        error
	)
	if host.JumpHostID != "" {
		jumpHost, loadErr := m.store.Host(userID, host.JumpHostID)
		if loadErr != nil {
			return nil, errors.New("跳板机不存在或无权访问")
		}
		if jumpHost.CredentialID == "" {
			return nil, fmt.Errorf("跳板机“%s”需要绑定已保存的凭据", jumpHost.Name)
		}
		jumpCredential, loadErr := m.store.Credential(userID, jumpHost.CredentialID)
		if loadErr != nil {
			return nil, fmt.Errorf("读取跳板机“%s”的凭据失败: %w", jumpHost.Name, loadErr)
		}
		jumpClient, err = m.dialHost(dialCtx, userID, jumpHost, jumpCredential, "", "", trust, visited, depth+1)
		if err != nil {
			return nil, fmt.Errorf("连接跳板机“%s”失败: %w", jumpHost.Name, err)
		}
		conn, err = dialThroughJump(dialCtx, jumpClient, addr)
	} else {
		dialer := net.Dialer{Timeout: timeout}
		conn, err = dialer.DialContext(dialCtx, "tcp", addr)
	}
	if err != nil {
		if jumpClient != nil {
			_ = jumpClient.Close()
		}
		return nil, err
	}
	if deadline, ok := dialCtx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	c, chs, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		_ = conn.Close()
		if jumpClient != nil {
			_ = jumpClient.Close()
		}
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	client := ssh.NewClient(c, chs, reqs)
	if jumpClient != nil {
		go func() {
			_ = client.Wait()
			_ = jumpClient.Close()
		}()
	}
	return client, nil
}

func dialThroughJump(ctx context.Context, client *ssh.Client, address string) (net.Conn, error) {
	type result struct {
		conn net.Conn
		err  error
	}
	ready := make(chan result, 1)
	go func() {
		conn, err := client.Dial("tcp", address)
		ready <- result{conn: conn, err: err}
	}()
	select {
	case <-ctx.Done():
		_ = client.Close()
		return nil, ctx.Err()
	case value := <-ready:
		return value.conn, value.err
	}
}

func keepalive(client *ssh.Client, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
			return
		}
	}
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func parseKey(key, pass []byte) (ssh.Signer, error) {
	if len(pass) > 0 {
		return ssh.ParsePrivateKeyWithPassphrase(key, pass)
	}
	return ssh.ParsePrivateKey(key)
}

func validateOwnership(client *ssh.Client, meta store.TerminalSession) error {
	cmd := fmt.Sprintf("tmux -L %s show-options -t %s -v @velin_owner", meta.TmuxSocket, meta.TmuxName)
	out, err := run(client, cmd)
	if err != nil {
		return fmt.Errorf("tmux session not found: %s", cleanOutput(out, err))
	}
	if strings.TrimSpace(string(out)) != meta.OwnerMarker {
		return errors.New("tmux ownership marker mismatch")
	}
	return nil
}

func run(client *ssh.Client, cmd string) ([]byte, error) {
	s, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	defer s.Close()
	return s.CombinedOutput(cmd)
}

func detectHostSystem(client *ssh.Client) (string, string) {
	if strings.Contains(strings.ToLower(string(client.ServerVersion())), "windows") {
		return "windows", ""
	}
	out, err := run(client, `uname -s 2>/dev/null || true; if [ -r /etc/os-release ]; then . /etc/os-release; printf '%s\n%s\n' "$ID" "$ID_LIKE"; fi`)
	if err != nil {
		return "", ""
	}
	lines := strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
	platform := platformFromName(valueAt(lines, 0))
	if platform != "linux" {
		return platform, ""
	}
	return platform, distributionFromRelease(valueAt(lines, 1), valueAt(lines, 2))
}

func valueAt(values []string, index int) string {
	if index >= len(values) {
		return ""
	}
	return strings.TrimSpace(values[index])
}

func distributionFromRelease(id, idLike string) string {
	id = strings.ToLower(strings.Trim(strings.TrimSpace(id), `"'`))
	idLike = strings.ToLower(strings.Trim(strings.TrimSpace(idLike), `"'`))
	aliases := map[string]string{
		"amzn": "amazon", "ol": "oracle", "opensuse-leap": "opensuse",
		"opensuse-tumbleweed": "opensuse", "sles": "opensuse",
		"rhel": "rhel", "redhat": "rhel", "redhatenterpriseserver": "rhel",
	}
	if canonical := aliases[id]; canonical != "" {
		return canonical
	}
	known := map[string]bool{
		"ubuntu": true, "debian": true, "linuxmint": true, "pop": true,
		"elementary": true, "centos": true, "rocky": true, "almalinux": true,
		"fedora": true, "arch": true, "manjaro": true, "endeavouros": true,
		"alpine": true, "opensuse": true, "gentoo": true, "kali": true,
		"raspbian": true, "nixos": true, "void": true, "amazon": true,
		"oracle": true, "proxmox": true,
	}
	if known[id] {
		return id
	}
	for _, family := range strings.Fields(idLike) {
		family = strings.Trim(family, `"'`)
		if canonical := aliases[family]; canonical != "" {
			return canonical
		}
		if known[family] {
			return family
		}
	}
	if id != "" && len(id) <= 32 {
		return id
	}
	return ""
}

func platformFromName(value string) string {
	name := strings.ToLower(strings.TrimSpace(value))
	switch {
	case strings.Contains(name, "darwin"):
		return "macos"
	case strings.Contains(name, "linux"):
		return "linux"
	case strings.Contains(name, "mingw"), strings.Contains(name, "msys"), strings.Contains(name, "cygwin"), strings.Contains(name, "windows"):
		return "windows"
	case strings.Contains(name, "freebsd"), strings.Contains(name, "openbsd"), strings.Contains(name, "netbsd"), strings.Contains(name, "dragonfly"):
		return "bsd"
	case name != "":
		return "unix"
	default:
		return ""
	}
}
func cleanOutput(out []byte, err error) string {
	msg := strings.TrimSpace(string(out))
	if msg == "" && err != nil {
		msg = err.Error()
	}
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return msg
}

func (s *Session) readLoop(r io.Reader) {
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			s.broadcast(Event{Type: "output", Data: base64.StdEncoding.EncodeToString(buf[:n])})
		}
		if err != nil {
			status := "background"
			if s.Meta().SessionMode == "normal" {
				status = "ended"
			}
			s.close(status, err.Error())
			return
		}
	}
}
func (s *Session) broadcast(ev Event) {
	s.mu.Lock()
	if ev.Type == "output" {
		raw, _ := base64.StdEncoding.DecodeString(ev.Data)
		s.buffer.Write(raw)
	}
	for _, ch := range s.subs {
		select {
		case ch <- ev:
		default:
		}
	}
	s.mu.Unlock()
}
func (s *Session) Subscribe(clientID string) (<-chan Event, []byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan Event, 128)
	s.subs[clientID] = ch
	if s.controller == "" || time.Since(s.controllerSeen) > 20*time.Second {
		s.controller = clientID
		s.controllerSeen = time.Now()
	}
	data, trunc := s.buffer.Bytes()
	return ch, data, trunc
}
func (s *Session) Unsubscribe(clientID string) {
	s.mu.Lock()
	if ch := s.subs[clientID]; ch != nil {
		delete(s.subs, clientID)
		close(ch)
	}
	if s.pendingControl == clientID {
		s.pendingControl = ""
	}
	grant := ""
	if s.controller == clientID {
		s.controller = ""
		if _, waiting := s.subs[s.pendingControl]; waiting {
			grant = s.pendingControl
			s.pendingControl = ""
			s.controller = grant
			s.controllerSeen = time.Now()
		}
	}
	s.mu.Unlock()
	if grant != "" {
		s.broadcast(Event{Type: "controller", Controller: grant})
	}
}
func (s *Session) RequestControl(clientID string) bool {
	s.mu.Lock()
	if s.controller == clientID {
		s.controllerSeen = time.Now()
		s.mu.Unlock()
		return true
	}
	if s.controller == "" || time.Since(s.controllerSeen) > 30*time.Second {
		s.controller = clientID
		s.controllerSeen = time.Now()
		s.pendingControl = ""
		s.mu.Unlock()
		s.broadcast(Event{Type: "controller", Controller: clientID})
		return true
	}
	controllerChannel := s.subs[s.controller]
	s.pendingControl = clientID
	if controllerChannel != nil {
		select {
		case controllerChannel <- Event{Type: "control_request", Requester: clientID}:
		default:
		}
	}
	s.mu.Unlock()
	return false
}
func (s *Session) RespondControl(clientID, requester string, approved bool) bool {
	s.mu.Lock()
	if s.controller != clientID || s.pendingControl != requester {
		s.mu.Unlock()
		return false
	}
	s.pendingControl = ""
	requesterChannel := s.subs[requester]
	if approved && requesterChannel != nil {
		s.controller = requester
		s.controllerSeen = time.Now()
	}
	if approved && requesterChannel != nil {
		s.mu.Unlock()
		s.broadcast(Event{Type: "controller", Controller: requester})
		return true
	}
	if requesterChannel != nil {
		select {
		case requesterChannel <- Event{Type: "control_denied"}:
		default:
		}
	}
	s.mu.Unlock()
	return false
}
func (s *Session) ReleaseControl(clientID string) bool {
	s.mu.Lock()
	if s.controller != clientID {
		s.mu.Unlock()
		return false
	}
	s.controller = ""
	grant := s.pendingControl
	if _, exists := s.subs[grant]; !exists {
		grant = ""
	}
	s.pendingControl = ""
	if grant != "" {
		s.controller = grant
		s.controllerSeen = time.Now()
	}
	s.mu.Unlock()
	s.broadcast(Event{Type: "controller", Controller: grant})
	return true
}
func (s *Session) TouchControl(clientID string) {
	s.mu.Lock()
	if s.controller == clientID {
		s.controllerSeen = time.Now()
	}
	s.mu.Unlock()
}
func (s *Session) IsController(clientID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.controller == clientID {
		s.controllerSeen = time.Now()
		return true
	}
	return false
}
func (s *Session) Write(clientID string, data []byte) error {
	if !s.IsController(clientID) {
		return ErrNotController
	}
	s.mu.RLock()
	w := s.stdin
	closed := s.closed
	s.mu.RUnlock()
	if closed || w == nil {
		return errors.New("terminal not attached")
	}
	_, err := w.Write(data)
	return err
}

func validTerminalColor(value string) bool {
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	for _, ch := range value[1:] {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
			return false
		}
	}
	return true
}

func (s *Session) SetTerminalColors(clientID, foreground, background string) error {
	if !s.IsController(clientID) {
		return ErrNotController
	}
	if !validTerminalColor(foreground) || !validTerminalColor(background) {
		return errors.New("invalid terminal colors")
	}
	s.mu.RLock()
	client, meta, closed := s.sshClient, s.meta, s.closed
	s.mu.RUnlock()
	if closed || client == nil {
		return errors.New("terminal not attached")
	}
	if meta.SessionMode == "normal" {
		return nil
	}
	style := shellQuote("fg=" + foreground + ",bg=" + background)
	cmd := fmt.Sprintf(
		"tmux -L %s set-option -t %s window-style %s \\; set-option -t %s window-active-style %s",
		meta.TmuxSocket, meta.TmuxName, style, meta.TmuxName, style,
	)
	if out, err := run(client, cmd); err != nil {
		return fmt.Errorf("configure terminal colors: %s", cleanOutput(out, err))
	}
	return nil
}

func (s *Session) Resize(clientID string, rows, cols int) error {
	if !s.IsController(clientID) {
		return ErrNotController
	}
	if rows < 2 || cols < 2 || rows > 500 || cols > 1000 {
		return errors.New("invalid terminal size")
	}
	s.mu.RLock()
	ss := s.sshSession
	s.mu.RUnlock()
	if ss == nil {
		return errors.New("terminal not attached")
	}
	return ss.WindowChange(rows, cols)
}
func (s *Session) killRemote(ctx context.Context) error {
	s.mu.RLock()
	client := s.sshClient
	meta := s.meta
	s.mu.RUnlock()
	if client == nil {
		return errors.New("SSH connection unavailable")
	}
	if meta.SessionMode == "normal" {
		return nil
	}
	if err := validateOwnership(client, meta); err != nil {
		return err
	}
	out, err := run(client, fmt.Sprintf("tmux -L %s kill-session -t %s", meta.TmuxSocket, meta.TmuxName))
	if err != nil && !strings.Contains(string(out), "can't find session") {
		return fmt.Errorf("terminate tmux: %s", cleanOutput(out, err))
	}
	return nil
}
func (s *Session) close(status, msg string) {
	s.mu.Lock()
	if s.closed {
		if status == "ended" && s.meta.Status != "ended" {
			s.meta.Status = "ended"
			s.meta.LastError = msg
			s.mu.Unlock()
			_ = s.manager.store.UpdateTerminalStatus(s.meta.UserID, s.meta.ID, status, msg)
			s.manager.mu.Lock()
			delete(s.manager.sessions, s.meta.ID)
			s.manager.mu.Unlock()
			return
		}
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.meta.Status = status
	s.meta.LastError = msg
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.sshSession != nil {
		_ = s.sshSession.Close()
	}
	if s.sshClient != nil {
		_ = s.sshClient.Close()
	}
	for id, ch := range s.subs {
		select {
		case ch <- Event{Type: "status", Status: status, Message: msg}:
		default:
		}
		close(ch)
		delete(s.subs, id)
	}
	s.mu.Unlock()
	_ = s.manager.store.UpdateTerminalStatus(s.meta.UserID, s.meta.ID, status, msg)
	s.manager.mu.Lock()
	delete(s.manager.sessions, s.meta.ID)
	s.manager.mu.Unlock()
}

type ringBuffer struct {
	max       int
	buf       []byte
	truncated bool
}

func newRingBuffer(max int) *ringBuffer { return &ringBuffer{max: max} }
func (r *ringBuffer) Write(p []byte) {
	if len(p) >= r.max {
		r.buf = append(r.buf[:0], p[len(p)-r.max:]...)
		r.truncated = true
		return
	}
	overflow := len(r.buf) + len(p) - r.max
	if overflow > 0 {
		copy(r.buf, r.buf[overflow:])
		r.buf = r.buf[:len(r.buf)-overflow]
		r.truncated = true
	}
	r.buf = append(r.buf, p...)
}
func (r *ringBuffer) Bytes() ([]byte, bool) { return bytes.Clone(r.buf), r.truncated }
