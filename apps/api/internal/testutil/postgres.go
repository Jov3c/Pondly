package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func randomSuffix() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// StartPostgres returns a pool connected to a fresh database. When
// PONDLY_TEST_DATABASE_URL is set, its server is reused and a unique database
// is created there; otherwise an embedded PostgreSQL 16 instance is started.
func StartPostgres(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	name := "pondly_test_" + randomSuffix()
	adminURL := os.Getenv("PONDLY_TEST_DATABASE_URL")
	var stop func()
	if adminURL == "" {
		var createErr error
		for attempt := 0; attempt < 3; attempt++ {
			adminURL, stop = startEmbedded(t)
			createErr = createDatabase(adminURL, name)
			if createErr == nil {
				break
			}
			stop()
			adminURL = ""
			stop = nil
		}
		if adminURL == "" {
			t.Fatalf("create embedded test database: %v", createErr)
		}
		t.Cleanup(stop)
	} else if err := createDatabase(adminURL, name); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	url := databaseURL(t, adminURL, name)
	t.Cleanup(func() {
		dropDatabase(adminURL, name)
	})
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var pingErr error
	for attempt := 0; attempt < 20; attempt++ {
		if pingErr = pool.Ping(context.Background()); pingErr == nil {
			return pool, url
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("ping test database: %v", pingErr)
	return nil, ""
}

func startEmbedded(t *testing.T) (string, func()) {
	t.Helper()
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		port := freePort(t)
		dir, err := os.MkdirTemp("", "pondly-pg-*")
		if err != nil {
			t.Fatal(err)
		}
		cfg := embeddedpostgres.DefaultConfig().
			Port(uint32(port)).
			Database("postgres").
			Username("postgres").
			Password("postgres").
			RuntimePath(filepath.Join(dir, "runtime")).
			DataPath(filepath.Join(dir, "data")).
			StartTimeout(120 * time.Second).
			Logger(io.Discard)
		db := embeddedpostgres.NewDatabase(cfg)
		if err := db.Start(); err != nil {
			lastErr = err
			_ = db.Stop()
			_ = os.RemoveAll(dir)
			continue
		}
		stop := func() {
			_ = db.Stop()
			_ = os.RemoveAll(dir)
		}
		return fmt.Sprintf("postgres://postgres:postgres@127.0.0.1:%d/postgres?sslmode=disable", port), stop
	}
	t.Fatalf("start embedded postgres: %v", lastErr)
	return "", nil
}

func createDatabase(adminURL, name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var lastErr error
	for attempt := 0; attempt < 40; attempt++ {
		conn, err := pgx.Connect(ctx, maintenanceURL(adminURL))
		if err == nil {
			_, execErr := conn.Exec(ctx, "CREATE DATABASE "+quoteIdent(name))
			_ = conn.Close(ctx)
			return execErr
		}
		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("connect maintenance db: %w", lastErr)
}

func dropDatabase(adminURL, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, maintenanceURL(adminURL))
	if err != nil {
		return
	}
	defer conn.Close(ctx)
	_, _ = conn.Exec(ctx, "DROP DATABASE IF EXISTS "+quoteIdent(name)+" WITH (FORCE)")
}

func quoteIdent(s string) string {
	return `"` + s + `"`
}

func maintenanceURL(raw string) string {
	return urlWithDatabase(raw, "postgres")
}

func databaseURL(t *testing.T, adminURL, name string) string {
	t.Helper()
	return urlWithDatabase(adminURL, name)
}

func urlWithDatabase(raw, database string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Path = "/" + database
	return u.String()
}

func MigrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "db", "migrations")
}
