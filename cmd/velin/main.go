package main

import (
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/google/uuid"
	"velin-webssh/internal/api"
	"velin-webssh/internal/config"
	"velin-webssh/internal/security"
	"velin-webssh/internal/store"
	"velin-webssh/internal/terminal"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	s, err := store.Open(cfg.DatabasePath)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	vault, err := security.LoadVault(cfg.MasterKeyPath)
	if err != nil {
		log.Fatal(err)
	}
	count, err := s.UserCount()
	if err != nil {
		log.Fatal(err)
	}
	if count == 0 {
		password := cfg.AdminPassword
		if password == "" {
			password, err = security.RandomToken(12)
			if err != nil {
				log.Fatal(err)
			}
		}
		hash, err := security.HashPassword(password)
		if err != nil {
			log.Fatal(err)
		}
		if err = s.CreateUser(uuid.NewString(), cfg.AdminUser, hash, "admin"); err != nil {
			log.Fatal(err)
		}
		slog.Warn("initial administrator created", "username", cfg.AdminUser, "password", password, "notice", "change this password after login")
	}
	manager := terminal.NewManager(s, vault, cfg.DeploymentID)
	handler := api.New(cfg, s, vault, manager).Router()
	server := &http.Server{Addr: cfg.Addr, Handler: handler, ReadHeaderTimeout: 10_000_000_000, IdleTimeout: 60_000_000_000}
	slog.Info("Velin Web SSH listening", "addr", cfg.Addr, "pid", os.Getpid())
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
