package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/settings"
	"github.com/gaw008/wingmanager/internal/task"
)

func TestM9bAccountPagesComponentsAndEmptyActions(t *testing.T) {
	for _, locale := range settings.Locales() {
		t.Run(locale, func(t *testing.T) {
			a := newM3App(t, locale)
			a.request(t, "GET", "/properties", nil, http.StatusOK, `class="property-card panel panel-pad"`, "Beach house", catalogs[locale]["properties_units"])
			unit, err := property.GetUnit(t.Context(), a.database, a.unitID)
			if err != nil {
				t.Fatal(err)
			}
			path := fmt.Sprintf("/properties/%d", unit.PropertyID)
			body := a.request(t, "GET", path, nil, http.StatusOK, `class="title-row"`, `href="/bookings?section=sync"`, `data-copy-target=`, `action="/units/`+fmt.Sprint(unit.ID)+`/token"`, `enctype="multipart/form-data"`)
			if strings.Contains(body, `data-feed-id=`) {
				t.Fatal("feed management must live in short-term rentals")
			}
			for _, test := range []struct{ path, action string }{{"/people", "/people/new"}, {"/tasks", "/tasks/new"}} {
				body := a.request(t, "GET", test.path, nil, http.StatusOK, `class="empty"`, `class="btn btn-primary" href="`+test.action+`"`)
				empty := regexp.MustCompile(`<div class="empty">([\s\S]*?)</div>`).FindString(body)
				if !strings.Contains(empty, `<svg class="icon"`) {
					t.Fatalf("%s empty state has no icon", test.path)
				}
			}
			a.request(t, "GET", "/settings", nil, http.StatusOK, `class="settings-stack form-panel"`, catalogs[locale]["regional_settings"], `class="select" id="locale"`)
			for _, path := range []string{"/properties/new", "/people/new", "/tasks/new"} {
				a.request(t, "GET", path, nil, http.StatusOK, `class="input"`, `class="btn btn-primary"`)
			}
			if err := property.SetArchived(t.Context(), a.database, unit.PropertyID, true); err != nil {
				t.Fatal(err)
			}
			body = a.request(t, "GET", "/properties", nil, http.StatusOK, `class="empty"`)
			if !strings.Contains(regexp.MustCompile(`<div class="empty">([\s\S]*?)</div>`).FindString(body), `href="/properties/new"`) {
				t.Fatal("empty properties need a primary action")
			}
		})
	}
}

func TestM9bTaskTabsContextAndModalLifecycle(t *testing.T) {
	for _, locale := range settings.Locales() {
		t.Run(locale, func(t *testing.T) {
			a := newM3App(t, locale)
			unit, err := property.GetUnit(t.Context(), a.database, a.unitID)
			if err != nil {
				t.Fatal(err)
			}
			bookingID, err := booking.Create(t.Context(), a.database, booking.Details{UnitID: a.unitID, Kind: booking.KindStay, Channel: booking.ChannelDirect, CheckIn: "2026-10-29", CheckOut: "2026-10-30"})
			if err != nil {
				t.Fatal(err)
			}
			clean, err := task.CleaningForBooking(t.Context(), a.database, bookingID)
			if err != nil {
				t.Fatal(err)
			}
			body := a.request(t, "GET", "/tasks?status=open&kind=cleaning&property="+fmt.Sprint(unit.PropertyID), nil, 200, "Beach house / Whole home", catalogs[locale]["checkout_cleaning"], `data-task-id="`+fmt.Sprint(clean.ID)+`"`)
			if !strings.Contains(body, `due=overdue&amp;kind=cleaning&amp;property=`+fmt.Sprint(unit.PropertyID)+`&amp;status=open`) {
				t.Fatal("overdue tab lost current property or kind")
			}
			form := url.Values{"property_id": {fmt.Sprint(unit.PropertyID)}, "unit_id": {fmt.Sprint(unit.ID)}, "kind": {task.KindMaintenance}, "title": {""}, "due_on": {"2026-10-31"}, "assignee": {"Keep assignee"}}
			request := httptest.NewRequest(http.MethodPost, "/tasks/new", strings.NewReader(form.Encode()))
			request.AddCookie(a.cookie)
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("HX-Request", "true")
			request.Header.Set("HX-Target", "modal-content")
			response := httptest.NewRecorder()
			a.handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "<!doctype") || !strings.Contains(response.Body.String(), catalogs[locale]["invalid_task"]) || !strings.Contains(response.Body.String(), `value="Keep assignee"`) {
				t.Fatalf("task modal validation: %d %s", response.Code, response.Body)
			}
			form.Set("title", "Repair lock")
			a.request(t, http.MethodPost, "/tasks/new", form, http.StatusSeeOther)
			values, err := task.List(t.Context(), a.database, task.Filter{Kind: task.KindMaintenance})
			if err != nil || len(values) != 1 || values[0].Title != "Repair lock" {
				t.Fatalf("task form fallback did not persist: %+v %v", values, err)
			}
		})
	}
}

func TestM9bContactSearchAndAuthCard(t *testing.T) {
	for _, locale := range settings.Locales() {
		t.Run(locale, func(t *testing.T) {
			a := newM3App(t, locale)
			if _, err := person.Create(t.Context(), a.database, person.Details{Name: "Alice Wong", Email: "alice@example.com", Phone: "123"}); err != nil {
				t.Fatal(err)
			}
			if _, err := person.Create(t.Context(), a.database, person.Details{Name: "Bob Smith"}); err != nil {
				t.Fatal(err)
			}
			body := a.request(t, http.MethodGet, "/people?q=wong", nil, 200, `href="mailto:alice@example.com"`, `href="tel:123"`, "Alice Wong")
			if strings.Contains(body, "Bob Smith") {
				t.Fatal("contact search widened its results")
			}
			request := httptest.NewRequest(http.MethodGet, "/login", nil)
			response := httptest.NewRecorder()
			a.handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `class="auth-card panel panel-pad"`) || !strings.Contains(response.Body.String(), catalogs[locale]["login_intro"]) {
				t.Fatalf("auth card: %d %s", response.Code, response.Body)
			}
			request = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(url.Values{"email": {"owner@example.com"}, "password": {"incorrect"}}.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response = httptest.NewRecorder()
			a.handler.ServeHTTP(response, request)
			body = response.Body.String()
			if response.Code != http.StatusUnauthorized || strings.Count(body, `role="alert"`) != 1 || strings.Index(body, `role="alert"`) < strings.Index(body, `class="auth-card panel panel-pad"`) {
				t.Fatalf("auth error must appear once inside the card: %d %s", response.Code, body)
			}
		})
	}
}
