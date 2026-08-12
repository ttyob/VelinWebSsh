package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"velin-webssh/internal/config"
	"velin-webssh/internal/security"
	"velin-webssh/internal/store"
	"velin-webssh/internal/terminal"
)

const cookieName = "velin_session"

type API struct {
	cfg       config.Config
	store     *store.Store
	vault     *security.Vault
	terminals *terminal.Manager
	started   time.Time
}

type contextKey string

const userKey contextKey = "user"

func New(cfg config.Config, s *store.Store, v *security.Vault, t *terminal.Manager) *API {
	return &API{cfg: cfg, store: s, vault: v, terminals: t, started: time.Now()}
}

func (a *API) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(a.recoverer, a.requestLog, a.securityHeaders)
	r.Get("/api/health", a.health)
	r.Post("/api/auth/login", a.login)
	r.Group(func(r chi.Router) {
		r.Use(a.authenticate)
		r.Get("/api/auth/me", a.me)
		r.Post("/api/auth/password", a.changePassword)
		r.Post("/api/auth/logout", a.logout)
		r.Get("/api/auth/devices", a.devices)
		r.Delete("/api/auth/devices/{id}", a.deleteDevice)
		r.Get("/api/hosts", a.hosts)
		r.Post("/api/hosts", a.saveHost)
		r.Put("/api/hosts/{id}", a.saveHost)
		r.Delete("/api/hosts/{id}", a.deleteHost)
		r.Get("/api/credentials", a.credentials)
		r.Post("/api/credentials", a.saveCredential)
		r.Delete("/api/credentials/{id}", a.deleteCredential)
		r.Get("/api/sessions", a.sessions)
		r.Post("/api/sessions", a.createSession)
		r.Post("/api/sessions/{id}/restore", a.restoreSession)
		r.Delete("/api/sessions/{id}", a.terminateSession)
		r.Get("/api/workspace", a.workspace)
		r.Put("/api/workspace", a.saveWorkspace)
		r.Get("/api/preferences", a.preferences)
		r.Put("/api/preferences", a.savePreferences)
		r.Get("/api/audit", a.audit)
		r.Group(func(r chi.Router) {
			r.Use(a.adminOnly)
			r.Get("/api/admin/users", a.users)
			r.Post("/api/admin/users", a.createUser)
			r.Patch("/api/admin/users/{id}", a.updateUser)
			r.Get("/api/admin/stats", a.stats)
			r.Post("/api/admin/backup", a.backup)
		})
	})
	r.Get("/ws/sessions/{id}", a.terminalWS)
	r.Handle("/*", a.staticHandler())
	return r
}

func (a *API) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				slog.Error("panic", "error", v, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal_error", "服务内部错误")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
func (a *API) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start).String())
	})
}
func (a *API) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self' ws: wss:; img-src 'self' data:; font-src 'self' data:")
		next.ServeHTTP(w, r)
	})
}

func (a *API) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := a.currentUser(r)
		if err != nil || u.Disabled {
			writeError(w, http.StatusUnauthorized, "unauthorized", "请重新登录")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	})
}
func (a *API) adminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if currentUser(r).Role != "admin" {
			writeError(w, http.StatusForbidden, "forbidden", "需要管理员权限")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (a *API) currentUser(r *http.Request) (store.User, error) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return store.User{}, err
	}
	return a.store.UserByToken(security.TokenHash(cookie.Value))
}
func currentUser(r *http.Request) store.User {
	u, _ := r.Context().Value(userKey).(store.User)
	return u
}
func ipOf(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username, Password string
		Remember           bool
	}
	if !decode(w, r, &in) {
		return
	}
	u, hash, err := a.store.UserByUsername(strings.TrimSpace(in.Username))
	if err != nil || u.Disabled || !security.VerifyPassword(hash, in.Password) {
		a.store.Audit(u.ID, "login_failed", "user", u.ID, ipOf(r), nil)
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "用户名或密码错误")
		return
	}
	token, err := security.RandomToken(32)
	if err != nil {
		writeError(w, 500, "internal_error", "无法创建登录会话")
		return
	}
	ttl := 24 * time.Hour
	if in.Remember {
		ttl = a.cfg.SessionTTL
	}
	sid := uuid.NewString()
	if err = a.store.CreateAuthSession(sid, u.ID, security.TokenHash(token), r.UserAgent(), ipOf(r), time.Now().Add(ttl)); err != nil {
		writeError(w, 500, "database_error", "登录失败")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, Secure: a.cfg.CookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: int(ttl.Seconds())})
	a.store.Audit(u.ID, "login", "user", u.ID, ipOf(r), nil)
	writeJSON(w, 200, map[string]any{"user": u})
}
func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		_ = a.store.DeleteAuthSession(security.TokenHash(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.cfg.CookieSecure, SameSite: http.SameSiteStrictMode})
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *API) me(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, currentUser(r)) }
func (a *API) changePassword(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct{ CurrentPassword, NewPassword string }
	if !decode(w, r, &in) {
		return
	}
	if len(in.NewPassword) < 10 {
		writeError(w, 400, "weak_password", "新密码至少 10 位")
		return
	}
	cookie, _ := r.Cookie(cookieName)
	currentTokenHash := ""
	if cookie != nil {
		currentTokenHash = security.TokenHash(cookie.Value)
	}
	if err := a.store.ChangePassword(u.ID, in.CurrentPassword, in.NewPassword, currentTokenHash); err != nil {
		writeError(w, 400, "password_change_failed", "当前密码错误")
		return
	}
	a.store.Audit(u.ID, "password_changed", "user", u.ID, ipOf(r), nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *API) devices(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	rows, err := a.store.DB.Query(`SELECT id,user_agent,ip,created_at,last_seen_at,expires_at FROM auth_sessions WHERE user_id=? ORDER BY last_seen_at DESC`, u.ID)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, agent, ip string
		var created, seen, expires time.Time
		_ = rows.Scan(&id, &agent, &ip, &created, &seen, &expires)
		out = append(out, map[string]any{"id": id, "user_agent": agent, "ip": ip, "created_at": created, "last_seen_at": seen, "expires_at": expires})
	}
	writeJSON(w, 200, nonNil(out))
}
func (a *API) deleteDevice(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	_, err := a.store.DB.Exec(`DELETE FROM auth_sessions WHERE id=? AND user_id=?`, chi.URLParam(r, "id"), u.ID)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *API) hosts(w http.ResponseWriter, r *http.Request) {
	out, err := a.store.Hosts(currentUser(r).ID)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	writeJSON(w, 200, nonNil(out))
}
func (a *API) saveHost(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var h store.Host
	if !decode(w, r, &h) {
		return
	}
	h.ID = chi.URLParam(r, "id")
	if h.ID == "" {
		h.ID = uuid.NewString()
	}
	h.UserID = u.ID
	h.Name = strings.TrimSpace(h.Name)
	h.Address = strings.TrimSpace(h.Address)
	h.Username = strings.TrimSpace(h.Username)
	if h.Port == 0 {
		h.Port = 22
	}
	if h.Name == "" || h.Address == "" || h.Username == "" || h.Port < 1 || h.Port > 65535 {
		writeError(w, 400, "invalid_host", "请完整填写主机名称、地址、端口和用户名")
		return
	}
	if h.CredentialID != "" {
		if _, err := a.store.Credential(u.ID, h.CredentialID); err != nil {
			writeError(w, 400, "invalid_credential", "凭据不存在")
			return
		}
	}
	if err := a.store.SaveHost(h); err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	a.store.Audit(u.ID, "host_saved", "host", h.ID, ipOf(r), map[string]any{"name": h.Name})
	saved, _ := a.store.Host(u.ID, h.ID)
	writeJSON(w, 200, saved)
}
func (a *API) deleteHost(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id := chi.URLParam(r, "id")
	if err := a.store.DeleteHost(u.ID, id); err != nil {
		writeError(w, 409, "host_in_use", err.Error())
		return
	}
	a.store.Audit(u.ID, "host_deleted", "host", id, ipOf(r), nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *API) credentials(w http.ResponseWriter, r *http.Request) {
	out, err := a.store.Credentials(currentUser(r).ID)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	writeJSON(w, 200, nonNil(out))
}
func (a *API) saveCredential(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct{ ID, Name, Kind, Secret, Passphrase string }
	if !decode(w, r, &in) {
		return
	}
	if in.Name == "" || (in.Kind != "password" && in.Kind != "key") || in.Secret == "" {
		writeError(w, 400, "invalid_credential", "请填写凭据名称和内容")
		return
	}
	enc, err := a.vault.Encrypt(in.Secret)
	if err != nil {
		writeError(w, 500, "encryption_error", "凭据加密失败")
		return
	}
	pass := ""
	if in.Passphrase != "" {
		pass, err = a.vault.Encrypt(in.Passphrase)
		if err != nil {
			writeError(w, 500, "encryption_error", "凭据加密失败")
			return
		}
	}
	if in.ID == "" {
		in.ID = uuid.NewString()
	}
	c := store.Credential{ID: in.ID, UserID: u.ID, Name: in.Name, Kind: in.Kind, Secret: enc, Passphrase: pass}
	if err = a.store.SaveCredential(c); err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	a.store.Audit(u.ID, "credential_saved", "credential", c.ID, ipOf(r), map[string]string{"kind": c.Kind})
	saved, _ := a.store.Credential(u.ID, c.ID)
	saved.Secret = ""
	saved.Passphrase = ""
	writeJSON(w, 200, saved)
}
func (a *API) deleteCredential(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id := chi.URLParam(r, "id")
	if err := a.store.DeleteCredential(u.ID, id); err != nil {
		writeError(w, 409, "credential_in_use", err.Error())
		return
	}
	a.store.Audit(u.ID, "credential_deleted", "credential", id, ipOf(r), nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

type sessionInput struct{ HostID, CredentialID, Secret, Passphrase, TrustFingerprint, Name string }

func (a *API) createSession(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in sessionInput
	if !decode(w, r, &in) {
		return
	}
	host, err := a.store.Host(u.ID, in.HostID)
	if err != nil {
		writeError(w, 404, "host_not_found", "主机不存在")
		return
	}
	var cred store.Credential
	if in.CredentialID == "" {
		in.CredentialID = host.CredentialID
	}
	if in.CredentialID != "" {
		cred, err = a.store.Credential(u.ID, in.CredentialID)
		if err != nil {
			writeError(w, 400, "credential_not_found", "凭据不存在")
			return
		}
	}
	meta, err := a.terminals.Create(r.Context(), u.ID, terminal.CreateRequest{Host: host, Credential: cred, Secret: in.Secret, Passphrase: in.Passphrase, TrustFingerprint: in.TrustFingerprint, Name: in.Name})
	if err != nil {
		var hk *terminal.HostKeyError
		if errors.As(err, &hk) {
			writeJSONStatus(w, 409, map[string]any{"code": hk.Kind, "message": "请确认远程主机指纹", "fingerprint": hk.Fingerprint})
			return
		}
		writeError(w, 502, "connection_failed", err.Error())
		return
	}
	a.store.Audit(u.ID, "session_created", "terminal", meta.ID, ipOf(r), map[string]string{"host_id": host.ID})
	writeJSON(w, 201, meta)
}
func (a *API) sessions(w http.ResponseWriter, r *http.Request) {
	out, err := a.store.Terminals(currentUser(r).ID)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	writeJSON(w, 200, nonNil(out))
}
func (a *API) restoreSession(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in sessionInput
	if r.ContentLength > 0 && !decode(w, r, &in) {
		return
	}
	s, err := a.terminals.Restore(r.Context(), u.ID, chi.URLParam(r, "id"), in.Secret, in.Passphrase, in.TrustFingerprint)
	if err != nil {
		var hk *terminal.HostKeyError
		if errors.As(err, &hk) {
			writeJSONStatus(w, 409, map[string]any{"code": hk.Kind, "message": "请确认远程主机指纹", "fingerprint": hk.Fingerprint})
			return
		}
		writeError(w, 502, "restore_failed", err.Error())
		return
	}
	writeJSON(w, 200, s.Meta())
}
func (a *API) terminateSession(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in sessionInput
	if r.ContentLength > 0 && !decode(w, r, &in) {
		return
	}
	id := chi.URLParam(r, "id")
	if err := a.terminals.Terminate(r.Context(), u.ID, id, in.Secret, in.Passphrase); err != nil {
		writeError(w, 502, "terminate_failed", err.Error())
		return
	}
	a.store.Audit(u.ID, "session_terminated", "terminal", id, ipOf(r), nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *API) workspace(w http.ResponseWriter, r *http.Request) {
	raw, version, err := a.store.Workspace(currentUser(r).ID)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"layout": raw, "version": version})
}
func (a *API) saveWorkspace(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct {
		Layout  json.RawMessage `json:"layout"`
		Version int             `json:"version"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.Layout) > 256*1024 {
		writeError(w, 413, "workspace_too_large", "工作区数据过大")
		return
	}
	if err := a.store.SaveWorkspace(u.ID, in.Layout, in.Version); err != nil {
		writeError(w, 409, "workspace_conflict", err.Error())
		return
	}
	raw, ver, _ := a.store.Workspace(u.ID)
	writeJSON(w, 200, map[string]any{"layout": raw, "version": ver})
}
func (a *API) preferences(w http.ResponseWriter, r *http.Request) {
	raw, err := a.store.Preferences(currentUser(r).ID)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	writeJSON(w, 200, raw)
}
func (a *API) savePreferences(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
	if err != nil || !json.Valid(raw) {
		writeError(w, 400, "invalid_preferences", "设置格式无效")
		return
	}
	if err = a.store.SavePreferences(u.ID, raw); err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	writeJSON(w, 200, json.RawMessage(raw))
}

func (a *API) audit(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	query := `SELECT id,event_type,resource_type,resource_id,ip,details,created_at FROM audit_events WHERE user_id=?`
	args := []any{u.ID}
	if u.Role == "admin" && r.URL.Query().Get("all") == "true" {
		query = `SELECT id,event_type,resource_type,resource_id,ip,details,created_at FROM audit_events`
		args = nil
	}
	query += ` ORDER BY id DESC LIMIT 200`
	rows, err := a.store.DB.Query(query, args...)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id int64
		var event, rt, rid, ip, details string
		var at time.Time
		_ = rows.Scan(&id, &event, &rt, &rid, &ip, &details, &at)
		out = append(out, map[string]any{"id": id, "event_type": event, "resource_type": rt, "resource_id": rid, "ip": ip, "details": json.RawMessage(details), "created_at": at})
	}
	writeJSON(w, 200, nonNil(out))
}

func (a *API) users(w http.ResponseWriter, r *http.Request) {
	rows, err := a.store.DB.Query(`SELECT id,username,role,disabled,created_at FROM users ORDER BY created_at`)
	if err != nil {
		writeError(w, 500, "database_error", err.Error())
		return
	}
	defer rows.Close()
	var out []store.User
	for rows.Next() {
		var u store.User
		_ = rows.Scan(&u.ID, &u.Username, &u.Role, &u.Disabled, &u.CreatedAt)
		out = append(out, u)
	}
	writeJSON(w, 200, nonNil(out))
}
func (a *API) createUser(w http.ResponseWriter, r *http.Request) {
	var in struct{ Username, Password, Role string }
	if !decode(w, r, &in) {
		return
	}
	if len(in.Username) < 3 || len(in.Password) < 10 {
		writeError(w, 400, "weak_account", "用户名至少 3 位，密码至少 10 位")
		return
	}
	if in.Role != "admin" {
		in.Role = "user"
	}
	hash, err := security.HashPassword(in.Password)
	if err != nil {
		writeError(w, 500, "internal_error", err.Error())
		return
	}
	id := uuid.NewString()
	if err = a.store.CreateUser(id, in.Username, hash, in.Role); err != nil {
		writeError(w, 409, "user_exists", "用户名已存在")
		return
	}
	a.store.Audit(currentUser(r).ID, "user_created", "user", id, ipOf(r), map[string]string{"username": in.Username})
	u, _ := a.store.UserByID(id)
	writeJSON(w, 201, u)
}
func (a *API) updateUser(w http.ResponseWriter, r *http.Request) {
	actor := currentUser(r)
	var in struct {
		Disabled *bool
		Password string
	}
	if !decode(w, r, &in) {
		return
	}
	id := chi.URLParam(r, "id")
	if id == actor.ID && in.Disabled != nil && *in.Disabled {
		writeError(w, 400, "cannot_disable_self", "不能禁用当前账户")
		return
	}
	if in.Disabled != nil {
		_, _ = a.store.DB.Exec(`UPDATE users SET disabled=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, *in.Disabled, id)
	}
	if in.Password != "" {
		if len(in.Password) < 10 {
			writeError(w, 400, "weak_password", "密码至少 10 位")
			return
		}
		hash, _ := security.HashPassword(in.Password)
		_, _ = a.store.DB.Exec(`UPDATE users SET password_hash=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, hash, id)
		_, _ = a.store.DB.Exec(`DELETE FROM auth_sessions WHERE user_id=?`, id)
	}
	a.store.Audit(actor.ID, "user_updated", "user", id, ipOf(r), nil)
	u, err := a.store.UserByID(id)
	if err != nil {
		writeError(w, 404, "not_found", "用户不存在")
		return
	}
	writeJSON(w, 200, u)
}
func (a *API) stats(w http.ResponseWriter, r *http.Request) {
	var users, hosts, sessions int
	_ = a.store.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users)
	_ = a.store.DB.QueryRow(`SELECT COUNT(*) FROM hosts`).Scan(&hosts)
	_ = a.store.DB.QueryRow(`SELECT COUNT(*) FROM terminal_sessions WHERE status NOT IN ('ended')`).Scan(&sessions)
	writeJSON(w, 200, map[string]any{"users": users, "hosts": hosts, "sessions": sessions, "uptime_seconds": int(time.Since(a.started).Seconds()), "deployment_id": a.cfg.DeploymentID})
}
func (a *API) backup(w http.ResponseWriter, r *http.Request) {
	name := "velin-" + time.Now().UTC().Format("20060102-150405") + ".db"
	path := filepath.Join(a.cfg.DataDir, name)
	if err := a.store.Backup(r.Context(), path); err != nil {
		writeError(w, 500, "backup_failed", err.Error())
		return
	}
	a.store.Audit(currentUser(r).ID, "backup_created", "system", name, ipOf(r), nil)
	writeJSON(w, 201, map[string]string{"file": name})
}
func (a *API) health(w http.ResponseWriter, r *http.Request) {
	if err := a.store.DB.PingContext(r.Context()); err != nil {
		writeError(w, 503, "not_ready", "数据库不可用")
		return
	}
	writeJSON(w, 200, map[string]any{"status": "ok", "uptime_seconds": int(time.Since(a.started).Seconds())})
}

func (a *API) terminalWS(w http.ResponseWriter, r *http.Request) {
	u, err := a.currentUser(r)
	if err != nil || u.Disabled {
		writeError(w, 401, "unauthorized", "请重新登录")
		return
	}
	origin := r.Header.Get("Origin")
	if origin != "" && !sameOrigin(r, origin) {
		writeError(w, 403, "origin_rejected", "来源不允许")
		return
	}
	id := chi.URLParam(r, "id")
	s, err := a.terminals.Get(u.ID, id)
	if err != nil {
		writeError(w, 409, "session_not_attached", "请先恢复会话")
		return
	}
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }, ReadBufferSize: 4096, WriteBufferSize: 32768}
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	clientID := uuid.NewString()
	events, history, truncated := s.Subscribe(clientID)
	defer s.Unsubscribe(clientID)
	controller := ""
	if s.IsController(clientID) {
		controller = clientID
	}
	_ = conn.WriteJSON(terminal.Event{Type: "hello", ClientID: clientID, Controller: controller, Data: base64.StdEncoding.EncodeToString(history), Truncated: truncated})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range events {
			if conn.WriteJSON(ev) != nil {
				return
			}
		}
	}()
	conn.SetReadLimit(128 * 1024)
	_ = conn.SetReadDeadline(time.Now().Add(45 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(45 * time.Second)) })
	for {
		var msg struct {
			Type, Data string
			Rows, Cols int
		}
		if err = conn.ReadJSON(&msg); err != nil {
			return
		}
		switch msg.Type {
		case "input":
			raw, e := base64.StdEncoding.DecodeString(msg.Data)
			if e == nil {
				_ = s.Write(clientID, raw)
			}
		case "resize":
			_ = s.Resize(clientID, msg.Rows, msg.Cols)
		case "request_control":
			if s.RequestControl(clientID) {
				_ = conn.WriteJSON(terminal.Event{Type: "control_granted", Controller: clientID})
			} else {
				_ = conn.WriteJSON(terminal.Event{Type: "control_denied"})
			}
		case "ping":
			_ = conn.WriteJSON(terminal.Event{Type: "pong"})
		}
		select {
		case <-done:
			return
		default:
		}
	}
}

func sameOrigin(r *http.Request, origin string) bool {
	host := strings.TrimPrefix(strings.TrimPrefix(origin, "https://"), "http://")
	return strings.TrimSuffix(host, "/") == r.Host
}
func (a *API) staticHandler() http.Handler {
	dist := a.cfg.WebDist
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/") {
			http.NotFound(w, r)
			return
		}
		path := filepath.Join(dist, filepath.Clean("/"+r.URL.Path))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			http.ServeFile(w, r, path)
			return
		}
		index := filepath.Join(dist, "index.html")
		if _, err := os.Stat(index); err == nil {
			http.ServeFile(w, r, index)
			return
		}
		http.Error(w, "Velin frontend has not been built", http.StatusServiceUnavailable)
	})
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, 400, "invalid_json", "请求格式无效")
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeJSONStatus(w http.ResponseWriter, status int, v any) { writeJSON(w, status, v) }
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}
func nonNil[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

var _ fs.FS
var _ = strconv.Itoa
