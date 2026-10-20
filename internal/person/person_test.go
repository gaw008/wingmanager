package person_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/gaw008/wingmanager/internal/db"
	"github.com/gaw008/wingmanager/internal/person"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
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
	return database
}

func TestValidationEditAndSearch(t *testing.T) {
	database := testDB(t)
	if _, err := person.Create(t.Context(), database, person.Details{Name: " \t\n "}); !errors.Is(err, person.ErrName) {
		t.Fatal(err)
	}
	id, err := person.Create(t.Context(), database, person.Details{Name: " Alice Smith ", Email: "alice@example.com", Phone: "123", Notes: "Guest"})
	if err != nil {
		t.Fatal(err)
	}
	value, err := person.Get(t.Context(), database, id)
	if err != nil || value.Name != "Alice Smith" || value.Email != "alice@example.com" || value.Phone != "123" || value.Notes != "Guest" {
		t.Fatalf("value=%+v err=%v", value, err)
	}
	if err := person.Update(t.Context(), database, id, person.Details{Name: " "}); !errors.Is(err, person.ErrName) {
		t.Fatal(err)
	}
	unchanged, err := person.Get(t.Context(), database, id)
	if err != nil || unchanged != value {
		t.Fatalf("invalid edit changed contact: %+v %v", unchanged, err)
	}
	if err := person.Update(t.Context(), database, id, person.Details{Name: " Alice Wong ", Email: "new@example.com", Phone: "456", Notes: "Updated"}); err != nil {
		t.Fatal(err)
	}
	value, err = person.Get(t.Context(), database, id)
	if err != nil || value.Name != "Alice Wong" || value.Email != "new@example.com" || value.Phone != "456" || value.Notes != "Updated" {
		t.Fatalf("value=%+v err=%v", value, err)
	}
	for _, name := range []string{"Bob", "王小明", "100%_Name"} {
		if _, err := person.Create(t.Context(), database, person.Details{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		search string
		count  int
		name   string
	}{
		{"", 4, ""}, {" wong ", 1, "Alice Wong"}, {"ALICE", 1, "Alice Wong"}, {"小明", 1, "王小明"}, {"%_", 1, "100%_Name"}, {"Smith", 0, ""}, {"missing", 0, ""},
	} {
		values, err := person.List(t.Context(), database, test.search)
		if err != nil || len(values) != test.count {
			t.Fatalf("search=%q values=%v err=%v", test.search, values, err)
		}
		if test.name != "" && values[0].Name != test.name {
			t.Fatalf("search=%q values=%v", test.search, values)
		}
	}
	if _, err := person.Get(t.Context(), database, 99); !errors.Is(err, person.ErrNotFound) {
		t.Fatal(err)
	}
	if err := person.Update(t.Context(), database, 99, person.Details{Name: "Missing"}); !errors.Is(err, person.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestCreateInTxLeavesCommitAndRollbackToCaller(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit"}[commit], func(t *testing.T) {
			database := testDB(t)
			tx, err := database.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err := person.CreateInTx(t.Context(), tx, person.Details{Name: " \t "}); !errors.Is(err, person.ErrName) {
				t.Fatalf("invalid transaction contact: %v", err)
			}
			id, err := person.CreateInTx(t.Context(), tx, person.Details{Name: " Guest ", Email: "guest@example.test"})
			if err != nil {
				t.Fatal(err)
			}
			if commit {
				err = tx.Commit()
			} else {
				err = tx.Rollback()
			}
			if err != nil {
				t.Fatal(err)
			}
			value, err := person.Get(t.Context(), database, id)
			if commit {
				if err != nil || value.Name != "Guest" || value.Email != "guest@example.test" {
					t.Fatalf("committed contact=%+v err=%v", value, err)
				}
			} else if !errors.Is(err, person.ErrNotFound) {
				t.Fatalf("rolled-back contact=%+v err=%v", value, err)
			}
		})
	}
}
