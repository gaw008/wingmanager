package jobs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/db"
	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/property"
)

func TestStartupSyncAndCancellationStopsInflightRequest(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.Migrate(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	p, err := property.Create(t.Context(), database, property.Details{Name: "Home"}, "Unit")
	if err != nil {
		t.Fatal(err)
	}
	units, err := property.ListUnits(t.Context(), database, p, false)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile("../ical/testdata/airbnb.ics")
	if err != nil {
		t.Fatal(err)
	}
	entered, cancelled := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			close(entered)
			<-r.Context().Done()
			close(cancelled)
			return
		}
		if r.URL.Path == "/error" {
			w.WriteHeader(500)
			return
		}
		_, _ = w.Write(body)
	}))
	defer server.Close()
	for _, path := range []string{"/error", "/normal", "/slow"} {
		if _, err := ical.Add(t.Context(), database, units[0].ID, booking.ChannelAirbnb, server.URL+path); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); Run(ctx, database, ical.NewService(database, nil, time.Now)) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("startup did not reach all feeds")
	}
	// Reaching feed three proves feed two committed before it, despite feed one's failure.
	feeds, err := ical.List(t.Context(), database)
	if err != nil || feeds[1].SyncGeneration != 1 || !feeds[0].LastError.Valid {
		t.Fatalf("feeds=%v err=%v", feeds, err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("jobs did not stop")
	}
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("network request not cancelled")
	}
	if SyncInterval != 15*time.Minute {
		t.Fatalf("interval=%v", SyncInterval)
	}
}
