package ledger_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/gaw008/wingmanager/internal/civildate"
	"github.com/gaw008/wingmanager/internal/db"
	"github.com/gaw008/wingmanager/internal/jobs"
	"github.com/gaw008/wingmanager/internal/lease"
	"github.com/gaw008/wingmanager/internal/ledger"
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
)

func day(t *testing.T, s string) civildate.Date {
	t.Helper()
	v, err := civildate.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func setup(t *testing.T, end string, active bool) (*sql.DB, int64) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Migrate(t.Context(), d); err != nil {
		t.Fatal(err)
	}
	p, err := property.Create(t.Context(), d, property.Details{Name: "Home"}, "Unit")
	if err != nil {
		t.Fatal(err)
	}
	units, err := property.ListUnits(t.Context(), d, p, false)
	if err != nil {
		t.Fatal(err)
	}
	id, err := lease.Create(t.Context(), d, lease.Details{UnitID: units[0].ID, StartsOn: "2026-01-01", EndsOn: end, RentCents: 100000, DueDay: 5, DepositCents: 200000})
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.AddTenant(t.Context(), d, id, 0, &person.Details{Name: "Tenant"}); err != nil {
		t.Fatal(err)
	}
	if active {
		if err := lease.Activate(t.Context(), d, id); err != nil {
			t.Fatal(err)
		}
	}
	return d, id
}
func entries(t *testing.T, d *sql.DB, id int64) []ledger.Entry {
	t.Helper()
	v, err := ledger.List(t.Context(), d, id)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func record(t *testing.T, d *sql.DB, id int64, kind, category string, amount int64, on string) int64 {
	t.Helper()
	entry, err := ledger.Record(t.Context(), d, id, ledger.Details{Kind: kind, Category: category, AmountCents: amount, OccurredOn: on})
	if err != nil {
		t.Fatal(err)
	}
	return entry
}
func balance(t *testing.T, d *sql.DB, id int64, on string) ledger.Summary {
	t.Helper()
	v, err := ledger.Balance(t.Context(), d, id, day(t, on))
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func post(t *testing.T, d *sql.DB, on string) {
	t.Helper()
	if err := ledger.PostRent(t.Context(), d, day(t, on)); err != nil {
		t.Fatal(err)
	}
}
func TestScenario2RepeatedOfflineAndConcurrentPosting(t *testing.T) {
	d, id := setup(t, "", true)
	for range 2 {
		post(t, d, "2026-01-05")
	}
	if got := entries(t, d, id); len(got) != 1 {
		t.Fatalf("first run: %v", got)
	}
	// The machine is off throughout February, March and April.
	for range 2 {
		post(t, d, "2026-05-05")
	}
	got := entries(t, d, id)
	if len(got) != 5 {
		t.Fatalf("catch up: %v", got)
	}
	for i, e := range got {
		if e.Period != []string{"2026-01", "2026-02", "2026-03", "2026-04", "2026-05"}[i] || e.AmountCents != 100000 {
			t.Fatalf("wrong period: %+v", e)
		}
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; errs <- ledger.PostRent(t.Context(), d, day(t, "2026-08-05")) }()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := entries(t, d, id); len(got) != 8 {
		t.Fatalf("concurrent: %v", got)
	}
	// An older scheduled invocation arriving late cannot remove or change later periods.
	post(t, d, "2026-03-05")
	if len(entries(t, d, id)) != 8 {
		t.Fatal("out-of-order run changed ledger")
	}
}
func TestDueDateRentChangeDraftAndEnd(t *testing.T) {
	d, id := setup(t, "2026-03-15", true)
	post(t, d, "2026-01-04")
	if len(entries(t, d, id)) != 0 {
		t.Fatal("posted in advance")
	}
	post(t, d, "2026-01-05")
	v, err := lease.Get(t.Context(), d, id)
	if err != nil {
		t.Fatal(err)
	}
	v.RentCents = 200000
	if err := lease.Update(t.Context(), d, id, v.Details); err != nil {
		t.Fatal(err)
	}
	post(t, d, "2026-02-05")
	got := entries(t, d, id)
	if len(got) != 2 || got[0].AmountCents != 100000 || got[1].AmountCents != 200000 {
		t.Fatal(got)
	}
	// Catch up after the inclusive end, then auto-end; no April charge is allowed.
	if err := jobs.Daily(t.Context(), d, day(t, "2026-06-10")); err != nil {
		t.Fatal(err)
	}
	got = entries(t, d, id)
	if len(got) != 3 || got[2].Period != "2026-03" || got[2].AmountCents != 96774 {
		t.Fatal(got)
	}
	v, err = lease.Get(t.Context(), d, id)
	if err != nil || v.Status != lease.StatusEnded {
		t.Fatalf("%v %v", v, err)
	}
	post(t, d, "2027-01-01")
	if len(entries(t, d, id)) != 3 {
		t.Fatal("ended charged")
	}
	draftDB, draftID := setup(t, "", false)
	post(t, draftDB, "2026-06-01")
	if len(entries(t, draftDB, draftID)) != 0 {
		t.Fatal("draft charged")
	}
	manualDB, manualID := setup(t, "", true)
	post(t, manualDB, "2026-01-05")
	if err := lease.End(t.Context(), manualDB, manualID, "2026-01-15"); err != nil {
		t.Fatal(err)
	}
	post(t, manualDB, "2026-04-01")
	if len(entries(t, manualDB, manualID)) != 1 {
		t.Fatal("manual end charged")
	}
}
func TestRentBatchRollbackAndRecovery(t *testing.T) {
	d, id := setup(t, "", true)
	// A real database failure on the second period must roll back the first too.
	if _, err := d.Exec(`CREATE TRIGGER fail_rent BEFORE INSERT ON ledger_entries WHEN NEW.period = '2026-02' BEGIN SELECT RAISE(ABORT,'injected rent failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := ledger.PostRent(t.Context(), d, day(t, "2026-03-05")); err == nil {
		t.Fatal("expected failure")
	}
	if len(entries(t, d, id)) != 0 {
		t.Fatal("partial rent batch committed")
	}
	if _, err := d.Exec("DROP TRIGGER fail_rent"); err != nil {
		t.Fatal(err)
	}
	post(t, d, "2026-03-05")
	if len(entries(t, d, id)) != 3 {
		t.Fatal("recovery incomplete")
	}
}
func TestVoidAndOverdueFIFO(t *testing.T) {
	d, id := setup(t, "", true)
	post(t, d, "2026-03-05")
	got := entries(t, d, id)
	if err := ledger.Void(t.Context(), d, got[0].ID, "correction"); !errors.Is(err, ledger.ErrAutoRent) {
		t.Fatal(err)
	}
	s := balance(t, d, id, "2026-03-10")
	if s.BalanceCents != 300000 || s.OldestUnpaidOn != "2026-01-05" || s.DaysOverdue != 64 {
		t.Fatal(s)
	}
	payment := record(t, d, id, ledger.KindPayment, ledger.CategoryBankTransfer, 150000, "2026-03-10")
	s = balance(t, d, id, "2026-03-10")
	if s.BalanceCents != 150000 || s.OldestUnpaidOn != "2026-02-05" || s.DaysOverdue != 33 {
		t.Fatal(s)
	}
	record(t, d, id, ledger.KindCredit, ledger.CategoryConcession, 60000, "2026-03-10")
	s = balance(t, d, id, "2026-03-10")
	if s.BalanceCents != 90000 || s.OldestUnpaidOn != "2026-03-05" || s.DaysOverdue != 5 {
		t.Fatal(s)
	}
	if err := ledger.Void(t.Context(), d, payment, " "); !errors.Is(err, ledger.ErrInvalid) {
		t.Fatal(err)
	}
	if err := ledger.Void(t.Context(), d, payment, "wrong payment"); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Void(t.Context(), d, payment, "must preserve original reason"); err != nil {
		t.Fatal(err)
	}
	s = balance(t, d, id, "2026-03-10")
	if s.BalanceCents != 240000 || s.OldestUnpaidOn != "2026-01-05" {
		t.Fatal(s)
	}
	for _, e := range entries(t, d, id) {
		if e.ID == payment && (e.VoidReason != "wrong payment" || e.VoidedAt == "") {
			t.Fatal(e)
		}
	}
	record(t, d, id, ledger.KindPayment, ledger.CategoryCash, 240000, "2026-03-10")
	s = balance(t, d, id, "2026-03-10")
	if s.BalanceCents != 0 || s.DaysOverdue != 0 {
		t.Fatal(s)
	}
}
func TestDueTodayIsNotOverdueAndPrepaymentFIFO(t *testing.T) {
	d, id := setup(t, "", true)
	record(t, d, id, ledger.KindPayment, ledger.CategoryCash, 150000, "2025-12-31")
	post(t, d, "2026-02-05")
	s := balance(t, d, id, "2026-02-05")
	if s.BalanceCents != 50000 || s.DaysOverdue != 0 || s.OldestUnpaidOn != "2026-02-05" {
		t.Fatal(s)
	}
	s = balance(t, d, id, "2026-02-06")
	if s.DaysOverdue != 1 {
		t.Fatal(s)
	}
}
func TestApplyDepositAtomicAndCorrect(t *testing.T) {
	d, id := setup(t, "", true)
	post(t, d, "2026-01-05")
	record(t, d, id, ledger.KindDepositIn, ledger.CategoryDeposit, 80000, "2026-01-01")
	if err := ledger.ApplyDeposit(t.Context(), d, id, 30000, "2026-01-06", "Apply"); err != nil {
		t.Fatal(err)
	}
	s := balance(t, d, id, "2026-01-06")
	if s.BalanceCents != 70000 || s.DepositHeldCents != 50000 {
		t.Fatal(s)
	}
	got := entries(t, d, id)
	if len(got) != 4 || got[2].Kind != ledger.KindDepositOut || got[3].Category != ledger.CategoryDepositApplied || got[2].AmountCents != got[3].AmountCents || got[2].PropertyID != got[3].PropertyID {
		t.Fatal(got)
	}
	if err := ledger.ApplyDeposit(t.Context(), d, id, 50001, "2026-01-06", ""); !errors.Is(err, ledger.ErrDeposit) {
		t.Fatal(err)
	}
	// Fail the second write after deposit_out succeeds inside the transaction.
	if _, err := d.Exec(`CREATE TRIGGER fail_payment BEFORE INSERT ON ledger_entries WHEN NEW.category = 'deposit_applied' BEGIN SELECT RAISE(ABORT,'injected payment failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := ledger.ApplyDeposit(t.Context(), d, id, 10000, "2026-01-06", ""); err == nil {
		t.Fatal("expected failure")
	}
	if len(entries(t, d, id)) != 4 {
		t.Fatal("partial deposit application committed")
	}
	s = balance(t, d, id, "2026-01-06")
	if s.BalanceCents != 70000 || s.DepositHeldCents != 50000 {
		t.Fatal(s)
	}
}
func TestKindsMatchCheckAndCategories(t *testing.T) {
	d, id := setup(t, "", true)
	var schema string
	if err := d.QueryRow("SELECT sql FROM sqlite_master WHERE name='ledger_entries'").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?s)kind IN\s*\(([^)]+)\)`)
	m := re.FindStringSubmatch(schema)
	if len(m) != 2 {
		t.Fatal(schema)
	}
	var kinds []string
	for _, v := range strings.Split(m[1], ",") {
		kinds = append(kinds, strings.Trim(strings.TrimSpace(v), "'"))
	}
	if !reflect.DeepEqual(kinds, ledger.Kinds()) {
		t.Fatalf("%v != %v", kinds, ledger.Kinds())
	}
	for _, kind := range ledger.LeaseKinds() {
		for _, category := range ledger.Categories(kind) {
			record(t, d, id, kind, category, 123, "2026-01-01")
		}
	}
	if _, err := ledger.Record(t.Context(), d, id, ledger.Details{Kind: ledger.KindCharge, Category: ledger.CategoryCash, AmountCents: 1, OccurredOn: "2026-01-01"}); !errors.Is(err, ledger.ErrInvalid) {
		t.Fatal(err)
	}
}

func TestZeroRoundedProrationAndMidmonthDue(t *testing.T) {
	d, id := setup(t, "", false)
	v, err := lease.Get(t.Context(), d, id)
	if err != nil {
		t.Fatal(err)
	}
	v.StartsOn = "2026-01-31"
	v.EndsOn = "2026-01-31"
	v.RentCents = 1
	if err := lease.Update(t.Context(), d, id, v.Details); err != nil {
		t.Fatal(err)
	}
	if err := lease.Activate(t.Context(), d, id); err != nil {
		t.Fatal(err)
	}
	post(t, d, "2026-01-31")
	if len(entries(t, d, id)) != 0 {
		t.Fatal("zero rent must not create a positive ledger entry")
	}
	d2, id2 := setup(t, "", false)
	v, err = lease.Get(t.Context(), d2, id2)
	if err != nil {
		t.Fatal(err)
	}
	v.StartsOn = "2026-01-16"
	v.RentCents = 310000
	if err := lease.Update(t.Context(), d2, id2, v.Details); err != nil {
		t.Fatal(err)
	}
	if err := lease.Activate(t.Context(), d2, id2); err != nil {
		t.Fatal(err)
	}
	post(t, d2, "2026-01-15")
	if len(entries(t, d2, id2)) != 0 {
		t.Fatal("first partial period posted before starts_on")
	}
	post(t, d2, "2026-01-16")
	got := entries(t, d2, id2)
	if len(got) != 1 || got[0].AmountCents != 160000 || got[0].OccurredOn != "2026-01-16" {
		t.Fatal(got)
	}
}

func TestVoidChargeCreditAndDeposits(t *testing.T) {
	d, id := setup(t, "", true)
	charge := record(t, d, id, ledger.KindCharge, ledger.CategoryLateFee, 1000, "2026-01-01")
	credit := record(t, d, id, ledger.KindCredit, ledger.CategoryConcession, 200, "2026-01-01")
	depositIn := record(t, d, id, ledger.KindDepositIn, ledger.CategoryDeposit, 2000, "2026-01-01")
	depositOut := record(t, d, id, ledger.KindDepositOut, ledger.CategoryDeposit, 500, "2026-01-01")
	s := balance(t, d, id, "2026-01-02")
	if s.BalanceCents != 800 || s.DepositHeldCents != 1500 {
		t.Fatal(s)
	}
	for _, entryID := range []int64{charge, credit, depositIn, depositOut} {
		if err := ledger.Void(t.Context(), d, entryID, "Wrong entry"); err != nil {
			t.Fatal(err)
		}
	}
	s = balance(t, d, id, "2026-01-02")
	if s.BalanceCents != 0 || s.DepositHeldCents != 0 || s.DaysOverdue != 0 {
		t.Fatal(s)
	}
}
