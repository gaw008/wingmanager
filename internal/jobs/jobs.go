// Package jobs runs the instance's context-bound background work.
package jobs

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/gaw008/wingmanager/internal/ical"
)

// SyncInterval is the feed polling cadence specified in PLAN section 6.1.
const SyncInterval = 15 * time.Minute

// Run synchronizes all feeds at startup and on each tick, until ctx is cancelled.
// Callers own its goroutine and must wait for it before closing the database.
func Run(ctx context.Context, database *sql.DB, service *ical.Service) {
	ticker := time.NewTicker(SyncInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		feeds, err := ical.List(ctx, database)
		if err != nil && ctx.Err() == nil {
			log.Printf("list calendar feeds: %v", err)
		}
		for _, feed := range feeds {
			if ctx.Err() != nil {
				return
			}
			if err := service.Sync(ctx, feed.ID); err != nil && ctx.Err() == nil {
				log.Printf("sync feed %d: %v", feed.ID, err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
