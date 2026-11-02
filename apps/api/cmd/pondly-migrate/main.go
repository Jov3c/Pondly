package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/Jov3c/Pondly/apps/api/internal/store"
)

func main() {
	databaseURL := os.Getenv("PONDLY_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("PONDLY_DATABASE_URL")
	}
	if databaseURL == "" {
		log.Fatal("PONDLY_MIGRATION_DATABASE_URL or PONDLY_DATABASE_URL is required")
	}
	dir := os.Getenv("PONDLY_MIGRATIONS_DIR")
	if dir == "" {
		dir = "db/migrations"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := store.Migrate(ctx, databaseURL, dir); err != nil {
		log.Fatal(err)
	}
	log.Printf("migrations applied from %s", dir)
}
