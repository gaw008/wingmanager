package property

import (
	"context"
	"database/sql"

	"github.com/gaw008/wingmanager/internal/civildate"
)

// VacantUnit has no confirmed booking or active lease occupying today. Future
// bookings and draft leases do not make a currently empty unit occupied.
type VacantUnit struct {
	ID                     int64
	PropertyName, UnitName string
}

// Vacant lists active units with no occupancy on today.
func Vacant(ctx context.Context, database *sql.DB, today civildate.Date) ([]VacantUnit, error) {
	if today.IsZero() {
		return nil, civildate.ErrDate
	}
	rows, err := New(database).vacantUnits(ctx, today.String())
	if err != nil {
		return nil, err
	}
	result := make([]VacantUnit, len(rows))
	for i, row := range rows {
		result[i] = VacantUnit(row)
	}
	return result, nil
}
