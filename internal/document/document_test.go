package document_test

import (
	"bytes"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gaw008/wingmanager/internal/db"
	"github.com/gaw008/wingmanager/internal/document"
)

func fixture(t *testing.T) (*sql.DB, *document.Store, string) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
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
	_, err = database.Exec(`
INSERT INTO properties (id,name) VALUES (1,'Home');
INSERT INTO units (id,property_id,name,ical_export_token) VALUES (1,1,'Unit','token');
INSERT INTO people (id,name) VALUES (1,'Tenant');
INSERT INTO leases (id,unit_id,status,starts_on,rent_cents,due_day) VALUES (1,1,'draft','2026-01-01',10000,1);
INSERT INTO ledger_entries (id,property_id,kind,category,amount_cents,occurred_on) VALUES (1,1,'expense','repairs',100,'2026-01-01');`)
	if err != nil {
		t.Fatal(err)
	}
	return database, document.NewStore(database, dir), dir
}

func assertEmpty(t *testing.T, database *sql.DB, dir string) {
	t.Helper()
	var count int
	if err := database.QueryRow(`SELECT count(*) FROM documents`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("documents count=%d err=%v", count, err)
	}
	if files, err := filepath.Glob(filepath.Join(dir, ".upload-*")); err != nil || len(files) != 0 {
		t.Fatalf("temporary files=%v err=%v", files, err)
	}
	files, err := os.ReadDir(filepath.Join(dir, "files"))
	if !errors.Is(err, os.ErrNotExist) && (err != nil || len(files) != 0) {
		t.Fatalf("published files=%v err=%v", files, err)
	}
}

func TestExactlyOneOwnerDatabaseConstraint(t *testing.T) {
	database, _, _ := fixture(t)
	for mask := range 32 {
		values := make([]any, 5)
		owners := 0
		for i := range values {
			if mask&(1<<i) != 0 {
				values[i] = int64(1)
				owners++
			}
		}
		_, err := database.Exec(`INSERT INTO documents (property_id,unit_id,lease_id,person_id,ledger_entry_id,filename,mime,size_bytes) VALUES (?,?,?,?,?,'receipt','text/plain',1)`, values...)
		if (err == nil) != (owners == 1) {
			t.Fatalf("owner mask=%05b count=%d err=%v", mask, owners, err)
		}
	}
}

func TestUploadRoundTripAllOwnersAndDelete(t *testing.T) {
	database, store, dir := fixture(t)
	body := []byte("<!DOCTYPE html><html><script>alert('receipt')</script></html>\x00\xff")
	owners := []document.Owner{
		{PropertyID: 1}, {UnitID: 1}, {LeaseID: 1}, {PersonID: 1}, {LedgerEntryID: 1},
	}
	var previous int64
	for _, owner := range owners {
		value, err := store.Upload(t.Context(), owner, "收据.html", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if value.ID <= previous || value.Owner != owner || value.Filename != "收据.html" || value.SizeBytes != int64(len(body)) || value.UploadedAt == "" || value.MIME != "text/html; charset=utf-8" {
			t.Fatalf("metadata=%+v", value)
		}
		previous = value.ID
		listed, err := store.List(t.Context(), owner)
		if err != nil || len(listed) != 1 || listed[0] != value {
			t.Fatalf("listed=%v err=%v", listed, err)
		}
		// Even stored metadata cannot override server detection at download time.
		if _, err := database.Exec(`UPDATE documents SET mime='application/pdf' WHERE id=?`, value.ID); err != nil {
			t.Fatal(err)
		}
		download, file, err := store.Open(t.Context(), value.ID)
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(file)
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || !bytes.Equal(data, body) || download.MIME != "text/html; charset=utf-8" {
			t.Fatalf("download=%+v body=%q err=%v close=%v", download, data, readErr, closeErr)
		}
		if err := store.Delete(t.Context(), value.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Get(t.Context(), value.ID); !errors.Is(err, document.ErrNotFound) {
			t.Fatalf("deleted metadata: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "files", strconv.FormatInt(value.ID, 10))); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("deleted bytes: %v", err)
		}
	}
	assertEmpty(t, database, dir)
}

func TestUploadRejectedInputsCleanUp(t *testing.T) {
	database, store, dir := fixture(t)
	for _, owner := range []document.Owner{{}, {PropertyID: 1, UnitID: 1}, {PropertyID: -1}} {
		if _, err := store.Upload(t.Context(), owner, "receipt", strings.NewReader("receipt")); !errors.Is(err, document.ErrInvalidOwner) {
			t.Fatal(err)
		}
		if _, err := store.List(t.Context(), owner); !errors.Is(err, document.ErrInvalidOwner) {
			t.Fatal(err)
		}
	}
	if _, err := store.Upload(t.Context(), document.Owner{PropertyID: 1}, "large", io.LimitReader(zeroReader{}, document.MaxSizeBytes+1)); !errors.Is(err, document.ErrTooLarge) {
		t.Fatalf("oversize: %v", err)
	}
	if _, err := store.Upload(t.Context(), document.Owner{PropertyID: 999}, "missing", strings.NewReader("receipt")); err == nil {
		t.Fatal("foreign key failure accepted")
	}
	if _, err := store.Upload(t.Context(), document.Owner{PropertyID: 1}, "broken", brokenReader{}); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("reader failure: %v", err)
	}
	assertEmpty(t, database, dir)
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestRenameFailureRollsBackMetadata(t *testing.T) {
	database, store, dir := fixture(t)
	filesDir := filepath.Join(dir, "files")
	if err := os.Mkdir(filesDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filesDir, 0700) })
	_, err := store.Upload(t.Context(), document.Owner{PropertyID: 1}, "receipt", strings.NewReader("receipt"))
	var pathErr *os.LinkError
	if !errors.As(err, &pathErr) || pathErr.Op != "rename" {
		t.Fatalf("expected rename failure in unwritable files directory, got %v", err)
	}
	assertEmpty(t, database, dir)
}

func TestCommitFailureRemovesRenamedFile(t *testing.T) {
	database, store, dir := fixture(t)
	// The real deferred foreign key rejects COMMIT after rename has succeeded.
	_, err := database.Exec(`
CREATE TABLE rejected_upload (property_id INTEGER REFERENCES properties(id) DEFERRABLE INITIALLY DEFERRED);
CREATE TRIGGER fail_document_commit AFTER INSERT ON documents BEGIN
  INSERT INTO rejected_upload (property_id) VALUES (-1);
END;`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upload(t.Context(), document.Owner{PropertyID: 1}, "receipt", strings.NewReader("receipt")); err == nil {
		t.Fatal("deferred foreign key accepted")
	}
	assertEmpty(t, database, dir)
}

func TestDeleteFailureKeepsMetadataOrLeavesOnlyOrphan(t *testing.T) {
	database, store, dir := fixture(t)
	value, err := store.Upload(t.Context(), document.Owner{PropertyID: 1}, "receipt", strings.NewReader("receipt"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TRIGGER reject_delete BEFORE DELETE ON documents BEGIN SELECT RAISE(ABORT,'no delete'); END`); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(t.Context(), value.ID); err == nil {
		t.Fatal("failed delete accepted")
	}
	if _, err := store.Get(t.Context(), value.ID); err != nil {
		t.Fatal("failed transaction lost metadata", err)
	}
	if _, err := database.Exec(`DROP TRIGGER reject_delete`); err != nil {
		t.Fatal(err)
	}
	filesDir := filepath.Join(dir, "files")
	if err := os.Chmod(filesDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filesDir, 0700) })
	if err := store.Delete(t.Context(), value.ID); err == nil {
		t.Fatal("disk cleanup failure hidden")
	}
	if _, err := store.Get(t.Context(), value.ID); !errors.Is(err, document.ErrNotFound) {
		t.Fatal("metadata was not committed before disk deletion", err)
	}
	if _, err := os.Stat(filepath.Join(filesDir, strconv.FormatInt(value.ID, 10))); err != nil {
		t.Fatal("expected orphan file after failed disk deletion", err)
	}
}

func TestConcurrentAndRepeatedUploadsRemainDistinct(t *testing.T) {
	_, store, _ := fixture(t)
	const uploads = 4
	var wg sync.WaitGroup
	for range uploads {
		wg.Go(func() {
			if _, err := store.Upload(t.Context(), document.Owner{PropertyID: 1}, "same-name", strings.NewReader("same bytes")); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	values, err := store.List(t.Context(), document.Owner{PropertyID: 1})
	if err != nil || len(values) != uploads {
		t.Fatalf("uploads=%+v err=%v", values, err)
	}
	for _, value := range values {
		_, file, err := store.Open(t.Context(), value.ID)
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(file)
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || string(data) != "same bytes" {
			t.Fatalf("id=%d body=%q err=%v close=%v", value.ID, data, readErr, closeErr)
		}
	}
}

func TestSlowUploadDoesNotBlockDeletingAnotherDocument(t *testing.T) {
	_, store, _ := fixture(t)
	existing, err := store.Upload(t.Context(), document.Owner{PropertyID: 1}, "existing", strings.NewReader("receipt"))
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	readBody := &waitingReader{entered: entered, release: release}
	uploadResult := make(chan error, 1)
	go func() {
		_, err := store.Upload(t.Context(), document.Owner{PropertyID: 1}, "slow", readBody)
		uploadResult <- err
	}()
	<-entered
	deleteResult := make(chan error, 1)
	go func() { deleteResult <- store.Delete(t.Context(), existing.ID) }()
	select {
	case err := <-deleteResult:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(5 * time.Second):
		t.Error("deletion blocked behind an unfinished upload body")
	}
	close(release)
	if err := <-uploadResult; err != nil {
		t.Fatal(err)
	}
}

type waitingReader struct {
	entered chan struct{}
	release chan struct{}
	done    bool
}

func (r *waitingReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	r.done = true
	close(r.entered)
	<-r.release
	return copy(p, "receipt"), nil
}
