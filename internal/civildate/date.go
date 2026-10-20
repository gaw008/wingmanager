// Package civildate owns calendar-date parsing and arithmetic without a time zone.
package civildate

import (
	"cmp"
	"errors"
	"fmt"
	"time"
)

var ErrDate = errors.New("invalid date: expected YYYY-MM-DD")

// Date is a calendar date. Its zero value means no date.
type Date struct {
	year  int
	month time.Month
	day   int
}

// Parse accepts only canonical YYYY-MM-DD calendar dates in years 0001–9999.
func Parse(value string) (Date, error) {
	parsed, err := time.Parse(time.DateOnly, value)
	if err != nil || parsed.Year() < 1 || parsed.Format(time.DateOnly) != value {
		return Date{}, ErrDate
	}
	return fromTime(parsed), nil
}

func fromTime(value time.Time) Date {
	year, month, day := value.Date()
	return Date{year: year, month: month, day: day}
}

func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.year, d.month, d.day)
}

// MidnightUTC encodes d as midnight UTC for date-only interchange formats.
// It does not represent a local stay instant. A zero Date returns a zero time.
func (d Date) MidnightUTC() time.Time {
	if d.IsZero() {
		return time.Time{}
	}
	return time.Date(d.year, d.month, d.day, 0, 0, 0, 0, time.UTC)
}

// AddDays performs civil arithmetic, independent of daylight saving changes.
// A result beyond year 9999 can represent an exclusive interval endpoint.
func (d Date) AddDays(days int) Date {
	return fromTime(time.Date(d.year, d.month, d.day, 0, 0, 0, 0, time.UTC).AddDate(0, 0, days))
}

// Compare returns -1, 0, or 1 when d is before, equal to, or after other.
func (d Date) Compare(other Date) int {
	if order := cmp.Compare(d.year, other.year); order != 0 {
		return order
	}
	if order := cmp.Compare(d.month, other.month); order != 0 {
		return order
	}
	return cmp.Compare(d.day, other.day)
}

func (d Date) IsZero() bool      { return d.year == 0 }
func (d Date) Year() int         { return d.year }
func (d Date) Month() time.Month { return d.month }
func (d Date) Day() int          { return d.day }

// MonthStart returns the first date in d's month.
func (d Date) MonthStart() Date {
	return Date{year: d.year, month: d.month, day: 1}
}
