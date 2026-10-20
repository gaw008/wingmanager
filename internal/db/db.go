// Package db opens the application database and applies embedded migrations.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open configures every pooled connection, including connections opened later.
func Open(path string) (*sql.DB, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}
	params := url.Values{}
	params.Add("_pragma", "busy_timeout(5000)") // Milliseconds to wait for another writer.
	params.Add("_pragma", "journal_mode(WAL)")
	params.Add("_pragma", "foreign_keys(ON)")
	// PLAN §6.3 relies on IMMEDIATE transactions taking the write lock before reading.
	params.Set("_txlock", "immediate")
	uri.RawQuery = params.Encode()
	database, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	if err := database.Ping(); err != nil {
		database.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	return database, nil
}

// Migrate applies each numbered migration once, atomically with its version record.
func Migrate(ctx context.Context, database *sql.DB) error {
	return migrate(ctx, database, migrations)
}

func migrate(ctx context.Context, database *sql.DB, files fs.FS) error {
	if _, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY)`); err != nil {
		return err
	}
	names, err := fs.Glob(files, "migrations/*.sql")
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := apply(ctx, database, files, name); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}

func apply(ctx context.Context, database *sql.DB, files fs.FS, name string) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)`, name).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return tx.Commit()
	}
	script, err := fs.ReadFile(files, name)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, string(script)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, name); err != nil {
		return err
	}
	return tx.Commit()
}
