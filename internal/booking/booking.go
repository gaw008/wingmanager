// Package booking owns manual bookings and their occupancy checks.
package booking

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/gaw008/wingmanager/internal/civildate"
	"github.com/gaw008/wingmanager/internal/occupancy"
	"github.com/gaw008/wingmanager/internal/person"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var (
	ErrInvalid  = errors.New("invalid booking details")
	ErrNotFound = errors.New("booking not found")
	ErrImported = errors.New("imported booking is not a manual booking")
)

// Booking is a stay or calendar block. GuestID zero means no guest; Imported
// identifies bookings whose dates and status belong to an external calendar.
// Dates are canonical YYYY-MM-DD strings, matching Details and form values.
type Booking struct {
	ID, UnitID, GuestID         int64
	Kind, Status, Channel       string
	CheckIn, CheckOut           string
	Notes, CreatedAt, UpdatedAt string
	Imported                    bool
}

const (
	PeriodUpcoming = "upcoming"
	PeriodPast     = "past"
)

// Filter selects bookings. Zero UnitID and empty Status or Period mean all.
// A non-empty Period requires Today in the caller's configured time zone.
// Check-out on Today counts as past; an ongoing stay counts as upcoming.
type Filter struct {
	UnitID         int64
	Status, Period string
	Today          civildate.Date
}

// Details contains the owner's editable fields. Dates are canonical YYYY-MM-DD.
// A stay can use GuestID (zero means none) or NewGuest; blocks cannot have a guest.
type Details struct {
	UnitID            int64
	Kind              string
	CheckIn, CheckOut string
	Channel           string
	GuestID           int64
	NewGuest          *person.Details
	Notes             string
}

// ConflictError lists the confirmed bookings occupying the requested dates.
type ConflictError struct{ Bookings []Booking }

func (e *ConflictError) Error() string { return "booking overlaps confirmed bookings" }

// Create saves a confirmed manual booking and any inline guest atomically.
// Use a database opened by db.Open so its IMMEDIATE transaction protects occupancy.
func Create(ctx context.Context, database *sql.DB, value Details) (int64, error) {
	requested, err := validate(value)
	if err != nil {
		return 0, err
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	queries := New(tx)
	if err := checkActiveUnit(ctx, queries, value.UnitID); err != nil {
		return 0, err
	}
	if err := checkUnitOccupancy(ctx, queries, value.UnitID, 0, requested); err != nil {
		return 0, err
	}
	guestID, err := saveGuest(ctx, tx, value)
	if err != nil {
		return 0, err
	}
	id, err := queries.createBooking(ctx, createBookingParams{
		UnitID: value.UnitID, Kind: value.Kind, Status: StatusConfirmed,
		CheckIn: value.CheckIn, CheckOut: value.CheckOut, Channel: value.Channel,
		GuestID: guestID, Notes: value.Notes,
	})
	if err != nil {
		return 0, writeError(err)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// Update edits a manual booking, preserving its confirmed or cancelled status.
// Confirmed bookings re-check occupancy; cancelled bookings do not occupy dates.
func Update(ctx context.Context, database *sql.DB, id int64, value Details) error {
	requested, err := validate(value)
	if err != nil {
		return err
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	queries := New(tx)
	current, err := queries.getBooking(ctx, id)
	if err != nil {
		return readError(err)
	}
	if current.FeedID.Valid {
		return ErrImported
	}
	if value.UnitID != current.UnitID {
		if err := checkActiveUnit(ctx, queries, value.UnitID); err != nil {
			return err
		}
	}
	if current.Status == StatusConfirmed {
		if err := checkUnitOccupancy(ctx, queries, value.UnitID, id, requested); err != nil {
			return err
		}
	}
	guestID, err := saveGuest(ctx, tx, value)
	if err != nil {
		return err
	}
	if err := queries.updateBooking(ctx, updateBookingParams{
		ID: id, UnitID: value.UnitID, Kind: value.Kind,
		CheckIn: value.CheckIn, CheckOut: value.CheckOut, Channel: value.Channel,
		GuestID: guestID, Notes: value.Notes,
	}); err != nil {
		return writeError(err)
	}
	return tx.Commit()
}

// SetCancelled cancels a manual booking or re-confirms it after checking occupancy.
// Repeating the current state is a no-op.
func SetCancelled(ctx context.Context, database *sql.DB, id int64, cancelled bool) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	queries := New(tx)
	current, err := queries.getBooking(ctx, id)
	if err != nil {
		return readError(err)
	}
	if current.FeedID.Valid {
		return ErrImported
	}
	status := StatusConfirmed
	if cancelled {
		status = StatusCancelled
	}
	if current.Status == status {
		return tx.Commit()
	}
	if !cancelled {
		requested, err := stayRange(current.CheckIn, current.CheckOut)
		if err != nil {
			return err
		}
		if err := checkUnitOccupancy(ctx, queries, current.UnitID, id, requested); err != nil {
			return err
		}
	}
	if err := queries.setBookingStatus(ctx, setBookingStatusParams{ID: id, Status: status}); err != nil {
		return err
	}
	return tx.Commit()
}

// Get includes cancelled bookings so the owner can edit and restore them.
func Get(ctx context.Context, database *sql.DB, id int64) (Booking, error) {
	value, err := New(database).getBooking(ctx, id)
	return fromRecord(value), readError(err)
}

// List returns matching bookings in check-in order, including cancelled bookings
// unless Status selects otherwise. Period uses civil dates, never the system clock.
func List(ctx context.Context, database *sql.DB, filter Filter) ([]Booking, error) {
	if filter.UnitID < 0 ||
		(filter.Status != "" && !slices.Contains(Statuses(), filter.Status)) ||
		(filter.Period != "" && filter.Period != PeriodUpcoming && filter.Period != PeriodPast) ||
		(filter.Period != "" && filter.Today.IsZero()) {
		return nil, ErrInvalid
	}
	records, err := New(database).listBookings(ctx)
	if err != nil {
		return nil, err
	}
	var values []Booking
	for _, record := range records {
		if filter.UnitID != 0 && record.UnitID != filter.UnitID || filter.Status != "" && record.Status != filter.Status {
			continue
		}
		if filter.Period != "" {
			checkout, err := civildate.Parse(record.CheckOut)
			if err != nil {
				return nil, err
			}
			past := checkout.Compare(filter.Today) <= 0
			if filter.Period == PeriodPast && !past || filter.Period == PeriodUpcoming && past {
				continue
			}
		}
		values = append(values, fromRecord(record))
	}
	return values, nil
}

func fromRecord(value bookingRecord) Booking {
	return Booking{
		ID: value.ID, UnitID: value.UnitID, GuestID: value.GuestID.Int64,
		Kind: value.Kind, Status: value.Status, Channel: value.Channel,
		CheckIn: value.CheckIn, CheckOut: value.CheckOut, Notes: value.Notes,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, Imported: value.FeedID.Valid,
	}
}

func validate(value Details) (occupancy.Range, error) {
	if !slices.Contains(Kinds(), value.Kind) || !slices.Contains(Channels(), value.Channel) {
		return occupancy.Range{}, ErrInvalid
	}
	if (value.GuestID != 0 && value.NewGuest != nil) ||
		(value.Kind == KindBlock && (value.GuestID != 0 || value.NewGuest != nil)) {
		return occupancy.Range{}, ErrInvalid
	}
	return stayRange(value.CheckIn, value.CheckOut)
}

func stayRange(checkIn, checkOut string) (occupancy.Range, error) {
	from, err := civildate.Parse(checkIn)
	if err != nil {
		return occupancy.Range{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	to, err := civildate.Parse(checkOut)
	if err != nil {
		return occupancy.Range{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	requested, err := occupancy.Stay(from, to)
	if err != nil {
		return occupancy.Range{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return requested, nil
}

func saveGuest(ctx context.Context, tx *sql.Tx, value Details) (sql.NullInt64, error) {
	if value.NewGuest == nil {
		return sql.NullInt64{Int64: value.GuestID, Valid: value.GuestID != 0}, nil
	}
	id, err := person.CreateInTx(ctx, tx, *value.NewGuest)
	return sql.NullInt64{Int64: id, Valid: err == nil}, err
}

func checkActiveUnit(ctx context.Context, queries *Queries, unitID int64) error {
	archived, err := queries.unitArchived(ctx, unitID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: unit does not exist", ErrInvalid)
	}
	if err != nil {
		return err
	}
	if archived {
		return fmt.Errorf("%w: cannot create or move a booking to an archived unit", ErrInvalid)
	}
	return nil
}

// checkUnitOccupancy is called before each write that can acquire occupied dates.
// PLAN §6.3: check-then-insert is safe here because db.Open uses BEGIN IMMEDIATE:
// SQLite takes the database write lock before this query, so no other writer can
// insert or change occupancy between this check and our commit.
func checkUnitOccupancy(ctx context.Context, queries *Queries, unitID, excludeID int64, requested occupancy.Range) error {
	candidates, err := queries.listOccupyingBookings(ctx, listOccupyingBookingsParams{
		UnitID: unitID, Status: StatusConfirmed, ID: excludeID,
	})
	if err != nil {
		return err
	}
	var conflicts []Booking
	for _, candidate := range candidates {
		occupied, err := stayRange(candidate.CheckIn, candidate.CheckOut)
		if err != nil {
			return err
		}
		if requested.Overlaps(occupied) {
			conflicts = append(conflicts, fromRecord(candidate))
		}
	}
	if len(conflicts) != 0 {
		return &ConflictError{Bookings: conflicts}
	}
	// M5 active-lease occupancy seam: check active leases here with the same
	// requested range and transaction, before any booking write.
	return nil
}

func readError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func writeError(err error) error {
	var constraint *sqlite.Error
	if errors.As(err, &constraint) && constraint.Code() == sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY {
		return fmt.Errorf("%w: unit or guest does not exist", ErrInvalid)
	}
	return err
}
