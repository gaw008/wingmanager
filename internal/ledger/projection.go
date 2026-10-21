package ledger

import (
	"cmp"
	"context"
	"database/sql"
	"slices"
	"time"

	"github.com/gaw008/wingmanager/internal/civildate"
	"github.com/gaw008/wingmanager/internal/lease"
)

const (
	StatusPaid    = "paid"
	StatusPartial = "partial"
	StatusUnpaid  = "unpaid"
	StatusNotDue  = "not_due"
)

// BalancedEntry includes the tenant balance after this entry in date/ID order.
// Voided entries and deposits leave the tenant balance unchanged.
type BalancedEntry struct {
	Entry
	BalanceCents int64
}

// RentPeriod describes a posted rent charge or the next unposted future period.
// PaidCents includes credits as well as payments allocated by FIFO.
type RentPeriod struct {
	Period, DueOn, Status  string
	AmountCents, PaidCents int64
	PaidPercent            int
}

// Projection contains unfiltered ledger rows and rent periods in chronological order.
type Projection struct {
	Summary
	Entries []BalancedEntry
	Periods []RentPeriod
}

// PaymentAllocation is the part of a proposed payment applied to one open charge.
// Period is empty for a manual charge; Settled means the charge is paid in full.
type PaymentAllocation struct {
	EntryID, AmountCents int64
	Period, OccurredOn   string
	Settled              bool
}

// PaymentPreview describes a positive payment without persisting anything.
// BalanceCents is signed; PrepaymentCents is total credit after the payment, and
// ExcessCents is the part of this payment left after settling all posted charges.
type PaymentPreview struct {
	Allocations                                []PaymentAllocation
	BalanceCents, PrepaymentCents, ExcessCents int64
}

type allocatedCharge struct {
	Entry
	paid int64
}

// allocate is the single FIFO rule for balances, overdue, rent status and previews.
// Credits and payments offset all charges in date/ID order, including prepayments.
func allocate(entries []Entry) (Summary, []BalancedEntry, []allocatedCharge) {
	var summary Summary
	var offsets int64
	rows := make([]BalancedEntry, 0, len(entries))
	var charges []allocatedCharge
	for _, entry := range entries {
		if entry.VoidedAt == "" {
			switch entry.Kind {
			case KindCharge:
				summary.BalanceCents += entry.AmountCents
				charges = append(charges, allocatedCharge{Entry: entry})
			case KindCredit, KindPayment:
				summary.BalanceCents -= entry.AmountCents
				offsets += entry.AmountCents
			case KindDepositIn:
				summary.DepositHeldCents += entry.AmountCents
			case KindDepositOut:
				summary.DepositHeldCents -= entry.AmountCents
			}
		}
		rows = append(rows, BalancedEntry{Entry: entry, BalanceCents: summary.BalanceCents})
	}
	for i := range charges {
		charges[i].paid = min(offsets, charges[i].AmountCents)
		offsets -= charges[i].paid
	}
	return summary, rows, charges
}

func overdueSummary(summary Summary, charges []allocatedCharge, today civildate.Date) (Summary, error) {
	for _, charge := range charges {
		if charge.paid == charge.AmountCents {
			continue
		}
		summary.OldestUnpaidOn = charge.OccurredOn
		due, err := civildate.Parse(charge.OccurredOn)
		if err != nil {
			return Summary{}, err
		}
		if due.Compare(today) < 0 {
			summary.DaysOverdue = int(today.MidnightUTC().Sub(due.MidnightUTC()).Hours() / 24)
		}
		break
	}
	return summary, nil
}

// Project derives display data from the complete ledger. Existing rent charges
// keep their saved amount; only the next unposted period uses current lease terms.
func Project(ctx context.Context, database *sql.DB, value lease.Lease, today civildate.Date) (Projection, error) {
	if today.IsZero() {
		return Projection{}, ErrInvalid
	}
	entries, err := List(ctx, database, value.ID)
	if err != nil {
		return Projection{}, err
	}
	summary, rows, charges := allocate(entries)
	summary, err = overdueSummary(summary, charges, today)
	if err != nil {
		return Projection{}, err
	}
	result := Projection{Summary: summary, Entries: rows}
	posted := make(map[string]bool)
	for _, charge := range charges {
		if charge.Period == "" {
			continue
		}
		status := StatusUnpaid
		switch {
		case charge.paid == charge.AmountCents:
			status = StatusPaid
		case charge.paid > 0:
			status = StatusPartial
		case charge.OccurredOn > today.String():
			status = StatusNotDue
		}
		result.Periods = append(result.Periods, RentPeriod{Period: charge.Period, DueOn: charge.OccurredOn, Status: status, AmountCents: charge.AmountCents, PaidCents: charge.paid, PaidPercent: percent(charge.paid, charge.AmountCents)})
		posted[charge.Period] = true
	}
	if value.Status == lease.StatusActive {
		month := today.MonthStart()
		if value.StartsOn > month.String() {
			start, err := civildate.Parse(value.StartsOn)
			if err != nil {
				return Projection{}, err
			}
			month = start.MonthStart()
		}
		for {
			if value.EndsOn != "" && month.String() > value.EndsOn {
				break
			}
			rent, err := lease.PeriodRent(value, month)
			if err != nil {
				return Projection{}, err
			}
			if rent.DueOn > today.String() && !posted[rent.Period] {
				result.Periods = append(result.Periods, RentPeriod{Period: rent.Period, DueOn: rent.DueOn, Status: StatusNotDue, AmountCents: rent.AmountCents})
				break
			}
			next := month.MidnightUTC().AddDate(0, 1, 0)
			if next.Year() > 9999 {
				break
			}
			month, err = civildate.Parse(next.Format(time.DateOnly))
			if err != nil {
				return Projection{}, err
			}
		}
	}
	slices.SortFunc(result.Periods, func(a, b RentPeriod) int {
		return cmp.Compare(a.Period, b.Period)
	})
	return result, nil
}

// percent avoids overflow when the amount is close to int64's upper bound.
func percent(paid, amount int64) int {
	return int(float64(paid) / float64(amount) * 100)
}

// PreviewPayment allocates a positive amount FIFO against currently open charges.
// It uses the same credits, void handling and order as Balance and Project.
func PreviewPayment(ctx context.Context, database *sql.DB, leaseID, amount int64) (PaymentPreview, error) {
	if amount <= 0 {
		return PaymentPreview{}, ErrInvalid
	}
	entries, err := List(ctx, database, leaseID)
	if err != nil {
		return PaymentPreview{}, err
	}
	summary, _, charges := allocate(entries)
	result := PaymentPreview{BalanceCents: summary.BalanceCents - amount, ExcessCents: amount}
	for _, charge := range charges {
		take := min(result.ExcessCents, charge.AmountCents-charge.paid)
		if take > 0 {
			result.Allocations = append(result.Allocations, PaymentAllocation{EntryID: charge.ID, AmountCents: take, Period: charge.Period, OccurredOn: charge.OccurredOn, Settled: take == charge.AmountCents-charge.paid})
			result.ExcessCents -= take
		}
	}
	result.PrepaymentCents = max(-result.BalanceCents, 0)
	return result, nil
}
