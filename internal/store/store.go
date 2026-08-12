package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
	"velin-webssh/internal/security"
)

type Store struct{ DB *sql.DB }

type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	Disabled  bool      `json:"disabled"`
	CreatedAt time.Time `json:"createdAt"`
}
type Host struct {
	ID           string    `json:"id"`
	UserID       string    `json:"userID"`
	Name         string    `json:"name"`
	Address      string    `json:"address"`
	Username     string    `json:"username"`
	GroupName    string    `json:"groupName"`
	Tags         string    `json:"tags"`
	Notes        string    `json:"notes"`
	CredentialID string    `json:"credentialID"`
	Port         int       `json:"port"`
	Favorite     bool      `json:"favorite"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}
type Credential struct {
	ID         string    `json:"id"`
	UserID     string    `json:"userID"`
	Name       string    `json:"name"`
	Kind       string    `json:"kind"`
	Secret     string    `json:"-"`
	Passphrase string    `json:"-"`
	CreatedAt  time.Time `json:"createdAt"`
}
type TerminalSession struct {
	ID           string    `json:"id"`
	UserID       string    `json:"userID"`
	HostID       string    `json:"hostID"`
	CredentialID string    `json:"credentialID"`
	Name         string    `json:"name"`
	RemoteUser   string    `json:"remoteUser"`
	TmuxSocket   string    `json:"tmuxSocket"`
	TmuxName     string    `json:"tmuxName"`
	OwnerMarker  string    `json:"-"`
	Status       string    `json:"status"`
	LastError    string    `json:"lastError"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;`); err != nil {
		return nil, err
	}
	s := &Store{DB: db}
	return s, s.migrate()
}

func (s *Store) migrate() error {
	_, err := s.DB.Exec(`
CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE COLLATE NOCASE, password_hash TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'user', disabled INTEGER NOT NULL DEFAULT 0, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE IF NOT EXISTS auth_sessions (id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, token_hash TEXT NOT NULL UNIQUE, user_agent TEXT NOT NULL DEFAULT '', ip TEXT NOT NULL DEFAULT '', expires_at DATETIME NOT NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, last_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE INDEX IF NOT EXISTS idx_auth_token ON auth_sessions(token_hash);
CREATE TABLE IF NOT EXISTS credentials (id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, name TEXT NOT NULL, kind TEXT NOT NULL, secret_enc TEXT NOT NULL, passphrase_enc TEXT NOT NULL DEFAULT '', created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE IF NOT EXISTS hosts (id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, name TEXT NOT NULL, address TEXT NOT NULL, port INTEGER NOT NULL DEFAULT 22, username TEXT NOT NULL, credential_id TEXT REFERENCES credentials(id) ON DELETE SET NULL, group_name TEXT NOT NULL DEFAULT '', tags TEXT NOT NULL DEFAULT '', notes TEXT NOT NULL DEFAULT '', favorite INTEGER NOT NULL DEFAULT 0, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE INDEX IF NOT EXISTS idx_hosts_user ON hosts(user_id, name);
CREATE TABLE IF NOT EXISTS known_host_keys (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id TEXT NOT NULL, address TEXT NOT NULL, port INTEGER NOT NULL, fingerprint TEXT NOT NULL, public_key TEXT NOT NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE(user_id,address,port));
CREATE TABLE IF NOT EXISTS terminal_sessions (id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, host_id TEXT NOT NULL, credential_id TEXT NOT NULL DEFAULT '', name TEXT NOT NULL, remote_user TEXT NOT NULL, tmux_socket TEXT NOT NULL, tmux_name TEXT NOT NULL, owner_marker TEXT NOT NULL, status TEXT NOT NULL, last_error TEXT NOT NULL DEFAULT '', created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE INDEX IF NOT EXISTS idx_terminal_user ON terminal_sessions(user_id, updated_at DESC);
CREATE TABLE IF NOT EXISTS workspaces (user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE, layout_json TEXT NOT NULL DEFAULT '{}', version INTEGER NOT NULL DEFAULT 1, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE IF NOT EXISTS user_preferences (user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE, preferences_json TEXT NOT NULL DEFAULT '{}', updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE IF NOT EXISTS audit_events (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id TEXT NOT NULL DEFAULT '', event_type TEXT NOT NULL, resource_type TEXT NOT NULL DEFAULT '', resource_id TEXT NOT NULL DEFAULT '', ip TEXT NOT NULL DEFAULT '', details TEXT NOT NULL DEFAULT '{}', created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);
`)
	return err
}

func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) CreateUser(id, username, hash, role string) error {
	_, err := s.DB.Exec(`INSERT INTO users(id,username,password_hash,role) VALUES(?,?,?,?)`, id, username, hash, role)
	return err
}
func (s *Store) UserCount() (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}
func (s *Store) UserByUsername(username string) (User, string, error) {
	var u User
	var hash string
	err := s.DB.QueryRow(`SELECT id,username,password_hash,role,disabled,created_at FROM users WHERE username=?`, username).Scan(&u.ID, &u.Username, &hash, &u.Role, &u.Disabled, &u.CreatedAt)
	return u, hash, err
}
func (s *Store) UserByID(id string) (User, error) {
	var u User
	err := s.DB.QueryRow(`SELECT id,username,role,disabled,created_at FROM users WHERE id=?`, id).Scan(&u.ID, &u.Username, &u.Role, &u.Disabled, &u.CreatedAt)
	return u, err
}
func (s *Store) CreateAuthSession(id, userID, tokenHash, agent, ip string, expires time.Time) error {
	_, err := s.DB.Exec(`INSERT INTO auth_sessions(id,user_id,token_hash,user_agent,ip,expires_at) VALUES(?,?,?,?,?,?)`, id, userID, tokenHash, agent, ip, expires)
	return err
}
func (s *Store) UserByToken(hash string) (User, error) {
	var u User
	err := s.DB.QueryRow(`SELECT u.id,u.username,u.role,u.disabled,u.created_at FROM auth_sessions a JOIN users u ON u.id=a.user_id WHERE a.token_hash=? AND a.expires_at>CURRENT_TIMESTAMP`, hash).Scan(&u.ID, &u.Username, &u.Role, &u.Disabled, &u.CreatedAt)
	if err == nil {
		_, _ = s.DB.Exec(`UPDATE auth_sessions SET last_seen_at=CURRENT_TIMESTAMP WHERE token_hash=?`, hash)
	}
	return u, err
}
func (s *Store) DeleteAuthSession(hash string) error {
	_, err := s.DB.Exec(`DELETE FROM auth_sessions WHERE token_hash=?`, hash)
	return err
}
func (s *Store) ChangePassword(userID, currentPassword, newPassword, currentTokenHash string) error {
	var hash string
	if err := s.DB.QueryRow(`SELECT password_hash FROM users WHERE id=?`, userID).Scan(&hash); err != nil {
		return err
	}
	if !security.VerifyPassword(hash, currentPassword) {
		return errors.New("current password is incorrect")
	}
	newHash, err := security.HashPassword(newPassword)
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE users SET password_hash=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, newHash, userID); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM auth_sessions WHERE user_id=? AND token_hash<>?`, userID, currentTokenHash); err != nil {
		return err
	}
	err = tx.Commit()
	return err
}

func (s *Store) Hosts(userID string) ([]Host, error) {
	rows, err := s.DB.Query(`SELECT id,user_id,name,address,port,username,COALESCE(credential_id,''),group_name,tags,notes,favorite,created_at,updated_at FROM hosts WHERE user_id=? ORDER BY favorite DESC,name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Host
	for rows.Next() {
		var h Host
		if err := rows.Scan(&h.ID, &h.UserID, &h.Name, &h.Address, &h.Port, &h.Username, &h.CredentialID, &h.GroupName, &h.Tags, &h.Notes, &h.Favorite, &h.CreatedAt, &h.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
func (s *Store) Host(userID, id string) (Host, error) {
	var h Host
	err := s.DB.QueryRow(`SELECT id,user_id,name,address,port,username,COALESCE(credential_id,''),group_name,tags,notes,favorite,created_at,updated_at FROM hosts WHERE user_id=? AND id=?`, userID, id).Scan(&h.ID, &h.UserID, &h.Name, &h.Address, &h.Port, &h.Username, &h.CredentialID, &h.GroupName, &h.Tags, &h.Notes, &h.Favorite, &h.CreatedAt, &h.UpdatedAt)
	return h, err
}
func (s *Store) SaveHost(h Host) error {
	_, err := s.DB.Exec(`INSERT INTO hosts(id,user_id,name,address,port,username,credential_id,group_name,tags,notes,favorite) VALUES(?,?,?,?,?,?,NULLIF(?,''),?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,address=excluded.address,port=excluded.port,username=excluded.username,credential_id=excluded.credential_id,group_name=excluded.group_name,tags=excluded.tags,notes=excluded.notes,favorite=excluded.favorite,updated_at=CURRENT_TIMESTAMP WHERE user_id=excluded.user_id`, h.ID, h.UserID, h.Name, h.Address, h.Port, h.Username, h.CredentialID, h.GroupName, h.Tags, h.Notes, h.Favorite)
	return err
}
func (s *Store) DeleteHost(userID, id string) error {
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM terminal_sessions WHERE user_id=? AND host_id=? AND status NOT IN ('ended')`, userID, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("host has %d active sessions", n)
	}
	_, err := s.DB.Exec(`DELETE FROM hosts WHERE user_id=? AND id=?`, userID, id)
	return err
}

func (s *Store) Credentials(userID string) ([]Credential, error) {
	rows, err := s.DB.Query(`SELECT id,user_id,name,kind,'','',created_at FROM credentials WHERE user_id=? ORDER BY name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Credential
	for rows.Next() {
		var c Credential
		if err := rows.Scan(&c.ID, &c.UserID, &c.Name, &c.Kind, &c.Secret, &c.Passphrase, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) Credential(userID, id string) (Credential, error) {
	var c Credential
	err := s.DB.QueryRow(`SELECT id,user_id,name,kind,secret_enc,passphrase_enc,created_at FROM credentials WHERE user_id=? AND id=?`, userID, id).Scan(&c.ID, &c.UserID, &c.Name, &c.Kind, &c.Secret, &c.Passphrase, &c.CreatedAt)
	return c, err
}
func (s *Store) SaveCredential(c Credential) error {
	_, err := s.DB.Exec(`INSERT INTO credentials(id,user_id,name,kind,secret_enc,passphrase_enc) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,kind=excluded.kind,secret_enc=excluded.secret_enc,passphrase_enc=excluded.passphrase_enc,updated_at=CURRENT_TIMESTAMP WHERE user_id=excluded.user_id`, c.ID, c.UserID, c.Name, c.Kind, c.Secret, c.Passphrase)
	return err
}
func (s *Store) DeleteCredential(userID, id string) error {
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM hosts WHERE user_id=? AND credential_id=?`, userID, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("credential is used by %d hosts", n)
	}
	_, err := s.DB.Exec(`DELETE FROM credentials WHERE user_id=? AND id=?`, userID, id)
	return err
}

func (s *Store) SaveTerminal(t TerminalSession) error {
	_, err := s.DB.Exec(`INSERT INTO terminal_sessions(id,user_id,host_id,credential_id,name,remote_user,tmux_socket,tmux_name,owner_marker,status,last_error) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,last_error=excluded.last_error,updated_at=CURRENT_TIMESTAMP`, t.ID, t.UserID, t.HostID, t.CredentialID, t.Name, t.RemoteUser, t.TmuxSocket, t.TmuxName, t.OwnerMarker, t.Status, t.LastError)
	return err
}
func scanTerminal(row interface{ Scan(...any) error }) (TerminalSession, error) {
	var t TerminalSession
	err := row.Scan(&t.ID, &t.UserID, &t.HostID, &t.CredentialID, &t.Name, &t.RemoteUser, &t.TmuxSocket, &t.TmuxName, &t.OwnerMarker, &t.Status, &t.LastError, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

const terminalCols = `id,user_id,host_id,credential_id,name,remote_user,tmux_socket,tmux_name,owner_marker,status,last_error,created_at,updated_at`

func (s *Store) Terminal(userID, id string) (TerminalSession, error) {
	return scanTerminal(s.DB.QueryRow(`SELECT `+terminalCols+` FROM terminal_sessions WHERE user_id=? AND id=?`, userID, id))
}
func (s *Store) Terminals(userID string) ([]TerminalSession, error) {
	rows, err := s.DB.Query(`SELECT `+terminalCols+` FROM terminal_sessions WHERE user_id=? ORDER BY updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TerminalSession
	for rows.Next() {
		t, e := scanTerminal(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s *Store) UpdateTerminalStatus(userID, id, status, msg string) error {
	_, err := s.DB.Exec(`UPDATE terminal_sessions SET status=?,last_error=?,updated_at=CURRENT_TIMESTAMP WHERE user_id=? AND id=?`, status, msg, userID, id)
	return err
}
func (s *Store) DeleteTerminal(userID, id string) error {
	_, err := s.DB.Exec(`DELETE FROM terminal_sessions WHERE user_id=? AND id=?`, userID, id)
	return err
}

func (s *Store) Workspace(userID string) (json.RawMessage, int, error) {
	var raw string
	var version int
	err := s.DB.QueryRow(`SELECT layout_json,version FROM workspaces WHERE user_id=?`, userID).Scan(&raw, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return json.RawMessage(`{"tabs":[],"panes":[]}`), 0, nil
	}
	return json.RawMessage(raw), version, err
}
func (s *Store) SaveWorkspace(userID string, raw json.RawMessage, version int) error {
	if !json.Valid(raw) {
		return errors.New("invalid workspace")
	}
	res, err := s.DB.Exec(`INSERT INTO workspaces(user_id,layout_json,version) VALUES(?,?,1) ON CONFLICT(user_id) DO UPDATE SET layout_json=excluded.layout_json,version=workspaces.version+1,updated_at=CURRENT_TIMESTAMP WHERE workspaces.version=?`, userID, string(raw), version)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("workspace version conflict")
	}
	return nil
}
func (s *Store) Preferences(userID string) (json.RawMessage, error) {
	var raw string
	err := s.DB.QueryRow(`SELECT preferences_json FROM user_preferences WHERE user_id=?`, userID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return json.RawMessage(`{}`), nil
	}
	return json.RawMessage(raw), err
}
func (s *Store) SavePreferences(userID string, raw json.RawMessage) error {
	if !json.Valid(raw) {
		return errors.New("invalid preferences")
	}
	_, err := s.DB.Exec(`INSERT INTO user_preferences(user_id,preferences_json)VALUES(?,?) ON CONFLICT(user_id)DO UPDATE SET preferences_json=excluded.preferences_json,updated_at=CURRENT_TIMESTAMP`, userID, string(raw))
	return err
}
func (s *Store) Audit(userID, event, resourceType, resourceID, ip string, details any) {
	raw, _ := json.Marshal(details)
	_, _ = s.DB.ExecContext(context.Background(), `INSERT INTO audit_events(user_id,event_type,resource_type,resource_id,ip,details)VALUES(?,?,?,?,?,?)`, userID, event, resourceType, resourceID, ip, string(raw))
}
func (s *Store) KnownHost(userID, address string, port int) (string, string, error) {
	var fp, key string
	err := s.DB.QueryRow(`SELECT fingerprint,public_key FROM known_host_keys WHERE user_id=? AND address=? AND port=?`, userID, address, port).Scan(&fp, &key)
	return fp, key, err
}
func (s *Store) TrustHost(userID, address string, port int, fp, key string) error {
	_, err := s.DB.Exec(`INSERT INTO known_host_keys(user_id,address,port,fingerprint,public_key)VALUES(?,?,?,?,?) ON CONFLICT(user_id,address,port)DO UPDATE SET fingerprint=excluded.fingerprint,public_key=excluded.public_key,created_at=CURRENT_TIMESTAMP`, userID, address, port, fp, key)
	return err
}
func (s *Store) Backup(ctx context.Context, path string) error {
	_, err := s.DB.ExecContext(ctx, `VACUUM INTO ?`, path)
	return err
}
