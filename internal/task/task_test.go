package task_test

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/db"
	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/task"
)

func setup(t *testing.T) (*sql.DB, int64) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "tasks.db"))
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
	p, err := property.Create(t.Context(), database, property.Details{Name: "Home"}, "Whole home")
	if err != nil {
		t.Fatal(err)
	}
	units, err := property.ListUnits(t.Context(), database, p, false)
	if err != nil || len(units) != 1 {
		t.Fatalf("units=%v err=%v", units, err)
	}
	return database, units[0].ID
}
func cleaning(t *testing.T, database *sql.DB, id int64) *task.Task {
	t.Helper()
	value, err := task.CleaningForBooking(t.Context(), database, id)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestManualStayCleaningLifecycleAndCompletedHistory(t *testing.T) {
	database, unit := setup(t)
	value := booking.Details{UnitID: unit, Kind: booking.KindStay, Channel: booking.ChannelDirect, CheckIn: "2026-11-01", CheckOut: "2026-11-04"}
	id, err := booking.Create(t.Context(), database, value)
	if err != nil {
		t.Fatal(err)
	}
	initial := cleaning(t, database, id)
	if initial == nil || initial.Status != task.StatusOpen || initial.DueOn.String != value.CheckOut || initial.Title != "" {
		t.Fatalf("task=%+v", initial)
	}
	for range 2 {
		if err := booking.SetCancelled(t.Context(), database, id, true); err != nil {
			t.Fatal(err)
		}
	}
	if c := cleaning(t, database, id); c.Status != task.StatusCancelled || c.ID != initial.ID {
		t.Fatalf("cancelled=%+v", c)
	}
	value.CheckOut = "2026-11-05"
	if err := booking.Update(t.Context(), database, id, value); err != nil {
		t.Fatal(err)
	}
	if c := cleaning(t, database, id); c.Status != task.StatusCancelled || c.DueOn.String != value.CheckOut {
		t.Fatalf("cancelled edit=%+v", c)
	}
	for range 2 {
		if err := booking.SetCancelled(t.Context(), database, id, false); err != nil {
			t.Fatal(err)
		}
	}
	value.CheckOut = "2026-11-06"
	if err := booking.Update(t.Context(), database, id, value); err != nil {
		t.Fatal(err)
	}
	if c := cleaning(t, database, id); c.Status != task.StatusOpen || c.DueOn.String != value.CheckOut || c.ID != initial.ID {
		t.Fatalf("restored/edit=%+v", c)
	}
	// Move unfinished work with a manual booking, including to another property.
	p, err := property.Create(t.Context(), database, property.Details{Name: "Other home"}, "Unit")
	if err != nil {
		t.Fatal(err)
	}
	units, err := property.ListUnits(t.Context(), database, p, false)
	if err != nil {
		t.Fatal(err)
	}
	value.UnitID = units[0].ID
	if err := booking.Update(t.Context(), database, id, value); err != nil {
		t.Fatal(err)
	}
	if c := cleaning(t, database, id); c.PropertyID != p || c.UnitID.Int64 != value.UnitID {
		t.Fatalf("moved=%+v", c)
	}
	if _, err := database.Exec(`UPDATE tasks SET status='done',done_at='2026-11-06T12:00:00Z',notes='Completed work',assignee='Cleaner' WHERE id=?`, initial.ID); err != nil {
		t.Fatal(err)
	}
	done := cleaning(t, database, id)
	value.CheckOut = "2026-11-07"
	if err := booking.Update(t.Context(), database, id, value); err != nil {
		t.Fatal(err)
	}
	if err := booking.SetCancelled(t.Context(), database, id, true); err != nil {
		t.Fatal(err)
	}
	if err := booking.SetCancelled(t.Context(), database, id, false); err != nil {
		t.Fatal(err)
	}
	value.Kind = booking.KindBlock
	if err := booking.Update(t.Context(), database, id, value); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(done, cleaning(t, database, id)) {
		t.Fatal("completed work changed")
	}
}

func TestBlocksHaveNoCleaningAndKindChangesUseSameTask(t *testing.T) {
	database, unit := setup(t)
	value := booking.Details{UnitID: unit, Kind: booking.KindBlock, Channel: booking.ChannelDirect, CheckIn: "2026-11-01", CheckOut: "2026-11-04"}
	id, err := booking.Create(t.Context(), database, value)
	if err != nil {
		t.Fatal(err)
	}
	if cleaning(t, database, id) != nil {
		t.Fatal("block created task")
	}
	value.Kind = booking.KindStay
	if err := booking.Update(t.Context(), database, id, value); err != nil {
		t.Fatal(err)
	}
	initial := cleaning(t, database, id)
	value.Kind = booking.KindBlock
	if err := booking.Update(t.Context(), database, id, value); err != nil {
		t.Fatal(err)
	}
	if c := cleaning(t, database, id); c.Status != task.StatusCancelled {
		t.Fatalf("former stay=%+v", c)
	}
	value.Kind = booking.KindStay
	if err := booking.Update(t.Context(), database, id, value); err != nil {
		t.Fatal(err)
	}
	if c := cleaning(t, database, id); c.ID != initial.ID || c.Status != task.StatusOpen {
		t.Fatalf("restored stay=%+v", c)
	}
}

func TestManualWriteRollsBackWhenCleaningFails(t *testing.T) {
	database, unit := setup(t)
	if _, err := database.Exec(`CREATE TRIGGER reject_task BEFORE INSERT ON tasks BEGIN SELECT RAISE(ABORT,'forced task failure'); END`); err != nil {
		t.Fatal(err)
	}
	_, err := booking.Create(t.Context(), database, booking.Details{UnitID: unit, Kind: booking.KindStay, Channel: booking.ChannelDirect, CheckIn: "2026-11-01", CheckOut: "2026-11-04"})
	if err == nil {
		t.Fatal("task failure was ignored")
	}
	values, err := booking.List(t.Context(), database, booking.Filter{})
	if err != nil || len(values) != 0 {
		t.Fatalf("partial manual write: %v %v", values, err)
	}
}

func TestEnumsMatchSchemaAndUniqueCleaning(t *testing.T) {
	database, unit := setup(t)
	var schema string
	if err := database.QueryRow(`SELECT sql FROM sqlite_schema WHERE name='tasks'`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	for column, want := range map[string][]string{"kind": task.Kinds(), "status": task.Statuses()} {
		match := regexp.MustCompile(`\b` + column + ` IN \(([^)]*)\)`).FindStringSubmatch(schema)
		if len(match) != 2 {
			t.Fatal(schema)
		}
		var got []string
		for _, v := range strings.Split(match[1], ",") {
			got = append(got, strings.Trim(strings.TrimSpace(v), "'"))
		}
		if !slices.Equal(got, want) {
			t.Fatalf("%s schema=%v Go=%v", column, got, want)
		}
	}
	id, err := booking.Create(t.Context(), database, booking.Details{UnitID: unit, Kind: booking.KindStay, Channel: booking.ChannelDirect, CheckIn: "2026-11-01", CheckOut: "2026-11-04"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO tasks(property_id,unit_id,booking_id,kind,title,status) SELECT property_id,unit_id,booking_id,kind,title,status FROM tasks WHERE booking_id=?`, id); err == nil {
		t.Fatal("duplicate cleaning accepted")
	}
}

func TestManualEditAndCancelRollBackWhenCleaningUpdateFails(t *testing.T) {
	database, unit := setup(t)
	value := booking.Details{UnitID: unit, Kind: booking.KindStay, Channel: booking.ChannelDirect, CheckIn: "2026-11-01", CheckOut: "2026-11-04"}
	id, err := booking.Create(t.Context(), database, value)
	if err != nil {
		t.Fatal(err)
	}
	before, err := booking.Get(t.Context(), database, id)
	if err != nil {
		t.Fatal(err)
	}
	oldTask := cleaning(t, database, id)
	if _, err := database.Exec(`CREATE TRIGGER reject_cleaning_update BEFORE UPDATE ON tasks BEGIN SELECT RAISE(ABORT,'forced cleaning update failure'); END`); err != nil {
		t.Fatal(err)
	}
	value.CheckOut = "2026-11-05"
	if err := booking.Update(t.Context(), database, id, value); err == nil {
		t.Fatal("failed cleaning did not abort date edit")
	}
	if err := booking.SetCancelled(t.Context(), database, id, true); err == nil {
		t.Fatal("failed cleaning did not abort cancellation")
	}
	after, err := booking.Get(t.Context(), database, id)
	if err != nil || after != before || !reflect.DeepEqual(oldTask, cleaning(t, database, id)) {
		t.Fatalf("partial manual write: %+v %v", after, err)
	}
}
