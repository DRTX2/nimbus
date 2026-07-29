package database

import (
	"context"
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database configuration: %w", err)
	}
	config.MaxConns = 10
	config.MinConns = 1
	config.MaxConnLifetime = time.Hour
	config.MaxConnIdleTime = 15 * time.Minute
	config.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// Migrate applies embedded, forward-only SQL migrations exactly once.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer connection.Release()
	if _, err := connection.Exec(ctx, "SELECT pg_advisory_lock(731928451)"); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = connection.Exec(unlockCtx, "SELECT pg_advisory_unlock(731928451)")
	}()

	if _, err := connection.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name text PRIMARY KEY,
			checksum text,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	if _, err := connection.Exec(ctx, "ALTER TABLE schema_migrations ADD COLUMN IF NOT EXISTS checksum text"); err != nil {
		return fmt.Errorf("upgrade migration table: %w", err)
	}

	entries, err := migrationEntries()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		contents, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		checksum := fmt.Sprintf("%x", sha256.Sum256(contents))
		tx, err := connection.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", entry.Name(), err)
		}
		var exists bool
		var appliedChecksum string
		err = tx.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name = $1),
			       COALESCE((SELECT checksum FROM schema_migrations WHERE name = $1), '')
		`, entry.Name()).Scan(&exists, &appliedChecksum)
		if err == nil && exists && appliedChecksum != "" && appliedChecksum != checksum {
			err = fmt.Errorf("migration %s checksum changed", entry.Name())
		}
		if err == nil && exists && appliedChecksum == "" {
			_, err = tx.Exec(ctx, "UPDATE schema_migrations SET checksum = $2 WHERE name = $1", entry.Name(), checksum)
		}
		if err == nil && !exists {
			if _, err = tx.Exec(ctx, string(contents)); err == nil {
				_, err = tx.Exec(ctx, "INSERT INTO schema_migrations (name, checksum) VALUES ($1, $2)", entry.Name(), checksum)
			}
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", entry.Name(), err)
		}
	}
	return nil
}

// VerifySchema prevents runtime components from serving with missing or modified migrations.
func VerifySchema(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := migrationEntries()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		contents, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		expected := fmt.Sprintf("%x", sha256.Sum256(contents))
		var valid bool
		if err := pool.QueryRow(ctx, "SELECT public.nimbus_verify_migration($1, $2)", entry.Name(), expected).Scan(&valid); err != nil {
			return fmt.Errorf("verify migration %s: %w", entry.Name(), err)
		}
		if !valid {
			return fmt.Errorf("verify migration %s: checksum mismatch", entry.Name())
		}
	}
	return nil
}

func migrationEntries() ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	entries = append([]fs.DirEntry(nil), entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	files := entries[:0]
	for _, entry := range entries {
		if !entry.IsDir() {
			files = append(files, entry)
		}
	}
	return files, nil
}
