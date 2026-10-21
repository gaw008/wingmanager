package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"github.com/gaw008/wingmanager/internal/lease"
	"github.com/gaw008/wingmanager/internal/ledger"
	"github.com/gaw008/wingmanager/internal/money"
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/settings"
)

type leaseRow struct {
	lease.Lease
	ledger.Summary
	UnitLabel, TenantNames string
}
type entryForm struct {
	Kind       string
	Categories []string
}
type leasePage struct {
	LeaseLabel, LedgerFilter, LeaseAction, LeaseActionTitle, EntryKind string
	EntryCategories                                                    []string
	LedgerRows                                                         []ledger.BalancedEntry
	RentPeriods                                                        []ledger.RentPeriod
	PaymentPreview                                                     ledger.PaymentPreview
	PreviewInvalid                                                     bool
	Lease                                                              lease.Lease
	LeaseRows                                                          []leaseRow
	LeaseStatusFilter                                                  string
	Tenants                                                            []person.Person
	Entries                                                            []ledger.Entry
	Balance                                                            ledger.Summary
	EntryForms                                                         []entryForm
	Today                                                              string
}

func (s *server) registerLeaseRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /leases", s.listLeases)
	mux.HandleFunc("GET /overdue", redirectView("/leases", "filter", "overdue"))
	mux.HandleFunc("GET /leases/new", s.editLease)
	mux.HandleFunc("POST /leases/new", s.editLease)
	mux.HandleFunc("GET /leases/{id}", s.editLease)
	mux.HandleFunc("GET /leases/{id}/payment-preview", s.paymentPreview)
	mux.HandleFunc("GET /leases/{id}/{action}", s.leaseForm)
	mux.HandleFunc("GET /leases/{id}/entries/{entry}/void", s.voidMoneyForm)
	mux.HandleFunc("POST /leases/{id}/{action}", s.leaseAction)
	mux.HandleFunc("POST /leases/{id}/entries/{entry}/void", s.voidLeaseEntry)
}
func (s *server) listLeases(w http.ResponseWriter, r *http.Request) {
	data := pageData{Page: "leases"}
	if r.URL.Query().Get("filter") == "overdue" {
		data.Page = "overdue"
	}
	data.LeaseStatusFilter = r.URL.Query().Get("status")
	if err := s.contactAndUnitChoices(r.Context(), &data); err != nil {
		s.fail(w, err)
		return
	}
	values, err := lease.List(r.Context(), s.database, data.LeaseStatusFilter)
	if err != nil {
		s.leaseResult(w, r, data, err)
		return
	}
	today, err := settings.Today(r.Context(), s.database, s.now)
	if err != nil {
		s.fail(w, err)
		return
	}
	labels := map[int64]string{}
	for _, u := range data.UnitChoices {
		labels[u.ID] = u.Label
	}
	for _, v := range values {
		summary, err := ledger.Balance(r.Context(), s.database, v.ID, today)
		if err != nil {
			s.fail(w, err)
			return
		}
		if data.Page == "overdue" && (summary.DaysOverdue == 0 || v.Status == lease.StatusDraft) {
			continue
		}
		tenants, err := lease.Tenants(r.Context(), s.database, v.ID)
		if err != nil {
			s.fail(w, err)
			return
		}
		names := ""
		for i, p := range tenants {
			if i > 0 {
				names += ", "
			}
			names += p.Name
		}
		data.LeaseRows = append(data.LeaseRows, leaseRow{Lease: v, Summary: summary, UnitLabel: labels[v.UnitID], TenantNames: names})
	}
	s.render(w, r, data, http.StatusOK)
}
func (s *server) loadLeasePage(r *http.Request, id int64) (pageData, error) {
	data := pageData{Page: "lease_detail"}
	var err error
	data.Lease, err = lease.Get(r.Context(), s.database, id)
	if err != nil {
		return data, err
	}
	if err := s.contactAndUnitChoices(r.Context(), &data); err != nil {
		return data, err
	}
	data.Tenants, err = lease.Tenants(r.Context(), s.database, id)
	if err != nil {
		return data, err
	}
	today, err := settings.Today(r.Context(), s.database, s.now)
	if err != nil {
		return data, err
	}
	data.Today = today.String()
	projection, err := ledger.Project(r.Context(), s.database, data.Lease, today)
	if err != nil {
		return data, err
	}
	data.Balance = projection.Summary
	data.RentPeriods = projection.Periods
	data.LedgerFilter = r.URL.Query().Get("ledger")
	for _, row := range projection.Entries {
		data.Entries = append(data.Entries, row.Entry)
		if data.LedgerFilter == "" || data.LedgerFilter == row.Kind || (data.LedgerFilter == "deposit" && (row.Kind == ledger.KindDepositIn || row.Kind == ledger.KindDepositOut)) {
			data.LedgerRows = append(data.LedgerRows, row)
		}
	}
	slices.Reverse(data.LedgerRows)
	for _, u := range data.UnitChoices {
		if u.ID == data.Lease.UnitID {
			data.LeaseLabel = u.Label
			break
		}
	}
	for _, kind := range ledger.LeaseKinds() {
		data.EntryForms = append(data.EntryForms, entryForm{kind, ledger.Categories(kind)})
	}
	return data, nil
}
func (s *server) editLease(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") != "" {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			s.notFound(w, r)
			return
		}
		data, err := s.loadLeasePage(r, id)
		s.leaseResult(w, r, data, err)
		return
	}
	data := pageData{Page: "new_lease"}
	data.Lease.Status = lease.StatusDraft
	data.Lease.DueDay = 1
	if err := s.contactAndUnitChoices(r.Context(), &data); err != nil {
		s.fail(w, err)
		return
	}
	if r.Method == http.MethodGet {
		s.render(w, r, data, http.StatusOK)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.leaseResult(w, r, data, lease.ErrInvalid)
		return
	}
	details, err := parseLease(r)
	data.Lease.Details = details
	if err != nil {
		s.leaseResult(w, r, data, err)
		return
	}
	id, err := lease.Create(r.Context(), s.database, details)
	if err != nil {
		s.leaseResult(w, r, data, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/leases/%d", id), http.StatusSeeOther)
}
func parseLease(r *http.Request) (lease.Details, error) {
	v := lease.Details{StartsOn: r.PostForm.Get("starts_on"), EndsOn: r.PostForm.Get("ends_on"), Notes: r.PostForm.Get("notes")}
	var err error
	v.UnitID, err = strconv.ParseInt(r.PostForm.Get("unit_id"), 10, 64)
	if err != nil {
		return v, lease.ErrInvalid
	}
	v.DueDay, err = strconv.ParseInt(r.PostForm.Get("due_day"), 10, 64)
	if err != nil {
		return v, lease.ErrInvalid
	}
	v.RentCents, err = money.Parse(r.PostForm.Get("rent"))
	if err != nil {
		return v, err
	}
	v.DepositCents, err = money.Parse(r.PostForm.Get("deposit"))
	return v, err
}
func (s *server) leaseAction(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	data, err := s.loadLeasePage(r, id)
	if err != nil {
		s.leaseResult(w, r, data, err)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.leaseResult(w, r, data, lease.ErrInvalid)
		return
	}
	if r.PathValue("action") != "activate" && r.PathValue("action") != "remove-tenant" {
		if !prepareLeaseAction(r, &data) {
			s.notFound(w, r)
			return
		}
	}
	today, err := settings.Today(r.Context(), s.database, s.now)
	if err != nil {
		s.fail(w, err)
		return
	}
	switch r.PathValue("action") {
	case "edit":
		var v lease.Details
		v, err = parseLease(r)
		if err == nil {
			err = lease.Update(r.Context(), s.database, id, v)
		}
		data.Lease.Details = v
	case "activate":
		err = lease.Activate(r.Context(), s.database, id)
		if err == nil {
			err = ledger.PostLeaseRent(r.Context(), s.database, id, today)
		}
	case "end":
		err = lease.End(r.Context(), s.database, id, r.PostForm.Get("ends_on"))
	case "tenants":
		var personID int64
		if raw := r.PostForm.Get("person_id"); raw != "" {
			personID, err = strconv.ParseInt(raw, 10, 64)
			if err != nil {
				err = lease.ErrInvalid
				break
			}
		}
		var p *person.Details
		if r.PostForm.Get("name") != "" || r.PostForm.Get("email") != "" || r.PostForm.Get("phone") != "" {
			p = &person.Details{Name: r.PostForm.Get("name"), Email: r.PostForm.Get("email"), Phone: r.PostForm.Get("phone")}
		}
		err = lease.AddTenant(r.Context(), s.database, id, personID, p)
	case "remove-tenant":
		var personID int64
		personID, err = strconv.ParseInt(r.PostForm.Get("person_id"), 10, 64)
		if err == nil {
			err = lease.RemoveTenant(r.Context(), s.database, id, personID)
		} else {
			err = lease.ErrInvalid
		}
	case "entries", "apply-deposit":
		var amount int64
		amount, err = money.Parse(r.PostForm.Get("amount"))
		if err != nil {
			break
		}
		if r.PathValue("action") == "apply-deposit" {
			err = ledger.ApplyDeposit(r.Context(), s.database, id, amount, r.PostForm.Get("occurred_on"), r.PostForm.Get("memo"))
		} else {
			_, err = ledger.Record(r.Context(), s.database, id, ledger.Details{Kind: r.PostForm.Get("kind"), Category: r.PostForm.Get("category"), AmountCents: amount, OccurredOn: r.PostForm.Get("occurred_on"), Memo: r.PostForm.Get("memo")})
		}
	default:
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.leaseResult(w, r, data, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/leases/%d", id), http.StatusSeeOther)
}
func (s *server) voidLeaseEntry(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	data, err := s.loadLeasePage(r, id)
	if err != nil {
		s.leaseResult(w, r, data, err)
		return
	}
	entryID, err := strconv.ParseInt(r.PathValue("entry"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	found := false
	for _, e := range data.Entries {
		if e.ID == entryID {
			found = true
			break
		}
	}
	if !found {
		s.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.leaseResult(w, r, data, ledger.ErrInvalid)
		return
	}
	data.Page = "void_entry"
	data.FormAction = r.URL.Path
	data.MoneyEntry, err = ledger.Get(r.Context(), s.database, entryID)
	if err != nil {
		s.leaseResult(w, r, data, err)
		return
	}
	err = ledger.Void(r.Context(), s.database, entryID, r.PostForm.Get("reason"))
	if err != nil {
		s.leaseResult(w, r, data, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/leases/%d", id), http.StatusSeeOther)
}
func (s *server) leaseResult(w http.ResponseWriter, r *http.Request, data pageData, err error) {
	status := http.StatusBadRequest
	switch {
	case err == nil:
		status = http.StatusOK
	case errors.Is(err, lease.ErrNotFound), errors.Is(err, ledger.ErrNotFound):
		s.notFound(w, r)
		return
	case errors.Is(err, lease.ErrConflict):
		data.Error = "lease_conflict"
		status = 409
	case errors.Is(err, lease.ErrTenant):
		data.Error = "lease_tenant_required"
	case errors.Is(err, ledger.ErrAutoRent):
		data.Error = "auto_rent_void"
	case errors.Is(err, ledger.ErrDeposit):
		data.Error = "invalid_deposit_application"
	case errors.Is(err, lease.ErrInvalid), errors.Is(err, ledger.ErrInvalid), errors.Is(err, person.ErrName), errors.Is(err, money.ErrAmount):
		data.Error = "invalid_lease_entry"
	default:
		s.fail(w, err)
		return
	}
	if data.Page == "lease_action" && data.EntryKind == ledger.KindPayment {
		if data.FormValues == nil {
			data.FormValues = r.PostForm
		}
		if err := s.loadPaymentPreview(r, &data, data.FormValues.Get("amount")); err != nil {
			s.fail(w, err)
			return
		}
	}
	s.render(w, r, data, status)
}

// prepareLeaseAction selects the same form for enhanced requests and ordinary links.
func prepareLeaseAction(r *http.Request, data *pageData) bool {
	action := r.PathValue("action")
	data.LeaseAction, data.LeaseActionTitle = action, action
	switch action {
	case "entries":
		kind := r.FormValue("kind")
		if !slices.Contains(ledger.LeaseKinds(), kind) {
			return false
		}
		data.EntryKind, data.EntryCategories = kind, ledger.Categories(kind)
		data.LeaseActionTitle = kind
		if kind == ledger.KindPayment || kind == ledger.KindCharge || kind == ledger.KindCredit {
			data.LeaseActionTitle = "record_" + kind
		}
	case "apply-deposit":
		data.LeaseActionTitle = "apply_deposit"
	case "edit":
		data.LeaseActionTitle = "change_rent"
	case "tenants":
		data.LeaseActionTitle = "add_tenant"
	case "end":
		data.LeaseActionTitle = "end_lease"
	default:
		return false
	}
	data.Page = "lease_action"
	data.FormAction = fmt.Sprintf("/leases/%d/%s", data.Lease.ID, action)
	return true
}

func (s *server) leaseForm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	data, err := s.loadLeasePage(r, id)
	if err != nil {
		s.leaseResult(w, r, data, err)
		return
	}
	if !prepareLeaseAction(r, &data) {
		s.notFound(w, r)
		return
	}
	if data.EntryKind == ledger.KindPayment {
		data.FormValues = url.Values{"amount": {money.Format(max(data.Balance.BalanceCents, 0))}}
	}
	s.leaseResult(w, r, data, nil)
}

func (s *server) paymentPreview(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if _, err := lease.Get(r.Context(), s.database, id); err != nil {
		s.leaseResult(w, r, pageData{}, err)
		return
	}
	data := pageData{Page: "payment_preview"}
	data.Lease.ID = id
	if err := s.loadPaymentPreview(r, &data, r.URL.Query().Get("amount")); err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, r, data, http.StatusOK)
}

// loadPaymentPreview shares parsing and allocation between initial forms and edits.
func (s *server) loadPaymentPreview(r *http.Request, data *pageData, raw string) error {
	amount, err := money.Parse(raw)
	if err == nil {
		data.PaymentPreview, err = ledger.PreviewPayment(r.Context(), s.database, data.Lease.ID, amount)
	}
	if errors.Is(err, money.ErrAmount) || errors.Is(err, ledger.ErrInvalid) {
		data.PreviewInvalid = true
		return nil
	}
	return err
}
