package jobs

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gaw008/wingmanager/internal/db"
	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/lease"
	"github.com/gaw008/wingmanager/internal/ledger"
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
)

func TestStartupPostsCatchupBeforeAutoEnding(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := db.Migrate(t.Context(), d); err != nil {
		t.Fatal(err)
	}
	p, err := property.Create(t.Context(), d, property.Details{Name: "Home"}, "Unit")
	if err != nil {
		t.Fatal(err)
	}
	units, err := property.ListUnits(t.Context(), d, p, false)
	if err != nil {
		t.Fatal(err)
	}
	id, err := lease.Create(t.Context(), d, lease.Details{UnitID: units[0].ID, StartsOn: "2026-01-01", EndsOn: "2026-03-02", RentCents: 310000, DueDay: 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.AddTenant(t.Context(), d, id, 0, &person.Details{Name: "Tenant"}); err != nil {
		t.Fatal(err)
	}
	if err := lease.Activate(t.Context(), d, id); err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC) }
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	defer func() { cancel(); <-done }()
	go func() { defer close(done); run(ctx, d, ical.NewService(d, nil, now), now) }()
	deadline := time.After(3 * time.Second)
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("startup lease job did not complete")
		case <-poll.C:
			v, err := lease.Get(t.Context(), d, id)
			if err != nil {
				t.Fatal(err)
			}
			if v.Status != lease.StatusEnded {
				continue
			}
			entries, err := ledger.List(t.Context(), d, id)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 3 || entries[2].AmountCents != 20000 || entries[2].OccurredOn != "2026-03-02" {
				t.Fatalf("%+v", entries)
			}
			return
		}
	}
}
