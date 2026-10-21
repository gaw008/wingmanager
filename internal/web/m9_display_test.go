package web

import (
	"encoding/csv"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/settings"
)

func TestM9LeaseDisplayCorrections(t *testing.T) {
	for _, tt := range []struct{ locale, start, end, period, due, label, dueValue string }{
		{"zh-CN", "2025年12月1日", "2027年1月31日", "2026年9月", "11月1日", "收租日", "每月 1 日"},
		{"en", "Dec 1, 2025", "Jan 31, 2027", "Sep 2026", "Nov 1", "Rent due day", "1st of each month"},
	} {
		t.Run(tt.locale, func(t *testing.T) {
			a := newM3App(t, tt.locale)
			form := leaseForm(a.unitID)
			form.Set("starts_on", "2025-12-01")
			form.Set("ends_on", "2027-01-31")
			form.Set("rent", "2250.00")
			form.Set("due_day", "1")
			a.request(t, "POST", "/leases/new", form, 303)
			a.request(t, "POST", "/leases/1/tenants", url.Values{"name": {"Tenant"}}, 303)
			a.request(t, "POST", "/leases/1/activate", nil, 303)
			t.Run("dates", func(t *testing.T) {
				for _, path := range []string{"/leases/1", "/leases", "/"} {
					a.request(t, "GET", path, nil, 200, tt.start, tt.end)
				}
				body := a.request(t, "GET", "/leases/1", nil, 200, tt.period, tt.due)
				for _, raw := range []string{"2025-12-01", "2027-01-31", "2026-09", "2026-11-01"} {
					if strings.Contains(body, raw) {
						t.Errorf("raw date %s in lease display", raw)
					}
				}
			})
			t.Run("terms", func(t *testing.T) {
				a.request(t, "GET", "/leases/1", nil, 200, "<dt>"+tt.label+"</dt><dd>"+tt.dueValue+"</dd>")
			})
			t.Run("money", func(t *testing.T) {
				a.request(t, "GET", "/leases/1", nil, 200, "$2,250.00", "$0.00")
			})
			t.Run("initial_preview", func(t *testing.T) {
				for _, modal := range []bool{false, true} {
					req := httptest.NewRequest("GET", "/leases/1/entries?kind=payment", nil)
					req.AddCookie(a.cookie)
					if modal {
						req.Header.Set("HX-Request", "true")
						req.Header.Set("HX-Target", "modal-content")
					}
					rec := httptest.NewRecorder()
					a.handler.ServeHTTP(rec, req)
					body := rec.Body.String()
					if rec.Code != 200 || !strings.Contains(body, `value="24750.00"`) || !strings.Contains(body, `class="preview-result"`) || !strings.Contains(body, `<b>$0.00</b>`) || !strings.Contains(body, tt.period) || strings.Contains(body, catalogs[tt.locale]["preview_invalid"]) {
						t.Errorf("initial preview modal=%v: %d %s", modal, rec.Code, body)
					}
				}
			})
			a.request(t, "GET", "/leases/1/edit", nil, 200, `value="2250.00"`, `value="2025-12-01"`, `value="2027-01-31"`)
		})
	}
}

func TestM9BookingMoneyTaskDateDisplays(t *testing.T) {
	for _, tt := range []struct{ locale, from, to string }{{settings.LocaleChinese, "10月29日", "10月31日"}, {settings.LocaleEnglish, "Oct 29", "Oct 31"}} {
		t.Run(tt.locale, func(t *testing.T) {
			a := newM3App(t, tt.locale)
			a.request(t, "POST", "/bookings/new", a.form("2026-10-29", "2026-10-31"), 303)
			for _, path := range []string{"/bookings?tab=inhouse", "/bookings/1", "/?tab=out"} {
				a.request(t, "GET", path, nil, 200, tt.from, tt.to)
			}
			a.request(t, "GET", "/bookings/1/edit", nil, 200, `value="2026-10-29"`, `value="2026-10-31"`)
			a.request(t, "GET", "/tasks", nil, 200, "<time>"+tt.to+"</time>")
			a.request(t, "GET", "/tasks/1", nil, 200, `value="2026-10-31"`)
			a.request(t, "POST", "/money/expense", url.Values{"property_id": {"1"}, "category": {"repairs"}, "amount": {"2250.00"}, "occurred_on": {"2026-10-31"}}, 303)
			for _, path := range []string{"/money?year=2026", "/money/1", "/money/1/void"} {
				a.request(t, "GET", path, nil, 200, tt.to, "$2,250.00")
			}
			body := a.request(t, "GET", "/reports/ledger.csv?year=2026", nil, 200, "2250.00", "2026-10-31")
			if strings.Contains(body, "$2,250.00") || strings.Contains(body, "2,250.00") {
				t.Fatal("display money leaked into CSV", body)
			}
			for _, currency := range []struct{ code, want string }{{"EUR", "€2,250.00"}, {"JPY", "¥2,250"}, {"CHF", "2,250.00 CHF"}} {
				value, err := settings.Get(t.Context(), a.database)
				if err != nil {
					t.Fatal(err)
				}
				value.Currency = currency.code
				if err := settings.Save(t.Context(), a.database, value); err != nil {
					t.Fatal(err)
				}
				a.request(t, "GET", "/money?year=2026", nil, 200, currency.want)
				a.request(t, "GET", "/money/1", nil, 200, currency.want)
				csvBody := a.request(t, "GET", "/reports/ledger.csv?year=2026", nil, 200)
				rows, err := csv.NewReader(strings.NewReader(csvBody)).ReadAll()
				if err != nil {
					t.Fatal(err)
				}
				if len(rows) != 3 || rows[2][7] != "2250.00" || rows[2][8] != "2026-10-31" {
					t.Fatalf("CSV changed with %s: %v", currency.code, rows)
				}
			}
		})
	}
}

func TestM9CurrentDisplayYearUsesSettingsTimezone(t *testing.T) {
	for _, tt := range []struct{ locale, losAngeles, tokyo string }{
		{"zh-CN", "12月31日", "2026年12月31日"},
		{"en", "Dec 31", "Dec 31, 2026"},
	} {
		t.Run(tt.locale, func(t *testing.T) {
			a := newM3App(t, tt.locale)
			now := func() time.Time { return time.Date(2027, 1, 1, 1, 0, 0, 0, time.UTC) }
			var err error
			a.handler, err = newHandler(a.database, now, ical.NewService(a.database, nil, now), Config{DataDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			a.request(t, "POST", "/money/expense", url.Values{"property_id": {"1"}, "category": {"repairs"}, "amount": {"1.00"}, "occurred_on": {"2026-12-31"}}, 303)
			a.request(t, "GET", "/money?year=2026", nil, 200, "<td>"+tt.losAngeles+"</td>")
			value, err := settings.Get(t.Context(), a.database)
			if err != nil {
				t.Fatal(err)
			}
			value.Timezone = "Asia/Tokyo"
			if err := settings.Save(t.Context(), a.database, value); err != nil {
				t.Fatal(err)
			}
			a.request(t, "GET", "/money?year=2026", nil, 200, "<td>"+tt.tokyo+"</td>")
		})
	}
}
