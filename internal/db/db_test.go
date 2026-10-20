package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	return database
}

func TestMigrateTwice(t *testing.T) {
	database := openTestDB(t)
	for range 2 {
		if err := Migrate(t.Context(), database); err != nil {
			t.Fatal(err)
		}
	}
	var versions, rows int
	if err := database.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT count(*) FROM settings`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if versions != 1 || rows != 1 {
		t.Fatalf("versions=%d settings=%d", versions, rows)
	}
	records, err := database.Query(`SELECT name FROM sqlite_master WHERE type='table' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer records.Close()
	var tables []string
	for records.Next() {
		var name string
		if err := records.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	if err := records.Err(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"schema_migrations", "sessions", "settings", "users"}; !reflect.DeepEqual(tables, want) {
		t.Fatalf("tables=%v want %v", tables, want)
	}
}

func TestFailedMigrationRollsBack(t *testing.T) {
	database := openTestDB(t)
	if err := Migrate(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{"migrations/0002_failure.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE partial (id INTEGER); INSERT INTO partial VALUES (1); INSERT INTO missing VALUES (1);`)}}
	if err := migrate(t.Context(), database, files); err == nil {
		t.Fatal("expected migration failure")
	}
	var partial, versions int
	if err := database.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='partial'`).Scan(&partial); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if partial != 0 || versions != 1 {
		t.Fatalf("partial schema=%d versions=%d", partial, versions)
	}
	// A corrected migration can apply after rollback without resetting the database.
	files["migrations/0002_failure.sql"].Data = []byte(`CREATE TABLE complete (id INTEGER);`)
	if err := migrate(t.Context(), database, files); err != nil {
		t.Fatal(err)
	}
}

func TestEveryConnectionConfiguredAndTransactionsImmediate(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	first, err := database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	for _, conn := range []*sql.Conn{first, second} {
		var mode string
		var foreignKeys, timeout int
		if err := conn.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil {
			t.Fatal(err)
		}
		if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
			t.Fatal(err)
		}
		if err := conn.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&timeout); err != nil {
			t.Fatal(err)
		}
		if mode != "wal" || foreignKeys != 1 || timeout <= 0 {
			t.Fatalf("mode=%s foreign_keys=%d busy_timeout=%d", mode, foreignKeys, timeout)
		}
	}
	tx, err := first.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// With deferred transactions, the second BEGIN would succeed before any writes.
	// Disable waiting only on this test connection to observe the lock immediately.
	if _, err := second.ExecContext(ctx, `PRAGMA busy_timeout=0`); err != nil {
		t.Fatal(err)
	}
	other, err := second.BeginTx(ctx, nil)
	if err == nil {
		other.Rollback()
		t.Fatal("second transaction acquired a writer lock")
	}
}
