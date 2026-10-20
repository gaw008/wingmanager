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
	return &value, nil
}
