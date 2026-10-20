package ledger

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/gaw008/wingmanager/internal/civildate"
)

var (
	ErrInvalid  = errors.New("invalid ledger entry")
	ErrNotFound = errors.New("ledger entry not found")
	ErrAutoRent = errors.New("automatic rent cannot be voided")
	ErrDeposit  = errors.New("deposit application exceeds held deposit or balance")
)

// Details describes a positive amount. Kind determines its effect on balances.
// Period is intentionally absent: only rent posting can create automatic charges.
type Details struct {
	Kind, Category   string
	AmountCents      int64
	OccurredOn, Memo string
}

// Entry is immutable except for its void audit fields.
type Entry struct {
	ID, PropertyID, LeaseID, BookingID int64
	Details
	Period, VoidedAt, VoidReason string
}

func fromRecord(r entryRecord) Entry {
	return Entry{ID: r.ID, PropertyID: r.PropertyID, LeaseID: r.LeaseID.Int64, BookingID: r.BookingID.Int64, Details: Details{Kind: r.Kind, Category: r.Category, AmountCents: r.AmountCents, OccurredOn: r.OccurredOn, Memo: r.Memo}, Period: r.Period.String, VoidedAt: r.VoidedAt.String, VoidReason: r.VoidReason.String}
}
func validate(v Details) error {
	if !slices.Contains(Kinds(), v.Kind) || !slices.Contains(Categories(v.Kind), v.Category) || v.AmountCents <= 0 {
		return ErrInvalid
	}
	if _, err := civildate.Parse(v.OccurredOn); err != nil {
		return ErrInvalid
	}
	return nil
}
func list(ctx context.Context, q *Queries, id int64) ([]Entry, error) {
	rows, err := q.listEntries(ctx, sql.NullInt64{Int64: id, Valid: true})
	if err != nil {
		return nil, err
	}
	result := make([]Entry, len(rows))
	for i, r := range rows {
		result[i] = fromRecord(r)
	}
	return result, nil
}

// List returns all entries, including voids, in FIFO order (date, then ID).
func List(ctx context.Context, database *sql.DB, leaseID int64) ([]Entry, error) {
	return list(ctx, New(database), leaseID)
}
func record(ctx context.Context, q *Queries, leaseID int64, v Details) (int64, error) {
	id, err := q.createEntry(ctx, createEntryParams{ID: leaseID, Kind: v.Kind, Category: v.Category, AmountCents: v.AmountCents, OccurredOn: v.OccurredOn, Memo: v.Memo})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrInvalid
	}
	return id, err
}

// Record derives property ownership from the lease's unit; callers cannot override it.
func Record(ctx context.Context, database *sql.DB, leaseID int64, v Details) (int64, error) {
	if err := validate(v); err != nil {
		return 0, err
	}
	if !slices.Contains(LeaseKinds(), v.Kind) {
		return 0, ErrInvalid
	}
	return record(ctx, New(database), leaseID, v)
}

// Void preserves the original entry and requires a reason. Repeating it preserves
// the first audit record. Automatic rent is never voidable, even with a reason.
func Void(ctx context.Context, database *sql.DB, id int64, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ErrInvalid
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := New(tx)
	v, err := q.getEntry(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if v.Period.Valid {
		return ErrAutoRent
	}
	if err := q.voidEntry(ctx, voidEntryParams{ID: id, VoidReason: sql.NullString{String: reason, Valid: true}}); err != nil {
		return err
	}
	return tx.Commit()
}

// Summary derives balances and the oldest unpaid charge after applying all
// non-voided credits and payments FIFO. DaysOverdue is zero on the due date.
type Summary struct {
	BalanceCents, DepositHeldCents int64
	OldestUnpaidOn                 string
	DaysOverdue                    int
}

func summarize(entries []Entry, today civildate.Date) (Summary, error) {
	var result Summary
	var offsets int64
	for _, v := range entries {
		if v.VoidedAt != "" {
			continue
		}
		switch v.Kind {
		case KindCharge:
			result.BalanceCents += v.AmountCents
		case KindCredit, KindPayment:
			result.BalanceCents -= v.AmountCents
			offsets += v.AmountCents
		case KindDepositIn:
			result.DepositHeldCents += v.AmountCents
		case KindDepositOut:
			result.DepositHeldCents -= v.AmountCents
		}
	}
	if result.BalanceCents <= 0 {
		return result, nil
	}
	for _, v := range entries {
		if v.VoidedAt != "" || v.Kind != KindCharge {
			continue
		}
		if offsets >= v.AmountCents {
			offsets -= v.AmountCents
			continue
		}
		result.OldestUnpaidOn = v.OccurredOn
		due, err := civildate.Parse(v.OccurredOn)
		if err != nil {
			return Summary{}, err
		}
		if due.Compare(today) < 0 {
			result.DaysOverdue = int(today.MidnightUTC().Sub(due.MidnightUTC()).Hours() / 24)
		}
		break
	}
	return result, nil
}

// Balance includes all non-voided entries; today is in the owner's configured zone.
func Balance(ctx context.Context, database *sql.DB, leaseID int64, today civildate.Date) (Summary, error) {
	if today.IsZero() {
		return Summary{}, ErrInvalid
	}
	entries, err := List(ctx, database, leaseID)
	if err != nil {
		return Summary{}, err
	}
	return summarize(entries, today)
}

// ApplyDeposit atomically releases held deposit and records an equal payment.
// No cash is received by this operation. The amount cannot exceed held deposit or debt.
func ApplyDeposit(ctx context.Context, database *sql.DB, leaseID, amount int64, on, memo string) error {
	v := Details{Kind: KindDepositOut, Category: CategoryDeposit, AmountCents: amount, OccurredOn: on, Memo: memo}
	if err := validate(v); err != nil {
		return err
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := New(tx)
	entries, err := list(ctx, q, leaseID)
	if err != nil {
		return err
	}
	day, err := civildate.Parse(on)
	if err != nil {
		return err
	}
	summary, err := summarize(entries, day)
	if err != nil {
		return err
	}
	if amount > summary.DepositHeldCents || amount > summary.BalanceCents {
		return ErrDeposit
	}
	if _, err := record(ctx, q, leaseID, v); err != nil {
		return err
	}
	v.Kind, v.Category = KindPayment, CategoryDepositApplied
	if _, err := record(ctx, q, leaseID, v); err != nil {
		return err
	}
	return tx.Commit()
}

// RecordProperty records an expense or other income. BookingID zero means no
// booking; only stay_payout may link a stay belonging to this property.
func RecordProperty(ctx context.Context, database *sql.DB, propertyID, bookingID int64, v Details) (int64, error) {
	if err := validate(v); err != nil {
		return 0, err
	}
	if propertyID <= 0 || bookingID < 0 || (v.Kind != KindIncome && v.Kind != KindExpense) || (bookingID != 0 && (v.Kind != KindIncome || v.Category != CategoryStayPayout)) {
		return 0, ErrInvalid
	}
	id, err := New(database).createPropertyEntry(ctx, createPropertyEntryParams{
		PropertyID: propertyID, BookingID: sql.NullInt64{Int64: bookingID, Valid: bookingID != 0},
		Kind: v.Kind, Category: v.Category, AmountCents: v.AmountCents, OccurredOn: v.OccurredOn, Memo: v.Memo,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrInvalid
	}
	return id, err
}

// Filter selects entries including voids. Zero property/year and empty kind mean all.
type Filter struct {
	PropertyID int64
	Year       int
	Kind       string
}

// NamedEntry includes the property's name for list and CSV presentation.
type NamedEntry struct {
	Entry
	PropertyName string
}

// ListEntries returns entries in date/ID order, with optional property, year and kind filters.
func ListEntries(ctx context.Context, database *sql.DB, filter Filter) ([]NamedEntry, error) {
	if filter.PropertyID < 0 || filter.Year < 0 || filter.Year > 9999 || (filter.Kind != "" && !slices.Contains(Kinds(), filter.Kind)) {
		return nil, ErrInvalid
	}
	return loadEntries(ctx, New(database), filter)
}

func loadEntries(ctx context.Context, q *Queries, f Filter) ([]NamedEntry, error) {
	from, to := "", "9999-12-31"
	if f.Year != 0 {
		from, to = yearBounds(f.Year)
	}
	rows, err := q.filteredEntries(ctx, filteredEntriesParams{PropertyID: f.PropertyID, FromDate: from, ToDate: to, Kind: f.Kind})
	if err != nil {
		return nil, err
	}
	result := make([]NamedEntry, len(rows))
	for i, row := range rows {
		result[i] = NamedEntry{Entry: fromRecord(row.EntryRecord), PropertyName: row.PropertyName}
	}
	return result, nil
}
