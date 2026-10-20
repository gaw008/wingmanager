package db

import (
	"context"
	"database/sql"
	"io/fs"
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
	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	if versions != len(files) || rows != 1 {
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
	if want := []string{"bookings", "calendar_feeds", "lease_tenants", "leases", "ledger_entries", "people", "properties", "schema_migrations", "sessions", "settings", "tasks", "units", "users"}; !reflect.DeepEqual(tables, want) {
		t.Fatalf("tables=%v want %v", tables, want)
	}
}

func TestFailedMigrationRollsBack(t *testing.T) {
	database := openTestDB(t)
	if err := Migrate(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	var before int
	if err := database.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{"migrations/9999_failure.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE partial (id INTEGER); INSERT INTO partial VALUES (1); INSERT INTO missing VALUES (1);`)}}
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
	if partial != 0 || versions != before {
		t.Fatalf("partial schema=%d versions=%d", partial, versions)
	}
	// A corrected migration can apply after rollback without resetting the database.
	files["migrations/9999_failure.sql"].Data = []byte(`CREATE TABLE complete (id INTEGER);`)
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

func TestM2MigrationPreservesM1Data(t *testing.T) {
	database := openTestDB(t)
	// Install the committed M1 schema before applying the newly added migration.
	script, err := migrations.ReadFile("migrations/0001_accounts.sql")
	if err != nil {
		t.Fatal(err)
	}
	first := fstest.MapFS{"migrations/0001_accounts.sql": &fstest.MapFile{Data: script}}
	if err := migrate(t.Context(), database, first); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO users (email,password_hash) VALUES ('owner@example.com','existing hash'); UPDATE settings SET landlord_name = 'Owner', locale = 'en';`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	var email, hash, name, locale string
	if err := database.QueryRow(`SELECT email, password_hash FROM users`).Scan(&email, &hash); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT landlord_name,locale FROM settings`).Scan(&name, &locale); err != nil {
		t.Fatal(err)
	}
	if email != "owner@example.com" || hash != "existing hash" || name != "Owner" || locale != "en" {
		t.Fatal("M2 migration changed M1 data")
	}
}

func TestM3MigrationPreservesM2Data(t *testing.T) {
	database := openTestDB(t)
	prior := fstest.MapFS{}
	for _, name := range []string{"migrations/0001_accounts.sql", "migrations/0002_properties_people.sql"} {
		script, err := migrations.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		prior[name] = &fstest.MapFile{Data: script}
	}
	if err := migrate(t.Context(), database, prior); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
 INSERT INTO properties (id, name, address, notes) VALUES (1, 'Home', '1 Main St', 'Property notes');
 INSERT INTO units (id, property_id, name, ical_export_token) VALUES (1, 1, 'Whole home', 'existing-export-token');
 INSERT INTO people (id, name, email, phone, notes) VALUES (1, 'Guest', 'guest@example.test', '123', 'Contact notes');
 `); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	var propertyName, address, propertyNotes, unitName, token, personName, email, phone, personNotes string
	if err := database.QueryRow(`SELECT p.name, p.address, p.notes, u.name, u.ical_export_token FROM properties p JOIN units u ON u.property_id = p.id WHERE u.id = 1`).Scan(&propertyName, &address, &propertyNotes, &unitName, &token); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT name, email, phone, notes FROM people WHERE id = 1`).Scan(&personName, &email, &phone, &personNotes); err != nil {
		t.Fatal(err)
	}
	if propertyName != "Home" || address != "1 Main St" || propertyNotes != "Property notes" || unitName != "Whole home" || token != "existing-export-token" || personName != "Guest" || email != "guest@example.test" || phone != "123" || personNotes != "Contact notes" {
		t.Fatal("M3 migration changed M2 data")
	}
	var feeds, bookings int
	if err := database.QueryRow(`SELECT (SELECT count(*) FROM calendar_feeds), (SELECT count(*) FROM bookings)`).Scan(&feeds, &bookings); err != nil {
		t.Fatal(err)
	}
	if feeds != 0 || bookings != 0 {
		t.Fatalf("unexpected M3 records: feeds=%d bookings=%d", feeds, bookings)
	}
}
