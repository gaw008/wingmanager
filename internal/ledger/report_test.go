package ledger_test

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/lease"
	"github.com/gaw008/wingmanager/internal/ledger"
	"github.com/gaw008/wingmanager/internal/money"
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
)

type yearFixture struct {
	database                                              *sql.DB
	home, cabin, homeLease, cabinLease, stay, voidExpense int64
}

func propertyEntry(t *testing.T, d *sql.DB, p, b int64, kind, category string, amount int64, on, memo string) int64 {
	t.Helper()
	id, err := ledger.RecordProperty(t.Context(), d, p, b, ledger.Details{Kind: kind, Category: category, AmountCents: amount, OccurredOn: on, Memo: memo})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func fullYear(t *testing.T) yearFixture {
	t.Helper()
	d, homeLease := setup(t, "2026-12-31", true)
	home, err := lease.Get(t.Context(), d, homeLease)
	if err != nil {
		t.Fatal(err)
	}
	unit, err := property.GetUnit(t.Context(), d, home.UnitID)
	if err != nil {
		t.Fatal(err)
	}
	cabin, err := property.Create(t.Context(), d, property.Details{Name: "Cabin, \"East\"\nHouse"}, "Long term")
	if err != nil {
		t.Fatal(err)
	}
	units, err := property.ListUnits(t.Context(), d, cabin, false)
	if err != nil {
		t.Fatal(err)
	}
	cabinLease, err := lease.Create(t.Context(), d, lease.Details{UnitID: units[0].ID, StartsOn: "2026-01-01", EndsOn: "2026-12-31", RentCents: 200000, DueDay: 5})
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.AddTenant(t.Context(), d, cabinLease, 0, &person.Details{Name: "Cabin tenant"}); err != nil {
		t.Fatal(err)
	}
	if err := lease.Activate(t.Context(), d, cabinLease); err != nil {
		t.Fatal(err)
	}
	shortUnit, err := property.AddUnit(t.Context(), d, cabin, "Short term")
	if err != nil {
		t.Fatal(err)
	}
	// The stay is in the prior year, but its receipt belongs to July 2026.
	stay, err := booking.Create(t.Context(), d, booking.Details{UnitID: shortUnit, Kind: booking.KindStay, Channel: booking.ChannelAirbnb, CheckIn: "2025-12-20", CheckOut: "2025-12-23"})
	if err != nil {
		t.Fatal(err)
	}
	post(t, d, "2026-12-05")
	for month := 1; month <= 12; month++ {
		on := fmt.Sprintf("2026-%02d-05", month)
		amount := int64(100000)
		if month == 12 {
			amount = 75000
		}
		record(t, d, homeLease, ledger.KindPayment, ledger.CategoryBankTransfer, amount, on)
		record(t, d, cabinLease, ledger.KindPayment, ledger.CategoryCash, 200000, on)
	}
	record(t, d, homeLease, ledger.KindDepositIn, ledger.CategoryDeposit, 50000, "2026-01-01")
	if err := ledger.ApplyDeposit(t.Context(), d, homeLease, 25000, "2026-12-15", "Apply deposit"); err != nil {
		t.Fatal(err)
	}
	record(t, d, homeLease, ledger.KindDepositOut, ledger.CategoryDeposit, 10000, "2026-12-31")
	record(t, d, cabinLease, ledger.KindDepositIn, ledger.CategoryDeposit, 70000, "2026-01-01")
	record(t, d, cabinLease, ledger.KindDepositOut, ledger.CategoryDeposit, 20000, "2026-02-01")
	record(t, d, cabinLease, ledger.KindCredit, ledger.CategoryConcession, 10000, "2026-03-01")
	propertyEntry(t, d, cabin, stay, ledger.KindIncome, ledger.CategoryStayPayout, 350050, "2026-07-01", "Receipt, \"July\"\npaid")
	propertyEntry(t, d, cabin, 0, ledger.KindIncome, ledger.CategoryOtherIncome, 12500, "2026-12-31", "Other")
	// Home: $100 + $200 + ... + $1,400 = $10,500 of expenses.
	for _, v := range []struct {
		category, on string
		amount       int64
	}{
		{"advertising", "2026-01-01", 10000}, {"auto_travel", "2026-02-01", 20000},
		{"cleaning_maintenance", "2026-03-01", 30000}, {"commissions", "2026-04-01", 40000},
		{"insurance", "2026-05-01", 50000}, {"legal_professional", "2026-06-01", 60000},
		{"management_fees", "2026-07-01", 70000}, {"mortgage_interest", "2026-08-01", 80000},
		{"other_interest", "2026-09-01", 90000}, {"repairs", "2026-10-01", 100000},
		{"supplies", "2026-11-01", 110000}, {"taxes", "2026-12-01", 120000},
		{"utilities", "2026-12-31", 130000}, {"other", "2026-01-01", 140000},
	} {
		propertyEntry(t, d, unit.PropertyID, 0, ledger.KindExpense, v.category, v.amount, v.on, "Expense")
	}
	for _, v := range []struct {
		category, on string
		amount       int64
	}{
		{"advertising", "2026-01-01", 5000}, {"repairs", "2026-10-01", 12525},
		{"taxes", "2026-12-31", 30000}, {"utilities", "2026-06-01", 10000}, {"other", "2026-02-01", 2500},
	} {
		propertyEntry(t, d, cabin, 0, ledger.KindExpense, v.category, v.amount, v.on, "Cabin expense")
	}
	voidExpense := propertyEntry(t, d, unit.PropertyID, 0, ledger.KindExpense, ledger.CategoryRepairs, 999999, "2026-02-01", "Voided expense")
	voidIncome := propertyEntry(t, d, cabin, 0, ledger.KindIncome, ledger.CategoryOtherIncome, 444444, "2026-04-01", "Voided income")
	for _, id := range []int64{voidExpense, voidIncome} {
		if err := ledger.Void(t.Context(), d, id, "Wrong entry, keep audit\noriginal"); err != nil {
			t.Fatal(err)
		}
	}
	record(t, d, homeLease, ledger.KindPayment, ledger.CategoryCash, 33333, "2025-12-31")
	record(t, d, homeLease, ledger.KindPayment, ledger.CategoryCash, 44444, "2027-01-01")
	propertyEntry(t, d, cabin, 0, ledger.KindExpense, ledger.CategoryUtilities, 11111, "2025-12-31", "Prior year")
	propertyEntry(t, d, cabin, 0, ledger.KindExpense, ledger.CategoryUtilities, 22222, "2027-01-01", "Next year")
	return yearFixture{d, unit.PropertyID, cabin, homeLease, cabinLease, stay, voidExpense}
}

type categoryWant struct {
	total  int64
	months [12]int64
}
type reportWant struct {
	income, expense, net int64
	months               [12][3]int64
	categories           map[string]categoryWant
	lines                []int64
}

// All expected numbers below are hand computed, in cents. They are never summed
// from the fixture or obtained through another report/CSV calculation.
func homeWant() reportWant {
	return reportWant{
		income: 1200000, expense: 1050000, net: 150000,
		months: [12][3]int64{{100000, 150000, -50000}, {100000, 20000, 80000}, {100000, 30000, 70000}, {100000, 40000, 60000}, {100000, 50000, 50000}, {100000, 60000, 40000}, {100000, 70000, 30000}, {100000, 80000, 20000}, {100000, 90000, 10000}, {100000, 100000, 0}, {100000, 110000, -10000}, {100000, 250000, -150000}},
		categories: map[string]categoryWant{
			"payment/bank_transfer":   {1175000, [12]int64{100000, 100000, 100000, 100000, 100000, 100000, 100000, 100000, 100000, 100000, 100000, 75000}},
			"payment/deposit_applied": {25000, [12]int64{11: 25000}},
			"expense/advertising":     {10000, [12]int64{0: 10000}}, "expense/auto_travel": {20000, [12]int64{1: 20000}},
			"expense/cleaning_maintenance": {30000, [12]int64{2: 30000}}, "expense/commissions": {40000, [12]int64{3: 40000}},
			"expense/insurance": {50000, [12]int64{4: 50000}}, "expense/legal_professional": {60000, [12]int64{5: 60000}},
			"expense/management_fees": {70000, [12]int64{6: 70000}}, "expense/mortgage_interest": {80000, [12]int64{7: 80000}},
			"expense/other_interest": {90000, [12]int64{8: 90000}}, "expense/repairs": {100000, [12]int64{9: 100000}},
			"expense/supplies": {110000, [12]int64{10: 110000}}, "expense/taxes": {120000, [12]int64{11: 120000}},
			"expense/utilities": {130000, [12]int64{11: 130000}}, "expense/other": {140000, [12]int64{0: 140000}},
		},
		lines: []int64{1200000, 10000, 20000, 30000, 40000, 50000, 60000, 70000, 80000, 90000, 100000, 110000, 120000, 130000, 140000},
	}
}
func cabinWant() reportWant {
	return reportWant{
		income: 2762550, expense: 60025, net: 2702525,
		months: [12][3]int64{{200000, 5000, 195000}, {200000, 2500, 197500}, {200000, 0, 200000}, {200000, 0, 200000}, {200000, 0, 200000}, {200000, 10000, 190000}, {550050, 0, 550050}, {200000, 0, 200000}, {200000, 0, 200000}, {200000, 12525, 187475}, {200000, 0, 200000}, {212500, 30000, 182500}},
		categories: map[string]categoryWant{
			"payment/cash":       {2400000, [12]int64{200000, 200000, 200000, 200000, 200000, 200000, 200000, 200000, 200000, 200000, 200000, 200000}},
			"income/stay_payout": {350050, [12]int64{6: 350050}}, "income/other_income": {12500, [12]int64{11: 12500}},
			"expense/advertising": {5000, [12]int64{0: 5000}}, "expense/repairs": {12525, [12]int64{9: 12525}},
			"expense/taxes": {30000, [12]int64{11: 30000}}, "expense/utilities": {10000, [12]int64{5: 10000}}, "expense/other": {2500, [12]int64{1: 2500}},
		},
		lines: []int64{2762550, 5000, 0, 0, 0, 0, 0, 0, 0, 0, 12525, 0, 30000, 10000, 2500},
	}
}
func allWant() reportWant {
	return reportWant{
		income: 3962550, expense: 1110025, net: 2852525,
		months: [12][3]int64{{300000, 155000, 145000}, {300000, 22500, 277500}, {300000, 30000, 270000}, {300000, 40000, 260000}, {300000, 50000, 250000}, {300000, 70000, 230000}, {650050, 70000, 580050}, {300000, 80000, 220000}, {300000, 90000, 210000}, {300000, 112525, 187475}, {300000, 110000, 190000}, {312500, 280000, 32500}},
		categories: map[string]categoryWant{
			"payment/bank_transfer":   {1175000, [12]int64{100000, 100000, 100000, 100000, 100000, 100000, 100000, 100000, 100000, 100000, 100000, 75000}},
			"payment/deposit_applied": {25000, [12]int64{11: 25000}},
			"payment/cash":            {2400000, [12]int64{200000, 200000, 200000, 200000, 200000, 200000, 200000, 200000, 200000, 200000, 200000, 200000}},
			"income/stay_payout":      {350050, [12]int64{6: 350050}}, "income/other_income": {12500, [12]int64{11: 12500}},
			"expense/advertising": {15000, [12]int64{0: 15000}}, "expense/auto_travel": {20000, [12]int64{1: 20000}},
			"expense/cleaning_maintenance": {30000, [12]int64{2: 30000}}, "expense/commissions": {40000, [12]int64{3: 40000}},
			"expense/insurance": {50000, [12]int64{4: 50000}}, "expense/legal_professional": {60000, [12]int64{5: 60000}},
			"expense/management_fees": {70000, [12]int64{6: 70000}}, "expense/mortgage_interest": {80000, [12]int64{7: 80000}},
			"expense/other_interest": {90000, [12]int64{8: 90000}}, "expense/repairs": {112525, [12]int64{9: 112525}},
			"expense/supplies": {110000, [12]int64{10: 110000}}, "expense/taxes": {150000, [12]int64{11: 150000}},
			"expense/utilities": {140000, [12]int64{5: 10000, 11: 130000}}, "expense/other": {142500, [12]int64{0: 140000, 1: 2500}},
		},
		lines: []int64{3962550, 15000, 20000, 30000, 40000, 50000, 60000, 70000, 80000, 90000, 112525, 110000, 150000, 140000, 142500},
	}
}

func assertTotals(t *testing.T, got ledger.Totals, want reportWant) {
	t.Helper()
	if got.IncomeCents != want.income || got.ExpenseCents != want.expense || got.NetCents != want.net {
		t.Fatalf("totals: %d/%d/%d want %d/%d/%d", got.IncomeCents, got.ExpenseCents, got.NetCents, want.income, want.expense, want.net)
	}
	for i, m := range got.Months {
		if m.Month != fmt.Sprintf("2026-%02d", i+1) || [3]int64{m.IncomeCents, m.ExpenseCents, m.NetCents} != want.months[i] {
			t.Errorf("month %d: %+v want %v", i+1, m, want.months[i])
		}
	}
	categories := append(append([]ledger.CategoryTotal{}, got.Income...), got.Expenses...)
	if len(got.Income) != 10 || len(got.Expenses) != 14 {
		t.Fatalf("category counts: %d/%d", len(got.Income), len(got.Expenses))
	}
	seen := map[string]bool{}
	for _, v := range categories {
		if v.Kind == ledger.KindPayment || v.Kind == ledger.KindIncome {
			if v.Line != 3 {
				t.Errorf("income category line: %+v", v)
			}
		}
		key := v.Kind + "/" + v.Category
		expected := want.categories[key]
		if seen[key] {
			t.Fatalf("duplicate category: %s", key)
		}
		seen[key] = true
		if v.AmountCents != expected.total || v.MonthlyCents != expected.months {
			t.Errorf("category %s: %+v want %+v", key, v, expected)
		}
	}
	for key := range want.categories {
		if !seen[key] {
			t.Errorf("missing category %s", key)
		}
	}
	lines := got.ScheduleE()
	lineNumbers := []int{3, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 19}
	if len(lines) != len(lineNumbers) {
		t.Fatal(lines)
	}
	for i, line := range lines {
		if line.Line != lineNumbers[i] || line.AmountCents != want.lines[i] {
			t.Errorf("Schedule E: %+v want line %d amount %d", line, lineNumbers[i], want.lines[i])
		}
	}
}

func TestFullYearCashBasisAndScheduleE(t *testing.T) {
	f := fullYear(t)
	if err := property.SetArchived(t.Context(), f.database, f.cabin, true); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []struct {
		property int64
		want     reportWant
	}{{0, allWant()}, {f.home, homeWant()}, {f.cabin, cabinWant()}} {
		t.Run(fmt.Sprint(scope.property), func(t *testing.T) {
			report, err := ledger.Report(t.Context(), f.database, 2026, scope.property)
			if err != nil {
				t.Fatal(err)
			}
			propertyCount, entryCount := 1, 43
			if scope.property == 0 {
				propertyCount, entryCount = 2, 78
			} else if scope.property == f.cabin {
				entryCount = 35
			}
			if report.Year != 2026 || report.PropertyID != scope.property || len(report.Properties) != propertyCount || len(report.Entries) != entryCount {
				t.Fatalf("report scope/counts: %+v", report)
			}
			assertTotals(t, report.Totals, scope.want)
			for _, p := range report.Properties {
				want := homeWant()
				if p.PropertyID == f.cabin {
					want = cabinWant()
				}
				assertTotals(t, p.Totals, want)
			}
			assertScheduleCSV(t, report, f)
			assertDetailCSV(t, report)
		})
	}
	for _, v := range []struct {
		year                 int
		income, expense, net int64
		date                 string
	}{{2025, 33333, 11111, 22222, "2025-12-31"}, {2027, 44444, 22222, 22222, "2027-01-01"}} {
		r, err := ledger.Report(t.Context(), f.database, v.year, 0)
		if err != nil {
			t.Fatal(err)
		}
		if r.IncomeCents != v.income || r.ExpenseCents != v.expense || r.NetCents != v.net || len(r.Entries) != 2 {
			t.Fatalf("boundary %d: %+v", v.year, r)
		}
		for _, e := range r.Entries {
			if e.OccurredOn != v.date {
				t.Fatal(e)
			}
		}
	}
}

func readCSV(t *testing.T, write func(io.Writer) error) [][]string {
	t.Helper()
	var b bytes.Buffer
	if err := write(&b); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&b).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 2 || !strings.Contains(rows[0][0], ledger.DisclaimerChinese) || !strings.Contains(rows[0][0], ledger.DisclaimerEnglish) {
		t.Fatal(rows)
	}
	return rows
}
func assertScheduleCSV(t *testing.T, r ledger.YearReport, f yearFixture) {
	t.Helper()
	rows := readCSV(t, func(w io.Writer) error { return ledger.WriteScheduleE(w, r) })
	if len(rows) != len(r.Properties)+2 {
		t.Fatal(rows)
	}
	for i, p := range r.Properties {
		want := homeWant()
		if p.PropertyID == f.cabin {
			want = cabinWant()
		}
		row := rows[i+2]
		if row[0] != "2026" || row[1] != fmt.Sprint(p.PropertyID) || row[2] != p.PropertyName {
			t.Fatal(row)
		}
		for j, line := range p.ScheduleE() {
			if rows[1][j+3] != fmt.Sprintf("line_%d", line.Line) || row[j+3] != money.Format(want.lines[j]) || row[j+3] != money.Format(line.AmountCents) {
				t.Fatalf("CSV line %d: %s", line.Line, row[j+3])
			}
		}
		if !reflect.DeepEqual(row[18:], []string{money.Format(want.income), money.Format(want.expense), money.Format(want.net)}) {
			t.Fatal(row)
		}
	}
}
func assertDetailCSV(t *testing.T, r ledger.YearReport) {
	t.Helper()
	rows := readCSV(t, func(w io.Writer) error { return ledger.WriteLedger(w, r) })
	if len(rows) != len(r.Entries)+2 {
		t.Fatal(len(rows), len(r.Entries))
	}
	if r.PropertyID == 0 && len(r.Entries) != 78 {
		t.Fatalf("year fixture entries=%d want 78", len(r.Entries))
	}
	for i, e := range r.Entries {
		row := rows[i+2]
		if row[0] != fmt.Sprint(e.ID) || row[1] != fmt.Sprint(e.PropertyID) || row[2] != e.PropertyName || row[5] != e.Kind || row[6] != e.Category || row[7] != money.Format(e.AmountCents) || row[8] != e.OccurredOn || row[9] != e.Period || row[10] != e.Memo || row[11] != e.VoidedAt || row[12] != e.VoidReason {
			t.Fatalf("detail mismatch: %v / %+v", row, e)
		}
		if row[8] < "2026-01-01" || row[8] > "2026-12-31" {
			t.Fatal(row)
		}
		for j, id := range []int64{e.LeaseID, e.BookingID} {
			want := ""
			if id != 0 {
				want = strconv.FormatInt(id, 10)
			}
			if row[3+j] != want {
				t.Fatal(row)
			}
		}
	}
}

func TestPropertyEntryCategoriesAndBookingOwnership(t *testing.T) {
	f := fullYear(t)
	expectedExpenses := []string{"advertising", "auto_travel", "cleaning_maintenance", "commissions", "insurance", "legal_professional", "management_fees", "mortgage_interest", "other_interest", "repairs", "supplies", "taxes", "utilities", "other"}
	if !reflect.DeepEqual(ledger.Categories(ledger.KindIncome), []string{"stay_payout", "other_income"}) || !reflect.DeepEqual(ledger.Categories(ledger.KindExpense), expectedExpenses) {
		t.Fatal("M6 categories differ from decision 0006")
	}
	for _, kind := range []string{ledger.KindExpense, ledger.KindIncome} {
		for _, category := range ledger.Categories(kind) {
			propertyEntry(t, f.database, f.home, 0, kind, category, 1, "2026-01-01", "")
		}
	}
	good := ledger.Details{Kind: ledger.KindIncome, Category: ledger.CategoryStayPayout, AmountCents: 100, OccurredOn: "2026-01-01"}
	for _, v := range []struct {
		p, b    int64
		details ledger.Details
	}{
		{f.home, f.stay, good}, {f.cabin, 999999, good}, {999999, 0, good},
		{f.cabin, f.stay, ledger.Details{Kind: ledger.KindIncome, Category: ledger.CategoryOtherIncome, AmountCents: 1, OccurredOn: "2026-01-01"}},
		{f.cabin, f.stay, ledger.Details{Kind: ledger.KindExpense, Category: ledger.CategoryOther, AmountCents: 1, OccurredOn: "2026-01-01"}},
		{f.home, 0, ledger.Details{Kind: ledger.KindIncome, Category: ledger.CategoryCash, AmountCents: 1, OccurredOn: "2026-01-01"}},
		{f.home, 0, ledger.Details{Kind: ledger.KindExpense, Category: ledger.CategoryStayPayout, AmountCents: 1, OccurredOn: "2026-01-01"}},
		{f.home, 0, ledger.Details{Kind: ledger.KindExpense, Category: ledger.CategoryOther, AmountCents: 0, OccurredOn: "2026-01-01"}},
		{f.home, 0, ledger.Details{Kind: ledger.KindExpense, Category: ledger.CategoryOther, AmountCents: 1, OccurredOn: "2026-02-30"}},
		{f.home, 0, ledger.Details{Kind: ledger.KindPayment, Category: ledger.CategoryCash, AmountCents: 1, OccurredOn: "2026-01-01"}},
	} {
		before, err := ledger.ListEntries(t.Context(), f.database, ledger.Filter{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ledger.RecordProperty(t.Context(), f.database, v.p, v.b, v.details); !errors.Is(err, ledger.ErrInvalid) {
			t.Fatalf("%+v: %v", v, err)
		}
		after, err := ledger.ListEntries(t.Context(), f.database, ledger.Filter{})
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("rejected write changed ledger", err)
		}
	}
	if _, err := ledger.Record(t.Context(), f.database, f.homeLease, good); !errors.Is(err, ledger.ErrInvalid) {
		t.Fatal("lease API accepted other income", err)
	}
}

func TestEmptyReportFiltersAndCSVWriteFailure(t *testing.T) {
	d, id := setup(t, "", false)
	l, err := lease.Get(t.Context(), d, id)
	if err != nil {
		t.Fatal(err)
	}
	u, err := property.GetUnit(t.Context(), d, l.UnitID)
	if err != nil {
		t.Fatal(err)
	}
	r, err := ledger.Report(t.Context(), d, 2026, u.PropertyID)
	if err != nil {
		t.Fatal(err)
	}
	if r.IncomeCents != 0 || r.ExpenseCents != 0 || r.NetCents != 0 || len(r.Properties) != 1 || len(r.Entries) != 0 {
		t.Fatal(r)
	}
	rows := readCSV(t, func(w io.Writer) error { return ledger.WriteScheduleE(w, r) })
	if len(rows) != 3 {
		t.Fatal(rows)
	}
	if err := ledger.WriteScheduleE(failedWriter{}, r); err == nil {
		t.Fatal("summary swallowed writer error")
	}
	if err := ledger.WriteLedger(failedWriter{}, r); err == nil {
		t.Fatal("detail swallowed writer error")
	}
	for _, year := range []int{0, -1, 10000} {
		if _, err := ledger.Report(t.Context(), d, year, 0); !errors.Is(err, ledger.ErrInvalid) {
			t.Fatal(err)
		}
	}
	if _, err := ledger.Report(t.Context(), d, 2026, 999999); !errors.Is(err, ledger.ErrNotFound) {
		t.Fatal(err)
	}
	for _, filter := range []ledger.Filter{{Year: -1}, {Year: 10000}, {PropertyID: -1}, {Kind: "bad"}} {
		if _, err := ledger.ListEntries(t.Context(), d, filter); !errors.Is(err, ledger.ErrInvalid) {
			t.Fatal(err)
		}
	}
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestPropertyEntriesRepeatedConcurrentOutOfOrderAndFailure(t *testing.T) {
	d, leaseID := setup(t, "", false)
	l, err := lease.Get(t.Context(), d, leaseID)
	if err != nil {
		t.Fatal(err)
	}
	u, err := property.GetUnit(t.Context(), d, l.UnitID)
	if err != nil {
		t.Fatal(err)
	}
	v := ledger.Details{Kind: ledger.KindExpense, Category: ledger.CategoryRepairs, AmountCents: 123, OccurredOn: "2026-12-31"}
	// Each submitted manual entry is a distinct financial record, even if identical.
	for range 2 {
		if _, err := ledger.RecordProperty(t.Context(), d, u.PropertyID, 0, v); err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := ledger.RecordProperty(t.Context(), d, u.PropertyID, 0, v)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	// An earlier occurrence date entered later is included in its own month.
	v.OccurredOn = "2026-01-01"
	first, err := ledger.RecordProperty(t.Context(), d, u.PropertyID, 0, v)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE TRIGGER fail_expense BEFORE INSERT ON ledger_entries WHEN NEW.kind = 'expense' BEGIN SELECT RAISE(ABORT,'injected expense failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.RecordProperty(t.Context(), d, u.PropertyID, 0, v); err == nil {
		t.Fatal("write failure was swallowed")
	}
	values, err := ledger.ListEntries(t.Context(), d, ledger.Filter{Year: 2026, PropertyID: u.PropertyID, Kind: ledger.KindExpense})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 5 || values[0].ID != first {
		t.Fatal("entry was duplicated, lost, partially written or out of order", values)
	}
	r, err := ledger.Report(t.Context(), d, 2026, u.PropertyID)
	if err != nil {
		t.Fatal(err)
	}
	if r.ExpenseCents != 615 || r.Months[0].ExpenseCents != 123 || r.Months[11].ExpenseCents != 492 {
		t.Fatal(r.Totals)
	}
	for _, reason := range []string{"Original reason", "Later reason"} {
		if err := ledger.Void(t.Context(), d, first, reason); err != nil {
			t.Fatal(err)
		}
	}
	values, err = ledger.ListEntries(t.Context(), d, ledger.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if values[0].VoidReason != "Original reason" {
		t.Fatal(values[0])
	}
	r, err = ledger.Report(t.Context(), d, 2026, u.PropertyID)
	if err != nil {
		t.Fatal(err)
	}
	if r.ExpenseCents != 492 || r.Months[0].ExpenseCents != 0 {
		t.Fatal(r.Totals)
	}
}

func TestScheduleELineExcludesNoncashAndInvalidCategories(t *testing.T) {
	for _, v := range []struct{ kind, category string }{
		{ledger.KindCharge, ledger.CategoryRent}, {ledger.KindCredit, ledger.CategoryConcession},
		{ledger.KindDepositIn, ledger.CategoryDeposit}, {ledger.KindDepositOut, ledger.CategoryDeposit},
		{ledger.KindExpense, "unknown"}, {ledger.KindIncome, ledger.CategoryRepairs}, {"unknown", ledger.CategoryOther},
	} {
		if line := ledger.ScheduleELine(v.kind, v.category); line != 0 {
			t.Fatalf("%s/%s has line %d", v.kind, v.category, line)
		}
	}
}
