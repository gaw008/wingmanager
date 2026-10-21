package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gaw008/wingmanager/internal/ledger"
	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/settings"
)

func TestM9bMoneyModalAndFullPageFallback(t *testing.T) {
	for _, locale := range settings.Locales() {
		t.Run(locale, func(t *testing.T) {
			a := newM3App(t, locale)
			unit, err := property.GetUnit(t.Context(), a.database, a.unitID)
			if err != nil {
				t.Fatal(err)
			}
			body := a.request(t, http.MethodGet, "/money?year=2026", nil, http.StatusOK, catalogs[locale]["money_ledger_tab"], catalogs[locale]["money_report_tab"], `class="empty"`, `data-modal="action-modal"`)
			if !strings.Contains(body, `/static/icons.svg#wallet`) || !strings.Contains(body, `href="/money/expense"`) {
				t.Fatal("empty money list has no icon or primary action")
			}
			for _, modal := range []bool{false, true} {
				request := func(method string, form url.Values) *httptest.ResponseRecorder {
					req := httptest.NewRequest(method, "/money/expense", strings.NewReader(form.Encode()))
					req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					req.AddCookie(a.cookie)
					if modal {
						req.Header.Set("HX-Request", "true")
						req.Header.Set("HX-Target", "modal-content")
					}
					rec := httptest.NewRecorder()
					a.handler.ServeHTTP(rec, req)
					return rec
				}
				rec := request(http.MethodGet, nil)
				if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "<!doctype") == modal || !strings.Contains(rec.Body.String(), `action="/money/expense"`) {
					t.Fatalf("money form modal=%v: %d %s", modal, rec.Code, rec.Body)
				}
				form := url.Values{"property_id": {fmt.Sprint(unit.PropertyID)}, "category": {ledger.CategoryRepairs}, "amount": {"0"}, "occurred_on": {"2026-10-31"}, "memo": {"Preserve this repair memo"}}
				rec = request(http.MethodPost, form)
				if rec.Code != http.StatusBadRequest || strings.Contains(rec.Body.String(), "<!doctype") == modal || !strings.Contains(rec.Body.String(), catalogs[locale]["invalid_money_entry"]) || !strings.Contains(rec.Body.String(), form.Get("memo")) {
					t.Fatalf("money validation modal=%v: %d %s", modal, rec.Code, rec.Body)
				}
				form.Set("amount", "12.34")
				rec = request(http.MethodPost, form)
				if (!modal && rec.Code != http.StatusSeeOther) || (modal && (rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("HX-Redirect"), "/money?"))) {
					t.Fatalf("money save modal=%v: %d %+v", modal, rec.Code, rec.Header())
				}
			}
			values, err := ledger.ListEntries(t.Context(), a.database, ledger.Filter{Year: 2026, Kind: ledger.KindExpense})
			if err != nil || len(values) != 2 || values[0].AmountCents != 1234 || values[1].AmountCents != 1234 {
				t.Fatalf("money form writes %+v, %v", values, err)
			}
			query := "year=2026&property=" + fmt.Sprint(unit.PropertyID)
			a.request(t, http.MethodGet, "/money?"+query, nil, http.StatusOK, `hx-get="/money/1/void?`, `data-entry-id="1"`)
			a.request(t, http.MethodGet, "/money?tab=reports&"+query, nil, http.StatusOK, `class="stats stats-three"`, `id="report-expense">$24.68`, catalogs[locale]["monthly_breakdown"], catalogs[locale]["schedule_e_disclaimer"])
		})
	}
}
