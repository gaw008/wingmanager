package lease

import (
	"math"
	"testing"

	"github.com/gaw008/wingmanager/internal/civildate"
)

func TestPeriodRent(t *testing.T) {
	tests := []struct {
		name, start, end, month string
		rent, due, amount       int64
		on                      string
	}{
		{"first day ignores due day", "2026-01-01", "", "2026-01-01", 310000, 20, 310000, "2026-01-20"},
		{"mid month", "2026-01-16", "", "2026-01-01", 310000, 5, 160000, "2026-01-16"},
		{"final month", "2026-01-01", "2026-03-15", "2026-03-01", 310000, 5, 150000, "2026-03-05"},
		{"same month", "2026-01-10", "2026-01-20", "2026-01-01", 310000, 5, 110000, "2026-01-10"},
		{"february", "2026-02-15", "", "2026-02-01", 280000, 1, 140000, "2026-02-15"},
		{"leap year", "2024-02-15", "", "2024-02-01", 290000, 1, 150000, "2024-02-15"},
		{"half up", "2026-04-16", "2026-04-30", "2026-04-01", 101, 1, 51, "2026-04-16"},
		{"final day before due day", "2026-01-01", "2026-03-02", "2026-03-01", 310000, 20, 20000, "2026-03-02"},
		{"largest year", "9999-12-01", "9999-12-31", "9999-12-01", 310000, 5, 310000, "9999-12-05"},
		{"large rent", "2026-01-01", "", "2026-01-01", math.MaxInt64, 1, math.MaxInt64, "2026-01-01"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			month, err := civildate.Parse(tt.month)
			if err != nil {
				t.Fatal(err)
			}
			got, err := PeriodRent(Lease{Details: Details{UnitID: 1, StartsOn: tt.start, EndsOn: tt.end, RentCents: tt.rent, DueDay: tt.due}}, month)
			if err != nil {
				t.Fatal(err)
			}
			if got.AmountCents != tt.amount || got.DueOn != tt.on || got.Period != tt.month[:7] {
				t.Fatalf("got %+v want amount=%d date=%s", got, tt.amount, tt.on)
			}
		})
	}
}
