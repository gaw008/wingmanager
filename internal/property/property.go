// Package property owns properties, units, and their lifecycle invariants.
package property

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var (
	ErrName              = errors.New("property name is required")
	ErrUnitName          = errors.New("unit name is required")
	ErrDuplicateUnitName = errors.New("unit name already exists in this property")
	ErrLastActiveUnit    = errors.New("a property must retain at least one active unit")
	ErrNotFound          = errors.New("property or unit not found")
)

// Details contains the editable property fields. Names are trimmed on save.
type Details struct{ Name, Address, Notes string }

// Property is an address with at least one active unit. Empty ArchivedAt means active.
type Property struct {
	ID                               int64
	Name, Address, Notes, ArchivedAt string
}

// Unit is an independently rentable part of a property. Empty ArchivedAt means active.
type Unit struct {
	ID, PropertyID                    int64
	Name, IcalExportToken, ArchivedAt string
}

// Create atomically creates a property and its first active unit, including a new token.
func Create(ctx context.Context, database *sql.DB, value Details, firstUnitName string) (int64, error) {
	value.Name = strings.TrimSpace(value.Name)
	if value.Name == "" {
		return 0, ErrName
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	queries := New(tx)
	id, err := queries.createProperty(ctx, createPropertyParams(value))
	if err != nil {
		return 0, err
	}
	if _, err := addUnit(ctx, queries, id, firstUnitName); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// Get includes archived properties so they remain editable and can be restored.
func Get(ctx context.Context, database *sql.DB, id int64) (Property, error) {
	row, err := New(database).getProperty(ctx, id)
	return Property(row), readError(err)
}

// List hides archived properties unless includeArchived is true.
func List(ctx context.Context, database *sql.DB, includeArchived bool) ([]Property, error) {
	rows, err := New(database).listProperties(ctx, includeArchived)
	if err != nil {
		return nil, err
	}
	values := make([]Property, len(rows))
	for i, row := range rows {
		values[i] = Property(row)
	}
	return values, nil
}

// Update changes only editable fields, preserving the property's archive state.
func Update(ctx context.Context, database *sql.DB, id int64, value Details) error {
	value.Name = strings.TrimSpace(value.Name)
	if value.Name == "" {
		return ErrName
	}
	rows, err := New(database).updateProperty(ctx, updatePropertyParams{Name: value.Name, Address: value.Address, Notes: value.Notes, ID: id})
	return writeError(rows, err)
}

// SetArchived archives or restores a property without changing its units.
// Repeating the same action preserves the archive timestamp and unit invariant.
func SetArchived(ctx context.Context, database *sql.DB, id int64, archived bool) error {
	var flag int64
	if archived {
		flag = 1
	}
	rows, err := New(database).setPropertyArchived(ctx, setPropertyArchivedParams{ID: id, Archived: flag})
	return writeError(rows, err)
}

// AddUnit creates an active unit with a unique, opaque export token.
func AddUnit(ctx context.Context, database *sql.DB, propertyID int64, name string) (int64, error) {
	return addUnit(ctx, New(database), propertyID, name)
}

func addUnit(ctx context.Context, queries *Queries, propertyID int64, name string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, ErrUnitName
	}
	token, err := newToken()
	if err != nil {
		return 0, err
	}
	id, err := queries.createUnit(ctx, createUnitParams{PropertyID: propertyID, Name: name, IcalExportToken: token})
	return id, unitError(err)
}

// GetUnit includes archived units. Tokens are only disclosed to the authenticated owner.
func GetUnit(ctx context.Context, database *sql.DB, id int64) (Unit, error) {
	row, err := New(database).getUnit(ctx, id)
	return Unit(row), readError(err)
}

// ListUnits hides archived units unless includeArchived is true.
func ListUnits(ctx context.Context, database *sql.DB, propertyID int64, includeArchived bool) ([]Unit, error) {
	rows, err := New(database).listUnits(ctx, listUnitsParams{PropertyID: propertyID, IncludeArchived: includeArchived})
	if err != nil {
		return nil, err
	}
	values := make([]Unit, len(rows))
	for i, row := range rows {
		values[i] = Unit(row)
	}
	return values, nil
}

// RenameUnit reserves names within the property, including names of archived units.
func RenameUnit(ctx context.Context, database *sql.DB, id int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrUnitName
	}
	rows, err := New(database).renameUnit(ctx, renameUnitParams{ID: id, Name: name})
	return writeError(rows, unitError(err))
}

// SetUnitArchived rejects archiving the last active unit, even in an archived property.
// Use a database opened by db.Open: its IMMEDIATE transaction serializes the count
// and update, so concurrent archives cannot both remove the last active units.
func SetUnitArchived(ctx context.Context, database *sql.DB, id int64, archived bool) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	queries := New(tx)
	unit, err := queries.getUnit(ctx, id)
	if err != nil {
		return readError(err)
	}
	var flag int64
	if archived {
		flag = 1
		if unit.ArchivedAt == "" {
			count, err := queries.countActiveUnits(ctx, unit.PropertyID)
			if err != nil {
				return err
			}
			if count == 1 {
				return ErrLastActiveUnit
			}
		}
	}
	if err := queries.setUnitArchived(ctx, setUnitArchivedParams{ID: id, Archived: flag}); err != nil {
		return err
	}
	return tx.Commit()
}

// RegenerateToken replaces the token atomically; the old token is no longer stored.
func RegenerateToken(ctx context.Context, database *sql.DB, id int64) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	token, err = New(database).regenerateToken(ctx, regenerateTokenParams{ID: id, IcalExportToken: token})
	return token, readError(err)
}

func newToken() (string, error) {
	const tokenBytes = 32
	secret := make([]byte, tokenBytes)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(secret), nil
}

func readError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func writeError(rows int64, err error) error {
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func unitError(err error) error {
	var constraint *sqlite.Error
	if errors.As(err, &constraint) && constraint.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE && strings.Contains(constraint.Error(), "units.property_id, units.name") {
		return ErrDuplicateUnitName
	}
	return err
}
