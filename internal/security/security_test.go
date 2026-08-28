package security

import (
	"bytes"
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

func TestEncryptedBackupRoundTrip(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "source.db")
	backup := filepath.Join(dir, "velin.db.enc")
	output := filepath.Join(dir, "restored.db")
	plain := []byte("sqlite backup contents")
	if err := os.WriteFile(input, plain, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EncryptBackupFile(input, backup, "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	if err := DecryptBackupFile(backup, output, "wrong backup key"); err == nil {
		t.Fatal("wrong backup key was accepted")
	}
	if err := DecryptBackupFile(backup, output, "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored, plain) {
		t.Fatalf("restored data=%q, want %q", restored, plain)
	}
	data, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 1
	if err := os.WriteFile(backup, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := DecryptBackupFile(backup, output, "correct horse battery staple"); err == nil {
		t.Fatal("tampered backup was accepted")
	}
}

func TestEncryptedBackupBundleIncludesMasterKey(t *testing.T) {
	dir := t.TempDir()
	databasePath := filepath.Join(dir, "source.db")
	masterKeyPath := filepath.Join(dir, "master.key")
	backupPath := filepath.Join(dir, "velin.db.enc")
	restoredDatabasePath := filepath.Join(dir, "restored.db")
	restoredMasterKeyPath := filepath.Join(dir, "restored-master.key")
	database := []byte("sqlite backup contents")
	masterKey := bytes.Repeat([]byte{0x42}, 32)
	if err := os.WriteFile(databasePath, database, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(masterKeyPath, masterKey, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EncryptBackupBundle(databasePath, masterKeyPath, backupPath, "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	if err := DecryptBackupBundle(backupPath, restoredDatabasePath, restoredMasterKeyPath, "wrong backup key"); err == nil {
		t.Fatal("wrong backup key was accepted")
	}
	if err := DecryptBackupBundle(backupPath, restoredDatabasePath, restoredMasterKeyPath, "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	restoredDatabase, err := os.ReadFile(restoredDatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	restoredMasterKey, err := os.ReadFile(restoredMasterKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restoredDatabase, database) || !bytes.Equal(restoredMasterKey, masterKey) {
		t.Fatalf("bundle contents did not round trip")
	}
}

func TestValidateBackupKey(t *testing.T) {
	if err := ValidateBackupKey("short"); err == nil {
		t.Fatal("short backup key was accepted")
	}
	if err := ValidateBackupKey("correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
}
