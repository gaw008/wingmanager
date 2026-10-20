package lease

import (
	"fmt"

	"github.com/gaw008/wingmanager/internal/civildate"
)

// Rent is one natural month's charge. DueOn is a civil date, AmountCents is rounded half-up.
type Rent struct {
	Period, DueOn string
	AmountCents   int64
}

// PeriodRent is the sole rent and proration rule: intersect the lease with the
// calendar month, using the current rent. Posted entries retain their saved amount.
func PeriodRent(v Lease, month civildate.Date) (Rent, error) {
	if err := validate(v.Details); err != nil {
		return Rent{}, err
	}
	if month.IsZero() {
		return Rent{}, ErrInvalid
	}
	first := month.MonthStart()
	total := int64(first.MidnightUTC().AddDate(0, 1, -1).Day())
	start, err := civildate.Parse(v.StartsOn)
	if err != nil {
		return Rent{}, err
	}
	from, to := first, first.AddDays(int(total)-1)
	if start.Compare(from) > 0 {
		from = start
	}
	if v.EndsOn != "" {
		end, err := civildate.Parse(v.EndsOn)
		if err != nil {
			return Rent{}, err
		}
		if end.Compare(to) < 0 {
			to = end
		}
	}
	if from.Compare(to) > 0 {
		return Rent{}, ErrInvalid
	}
	days := int64(to.Day() - from.Day() + 1)
	// Divide before multiplying to retain int64 range even at the largest rent.
	amount := v.RentCents/total*days + (v.RentCents%total*days+total/2)/total
	due := first.AddDays(int(v.DueDay) - 1)
	if start.Compare(first) > 0 {
		due = start
	}
	if v.EndsOn != "" && due.String() > v.EndsOn {
		// PLAN-DEVIATION: when the final day precedes due_day, make final rent due
		// on ends_on so daily auto-end cannot discard that month's prorated charge.
		due = to
	}
	return Rent{Period: fmt.Sprintf("%04d-%02d", first.Year(), first.Month()), DueOn: due.String(), AmountCents: amount}, nil
}
