package booking

import (
	"context"
	"database/sql"
	"errors"

	"github.com/gaw008/wingmanager/internal/civildate"
	"github.com/gaw008/wingmanager/internal/person"
)

// ImportedEvent is an external calendar's authoritative occupancy, without any
// owner-managed fields. Dates must be canonical YYYY-MM-DD civil dates.
type ImportedEvent struct{ UID, Kind, CheckIn, CheckOut string }

// ApplySnapshot reconciles a fully parsed feed inside the caller's IMMEDIATE
// transaction. Imports record overlaps as facts; they never check availability.
// Missing events with checkout before today remain untouched as history.
func ApplySnapshot(ctx context.Context, tx *sql.Tx, feedID, unitID int64, channel string, events []ImportedEvent, today civildate.Date) error {
	queries := New(tx)
	previous, err := queries.listFeedBookings(ctx, sql.NullInt64{Int64: feedID, Valid: true})
	if err != nil {
		return err
	}
	previousByUID := make(map[string]int64, len(previous))
	for _, value := range previous {
		previousByUID[value.ExternalUid.String] = value.ID
	}
	seen := make(map[string]bool, len(events))
	for _, event := range events {
		seen[event.UID] = true
		id, err := queries.upsertImported(ctx, upsertImportedParams{UnitID: unitID, Kind: event.Kind, CheckIn: event.CheckIn, CheckOut: event.CheckOut, Channel: channel, FeedID: sql.NullInt64{Int64: feedID, Valid: true}, ExternalUid: sql.NullString{String: event.UID, Valid: true}})
		// SQLite RETURNING emits no row when the conflict's WHERE clause is false.
		if errors.Is(err, sql.ErrNoRows) {
			id, err = previousByUID[event.UID], nil
		}
		if err != nil {
			return err
		}
		if err := maintainCleaning(ctx, tx, id); err != nil {
			return err
		}
	}
	for _, value := range previous {
		if seen[value.ExternalUid.String] || value.CheckOut < today.String() || value.Status == StatusCancelled {
			continue
		}
		if err := queries.setBookingStatus(ctx, setBookingStatusParams{ID: value.ID, Status: StatusCancelled}); err != nil {
			return err
		}
		if err := maintainCleaning(ctx, tx, value.ID); err != nil {
			return err
		}
	}
	return nil
}

// DetachFeed transfers every imported booking to owner control before feed removal.
// It preserves occupancy, channel, owner fields, and cleaning work.
func DetachFeed(ctx context.Context, tx *sql.Tx, feedID int64) error {
	return New(tx).detachFeed(ctx, sql.NullInt64{Int64: feedID, Valid: true})
}

// OwnerDetails is the only editable portion of an imported booking.
// GuestID zero clears the guest; NewGuest creates and links a person atomically.
type OwnerDetails struct {
	GuestID  int64
	NewGuest *person.Details
	Notes    string
}

// UpdateImported edits owner fields without writing external occupancy fields.
func UpdateImported(ctx context.Context, database *sql.DB, id int64, value OwnerDetails) error {
	if value.GuestID != 0 && value.NewGuest != nil {
		return ErrInvalid
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := New(tx)
	current, err := q.getBooking(ctx, id)
	if err != nil {
		return readError(err)
	}
	if !current.FeedID.Valid {
		return ErrInvalid
	}
	guest, err := saveGuest(ctx, tx, Details{GuestID: value.GuestID, NewGuest: value.NewGuest})
	if err != nil {
		return err
	}
	if err := q.updateImportedOwnerFields(ctx, updateImportedOwnerFieldsParams{ID: id, GuestID: guest, Notes: value.Notes}); err != nil {
		return writeError(err)
	}
	return tx.Commit()
}
