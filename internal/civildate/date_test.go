package civildate

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	for _, value := range []string{"0001-01-01", "2024-02-29", "2026-10-20", "9999-12-31"} {
		t.Run(value, func(t *testing.T) {
			date, err := Parse(value)
			if err != nil || date.IsZero() || date.String() != value {
				t.Fatalf("Parse(%q) = %v, %v", value, date, err)
			}
		})
	}
	for _, value := range []string{
		"", "0000-01-01", "10000-01-01", "2026-02-30", "2026-02-29", "1900-02-29",
		"2026-04-31", "2026-13-01", "2026-00-01", "2026-01-00", "2026-01-32",
		"2026-1-01", "2026-01-1", "26-01-01", "2026-01-01 ", " 2026-01-01",
		"2026/01/01", "2026-01-01T00:00:00Z",
	} {
		t.Run(value, func(t *testing.T) {
			date, err := Parse(value)
			if err != ErrDate || !date.IsZero() {
				t.Fatalf("Parse(%q) = %v, %v; want zero date and ErrDate", value, date, err)
			}
		})
	}
}

func TestDateArithmetic(t *testing.T) {
	for _, tt := range []struct {
		from string
		days int
		want string
	}{
		{"2024-02-28", 1, "2024-02-29"},
		{"2024-02-28", 2, "2024-03-01"},
		{"2026-02-28", 1, "2026-03-01"},
		{"2026-01-01", -1, "2025-12-31"},
		{"2026-03-08", 1, "2026-03-09"},
		{"9999-12-31", 1, "10000-01-01"},
	} {
		date, err := Parse(tt.from)
		if err != nil {
			t.Fatal(err)
		}
		if got := date.AddDays(tt.days).String(); got != tt.want {
			t.Errorf("%s + %d days = %s; want %s", tt.from, tt.days, got, tt.want)
		}
	}
	date, err := Parse("2026-10-20")
	if err != nil {
		t.Fatal(err)
	}
	if date.Year() != 2026 || date.Month() != time.October || date.Day() != 20 || date.MonthStart().String() != "2026-10-01" {
		t.Fatalf("date parts or month start do not match %s", date)
	}
	if date.Compare(date) != 0 || date.Compare(date.AddDays(1)) != -1 || date.Compare(date.AddDays(-1)) != 1 {
		t.Fatal("date comparison does not follow calendar ordering")
	}
	var zero Date
	if !zero.IsZero() || zero.String() != "" {
		t.Fatal("zero Date must represent an absent date")
	}
}

func TestMidnightUTC(t *testing.T) {
	for _, value := range []string{"0001-01-01", "2024-02-29", "2026-03-08", "2026-11-01", "9999-12-31"} {
		date, err := Parse(value)
		if err != nil {
			t.Fatal(err)
		}
		got := date.MidnightUTC()
		if got.Format(time.DateOnly) != value || got.Location() != time.UTC || got.Hour() != 0 || got.Minute() != 0 || got.Second() != 0 || got.Nanosecond() != 0 {
			t.Fatalf("%s midnight UTC = %v", value, got)
		}
	}
	if got := (Date{}).MidnightUTC(); !got.IsZero() {
		t.Fatalf("absent date = %v; want zero time", got)
	}
}
