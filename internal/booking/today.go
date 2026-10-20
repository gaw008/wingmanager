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
