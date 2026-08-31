//go:build windows

package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
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

//go:embed desktop/dist
var desktopAssets embed.FS

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
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find desktop executable: %w", err)
	}
	executableDir := filepath.Dir(executable)
	if err = os.Chdir(executableDir); err != nil {
		return fmt.Errorf("set desktop working directory: %w", err)
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("find Windows application data directory: %w", err)
	}
	if os.Getenv("VELIN_DATA_DIR") == "" {
		_ = os.Setenv("VELIN_DATA_DIR", filepath.Join(configDir, "Velin", "data"))
	}
	if os.Getenv("VELIN_WEB_DIST") == "" {
		_ = os.Setenv("VELIN_WEB_DIST", filepath.Join(executableDir, "web", "dist"))
	}
	if os.Getenv("VELIN_ADDR") == "" {
		_ = os.Setenv("VELIN_ADDR", "127.0.0.1:8378")
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

	return wails.Run(&options.App{
		Title:            "Velin Web SSH",
		Width:            1440,
		Height:           900,
		MinWidth:         980,
		MinHeight:        640,
		BackgroundColour: options.NewRGB(16, 20, 24),
		AssetServer:      &assetserver.Options{Assets: desktopAssets},
		Windows:          &windows.Options{Theme: windows.Dark},
		OnShutdown: func(ctx context.Context) {
			appServer.close(ctx)
		},
	})
}
