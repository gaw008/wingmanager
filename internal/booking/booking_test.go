package booking_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/civildate"
	"github.com/gaw008/wingmanager/internal/db"
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
)

func testDB(t *testing.T) (*sql.DB, string, int64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bookings.db")
	database := openDB(t, path)
	if err := db.Migrate(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	id, err := property.Create(t.Context(), database, property.Details{Name: "Home"}, "Whole home")
	if err != nil {
		t.Fatal(err)
	}
	units, err := property.ListUnits(t.Context(), database, id, false)
	if err != nil || len(units) != 1 {
		t.Fatalf("units=%v err=%v", units, err)
	}
	return database, path, units[0].ID
}

func openDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	database, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	return database
}

func details(unitID int64, checkIn, checkOut string) booking.Details {
	return booking.Details{UnitID: unitID, Kind: booking.KindStay, CheckIn: checkIn, CheckOut: checkOut, Channel: booking.ChannelDirect}
}

func create(t *testing.T, database *sql.DB, value booking.Details) int64 {
	t.Helper()
	id, err := booking.Create(t.Context(), database, value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func requireConflict(t *testing.T, err error, ids ...int64) {
	t.Helper()
	var conflict *booking.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	got := make([]int64, len(conflict.Bookings))
	for i, value := range conflict.Bookings {
		got[i] = value.ID
		if value.Status != booking.StatusConfirmed || value.CheckIn == "" || value.CheckOut == "" {
			t.Fatalf("conflict missing booking details: %+v", value)
		}
	}
	if !slices.Equal(got, ids) {
		t.Fatalf("conflict IDs=%v want=%v", got, ids)
	}
}

func TestSameDayTurnoverAndOverlappingConflict(t *testing.T) {
	database, _, unitID := testDB(t)
	first := details(unitID, "2026-10-20", "2026-10-23")
	first.Notes = "First guest"
	firstID := create(t, database, first)
	turnover := details(unitID, "2026-10-23", "2026-10-24")
	turnover.Kind, turnover.Channel = booking.KindBlock, booking.ChannelAirbnb
	turnoverID := create(t, database, turnover)
	id, err := booking.Create(t.Context(), database, details(unitID, "2026-10-22", "2026-10-24"))
	if id != 0 {
		t.Fatalf("rejected booking returned id=%d", id)
	}
	requireConflict(t, err, firstID, turnoverID)

	unit, err := property.GetUnit(t.Context(), database, unitID)
	if err != nil {
		t.Fatal(err)
	}
	otherUnit, err := property.AddUnit(t.Context(), database, unit.PropertyID, "Other unit")
	if err != nil {
		t.Fatal(err)
	}
	create(t, database, details(otherUnit, "2026-10-20", "2026-10-23"))
	values, err := booking.List(t.Context(), database, booking.Filter{})
	if err != nil || len(values) != 3 {
		t.Fatalf("bookings=%v err=%v", values, err)
	}
	saved, err := booking.Get(t.Context(), database, firstID)
	if err != nil || saved.Notes != first.Notes || saved.Imported || saved.CreatedAt == "" || saved.UpdatedAt == "" {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
}

func TestConcurrentOverlappingCreatesAcrossConnections(t *testing.T) {
	database, path, unitID := testDB(t)
	other := openDB(t, path)
	start := make(chan struct{})
	type result struct {
		id  int64
		err error
	}
	results := make(chan result, 2)
	for _, connection := range []*sql.DB{database, other} {
		go func() {
			<-start
			id, err := booking.Create(t.Context(), connection, details(unitID, "2026-10-20", "2026-10-23"))
			results <- result{id, err}
		}()
	}
	close(start)
	var winner int64
	var loser error
	var successes, conflicts int
	for range 2 {
		value := <-results
		if value.err == nil {
			winner = value.id
			successes++
		} else {
			loser = value.err
			var conflict *booking.ConflictError
			if !errors.As(value.err, &conflict) {
				t.Fatalf("concurrent create failed without a conflict: %v", value.err)
			}
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	requireConflict(t, loser, winner)
	values, err := booking.List(t.Context(), database, booking.Filter{})
	if err != nil || len(values) != 1 || values[0].ID != winner {
		t.Fatalf("bookings=%v err=%v", values, err)
	}
}

func TestCancelFreesDatesAndRestoreRechecks(t *testing.T) {
	database, _, unitID := testDB(t)
	value := details(unitID, "2026-10-20", "2026-10-23")
	first := create(t, database, value)
	for range 2 {
		if err := booking.SetCancelled(t.Context(), database, first, true); err != nil {
			t.Fatal(err)
		}
	}
	second := create(t, database, value)
	requireConflict(t, booking.SetCancelled(t.Context(), database, first, false), second)
	saved, err := booking.Get(t.Context(), database, first)
	if err != nil || saved.Status != booking.StatusCancelled {
		t.Fatalf("rejected restore changed state: %+v %v", saved, err)
	}
	if err := booking.SetCancelled(t.Context(), database, second, true); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := booking.SetCancelled(t.Context(), database, first, false); err != nil {
			t.Fatal(err)
		}
	}
	saved, err = booking.Get(t.Context(), database, first)
	if err != nil || saved.Status != booking.StatusConfirmed {
		t.Fatalf("restored=%+v err=%v", saved, err)
	}
}

func TestEditExcludesSelfAndPreservesStateOnConflict(t *testing.T) {
	database, _, unitID := testDB(t)
	value := details(unitID, "2026-10-20", "2026-10-23")
	id := create(t, database, value)
	value.Notes, value.Channel = "Edited", booking.ChannelVRBO
	if err := booking.Update(t.Context(), database, id, value); err != nil {
		t.Fatalf("self edit rejected: %v", err)
	}
	other := create(t, database, details(unitID, "2026-10-23", "2026-10-25"))
	conflicting := value
	conflicting.CheckOut = "2026-10-24"
	conflicting.Notes = "Must not be saved"
	requireConflict(t, booking.Update(t.Context(), database, id, conflicting), other)
	saved, err := booking.Get(t.Context(), database, id)
	if err != nil || saved.CheckOut != value.CheckOut || saved.Notes != value.Notes || saved.Channel != value.Channel {
		t.Fatalf("rejected edit changed booking: %+v %v", saved, err)
	}
	if err := booking.SetCancelled(t.Context(), database, id, true); err != nil {
		t.Fatal(err)
	}
	if err := booking.Update(t.Context(), database, id, conflicting); err != nil {
		t.Fatalf("cancelled booking should not occupy dates: %v", err)
	}
	saved, err = booking.Get(t.Context(), database, id)
	if err != nil || saved.Status != booking.StatusCancelled || saved.CheckOut != conflicting.CheckOut {
		t.Fatalf("cancelled edit=%+v err=%v", saved, err)
	}
	requireConflict(t, booking.SetCancelled(t.Context(), database, id, false), other)
}

func TestInvalidDatesAndDetails(t *testing.T) {
	database, _, unitID := testDB(t)
	base := details(unitID, "2026-10-20", "2026-10-23")
	for _, test := range []struct {
		name   string
		change func(*booking.Details)
	}{
		{"same date", func(v *booking.Details) { v.CheckOut = v.CheckIn }},
		{"reverse dates", func(v *booking.Details) { v.CheckOut = "2026-10-19" }},
		{"invalid calendar day", func(v *booking.Details) { v.CheckIn = "2026-02-30" }},
		{"noncanonical date", func(v *booking.Details) { v.CheckOut = "2026-10-4" }},
		{"timestamp", func(v *booking.Details) { v.CheckIn = "2026-10-20T00:00:00Z" }},
		{"unknown kind", func(v *booking.Details) { v.Kind = "lease" }},
		{"unknown channel", func(v *booking.Details) { v.Channel = "unknown" }},
		{"block with guest", func(v *booking.Details) {
			v.Kind, v.GuestID = booking.KindBlock, 1
		}},
		{"block with inline guest", func(v *booking.Details) {
			v.Kind, v.NewGuest = booking.KindBlock, &person.Details{Name: "Guest"}
		}},
		{"two guest selections", func(v *booking.Details) {
			v.GuestID, v.NewGuest = 1, &person.Details{Name: "Guest"}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := base
			test.change(&value)
			if id, err := booking.Create(t.Context(), database, value); id != 0 || !errors.Is(err, booking.ErrInvalid) {
				t.Fatalf("id=%d err=%v", id, err)
			}
		})
	}
	values, err := booking.List(t.Context(), database, booking.Filter{})
	if err != nil || len(values) != 0 {
		t.Fatalf("invalid creates wrote bookings: %v %v", values, err)
	}
}

func TestGuestSelectionAndInlineGuestAtomicity(t *testing.T) {
	database, _, unitID := testDB(t)
	value := details(unitID, "2026-10-20", "2026-10-23")
	value.NewGuest = &person.Details{Name: " Guest ", Email: "guest@example.test", Phone: "123", Notes: "Contact notes"}
	id := create(t, database, value)
	saved, err := booking.Get(t.Context(), database, id)
	if err != nil || saved.GuestID == 0 {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	guest, err := person.Get(t.Context(), database, saved.GuestID)
	if err != nil || guest.Name != "Guest" || guest.Email != value.NewGuest.Email || guest.Phone != value.NewGuest.Phone || guest.Notes != value.NewGuest.Notes {
		t.Fatalf("guest=%+v err=%v", guest, err)
	}
	value.CheckIn, value.CheckOut = "2026-10-23", "2026-10-24"
	value.NewGuest, value.GuestID = nil, saved.GuestID
	second := create(t, database, value)
	saved, err = booking.Get(t.Context(), database, second)
	if err != nil || saved.GuestID != guest.ID {
		t.Fatalf("existing guest not linked: %+v %v", saved, err)
	}

	value.NewGuest, value.GuestID = &person.Details{Name: "Must roll back"}, 0
	value.CheckIn, value.CheckOut = "2026-11-01", "2026-11-03"
	if _, err := database.Exec(`CREATE TRIGGER reject_booking BEFORE INSERT ON bookings BEGIN SELECT RAISE(ABORT, 'booking insert failed'); END`); err != nil {
		t.Fatal(err)
	}
	if id, err := booking.Create(t.Context(), database, value); id != 0 || err == nil {
		t.Fatalf("forced failure: id=%d err=%v", id, err)
	}
	people, err := person.List(t.Context(), database, "")
	if err != nil || len(people) != 1 {
		t.Fatalf("booking failure left inline guest: %v %v", people, err)
	}
	if _, err := database.Exec(`CREATE TRIGGER reject_booking_edit BEFORE UPDATE ON bookings BEGIN SELECT RAISE(ABORT, 'booking update failed'); END`); err != nil {
		t.Fatal(err)
	}
	if err := booking.Update(t.Context(), database, second, value); err == nil {
		t.Fatal("forced update unexpectedly succeeded")
	}
	people, err = person.List(t.Context(), database, "")
	if err != nil || len(people) != 1 {
		t.Fatalf("booking edit failure left inline guest: %v %v", people, err)
	}
	saved, err = booking.Get(t.Context(), database, second)
	if err != nil || saved.GuestID != guest.ID || saved.CheckIn != "2026-10-23" {
		t.Fatalf("failed edit changed booking: %+v %v", saved, err)
	}
}

func TestMissingUnitOrGuestIsInvalidAndRollsBackInlineGuest(t *testing.T) {
	database, _, unitID := testDB(t)
	value := details(-1, "2026-10-20", "2026-10-23")
	value.NewGuest = &person.Details{Name: "Guest on missing unit"}
	if id, err := booking.Create(t.Context(), database, value); id != 0 || !errors.Is(err, booking.ErrInvalid) {
		t.Fatalf("missing unit: id=%d err=%v", id, err)
	}
	people, err := person.List(t.Context(), database, "")
	if err != nil || len(people) != 0 {
		t.Fatalf("FK failure left inline guest: %v %v", people, err)
	}
	value.UnitID, value.NewGuest = unitID, nil
	value.GuestID = -1
	if id, err := booking.Create(t.Context(), database, value); id != 0 || !errors.Is(err, booking.ErrInvalid) {
		t.Fatalf("missing guest: id=%d err=%v", id, err)
	}
	value.GuestID = 0
	id := create(t, database, value)
	value.UnitID, value.NewGuest = -1, &person.Details{Name: "Edit on missing unit"}
	if err := booking.Update(t.Context(), database, id, value); !errors.Is(err, booking.ErrInvalid) {
		t.Fatalf("edit with missing unit: %v", err)
	}
	people, err = person.List(t.Context(), database, "")
	if err != nil || len(people) != 0 {
		t.Fatalf("failed edit left inline guest: %v %v", people, err)
	}
	saved, err := booking.Get(t.Context(), database, id)
	if err != nil || saved.UnitID != unitID || saved.GuestID != 0 {
		t.Fatalf("failed edit changed booking: %+v %v", saved, err)
	}
}

func TestMissingBookings(t *testing.T) {
	database, _, unitID := testDB(t)
	for _, action := range []func() error{
		func() error { _, err := booking.Get(t.Context(), database, 99); return err },
		func() error {
			return booking.Update(t.Context(), database, 99, details(unitID, "2026-10-20", "2026-10-23"))
		},
		func() error { return booking.SetCancelled(t.Context(), database, 99, true) },
		func() error { return booking.SetCancelled(t.Context(), database, 99, false) },
	} {
		if err := action(); !errors.Is(err, booking.ErrNotFound) {
			t.Fatal(err)
		}
	}
}

func TestEnumsMatchDatabaseChecks(t *testing.T) {
	database, _, _ := testDB(t)
	var schema string
	if err := database.QueryRow(`SELECT sql FROM sqlite_schema WHERE name = 'bookings'`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	for column, want := range map[string][]string{
		"kind": booking.Kinds(), "status": booking.Statuses(), "channel": booking.Channels(),
	} {
		match := regexp.MustCompile(`\b` + column + ` IN \(([^)]*)\)`).FindStringSubmatch(schema)
		if len(match) != 2 {
			t.Fatalf("missing %s CHECK in %s", column, schema)
		}
		var got []string
		for _, raw := range strings.Split(match[1], ",") {
			got = append(got, strings.Trim(strings.TrimSpace(raw), "'"))
		}
		if !slices.Equal(got, want) {
			t.Fatalf("%s CHECK=%v Go=%v", column, got, want)
		}
	}
}

func TestArchivedUnitRejectsCreateAndMove(t *testing.T) {
	for _, kind := range booking.Kinds() {
		for _, status := range booking.Statuses() {
			t.Run(kind+"/"+status, func(t *testing.T) {
				database, _, unitID := testDB(t)
				unit, err := property.GetUnit(t.Context(), database, unitID)
				if err != nil {
					t.Fatal(err)
				}
				target, err := property.AddUnit(t.Context(), database, unit.PropertyID, "Archive target")
				if err != nil {
					t.Fatal(err)
				}
				value := details(unitID, "2026-10-20", "2026-10-23")
				value.Kind = kind
				id := create(t, database, value)
				if status == booking.StatusCancelled {
					if err := booking.SetCancelled(t.Context(), database, id, true); err != nil {
						t.Fatal(err)
					}
				}
				if err := property.SetUnitArchived(t.Context(), database, target, true); err != nil {
					t.Fatal(err)
				}
				value.UnitID = target
				if kind == booking.KindStay {
					value.NewGuest = &person.Details{Name: "Must not be created"}
				}
				for range 2 {
					created, err := booking.Create(t.Context(), database, value)
					if created != 0 || !errors.Is(err, booking.ErrInvalid) || !strings.Contains(err.Error(), "archived unit") {
						t.Fatalf("archived create: id=%d err=%v", created, err)
					}
					if err := booking.Update(t.Context(), database, id, value); !errors.Is(err, booking.ErrInvalid) || !strings.Contains(err.Error(), "archived unit") {
						t.Fatalf("archived move: %v", err)
					}
				}
				saved, err := booking.Get(t.Context(), database, id)
				if err != nil || saved.UnitID != unitID || saved.Status != status {
					t.Fatalf("rejected move changed booking: %+v %v", saved, err)
				}
				people, err := person.List(t.Context(), database, "")
				if err != nil || len(people) != 0 {
					t.Fatalf("rejected write left guest: %v %v", people, err)
				}
			})
		}
	}
}

func TestListFiltersUseCallerToday(t *testing.T) {
	database, _, unitID := testDB(t)
	unit, err := property.GetUnit(t.Context(), database, unitID)
	if err != nil {
		t.Fatal(err)
	}
	otherUnit, err := property.AddUnit(t.Context(), database, unit.PropertyID, "Other unit")
	if err != nil {
		t.Fatal(err)
	}
	// Insert out of date order to verify List preserves check-in and ID ordering.
	future := create(t, database, details(unitID, "2026-11-02", "2026-11-04"))
	past := create(t, database, details(unitID, "2026-10-29", "2026-10-30"))
	checkoutToday := create(t, database, details(unitID, "2026-10-30", "2026-10-31"))
	ongoing := create(t, database, details(unitID, "2026-10-31", "2026-11-02"))
	cancelled := create(t, database, details(otherUnit, "2026-10-31", "2026-11-02"))
	if err := booking.SetCancelled(t.Context(), database, cancelled, true); err != nil {
		t.Fatal(err)
	}
	blockDetails := details(otherUnit, "2026-10-31", "2026-11-02")
	blockDetails.Kind = booking.KindBlock
	block := create(t, database, blockDetails)
	today, err := civildate.Parse("2026-10-31")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		filter booking.Filter
		want   []int64
	}{
		{"all", booking.Filter{}, []int64{past, checkoutToday, ongoing, cancelled, block, future}},
		{"past includes checkout today", booking.Filter{Period: booking.PeriodPast, Today: today}, []int64{past, checkoutToday}},
		{"upcoming includes today", booking.Filter{Period: booking.PeriodUpcoming, Today: today}, []int64{ongoing, cancelled, block, future}},
		{"ongoing stay", booking.Filter{UnitID: unitID, Period: booking.PeriodUpcoming, Today: today.AddDays(1)}, []int64{ongoing, future}},
		{"caller controls today", booking.Filter{Period: booking.PeriodPast, Today: today.AddDays(2)}, []int64{past, checkoutToday, ongoing, cancelled, block}},
		{"unit", booking.Filter{UnitID: otherUnit}, []int64{cancelled, block}},
		{"status", booking.Filter{Status: booking.StatusCancelled}, []int64{cancelled}},
		{"combined", booking.Filter{UnitID: otherUnit, Status: booking.StatusConfirmed, Period: booking.PeriodUpcoming, Today: today}, []int64{block}},
		{"unknown unit", booking.Filter{UnitID: otherUnit + 1}, nil},
		{"no matching combination", booking.Filter{UnitID: unitID, Status: booking.StatusCancelled}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			values, err := booking.List(t.Context(), database, test.filter)
			if err != nil {
				t.Fatal(err)
			}
			var ids []int64
			for _, value := range values {
				ids = append(ids, value.ID)
			}
			if !slices.Equal(ids, test.want) {
				t.Fatalf("bookings=%v want=%v", ids, test.want)
			}
		})
	}
	for _, filter := range []booking.Filter{
		{UnitID: -1}, {Status: "unknown"}, {Period: "unknown", Today: today},
		{Period: booking.PeriodPast}, {Period: booking.PeriodUpcoming},
	} {
		if _, err := booking.List(t.Context(), database, filter); !errors.Is(err, booking.ErrInvalid) {
			t.Fatalf("filter=%+v err=%v", filter, err)
		}
	}
}

func TestArchivedBookingKeepsHistoryEditableAndCanMoveOut(t *testing.T) {
	database, _, unitID := testDB(t)
	unit, err := property.GetUnit(t.Context(), database, unitID)
	if err != nil {
		t.Fatal(err)
	}
	activeUnit, err := property.AddUnit(t.Context(), database, unit.PropertyID, "Active unit")
	if err != nil {
		t.Fatal(err)
	}
	value := details(unitID, "2026-10-20", "2026-10-23")
	id := create(t, database, value)
	if err := property.SetUnitArchived(t.Context(), database, unitID, true); err != nil {
		t.Fatal(err)
	}
	value.Notes = "Historical correction"
	if err := booking.Update(t.Context(), database, id, value); err != nil {
		t.Fatal(err)
	}
	saved, err := booking.Get(t.Context(), database, id)
	if err != nil || saved.Notes != value.Notes || saved.UnitID != unitID {
		t.Fatalf("historical edit=%+v err=%v", saved, err)
	}
	value.UnitID = activeUnit
	if err := booking.Update(t.Context(), database, id, value); err != nil {
		t.Fatal(err)
	}
	saved, err = booking.Get(t.Context(), database, id)
	if err != nil || saved.UnitID != activeUnit {
		t.Fatalf("move out=%+v err=%v", saved, err)
	}
}

func TestImportedBookingReadModel(t *testing.T) {
	database, _, unitID := testDB(t)
	// Seed an existing imported record; this does not exercise iCal synchronization.
	var feedID, id int64
	if err := database.QueryRow(`INSERT INTO calendar_feeds (unit_id, channel, url) VALUES (?, 'airbnb', 'https://example.test/calendar') RETURNING id`, unitID).Scan(&feedID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`INSERT INTO bookings (unit_id, kind, status, check_in, check_out, channel, feed_id, external_uid, notes)
		VALUES (?, 'stay', 'confirmed', '2026-10-20', '2026-10-23', 'airbnb', ?, 'event-uid', 'Owner notes') RETURNING id`, unitID, feedID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	saved, err := booking.Get(t.Context(), database, id)
	if err != nil || !saved.Imported || saved.GuestID != 0 || saved.Notes != "Owner notes" {
		t.Fatalf("imported booking=%+v err=%v", saved, err)
	}
	values, err := booking.List(t.Context(), database, booking.Filter{})
	if err != nil || len(values) != 1 || values[0] != saved {
		t.Fatalf("imported list=%v err=%v", values, err)
	}
	_, err = booking.Create(t.Context(), database, details(unitID, saved.CheckIn, saved.CheckOut))
	requireConflict(t, err, id)
	var conflict *booking.ConflictError
	if !errors.As(err, &conflict) || conflict.Bookings[0] != saved {
		t.Fatalf("imported conflict lost details: %v", err)
	}
}
