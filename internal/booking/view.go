package booking

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/gaw008/wingmanager/internal/civildate"
)

const (
	ViewUpcoming  = "upcoming"
	ViewInHouse   = "inhouse"
	ViewEnded     = "ended"
	ViewCancelled = "cancelled"
)

// ViewState describes the booking's date-based state on Today. Checkout today
// remains in-house until the following day, matching the calendar detail view.
type ViewState struct {
	View, StatusKey string
	Nights          int
}

// State derives the list grouping, status label and nights from civil dates.
func (b Booking) State(today civildate.Date) (ViewState, error) {
	from, err := civildate.Parse(b.CheckIn)
	if err != nil {
		return ViewState{}, err
	}
	to, err := civildate.Parse(b.CheckOut)
	if err != nil {
		return ViewState{}, err
	}
	result := ViewState{Nights: int(to.MidnightUTC().Sub(from.MidnightUTC()) / (24 * time.Hour))}
	switch {
	case b.Status == StatusCancelled:
		result.View, result.StatusKey = ViewCancelled, "cancelled"
	case to.Compare(today) < 0:
		result.View, result.StatusKey = ViewEnded, "booking_checked_out"
	case from.Compare(today) > 0:
		result.View, result.StatusKey = ViewUpcoming, "confirmed"
	case to.Compare(today) == 0:
		result.View, result.StatusKey = ViewInHouse, "booking_check_out_today"
	case from.Compare(today) == 0:
		result.View, result.StatusKey = ViewInHouse, "booking_check_in_today"
	default:
		result.View, result.StatusKey = ViewInHouse, "staying_now"
	}
	return result, nil
}

// HasConflict checks the same occupancy rules as manual writes, excluding this
// booking. Imported overlaps are stored as facts and surfaced for owner review.
func HasConflict(ctx context.Context, database *sql.DB, value Booking) (bool, error) {
	if value.Status != StatusConfirmed {
		return false, nil
	}
	requested, err := stayRange(value.CheckIn, value.CheckOut)
	if err != nil {
		return false, err
	}
	err = checkUnitOccupancy(ctx, New(database), value.UnitID, value.ID, requested)
	var conflict *ConflictError
	if errors.As(err, &conflict) {
		return true, nil
	}
	return false, err
}
