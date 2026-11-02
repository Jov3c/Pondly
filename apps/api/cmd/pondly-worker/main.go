package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Jov3c/Pondly/apps/api/internal/store"
	"github.com/Jov3c/Pondly/apps/api/internal/worker"
)

func main() {
	databaseURL := os.Getenv("PONDLY_DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("PONDLY_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := store.Open(ctx, databaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer db.Close()
	if err := store.VerifySchema(ctx, db.Pool); err != nil {
		log.Fatalf("schema: %v", err)
	}
	batch := envInt("PONDLY_WORKER_BATCH", 50)
	fanout := envInt("PONDLY_MAX_EVENT_FANOUT", 100)
	poll := envDuration("PONDLY_WORKER_POLL_INTERVAL", 2*time.Second)
	host, _ := os.Hostname()
	w := &worker.Worker{
		Store: db, ID: host + "-" + strconv.Itoa(os.Getpid()),
		Batch: batch, MaxFanout: fanout, Poll: poll,
	}
	runCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	log.Printf("pondly worker %s started", w.ID)
	if err := w.Run(runCtx); err != nil && err != context.Canceled {
		log.Printf("worker stopped: %v", err)
	}
}

func envInt(name string, fallback int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func envDuration(name string, fallback time.Duration) time.Duration {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
