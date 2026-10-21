package booking_test

import (
	"testing"

	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/civildate"
	"github.com/gaw008/wingmanager/internal/property"
)

func TestTodayReservationsUseCivilDatesAndFourteenDayWindow(t *testing.T) {
	database, _, unitID := testDB(t)
	unit, err := property.GetUnit(t.Context(), database, unitID)
	if err != nil {
		t.Fatal(err)
	}
	for _, dates := range [][2]string{{"2026-10-30", "2026-10-31"}, {"2026-10-31", "2026-11-03"}, {"2026-10-29", "2026-11-20"}, {"2026-11-14", "2026-11-15"}, {"2026-11-15", "2026-11-16"}} {
		id, err := property.AddUnit(t.Context(), database, unit.PropertyID, dates[0])
		if err != nil {
			t.Fatal(err)
		}
		create(t, database, details(id, dates[0], dates[1]))
	}
	today, err := civildate.Parse("2026-10-31")
	if err != nil {
		t.Fatal(err)
	}
	values, err := booking.TodayReservations(t.Context(), database, today)
	if err != nil {
		t.Fatal(err)
	}
	if values.CheckInCount != 1 || values.CheckOutCount != 1 || values.StayingCount != 1 || values.UpcomingCount != 2 {
		t.Fatalf("counts: %+v", values)
	}
	if values.CheckIns[0].Nights != 3 || values.CheckOuts[0].Nights != 1 {
		t.Fatalf("nights: %+v", values)
	}
	if !values.Upcoming[0].Departing || values.Upcoming[0].EventDays != 3 || values.Upcoming[1].Departing || values.Upcoming[1].EventDays != 14 {
		t.Fatalf("events: %+v", values.Upcoming)
	}
}
