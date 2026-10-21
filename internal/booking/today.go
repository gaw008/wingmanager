package booking

import (
	"context"
	"database/sql"

	"github.com/gaw008/wingmanager/internal/civildate"
)

// Turnover is a confirmed stay's arrival or departure, including its display names.
type Turnover struct {
	ID                                                   int64
	CheckIn, CheckOut, PropertyName, UnitName, GuestName string
}

// TurnoverSummary separates today from the following seven civil dates. Blocks
// and cancelled stays never represent arriving or departing guests.
type TurnoverSummary struct {
	CheckIns, CheckOuts, UpcomingCheckIns, UpcomingCheckOuts []Turnover
}

func Turnovers(ctx context.Context, database *sql.DB, today civildate.Date) (TurnoverSummary, error) {
	if today.IsZero() {
		return TurnoverSummary{}, ErrInvalid
	}
	rows, err := New(database).turnovers(ctx, turnoversParams{Today: today.String(), ThroughDate: today.AddDays(7).String()})
	if err != nil {
		return TurnoverSummary{}, err
	}
	var result TurnoverSummary
	for _, row := range rows {
		value := Turnover(row)
		switch {
		case row.CheckIn == today.String():
			result.CheckIns = append(result.CheckIns, value)
		case row.CheckIn > today.String() && row.CheckIn <= today.AddDays(7).String():
			result.UpcomingCheckIns = append(result.UpcomingCheckIns, value)
		}
		switch {
		case row.CheckOut == today.String():
			result.CheckOuts = append(result.CheckOuts, value)
		case row.CheckOut > today.String() && row.CheckOut <= today.AddDays(7).String():
			result.UpcomingCheckOuts = append(result.UpcomingCheckOuts, value)
		}
	}
	return result, nil
}

// Reservation contains the confirmed guest stay and civil-date durations used
// by the Today cards. EventDays is measured to the next arrival or departure.
type Reservation struct {
	Turnover
	Channel           string
	Nights, EventDays int
	Departing         bool
}

// Reservations separates today's arrivals/departures, guests already staying,
// and stays with an arrival or departure in the following fourteen civil dates.
// A same-day arrival belongs to CheckIns, not Staying.
type Reservations struct {
	CheckIns, CheckOuts, Staying, Upcoming                   []Reservation
	CheckInCount, CheckOutCount, StayingCount, UpcomingCount int
}

// TodayReservations derives card groups and durations from confirmed stays.
func TodayReservations(ctx context.Context, database *sql.DB, today civildate.Date) (Reservations, error) {
	if today.IsZero() {
		return Reservations{}, ErrInvalid
	}
	through := today.AddDays(14).String()
	rows, err := New(database).todayStays(ctx, todayStaysParams{Today: today.String(), ThroughDate: through})
	if err != nil {
		return Reservations{}, err
	}
	var result Reservations
	for _, row := range rows {
		from, err := civildate.Parse(row.CheckIn)
		if err != nil {
			return Reservations{}, err
		}
		to, err := civildate.Parse(row.CheckOut)
		if err != nil {
			return Reservations{}, err
		}
		v := Reservation{Turnover: Turnover{ID: row.ID, CheckIn: row.CheckIn, CheckOut: row.CheckOut, PropertyName: row.PropertyName, UnitName: row.UnitName, GuestName: row.GuestName}, Channel: row.Channel, Nights: int(to.MidnightUTC().Sub(from.MidnightUTC()).Hours() / 24)}
		if row.CheckIn == today.String() {
			result.CheckIns = append(result.CheckIns, v)
		}
		if row.CheckOut == today.String() {
			result.CheckOuts = append(result.CheckOuts, v)
		}
		if row.CheckIn < today.String() && row.CheckOut > today.String() {
			result.Staying = append(result.Staying, v)
		}
		event := from
		if row.CheckIn <= today.String() {
			event, v.Departing = to, true
		}
		if event.Compare(today) > 0 && event.String() <= through {
			v.EventDays = int(event.MidnightUTC().Sub(today.MidnightUTC()).Hours() / 24)
			result.Upcoming = append(result.Upcoming, v)
		}
	}
	result.CheckInCount, result.CheckOutCount = len(result.CheckIns), len(result.CheckOuts)
	result.StayingCount, result.UpcomingCount = len(result.Staying), len(result.Upcoming)
	return result, nil
}
