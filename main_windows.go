//go:build windows

package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"velin-webssh/internal/agent"
	"velin-webssh/internal/api"
	"velin-webssh/internal/config"
	"velin-webssh/internal/forward"
	"velin-webssh/internal/security"
	"velin-webssh/internal/store"
	"velin-webssh/internal/terminal"
)

//go:embed desktop/dist/index.html
var desktopIndexHTML string

type desktopServer struct {
	server *http.Server
	store  *store.Store
	agent  *agent.Manager
	once   sync.Once
}

func (d *desktopServer) close(ctx context.Context) {
	d.once.Do(func() {
		if d.server != nil {
			_ = d.server.Shutdown(ctx)
		}
		if d.agent != nil {
			d.agent.Close()
		}
		if d.store != nil {
			_ = d.store.Close()
		}
	})
}

func main() {
	if err := runDesktop(); err != nil {
		log.Fatal(err)
	}
}

func runDesktop() error {
	const defaultDesktopAddr = "127.0.0.1:0"

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find desktop executable: %w", err)
	}
	executableDir := filepath.Dir(executable)
	if err = os.Chdir(executableDir); err != nil {
		return fmt.Errorf("set desktop working directory: %w", err)
	}
	if err = godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load desktop .env: %w", err)
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("find Windows application data directory: %w", err)
	}
	closeLog, logErr := configureDesktopLogging(configDir)
	if logErr != nil {
		log.Printf("configure desktop logging: %v", logErr)
	} else {
		defer closeLog()
	}
	if os.Getenv("VELIN_DATA_DIR") == "" {
		_ = os.Setenv("VELIN_DATA_DIR", filepath.Join(configDir, "Velin", "data"))
	}
	if os.Getenv("VELIN_WEB_DIST") == "" {
		_ = os.Setenv("VELIN_WEB_DIST", filepath.Join(executableDir, "web", "dist"))
	}
	if os.Getenv("VELIN_ADDR") == "" {
		_ = os.Setenv("VELIN_ADDR", defaultDesktopAddr)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	s, err := store.Open(cfg.DatabasePath)
	if err != nil {
		return err
	}
	vault, err := security.LoadVault(cfg.MasterKeyPath)
	if err != nil {
		_ = s.Close()
		return err
	}
	count, err := s.UserCount()
	if err != nil {
		_ = s.Close()
		return err
	}
	if count == 0 {
		password := cfg.AdminPassword
		if password == "" {
			password, err = security.RandomToken(12)
			if err != nil {
				_ = s.Close()
				return err
			}
		}
		hash, hashErr := security.HashPassword(password)
		if hashErr != nil {
			_ = s.Close()
			return hashErr
		}
		if err = s.CreateUser(uuid.NewString(), cfg.AdminUser, hash, "admin"); err != nil {
			_ = s.Close()
			return err
		}
		slog.Warn("initial administrator created", "username", cfg.AdminUser, "password", password, "notice", "change this password after login")
	}
	manager := terminal.NewManagerWithFFmpeg(s, vault, cfg.DeploymentID, filepath.Join(cfg.DataDir, "recordings"), cfg.FFmpegBinary)
	forwardManager := forward.NewManager(s, manager)
	agentManager := agent.NewManager(
		manager,
		agent.AIConfig{BaseURL: cfg.AIBaseURL, APIKey: cfg.AIAPIKey, Model: cfg.AIModel},
		agent.CrushConfig{Binary: cfg.CrushBinary, DataDir: cfg.CrushDataDir},
	)
	appServer := &desktopServer{store: s, agent: agentManager}
	appServer.server = &http.Server{Addr: cfg.Addr, Handler: api.New(cfg, s, vault, manager, forwardManager, agentManager).Router(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	listener, err := net.Listen("tcp4", cfg.Addr)
	if err != nil && errors.Is(err, syscall.EADDRINUSE) {
		slog.Warn("configured desktop port is in use, selecting a free loopback port", "addr", cfg.Addr)
		listener, err = net.Listen("tcp4", "127.0.0.1:0")
	}
	if err != nil {
		appServer.close(context.Background())
		return err
	}
	go func() {
		if serveErr := appServer.server.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			slog.Error("desktop web server stopped", "error", serveErr)
		}
	}()
	defer appServer.close(context.Background())
	// Use the address assigned by the listener. This is important when the
	// configured port is 0, and also avoids pointing the WebView at a stale
	// configured port after address normalization.
	serviceURL, err := desktopServiceURL(listener.Addr().String())
	if err != nil {
		return err
	}
	slog.Info("desktop web service listening", "configured_addr", cfg.Addr, "service_url", serviceURL)
	if err = waitForDesktopServer(serviceURL); err != nil {
		return err
	}
	slog.Info("desktop web service ready", "service_url", serviceURL)
	page := []byte(strings.Replace(desktopIndexHTML, "__VELIN_DESKTOP_URL__", serviceURL, 1))

	return wails.Run(&options.App{
		Title:            "Velin Web SSH",
		Width:            1440,
		Height:           900,
		MinWidth:         980,
		MinHeight:        640,
		BackgroundColour: options.NewRGB(16, 20, 24),
		AssetServer: &assetserver.Options{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || (r.URL.Path != "/" && r.URL.Path != "/index.html") {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(page)
		})},
		Windows: &windows.Options{Theme: windows.Dark},
		OnShutdown: func(ctx context.Context) {
			appServer.close(ctx)
		},
	})
}

func desktopServiceURL(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("invalid desktop service address %q: %w", addr, err)
	}
	if host == "" || host == "0.0.0.0" || strings.EqualFold(host, "localhost") {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/", nil
}

func waitForDesktopServer(serviceURL string) error {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for attempt := 0; attempt < 40; attempt++ {
		response, err := client.Get(serviceURL + "api/health/live")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("desktop web service did not become ready at %s", serviceURL)
}

func configureDesktopLogging(configDir string) (func(), error) {
	logDir := filepath.Join(configDir, "Velin")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return func() {}, err
	}
	file, err := os.OpenFile(filepath.Join(logDir, "velin-gui.log"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return func() {}, err
	}
	output := io.MultiWriter(os.Stderr, file)
	log.SetOutput(output)
	slog.SetDefault(slog.New(slog.NewTextHandler(output, nil)))
	return func() { _ = file.Close() }, nil
}
