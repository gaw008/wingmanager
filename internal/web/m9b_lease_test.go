package web

import (
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gaw008/wingmanager/internal/civildate"
	"github.com/gaw008/wingmanager/internal/lease"
	"github.com/gaw008/wingmanager/internal/ledger"
	"github.com/gaw008/wingmanager/internal/settings"
)

func TestM9bPaymentFormsPreviewAndSession(t *testing.T) {
	for _, locale := range settings.Locales() {
		t.Run(locale, func(t *testing.T) {
			a := newM3App(t, locale)
			a.request(t, "POST", "/leases/new", leaseForm(a.unitID), 303)
			a.request(t, "POST", "/leases/1/tenants", url.Values{"name": {"Tenant"}}, 303)
			a.request(t, "POST", "/leases/1/activate", nil, 303)
			path := "/leases/1/entries?kind=payment"
			for _, modal := range []bool{false, true} {
				request := func(method, path string, values url.Values) *httptest.ResponseRecorder {
					req := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
					req.AddCookie(a.cookie)
					if method == "POST" {
						req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					}
					if modal {
						req.Header.Set("HX-Request", "true")
						req.Header.Set("HX-Target", "modal-content")
					}
					rec := httptest.NewRecorder()
					a.handler.ServeHTTP(rec, req)
					return rec
				}
				rec := request("GET", path, nil)
				if rec.Code != 200 || !strings.Contains(rec.Body.String(), `name="amount"`) || strings.Contains(rec.Body.String(), "<!doctype") == modal {
					t.Fatalf("form modal=%v: %d %s", modal, rec.Code, rec.Body.String())
				}
				values := url.Values{"kind": {"payment"}, "category": {"bank_transfer"}, "amount": {"0"}, "occurred_on": {"2026-10-31"}, "memo": {"preserve me"}}
				rec = request("POST", "/leases/1/entries", values)
				if rec.Code != 400 || !strings.Contains(rec.Body.String(), catalogs[locale]["invalid_lease_entry"]) || !strings.Contains(rec.Body.String(), "preserve me") || strings.Contains(rec.Body.String(), "<!doctype") == modal {
					t.Fatalf("validation modal=%v: %d %s", modal, rec.Code, rec.Body.String())
				}
				values.Set("amount", "500.00")
				rec = request("POST", "/leases/1/entries", values)
				if (!modal && rec.Code != 303) || (modal && (rec.Code != 200 || rec.Header().Get("HX-Redirect") != "/leases/1")) {
					t.Fatalf("save modal=%v %d %v", modal, rec.Code, rec.Header())
				}
			}
			summary, err := ledger.Balance(t.Context(), a.database, 1, testToday())
			if err != nil || summary.BalanceCents != 200000 {
				t.Fatalf("two saves balance=%+v %v", summary, err)
			}
			for _, amount := range []string{"500.00", "2000.00", "2500.00", "0", "garbage"} {
				rec := httptest.NewRecorder()
				req := httptest.NewRequest("GET", "/leases/1/payment-preview?amount="+amount, nil)
				req.AddCookie(a.cookie)
				a.handler.ServeHTTP(rec, req)
				if rec.Code != 200 || strings.Contains(rec.Body.String(), "<!doctype") {
					t.Fatalf("preview %s %d %s", amount, rec.Code, rec.Body.String())
				}
				switch amount {
				case "500.00":
					if !strings.Contains(rec.Body.String(), "2026-09 · 500.00") || !strings.Contains(rec.Body.String(), "<b>1500.00</b>") {
						t.Fatal("incorrect partial payment preview", rec.Body.String())
					}
				case "2000.00":
					if !strings.Contains(rec.Body.String(), "2026-09 · 1000.00") || !strings.Contains(rec.Body.String(), "2026-10 · 1000.00") || !strings.Contains(rec.Body.String(), "<b>0.00</b>") {
						t.Fatal("incorrect exact settlement preview", rec.Body.String())
					}
				case "2500.00":
					if !strings.Contains(rec.Body.String(), "<b>-500.00</b>") || !strings.Contains(rec.Body.String(), "<b>500.00</b>") || !strings.Contains(rec.Body.String(), catalogs[locale]["prepayment"]) {
						t.Fatal("incorrect prepayment preview", rec.Body.String())
					}
				}
				if amount == "0" || amount == "garbage" {
					if !strings.Contains(rec.Body.String(), catalogs[locale]["preview_invalid"]) {
						t.Fatal("missing preview validation")
					}
				}
			}
			rec := httptest.NewRecorder()
			a.handler.ServeHTTP(rec, httptest.NewRequest("GET", "/leases/1/payment-preview?amount=10", nil))
			if rec.Code != 303 || rec.Header().Get("Location") != "/login" {
				t.Fatal("preview requires session")
			}
			req := httptest.NewRequest("GET", "/leases/1/payment-preview?amount=10", nil)
			req.Header.Set("HX-Request", "true")
			rec = httptest.NewRecorder()
			a.handler.ServeHTTP(rec, req)
			if rec.Code != 200 || rec.Header().Get("HX-Redirect") != "/login" || strings.Contains(rec.Body.String(), "preview-result") {
				t.Fatal("enhanced preview must navigate to login without exposing data")
			}

		})
	}
}

func TestM9bOverdueTabExactMembership(t *testing.T) {
	a := newM3App(t, settings.LocaleEnglish)
	a.request(t, "POST", "/leases/new", leaseForm(a.unitID), 303)
	a.request(t, "POST", "/leases/1/tenants", url.Values{"name": {"Tenant"}}, 303)
	a.request(t, "POST", "/leases/1/activate", nil, 303)
	// A draft with a historical charge is still not an overdue lease.
	a.request(t, "POST", "/leases/new", leaseForm(a.unitID), 303)
	if _, err := ledger.Record(t.Context(), a.database, 2, ledger.Details{Kind: ledger.KindCharge, Category: ledger.CategoryRent, AmountCents: 100, OccurredOn: "2026-08-01"}); err != nil {
		t.Fatal(err)
	}
	body := a.request(t, "GET", "/leases?filter=overdue", nil, 200)
	if strings.Count(body, `data-lease-id=`) != 1 || !strings.Contains(body, `data-lease-id="1"`) {
		t.Fatal(body)
	}
	a.request(t, "POST", "/leases/1/entries", url.Values{"kind": {"payment"}, "category": {"bank_transfer"}, "amount": {"3000"}, "occurred_on": {"2026-10-31"}}, 303)
	body = a.request(t, "GET", "/leases?filter=overdue", nil, 200)
	if strings.Contains(body, `data-lease-id=`) {
		t.Fatal("paid or draft lease listed overdue")
	}
	values, err := lease.List(t.Context(), a.database, "")
	if err != nil || len(values) != 2 {
		t.Fatal(fmt.Sprint(values, err))
	}
}

func testToday() civildate.Date { d, _ := civildate.Parse("2026-10-31"); return d }
