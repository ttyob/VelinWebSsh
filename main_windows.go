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
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
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

type desktopCredentialNotice struct {
	username string
	password string
	reset    bool
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

func runDesktop() (runErr error) {
	const defaultDesktopAddr = "127.0.0.1:0"

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find desktop executable: %w", err)
	}
	executableDir := filepath.Dir(executable)
	logPath := filepath.Join(executableDir, "velin-gui.log")
	closeLog, err := configureDesktopLogging(executableDir)
	if err != nil {
		return fmt.Errorf("configure desktop logging: %w", err)
	}
	defer func() {
		if runErr != nil {
			slog.Error("desktop initialization failed", "error", runErr)
		}
		closeLog()
	}()
	slog.Info("Velin GUI starting", "executable", executable, "log_file", logPath)

	if err = os.Chdir(executableDir); err != nil {
		return fmt.Errorf("set desktop working directory: %w", err)
	}
	if err = godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load desktop .env: %w", err)
	}
	if os.Getenv("VELIN_DATA_DIR") == "" {
		_ = os.Setenv("VELIN_DATA_DIR", executableDir)
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
	slog.Info("desktop configuration loaded", "data_dir", cfg.DataDir, "database", cfg.DatabasePath, "log_file", logPath)
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
	var credentialNotice *desktopCredentialNotice
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
		credentialNotice = &desktopCredentialNotice{username: cfg.AdminUser, password: password}
		slog.Warn("GUI ADMIN CREDENTIALS", "username", cfg.AdminUser, "password", password, "notice", "save this password and change it after login")
	} else if desktopArgumentPresent("--reset-admin-password") {
		user, _, lookupErr := s.UserByUsername(cfg.AdminUser)
		if lookupErr != nil {
			_ = s.Close()
			return fmt.Errorf("find desktop administrator %q: %w", cfg.AdminUser, lookupErr)
		}
		if user.Role != "admin" {
			_ = s.Close()
			return fmt.Errorf("desktop user %q is not an administrator", cfg.AdminUser)
		}
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
		if err = s.ResetUserPassword(user.ID, hash, true); err != nil {
			_ = s.Close()
			return fmt.Errorf("reset desktop administrator password: %w", err)
		}
		credentialNotice = &desktopCredentialNotice{username: user.Username, password: password, reset: true}
		slog.Warn("GUI ADMIN PASSWORD RESET", "username", user.Username, "password", password, "notice", "save this password and change it after login")
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
		OnStartup: func(ctx context.Context) {
			showDesktopCredentialNotice(ctx, credentialNotice)
		},
		OnShutdown: func(ctx context.Context) {
			appServer.close(ctx)
		},
	})
}

func desktopArgumentPresent(name string) bool {
	for _, argument := range os.Args[1:] {
		if argument == name {
			return true
		}
	}
	return false
}

func showDesktopCredentialNotice(ctx context.Context, notice *desktopCredentialNotice) {
	if notice == nil {
		return
	}
	title := "Initial administrator credentials"
	messagePrefix := "The initial administrator account was created."
	if notice.reset {
		title = "Administrator password reset"
		messagePrefix = "The administrator password was reset."
	}
	_, err := wailsruntime.MessageDialog(ctx, wailsruntime.MessageDialogOptions{
		Type:          wailsruntime.WarningDialog,
		Title:         title,
		Message:       fmt.Sprintf("%s\n\nUsername: %s\nPassword: %s\n\nSave this password, then change it after login. It is also recorded in velin-gui.log.", messagePrefix, notice.username, notice.password),
		Buttons:       []string{"OK"},
		DefaultButton: "OK",
	})
	if err != nil {
		slog.Error("show desktop administrator credentials", "error", err)
	}
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

func configureDesktopLogging(logDir string) (func(), error) {
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
