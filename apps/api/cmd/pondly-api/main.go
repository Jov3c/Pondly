package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Jov3c/Pondly/apps/api/internal/httpapi"
	"github.com/Jov3c/Pondly/apps/api/internal/mail"
	"github.com/Jov3c/Pondly/apps/api/internal/platform"
	"github.com/Jov3c/Pondly/apps/api/internal/ratelimit"
	"github.com/Jov3c/Pondly/apps/api/internal/store"
)

func main() {
	cfg, err := platform.FromEnvironment()
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	deps := httpapi.Dependencies{}
	var closeDeps func()
	if cfg.DatabaseURL != "" {
		db, err := store.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("database: %v", err)
		}
		db.ReplayKey = cfg.SecretReplayKey
		db.ReplayTTL = cfg.SecretReplayTTL
		if err := store.VerifySchema(ctx, db.Pool); err != nil {
			db.Close()
			log.Fatalf("schema: %v", err)
		}
		limiter, err := ratelimit.NewFromURL(ctx, cfg.RedisURL)
		if err != nil {
			db.Close()
			log.Fatalf("redis: %v", err)
		}
		mailer := &mail.SMTPMailer{
			Host: cfg.SMTP.Host, Port: cfg.SMTP.Port,
			Username: cfg.SMTP.Username, Password: cfg.SMTP.Password, From: cfg.SMTP.From,
		}
		deps.Store = db
		deps.Limiter = limiter
		deps.Mailer = mailer
		deps.Readiness = func(ctx context.Context) bool {
			checkCtx, checkCancel := context.WithTimeout(ctx, 2*time.Second)
			defer checkCancel()
			if err := db.Pool.Ping(checkCtx); err != nil {
				return false
			}
			if err := limiter.Ping(checkCtx); err != nil {
				return false
			}
			return store.VerifySchema(checkCtx, db.Pool) == nil
		}
		closeDeps = func() {
			_ = limiter.Close()
			db.Close()
		}
	}

	h := httpapi.New(cfg, deps)
	server := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    8 * 1024,
	}
	var adminServer *http.Server
	if cfg.DatabaseURL != "" {
		adminServer = &http.Server{
			Addr:              cfg.AdminListenAddress,
			Handler:           httpapi.NewAdmin(cfg, deps),
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      15 * time.Second,
			IdleTimeout:       30 * time.Second,
			MaxHeaderBytes:    8 * 1024,
		}
		go func() {
			log.Printf("pondly private admin listening on %s", cfg.AdminListenAddress)
			if err := adminServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("admin server failed: %v", err)
			}
		}()
	}

	shutdown, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-shutdown.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown failed: %v", err)
		}
		if adminServer != nil {
			if err := adminServer.Shutdown(shutdownCtx); err != nil {
				log.Printf("admin shutdown failed: %v", err)
			}
		}
	}()

	mode := "skeleton"
	if cfg.DatabaseURL != "" {
		mode = "pilot"
	}
	log.Printf("pondly API (%s) listening on %s", mode, cfg.ListenAddress)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("server failed: %v", err)
		if closeDeps != nil {
			closeDeps()
		}
		os.Exit(1)
	}
	if closeDeps != nil {
		closeDeps()
	}
}
