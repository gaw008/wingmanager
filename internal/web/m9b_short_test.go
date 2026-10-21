package web

import (
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/settings"
	"github.com/gaw008/wingmanager/internal/task"
)

func TestM9bBookingTabsAndUnitFilter(t *testing.T) {
	a := newM3App(t, settings.LocaleEnglish)
	create := func(unit int64, from, to string) int64 {
		t.Helper()
		id, err := booking.Create(t.Context(), a.database, booking.Details{UnitID: unit, Kind: booking.KindStay, Channel: booking.ChannelDirect, CheckIn: from, CheckOut: to})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	unit, err := property.GetUnit(t.Context(), a.database, a.unitID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := property.AddUnit(t.Context(), a.database, unit.PropertyID, "Second unit")
	if err != nil {
		t.Fatal(err)
	}
	ended := create(a.unitID, "2026-10-20", "2026-10-21")
	inhouse := create(a.unitID, "2026-10-29", "2026-10-31")
	upcoming := create(a.unitID, "2026-11-02", "2026-11-05")
	cancelled := create(a.unitID, "2026-11-06", "2026-11-07")
	if err := booking.SetCancelled(t.Context(), a.database, cancelled, true); err != nil {
		t.Fatal(err)
	}
	other := create(second, "2026-11-02", "2026-11-05")
	all := []int64{ended, inhouse, upcoming, cancelled, other}
	for _, tc := range []struct {
		path string
		want []int64
	}{
		{"/bookings", []int64{upcoming, other}},
		{"/bookings?tab=upcoming", []int64{upcoming, other}},
		{"/bookings?tab=inhouse", []int64{inhouse}},
		{"/bookings?tab=ended", []int64{ended}},
		{"/bookings?tab=cancelled", []int64{cancelled}},
		{fmt.Sprintf("/bookings?tab=upcoming&unit=%d", second), []int64{other}},
	} {
		body := a.request(t, "GET", tc.path, nil, 200)
		for _, id := range all {
			expected := false
			for _, want := range tc.want {
				if want == id {
					expected = true
				}
			}
			if strings.Contains(body, fmt.Sprintf(`data-booking-id="%d"`, id)) != expected {
				t.Fatalf("%s booking %d expected=%t", tc.path, id, expected)
			}
		}
	}
	a.request(t, "GET", "/bookings?tab=invalid", nil, 400, catalogs[settings.LocaleEnglish]["invalid_booking"])
}

func TestM9bBookingReadOnlyDrawerAndEditableForm(t *testing.T) {
	for _, locale := range settings.Locales() {
		t.Run(locale, func(t *testing.T) {
			a := newM3App(t, locale)
			id, err := booking.Create(t.Context(), a.database, booking.Details{UnitID: a.unitID, Kind: booking.KindStay, Channel: booking.ChannelDirect, CheckIn: "2026-10-29", CheckOut: "2026-10-31", NewGuest: &person.Details{Name: "Emily Chen"}, Notes: "Guest note"})
			if err != nil {
				t.Fatal(err)
			}
			path := fmt.Sprintf("/bookings/%d", id)
			for _, enhanced := range []bool{false, true} {
				request := httptest.NewRequest("GET", path, nil)
				request.AddCookie(a.cookie)
				if enhanced {
					request.Header.Set("HX-Request", "true")
					request.Header.Set("HX-Target", "drawer-body")
				}
				response := httptest.NewRecorder()
				a.handler.ServeHTTP(response, request)
				body := response.Body.String()
				if response.Code != 200 || strings.Contains(body, `name="unit_id"`) || !strings.Contains(body, "Emily Chen") || !strings.Contains(body, `>EC</span>`) || !strings.Contains(body, catalogs[locale]["booking_manual_notice"]) || !strings.Contains(body, catalogs[locale]["booking_check_out_today"]) || !strings.Contains(body, `hx-get="`+path+`/edit"`) || !strings.Contains(body, `data-cleaning-id=`) {
					t.Fatalf("detail enhanced=%t: %d %s", enhanced, response.Code, body)
				}
				if strings.Contains(body, "<!doctype html>") == enhanced {
					t.Fatal("incorrect full/partial layout")
				}
			}
			a.request(t, "GET", path+"/edit", nil, 200, `name="unit_id"`, `name="check_in"`, `value="2026-10-29"`, `name="guest_id"`, "Guest note")
			form := a.form("2026-10-29", "2026-10-31")
			form.Set("guest_name", "Updated guest")
			a.request(t, "POST", path+"/edit", form, 303)
			a.request(t, "GET", path, nil, 200, "Updated guest")
		})
	}
}

func TestM9bTodayCleaningShowsUnitReasonDateAndAssignee(t *testing.T) {
	a := newM3App(t, settings.LocaleEnglish)
	id, err := booking.Create(t.Context(), a.database, booking.Details{UnitID: a.unitID, Kind: booking.KindStay, Channel: booking.ChannelDirect, CheckIn: "2026-10-29", CheckOut: "2026-10-31"})
	if err != nil {
		t.Fatal(err)
	}
	cleaning, err := task.CleaningForBooking(t.Context(), a.database, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := task.Update(t.Context(), a.database, cleaning.ID, task.Edit{DueOn: cleaning.DueOn, Assignee: "Cleaner Ava"}); err != nil {
		t.Fatal(err)
	}
	a.request(t, "GET", "/", nil, 200, "Beach house · Whole home Checkout cleaning", "2026-10-31", "Cleaner Ava")
	a.request(t, "POST", fmt.Sprintf("/tasks/%d/status", cleaning.ID), url.Values{"status": {task.StatusDone}}, 303)
	body := a.request(t, "GET", "/", nil, 200)
	if strings.Contains(body, fmt.Sprintf(`data-task-id="%d"`, cleaning.ID)) {
		t.Fatal("completed cleaning still needs attention")
	}
}
