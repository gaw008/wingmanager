package settings

import (
	"context"
	"database/sql"
	"time"

	"github.com/gaw008/wingmanager/internal/civildate"
)

// Today returns the current civil date in the instance's configured timezone.
// The caller supplies its clock so every date-dependent operation is testable.
func Today(ctx context.Context, database *sql.DB, clock func() time.Time) (civildate.Date, error) {
	value, err := Get(ctx, database)
	if err != nil {
		return civildate.Date{}, err
	}
	location, err := time.LoadLocation(value.Timezone)
	if err != nil {
		return civildate.Date{}, err
	}
	return civildate.Parse(clock().In(location).Format(time.DateOnly))
}
