package occupancy_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/db"
	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/lease"
	"github.com/gaw008/wingmanager/internal/occupancy"
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/settings"
)

func TestCalendarTurnoverConflictsClippingLeasesAndSettingsToday(t *testing.T) {
	ctx := t.Context()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	database, err := db.Open(filepath.Join(t.TempDir(), "calendar.db"))
	must(err)
	t.Cleanup(func() { must(database.Close()) })
	must(db.Migrate(ctx, database))
	propertyID, err := property.Create(ctx, database, property.Details{Name: "House"}, "Turnover")
	must(err)
	units, err := property.ListUnits(ctx, database, propertyID, false)
	must(err)
	unitID := units[0].ID
	conflictUnit, err := property.AddUnit(ctx, database, propertyID, "Conflicts")
	must(err)
	leaseUnit, err := property.AddUnit(ctx, database, propertyID, "Lease")
	must(err)
	emptyUnit, err := property.AddUnit(ctx, database, propertyID, "Vacant")
	must(err)
	draftUnit, err := property.AddUnit(ctx, database, propertyID, "Draft only")
	must(err)
	create := func(from, to, kind string) int64 {
		t.Helper()
		details := booking.Details{UnitID: unitID, Kind: kind, Channel: booking.ChannelDirect, CheckIn: from, CheckOut: to}
		if kind == booking.KindStay {
			details.NewGuest = &person.Details{Name: "Emily Chen"}
		}
		id, err := booking.Create(ctx, database, details)
		must(err)
		return id
	}
	left := create("2026-09-29", "2026-10-03", booking.KindStay)
	turnover := create("2026-10-03", "2026-10-06", booking.KindStay)
	todayOut := create("2026-10-06", "2026-10-20", booking.KindStay)
	right := create("2026-10-30", "2026-11-04", booking.KindBlock)
	feedID, err := ical.Add(ctx, database, conflictUnit, booking.ChannelAirbnb, "https://example.com/calendar")
	must(err)
	config, err := settings.Get(ctx, database)
	must(err)
	config.Timezone = "America/Los_Angeles"
	must(settings.Save(ctx, database, config))
	// UTC is already the next day; past flags must use the settings date.
	today, err := settings.Today(ctx, database, func() time.Time { return time.Date(2026, 10, 21, 1, 0, 0, 0, time.UTC) })
	must(err)
	if today.String() != "2026-10-20" {
		t.Fatal(today)
	}
	tx, err := database.BeginTx(ctx, nil)
	must(err)
	must(booking.ApplySnapshot(ctx, tx, feedID, conflictUnit, booking.ChannelAirbnb, []booking.ImportedEvent{
		{UID: "first", Kind: booking.KindStay, CheckIn: "2026-10-22", CheckOut: "2026-10-25"},
		{UID: "second", Kind: booking.KindStay, CheckIn: "2026-10-24", CheckOut: "2026-10-27"},
	}, today))
	must(tx.Commit())
	leaseID, err := lease.Create(ctx, database, lease.Details{UnitID: leaseUnit, StartsOn: "2026-08-01", EndsOn: "2026-11-30", RentCents: 225000, DueDay: 1})
	must(err)
	must(lease.AddTenant(ctx, database, leaseID, 0, &person.Details{Name: "Tenant"}))
	must(lease.Activate(ctx, database, leaseID))
	_, err = lease.Create(ctx, database, lease.Details{UnitID: draftUnit, StartsOn: "2026-08-01", RentCents: 100000, DueDay: 1})
	must(err)
	view, err := occupancy.Calendar(ctx, database, today, today, 0)
	must(err)
	if view.Month != "2026-10" || view.PreviousMonth != "2026-09" || view.NextMonth != "2026-11" || view.HalfDays != 62 || len(view.Days) != 31 || !view.Days[19].Today || !view.Days[18].Past || view.Days[20].Past {
		t.Fatalf("month/day model: %+v", view)
	}
	if len(view.Properties) != 1 || view.ConflictCount != 1 || view.FirstConflictURL == "" {
		t.Fatalf("properties/conflicts: %+v", view)
	}
	byID := make(map[int64]occupancy.CalendarUnit)
	for _, u := range view.Properties[0].Units {
		byID[u.ID] = u
	}
	bars := make(map[int64]occupancy.CalendarBar)
	for _, bar := range byID[unitID].Bars {
		bars[bar.ID] = bar
	}
	if bars[left].EndColumn != bars[turnover].StartColumn || bars[left].Lane != bars[turnover].Lane || bars[left].Conflict || bars[turnover].Conflict {
		t.Fatalf("same-day turnover: %+v %+v", bars[left], bars[turnover])
	}
	if bars[left].StartColumn != 2 || !bars[left].CutLeft || bars[left].CutRight || !bars[left].Past || bars[left].Initials != "EC" {
		t.Fatalf("left-clipped past bar: %+v", bars[left])
	}
	if bars[right].EndColumn != 64 || !bars[right].CutRight || bars[right].CutLeft || bars[right].Past || bars[right].Kind != booking.KindBlock {
		t.Fatalf("right-clipped block: %+v", bars[right])
	}
	if bars[todayOut].Past {
		t.Fatalf("checkout today must remain current: %+v", bars[todayOut])
	}
	conflicts := byID[conflictUnit]
	if len(conflicts.Bars) != 2 || conflicts.Lanes != 2 || conflicts.Bars[0].Lane == conflicts.Bars[1].Lane || !conflicts.Bars[0].Conflict || !conflicts.Bars[1].Conflict {
		t.Fatalf("overlapping imported bars: %+v", conflicts)
	}
	leases := byID[leaseUnit]
	if len(leases.Bars) != 1 || !leases.HasLease {
		t.Fatalf("lease bars: %+v", leases)
	}
	bar := leases.Bars[0]
	if bar.Kind != "lease" || !bar.CutLeft || !bar.CutRight || bar.StartColumn != 2 || bar.EndColumn != 64 || bar.GuestName != "Tenant" || bar.RentCents != 225000 {
		t.Fatalf("lease view: %+v", bar)
	}
	if byID[emptyUnit].Lanes != 1 || len(byID[emptyUnit].Bars) != 0 || len(byID[draftUnit].Bars) != 0 {
		t.Fatalf("empty/draft rows: %+v %+v", byID[emptyUnit], byID[draftUnit])
	}
	filtered, err := occupancy.Calendar(ctx, database, today, today, propertyID+1)
	must(err)
	if len(filtered.Properties) != 0 || filtered.ConflictCount != 0 {
		t.Fatalf("property filter: %+v", filtered)
	}
}

func TestCalendarMonthBoundaryMorningAndInclusiveLeaseEnd(t *testing.T) {
	ctx := t.Context()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	database, err := db.Open(filepath.Join(t.TempDir(), "boundary.db"))
	must(err)
	t.Cleanup(func() { must(database.Close()) })
	must(db.Migrate(ctx, database))
	propertyID, err := property.Create(ctx, database, property.Details{Name: "Boundaries"}, "Stay")
	must(err)
	units, err := property.ListUnits(ctx, database, propertyID, false)
	must(err)
	leaseUnit, err := property.AddUnit(ctx, database, propertyID, "Lease ended before month")
	must(err)
	for _, dates := range [][2]string{{"2026-09-29", "2026-10-01"}, {"2026-10-01", "2026-10-03"}} {
		_, err := booking.Create(ctx, database, booking.Details{UnitID: units[0].ID, Kind: booking.KindStay, Channel: booking.ChannelDirect, CheckIn: dates[0], CheckOut: dates[1]})
		must(err)
	}
	leaseID, err := lease.Create(ctx, database, lease.Details{UnitID: leaseUnit, StartsOn: "2026-09-01", EndsOn: "2026-09-30", RentCents: 100000, DueDay: 1})
	must(err)
	must(lease.AddTenant(ctx, database, leaseID, 0, &person.Details{Name: "Tenant"}))
	must(lease.Activate(ctx, database, leaseID))
	today, err := settings.Today(ctx, database, func() time.Time { return time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC) })
	must(err)
	view, err := occupancy.Calendar(ctx, database, today, today, propertyID)
	must(err)
	for _, u := range view.Properties[0].Units {
		switch u.ID {
		case units[0].ID:
			if len(u.Bars) != 2 {
				t.Fatalf("checkout-morning bar missing: %+v", u)
			}
			left, right := u.Bars[0], u.Bars[1]
			if left.StartColumn != 2 || left.EndColumn != 3 || !left.CutLeft || left.EndColumn != right.StartColumn || left.Lane != right.Lane || left.Conflict || right.Conflict {
				t.Fatalf("month boundary turnover: %+v, %+v", left, right)
			}
		case leaseUnit:
			if len(u.Bars) != 0 {
				t.Fatalf("lease ending before the month must not paint day one: %+v", u)
			}
		}
	}
}
