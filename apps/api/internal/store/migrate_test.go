package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jov3c/Pondly/apps/api/internal/testutil"
)

func TestMigrateEmptyDatabaseAndRerun(t *testing.T) {
	ctx := context.Background()
	pool, url := testutil.StartPostgres(t)
	dir := testutil.MigrationsDir(t)

	if err := Migrate(ctx, url, dir); err != nil {
		t.Fatal(err)
	}
	if err := VerifySchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, url, dir); err != nil {
		t.Fatalf("rerun must be a no-op: %v", err)
	}

	var tables int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name IN
		('schema_migrations','owner_sessions','owner_otp_challenges','enrollment_tickets',
		 'claim_challenges','agent_recovery_tickets','agent_profiles','comments',
		 'agent_inboxes','activity_events','notifications','admin_accounts',
		 'admin_sessions','moderation_actions','audit_logs','pilot_publish_allowlist')
	`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 16 {
		t.Fatalf("pilot tables = %d, want 16", tables)
	}
}

func TestMigrateRejectsChecksumDrift(t *testing.T) {
	ctx := context.Background()
	_, url := testutil.StartPostgres(t)
	dir := t.TempDir()
	copyMigrations(t, testutil.MigrationsDir(t), dir)

	if err := Migrate(ctx, url, dir); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "0002_pilot_foundation.sql")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, append(data, []byte("\n-- drift\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, url, dir); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}

func TestMigrateRejectsExistingSchemaWithoutLedger(t *testing.T) {
	ctx := context.Background()
	pool, url := testutil.StartPostgres(t)
	if _, err := pool.Exec(ctx, `CREATE TABLE owners (id uuid PRIMARY KEY DEFAULT gen_random_uuid())`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, url, testutil.MigrationsDir(t)); err == nil || !strings.Contains(err.Error(), "without migration ledger") {
		t.Fatalf("expected ledger guard, got %v", err)
	}
}

func copyMigrations(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, entry.Name()), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
