package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Addr          string
	DataDir       string
	DatabasePath  string
	MasterKeyPath string
	WebDist       string
	CookieSecure  bool
	HostPortAddr  string
	SessionTTL    time.Duration
	DeploymentID  string
	AdminUser     string
	AdminPassword string
	AIBaseURL     string
	AIAPIKey      string
	AIModel       string
	CrushBinary   string
	CrushDataDir  string
	FFmpegBinary  string
}

func Load() (Config, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("load .env: %w", err)
	}
	dataDir := env("VELIN_DATA_DIR", "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return Config{}, fmt.Errorf("create data directory: %w", err)
	}
	deploymentID, err := persistedID(filepath.Join(dataDir, "deployment_id"))
	if err != nil {
		return Config{}, err
	}
	hostPortAddr := env("VELIN_HOST_PORT_ADDR", "127.0.0.1")
	if parsed := net.ParseIP(hostPortAddr); parsed == nil || parsed.To4() == nil {
		return Config{}, fmt.Errorf("VELIN_HOST_PORT_ADDR must be an IPv4 address")
	}
	return Config{
		Addr:          env("VELIN_ADDR", "0.0.0.0:8377"),
		DataDir:       dataDir,
		DatabasePath:  filepath.Join(dataDir, "velin.db"),
		MasterKeyPath: filepath.Join(dataDir, "master.key"),
		WebDist:       env("VELIN_WEB_DIST", "web/dist"),
		CookieSecure:  strings.EqualFold(env("VELIN_COOKIE_SECURE", "false"), "true"),
		HostPortAddr:  hostPortAddr,
		SessionTTL:    7 * 24 * time.Hour,
		DeploymentID:  deploymentID,
		AdminUser:     env("VELIN_ADMIN_USER", "admin"),
		AdminPassword: os.Getenv("VELIN_ADMIN_PASSWORD"),
		AIBaseURL:     strings.TrimRight(env("VELIN_AI_BASE_URL", ""), "/"),
		AIAPIKey:      strings.TrimSpace(os.Getenv("VELIN_AI_API_KEY")),
		AIModel:       strings.TrimSpace(os.Getenv("VELIN_AI_MODEL")),
		CrushBinary:   env("VELIN_CRUSH_BINARY", "/usr/local/bin/crush"),
		CrushDataDir:  env("VELIN_CRUSH_DATA_DIR", filepath.Join(dataDir, "crush")),
		FFmpegBinary:  env("VELIN_FFMPEG_BINARY", "ffmpeg"),
	}, nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func persistedID(path string) (string, error) {
	if value, err := os.ReadFile(path); err == nil {
		id := strings.TrimSpace(string(value))
		if id != "" {
			return id, nil
		}
	}
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate deployment id: %w", err)
	}
	id := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("persist deployment id: %w", err)
	}
	return id, nil
}
