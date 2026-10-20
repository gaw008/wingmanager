package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Backup creates a new directory containing a consistent database snapshot and
// its document files. The source may be serving requests; writes wait until the
// snapshot and file copy finish. An existing destination is never overwritten. The
// destination must be outside the source document files directory.
func Backup(ctx context.Context, dataDir, dest string) (err error) {
	dataDir, err = filepath.Abs(dataDir)
	if err != nil {
		return err
	}
	dest, err = filepath.Abs(dest)
	if err != nil {
		return err
	}
	files := filepath.Join(dataDir, "files")
	relative, err := filepath.Rel(files, dest)
	if err != nil {
		return err
	}
	if filepath.IsLocal(relative) {
		return errors.New("backup destination must be outside the source document files directory")
	}
	source := filepath.Join(dataDir, "wingmanager.db")
	if _, err := os.Stat(source); err != nil {
		return err
	}
	if err := os.Mkdir(dest, 0700); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.RemoveAll(dest))
		}
	}()
	database, err := Open(source)
	if err != nil {
		return fmt.Errorf("open backup source: %w", err)
	}
	defer func() { err = errors.Join(err, database.Close()) }()

	// A separate IMMEDIATE transaction freezes document publication/deletion.
	// VACUUM needs its own connection outside a transaction; WAL allows its read
	// snapshot while this connection holds the writer lock.
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("lock backup source: %w", err)
	}
	defer func() { err = errors.Join(err, tx.Rollback()) }()
	if _, err := database.ExecContext(ctx, `VACUUM INTO ?`, filepath.Join(dest, "wingmanager.db")); err != nil {
		return fmt.Errorf("backup database: %w", err)
	}
	if _, err := os.Stat(files); errors.Is(err, os.ErrNotExist) {
		return os.Mkdir(filepath.Join(dest, "files"), 0700)
	} else if err != nil {
		return err
	}
	if err := os.CopyFS(filepath.Join(dest, "files"), os.DirFS(files)); err != nil {
		return fmt.Errorf("backup document files: %w", err)
	}
	return nil
}
