package ledger

import (
	"context"
	"database/sql"
	"time"

	"github.com/gaw008/wingmanager/internal/civildate"
	"github.com/gaw008/wingmanager/internal/lease"
)

// PostRent performs full catch-up for every active lease, one transaction per lease.
// Even out-of-order runs are safe: the unique period index preserves posted amounts.
func PostRent(ctx context.Context, database *sql.DB, today civildate.Date) error {
	if today.IsZero() {
		return ErrInvalid
	}
	leases, err := lease.List(ctx, database, lease.StatusActive)
	if err != nil {
		return err
	}
	for _, v := range leases {
		if err := PostLeaseRent(ctx, database, v.ID, today); err != nil {
			return err
		}
	}
	return nil
}

// PostLeaseRent re-reads status and terms under the IMMEDIATE write lock so rent
// changes, ending, and concurrent posting serialize with the whole catch-up batch.
func PostLeaseRent(ctx context.Context, database *sql.DB, id int64, today civildate.Date) error {
	if today.IsZero() {
		return ErrInvalid
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	v, err := lease.GetInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if v.Status != lease.StatusActive {
		return tx.Commit()
	}
	start, err := civildate.Parse(v.StartsOn)
	if err != nil {
		return err
	}
	last := today.String()[:7]
	if v.EndsOn != "" && v.EndsOn[:7] < last {
		last = v.EndsOn[:7]
	}
	for month := start.MonthStart(); month.String()[:7] <= last; {
		rent, err := lease.PeriodRent(v, month)
		if err != nil {
			return err
		}
		// PLAN-DEVIATION: a prorated amount rounded to zero cannot be represented
		// by the positive-only ledger schema; there is no debt to post for that period.
		if rent.DueOn <= today.String() && rent.AmountCents > 0 {
			if err := New(tx).postRent(ctx, postRentParams{ID: id, AmountCents: rent.AmountCents, OccurredOn: rent.DueOn, Period: sql.NullString{String: rent.Period, Valid: true}}); err != nil {
				return err
			}
		}
		next := month.MidnightUTC().AddDate(0, 1, 0)
		if next.Year() > 9999 {
			break
		}
		month, err = civildate.Parse(next.Format(time.DateOnly))
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
