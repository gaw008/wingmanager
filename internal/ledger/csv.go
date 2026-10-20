package ledger

import (
	"encoding/csv"
	"io"
	"strconv"

	"github.com/gaw008/wingmanager/internal/money"
)

const (
	DisclaimerChinese = "按 Schedule E 行号整理，仅供参考，不构成税务建议；行号以当年表格为准"
	DisclaimerEnglish = "Organized by Schedule E line number for reference only; not tax advice. Verify line numbers against the form for the tax year."
)

// WriteScheduleE writes one summary row per property, including zero-activity
// properties. All amounts are decimal dollars; a bilingual disclaimer heads the CSV.
func WriteScheduleE(w io.Writer, report YearReport) error {
	header := []string{"year", "property_id", "property"}
	for _, line := range report.ScheduleE() {
		header = append(header, "line_"+strconv.Itoa(line.Line))
	}
	header = append(header, "income", "expense", "net")
	rows := [][]string{csvDisclaimer(len(header)), header}
	for _, p := range report.Properties {
		row := []string{strconv.Itoa(report.Year), strconv.FormatInt(p.PropertyID, 10), p.PropertyName}
		for _, line := range p.ScheduleE() {
			row = append(row, money.Format(line.AmountCents))
		}
		row = append(row, money.Format(p.IncomeCents), money.Format(p.ExpenseCents), money.Format(p.NetCents))
		rows = append(rows, row)
	}
	return csv.NewWriter(w).WriteAll(rows)
}

// WriteLedger writes every entry in the report year and scope, including deposits,
// noncash charges/credits and void audit fields. Amounts remain positive by kind.
func WriteLedger(w io.Writer, report YearReport) error {
	header := []string{"id", "property_id", "property", "lease_id", "booking_id", "kind", "category", "amount", "occurred_on", "period", "memo", "voided_at", "void_reason"}
	rows := [][]string{csvDisclaimer(len(header)), header}
	for _, e := range report.Entries {
		rows = append(rows, []string{strconv.FormatInt(e.ID, 10), strconv.FormatInt(e.PropertyID, 10), e.PropertyName, optionalID(e.LeaseID), optionalID(e.BookingID), e.Kind, e.Category, money.Format(e.AmountCents), e.OccurredOn, e.Period, e.Memo, e.VoidedAt, e.VoidReason})
	}
	return csv.NewWriter(w).WriteAll(rows)
}

func csvDisclaimer(columns int) []string {
	row := make([]string, columns)
	row[0] = DisclaimerChinese + " / " + DisclaimerEnglish
	return row
}

func optionalID(id int64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}
