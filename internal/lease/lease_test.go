package lease_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/civildate"
	"github.com/gaw008/wingmanager/internal/db"
	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/lease"
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
)

func setup(t *testing.T) (*sql.DB, int64) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "lease.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Migrate(t.Context(), d); err != nil {
		t.Fatal(err)
	}
	p, err := property.Create(t.Context(), d, property.Details{Name: "Home"}, "Unit")
	if err != nil {
		t.Fatal(err)
	}
	units, err := property.ListUnits(t.Context(), d, p, false)
	if err != nil {
		t.Fatal(err)
	}
	return d, units[0].ID
}
func draft(t *testing.T, d *sql.DB, u int64, start, end string) int64 {
	t.Helper()
	id, err := lease.Create(t.Context(), d, lease.Details{UnitID: u, StartsOn: start, EndsOn: end, RentCents: 100000, DueDay: 1, DepositCents: 200000})
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func tenant(t *testing.T, d *sql.DB, id int64) {
	t.Helper()
	if err := lease.AddTenant(t.Context(), d, id, 0, &person.Details{Name: "Tenant"}); err != nil {
		t.Fatal(err)
	}
}
func TestStateEditingAndTenants(t *testing.T) {
	d, u := setup(t)
	id := draft(t, d, u, "2026-01-01", "2026-03-31")
	if err := lease.Activate(t.Context(), d, id); !errors.Is(err, lease.ErrTenant) {
		t.Fatal(err)
	}
	tenant(t, d, id)
	p, err := person.Create(t.Context(), d, person.Details{Name: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.AddTenant(t.Context(), d, id, p, nil); err != nil {
		t.Fatal(err)
	}
	if err := lease.Activate(t.Context(), d, id); err != nil {
		t.Fatal(err)
	}
	if err := lease.RemoveTenant(t.Context(), d, id, p); err != nil {
		t.Fatal(err)
	}
	ts, err := lease.Tenants(t.Context(), d, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.RemoveTenant(t.Context(), d, id, ts[0].ID); !errors.Is(err, lease.ErrTenant) {
		t.Fatal(err)
	}
	v, err := lease.Get(t.Context(), d, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*lease.Details){func(v *lease.Details) { v.StartsOn = "2026-01-02" }, func(v *lease.Details) { v.DueDay = 2 }, func(v *lease.Details) { v.DepositCents++ }, func(v *lease.Details) { v.UnitID++ }} {
		details := v.Details
		change(&details)
		if err := lease.Update(t.Context(), d, id, details); !errors.Is(err, lease.ErrInvalid) {
			t.Fatalf("invalid active edit err=%v", err)
		}
	}
	v.RentCents++
	v.Notes = "Changed"
	if err := lease.Update(t.Context(), d, id, v.Details); err != nil {
		t.Fatal(err)
	}
	if err := lease.End(t.Context(), d, id, "2025-12-31"); !errors.Is(err, lease.ErrInvalid) {
		t.Fatal(err)
	}
	if err := lease.End(t.Context(), d, id, "2026-03-15"); err != nil {
		t.Fatal(err)
	}
	v, err = lease.Get(t.Context(), d, id)
	if err != nil {
		t.Fatal(err)
	}
	v.Notes = "Ended notes"
	if err := lease.Update(t.Context(), d, id, v.Details); err != nil {
		t.Fatal(err)
	}
	v.RentCents++
	if err := lease.Update(t.Context(), d, id, v.Details); !errors.Is(err, lease.ErrInvalid) {
		t.Fatal(err)
	}
	if err := lease.AddTenant(t.Context(), d, id, p, nil); !errors.Is(err, lease.ErrInvalid) {
		t.Fatal(err)
	}
}
func TestOccupancyActivationBookingBoundariesAndExtension(t *testing.T) {
	d, u := setup(t)
	ctx := t.Context()
	createBooking := func(from, to string) error {
		_, err := booking.Create(ctx, d, booking.Details{UnitID: u, Kind: booking.KindStay, Channel: booking.ChannelDirect, CheckIn: from, CheckOut: to})
		return err
	}
	id := draft(t, d, u, "2026-01-01", "2026-01-31")
	tenant(t, d, id)
	if err := createBooking("2025-12-31", "2026-01-01"); err != nil {
		t.Fatal(err)
	}
	if err := lease.Activate(ctx, d, id); err != nil {
		t.Fatal(err)
	}
	var conflict *booking.ConflictError
	if err := createBooking("2026-01-31", "2026-02-02"); !errors.As(err, &conflict) {
		t.Fatalf("lease overlap %v", err)
	}
	if err := createBooking("2026-02-01", "2026-02-03"); err != nil {
		t.Fatal(err)
	}
	v, err := lease.Get(ctx, d, id)
	if err != nil {
		t.Fatal(err)
	}
	v.EndsOn = "2026-02-01"
	if err := lease.Update(ctx, d, id, v.Details); !errors.Is(err, lease.ErrConflict) {
		t.Fatal(err)
	}
	other := draft(t, d, u, "2026-01-20", "2026-02-10")
	tenant(t, d, other)
	if err := lease.Activate(ctx, d, other); !errors.Is(err, lease.ErrConflict) {
		t.Fatal(err)
	}
	b := draft(t, d, u, "2026-02-02", "2026-02-10")
	tenant(t, d, b)
	if err := lease.Activate(ctx, d, b); !errors.Is(err, lease.ErrConflict) {
		t.Fatal(err)
	}
	adjacent := draft(t, d, u, "2026-02-03", "")
	tenant(t, d, adjacent)
	if err := lease.Activate(ctx, d, adjacent); err != nil {
		t.Fatal(err)
	}
}
func TestConcurrentActivationAndBooking(t *testing.T) {
	d, u := setup(t)
	id := draft(t, d, u, "2026-01-01", "")
	tenant(t, d, id)
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); <-start; errs <- lease.Activate(t.Context(), d, id) }()
	go func() {
		defer wg.Done()
		<-start
		_, err := booking.Create(t.Context(), d, booking.Details{UnitID: u, Kind: booking.KindStay, Channel: booking.ChannelDirect, CheckIn: "2026-01-01", CheckOut: "2026-01-02"})
		errs <- err
	}()
	close(start)
	wg.Wait()
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
		} else {
			var conflict *booking.ConflictError
			if !errors.Is(err, lease.ErrConflict) && !errors.As(err, &conflict) {
				t.Fatal(err)
			}
		}
	}
	if success != 1 {
		t.Fatalf("successes=%d", success)
	}
}
func TestStatusesMatchCheck(t *testing.T) {
	d, _ := setup(t)
	var schema string
	if err := d.QueryRow("SELECT sql FROM sqlite_master WHERE name='leases'").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`status IN \(([^)]+)\)`)
	m := re.FindStringSubmatch(schema)
	if len(m) != 2 {
		t.Fatal(schema)
	}
	var values []string
	for _, s := range strings.Split(m[1], ",") {
		values = append(values, strings.Trim(strings.TrimSpace(s), "'"))
	}
	if !reflect.DeepEqual(values, lease.Statuses()) {
		t.Fatalf("%v != %v", values, lease.Statuses())
	}
}

func TestShorteningDoesNotRejectExistingImportedOverlap(t *testing.T) {
	d, u := setup(t)
	id := draft(t, d, u, "2026-01-01", "2026-12-31")
	tenant(t, d, id)
	if err := lease.Activate(t.Context(), d, id); err != nil {
		t.Fatal(err)
	}
	// Imported occupancy remains an external fact even when it conflicts with a lease.
	feedID, err := ical.Add(t.Context(), d, u, booking.ChannelAirbnb, "https://example.test/calendar.ics")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := d.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	today, err := civildate.Parse("2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if err := booking.ApplySnapshot(t.Context(), tx, feedID, u, booking.ChannelAirbnb, []booking.ImportedEvent{{UID: "external-overlap", Kind: booking.KindStay, CheckIn: "2026-01-10", CheckOut: "2026-01-11"}}, today); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	v, err := lease.Get(t.Context(), d, id)
	if err != nil {
		t.Fatal(err)
	}
	v.EndsOn = "2026-11-30"
	if err := lease.Update(t.Context(), d, id, v.Details); err != nil {
		t.Fatalf("shortening must not acquire more occupancy: %v", err)
	}
}

func TestActivationRejectsOnlyActiveLeaseAndAllowsNextDay(t *testing.T) {
	d, u := setup(t)
	first := draft(t, d, u, "2026-01-01", "2026-01-31")
	tenant(t, d, first)
	// Drafts do not occupy dates, so another overlapping draft can be created.
	second := draft(t, d, u, "2026-01-31", "2026-02-28")
	tenant(t, d, second)
	if err := lease.Activate(t.Context(), d, first); err != nil {
		t.Fatal(err)
	}
	if err := lease.Activate(t.Context(), d, second); !errors.Is(err, lease.ErrConflict) {
		t.Fatalf("active lease overlap accepted: %v", err)
	}
	v, err := lease.Get(t.Context(), d, second)
	if err != nil {
		t.Fatal(err)
	}
	v.StartsOn = "2026-02-01"
	if err := lease.Update(t.Context(), d, second, v.Details); err != nil {
		t.Fatal(err)
	}
	if err := lease.Activate(t.Context(), d, second); err != nil {
		t.Fatalf("adjacent lease rejected: %v", err)
	}
}
