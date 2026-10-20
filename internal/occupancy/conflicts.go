package occupancy

import (
	"context"
	"database/sql"

	"github.com/gaw008/wingmanager/internal/civildate"
)

// Occupant identifies an occupied interval and its source record.
type Occupant struct {
	ID                       int64
	Source, StartsOn, EndsOn string
}

type Conflict struct {
	PropertyName, UnitName string
	First, Second          Occupant
}

// FutureConflicts reports each pair once when its overlap includes today or a
// later date. Stay and lease boundaries use this package's interval semantics.
func FutureConflicts(ctx context.Context, database *sql.DB, today civildate.Date) ([]Conflict, error) {
	if today.IsZero() {
		return nil, ErrRange
	}
	rows, err := New(database).futureOccupants(ctx, today.String())
	if err != nil {
		return nil, err
	}
	ranges := make([]Range, len(rows))
	for i, row := range rows {
		from, err := civildate.Parse(row.StartsOn)
		if err != nil {
			return nil, err
		}
		var to *civildate.Date
		if row.EndsOn != "" {
			date, err := civildate.Parse(row.EndsOn)
			if err != nil {
				return nil, err
			}
			to = &date
		}
		if row.Source == "lease" {
			ranges[i], err = Lease(from, to)
		} else {
			ranges[i], err = Stay(from, *to)
		}
		if err != nil {
			return nil, err
		}
	}
	var result []Conflict
	for i, first := range rows {
		for j := i + 1; j < len(rows) && rows[j].UnitID == first.UnitID; j++ {
			second := rows[j]
			if ranges[i].Overlaps(ranges[j]) {
				result = append(result, Conflict{PropertyName: first.PropertyName, UnitName: first.UnitName,
					First:  Occupant{ID: first.ID, Source: first.Source, StartsOn: first.StartsOn, EndsOn: first.EndsOn},
					Second: Occupant{ID: second.ID, Source: second.Source, StartsOn: second.StartsOn, EndsOn: second.EndsOn}})
			}
		}
	}
	return result, nil
}
