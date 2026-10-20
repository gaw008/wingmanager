package ledger

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/gaw008/wingmanager/internal/civildate"
)

// CategoryTotal retains the kind because payment categories describe receipt methods.
// MonthlyCents is indexed January through December, including months without entries.
type CategoryTotal struct {
	Kind, Category string
	Line           int
	AmountCents    int64
	MonthlyCents   [12]int64
}

type MonthTotal struct {
	Month                               string
	IncomeCents, ExpenseCents, NetCents int64
}

// Totals contains cash-basis category and monthly totals, excluding deposits and voids.
type Totals struct {
	Income, Expenses                    []CategoryTotal
	IncomeCents, ExpenseCents, NetCents int64
	Months                              [12]MonthTotal
}

type PropertyReport struct {
	PropertyID   int64
	PropertyName string
	Totals
}

// YearReport uses one database snapshot for totals, per-property summaries, and the
// audit entries. Entries includes all kinds and voids so exports can be reconciled.
type YearReport struct {
	Year       int
	PropertyID int64
	Totals
	Properties []PropertyReport
	Entries    []NamedEntry
}

func yearBounds(year int) (string, string) {
	return fmt.Sprintf("%04d-01-01", year), fmt.Sprintf("%04d-12-31", year)
}

func emptyTotals(year int) Totals {
	var result Totals
	for _, kind := range []string{KindPayment, KindIncome, KindExpense} {
		for _, category := range Categories(kind) {
			row := CategoryTotal{Kind: kind, Category: category, Line: ScheduleELine(kind, category)}
			if kind == KindExpense {
				result.Expenses = append(result.Expenses, row)
			} else {
				result.Income = append(result.Income, row)
			}
		}
	}
	for i := range result.Months {
		result.Months[i].Month = fmt.Sprintf("%04d-%02d", year, i+1)
	}
	return result
}

func (v *Totals) add(e Entry) {
	if e.VoidedAt != "" {
		return
	}
	if e.Kind != KindPayment && e.Kind != KindIncome && e.Kind != KindExpense {
		return
	}
	// Entry dates are canonical civil dates, validated at the ledger write boundary.
	month := int(e.OccurredOn[5]-'0')*10 + int(e.OccurredOn[6]-'0')
	index := month - 1
	rows := v.Income
	if e.Kind == KindExpense {
		rows = v.Expenses
		v.ExpenseCents += e.AmountCents
		v.Months[index].ExpenseCents += e.AmountCents
	} else {
		v.IncomeCents += e.AmountCents
		v.Months[index].IncomeCents += e.AmountCents
	}
	for i := range rows {
		if rows[i].Kind == e.Kind && rows[i].Category == e.Category {
			rows[i].AmountCents += e.AmountCents
			rows[i].MonthlyCents[index] += e.AmountCents
			break
		}
	}
	v.NetCents = v.IncomeCents - v.ExpenseCents
	v.Months[index].NetCents = v.Months[index].IncomeCents - v.Months[index].ExpenseCents
}

// Report returns the cash-basis natural-year report. PropertyID zero selects all
// properties, including archived ones; a positive ID selects exactly that property.
// Receipt dates, including deposit_applied payments, determine income attribution.
func Report(ctx context.Context, database *sql.DB, year int, propertyID int64) (YearReport, error) {
	return report(ctx, database, year, propertyID, "")
}

// ReportToDate uses the report's cash-basis calculation for all properties from
// January 1 through today, inclusive. Future-dated entries are excluded.
func ReportToDate(ctx context.Context, database *sql.DB, today civildate.Date) (YearReport, error) {
	if today.IsZero() {
		return YearReport{}, ErrInvalid
	}
	return report(ctx, database, today.Year(), 0, today.String())
}

func report(ctx context.Context, database *sql.DB, year int, propertyID int64, through string) (YearReport, error) {
	if year < 1 || year > 9999 || propertyID < 0 {
		return YearReport{}, ErrInvalid
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return YearReport{}, err
	}
	defer tx.Rollback()
	q := New(tx)
	properties, err := q.reportProperties(ctx, propertyID)
	if err != nil {
		return YearReport{}, err
	}
	if propertyID != 0 && len(properties) == 0 {
		return YearReport{}, ErrNotFound
	}
	entries, err := loadEntries(ctx, q, Filter{Year: year, PropertyID: propertyID})
	if err != nil {
		return YearReport{}, err
	}
	if through != "" {
		past := entries[:0]
		for _, entry := range entries {
			if entry.OccurredOn <= through {
				past = append(past, entry)
			}
		}
		entries = past
	}
	if err := tx.Commit(); err != nil {
		return YearReport{}, err
	}
	result := YearReport{Year: year, PropertyID: propertyID, Totals: emptyTotals(year), Entries: entries}
	byProperty := make(map[int64]int, len(properties))
	for _, p := range properties {
		byProperty[p.ID] = len(result.Properties)
		result.Properties = append(result.Properties, PropertyReport{PropertyID: p.ID, PropertyName: p.Name, Totals: emptyTotals(year)})
	}
	for _, e := range entries {
		result.Totals.add(e.Entry)
		result.Properties[byProperty[e.PropertyID]].Totals.add(e.Entry)
	}
	return result, nil
}

// LineTotal is a reference Schedule E line, not a tax calculation.
type LineTotal struct {
	Line        int
	AmountCents int64
}

// ScheduleE groups all receipts on rental income and each expense on its reference
// line. Depreciation is excluded because wingmanager has no asset ledger.
func (v Totals) ScheduleE() []LineTotal {
	result := []LineTotal{{Line: rentalIncomeLine, AmountCents: v.IncomeCents}}
	for _, row := range v.Expenses {
		result = append(result, LineTotal{Line: row.Line, AmountCents: row.AmountCents})
	}
	return result
}
