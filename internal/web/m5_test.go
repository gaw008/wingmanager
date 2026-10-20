package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gaw008/wingmanager/internal/lease"
	"github.com/gaw008/wingmanager/internal/ledger"
	"github.com/gaw008/wingmanager/internal/settings"
)

func leaseForm(unit int64) url.Values {
	return url.Values{"unit_id": {fmt.Sprint(unit)}, "starts_on": {"2026-08-01"}, "ends_on": {"2026-12-31"}, "rent": {"1000.00"}, "due_day": {"5"}, "deposit": {"2000.00"}, "notes": {"Lease notes"}}
}
func TestM5HTTPCreateActivateRentPaymentDepositVoidAndOverdue(t *testing.T) {
	for _, locale := range settings.Locales() {
		t.Run(locale, func(t *testing.T) {
			a := newM3App(t, locale)
			form := leaseForm(a.unitID)
			a.request(t, "GET", "/leases/new", nil, 200, catalogs[locale]["monthly_rent"])
			a.request(t, "POST", "/leases/new", form, 303)
			values, err := lease.List(t.Context(), a.database, "")
			if err != nil || len(values) != 1 {
				t.Fatalf("%v %v", values, err)
			}
			id := values[0].ID
			path := fmt.Sprintf("/leases/%d", id)
			a.request(t, "POST", path+"/activate", nil, 400, catalogs[locale]["lease_tenant_required"])
			a.request(t, "POST", path+"/tenants", url.Values{"name": {"Tenant <script>x</script>"}, "email": {"tenant@example.com"}}, 303)
			a.request(t, "POST", path+"/activate", nil, 303)
			a.request(t, "GET", path, nil, 200, `id="lease-balance">3000.00`, "2026-08", "2026-09", "2026-10", "Tenant &lt;script&gt;x&lt;/script&gt;")
			a.request(t, "GET", "/calendar?month=2026-10", nil, 200, `class="occupied lease"`, `href="`+path+`"`, catalogs[locale]["lease"])
			a.request(t, "POST", "/bookings/new", a.form("2026-10-30", "2026-11-01"), 409, catalogs[locale]["booking_lease_conflict"])
			a.request(t, "POST", path+"/entries", url.Values{"kind": {"payment"}, "category": {"bank_transfer"}, "amount": {"1500.25"}, "occurred_on": {"2026-10-30"}}, 303)
			a.request(t, "GET", path, nil, 200, `id="lease-balance">1499.75`)
			// FIFO: August is paid, September partly paid, October remains wholly unpaid.
			a.request(t, "GET", "/overdue", nil, 200, `data-lease-id="1"`, "1499.75", "2026-09-05", `56 `+catalogs[locale]["days_overdue"])
			a.request(t, "POST", path+"/entries", url.Values{"kind": {"credit"}, "category": {"concession"}, "amount": {"499.75"}, "occurred_on": {"2026-10-31"}}, 303)
			a.request(t, "GET", "/overdue", nil, 200, "2026-10-05", `26 `+catalogs[locale]["days_overdue"])
			a.request(t, "POST", path+"/entries", url.Values{"kind": {"deposit_in"}, "category": {"deposit"}, "amount": {"2000.00"}, "occurred_on": {"2026-08-01"}}, 303)
			a.request(t, "POST", path+"/apply-deposit", url.Values{"amount": {"1000.00"}, "occurred_on": {"2026-10-31"}, "memo": {"Deposit applied"}}, 303)
			a.request(t, "GET", path, nil, 200, `id="lease-balance">0.00`, `id="deposit-held">1000.00`)
			body := a.request(t, "GET", "/overdue", nil, 200)
			if strings.Contains(body, `data-lease-id=`) {
				t.Fatal("fully paid lease remains overdue")
			}
			entries, err := ledger.List(t.Context(), a.database, id)
			if err != nil {
				t.Fatal(err)
			}
			var rent, payment int64
			for _, e := range entries {
				if e.Period != "" {
					rent = e.ID
				}
				if e.Kind == ledger.KindPayment && e.Category == ledger.CategoryBankTransfer {
					payment = e.ID
				}
			}
			a.request(t, "POST", fmt.Sprintf("%s/entries/%d/void", path, rent), url.Values{"reason": {"Correction"}}, 400, catalogs[locale]["auto_rent_void"])
			a.request(t, "POST", fmt.Sprintf("%s/entries/%d/void", path, payment), url.Values{"reason": {"Wrong payment"}}, 303)
			a.request(t, "GET", path, nil, 200, `id="lease-balance">1500.25`, "Wrong payment")
			a.request(t, "GET", "/leases?status=active", nil, 200, `data-lease-id="1"`)
			form.Set("starts_on", "2026-08-02")
			a.request(t, "POST", path+"/edit", form, 400, catalogs[locale]["invalid_lease_entry"])
			form.Set("starts_on", "2026-08-01")
			form.Set("rent", "1200.50")
			a.request(t, "POST", path+"/edit", form, 303)
			a.request(t, "POST", path+"/end", url.Values{"ends_on": {"2026-10-31"}}, 303)
			a.request(t, "GET", path, nil, 200, catalogs[locale]["ended"], `name="rent" type="number" min="0.01" step="0.01" value="1200.50" required readonly`)
			a.request(t, "GET", "/leases?status=ended", nil, 200, `data-lease-id="1"`)
		})
	}
}
func TestM5RoutesAuthenticationAndOrigin(t *testing.T) {
	a := newM3App(t, settings.LocaleEnglish)
	for _, path := range []string{"/leases", "/overdue", "/leases/new", "/leases/1"} {
		request := httptest.NewRequest("GET", path, nil)
		response := httptest.NewRecorder()
		a.handler.ServeHTTP(response, request)
		if response.Code != 303 || response.Header().Get("Location") != "/login" {
			t.Fatalf("unprotected %s", path)
		}
	}
	for _, path := range []string{"/leases/new", "/leases/1/activate", "/leases/1/entries", "/leases/1/apply-deposit", "/leases/1/entries/1/void"} {
		request := httptest.NewRequest("POST", "http://wing.example"+path, nil)
		request.AddCookie(a.cookie)
		request.Header.Set("Origin", "https://other.example")
		response := httptest.NewRecorder()
		a.handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("cross-origin %s status=%d", path, response.Code)
		}
	}
}
