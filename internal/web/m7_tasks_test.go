package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/settings"
	"github.com/gaw008/wingmanager/internal/task"
)

func TestM7TaskHTTPBilingualLifecycleFiltersAndCleaning(t *testing.T) {
	for _, locale := range settings.Locales() {
		t.Run(locale, func(t *testing.T) {
			a := newM3App(t, locale)
			unit, err := property.GetUnit(t.Context(), a.database, a.unitID)
			if err != nil {
				t.Fatal(err)
			}
			a.request(t, "GET", "/tasks", nil, 200, catalogs[locale]["new_task"], catalogs[locale]["no_tasks"])
			body := a.request(t, "GET", "/tasks/new", nil, 200, catalogs[locale]["maintenance"], catalogs[locale]["assignee"])
			if strings.Contains(body, `value="cleaning"`) {
				t.Fatal("create form offered cleaning")
			}
			form := url.Values{"property_id": {fmt.Sprint(unit.PropertyID)}, "unit_id": {fmt.Sprint(unit.ID)}, "kind": {task.KindMaintenance}, "title": {"Fix <sink>"}, "due_on": {"2026-10-30"}, "assignee": {"Pat"}, "notes": {"Leak"}}
			a.request(t, "POST", "/tasks/new", form, 303)
			values, err := task.List(t.Context(), a.database, task.Filter{})
			if err != nil || len(values) != 1 {
				t.Fatalf("tasks=%+v err=%v", values, err)
			}
			id := values[0].ID
			path := fmt.Sprintf("/tasks/%d", id)
			a.request(t, "GET", path, nil, 200, "Fix &lt;sink&gt;", catalogs[locale]["complete_task"], catalogs[locale]["cancel_task"])
			a.request(t, "GET", "/tasks?status=open&kind=maintenance&due=overdue&property="+fmt.Sprint(unit.PropertyID), nil, 200, fmt.Sprintf(`data-task-id="%d"`, id))
			a.request(t, "GET", "/tasks?due=week", nil, 200, fmt.Sprintf(`data-task-id="%d"`, id))
			for _, query := range []string{"status=done", "kind=other", "property=999"} {
				body := a.request(t, "GET", "/tasks?"+query, nil, 200)
				if strings.Contains(body, `data-task-id=`) {
					t.Fatalf("filter did not apply: %s", query)
				}
			}
			form.Set("title", "Replace sink")
			form.Set("due_on", "")
			form.Set("assignee", "Alex")
			form.Set("notes", "Ordered")
			a.request(t, "POST", path, form, 303)
			for _, status := range []string{task.StatusDone, task.StatusOpen, task.StatusCancelled} {
				a.request(t, "POST", path+"/status", url.Values{"status": {status}}, 303)
				value, err := task.Get(t.Context(), a.database, id)
				if err != nil || value.Status != status || (value.DoneAt != "") != (status == task.StatusDone) {
					t.Fatalf("status=%+v err=%v", value, err)
				}
			}
			form.Set("kind", task.KindCleaning)
			a.request(t, "POST", "/tasks/new", form, 400, catalogs[locale]["cleaning_task_restricted"])
			bookingID, err := booking.Create(t.Context(), a.database, booking.Details{UnitID: unit.ID, Kind: booking.KindStay, Channel: booking.ChannelDirect, CheckIn: "2026-11-01", CheckOut: "2026-11-03"})
			if err != nil {
				t.Fatal(err)
			}
			clean, err := task.CleaningForBooking(t.Context(), a.database, bookingID)
			if err != nil {
				t.Fatal(err)
			}
			cleanPath := fmt.Sprintf("/tasks/%d", clean.ID)
			body = a.request(t, "GET", cleanPath, nil, 200, catalogs[locale]["cleaning_task_hint"], "readonly")
			if strings.Contains(body, `value="cancelled"`) {
				t.Fatal("cleaning form offered cancellation")
			}
			a.request(t, "POST", cleanPath+"/status", url.Values{"status": {task.StatusCancelled}}, 400, catalogs[locale]["cleaning_task_restricted"])
			cleanForm := url.Values{"title": {"Tampered"}, "due_on": {clean.DueOn}, "assignee": {"Cleaner"}, "notes": {"Call before arrival"}}
			a.request(t, "POST", cleanPath, cleanForm, 400, catalogs[locale]["cleaning_task_restricted"])
			cleanForm.Set("title", clean.Title)
			a.request(t, "POST", cleanPath, cleanForm, 303)
			a.request(t, "POST", cleanPath+"/status", url.Values{"status": {task.StatusDone}}, 303)
			a.request(t, "POST", cleanPath+"/status", url.Values{"status": {task.StatusOpen}}, 303)
			for _, path := range []string{"/tasks?status=wrong", "/tasks?kind=wrong", "/tasks?due=wrong", "/tasks?property=-1"} {
				a.request(t, "GET", path, nil, 400, catalogs[locale]["invalid_task"])
			}
			a.request(t, "GET", "/tasks/9999", nil, 404, catalogs[locale]["not_found"])
			request := httptest.NewRequest(http.MethodGet, "/tasks", nil)
			response := httptest.NewRecorder()
			a.handler.ServeHTTP(response, request)
			if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/login" {
				t.Fatalf("anonymous task access=%+v", response)
			}
		})
	}
}
