package db_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gaw008/wingmanager/internal/db"
	"github.com/gaw008/wingmanager/internal/document"
	"github.com/gaw008/wingmanager/internal/property"
)

func backupSource(t *testing.T) (string, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "wingmanager.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	return dir, database
}

func TestBackupCopiesFilesAndOpensAsInstance(t *testing.T) {
	dir, database := backupSource(t)
	id, err := property.Create(t.Context(), database, property.Details{Name: "Demo home"}, "Suite")
	if err != nil {
		t.Fatal(err)
	}
	body := "A complete document in the backup."
	record, err := document.NewStore(database, dir).Upload(t.Context(), document.Owner{PropertyID: id}, "receipt.txt", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "backup")
	if err := db.Backup(t.Context(), dir, dest); err != nil {
		t.Fatal(err)
	}
	copyDB, err := db.Open(filepath.Join(dest, "wingmanager.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer copyDB.Close()
	if err := db.Migrate(t.Context(), copyDB); err != nil {
		t.Fatal(err)
	}
	got, file, err := document.NewStore(copyDB, dest).Open(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	bytes, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(bytes) != body || got.Filename != "receipt.txt" || got.Owner.PropertyID != id {
		t.Fatalf("restored document=%+v body=%q", got, bytes)
	}
}

func TestBackupRefusesExistingDestination(t *testing.T) {
	dir, _ := backupSource(t)
	for _, kind := range []string{"directory", "file"} {
		t.Run(kind, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "existing")
			marker := dest
			if kind == "directory" {
				if err := os.Mkdir(dest, 0700); err != nil {
					t.Fatal(err)
				}
				marker = filepath.Join(dest, "marker")
			}
			if err := os.WriteFile(marker, []byte("keep me"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := db.Backup(t.Context(), dir, dest); !errors.Is(err, os.ErrExist) {
				t.Fatalf("backup error=%v", err)
			}
			got, err := os.ReadFile(marker)
			if err != nil || string(got) != "keep me" {
				t.Fatalf("existing content=%q err=%v", got, err)
			}
		})
	}
}

func TestBackupConsistentWhileAnotherConnectionWrites(t *testing.T) {
	dir, database := backupSource(t)
	if _, err := database.Exec(`INSERT INTO properties (id, name, address) VALUES (1, '0', '0')`); err != nil {
		t.Fatal(err)
	}
	writer, err := db.Open(filepath.Join(dir, "wingmanager.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		tx, err := writer.BeginTx(t.Context(), nil)
		if err != nil {
			done <- err
			return
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`UPDATE properties SET name = '1' WHERE id = 1`); err != nil {
			done <- err
			return
		}
		close(started)
		// Start the backup while this transaction contains inconsistent fields.
		// It must wait for the complete commit instead of copying this state.
		time.Sleep(20 * time.Millisecond)
		if _, err := tx.Exec(`UPDATE properties SET address = '1' WHERE id = 1`); err != nil {
			done <- err
			return
		}
		done <- tx.Commit()
	}()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("writer failed: %v", err)
	}
	dest := filepath.Join(t.TempDir(), "backup")
	backupErr := db.Backup(t.Context(), dir, dest)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if backupErr != nil {
		t.Fatal(backupErr)
	}
	copyDB, err := db.Open(filepath.Join(dest, "wingmanager.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer copyDB.Close()
	if err := db.Migrate(t.Context(), copyDB); err != nil {
		t.Fatal(err)
	}
	var name, address, integrity string
	if err := copyDB.QueryRow(`SELECT name, address FROM properties WHERE id = 1`).Scan(&name, &address); err != nil {
		t.Fatal(err)
	}
	if err := copyDB.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatal(err)
	}
	if name != "1" || address != "1" || integrity != "ok" {
		t.Fatalf("snapshot name=%s address=%s integrity=%s", name, address, integrity)
	}
	if info, err := os.Stat(filepath.Join(dest, "files")); err != nil || !info.IsDir() {
		t.Fatalf("empty document directory: %v, %v", info, err)
	}
}

func TestBackupFailureRemovesIncompleteDestination(t *testing.T) {
	dir, _ := backupSource(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	dest := filepath.Join(t.TempDir(), "backup")
	if err := db.Backup(ctx, dir, dest); !errors.Is(err, context.Canceled) {
		t.Fatalf("backup error=%v", err)
	}
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("incomplete destination remains: %v", err)
	}
}

func TestBackupRefusesDestinationInsideSourceFiles(t *testing.T) {
	dir, _ := backupSource(t)
	files := filepath.Join(dir, "files")
	if err := os.Mkdir(files, 0700); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(files, "backup")
	if err := db.Backup(t.Context(), dir, dest); err == nil {
		t.Fatal("backup into its own document files was accepted")
	}
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected destination was created: %v", err)
	}
}
