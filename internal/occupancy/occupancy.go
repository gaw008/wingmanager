// Package occupancy owns half-open occupancy intervals and overlap semantics.
package occupancy

import (
	"errors"

	"github.com/gaw008/wingmanager/internal/civildate"
)

var ErrRange = errors.New("invalid occupancy range")

// Range represents [from, to). Only the constructors can establish its bounds.
type Range struct {
	from civildate.Date
	to   civildate.Date // A missing endpoint means the range is open-ended.
}

// Stay excludes the checkout date; checkout must be after check-in.
func Stay(checkIn, checkOut civildate.Date) (Range, error) {
	if checkIn.IsZero() || checkOut.IsZero() || checkOut.Compare(checkIn) <= 0 {
		return Range{}, ErrRange
	}
	return Range{from: checkIn, to: checkOut}, nil
}

// Lease includes its last day. A nil endsOn means the lease is open-ended.
func Lease(startsOn civildate.Date, endsOn *civildate.Date) (Range, error) {
	if startsOn.IsZero() {
		return Range{}, ErrRange
	}
	if endsOn == nil {
		return Range{from: startsOn}, nil
	}
	if endsOn.IsZero() || endsOn.Compare(startsOn) < 0 {
		return Range{}, ErrRange
	}
	return Range{from: startsOn, to: endsOn.AddDays(1)}, nil
}

// Overlaps reports whether both ranges occupy any common date.
func (r Range) Overlaps(other Range) bool {
	return (r.to.IsZero() || other.from.Compare(r.to) < 0) &&
		(other.to.IsZero() || r.from.Compare(other.to) < 0)
}
