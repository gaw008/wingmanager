// Package lease owns lease lifecycle, tenants, and calendar-month rent calculation.
package lease

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/gaw008/wingmanager/internal/civildate"
	"github.com/gaw008/wingmanager/internal/occupancy"
	"github.com/gaw008/wingmanager/internal/person"
)

const (
	StatusDraft  = "draft"
	StatusActive = "active"
	StatusEnded  = "ended"
)

func Statuses() []string { return []string{StatusDraft, StatusActive, StatusEnded} }

var (
	ErrInvalid  = errors.New("invalid lease details or state change")
	ErrNotFound = errors.New("lease not found")
	ErrConflict = errors.New("lease overlaps occupied dates")
	ErrTenant   = errors.New("an active lease requires at least one tenant")
)

// Details contains lease terms. Empty EndsOn means month-to-month.
type Details struct {
	UnitID           int64
	StartsOn, EndsOn string
	RentCents        int64
	DueDay           int64
	DepositCents     int64
	Notes            string
}

// Lease has immutable status transitions: draft -> active -> ended.
type Lease struct {
	ID     int64
	Status string
	Details
}

func fromRecord(r leaseRecord) Lease {
	return Lease{ID: r.ID, Status: r.Status, Details: Details{UnitID: r.UnitID, StartsOn: r.StartsOn, EndsOn: r.EndsOn.String, RentCents: r.RentCents, DueDay: r.DueDay, DepositCents: r.DepositCents, Notes: r.Notes}}
}
func readError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
func optional(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

// Occupancy converts the inclusive lease end through the shared interval rules.
func Occupancy(v Lease) (occupancy.Range, error) {
	start, err := civildate.Parse(v.StartsOn)
	if err != nil {
		return occupancy.Range{}, err
	}
	var end *civildate.Date
	if v.EndsOn != "" {
		d, err := civildate.Parse(v.EndsOn)
		if err != nil {
			return occupancy.Range{}, err
		}
		end = &d
	}
	return occupancy.Lease(start, end)
}
func validate(v Details) error {
	if v.UnitID <= 0 || v.RentCents <= 0 || v.DepositCents < 0 || v.DueDay < 1 || v.DueDay > 28 {
		return ErrInvalid
	}
	if _, err := Occupancy(Lease{Details: v}); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return nil
}
func available(ctx context.Context, q *Queries, id int64) error {
	ok, err := q.unitAvailable(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalid
	}
	if err != nil {
		return err
	}
	if !ok.Bool {
		return ErrInvalid
	}
	return nil
}

// Create saves a non-occupying draft. Tenants are added separately.
func Create(ctx context.Context, database *sql.DB, v Details) (int64, error) {
	if err := validate(v); err != nil {
		return 0, err
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	q := New(tx)
	if err := available(ctx, q, v.UnitID); err != nil {
		return 0, err
	}
	id, err := q.createLease(ctx, createLeaseParams{UnitID: v.UnitID, StartsOn: v.StartsOn, EndsOn: optional(v.EndsOn), RentCents: v.RentCents, DueDay: v.DueDay, DepositCents: v.DepositCents, Notes: v.Notes})
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// Get returns terms and status, without exposing generated records.
func Get(ctx context.Context, database *sql.DB, id int64) (Lease, error) {
	r, err := New(database).getLease(ctx, id)
	return fromRecord(r), readError(err)
}

// GetInTx reads terms under the caller's write lock, for atomic rent posting.
func GetInTx(ctx context.Context, tx *sql.Tx, id int64) (Lease, error) {
	r, err := New(tx).getLease(ctx, id)
	return fromRecord(r), readError(err)
}

// List filters by status; an empty status returns all leases, including ended ones.
func List(ctx context.Context, database *sql.DB, status string) ([]Lease, error) {
	if status != "" && !slices.Contains(Statuses(), status) {
		return nil, ErrInvalid
	}
	rows, err := New(database).listLeases(ctx)
	if err != nil {
		return nil, err
	}
	var result []Lease
	for _, r := range rows {
		if status == "" || r.Status == status {
			result = append(result, fromRecord(r))
		}
	}
	return result, nil
}

// Update enforces editable terms for each state, including for direct callers.
func Update(ctx context.Context, database *sql.DB, id int64, v Details) error {
	if err := validate(v); err != nil {
		return err
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := New(tx)
	old, err := GetInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if old.Status == StatusEnded {
		expected := old.Details
		expected.Notes = v.Notes
		if v != expected {
			return ErrInvalid
		}
	}
	if old.Status == StatusActive && (v.UnitID != old.UnitID || v.StartsOn != old.StartsOn || v.DueDay != old.DueDay || v.DepositCents != old.DepositCents) {
		return ErrInvalid
	}
	if old.Status == StatusDraft && v.UnitID != old.UnitID {
		if err := available(ctx, q, v.UnitID); err != nil {
			return err
		}
	}
	if old.Status == StatusActive && old.EndsOn != "" && (v.EndsOn == "" || v.EndsOn > old.EndsOn) {
		if err := checkOccupancy(ctx, q, Lease{ID: id, Details: v}); err != nil {
			return err
		}
	}
	if err := q.updateLease(ctx, updateLeaseParams{ID: id, UnitID: v.UnitID, StartsOn: v.StartsOn, EndsOn: optional(v.EndsOn), RentCents: v.RentCents, DueDay: v.DueDay, DepositCents: v.DepositCents, Notes: v.Notes}); err != nil {
		return err
	}
	return tx.Commit()
}

// Activate acquires occupancy atomically. The caller posts due rent after success.
func Activate(ctx context.Context, database *sql.DB, id int64) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := New(tx)
	v, err := GetInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if v.Status != StatusDraft {
		return ErrInvalid
	}
	tenants, err := q.listTenants(ctx, id)
	if err != nil {
		return err
	}
	if len(tenants) == 0 {
		return ErrTenant
	}
	if err := available(ctx, q, v.UnitID); err != nil {
		return err
	}
	if err := checkOccupancy(ctx, q, v); err != nil {
		return err
	}
	if err := q.setStatus(ctx, setStatusParams{ID: id, Status: StatusActive}); err != nil {
		return err
	}
	return tx.Commit()
}

// End manually ends an active lease on its chosen inclusive final day.
func End(ctx context.Context, database *sql.DB, id int64, endsOn string) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	v, err := GetInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if v.Status != StatusActive {
		return ErrInvalid
	}
	v.EndsOn = endsOn
	if endsOn == "" {
		return ErrInvalid
	}
	if err := validate(v.Details); err != nil {
		return err
	}
	if err := New(tx).endLease(ctx, endLeaseParams{ID: id, EndsOn: optional(endsOn)}); err != nil {
		return err
	}
	return tx.Commit()
}

// AutoEnd runs after rent catch-up so an offline machine does not lose final rent.
func AutoEnd(ctx context.Context, database *sql.DB, today civildate.Date) error {
	if today.IsZero() {
		return ErrInvalid
	}
	return New(database).autoEnd(ctx, optional(today.String()))
}

// AddTenant attaches an existing person or atomically creates an inline contact.
func AddTenant(ctx context.Context, database *sql.DB, id, personID int64, newPerson *person.Details) error {
	if (personID > 0) == (newPerson != nil) || personID < 0 {
		return ErrInvalid
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	v, err := GetInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if v.Status == StatusEnded {
		return ErrInvalid
	}
	if newPerson != nil {
		personID, err = person.CreateInTx(ctx, tx, *newPerson)
		if err != nil {
			return err
		}
	}
	if err := New(tx).addTenant(ctx, addTenantParams{LeaseID: id, PersonID: personID}); err != nil {
		return err
	}
	return tx.Commit()
}

// RemoveTenant keeps at least one tenant on an active lease. Missing links are a no-op.
func RemoveTenant(ctx context.Context, database *sql.DB, id, personID int64) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := New(tx)
	v, err := GetInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if v.Status == StatusEnded {
		return ErrInvalid
	}
	if err := q.removeTenant(ctx, removeTenantParams{LeaseID: id, PersonID: personID}); err != nil {
		return err
	}
	rows, err := q.listTenants(ctx, id)
	if err != nil {
		return err
	}
	if v.Status == StatusActive && len(rows) == 0 {
		return ErrTenant
	}
	return tx.Commit()
}

// Tenants lists the lease's contacts in name order.
func Tenants(ctx context.Context, database *sql.DB, id int64) ([]person.Person, error) {
	rows, err := New(database).listTenants(ctx, id)
	if err != nil {
		return nil, err
	}
	result := make([]person.Person, len(rows))
	for i, r := range rows {
		result[i] = person.Person{ID: r.ID, Name: r.Name, Email: r.Email, Phone: r.Phone, Notes: r.Notes}
	}
	return result, nil
}

// db.Open's BEGIN IMMEDIATE holds the write lock across this check and activation
// or extension, excluding concurrent booking and lease occupancy writes.
func checkOccupancy(ctx context.Context, q *Queries, v Lease) error {
	requested, err := Occupancy(v)
	if err != nil {
		return err
	}
	bookings, err := q.occupyingBookings(ctx, v.UnitID)
	if err != nil {
		return err
	}
	for _, b := range bookings {
		from, err := civildate.Parse(b.CheckIn)
		if err != nil {
			return err
		}
		to, err := civildate.Parse(b.CheckOut)
		if err != nil {
			return err
		}
		r, err := occupancy.Stay(from, to)
		if err != nil {
			return err
		}
		if requested.Overlaps(r) {
			return ErrConflict
		}
	}
	leases, err := q.occupyingLeases(ctx, occupyingLeasesParams{UnitID: v.UnitID, ID: v.ID})
	if err != nil {
		return err
	}
	for _, l := range leases {
		r, err := Occupancy(fromRecord(l))
		if err != nil {
			return err
		}
		if requested.Overlaps(r) {
			return ErrConflict
		}
	}
	return nil
}
