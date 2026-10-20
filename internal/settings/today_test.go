package settings

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/gaw008/wingmanager/internal/db"
)

func TestTodayUsesConfiguredTimezone(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.Migrate(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	value, err := Get(t.Context(), database)
	if err != nil {
		t.Fatal(err)
	}
	clock := func() time.Time { return time.Date(2026, time.October, 20, 1, 30, 0, 0, time.UTC) }
	for _, tt := range []struct {
		zone string
		want string
	}{
		{"America/Los_Angeles", "2026-10-19"},
		{"UTC", "2026-10-20"},
		{"Asia/Tokyo", "2026-10-20"},
	} {
		t.Run(tt.zone, func(t *testing.T) {
			value.Timezone = tt.zone
			if err := Save(t.Context(), database, value); err != nil {
				t.Fatal(err)
			}
			got, err := Today(t.Context(), database, clock)
			if err != nil || got.String() != tt.want {
				t.Fatalf("Today = %s, %v; want %s", got, err, tt.want)
			}
		})
	}
}
