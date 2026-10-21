package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/civildate"
	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/lease"
	"github.com/gaw008/wingmanager/internal/ledger"
	"github.com/gaw008/wingmanager/internal/money"
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/settings"
	"github.com/gaw008/wingmanager/internal/task"
)

func TestM7TodayAnswersLandlordQuestionsInConfiguredTimezone(t *testing.T) {
	for _, locale := range settings.Locales() {
		t.Run(locale, func(t *testing.T) {
			a := newM3App(t, locale)
			ctx := t.Context()
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			today, err := civildate.Parse("2026-10-31")
			must(err)
			unit, err := property.GetUnit(ctx, a.database, a.unitID)
			must(err)
			addUnit := func(name string) int64 {
				t.Helper()
				id, err := property.AddUnit(ctx, a.database, unit.PropertyID, name)
				must(err)
				return id
			}
			createStay := func(unitID int64, from, to, guest string) int64 {
				t.Helper()
				id, err := booking.Create(ctx, a.database, booking.Details{UnitID: unitID, Kind: booking.KindStay, Channel: booking.ChannelDirect, CheckIn: from, CheckOut: to, NewGuest: &person.Details{Name: guest}})
				must(err)
				return id
			}
			departing := createStay(a.unitID, "2026-10-30", "2026-10-31", "Departing guest")
			arriving := createStay(a.unitID, "2026-10-31", "2026-11-02", "Arriving guest")
			next := createStay(a.unitID, "2026-11-02", "2026-11-07", "Next guest")
			createStay(a.unitID, "2026-11-08", "2026-11-10", "Outside seven days")
			createStay(addUnit("UTC tomorrow"), "2026-11-01", "2026-11-02", "UTC tomorrow guest")
			longStay := createStay(addUnit("Long stay"), "2026-10-31", "2026-12-01", "Long stay guest")
			cancelled := createStay(addUnit("Cancelled unit"), "2026-10-31", "2026-11-01", "Cancelled guest")
			must(booking.SetCancelled(ctx, a.database, cancelled, true))
			block, err := booking.Create(ctx, a.database, booking.Details{UnitID: addUnit("Blocked unit"), Kind: booking.KindBlock, Channel: booking.ChannelDirect, CheckIn: "2026-10-31", CheckOut: "2026-11-01"})
			must(err)

			rentalUnit := addUnit("Long-term unit")
			leaseID, err := lease.Create(ctx, a.database, lease.Details{UnitID: rentalUnit, StartsOn: "2026-10-01", EndsOn: "2026-12-31", RentCents: 100000, DueDay: 5})
			must(err)
			must(lease.AddTenant(ctx, a.database, leaseID, 0, &person.Details{Name: "Overdue tenant"}))
			must(lease.Activate(ctx, a.database, leaseID))
			_, err = ledger.Record(ctx, a.database, leaseID, ledger.Details{Kind: ledger.KindCharge, Category: ledger.CategoryRent, AmountCents: 100000, OccurredOn: "2026-10-05"})
			must(err)
			_, err = ledger.Record(ctx, a.database, leaseID, ledger.Details{Kind: ledger.KindPayment, Category: ledger.CategoryCash, AmountCents: 25000, OccurredOn: "2026-10-15"})
			must(err)
			for _, entry := range []ledger.Details{
				{Kind: ledger.KindIncome, Category: ledger.CategoryOtherIncome, AmountCents: 50000, OccurredOn: "2026-01-01"},
				{Kind: ledger.KindExpense, Category: ledger.CategoryRepairs, AmountCents: 12345, OccurredOn: "2026-10-31"},
				{Kind: ledger.KindIncome, Category: ledger.CategoryOtherIncome, AmountCents: 90000, OccurredOn: "2026-11-01"},
				{Kind: ledger.KindIncome, Category: ledger.CategoryOtherIncome, AmountCents: 80000, OccurredOn: "2025-12-31"},
			} {
				_, err := ledger.RecordProperty(ctx, a.database, unit.PropertyID, 0, entry)
				must(err)
			}
			_, err = ledger.Record(ctx, a.database, leaseID, ledger.Details{Kind: ledger.KindDepositIn, Category: ledger.CategoryDeposit, AmountCents: 200000, OccurredOn: "2026-10-01"})
			must(err)

			var dueTaskIDs []int64
			for _, v := range []struct{ title, due, status string }{
				{"Overdue repair", "2026-10-30", task.StatusOpen}, {"Due today repair", "2026-10-31", task.StatusOpen},
				{"Future repair", "2026-11-01", task.StatusOpen}, {"Undated repair", "", task.StatusOpen},
				{"Completed repair", "2026-10-30", task.StatusDone}, {"Cancelled repair", "2026-10-30", task.StatusCancelled},
			} {
				id, err := task.Create(ctx, a.database, task.Details{PropertyID: unit.PropertyID, Kind: task.KindMaintenance, Title: v.title, DueOn: v.due})
				must(err)
				if v.status != task.StatusOpen {
					must(task.SetStatus(ctx, a.database, id, v.status))
				}
				if v.status == task.StatusOpen && v.due != "" && v.due <= today.String() {
					dueTaskIDs = append(dueTaskIDs, id)
				}
			}

			feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/error" {
					http.Error(w, "Unavailable", http.StatusInternalServerError)
					return
				}
				fmt.Fprint(w, "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\nEND:VCALENDAR\r\n")
			}))
			t.Cleanup(feedServer.Close)
			now := time.Date(2026, 11, 1, 1, 30, 0, 0, time.UTC)
			feedUnit := addUnit("Calendar sources")
			var failing, stale, never, healthy, boundary int64
			for _, v := range []struct {
				path string
				ago  time.Duration
				id   *int64
			}{
				{"/error", time.Hour, &failing}, {"/stale", 25 * time.Hour, &stale}, {"/never", 0, &never},
				{"/healthy", time.Hour, &healthy}, {"/boundary", 24 * time.Hour, &boundary},
			} {
				id, err := ical.Add(ctx, a.database, feedUnit, booking.ChannelOther, feedServer.URL+v.path)
				must(err)
				*v.id = id
				if v.path != "/never" {
					syncTime := now.Add(-v.ago)
					err := ical.NewService(a.database, feedServer.Client(), func() time.Time { return syncTime }).Sync(ctx, id)
					if v.path == "/error" {
						if err == nil {
							t.Fatal("failing feed succeeded")
						}
					} else {
						must(err)
					}
				}
			}

			conflictFeed, err := ical.Add(ctx, a.database, a.unitID, booking.ChannelAirbnb, feedServer.URL+"/conflict")
			must(err)
			tx, err := a.database.BeginTx(ctx, nil)
			must(err)
			must(booking.ApplySnapshot(ctx, tx, conflictFeed, a.unitID, booking.ChannelAirbnb, []booking.ImportedEvent{
				{UID: "future-overlap", Kind: booking.KindStay, CheckIn: "2026-11-04", CheckOut: "2026-11-06"},
				{UID: "past-first", Kind: booking.KindStay, CheckIn: "2026-08-01", CheckOut: "2026-08-03"},
				{UID: "past-second", Kind: booking.KindStay, CheckIn: "2026-08-02", CheckOut: "2026-08-04"},
			}, today))
			must(tx.Commit())

			body := a.request(t, "GET", "/", nil, http.StatusOK,
				`id="today-date" datetime="2026-10-31"`, "America/Los_Angeles", "Overdue tenant", "$750", map[string]string{"zh-CN": "26 天逾期", "en": "26 days overdue"}[locale],
				"Overdue repair", "Due today repair", `id="ytd-net">$626.55`)
			section := func(page, id string) string {
				t.Helper()
				start := strings.Index(page, `<section id="`+id+`">`)
				if start < 0 {
					t.Fatalf("missing section %s", id)
				}
				end := strings.Index(page[start:], "</section>")
				return page[start : start+end]
			}
			checkIns := section(a.request(t, "GET", "/?tab=in", nil, http.StatusOK), "today-reservations")
			checkOuts := section(body, "today-reservations")
			if !strings.Contains(checkIns, fmt.Sprintf(`data-booking-id="%d"`, arriving)) || !strings.Contains(checkIns, fmt.Sprintf(`data-booking-id="%d"`, longStay)) || strings.Contains(checkIns, "UTC tomorrow guest") || strings.Contains(checkIns, "Departing guest") {
				t.Fatal(checkIns)
			}
			if !strings.Contains(checkOuts, fmt.Sprintf(`data-booking-id="%d"`, departing)) || strings.Contains(checkOuts, "Arriving guest") {
				t.Fatal(checkOuts)
			}
			soon := section(a.request(t, "GET", "/?tab=soon", nil, http.StatusOK), "today-reservations")
			if !strings.Contains(soon, fmt.Sprintf(`data-booking-id="%d"`, next)) || !strings.Contains(soon, "Outside seven days") || strings.Contains(soon, "Long stay guest") {
				t.Fatal(soon)
			}
			for _, hidden := range []string{"Cancelled guest", fmt.Sprintf(`data-booking-id="%d"`, block)} {
				if strings.Contains(checkIns+checkOuts+soon, hidden) {
					t.Fatalf("unexpected %q", hidden)
				}
			}
			attention := section(body, "today-attention")
			for _, id := range dueTaskIDs {
				if !strings.Contains(attention, fmt.Sprintf(`data-task-id="%d"`, id)) {
					t.Fatal("missing due task")
				}
			}
			cleaning, err := task.CleaningForBooking(ctx, a.database, departing)
			must(err)
			if cleaning == nil || !strings.Contains(attention, fmt.Sprintf(`<a href="/tasks/%d">Beach house · %s %s</a>`, cleaning.ID, unit.Name, catalogs[locale]["checkout_cleaning"])) {
				t.Fatal("generated cleaning task is missing its unit and translated checkout-cleaning reason")
			}
			for _, hidden := range []string{"Future repair", "Undated repair", "Completed repair", "Cancelled repair"} {
				if strings.Contains(attention, hidden) {
					t.Fatalf("unexpected task %s", hidden)
				}
			}
			for _, id := range []int64{failing, stale, never} {
				if !strings.Contains(attention, fmt.Sprintf(`data-feed-id="%d"`, id)) {
					t.Fatal("missing problem feed")
				}
			}
			for _, id := range []int64{healthy, boundary} {
				if strings.Contains(attention, fmt.Sprintf(`data-feed-id="%d"`, id)) {
					t.Fatal("healthy feed flagged")
				}
			}
			for _, want := range []string{`id="check-in-count">2`, `id="check-out-count">1`, `id="overdue-total">$750`, map[string]string{"zh-CN": "1 份租约 · 最长 26 天", "en": "1 lease · Longest 26 days"}[locale], `data-current-lease-id="` + fmt.Sprint(leaseID) + `"`, `$2,000`, `id="todo-count">10`} {
				if !strings.Contains(body, want) {
					t.Fatalf("missing Today summary %s", want)
				}
			}
			// Conflicts remain visible on the calendar, whose view owns conflict navigation.
			calendar := a.request(t, "GET", "/calendar?month=2026-11", nil, http.StatusOK)
			if !strings.Contains(calendar, `class="conflict-banner"`) || !strings.Contains(calendar, `<b>1 `) {
				t.Fatal("upcoming imported conflict banner missing or incorrect")
			}
			conflictBars := regexp.MustCompile(`<a class="bar [^"]* conflict [^"]*"[^>]+>`).FindAllString(calendar, -1)
			if len(conflictBars) != 2 {
				t.Fatalf("expected both sides of one overlap, got %v", conflictBars)
			}
			for _, dates := range map[string][]string{"zh-CN": {"11月2日 → 11月7日", "11月4日 → 11月6日"}, "en": {"Nov 2 → Nov 7", "Nov 4 → Nov 6"}}[locale] {
				if !strings.Contains(strings.Join(conflictBars, ""), dates) {
					t.Fatalf("unflagged conflicting stay %s", dates)
				}
			}
			if strings.Contains(strings.Join(conflictBars, ""), "2026-08") {
				t.Fatal("past conflict leaked into future calendar month")
			}

			report, err := ledger.ReportToDate(ctx, a.database, today)
			must(err)
			if report.NetCents != 62655 || !strings.Contains(body, `id="ytd-net">`+money.FormatCurrency("USD", report.NetCents)) {
				t.Fatalf("YTD: %+v", report)
			}
			fullYear, err := ledger.Report(ctx, a.database, 2026, 0)
			must(err)
			if fullYear.NetCents != report.NetCents+90000 {
				t.Fatal("future receipts not excluded from YTD")
			}
		})
	}
}
