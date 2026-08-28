package security

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

type Vault struct {
	mu   sync.RWMutex
	aead cipher.AEAD
}

const (
	backupMagic         = "VELIN-ENCRYPTED-BACKUP\x00"
	backupVersion       = byte(1)
	backupBundleVersion = byte(2)
	backupSaltSize      = 16
	maxBackupDataSize   = int64(1 << 30)
)

func LoadVault(path string) (*Vault, error) {
	key, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		if err = os.WriteFile(path, key, 0o600); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("master key must be exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	return &Vault{aead: aead}, err
}

func (v *Vault) Encrypt(plain string) (string, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := v.aead.Seal(nonce, nonce, []byte(plain), nil)
	return base64.RawStdEncoding.EncodeToString(out), nil
}

func (v *Vault) Decrypt(encoded string) (string, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	data, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil || len(data) < v.aead.NonceSize() {
		return "", errors.New("invalid encrypted value")
	}
	nonce, ciphertext := data[:v.aead.NonceSize()], data[v.aead.NonceSize():]
	plain, err := v.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", errors.New("cannot decrypt value")
	}
	return string(plain), nil
}

func (v *Vault) Reload(path string) error {
	key, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(key) != 32 {
		return fmt.Errorf("master key must be exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	v.mu.Lock()
	v.aead = aead
	v.mu.Unlock()
	return nil
}

func ValidateBackupKey(key string) error {
	length := len([]rune(key))
	if length < 12 {
		return errors.New("backup key must be at least 12 characters")
	}
	if length > 256 {
		return errors.New("backup key must not exceed 256 characters")
	}
	return nil
}

func backupKey(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
}

func EncryptBackupFile(inputPath, outputPath, password string) error {
	if err := ValidateBackupKey(password); err != nil {
		return err
	}
	plain, err := readBackupData(inputPath)
	if err != nil {
		return err
	}
	salt := make([]byte, backupSaltSize)
	if _, err = rand.Read(salt); err != nil {
		return err
	}
	block, err := aes.NewCipher(backupKey(password, salt))
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	ciphertext := aead.Seal(nil, nonce, plain, []byte(backupMagic))

	file, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(outputPath)
		}
	}()
	if _, err = file.WriteString(backupMagic); err != nil {
		return err
	}
	if _, err = file.Write([]byte{backupVersion}); err != nil {
		return err
	}
	if _, err = file.Write(salt); err != nil {
		return err
	}
	if _, err = file.Write(nonce); err != nil {
		return err
	}
	if _, err = file.Write(ciphertext); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	ok = true
	return nil
}

func DecryptBackupFile(inputPath, outputPath, password string) error {
	if err := ValidateBackupKey(password); err != nil {
		return err
	}
	data, err := readBackupData(inputPath)
	if err != nil {
		return err
	}
	headerSize := len(backupMagic) + 1 + backupSaltSize + 12
	if len(data) <= headerSize || !bytes.Equal(data[:len(backupMagic)], []byte(backupMagic)) {
		return errors.New("invalid encrypted backup")
	}
	if data[len(backupMagic)] != backupVersion {
		return errors.New("unsupported encrypted backup version")
	}
	saltStart := len(backupMagic) + 1
	salt := data[saltStart : saltStart+backupSaltSize]
	nonceStart := saltStart + backupSaltSize
	nonce := data[nonceStart : nonceStart+12]
	block, err := aes.NewCipher(backupKey(password, salt))
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	plain, err := aead.Open(nil, nonce, data[nonceStart+12:], []byte(backupMagic))
	if err != nil {
		return errors.New("backup key is incorrect or backup is damaged")
	}
	return writeBackupData(outputPath, plain)
}

const backupPayloadMagic = "VELIN-DATABASE-BUNDLE\x00"

func EncryptBackupBundle(databasePath, masterKeyPath, outputPath, password string) error {
	if err := ValidateBackupKey(password); err != nil {
		return err
	}
	database, err := readBackupData(databasePath)
	if err != nil {
		return err
	}
	masterKey, err := os.ReadFile(masterKeyPath)
	if err != nil {
		return err
	}
	if len(masterKey) != 32 {
		return errors.New("master key must be exactly 32 bytes")
	}
	payload := make([]byte, len(backupPayloadMagic)+8+len(database)+4+len(masterKey))
	offset := copy(payload, backupPayloadMagic)
	binary.BigEndian.PutUint64(payload[offset:], uint64(len(database)))
	offset += 8
	offset += copy(payload[offset:], database)
	binary.BigEndian.PutUint32(payload[offset:], uint32(len(masterKey)))
	offset += 4
	copy(payload[offset:], masterKey)
	return encryptBackupPayload(payload, outputPath, password, backupBundleVersion)
}

func DecryptBackupBundle(inputPath, databasePath, masterKeyPath, password string) error {
	if err := ValidateBackupKey(password); err != nil {
		return err
	}
	payload, version, err := decryptBackupPayload(inputPath, password)
	if err != nil {
		return err
	}
	if version != backupBundleVersion || len(payload) < len(backupPayloadMagic)+8+4 || !bytes.Equal(payload[:len(backupPayloadMagic)], []byte(backupPayloadMagic)) {
		return errors.New("unsupported encrypted backup format")
	}
	offset := len(backupPayloadMagic)
	databaseSize := binary.BigEndian.Uint64(payload[offset:])
	offset += 8
	if databaseSize > uint64(maxBackupDataSize) || databaseSize > uint64(len(payload)-offset) {
		return errors.New("invalid encrypted backup contents")
	}
	databaseEnd := offset + int(databaseSize)
	if databaseEnd+4 > len(payload) {
		return errors.New("invalid encrypted backup contents")
	}
	masterKeySize := binary.BigEndian.Uint32(payload[databaseEnd:])
	masterKeyStart := databaseEnd + 4
	if masterKeySize != 32 || int(masterKeySize) != len(payload)-masterKeyStart {
		return errors.New("invalid encrypted backup master key")
	}
	if err = writeBackupData(databasePath, payload[offset:databaseEnd]); err != nil {
		return err
	}
	if err = os.WriteFile(masterKeyPath, payload[masterKeyStart:], 0o600); err != nil {
		return err
	}
	return nil
}

func encryptBackupPayload(payload []byte, outputPath, password string, version byte) error {
	if int64(len(payload)) > maxBackupDataSize {
		return errors.New("backup file exceeds 1 GB")
	}
	salt := make([]byte, backupSaltSize)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	block, err := aes.NewCipher(backupKey(password, salt))
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	ciphertext := aead.Seal(nil, nonce, payload, []byte(backupMagic))
	file, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(outputPath)
		}
	}()
	if _, err = file.WriteString(backupMagic); err != nil {
		return err
	}
	if _, err = file.Write([]byte{version}); err != nil {
		return err
	}
	if _, err = file.Write(salt); err != nil {
		return err
	}
	if _, err = file.Write(nonce); err != nil {
		return err
	}
	if _, err = file.Write(ciphertext); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	ok = true
	return nil
}

func decryptBackupPayload(inputPath, password string) ([]byte, byte, error) {
	data, err := readBackupData(inputPath)
	if err != nil {
		return nil, 0, err
	}
	headerSize := len(backupMagic) + 1 + backupSaltSize + 12
	if len(data) <= headerSize || !bytes.Equal(data[:len(backupMagic)], []byte(backupMagic)) {
		return nil, 0, errors.New("invalid encrypted backup")
	}
	version := data[len(backupMagic)]
	saltStart := len(backupMagic) + 1
	salt := data[saltStart : saltStart+backupSaltSize]
	nonceStart := saltStart + backupSaltSize
	nonce := data[nonceStart : nonceStart+12]
	block, err := aes.NewCipher(backupKey(password, salt))
	if err != nil {
		return nil, 0, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, 0, err
	}
	plain, err := aead.Open(nil, nonce, data[nonceStart+12:], []byte(backupMagic))
	if err != nil {
		return nil, 0, errors.New("backup key is incorrect or backup is damaged")
	}
	return plain, version, nil
}

func readBackupData(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBackupDataSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBackupDataSize {
		return nil, errors.New("backup file exceeds 1 GB")
	}
	return data, nil
}

func writeBackupData(path string, data []byte) error {
	if int64(len(data)) > maxBackupDataSize {
		return errors.New("backup file exceeds 1 GB")
	}
	return os.WriteFile(path, data, 0o600)
}

func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, 2, 64*1024, 2, 32)
	return fmt.Sprintf("argon2id$v=19$m=65536,t=2,p=2$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}

func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" || parts[1] != "v=19" || parts[2] != "m=65536,t=2,p=2" {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[3])
	expected, err2 := base64.RawStdEncoding.DecodeString(parts[4])
	if err1 != nil || err2 != nil {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, 2, 64*1024, 2, uint32(len(expected)))
	return subtleEqual(actual, expected)
}

func RandomToken(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func TokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func subtleEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
