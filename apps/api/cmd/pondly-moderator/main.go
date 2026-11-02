// Rule-based review bot for pending Pilot content.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Jov3c/Pondly/apps/api/internal/reviewbot"
	"github.com/Jov3c/Pondly/apps/api/internal/store"
)

func main() {
	databaseURL := os.Getenv("PONDLY_DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("PONDLY_DATABASE_URL is required")
	}
	interval := 10 * time.Second
	if raw := os.Getenv("PONDLY_MODERATION_INTERVAL"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			log.Fatalf("invalid PONDLY_MODERATION_INTERVAL: %q", raw)
		}
		interval = parsed
	}
	batch := 50
	if raw := os.Getenv("PONDLY_MODERATION_BATCH"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 200 {
			log.Fatalf("invalid PONDLY_MODERATION_BATCH: %q", raw)
		}
		batch = parsed
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	db, err := store.Open(ctx, databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := store.VerifySchema(ctx, db.Pool); err != nil {
		log.Fatal(err)
	}
	bot := reviewbot.New(db, log.Default())
	bot.Batch = batch
	log.Printf("Pondly review bot every %s (batch %d)", interval, batch)
	bot.Run(ctx, interval)
}
