package ledger

import (
	"context"
	"database/sql"

	"github.com/gaw008/wingmanager/internal/civildate"
)

// OverdueLease keeps joint tenants together because debt belongs to their lease.
type OverdueLease struct {
	LeaseID                             int64
	PropertyName, UnitName, TenantNames string
	Summary
}

// Overdue uses the same FIFO balance calculation as the lease ledger. Draft
// leases are excluded; ended leases retain any outstanding historical debt.
func Overdue(ctx context.Context, database *sql.DB, today civildate.Date) ([]OverdueLease, error) {
	if today.IsZero() {
		return nil, ErrInvalid
	}
	rows, err := New(database).overdueCandidates(ctx)
	if err != nil {
		return nil, err
	}
	var result []OverdueLease
	for _, row := range rows {
		summary, err := Balance(ctx, database, row.ID, today)
		if err != nil {
			return nil, err
		}
		if summary.DaysOverdue > 0 {
			result = append(result, OverdueLease{LeaseID: row.ID, PropertyName: row.PropertyName, UnitName: row.UnitName, TenantNames: row.TenantNames, Summary: summary})
		}
	}
	return result, nil
}

// Get returns an immutable ledger entry and its void audit fields.
func Get(ctx context.Context, database *sql.DB, id int64) (Entry, error) {
	value, err := New(database).getEntry(ctx, id)
	if err == sql.ErrNoRows {
		return Entry{}, ErrNotFound
	}
	return fromRecord(value), err
}
