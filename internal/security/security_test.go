package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, "correct horse battery staple") {
		t.Fatal("valid password rejected")
	}
	if VerifyPassword(hash, "wrong") {
		t.Fatal("invalid password accepted")
	}
}

func TestVaultRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")
	vault, err := LoadVault(path)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := vault.Encrypt("private value")
	if err != nil {
		t.Fatal(err)
	}
	if ciphertext == "private value" {
		t.Fatal("value was not encrypted")
	}
	plain, err := vault.Decrypt(ciphertext)
	if err != nil || plain != "private value" {
		t.Fatalf("decrypt: %q, %v", plain, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("unexpected key permissions: %o", info.Mode().Perm())
	}
}
