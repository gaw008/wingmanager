package property_test

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"path/filepath"
	"testing"

	"github.com/gaw008/wingmanager/internal/db"
	"github.com/gaw008/wingmanager/internal/property"
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

func create(t *testing.T, database *sql.DB, name string) (int64, property.Unit) {
	t.Helper()
	id, err := property.Create(t.Context(), database, property.Details{Name: name, Address: "1 Main St", Notes: "Notes"}, " Whole home ")
	if err != nil {
		t.Fatal(err)
	}
	units, err := property.ListUnits(t.Context(), database, id, true)
	if err != nil || len(units) != 1 {
		t.Fatalf("units=%v err=%v", units, err)
	}
	return id, units[0]
}

func TestCreatePropertyAndFirstUnitAtomic(t *testing.T) {
	database := testDB(t)
	id, unit := create(t, database, " Home ")
	value, err := property.Get(t.Context(), database, id)
	if err != nil {
		t.Fatal(err)
	}
	if value.Name != "Home" || value.Address != "1 Main St" || value.Notes != "Notes" || value.ArchivedAt != "" || unit.PropertyID != id || unit.Name != "Whole home" || unit.ArchivedAt != "" {
		t.Fatalf("property=%+v unit=%+v", value, unit)
	}
	secret, err := base64.RawURLEncoding.DecodeString(unit.IcalExportToken)
	if err != nil || len(secret) != 32 {
		t.Fatalf("token is not 32 base64url bytes: %v", err)
	}
	for _, test := range []struct {
		name, unit string
		want       error
	}{
		{" \n ", "Unit", property.ErrName},
		{"Another home", " \t ", property.ErrUnitName},
	} {
		if id, err := property.Create(t.Context(), database, property.Details{Name: test.name}, test.unit); id != 0 || !errors.Is(err, test.want) {
			t.Fatalf("id=%d err=%v want=%v", id, err, test.want)
		}
	}
	// Force a real insert failure after the property insert, exercising transaction rollback.
	if _, err := database.Exec(`CREATE TRIGGER reject_unit BEFORE INSERT ON units BEGIN SELECT RAISE(ABORT, 'unit insert failed'); END`); err != nil {
		t.Fatal(err)
	}
	if id, err := property.Create(t.Context(), database, property.Details{Name: "Fails"}, "Valid unit"); id != 0 || err == nil {
		t.Fatalf("id=%d err=%v", id, err)
	}
	for _, table := range []string{"properties", "units"} {
		var count int
		if err := database.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("%s: partial or invalid create left %d rows", table, count)
		}
	}
}

func TestUnitNamesAndDatabaseConstraints(t *testing.T) {
	database := testDB(t)
	id, first := create(t, database, "First")
	other, _ := create(t, database, "Other")
	if _, err := property.AddUnit(t.Context(), database, id, " Whole home "); !errors.Is(err, property.ErrDuplicateUnitName) {
		t.Fatal(err)
	}
	if _, err := property.AddUnit(t.Context(), database, other, "Second"); err != nil {
		t.Fatal(err)
	}
	second, err := property.AddUnit(t.Context(), database, id, " Second ")
	if err != nil {
		t.Fatal(err)
	}
	if err := property.RenameUnit(t.Context(), database, second, "Whole home"); !errors.Is(err, property.ErrDuplicateUnitName) {
		t.Fatal(err)
	}
	if err := property.RenameUnit(t.Context(), database, second, " \n "); !errors.Is(err, property.ErrUnitName) {
		t.Fatal(err)
	}
	if err := property.RenameUnit(t.Context(), database, second, " Guest suite "); err != nil {
		t.Fatal(err)
	}
	unit, err := property.GetUnit(t.Context(), database, second)
	if err != nil || unit.Name != "Guest suite" {
		t.Fatalf("unit=%+v err=%v", unit, err)
	}
	if err := property.SetUnitArchived(t.Context(), database, second, true); err != nil {
		t.Fatal(err)
	}
	if _, err := property.AddUnit(t.Context(), database, id, "Guest suite"); !errors.Is(err, property.ErrDuplicateUnitName) {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE units SET ical_export_token = ? WHERE id = ?`, first.IcalExportToken, second); err == nil {
		t.Fatal("duplicate token accepted")
	}
	if _, err := property.AddUnit(t.Context(), database, -1, "Unknown parent"); err == nil {
		t.Fatal("foreign key accepted")
	}
	if _, err := property.AddUnit(t.Context(), database, id, " "); !errors.Is(err, property.ErrUnitName) {
		t.Fatal(err)
	}
}

func TestArchiveInvariantAndConcurrentRequests(t *testing.T) {
	database := testDB(t)
	id, first := create(t, database, "Home")
	if err := property.SetUnitArchived(t.Context(), database, first.ID, true); !errors.Is(err, property.ErrLastActiveUnit) {
		t.Fatal(err)
	}
	second, err := property.AddUnit(t.Context(), database, id, "Second")
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, unitID := range []int64{first.ID, second} {
		go func() { <-start; results <- property.SetUnitArchived(t.Context(), database, unitID, true) }()
	}
	close(start)
	var success, rejected int
	for range 2 {
		switch err := <-results; {
		case err == nil:
			success++
		case errors.Is(err, property.ErrLastActiveUnit):
			rejected++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatalf("success=%d rejected=%d", success, rejected)
	}
	active, err := property.ListUnits(t.Context(), database, id, false)
	if err != nil || len(active) != 1 {
		t.Fatalf("active=%v err=%v", active, err)
	}
	all, err := property.ListUnits(t.Context(), database, id, true)
	if err != nil || len(all) != 2 {
		t.Fatalf("all=%v err=%v", all, err)
	}
	for _, unit := range all {
		if unit.ArchivedAt == "" {
			continue
		}
		if err := property.SetUnitArchived(t.Context(), database, unit.ID, true); err != nil {
			t.Fatal(err)
		}
		repeated, err := property.GetUnit(t.Context(), database, unit.ID)
		if err != nil || repeated.ArchivedAt != unit.ArchivedAt {
			t.Fatal("repeated archive changed timestamp", err)
		}
		if err := property.SetUnitArchived(t.Context(), database, unit.ID, false); err != nil {
			t.Fatal(err)
		}
	}
	active, err = property.ListUnits(t.Context(), database, id, false)
	if err != nil || len(active) != 2 {
		t.Fatalf("restore: %v %v", active, err)
	}
}

func TestPropertyEditArchiveAndRestore(t *testing.T) {
	database := testDB(t)
	id, unit := create(t, database, "Home")
	if err := property.SetArchived(t.Context(), database, id, true); err != nil {
		t.Fatal(err)
	}
	archived, err := property.Get(t.Context(), database, id)
	if err != nil || archived.ArchivedAt == "" {
		t.Fatalf("archive: %+v %v", archived, err)
	}
	if err := property.Update(t.Context(), database, id, property.Details{Name: " \t "}); !errors.Is(err, property.ErrName) {
		t.Fatal(err)
	}
	if err := property.Update(t.Context(), database, id, property.Details{Name: " Updated ", Address: "2 Main St", Notes: "New notes"}); err != nil {
		t.Fatal(err)
	}
	if err := property.SetArchived(t.Context(), database, id, true); err != nil {
		t.Fatal(err)
	}
	updated, err := property.Get(t.Context(), database, id)
	if err != nil || updated.Name != "Updated" || updated.Address != "2 Main St" || updated.Notes != "New notes" || updated.ArchivedAt != archived.ArchivedAt {
		t.Fatalf("update: %+v %v", updated, err)
	}
	for _, include := range []bool{false, true} {
		values, err := property.List(t.Context(), database, include)
		if err != nil {
			t.Fatal(err)
		}
		if (include && len(values) != 1) || (!include && len(values) != 0) {
			t.Fatalf("include=%v values=%v", include, values)
		}
	}
	if err := property.SetUnitArchived(t.Context(), database, unit.ID, true); !errors.Is(err, property.ErrLastActiveUnit) {
		t.Fatal("archived property lost its final active unit", err)
	}
	if err := property.SetArchived(t.Context(), database, id, false); err != nil {
		t.Fatal(err)
	}
	updated, err = property.Get(t.Context(), database, id)
	if err != nil || updated.ArchivedAt != "" {
		t.Fatalf("restore: %+v %v", updated, err)
	}
	active, err := property.ListUnits(t.Context(), database, id, false)
	if err != nil || len(active) != 1 || active[0].IcalExportToken != unit.IcalExportToken {
		t.Fatalf("property lifecycle changed units: %v %v", active, err)
	}
}

func TestTokenRegenerationAndUniqueness(t *testing.T) {
	database := testDB(t)
	id, first := create(t, database, "Home")
	secondID, err := property.AddUnit(t.Context(), database, id, "Second")
	if err != nil {
		t.Fatal(err)
	}
	second, err := property.GetUnit(t.Context(), database, secondID)
	if err != nil {
		t.Fatal(err)
	}
	token, err := property.RegenerateToken(t.Context(), database, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(bytes) != 32 || token == first.IcalExportToken || token == second.IcalExportToken || first.IcalExportToken == second.IcalExportToken {
		t.Fatal("invalid or duplicated token", err)
	}
	var oldMatches, newMatches int
	if err := database.QueryRow(`SELECT count(*) FROM units WHERE ical_export_token = ?`, first.IcalExportToken).Scan(&oldMatches); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT count(*) FROM units WHERE ical_export_token = ?`, token).Scan(&newMatches); err != nil {
		t.Fatal(err)
	}
	if oldMatches != 0 || newMatches != 1 {
		t.Fatalf("old matches=%d new matches=%d", oldMatches, newMatches)
	}
	saved, err := property.GetUnit(t.Context(), database, first.ID)
	if err != nil || saved.IcalExportToken != token {
		t.Fatal("token was not persisted", err)
	}
}

func TestMissingRecords(t *testing.T) {
	database := testDB(t)
	for _, action := range []func() error{
		func() error { _, err := property.Get(t.Context(), database, 99); return err },
		func() error { _, err := property.GetUnit(t.Context(), database, 99); return err },
		func() error { return property.Update(t.Context(), database, 99, property.Details{Name: "Missing"}) },
		func() error { return property.SetArchived(t.Context(), database, 99, true) },
		func() error { return property.RenameUnit(t.Context(), database, 99, "Missing") },
		func() error { return property.SetUnitArchived(t.Context(), database, 99, true) },
		func() error { _, err := property.RegenerateToken(t.Context(), database, 99); return err },
	} {
		if err := action(); !errors.Is(err, property.ErrNotFound) {
			t.Fatal(err)
		}
	}
}
