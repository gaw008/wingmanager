// Package person owns the people shared by guests and tenants.
package person

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

var (
	ErrName     = errors.New("person name is required")
	ErrNotFound = errors.New("person not found")
)

// Details contains the editable contact fields. Names are trimmed on save.
type Details struct{ Name, Email, Phone, Notes string }

// Person is a contact; guest and tenant roles are determined by later relationships.
type Person struct {
	ID                        int64
	Name, Email, Phone, Notes string
}

// Create saves a contact with a non-empty, trimmed name.
func Create(ctx context.Context, database *sql.DB, value Details) (int64, error) {
	return create(ctx, New(database), value)
}

// CreateInTx saves a contact in the caller's transaction without committing it.
func CreateInTx(ctx context.Context, tx *sql.Tx, value Details) (int64, error) {
	return create(ctx, New(tx), value)
}

func create(ctx context.Context, queries *Queries, value Details) (int64, error) {
	value.Name = strings.TrimSpace(value.Name)
	if value.Name == "" {
		return 0, ErrName
	}
	return queries.createPerson(ctx, createPersonParams(value))
}

// Get reads an existing contact.
func Get(ctx context.Context, database *sql.DB, id int64) (Person, error) {
	row, err := New(database).getPerson(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Person{}, ErrNotFound
	}
	return Person(row), err
}

// List searches for a literal substring of the name; empty search lists everyone.
// SQLite lower() is case-insensitive for ASCII and preserves other scripts.
func List(ctx context.Context, database *sql.DB, search string) ([]Person, error) {
	rows, err := New(database).listPeople(ctx, strings.TrimSpace(search))
	if err != nil {
		return nil, err
	}
	values := make([]Person, len(rows))
	for i, row := range rows {
		values[i] = Person(row)
	}
	return values, nil
}

// Update changes only the contact fields of an existing person.
func Update(ctx context.Context, database *sql.DB, id int64, value Details) error {
	value.Name = strings.TrimSpace(value.Name)
	if value.Name == "" {
		return ErrName
	}
	rows, err := New(database).updatePerson(ctx, updatePersonParams{ID: id, Name: value.Name, Email: value.Email, Phone: value.Phone, Notes: value.Notes})
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
