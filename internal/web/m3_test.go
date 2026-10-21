package web

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gaw008/wingmanager/internal/auth"
	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/db"
	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/settings"
)

type m3App struct {
	database *sql.DB
	handler  http.Handler
	cookie   *http.Cookie
	unitID   int64
}

func newM3App(t *testing.T, locale string) m3App {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	service := auth.NewService(database)
	if err := service.Setup(t.Context(), "owner@example.com", "correct password"); err != nil {
		t.Fatal(err)
	}
	token, _, err := service.Login(t.Context(), "owner@example.com", "correct password")
	if err != nil {
		t.Fatal(err)
	}
	value, err := settings.Get(t.Context(), database)
	if err != nil {
		t.Fatal(err)
	}
	value.Locale = locale
	value.Timezone = "America/Los_Angeles"
	if err := settings.Save(t.Context(), database, value); err != nil {
		t.Fatal(err)
	}
	// UTC is already Nov 1, while the configured local date is still Oct 31.
	now := func() time.Time { return time.Date(2026, 11, 1, 1, 30, 0, 0, time.UTC) }
	handler, err := newHandler(database, now, ical.NewService(database, nil, now), Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	id, err := property.Create(t.Context(), database, property.Details{Name: "Beach house"}, "Whole home")
	if err != nil {
		t.Fatal(err)
	}
	units, err := property.ListUnits(t.Context(), database, id, false)
	if err != nil || len(units) != 1 {
		t.Fatalf("units=%v err=%v", units, err)
	}
	return m3App{database, handler, &http.Cookie{Name: sessionCookie, Value: token}, units[0].ID}
}

func (a m3App) request(t *testing.T, method, path string, form url.Values, status int, contains ...string) string {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(a.cookie)
	response := httptest.NewRecorder()
	a.handler.ServeHTTP(response, request)
	if response.Code != status {
		t.Fatalf("%s %s status=%d want=%d body=%s", method, path, response.Code, status, response.Body)
	}
	for _, text := range contains {
		if !strings.Contains(response.Body.String(), text) {
			t.Fatalf("%s %s missing %q in %s", method, path, text, response.Body)
		}
	}
	return response.Body.String()
}

func (a m3App) form(from, to string) url.Values {
	return url.Values{"unit_id": {fmt.Sprint(a.unitID)}, "kind": {booking.KindStay}, "channel": {booking.ChannelAirbnb}, "check_in": {from}, "check_out": {to}, "notes": {"Manual OTA booking"}}
}

func TestM3HTTPCellTurnoverConflictAndLifecycle(t *testing.T) {
	for _, locale := range settings.Locales() {
		t.Run(locale, func(t *testing.T) {
			a := newM3App(t, locale)
			cell := fmt.Sprintf("/bookings/new?unit=%d&date=2026-10-10", a.unitID)
			a.request(t, "GET", cell, nil, 200, `value="2026-10-10"`, `value="2026-10-11"`, fmt.Sprintf(`value="%d" selected`, a.unitID))
			form := a.form("2026-10-10", "2026-10-12")
			form.Set("guest_name", "Guest <script>alert(1)</script>")
			form.Set("guest_email", "guest@example.com")
			a.request(t, "POST", "/bookings/new", form, 303)
			values, err := booking.List(t.Context(), a.database, booking.Filter{})
			if err != nil || len(values) != 1 {
				t.Fatalf("values=%v err=%v", values, err)
			}
			first := values[0]
			if first.Channel != booking.ChannelAirbnb || first.GuestID == 0 {
				t.Fatalf("booking=%+v", first)
			}
			firstPath := fmt.Sprintf("/bookings/%d", first.ID)
			a.request(t, "GET", firstPath+"/edit", nil, 200, "Guest &lt;script&gt;alert(1)&lt;/script&gt;", `value="airbnb" selected`, `value="2026-10-12"`)
			// Same-day turnover succeeds through the exact form used by the UI.
			secondForm := a.form("2026-10-12", "2026-10-14")
			a.request(t, "POST", "/bookings/new", secondForm, 303)
			a.request(t, "POST", "/bookings/new", a.form("2026-10-11", "2026-10-13"), 409, catalogs[locale]["booking_conflict"], firstPath, map[string]string{"zh-CN": "10月10日", "en": "Oct 10"}[locale], map[string]string{"zh-CN": "10月12日", "en": "Oct 12"}[locale])
			a.request(t, "POST", firstPath+"/edit", form, 303)
			a.request(t, "POST", firstPath+"/cancel", nil, 303)
			body := a.request(t, "GET", "/calendar?month=2026-10", nil, 200)
			if strings.Contains(body, `class="booking-bar" href="`+firstPath+`"`) {
				t.Fatal("cancelled booking visible in calendar")
			}
			replacement := a.form("2026-10-10", "2026-10-12")
			replacement.Set("kind", booking.KindBlock)
			a.request(t, "POST", "/bookings/new", replacement, 303)
			a.request(t, "POST", firstPath+"/restore", nil, 409, catalogs[locale]["booking_conflict"])
			a.request(t, "GET", "/bookings?status=cancelled", nil, 200, `data-booking-id="`+fmt.Sprint(first.ID)+`"`)
			a.request(t, "GET", "/bookings?tab=cancelled&unit="+fmt.Sprint(a.unitID), nil, 200, "Guest &lt;script&gt;alert(1)&lt;/script&gt;")
			a.request(t, "POST", "/bookings/new", a.form("2026-10-20", "2026-10-20"), 400, catalogs[locale]["invalid_booking"])
			a.request(t, "GET", "/bookings/9999", nil, 404, catalogs[locale]["not_found"])
		})
	}
}

func TestM3CalendarMonthTodayBarsAndActiveUnits(t *testing.T) {
	a := newM3App(t, settings.LocaleEnglish)
	create := func(from, to, kind string) int64 {
		t.Helper()
		id, err := booking.Create(t.Context(), a.database, booking.Details{UnitID: a.unitID, Kind: kind, Channel: booking.ChannelDirect, CheckIn: from, CheckOut: to})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	stay := create("2026-09-29", "2026-10-03", booking.KindStay)
	block := create("2026-10-30", "2026-11-04", booking.KindBlock)
	create("2026-11-20", "2026-11-21", booking.KindStay)
	u, err := property.GetUnit(t.Context(), a.database, a.unitID)
	if err != nil {
		t.Fatal(err)
	}
	archived, err := property.AddUnit(t.Context(), a.database, u.PropertyID, "Archived unit")
	if err != nil {
		t.Fatal(err)
	}
	if err := property.SetUnitArchived(t.Context(), a.database, archived, true); err != nil {
		t.Fatal(err)
	}
	p, err := property.Create(t.Context(), a.database, property.Details{Name: "Archived property"}, "Hidden home")
	if err != nil {
		t.Fatal(err)
	}
	if err := property.SetArchived(t.Context(), a.database, p, true); err != nil {
		t.Fatal(err)
	}
	body := a.request(t, "GET", "/calendar", nil, 200, `data-month="2026-10"`, `data-date="2026-10-31" class="day-head weekend today" id="today-col" aria-current="date"`, `/calendar?month=2026-09`, `/calendar?month=2026-11`, "Beach house", "Whole home", fmt.Sprintf(`/bookings/%d`, stay), fmt.Sprintf(`/bookings/%d`, block), `class="bar stay past cut-left channel-direct"`, `grid-column:2/7;grid-row:1`, `class="bar block cut-right channel-direct"`, `grid-column:61/64;grid-row:1`, fmt.Sprintf(`/bookings/new?unit=%d&amp;date=2026-10-03`, a.unitID))
	for _, hidden := range []string{"Archived unit", "Archived property", "Hidden home", `data-date="2026-11-01"`} {
		if strings.Contains(body, hidden) {
			t.Fatalf("unexpected %q", hidden)
		}
	}
	november := a.request(t, "GET", "/calendar?month=2026-11", nil, 200, `data-month="2026-11"`, `data-date="2026-11-30"`, `class="bar block cut-left channel-direct"`, `grid-column:2/9;grid-row:1`)
	if strings.Contains(november, `aria-current="date"`) {
		t.Fatal("today marker in wrong month")
	}
	a.request(t, "GET", "/calendar?month=2026-02", nil, 200, `data-date="2026-02-28"`)
	a.request(t, "GET", "/calendar?month=2026-2", nil, 400, "Enter a valid month")
	a.request(t, "GET", "/bookings/new?date=2026-02-30", nil, 400, "Choose a valid unit")
	// Cancel then restore has the same form action and reappears as a bar.
	path := fmt.Sprintf("/bookings/%d", stay)
	a.request(t, "POST", path+"/cancel", nil, 303)
	a.request(t, "POST", path+"/restore", nil, 303)
	a.request(t, "GET", "/calendar", nil, 200, fmt.Sprintf(`class="bar stay past cut-left channel-direct" href="/bookings/%d"`, stay))
}

func TestM3BookingFiltersAndExistingGuest(t *testing.T) {
	a := newM3App(t, settings.LocaleEnglish)
	guest, err := person.Create(t.Context(), a.database, person.Details{Name: "Existing guest"})
	if err != nil {
		t.Fatal(err)
	}
	form := a.form("2026-10-30", "2026-10-31")
	form.Set("guest_id", fmt.Sprint(guest))
	a.request(t, "POST", "/bookings/new", form, 303)
	a.request(t, "POST", "/bookings/new", a.form("2026-10-31", "2026-11-02"), 303)
	body := a.request(t, "GET", "/bookings?period=past", nil, 200, `data-booking-id="1"`, "Existing guest")
	if strings.Contains(body, `data-booking-id="2"`) {
		t.Fatal("future booking in past filter")
	}
	body = a.request(t, "GET", "/bookings?period=upcoming", nil, 200, `data-booking-id="2"`)
	if strings.Contains(body, `data-booking-id="1"`) {
		t.Fatal("past booking in upcoming filter")
	}
	a.request(t, "GET", "/bookings?unit=999", nil, 200, "No matching bookings")
	a.request(t, "GET", "/bookings?period=invalid", nil, 400, "Choose a valid unit")
	a.request(t, "GET", "/bookings?status=invalid", nil, 400, "Choose a valid unit")
}

func TestM3RoutesRequireSessionAndRejectCrossOrigin(t *testing.T) {
	a := newM3App(t, settings.LocaleEnglish)
	routes := []struct{ method, path string }{{"GET", "/calendar"}, {"GET", "/bookings"}, {"GET", "/bookings/new"}, {"POST", "/bookings/new"}, {"GET", "/bookings/1"}, {"GET", "/bookings/1/edit"}, {"POST", "/bookings/1/edit"}, {"POST", "/bookings/1/cancel"}, {"POST", "/bookings/1/restore"}}
	for _, route := range routes {
		request := httptest.NewRequest(route.method, route.path, nil)
		response := httptest.NewRecorder()
		a.handler.ServeHTTP(response, request)
		if response.Code != 303 || response.Header().Get("Location") != "/login" {
			t.Fatalf("unprotected %+v", route)
		}
		if route.method == "POST" {
			request = httptest.NewRequest(route.method, "http://wingmanager.example"+route.path, nil)
			request.AddCookie(a.cookie)
			request.Header.Set("Origin", "https://other.example")
			response = httptest.NewRecorder()
			a.handler.ServeHTTP(response, request)
			if response.Code != 403 {
				t.Fatalf("cross-origin allowed %+v", route)
			}
		}
	}
	values, err := booking.List(t.Context(), a.database, booking.Filter{})
	if err != nil || len(values) != 0 {
		t.Fatalf("unauthorized write: %v %v", values, err)
	}
}
