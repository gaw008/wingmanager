package occupancy

import (
	"testing"

	"github.com/gaw008/wingmanager/internal/civildate"
)

func date(t *testing.T, value string) civildate.Date {
	t.Helper()
	parsed, err := civildate.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestOverlaps(t *testing.T) {
	tests := []struct {
		name     string
		from     string
		to       string
		lease    bool
		stayFrom string
		stayTo   string
		want     bool
	}{
		{"same-day turnover", "2026-10-01", "2026-10-05", false, "2026-10-05", "2026-10-08", false},
		{"identical", "2026-10-01", "2026-10-05", false, "2026-10-01", "2026-10-05", true},
		{"contains", "2026-10-01", "2026-10-10", false, "2026-10-03", "2026-10-05", true},
		{"contained", "2026-10-03", "2026-10-05", false, "2026-10-01", "2026-10-10", true},
		{"partial overlap left", "2026-10-03", "2026-10-07", false, "2026-10-01", "2026-10-05", true},
		{"partial overlap right", "2026-10-03", "2026-10-07", false, "2026-10-05", "2026-10-09", true},
		{"separated", "2026-10-01", "2026-10-03", false, "2026-10-05", "2026-10-08", false},
		{"one night same date", "2026-10-01", "2026-10-02", false, "2026-10-01", "2026-10-02", true},
		{"one night next date", "2026-10-01", "2026-10-02", false, "2026-10-02", "2026-10-03", false},
		{"lease includes final day", "2026-10-01", "2026-10-05", true, "2026-10-05", "2026-10-06", true},
		{"lease next day free", "2026-10-01", "2026-10-05", true, "2026-10-06", "2026-10-07", false},
		{"one-day lease", "2026-10-05", "2026-10-05", true, "2026-10-05", "2026-10-06", true},
		{"open-ended lease far future", "2026-10-01", "", true, "9999-12-30", "9999-12-31", true},
		{"open-ended lease before start", "2026-10-01", "", true, "2026-09-01", "2026-10-01", false},
		{"lease final supported date", "9999-12-31", "9999-12-31", true, "9999-12-30", "9999-12-31", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var first Range
			var err error
			if tt.lease {
				var end *civildate.Date
				if tt.to != "" {
					value := date(t, tt.to)
					end = &value
				}
				first, err = Lease(date(t, tt.from), end)
			} else {
				first, err = Stay(date(t, tt.from), date(t, tt.to))
			}
			if err != nil {
				t.Fatal(err)
			}
			second, err := Stay(date(t, tt.stayFrom), date(t, tt.stayTo))
			if err != nil {
				t.Fatal(err)
			}
			if got := first.Overlaps(second); got != tt.want {
				t.Errorf("first.Overlaps(second) = %t; want %t", got, tt.want)
			}
			if got := second.Overlaps(first); got != tt.want {
				t.Errorf("second.Overlaps(first) = %t; want %t", got, tt.want)
			}
		})
	}
}

func TestInvalidRanges(t *testing.T) {
	start := date(t, "2026-10-05")
	before := start.AddDays(-1)
	var zero civildate.Date
	for _, end := range []civildate.Date{zero, before, start} {
		if _, err := Stay(start, end); err != ErrRange {
			t.Errorf("Stay(%s, %s): %v; want ErrRange", start, end, err)
		}
	}
	if _, err := Stay(zero, start); err != ErrRange {
		t.Errorf("Stay with missing check-in: %v", err)
	}
	for _, end := range []*civildate.Date{&zero, &before} {
		if _, err := Lease(start, end); err != ErrRange {
			t.Errorf("Lease(%s, %s): %v; want ErrRange", start, end, err)
		}
	}
	if _, err := Lease(zero, nil); err != ErrRange {
		t.Errorf("Lease with missing start: %v", err)
	}
}
