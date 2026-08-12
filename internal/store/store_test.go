package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestHostOwnership(t *testing.T) {
	s := testStore(t)
	for _, id := range []string{"u1", "u2"} {
		if err := s.CreateUser(id, id, "hash", "user"); err != nil {
			t.Fatal(err)
		}
	}
	h := Host{ID: "h1", UserID: "u1", Name: "server", Address: "127.0.0.1", Port: 22, Username: "root"}
	if err := s.SaveHost(h); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Host("u2", "h1"); err == nil {
		t.Fatal("other user accessed host")
	}
}

func TestWorkspaceVersionConflict(t *testing.T) {
	s := testStore(t)
	if err := s.CreateUser("u1", "user", "hash", "user"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveWorkspace("u1", json.RawMessage(`{"tabs":[]}`), 0); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveWorkspace("u1", json.RawMessage(`{"tabs":["one"]}`), 0); err == nil {
		t.Fatal("stale workspace write succeeded")
	}
	if err := s.SaveWorkspace("u1", json.RawMessage(`{"tabs":["one"]}`), 1); err != nil {
		t.Fatal(err)
	}
}

func TestBackup(t *testing.T) {
	s := testStore(t)
	if err := s.CreateUser("u1", "user", "hash", "user"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "backup.db")
	if err := s.Backup(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	backup, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	if n, err := backup.UserCount(); err != nil || n != 1 {
		t.Fatalf("backup user count=%d err=%v", n, err)
	}
}
