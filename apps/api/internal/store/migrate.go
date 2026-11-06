package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/jackc/pgx/v5"
)

const RequiredSchemaVersion = "0018"

var migrationFilePattern = regexp.MustCompile(`^(\d{4})_[a-z0-9_]+\.sql$`)

var (
	leadingBegin   = regexp.MustCompile(`(?is)^\s*BEGIN\s*;`)
	trailingCommit = regexp.MustCompile(`(?is)COMMIT\s*;\s*$`)
)

// Migrate applies NNNN_*.sql files in order and records their checksums. It
// refuses to guess when tables exist without a migration ledger.
func Migrate(ctx context.Context, databaseURL, migrationsDir string) error {
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect for migration: %w", err)
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version text PRIMARY KEY,
		checksum text NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied := map[string]string{}
	rows, err := conn.Query(ctx, `SELECT version, checksum FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	for rows.Next() {
		var version, checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			rows.Close()
			return err
		}
		applied[version] = checksum
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	if len(applied) == 0 {
		var existing *string
		if err := conn.QueryRow(ctx, `SELECT to_regclass('public.owners')::text`).Scan(&existing); err != nil {
			return fmt.Errorf("inspect existing schema: %w", err)
		}
		if existing != nil {
			return fmt.Errorf("existing schema without migration ledger; stop and inspect the database manually")
		}
	}

	files, err := migrationFiles(migrationsDir)
	if err != nil {
		return err
	}
	for _, file := range files {
		data, err := os.ReadFile(file.path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		checksum := hex.EncodeToString(sum[:])
		if old, ok := applied[file.version]; ok {
			if old != checksum {
				return fmt.Errorf("migration %s checksum mismatch: applied=%s current=%s", file.version, old, checksum)
			}
			continue
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		body := stripOuterTransaction(string(data))
		if _, err := tx.Exec(ctx, body); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", file.name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version, checksum) VALUES ($1, $2)`, file.version, checksum); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", file.name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", file.name, err)
		}
	}
	return nil
}

func VerifySchema(ctx context.Context, q Querier) error {
	var version string
	if err := q.QueryRow(ctx, `SELECT version FROM schema_migrations WHERE version = $1`, RequiredSchemaVersion).Scan(&version); err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("schema version %s is not applied", RequiredSchemaVersion)
		}
		return fmt.Errorf("verify schema: %w", err)
	}
	return nil
}

type migrationFile struct {
	version string
	name    string
	path    string
}

func migrationFiles(dir string) ([]migrationFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}
	var files []migrationFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := migrationFilePattern.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		files = append(files, migrationFile{
			version: match[1],
			name:    entry.Name(),
			path:    filepath.Join(dir, entry.Name()),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	if len(files) == 0 {
		return nil, fmt.Errorf("no migration files in %s", dir)
	}
	return files, nil
}

func stripOuterTransaction(body string) string {
	body = leadingBegin.ReplaceAllString(body, "")
	body = trailingCommit.ReplaceAllString(body, "")
	return body
}
