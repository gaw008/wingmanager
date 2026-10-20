// Package task owns tasks and transactional cleaning-task maintenance.
package task

import (
	"context"
	"database/sql"
	"errors"
)

const (
	KindCleaning    = "cleaning"
	KindMaintenance = "maintenance"
	KindOther       = "other"
	StatusOpen      = "open"
	StatusDone      = "done"
	StatusCancelled = "cancelled"
)

func Kinds() []string    { return []string{KindCleaning, KindMaintenance, KindOther} }
func Statuses() []string { return []string{StatusOpen, StatusDone, StatusCancelled} }

// Task is work for a property. Zero UnitID or BookingID means no association;
// empty DueOn or DoneAt means no due date or completion timestamp, respectively.
// DueOn is a canonical YYYY-MM-DD date.
type Task struct {
	ID, PropertyID, UnitID, BookingID int64
	Kind, Title, DueOn, Status        string
	Assignee, Notes, DoneAt           string
}

// EnsureCleaning creates or reopens the booking's cleaning task, moving unfinished
// work to its current unit and checkout date. Unconfirmed stays keep cancelled
// work; completed work is immutable.
func EnsureCleaning(ctx context.Context, tx *sql.Tx, bookingID int64, confirmed bool) error {
	status := StatusCancelled
	if confirmed {
		status = StatusOpen
	}
	return New(tx).ensureCleaning(ctx, ensureCleaningParams{Status: status, BookingID: bookingID})
}

// CancelCleaning cancels open work only; completed work remains historical fact.
func CancelCleaning(ctx context.Context, tx *sql.Tx, bookingID int64) error {
	return New(tx).cancelCleaning(ctx, cancelCleaningParams{Status: StatusCancelled, BookingID: sql.NullInt64{Int64: bookingID, Valid: true}})
}

// CleaningForBooking returns nil when a booking has no cleaning task.
func CleaningForBooking(ctx context.Context, database *sql.DB, bookingID int64) (*Task, error) {
	value, err := New(database).cleaningForBooking(ctx, sql.NullInt64{Int64: bookingID, Valid: true})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &Task{
		ID: value.ID, PropertyID: value.PropertyID, UnitID: value.UnitID.Int64,
		BookingID: value.BookingID.Int64, Kind: value.Kind, Title: value.Title,
		DueOn: value.DueOn.String, Status: value.Status, Assignee: value.Assignee,
		Notes: value.Notes, DoneAt: value.DoneAt.String,
	}, nil
}
