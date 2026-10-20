package web

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/ledger"
	"github.com/gaw008/wingmanager/internal/money"
	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/settings"
)

type moneyPage struct {
	MoneyFilter     ledger.Filter
	MoneyKinds      []string
	MoneyEntries    []ledger.NamedEntry
	MoneyEntry      ledger.Entry
	MoneyCategories []string
	MoneyBookings   []bookingRow
	MoneyReport     ledger.YearReport
}

func (s *server) registerMoneyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /money", s.listMoney)
	mux.HandleFunc("GET /money/expense", s.addMoney)
	mux.HandleFunc("POST /money/expense", s.addMoney)
	mux.HandleFunc("GET /money/income", s.addMoney)
	mux.HandleFunc("POST /money/income", s.addMoney)
	mux.HandleFunc("POST /money/{id}/void", s.voidMoney)
	mux.HandleFunc("GET /reports", s.reportMoney)
	mux.HandleFunc("GET /reports/schedule-e.csv", s.reportMoney)
	mux.HandleFunc("GET /reports/ledger.csv", s.reportMoney)
}

func (s *server) moneyFilters(r *http.Request) (ledger.Filter, error) {
	today, err := settings.Today(r.Context(), s.database, s.now)
	if err != nil {
		return ledger.Filter{}, err
	}
	filter := ledger.Filter{Year: today.Year(), Kind: r.URL.Query().Get("kind")}
	if raw := r.URL.Query().Get("year"); raw != "" {
		filter.Year, err = strconv.Atoi(raw)
		if err != nil || filter.Year < 1 || filter.Year > 9999 {
			return filter, ledger.ErrInvalid
		}
	}
	if raw := r.URL.Query().Get("property"); raw != "" {
		filter.PropertyID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || filter.PropertyID < 0 {
			return filter, ledger.ErrInvalid
		}
	}
	return filter, nil
}

func (s *server) listMoney(w http.ResponseWriter, r *http.Request) {
	data := pageData{Page: "money"}
	var err error
	data.MoneyKinds = ledger.Kinds()
	data.Properties, err = property.List(r.Context(), s.database, true)
	if err == nil {
		data.MoneyFilter, err = s.moneyFilters(r)
	}
	if err == nil {
		data.MoneyEntries, err = ledger.ListEntries(r.Context(), s.database, data.MoneyFilter)
	}
	s.moneyResult(w, r, data, err)
}

func (s *server) addMoney(w http.ResponseWriter, r *http.Request) {
	kind := ledger.KindExpense
	page := "add_expense"
	if r.URL.Path == "/money/income" {
		kind, page = ledger.KindIncome, "add_income"
	}
	data := pageData{Page: page}
	data.MoneyEntry.Kind = kind
	data.MoneyCategories = ledger.Categories(kind)
	var err error
	data.Properties, err = property.List(r.Context(), s.database, true)
	if err != nil {
		s.fail(w, err)
		return
	}
	if kind == ledger.KindIncome {
		if err := s.contactAndUnitChoices(r.Context(), &data); err != nil {
			s.fail(w, err)
			return
		}
		values, err := booking.List(r.Context(), s.database, booking.Filter{})
		if err != nil {
			s.fail(w, err)
			return
		}
		data.MoneyBookings = bookingRows(values, data)
	}
	today, err := settings.Today(r.Context(), s.database, s.now)
	if err != nil {
		s.fail(w, err)
		return
	}
	data.MoneyEntry.OccurredOn = today.String()
	if r.Method == http.MethodGet {
		s.render(w, r, data, http.StatusOK)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.moneyResult(w, r, data, ledger.ErrInvalid)
		return
	}
	v := &data.MoneyEntry
	v.Category, v.OccurredOn, v.Memo = r.PostForm.Get("category"), r.PostForm.Get("occurred_on"), r.PostForm.Get("memo")
	v.PropertyID, err = strconv.ParseInt(r.PostForm.Get("property_id"), 10, 64)
	if err == nil && r.PostForm.Get("booking_id") != "" {
		v.BookingID, err = strconv.ParseInt(r.PostForm.Get("booking_id"), 10, 64)
	}
	if err != nil {
		s.moneyResult(w, r, data, ledger.ErrInvalid)
		return
	}
	v.AmountCents, err = money.Parse(r.PostForm.Get("amount"))
	if err == nil {
		_, err = ledger.RecordProperty(r.Context(), s.database, v.PropertyID, v.BookingID, v.Details)
	}
	if err != nil {
		s.moneyResult(w, r, data, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/money?year=%s&property=%d", v.OccurredOn[:4], v.PropertyID), http.StatusSeeOther)
}

func (s *server) voidMoney(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.moneyResult(w, r, pageData{Page: "money"}, ledger.ErrInvalid)
		return
	}
	if err := ledger.Void(r.Context(), s.database, id, r.PostForm.Get("reason")); err != nil {
		s.moneyResult(w, r, pageData{Page: "money"}, err)
		return
	}
	http.Redirect(w, r, "/money?"+r.URL.RawQuery, http.StatusSeeOther)
}

func (s *server) reportMoney(w http.ResponseWriter, r *http.Request) {
	data := pageData{Page: "reports"}
	var err error
	data.Properties, err = property.List(r.Context(), s.database, true)
	if err == nil {
		data.MoneyFilter, err = s.moneyFilters(r)
	}
	if err == nil {
		data.MoneyReport, err = ledger.Report(r.Context(), s.database, data.MoneyFilter.Year, data.MoneyFilter.PropertyID)
	}
	if err != nil {
		s.moneyResult(w, r, data, err)
		return
	}
	if r.URL.Path == "/reports" {
		s.render(w, r, data, http.StatusOK)
		return
	}
	var content bytes.Buffer
	filename := fmt.Sprintf("ledger-%d.csv", data.MoneyReport.Year)
	if r.URL.Path == "/reports/schedule-e.csv" {
		filename = fmt.Sprintf("schedule-e-%d.csv", data.MoneyReport.Year)
		err = ledger.WriteScheduleE(&content, data.MoneyReport)
	} else {
		err = ledger.WriteLedger(&content, data.MoneyReport)
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Cache-Control", "no-store")
	if _, err := w.Write(content.Bytes()); err != nil {
		log.Printf("write CSV response: %v", err)
	}
}

func (s *server) moneyResult(w http.ResponseWriter, r *http.Request, data pageData, err error) {
	switch {
	case err == nil:
		s.render(w, r, data, http.StatusOK)
	case errors.Is(err, ledger.ErrNotFound):
		s.notFound(w, r)
	case errors.Is(err, ledger.ErrAutoRent):
		data.Error = "auto_rent_void"
		s.render(w, r, data, http.StatusBadRequest)
	case errors.Is(err, ledger.ErrInvalid), errors.Is(err, money.ErrAmount):
		data.Error = "invalid_money_entry"
		s.render(w, r, data, http.StatusBadRequest)
	default:
		s.fail(w, err)
	}
}
