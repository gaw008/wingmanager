// Package document owns document metadata and the corresponding disk files.
package document

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

const MaxSizeBytes int64 = 20 << 20

var (
	ErrTooLarge     = errors.New("document exceeds size limit")
	ErrInvalidOwner = errors.New("document requires exactly one owner")
	ErrNotFound     = errors.New("document not found")
)

// Owner identifies exactly one attachment target. Zero IDs mean no association.
type Owner struct {
	PropertyID, UnitID, LeaseID, PersonID, LedgerEntryID int64
}

type Document struct {
	ID             int64
	Owner          Owner
	Filename, MIME string
	SizeBytes      int64
	UploadedAt     string
}

// Store coordinates metadata transactions with files under the data directory.
// PLAN-DEVIATION: document IDs use AUTOINCREMENT to prevent post-delete disk
// cleanup from removing a new upload that reused the deleted document's ID.
type Store struct {
	database *sql.DB
	dataDir  string
	mu       sync.Mutex
}

func NewStore(database *sql.DB, dataDir string) *Store {
	return &Store{database: database, dataDir: dataDir}
}

// Upload publishes metadata only after the complete, size-limited file is in
// place. A failed commit removes the renamed file; interrupted processes can
// leave orphan files but never committed metadata pointing at a partial upload.
func (s *Store) Upload(ctx context.Context, owner Owner, filename string, body io.Reader) (_ Document, err error) {
	if err := owner.validate(); err != nil {
		return Document{}, err
	}
	if err := os.MkdirAll(filepath.Join(s.dataDir, "files"), 0700); err != nil {
		return Document{}, err
	}
	temp, err := os.CreateTemp(s.dataDir, ".upload-*")
	if err != nil {
		return Document{}, err
	}
	cleanupPath := temp.Name()
	locked := false
	defer func() {
		if cleanupPath != "" {
			err = errors.Join(err, os.Remove(cleanupPath))
		}
		if locked {
			s.mu.Unlock()
		}
	}()
	size, copyErr := io.Copy(temp, io.LimitReader(body, MaxSizeBytes+1))
	if copyErr != nil {
		return Document{}, errors.Join(copyErr, temp.Close())
	}
	if size > MaxSizeBytes {
		return Document{}, errors.Join(ErrTooLarge, temp.Close())
	}
	mime, detectErr := detectMIME(temp)
	if closeErr := temp.Close(); detectErr != nil || closeErr != nil {
		return Document{}, errors.Join(detectErr, closeErr)
	}
	// Failed transactions may reuse their uncommitted ID. Keep the next upload
	// behind cleanup without blocking other publications on network body reads.
	s.mu.Lock()
	locked = true
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return Document{}, err
	}
	defer tx.Rollback()
	record, err := New(tx).createDocument(ctx, createDocumentParams{
		PropertyID: nullID(owner.PropertyID), UnitID: nullID(owner.UnitID),
		LeaseID: nullID(owner.LeaseID), PersonID: nullID(owner.PersonID),
		LedgerEntryID: nullID(owner.LedgerEntryID), Filename: filename,
		Mime: mime, SizeBytes: size,
	})
	if err != nil {
		return Document{}, err
	}
	finalPath := s.path(record.ID)
	if err := os.Rename(temp.Name(), finalPath); err != nil {
		return Document{}, err
	}
	cleanupPath = finalPath
	if err := tx.Commit(); err != nil {
		return Document{}, err
	}
	cleanupPath = ""
	return fromRecord(record), nil
}

func (s *Store) List(ctx context.Context, owner Owner) ([]Document, error) {
	if err := owner.validate(); err != nil {
		return nil, err
	}
	records, err := New(s.database).listDocuments(ctx, listDocumentsParams{
		PropertyID: nullID(owner.PropertyID), UnitID: nullID(owner.UnitID),
		LeaseID: nullID(owner.LeaseID), PersonID: nullID(owner.PersonID),
		LedgerEntryID: nullID(owner.LedgerEntryID),
	})
	if err != nil {
		return nil, err
	}
	values := make([]Document, 0, len(records))
	for _, record := range records {
		values = append(values, fromRecord(record))
	}
	return values, nil
}

func (s *Store) Get(ctx context.Context, id int64) (Document, error) {
	record, err := New(s.database).getDocument(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	if err != nil {
		return Document{}, err
	}
	return fromRecord(record), nil
}

// Open returns a file positioned at its beginning and MIME detected from its
// current bytes. The caller owns closing the file and setting download headers.
func (s *Store) Open(ctx context.Context, id int64) (Document, *os.File, error) {
	record, err := New(s.database).getDocument(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, nil, ErrNotFound
	}
	if err != nil {
		return Document{}, nil, err
	}
	file, err := os.Open(s.path(id))
	if err != nil {
		return Document{}, nil, err
	}
	mime, err := detectMIME(file)
	if err != nil {
		return Document{}, nil, errors.Join(err, file.Close())
	}
	value := fromRecord(record)
	value.MIME = mime
	return value, file, nil
}

// Delete removes metadata before its file; disk cleanup errors are returned and
// may leave an orphan file, never a row referencing a deleted file.
func (s *Store) Delete(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := New(tx).getDocument(ctx, id); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if err := New(tx).deleteDocument(ctx, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return os.Remove(s.path(id))
}

func (s *Store) path(id int64) string {
	return filepath.Join(s.dataDir, "files", strconv.FormatInt(id, 10))
}

func (o Owner) validate() error {
	count := 0
	for _, id := range []int64{o.PropertyID, o.UnitID, o.LeaseID, o.PersonID, o.LedgerEntryID} {
		if id < 0 {
			return ErrInvalidOwner
		}
		if id > 0 {
			count++
		}
	}
	if count != 1 {
		return ErrInvalidOwner
	}
	return nil
}

func nullID(id int64) sql.NullInt64 {
	return sql.NullInt64{Int64: id, Valid: id != 0}
}

func fromRecord(record documentRecord) Document {
	return Document{
		ID: record.ID, Owner: Owner{
			PropertyID: record.PropertyID.Int64, UnitID: record.UnitID.Int64,
			LeaseID: record.LeaseID.Int64, PersonID: record.PersonID.Int64,
			LedgerEntryID: record.LedgerEntryID.Int64,
		},
		Filename: record.Filename, MIME: record.Mime,
		SizeBytes: record.SizeBytes, UploadedAt: record.UploadedAt,
	}
}

func detectMIME(file *os.File) (string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	var prefix [512]byte
	size, err := file.Read(prefix[:])
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	return http.DetectContentType(prefix[:size]), nil
}
