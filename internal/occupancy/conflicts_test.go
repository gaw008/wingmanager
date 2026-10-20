package occupancy_test

import (
	"path/filepath"
	"testing"

	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/civildate"
	"github.com/gaw008/wingmanager/internal/db"
	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/lease"
	"github.com/gaw008/wingmanager/internal/occupancy"
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
)

func TestFutureConflictsRespectLeaseInclusiveEndAndOpenEnd(t *testing.T) {
	ctx := t.Context()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	database, err := db.Open(filepath.Join(t.TempDir(), "occupancy.db"))
	must(err)
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	must(db.Migrate(ctx, database))
	propertyID, err := property.Create(ctx, database, property.Details{Name: "House"}, "Lease ends today")
	must(err)
	units, err := property.ListUnits(ctx, database, propertyID, false)
	must(err)
	openUnit, err := property.AddUnit(ctx, database, propertyID, "Open lease")
	must(err)
	today, err := civildate.Parse("2026-10-31")
	must(err)
	for _, value := range []struct {
		unitID int64
		endsOn string
	}{{units[0].ID, "2026-10-31"}, {openUnit, ""}} {
		leaseID, err := lease.Create(ctx, database, lease.Details{UnitID: value.unitID, StartsOn: "2026-10-01", EndsOn: value.endsOn, RentCents: 100000, DueDay: 5})
		must(err)
		must(lease.AddTenant(ctx, database, leaseID, 0, &person.Details{Name: "Tenant"}))
		must(lease.Activate(ctx, database, leaseID))
		feedID, err := ical.Add(ctx, database, value.unitID, booking.ChannelOther, "https://example.com/calendar")
		must(err)
		events := []booking.ImportedEvent{
			{UID: "arrival-today", Kind: booking.KindStay, CheckIn: "2026-10-31", CheckOut: "2026-11-01"},
			{UID: "next-day-block", Kind: booking.KindBlock, CheckIn: "2026-11-01", CheckOut: "2026-11-02"},
			{UID: "past-first", Kind: booking.KindStay, CheckIn: "2026-08-01", CheckOut: "2026-08-03"},
			{UID: "past-second", Kind: booking.KindStay, CheckIn: "2026-08-02", CheckOut: "2026-08-04"},
		}
		tx, err := database.BeginTx(ctx, nil)
		must(err)
		must(booking.ApplySnapshot(ctx, tx, feedID, value.unitID, booking.ChannelOther, events, today))
		must(tx.Commit())
	}
	conflicts, err := occupancy.FutureConflicts(ctx, database, today)
	must(err)
	// The ended-today lease overlaps today's arrival only. The open lease also
	// overlaps tomorrow's block; same-day stay turnover never creates a fourth pair.
	if len(conflicts) != 3 {
		t.Fatalf("conflicts: %+v", conflicts)
	}
	for _, value := range conflicts {
		if value.First.Source != "lease" || value.Second.Source != "booking" || value.Second.StartsOn < today.String() {
			t.Fatalf("unexpected pair: %+v", value)
		}
		if value.UnitName == "Lease ends today" && value.Second.StartsOn != today.String() {
			t.Fatalf("lease endpoint excluded incorrectly: %+v", value)
		}
	}
	conflicts, err = occupancy.FutureConflicts(ctx, database, today.AddDays(2))
	must(err)
	if len(conflicts) != 0 {
		t.Fatalf("past conflicts remain: %+v", conflicts)
	}
}
