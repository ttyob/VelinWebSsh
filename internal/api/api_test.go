package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pquerna/otp/totp"
	"velin-webssh/internal/security"
	"velin-webssh/internal/store"
)

func TestParseOpenSSH(t *testing.T) {
	config := `
Host *.internal
  User ignored
Host production
  HostName 192.0.2.10
  User deploy
  Port 2222
  IdentityFile ~/.ssh/id_ed25519
  ProxyJump bastion
  Compression yes
Host broken
  HostName example.invalid
  Port 70000
`
	hosts := parseOpenSSH(config)
	if len(hosts) != 2 {
		t.Fatalf("hosts=%d: %+v", len(hosts), hosts)
	}
	production := hosts[0]
	if production.Alias != "production" || production.HostName != "192.0.2.10" || production.User != "deploy" || production.Port != 2222 || production.IdentityFile != "~/.ssh/id_ed25519" || production.ProxyJump != "bastion" {
		t.Fatalf("production=%+v", production)
	}
	if len(production.Warnings) != 1 || !strings.Contains(production.Warnings[0], "Compression") {
		t.Fatalf("warnings=%v", production.Warnings)
	}
	if hosts[1].Port != 22 || len(hosts[1].Warnings) != 1 || !strings.Contains(hosts[1].Warnings[0], "端口无效") {
		t.Fatalf("broken=%+v", hosts[1])
	}
}

func TestVerifySecondFactor(t *testing.T) {
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "Velin", AccountName: "test"})
	if err != nil {
		t.Fatal(err)
	}
	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	hashes := []string{security.TokenHash("RECOVERY-ONE"), security.TokenHash("RECOVERY-TWO")}
	valid, remaining, used := verifySecondFactor(key.Secret(), hashes, code)
	if !valid || used || len(remaining) != 2 {
		t.Fatalf("totp valid=%v used=%v remaining=%d", valid, used, len(remaining))
	}
	valid, remaining, used = verifySecondFactor(key.Secret(), hashes, " recovery-one ")
	if !valid || !used || len(remaining) != 1 || remaining[0] != hashes[1] {
		t.Fatalf("recovery valid=%v used=%v remaining=%v", valid, used, remaining)
	}
	valid, _, used = verifySecondFactor(key.Secret(), remaining, "RECOVERY-ONE")
	if valid || used {
		t.Fatal("consumed recovery code was accepted")
	}
}

func TestValidLockPIN(t *testing.T) {
	for _, value := range []string{"123456", "000000", "987654"} {
		if !validLockPIN(value) {
			t.Fatalf("valid PIN %q rejected", value)
		}
	}
	for _, value := range []string{"", "12345", "1234567", "12345a", " 123456", "１２３４５６"} {
		if validLockPIN(value) {
			t.Fatalf("invalid PIN %q accepted", value)
		}
	}
}

func TestNormalizeHostGroup(t *testing.T) {
	for input, expected := range map[string]string{
		"":                        "",
		"  production / east/db ": "production/east/db",
		"/one//two/":              "one/two",
	} {
		if actual := normalizeHostGroup(input); actual != expected {
			t.Fatalf("normalizeHostGroup(%q)=%q, want %q", input, actual, expected)
		}
	}
}

func TestWritableRemotePath(t *testing.T) {
	for _, value := range []string{"", ".", "/", "..", "../", " /../ ", "bad\x00path"} {
		if _, err := writableRemotePath(value); err == nil {
			t.Fatalf("dangerous path %q accepted", value)
		}
	}
	for input, expected := range map[string]string{"/srv/data/file.txt": "/srv/data/file.txt", "reports/../today.txt": "today.txt", " ./safe.txt ": "safe.txt"} {
		actual, err := writableRemotePath(input)
		if err != nil || actual != expected {
			t.Fatalf("path %q=%q err=%v", input, actual, err)
		}
	}
}

func TestEditableTextVersion(t *testing.T) {
	first := editableTextVersion([]byte("key=value\n"))
	if first == "" || len(first) != 64 {
		t.Fatalf("unexpected version %q", first)
	}
	if first != editableTextVersion([]byte("key=value\n")) {
		t.Fatal("same content produced different versions")
	}
	if first == editableTextVersion([]byte("key=changed\n")) {
		t.Fatal("different content produced the same version")
	}
}

func TestCSRFProtectionRejectsExplicitCrossSiteRequest(t *testing.T) {
	a := &API{}
	handler := a.csrfProtection(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	request := httptest.NewRequest(http.MethodPut, "http://velin.example/api/preferences", strings.NewReader(`{}`))
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "cross_site_rejected") {
		t.Fatalf("cross-site status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodPut, "http://velin.example/api/preferences", strings.NewReader(`{}`))
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("valid CSRF status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCSRFProtectionDoesNotConsumeProxiedApplicationRequests(t *testing.T) {
	a := &API{}
	handler := a.csrfProtection(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	request := httptest.NewRequest(http.MethodPost, "http://velin.example/web-proxy/token/api/save", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("proxy POST status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestUpdateUserProfile(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.CreateUser("admin", "admin", "hash", "admin"); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateUser("member", "member", "hash", "user"); err != nil {
		t.Fatal(err)
	}
	a := &API{store: s}

	body := []byte(`{"username":"operator","role":"admin","disabled":true}`)
	req := httptest.NewRequest("PATCH", "/api/admin/users/member", bytes.NewReader(body))
	route := chi.NewRouteContext()
	route.URLParams.Add("id", "member")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, route)
	ctx = context.WithValue(ctx, userKey, store.User{ID: "admin", Role: "admin"})
	recorder := httptest.NewRecorder()
	a.updateUser(recorder, req.WithContext(ctx))
	if recorder.Code != 200 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var updated store.User
	if err = json.NewDecoder(recorder.Body).Decode(&updated); err != nil {
		t.Fatal(err)
	}
	if updated.Username != "operator" || updated.Role != "admin" || !updated.Disabled {
		t.Fatalf("updated user=%+v", updated)
	}
}

func TestUpdateUserRejectsSelfDemotion(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.CreateUser("admin", "admin", "hash", "admin"); err != nil {
		t.Fatal(err)
	}
	a := &API{store: s}

	req := httptest.NewRequest("PATCH", "/api/admin/users/admin", strings.NewReader(`{"role":"user"}`))
	route := chi.NewRouteContext()
	route.URLParams.Add("id", "admin")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, route)
	ctx = context.WithValue(ctx, userKey, store.User{ID: "admin", Role: "admin"})
	recorder := httptest.NewRecorder()
	a.updateUser(recorder, req.WithContext(ctx))
	if recorder.Code != 400 || !strings.Contains(recorder.Body.String(), "cannot_demote_self") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
