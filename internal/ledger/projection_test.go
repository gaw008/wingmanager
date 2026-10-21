package ledger_test

import (
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"github.com/gaw008/wingmanager/internal/lease"
	"github.com/gaw008/wingmanager/internal/ledger"
)

func projectionFixture(t *testing.T) (*sql.DB, int64) {
	t.Helper()
	d, id := setup(t, "", true)
	post(t, d, "2026-03-05")
	record(t, d, id, ledger.KindDepositIn, ledger.CategoryDeposit, 200000, "2026-01-06")
	record(t, d, id, ledger.KindPayment, ledger.CategoryCash, 150000, "2026-01-10")
	record(t, d, id, ledger.KindCredit, ledger.CategoryConcession, 20000, "2026-02-10")
	voided := record(t, d, id, ledger.KindPayment, ledger.CategoryCash, 100000, "2026-03-10")
	if err := ledger.Void(t.Context(), d, voided, "Wrong entry"); err != nil {
		t.Fatal(err)
	}
	return d, id
}

func TestProjectionRunningBalanceAndRentFIFO(t *testing.T) {
	d, id := projectionFixture(t)
	value, err := lease.Get(t.Context(), d, id)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ledger.Project(t.Context(), d, value, day(t, "2026-03-10"))
	if err != nil {
		t.Fatal(err)
	}
	var balances []int64
	for _, entry := range got.Entries {
		balances = append(balances, entry.BalanceCents)
	}
	if want := []int64{100000, 100000, -50000, 50000, 30000, 130000, 130000}; !reflect.DeepEqual(balances, want) {
		t.Fatalf("running balances %v, want %v", balances, want)
	}
	if got.Entries[6].VoidedAt == "" {
		t.Fatal("voided entry must remain visible")
	}
	wantPeriods := []ledger.RentPeriod{
		{Period: "2026-01", DueOn: "2026-01-05", Status: ledger.StatusPaid, AmountCents: 100000, PaidCents: 100000, PaidPercent: 100},
		{Period: "2026-02", DueOn: "2026-02-05", Status: ledger.StatusPartial, AmountCents: 100000, PaidCents: 70000, PaidPercent: 70},
		{Period: "2026-03", DueOn: "2026-03-05", Status: ledger.StatusUnpaid, AmountCents: 100000},
		{Period: "2026-04", DueOn: "2026-04-05", Status: ledger.StatusNotDue, AmountCents: 100000},
	}
	if !reflect.DeepEqual(got.Periods, wantPeriods) {
		t.Fatalf("rent periods %+v, want %+v", got.Periods, wantPeriods)
	}
	if want := balance(t, d, id, "2026-03-10"); got.Summary != want || want.BalanceCents != 130000 || want.DepositHeldCents != 200000 || want.OldestUnpaidOn != "2026-02-05" {
		t.Fatalf("projection and overdue disagree: %+v / %+v", got.Summary, want)
	}
}

func TestPaymentPreviewFIFOAndPrepayment(t *testing.T) {
	d, id := projectionFixture(t)
	tests := []struct {
		name        string
		amount      int64
		balance     int64
		prepay      int64
		excess      int64
		allocations []int64
		settled     []bool
	}{
		{name: "partial period", amount: 25000, balance: 105000, allocations: []int64{25000}, settled: []bool{false}},
		{name: "settle partial period", amount: 30000, balance: 100000, allocations: []int64{30000}, settled: []bool{true}},
		{name: "exact balance", amount: 130000, allocations: []int64{30000, 100000}, settled: []bool{true, true}},
		{name: "prepayment", amount: 150000, balance: -20000, prepay: 20000, excess: 20000, allocations: []int64{30000, 100000}, settled: []bool{true, true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ledger.PreviewPayment(t.Context(), d, id, test.amount)
			if err != nil {
				t.Fatal(err)
			}
			if got.BalanceCents != test.balance || got.PrepaymentCents != test.prepay || got.ExcessCents != test.excess || len(got.Allocations) != len(test.allocations) {
				t.Fatalf("preview %+v", got)
			}
			periods := []string{"2026-02", "2026-03"}
			for i, allocation := range got.Allocations {
				if allocation.AmountCents != test.allocations[i] || allocation.Settled != test.settled[i] || allocation.Period != periods[i] || allocation.EntryID == 0 {
					t.Fatalf("allocation %+v", allocation)
				}
			}
		})
	}
	for _, amount := range []int64{0, -1} {
		if _, err := ledger.PreviewPayment(t.Context(), d, id, amount); !errors.Is(err, ledger.ErrInvalid) {
			t.Fatalf("amount %d: %v", amount, err)
		}
	}
	if got := balance(t, d, id, "2026-03-10"); got.BalanceCents != 130000 || len(entries(t, d, id)) != 7 {
		t.Fatal("preview changed the ledger", got)
	}
}

func TestPaymentPreviewManualChargeAndExistingPrepayment(t *testing.T) {
	d, id := setup(t, "", true)
	record(t, d, id, ledger.KindCharge, ledger.CategoryLateFee, 10000, "2026-01-01")
	got, err := ledger.PreviewPayment(t.Context(), d, id, 10000)
	if err != nil || len(got.Allocations) != 1 || got.Allocations[0].Period != "" || got.Allocations[0].OccurredOn != "2026-01-01" || !got.Allocations[0].Settled {
		t.Fatalf("manual charge %+v, %v", got, err)
	}
	record(t, d, id, ledger.KindPayment, ledger.CategoryCash, 15000, "2026-01-02")
	got, err = ledger.PreviewPayment(t.Context(), d, id, 2000)
	if err != nil || len(got.Allocations) != 0 || got.BalanceCents != -7000 || got.PrepaymentCents != 7000 || got.ExcessCents != 2000 {
		t.Fatalf("existing prepayment %+v, %v", got, err)
	}
}

func TestProjectionFuturePeriodsAndLeaseEnd(t *testing.T) {
	d, id := setup(t, "2026-03-15", true)
	post(t, d, "2026-03-05")
	value, err := lease.Get(t.Context(), d, id)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ledger.Project(t.Context(), d, value, day(t, "2026-02-01"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Periods) != 3 || got.Periods[1].Status != ledger.StatusNotDue || got.Periods[2].Status != ledger.StatusNotDue {
		t.Fatalf("future posted periods %+v", got.Periods)
	}
	got, err = ledger.Project(t.Context(), d, value, day(t, "2026-03-10"))
	if err != nil || len(got.Periods) != 3 {
		t.Fatalf("next period exceeded lease end: %+v, %v", got.Periods, err)
	}
	if err := lease.End(t.Context(), d, id, "2026-03-15"); err != nil {
		t.Fatal(err)
	}
	value, err = lease.Get(t.Context(), d, id)
	if err != nil {
		t.Fatal(err)
	}
	got, err = ledger.Project(t.Context(), d, value, day(t, "2026-02-01"))
	if err != nil || len(got.Periods) != 3 {
		t.Fatalf("ended projection %+v, %v", got, err)
	}
}

func TestProjectionNextPeriodUsesCurrentTermsWithoutChangingPostedRent(t *testing.T) {
	d, id := setup(t, "", true)
	post(t, d, "2026-01-05")
	value, err := lease.Get(t.Context(), d, id)
	if err != nil {
		t.Fatal(err)
	}
	value.RentCents = 200000
	if err := lease.Update(t.Context(), d, id, value.Details); err != nil {
		t.Fatal(err)
	}
	got, err := ledger.Project(t.Context(), d, value, day(t, "2026-01-10"))
	if err != nil || len(got.Periods) != 2 || got.Periods[0].AmountCents != 100000 || got.Periods[1].AmountCents != 200000 || got.Periods[1].Status != ledger.StatusNotDue {
		t.Fatalf("rent change projection %+v, %v", got, err)
	}
	draftDB, draftID := setup(t, "", false)
	value, err = lease.Get(t.Context(), draftDB, draftID)
	if err != nil {
		t.Fatal(err)
	}
	got, err = ledger.Project(t.Context(), draftDB, value, day(t, "2026-01-10"))
	if err != nil || len(got.Periods) != 0 {
		t.Fatalf("draft projection %+v, %v", got, err)
	}
}
