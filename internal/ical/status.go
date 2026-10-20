package ical

import (
	"context"
	"database/sql"
	"time"
)

const staleAfter = 24 * time.Hour

// ProblemFeed includes a stale flag even when an error is also present.
type ProblemFeed struct {
	CalendarFeed
	PropertyID             int64
	PropertyName, UnitName string
	Stale                  bool
}

// ProblemFeeds returns errors and feeds without a successful sync in the last
// 24 hours. A feed which has never succeeded is stale immediately.
func ProblemFeeds(ctx context.Context, database *sql.DB, now time.Time) ([]ProblemFeed, error) {
	feeds, err := New(database).statusFeeds(ctx)
	if err != nil {
		return nil, err
	}
	var result []ProblemFeed
	for _, row := range feeds {
		feed := fromRecord(row.FeedRecord)
		stale := feed.LastSuccessAt == ""
		if !stale {
			success, err := time.Parse(time.RFC3339Nano, feed.LastSuccessAt)
			if err != nil {
				return nil, err
			}
			stale = now.Sub(success) > staleAfter
		}
		if stale || feed.LastError != "" {
			result = append(result, ProblemFeed{CalendarFeed: feed, PropertyID: row.PropertyID, PropertyName: row.PropertyName, UnitName: row.UnitName, Stale: stale})
		}
	}
	return result, nil
}
