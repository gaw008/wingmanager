package lease

import (
	"context"
	"database/sql"

	"github.com/gaw008/wingmanager/internal/civildate"
)

// CurrentLease includes display names for an active lease occupying today.
type CurrentLease struct {
	Lease
	PropertyName, UnitName, TenantNames string
}

// Current returns active leases whose inclusive dates contain today.
func Current(ctx context.Context, database *sql.DB, today civildate.Date) ([]CurrentLease, error) {
	if today.IsZero() {
		return nil, ErrInvalid
	}
	rows, err := New(database).currentLeases(ctx, today.String())
	if err != nil {
		return nil, err
	}
	result := make([]CurrentLease, len(rows))
	for i, row := range rows {
		result[i] = CurrentLease{Lease: fromRecord(row.LeaseRecord), PropertyName: row.PropertyName, UnitName: row.UnitName, TenantNames: row.TenantNames}
	}
	return result, nil
}
