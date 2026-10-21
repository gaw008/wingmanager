package web

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/civildate"
	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/lease"
	"github.com/gaw008/wingmanager/internal/ledger"
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/settings"
)

func TestCountTranslations(t *testing.T) {
	cases := []struct {
		key, one, other, chinese string
	}{
		{"checkins_today", "%d check-in", "%d check-ins", "%d 位入住"},
		{"checkouts_today", "%d check-out", "%d check-outs", "%d 位退房"},
		{"today_check_ins_count", "Today's check-in（%d）", "Today's check-ins（%d）", "今天入住（%d）"},
		{"today_check_outs_count", "Today's check-out（%d）", "Today's check-outs（%d）", "今天退房（%d）"},
		{"staying_now_count", "Staying now（%d）", "Staying now（%d）", "入住中（%d）"},
		{"coming_soon_count", "Coming up（%d）", "Coming up（%d）", "即将到来（%d）"},
		{"arrives_in_days", "Checks in in %d day", "Checks in in %d days", "%d 天后入住"},
		{"departs_in_days", "Checks out in %d day", "Checks out in %d days", "%d 天后退房"},
		{"nights_count", "%d night", "%d nights", "%d 晚"},
		{"items", "%d item", "%d items", "%d 项"},
		{"overdue_count", "%d lease", "%d leases", "%d 份租约"},
		{"days", "%d day", "%d days", "%d 天"},
		{"days_overdue", "%d day overdue", "%d days overdue", "%d 天逾期"},
		{"calendar_conflicts", "%d booking conflict", "%d booking conflicts", "%d 处订单冲突"},
		{"calendar_feed_count", "%d calendar feed", "%d calendar feeds", "%d 个日历源"},
		{"bytes", "%d byte", "%d bytes", "%d 字节"},
	}
	if len(cases) != len(countCatalogs[settings.LocaleEnglish]) {
		t.Fatal("every count message needs a translation test")
	}
	for _, tt := range cases {
		t.Run(tt.key, func(t *testing.T) {
			for _, locale := range settings.Locales() {
				for _, count := range []int{0, 1, 2, 21} {
					format := tt.chinese
					if locale == settings.LocaleEnglish {
						format = tt.other
						if count == 1 {
							format = tt.one
						}
					}
					want := fmt.Sprintf(format, count)
					for _, value := range []any{count, int64(count)} {
						got, err := translateCount(locale, tt.key, value)
						if err != nil || got != want {
							t.Errorf("%s/%s(%v, %T) = %q, %v; want %q", locale, tt.key, value, value, got, err, want)
						}
					}
				}
			}
		})
	}
}

func TestCountTranslationErrors(t *testing.T) {
	for _, tt := range []struct {
		locale, key string
		count       any
	}{
		{settings.LocaleEnglish, "unknown", 1},
		{"unknown", "items", 1},
		{settings.LocaleEnglish, "items", "1"},
	} {
		if got, err := translateCount(tt.locale, tt.key, tt.count); err == nil || got != "" {
			t.Errorf("translateCount(%q, %q, %v) = %q, %v; want error", tt.locale, tt.key, tt.count, got, err)
		}
	}
}

func TestTodayHeaderCountGrammar(t *testing.T) {
	for _, locale := range settings.Locales() {
		t.Run(locale, func(t *testing.T) {
			a := newM3App(t, locale)
			check := func(ins, outs int) {
				t.Helper()
				body := a.request(t, http.MethodGet, "/?tab=in", nil, http.StatusOK)
				heading := regexp.MustCompile(`<time id="today-date"[^>]*>([^<]*)</time>`).FindStringSubmatch(body)
				var want string
				if locale == settings.LocaleEnglish {
					in := []string{"0 check-ins", "1 check-in", "2 check-ins"}[ins]
					out := []string{"0 check-outs", "1 check-out", "2 check-outs"}[outs]
					want = "Oct 31, Saturday · " + in + " and " + out + " today"
				} else {
					want = fmt.Sprintf("10月31日 星期六 · 今天有 %d 位入住、%d 位退房", ins, outs)
				}
				if len(heading) != 2 || heading[1] != want {
					t.Fatalf("header = %v, want %q", heading, want)
				}
				inTab := fmt.Sprintf("今天入住（%d）", ins)
				outTab := fmt.Sprintf("今天退房（%d）", outs)
				if locale == settings.LocaleEnglish {
					inTab = []string{"Today's check-ins（0）", "Today's check-in（1）", "Today's check-ins（2）"}[ins]
					outTab = []string{"Today's check-outs（0）", "Today's check-out（1）", "Today's check-outs（2）"}[outs]
				}
				for _, tab := range []string{inTab, outTab} {
					if !strings.Contains(html.UnescapeString(body), ">"+tab+"</a>") {
						t.Errorf("missing counted tab %q", tab)
					}
				}
				if ins > 0 {
					nights := map[string]string{settings.LocaleEnglish: "1 night", settings.LocaleChinese: "1 晚"}[locale]
					if !strings.Contains(body, " · "+nights+"</div>") {
						t.Fatalf("missing one-night arrival card: %s", body)
					}
				}
			}
			check(0, 0)
			unit, err := property.GetUnit(t.Context(), a.database, a.unitID)
			if err != nil {
				t.Fatal(err)
			}
			for count := 1; count <= 2; count++ {
				unitID, err := property.AddUnit(t.Context(), a.database, unit.PropertyID, fmt.Sprintf("Unit %d", count))
				if err != nil {
					t.Fatal(err)
				}
				for i, dates := range [][2]string{{"2026-10-31", "2026-11-01"}, {"2026-10-30", "2026-10-31"}} {
					_, err := booking.Create(t.Context(), a.database, booking.Details{UnitID: unitID, Kind: booking.KindStay, Channel: booking.ChannelDirect, CheckIn: dates[0], CheckOut: dates[1]})
					if err != nil {
						t.Fatal(err)
					}
					check(count, count-1+i)
				}
			}
		})
	}
}

func TestCountedBookingCalendarHTTP(t *testing.T) {
	for _, locale := range settings.Locales() {
		t.Run(locale, func(t *testing.T) {
			a := newM3App(t, locale)
			today, err := civildate.Parse("2026-10-31")
			if err != nil {
				t.Fatal(err)
			}
			for i, dates := range [][2]string{
				{"2026-10-31", "2026-11-01"}, {"2026-11-01", "2026-11-03"}, {"2026-11-03", "2026-11-05"},
			} {
				id, err := booking.Create(t.Context(), a.database, booking.Details{UnitID: a.unitID, Kind: booking.KindStay, Channel: booking.ChannelDirect, CheckIn: dates[0], CheckOut: dates[1]})
				if err != nil {
					t.Fatal(err)
				}
				nights := []string{"1 night", "2 nights", "2 nights"}[i]
				if locale == settings.LocaleChinese {
					nights = []string{"1 晚", "2 晚", "2 晚"}[i]
				}
				a.request(t, http.MethodGet, fmt.Sprintf("/bookings/%d", id), nil, http.StatusOK, ">"+nights+"</dd>")
				if i < 2 {
					path := "/bookings?tab=inhouse"
					if i == 1 {
						path = "/bookings?tab=upcoming"
					}
					a.request(t, http.MethodGet, path, nil, http.StatusOK, " · "+nights+"</div>")
				}
			}
			for count, dates := range [][2]string{{"2026-11-01", "2026-11-02"}, {"2026-11-02", "2026-11-03"}} {
				feedID, err := ical.Add(t.Context(), a.database, a.unitID, booking.ChannelOther, fmt.Sprintf("https://example.invalid/%d.ics", count))
				if err != nil {
					t.Fatal(err)
				}
				tx, err := a.database.BeginTx(t.Context(), nil)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				if err := booking.ApplySnapshot(t.Context(), tx, feedID, a.unitID, booking.ChannelOther, []booking.ImportedEvent{{UID: "overlap", Kind: booking.KindStay, CheckIn: dates[0], CheckOut: dates[1]}}, today); err != nil {
					t.Fatal(err)
				}
				if err := tx.Commit(); err != nil {
					t.Fatal(err)
				}
				conflicts := []string{"1 booking conflict", "2 booking conflicts"}[count]
				feeds := []string{"Short stay · 1 calendar feed", "Short stay · 2 calendar feeds"}[count]
				if locale == settings.LocaleChinese {
					conflicts = fmt.Sprintf("%d 处订单冲突", count+1)
					feeds = fmt.Sprintf("短租 · %d 个日历源", count+1)
				}
				a.request(t, http.MethodGet, "/calendar?month=2026-11", nil, http.StatusOK, ">"+conflicts+"</b>", ">"+feeds+"</div>")
			}
			soon := []string{"Checks in in 1 day", "Checks in in 2 days", "Checks out in 1 day"}
			if locale == settings.LocaleChinese {
				soon = []string{"1 天后入住", "2 天后入住", "1 天后退房"}
			}
			a.request(t, http.MethodGet, "/?tab=soon", nil, http.StatusOK, soon...)
		})
	}
}

func TestCountedLeaseAndDocumentHTTP(t *testing.T) {
	for _, locale := range settings.Locales() {
		t.Run(locale, func(t *testing.T) {
			a := newM3App(t, locale)
			unit, err := property.GetUnit(t.Context(), a.database, a.unitID)
			if err != nil {
				t.Fatal(err)
			}
			for count := 1; count <= 2; count++ {
				unitID, err := property.AddUnit(t.Context(), a.database, unit.PropertyID, fmt.Sprintf("Lease unit %d", count))
				if err != nil {
					t.Fatal(err)
				}
				id, err := lease.Create(t.Context(), a.database, lease.Details{UnitID: unitID, StartsOn: "2026-10-01", RentCents: 100000, DueDay: 1})
				if err != nil {
					t.Fatal(err)
				}
				if err := lease.AddTenant(t.Context(), a.database, id, 0, &person.Details{Name: "Tenant"}); err != nil {
					t.Fatal(err)
				}
				if err := lease.Activate(t.Context(), a.database, id); err != nil {
					t.Fatal(err)
				}
				_, err = ledger.Record(t.Context(), a.database, id, ledger.Details{Kind: ledger.KindCharge, Category: ledger.CategoryRent, AmountCents: 100000, OccurredOn: fmt.Sprintf("2026-10-%d", 31-count)})
				if err != nil {
					t.Fatal(err)
				}
				note := []string{"1 lease · Longest 1 day", "2 leases · Longest 2 days"}[count-1]
				overdue := []string{"1 day overdue", "2 days overdue"}[count-1]
				items := []string{"1 item", "2 items"}[count-1]
				if locale == settings.LocaleChinese {
					note = fmt.Sprintf("%d 份租约 · 最长 %d 天", count, count)
					overdue = fmt.Sprintf("%d 天逾期", count)
					items = fmt.Sprintf("%d 项", count)
				}
				a.request(t, http.MethodGet, "/", nil, http.StatusOK, ">"+note+"</div>", ">"+overdue+"</span>", `id="todo-count">`+items+"</span>")
				a.request(t, http.MethodGet, "/leases", nil, http.StatusOK, ">"+overdue+"</span>")
				a.request(t, http.MethodGet, fmt.Sprintf("/leases/%d", id), nil, http.StatusOK, ">"+overdue+"</span>")
				path := fmt.Sprintf("/properties/%d", unit.PropertyID)
				upload := uploadRequest(t, path+"/documents", "size.txt", "text/plain", []byte(strings.Repeat("x", count)))
				upload.AddCookie(a.cookie)
				response := httptest.NewRecorder()
				a.handler.ServeHTTP(response, upload)
				if response.Code != http.StatusSeeOther {
					t.Fatalf("upload: %d %s", response.Code, response.Body)
				}
				size := []string{"1 byte", "2 bytes"}[count-1]
				if locale == settings.LocaleChinese {
					size = fmt.Sprintf("%d 字节", count)
				}
				a.request(t, http.MethodGet, path, nil, http.StatusOK, ">"+size+" · ")
			}
		})
	}
}
