// Package migrator applies SQL migrations from a directory and tracks
// applied files in a schema_migrations table. It is idempotent and safe
// to run at service startup.
//
// Migration files must be named NNN_description.sql where NNN is a
// zero-padded numeric prefix that defines apply order. Each file is
// executed in a single transaction; on success the filename is recorded
// in schema_migrations with the checksum of its content.
package migrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const createTableSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	filename   TEXT PRIMARY KEY,
	checksum   TEXT NOT NULL,
	applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);`

// Run discovers all *.sql files in dir, sorts them by filename, and
// applies any that are not yet recorded in schema_migrations. Each
// migration runs inside its own transaction. If a migration fails the
// transaction is rolled back and the error is returned — subsequent
// migrations are not attempted.
//
// On first run against a legacy database (where the users table already
// exists but schema_migrations does not), the migrator marks every file
// in dir as applied without executing it. This prevents re-running
// migrations that were originally applied via docker-entrypoint-initdb.d.
func Run(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	tableExisted, err := tableExists(ctx, pool, "schema_migrations")
	if err != nil {
		return fmt.Errorf("check schema_migrations: %w", err)
	}
	if _, err := pool.Exec(ctx, createTableSQL); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	files, err := discover(dir)
	if err != nil {
		return fmt.Errorf("discover migrations: %w", err)
	}
	if len(files) == 0 {
		slog.Info("migrator: no migration files found", "dir", dir)
		return nil
	}

	if !tableExisted {
		legacy, err := tableExists(ctx, pool, "users")
		if err != nil {
			return fmt.Errorf("check legacy users table: %w", err)
		}
		if legacy {
			if err := markAllApplied(ctx, pool, files); err != nil {
				return fmt.Errorf("bootstrap legacy: %w", err)
			}
			slog.Info("migrator: legacy DB detected, marked all existing migrations as applied",
				"file_count", len(files),
			)
			return nil
		}
	}

	applied, err := loadApplied(ctx, pool)
	if err != nil {
		return fmt.Errorf("load applied: %w", err)
	}

	var appliedCount int
	for _, f := range files {
		name := filepath.Base(f)
		if _, ok := applied[name]; ok {
			continue
		}

		content, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		checksum := sha256Hex(content)

		start := time.Now()
		if err := applyOne(ctx, pool, name, string(content), checksum); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
		slog.Info("migrator: migration applied",
			"file", name,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		appliedCount++
	}

	slog.Info("migrator: finished",
		"total_files", len(files),
		"newly_applied", appliedCount,
		"already_applied", len(files)-appliedCount,
	)
	return nil
}

func applyOne(ctx context.Context, pool *pgxpool.Pool, name, sql, checksum string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, sql); err != nil {
		return fmt.Errorf("exec sql: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO schema_migrations (filename, checksum) VALUES ($1, $2)`,
		name, checksum,
	); err != nil {
		return fmt.Errorf("record migration: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func discover(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}
	sort.Strings(files)
	return files, nil
}

func loadApplied(ctx context.Context, pool *pgxpool.Pool) (map[string]string, error) {
	rows, err := pool.Query(ctx, `SELECT filename, checksum FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[string]string)
	for rows.Next() {
		var name, checksum string
		if err := rows.Scan(&name, &checksum); err != nil {
			return nil, err
		}
		applied[name] = checksum
	}
	return applied, rows.Err()
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func tableExists(ctx context.Context, pool *pgxpool.Pool, name string) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)`,
		name,
	).Scan(&exists)
	return exists, err
}

func markAllApplied(ctx context.Context, pool *pgxpool.Pool, files []string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, f := range files {
		content, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("read %s: %w", filepath.Base(f), err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations (filename, checksum) VALUES ($1, $2) ON CONFLICT (filename) DO NOTHING`,
			filepath.Base(f), sha256Hex(content),
		); err != nil {
			return fmt.Errorf("insert %s: %w", filepath.Base(f), err)
		}
	}
	return tx.Commit(ctx)
}
