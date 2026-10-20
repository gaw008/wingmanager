// Package jobs runs the instance's context-bound background work.
package jobs

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/gaw008/wingmanager/internal/civildate"
	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/lease"
	"github.com/gaw008/wingmanager/internal/ledger"
	"github.com/gaw008/wingmanager/internal/settings"
)

// SyncInterval is the feed polling cadence specified in PLAN section 6.1.
const SyncInterval = 15 * time.Minute

// Run posts rent and auto-ends leases at startup and once per local day, and
// synchronizes feeds at startup and on each tick until ctx is cancelled.
// Callers own its goroutine and must wait for it before closing the database.
func Run(ctx context.Context, database *sql.DB, service *ical.Service) {
	run(ctx, database, service, time.Now)
}

func run(ctx context.Context, database *sql.DB, service *ical.Service, now func() time.Time) {
	var lastRentDay string
	ticker := time.NewTicker(SyncInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		today, err := settings.Today(ctx, database, now)
		if err == nil && today.String() != lastRentDay {
			err = Daily(ctx, database, today)
			if err == nil {
				lastRentDay = today.String()
			}
		}
		if err != nil && ctx.Err() == nil {
			log.Printf("daily lease jobs: %v", err)
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

// Daily catches up rent before ending expired leases, preserving missed final
// periods after downtime. Every lease's posting batch is atomic and repeatable.
func Daily(ctx context.Context, database *sql.DB, today civildate.Date) error {
	if err := ledger.PostRent(ctx, database, today); err != nil {
		return err
	}
	return lease.AutoEnd(ctx, database, today)
}
