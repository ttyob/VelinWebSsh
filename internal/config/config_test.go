package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadReadsDotEnvAndPreservesEnvironment(t *testing.T) {
	workDir := t.TempDir()
	changeWorkingDirectory(t, workDir)
	preserveAndUnsetEnv(t, "VELIN_DATA_DIR")
	preserveAndUnsetEnv(t, "VELIN_ADMIN_USER")
	preserveAndUnsetEnv(t, "VELIN_ADDR")
	t.Setenv("VELIN_ADMIN_USER", "environment-admin")

	content := "VELIN_DATA_DIR=state\nVELIN_ADMIN_USER=file-admin\n"
	if err := os.WriteFile(filepath.Join(workDir, ".env"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != "state" {
		t.Fatalf("data directory=%q", cfg.DataDir)
	}
	if cfg.AdminUser != "environment-admin" {
		t.Fatalf("admin user=%q", cfg.AdminUser)
	}
	if cfg.Addr != "0.0.0.0:8377" {
		t.Fatalf("listen address=%q", cfg.Addr)
	}
}

func TestLoadRejectsInvalidDotEnv(t *testing.T) {
	workDir := t.TempDir()
	changeWorkingDirectory(t, workDir)
	if err := os.WriteFile(filepath.Join(workDir, ".env"), []byte("invalid line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "load .env") {
		t.Fatalf("error=%v", err)
	}
}

func changeWorkingDirectory(t *testing.T, path string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
}

func preserveAndUnsetEnv(t *testing.T, key string) {
	t.Helper()
	value, exists := os.LookupEnv(key)
	t.Cleanup(func() {
		if exists {
			_ = os.Setenv(key, value)
		} else {
			_ = os.Unsetenv(key)
		}
	})
	_ = os.Unsetenv(key)
}
