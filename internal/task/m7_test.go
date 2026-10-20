package task_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/civildate"
	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/task"
)

func TestManualTaskLifecycleAndCleaningRestrictions(t *testing.T) {
	database, unitID := setup(t)
	unit, err := property.GetUnit(t.Context(), database, unitID)
	if err != nil {
		t.Fatal(err)
	}
	details := task.Details{PropertyID: unit.PropertyID, UnitID: unitID, Kind: task.KindMaintenance, Title: "  Repair sink  ", DueOn: "2026-10-31", Assignee: "Pat", Notes: "Leak"}
	id, err := task.Create(t.Context(), database, details)
	if err != nil {
		t.Fatal(err)
	}
	value, err := task.Get(t.Context(), database, id)
	if err != nil || value.Title != "Repair sink" || value.Status != task.StatusOpen || value.DoneAt != "" || value.UnitID != unitID {
		t.Fatalf("created=%+v err=%v", value, err)
	}
	for _, status := range []string{task.StatusDone, task.StatusDone, task.StatusOpen, task.StatusCancelled, task.StatusOpen, task.StatusDone} {
		before := value
		if err := task.SetStatus(t.Context(), database, id, status); err != nil {
			t.Fatal(err)
		}
		value, err = task.Get(t.Context(), database, id)
		if err != nil || value.Status != status || (value.DoneAt != "") != (status == task.StatusDone) {
			t.Fatalf("status=%s value=%+v err=%v", status, value, err)
		}
		if status == task.StatusDone && before.Status == task.StatusDone && value.DoneAt != before.DoneAt {
			t.Fatal("duplicate completion changed timestamp")
		}
	}
	edit := task.Edit{Title: "Replace sink", DueOn: "", Assignee: "Alex", Notes: "Ordered"}
	if err := task.Update(t.Context(), database, id, edit); err != nil {
		t.Fatal(err)
	}
	value, err = task.Get(t.Context(), database, id)
	if err != nil || value.Title != edit.Title || value.DueOn != "" || value.Assignee != edit.Assignee || value.Notes != edit.Notes || value.Status != task.StatusDone || value.DoneAt == "" {
		t.Fatalf("edited=%+v err=%v", value, err)
	}
	details.Kind = task.KindCleaning
	if _, err := task.Create(t.Context(), database, details); !errors.Is(err, task.ErrCleaning) {
		t.Fatalf("manual cleaning err=%v", err)
	}
	bookingID, err := booking.Create(t.Context(), database, booking.Details{UnitID: unitID, Kind: booking.KindStay, Channel: booking.ChannelDirect, CheckIn: "2026-11-01", CheckOut: "2026-11-03"})
	if err != nil {
		t.Fatal(err)
	}
	clean := cleaning(t, database, bookingID)
	for _, edit := range []task.Edit{{Title: "Changed", DueOn: clean.DueOn}, {Title: clean.Title, DueOn: "2026-11-04"}} {
		if err := task.Update(t.Context(), database, clean.ID, edit); !errors.Is(err, task.ErrCleaning) {
			t.Fatalf("cleaning edit err=%v", err)
		}
	}
	if err := task.SetStatus(t.Context(), database, clean.ID, task.StatusCancelled); !errors.Is(err, task.ErrCleaning) {
		t.Fatalf("manual cleaning cancellation err=%v", err)
	}
	if err := task.Update(t.Context(), database, clean.ID, task.Edit{Title: clean.Title, DueOn: clean.DueOn, Assignee: "Cleaner", Notes: "Leave keys"}); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{task.StatusDone, task.StatusOpen} {
		if err := task.SetStatus(t.Context(), database, clean.ID, status); err != nil {
			t.Fatal(err)
		}
		updated := cleaning(t, database, bookingID)
		if updated.Status != status || (updated.DoneAt != "") != (status == task.StatusDone) || updated.Assignee != "Cleaner" || updated.Notes != "Leave keys" || updated.DueOn != clean.DueOn {
			t.Fatalf("cleaning lifecycle=%+v", updated)
		}
	}
	if err := booking.SetCancelled(t.Context(), database, bookingID, true); err != nil {
		t.Fatal(err)
	}
	if clean := cleaning(t, database, bookingID); clean.Status != task.StatusCancelled {
		t.Fatalf("booking did not cancel cleaning: %+v", clean)
	}
}

func TestTaskValidationAndNoPartialWrites(t *testing.T) {
	database, unitID := setup(t)
	unit, err := property.GetUnit(t.Context(), database, unitID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := property.Create(t.Context(), database, property.Details{Name: "Other"}, "Unit")
	if err != nil {
		t.Fatal(err)
	}
	base := task.Details{PropertyID: unit.PropertyID, UnitID: unitID, Kind: task.KindOther, Title: "Call contractor"}
	for _, test := range []struct {
		name   string
		change func(*task.Details)
	}{
		{"missing title", func(v *task.Details) { v.Title = " " }},
		{"invalid date", func(v *task.Details) { v.DueOn = "2026-02-30" }},
		{"bad kind", func(v *task.Details) { v.Kind = "invalid" }},
		{"mismatched unit", func(v *task.Details) { v.PropertyID = other }},
		{"missing property", func(v *task.Details) { v.PropertyID = 999 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := base
			test.change(&v)
			if _, err := task.Create(t.Context(), database, v); !errors.Is(err, task.ErrInvalid) {
				t.Fatalf("err=%v", err)
			}
		})
	}
	values, err := task.List(t.Context(), database, task.Filter{})
	if err != nil || len(values) != 0 {
		t.Fatalf("partial create=%+v err=%v", values, err)
	}
	id, err := task.Create(t.Context(), database, base)
	if err != nil {
		t.Fatal(err)
	}
	if err := task.Update(t.Context(), database, id, task.Edit{Title: "", Notes: "Bad"}); !errors.Is(err, task.ErrInvalid) {
		t.Fatalf("invalid edit=%v", err)
	}
	if err := task.SetStatus(t.Context(), database, id, "canceled"); !errors.Is(err, task.ErrInvalid) {
		t.Fatalf("invalid status=%v", err)
	}
	if err := task.SetStatus(t.Context(), database, 999, task.StatusDone); !errors.Is(err, task.ErrNotFound) {
		t.Fatalf("missing task=%v", err)
	}
	if _, err := database.Exec(`CREATE TRIGGER reject_task_edit BEFORE UPDATE ON tasks BEGIN SELECT RAISE(ABORT,'forced task failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := task.SetStatus(t.Context(), database, id, task.StatusDone); err == nil {
		t.Fatal("ignored write failure")
	}
	value, err := task.Get(t.Context(), database, id)
	if err != nil || value.Status != task.StatusOpen || value.DoneAt != "" || value.Title != base.Title {
		t.Fatalf("partial edit=%+v err=%v", value, err)
	}
}

func TestTaskFiltersAndCalendarWeek(t *testing.T) {
	database, unitID := setup(t)
	unit, err := property.GetUnit(t.Context(), database, unitID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := property.Create(t.Context(), database, property.Details{Name: "Other"}, "Unit")
	if err != nil {
		t.Fatal(err)
	}
	dates := []string{"2026-10-25", "2026-10-26", "2026-10-30", "2026-10-31", "2026-11-01", "2026-11-02", ""}
	var ids []int64
	for i, date := range dates {
		id, err := task.Create(t.Context(), database, task.Details{PropertyID: unit.PropertyID, Kind: task.KindMaintenance, Title: fmt.Sprint(i), DueOn: date})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	id, err := task.Create(t.Context(), database, task.Details{PropertyID: other, Kind: task.KindOther, Title: "Other", DueOn: "2026-10-30"})
	if err != nil {
		t.Fatal(err)
	}
	if err := task.SetStatus(t.Context(), database, id, task.StatusDone); err != nil {
		t.Fatal(err)
	}
	today, err := civildate.Parse("2026-10-31")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		filter task.Filter
		want   []int64
	}{
		{task.Filter{PropertyID: unit.PropertyID, Due: task.DueOverdue, Today: today}, ids[:3]},
		{task.Filter{PropertyID: unit.PropertyID, Due: task.DueThroughToday, Today: today}, ids[:4]},
		{task.Filter{PropertyID: unit.PropertyID, Due: task.DueWeek, Today: today}, ids[1:5]},
		{task.Filter{Kind: task.KindOther, Status: task.StatusDone}, []int64{id}},
		{task.Filter{Kind: task.KindOther, Status: task.StatusOpen}, []int64{}},
		{task.Filter{PropertyID: unit.PropertyID, Kind: task.KindMaintenance}, ids},
	} {
		values, err := task.List(t.Context(), database, test.filter)
		if err != nil {
			t.Fatal(err)
		}
		var got []int64
		for _, v := range values {
			got = append(got, v.ID)
		}
		if !slices.Equal(got, test.want) {
			t.Fatalf("filter=%+v got=%v want=%v", test.filter, got, test.want)
		}
	}
	for _, filter := range []task.Filter{{Due: task.DueWeek}, {Due: "invalid", Today: today}, {Kind: "bad"}, {Status: "bad"}, {PropertyID: -1}} {
		if _, err := task.List(t.Context(), database, filter); !errors.Is(err, task.ErrInvalid) {
			t.Fatalf("filter=%+v err=%v", filter, err)
		}
	}
}
