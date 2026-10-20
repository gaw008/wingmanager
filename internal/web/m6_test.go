package web

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/ledger"
	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/settings"
)

func TestM6HTTPMoneyReportCSVAndVoid(t *testing.T) {
	for _, locale := range settings.Locales() {
		t.Run(locale, func(t *testing.T) {
			a := newM3App(t, locale)
			unit, err := property.GetUnit(t.Context(), a.database, a.unitID)
			if err != nil {
				t.Fatal(err)
			}
			propertyID := fmt.Sprint(unit.PropertyID)
			a.request(t, "GET", "/money", nil, 200, catalogs[locale]["money"], catalogs[locale]["add_expense"], `value="2026"`)
			a.request(t, "GET", "/money/expense", nil, 200, catalogs[locale]["repairs"], `value="2026-10-31"`)
			a.request(t, "GET", "/money/income", nil, 200, catalogs[locale]["stay_payout"], catalogs[locale]["booking_optional"])
			form := url.Values{"property_id": {propertyID}, "category": {"repairs"}, "amount": {"123.45"}, "occurred_on": {"2026-01-01"}, "memo": {"Repair, <script>x</script>"}}
			a.request(t, "POST", "/money/expense", form, 303)
			list := a.request(t, "GET", "/money?year=2026&property="+propertyID+"&kind=expense", nil, 200, "123.45", "Repair, &lt;script&gt;x&lt;/script&gt;")
			if !strings.Contains(list, `data-entry-id="1"`) {
				t.Fatal(list)
			}
			for _, path := range []string{"/money?year=2025", "/money?year=2026&kind=income", "/money?year=2026&property=99999"} {
				body := a.request(t, "GET", path, nil, 200)
				if strings.Contains(body, `data-entry-id=`) {
					t.Fatalf("filter failed: %s", path)
				}
			}
			a.request(t, "GET", "/reports?year=2026&property="+propertyID, nil, 200, `id="report-income">0.00`, `id="report-expense">123.45`, `id="report-net">-123.45`, catalogs[locale]["schedule_e_disclaimer"], "/reports/schedule-e.csv?year=2026&amp;property="+propertyID, "/reports/ledger.csv?year=2026&amp;property="+propertyID)
			b, err := booking.Create(t.Context(), a.database, booking.Details{UnitID: a.unitID, Kind: booking.KindStay, Channel: booking.ChannelAirbnb, CheckIn: "2026-01-01", CheckOut: "2026-01-03"})
			if err != nil {
				t.Fatal(err)
			}
			a.request(t, "GET", "/money/income", nil, 200, fmt.Sprintf(`value="%d"`, b), "Beach house / Whole home")
			income := url.Values{"property_id": {propertyID}, "category": {"stay_payout"}, "amount": {"500.75"}, "occurred_on": {"2026-12-31"}, "booking_id": {fmt.Sprint(b)}, "memo": {"Payout"}}
			a.request(t, "POST", "/money/income", income, 303)
			a.request(t, "GET", "/money?year=2026&kind=income", nil, 200, "500.75", fmt.Sprintf(`/bookings/%d`, b))
			a.request(t, "GET", "/reports?year=2026", nil, 200, `id="report-income">500.75`, `id="report-expense">123.45`, `id="report-net">377.30`)
			other, err := property.Create(t.Context(), a.database, property.Details{Name: "Another house"}, "Unit")
			if err != nil {
				t.Fatal(err)
			}
			income.Set("property_id", fmt.Sprint(other))
			a.request(t, "POST", "/money/income", income, 400, catalogs[locale]["invalid_money_entry"])
			income.Set("property_id", propertyID)
			income.Set("category", "other_income")
			a.request(t, "POST", "/money/income", income, 400, catalogs[locale]["invalid_money_entry"])
			income.Del("booking_id")
			a.request(t, "POST", "/money/income", income, 303)
			form.Set("amount", "1.234")
			a.request(t, "POST", "/money/expense", form, 400, catalogs[locale]["invalid_money_entry"])
			form.Set("amount", "1.00")
			form.Set("category", "stay_payout")
			a.request(t, "POST", "/money/expense", form, 400, catalogs[locale]["invalid_money_entry"])
			for _, path := range []string{"/reports/schedule-e.csv", "/reports/ledger.csv"} {
				request := httptest.NewRequest("GET", path+"?year=2026&property="+propertyID, nil)
				request.AddCookie(a.cookie)
				response := httptest.NewRecorder()
				a.handler.ServeHTTP(response, request)
				if response.Code != 200 || response.Header().Get("Content-Type") != "text/csv; charset=utf-8" || !strings.HasPrefix(response.Header().Get("Content-Disposition"), "attachment;") {
					t.Fatalf("download headers/status: %+v", response)
				}
				rows, err := csv.NewReader(response.Body).ReadAll()
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(rows[0][0], ledger.DisclaimerChinese) || !strings.Contains(rows[0][0], ledger.DisclaimerEnglish) {
					t.Fatal(rows)
				}
				if path == "/reports/schedule-e.csv" {
					if len(rows) != 3 || rows[2][3] != "1001.50" || rows[2][13] != "123.45" {
						t.Fatal(rows)
					}
				} else if len(rows) != 5 {
					t.Fatal(rows)
				}
			}
			a.request(t, "POST", "/money/1/void", url.Values{"reason": {" "}}, 400, catalogs[locale]["invalid_money_entry"])
			a.request(t, "POST", "/money/1/void?year=2026&property="+propertyID, url.Values{"reason": {"Wrong repair"}}, 303)
			a.request(t, "POST", "/money/1/void", url.Values{"reason": {"Second reason"}}, 303)
			a.request(t, "GET", "/money?year=2026", nil, 200, catalogs[locale]["voided"], "Wrong repair")
			a.request(t, "GET", "/reports?year=2026&property="+propertyID, nil, 200, `id="report-income">1001.50`, `id="report-expense">0.00`, `id="report-net">1001.50`)
			a.request(t, "POST", "/money/2/void", url.Values{"reason": {"Wrong payout"}}, 303)
			a.request(t, "GET", "/reports?year=2026&property="+propertyID, nil, 200, `id="report-income">500.75`)
			a.request(t, "POST", "/money/99999/void", url.Values{"reason": {"Missing"}}, 404)
			for _, path := range []string{"/money?year=0", "/money?year=10000", "/money?year=bad", "/money?property=-1", "/money?kind=bad", "/reports?year=-1"} {
				a.request(t, "GET", path, nil, 400, catalogs[locale]["invalid_money_entry"])
			}
			a.request(t, "GET", "/reports?property=999999", nil, 404)
		})
	}
}

func TestM6HTTPRequiresSessionAndCrossOriginProtection(t *testing.T) {
	a := newM3App(t, settings.LocaleEnglish)
	for _, path := range []string{"/money", "/money/expense", "/money/income", "/reports", "/reports/schedule-e.csv?year=2026", "/reports/ledger.csv?year=2026"} {
		for _, cookie := range []*http.Cookie{nil, {Name: sessionCookie, Value: "invalid"}} {
			request := httptest.NewRequest("GET", path, nil)
			if cookie != nil {
				request.AddCookie(cookie)
			}
			response := httptest.NewRecorder()
			a.handler.ServeHTTP(response, request)
			if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/login" {
				t.Fatalf("unauthenticated %s: %d", path, response.Code)
			}
		}
	}
	for _, path := range []string{"/money/expense", "/money/income", "/money/1/void"} {
		request := httptest.NewRequest("POST", path, strings.NewReader("reason=Wrong"))
		request.Header.Set("Sec-Fetch-Site", "cross-site")
		request.AddCookie(a.cookie)
		response := httptest.NewRecorder()
		a.handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("cross-origin %s: %d", path, response.Code)
		}
	}
}
