package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	goical "github.com/emersion/go-ical"
	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/settings"
)

func TestM4FeedUIImportCalendarOwnerFieldsAndPublicExport(t *testing.T) {
	for _, locale := range settings.Locales() {
		t.Run(locale, func(t *testing.T) {
			a := newM3App(t, locale)
			fixture, err := os.ReadFile("../ical/testdata/airbnb.ics")
			if err != nil {
				t.Fatal(err)
			}
			feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(fixture) }))
			defer feedServer.Close()
			manual := a.form("2026-11-01", "2026-11-02")
			a.request(t, "POST", "/bookings/new", manual, 303)
			a.request(t, "POST", fmt.Sprintf("/units/%d/feeds", a.unitID), url.Values{"channel": {booking.ChannelAirbnb}, "url": {feedServer.URL}}, 303)
			feeds, err := ical.List(t.Context(), a.database)
			if err != nil || len(feeds) != 1 {
				t.Fatalf("feeds=%v err=%v", feeds, err)
			}
			feedPath := fmt.Sprintf("/feeds/%d", feeds[0].ID)
			a.request(t, "POST", fmt.Sprintf("/units/%d/feeds", a.unitID), url.Values{"channel": {booking.ChannelAirbnb}, "url": {feedServer.URL}}, 400, catalogs[locale]["duplicate_feed"])
			a.request(t, "POST", fmt.Sprintf("/units/%d/feeds", a.unitID), url.Values{"channel": {booking.ChannelAirbnb}, "url": {"file:///private/calendar.ics"}}, 400, catalogs[locale]["invalid_feed"])

			unit, err := property.GetUnit(t.Context(), a.database, a.unitID)
			if err != nil {
				t.Fatal(err)
			}
			propertyPath := fmt.Sprintf("/properties/%d", unit.PropertyID)
			a.request(t, "GET", propertyPath, nil, 200, catalogs[locale]["feeds"], catalogs[locale]["never_synced"], catalogs[locale]["remove_feed_hint"], `data-copy-target=`, `/ical/`+unit.IcalExportToken+`.ics`, feedPath+"/edit")
			a.request(t, "POST", feedPath+"/sync", nil, 303)
			a.request(t, "GET", propertyPath, nil, 200, `<time>`, catalogs[locale]["sync_now"])
			values, err := booking.List(t.Context(), a.database, booking.Filter{})
			if err != nil {
				t.Fatal(err)
			}
			var imported booking.Booking
			for _, b := range values {
				if b.Imported && b.Kind == booking.KindStay && b.CheckIn == "2026-11-01" {
					imported = b
				}
			}
			if imported.ID == 0 {
				t.Fatal("no imported stay")
			}
			bPath := fmt.Sprintf("/bookings/%d", imported.ID)
			a.request(t, "GET", "/calendar?month=2026-11", nil, 200, `channel-airbnb`, `conflict`, catalogs[locale]["conflict"], bPath, `/bookings/1`)
			page := a.request(t, "GET", bPath, nil, 200, `<fieldset disabled>`, feedServer.URL, catalogs[locale]["source"], catalogs[locale]["cleaning"], `data-cleaning-id=`)
			if strings.Contains(page, bPath+"/cancel") || strings.Contains(page, bPath+"/restore") {
				t.Fatal("imported status action visible")
			}
			a.request(t, "POST", bPath+"/edit", url.Values{"guest_name": {"Private guest"}, "notes": {"Local owner notes"}, "check_in": {"2026-12-01"}, "check_out": {"2026-12-10"}, "kind": {booking.KindBlock}, "channel": {booking.ChannelDirect}}, 303)
			updated, err := booking.Get(t.Context(), a.database, imported.ID)
			if err != nil || updated.GuestID == 0 || updated.Notes != "Local owner notes" || updated.CheckIn != imported.CheckIn || updated.CheckOut != imported.CheckOut || updated.Kind != imported.Kind || updated.Channel != imported.Channel {
				t.Fatalf("owner edit=%+v err=%v", updated, err)
			}
			a.request(t, "POST", bPath+"/cancel", nil, 400, catalogs[locale]["imported_booking_readonly"])
			// Public means no cookie, including when the client is an OTA rather than a browser.
			request := httptest.NewRequest("GET", "/ical/"+unit.IcalExportToken+".ics", nil)
			response := httptest.NewRecorder()
			a.handler.ServeHTTP(response, request)
			if response.Code != 200 || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/calendar") {
				t.Fatalf("export=%d %s", response.Code, response.Body)
			}
			if strings.Contains(response.Body.String(), "Private guest") || strings.Contains(response.Body.String(), "Local owner notes") {
				t.Fatal("private fields exported")
			}
			parsed, err := goical.NewDecoder(strings.NewReader(response.Body.String())).Decode()
			if err != nil || len(parsed.Events()) != 3 {
				t.Fatalf("export events=%v err=%v", parsed, err)
			}
			a.request(t, "POST", fmt.Sprintf("/units/%d/token", a.unitID), nil, 303)
			response = httptest.NewRecorder()
			a.handler.ServeHTTP(response, request)
			if response.Code != 404 {
				t.Fatalf("old export token status=%d", response.Code)
			}
			response = httptest.NewRecorder()
			a.handler.ServeHTTP(response, httptest.NewRequest("GET", "/ical/unknown.ics", nil))
			if response.Code != 404 {
				t.Fatalf("unknown token status=%d", response.Code)
			}
			a.request(t, "POST", feedPath+"/edit", url.Values{"channel": {booking.ChannelVRBO}, "url": {feedServer.URL + "/changed.ics"}}, 303)
			edited, err := ical.Get(t.Context(), a.database, feeds[0].ID)
			if err != nil || edited.Channel != booking.ChannelVRBO {
				t.Fatalf("edited=%+v err=%v", edited, err)
			}
			a.request(t, "POST", feedPath+"/remove", nil, 303)
			after, err := booking.Get(t.Context(), a.database, imported.ID)
			if err != nil || after.Imported || after.Notes != "Local owner notes" || after.GuestID != updated.GuestID {
				t.Fatalf("removed=%+v err=%v", after, err)
			}
		})
	}
}

func TestM4FeedActionsRequireSessionAndRejectCrossOrigin(t *testing.T) {
	a := newM3App(t, settings.LocaleEnglish)
	for _, path := range []string{fmt.Sprintf("/units/%d/feeds", a.unitID), "/feeds/1/edit", "/feeds/1/remove", "/feeds/1/sync"} {
		request := httptest.NewRequest("POST", path, nil)
		response := httptest.NewRecorder()
		a.handler.ServeHTTP(response, request)
		if response.Code != 303 || response.Header().Get("Location") != "/login" {
			t.Fatalf("unprotected %s: %d", path, response.Code)
		}
		request = httptest.NewRequest("POST", "http://wingmanager.example"+path, nil)
		request.AddCookie(a.cookie)
		request.Header.Set("Origin", "https://other.example")
		response = httptest.NewRecorder()
		a.handler.ServeHTTP(response, request)
		if response.Code != 403 {
			t.Fatalf("cross-origin %s: %d", path, response.Code)
		}
	}
}

func TestM4SyncWarningVisibleAndCanRecover(t *testing.T) {
	a := newM3App(t, settings.LocaleChinese)
	body, err := os.ReadFile("../ical/testdata/airbnb.ics")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body) }))
	defer server.Close()
	a.request(t, "POST", fmt.Sprintf("/units/%d/feeds", a.unitID), url.Values{"channel": {booking.ChannelAirbnb}, "url": {server.URL}}, 303)
	feeds, err := ical.List(t.Context(), a.database)
	if err != nil || len(feeds) != 1 {
		t.Fatalf("feeds=%v err=%v", feeds, err)
	}
	syncPath := fmt.Sprintf("/feeds/%d/sync", feeds[0].ID)
	a.request(t, "POST", syncPath, nil, 303)
	body, err = os.ReadFile("../ical/testdata/empty.ics")
	if err != nil {
		t.Fatal(err)
	}
	a.request(t, "POST", syncPath, nil, 303)
	unit, err := property.GetUnit(t.Context(), a.database, a.unitID)
	if err != nil {
		t.Fatal(err)
	}
	a.request(t, "GET", fmt.Sprintf("/properties/%d", unit.PropertyID), nil, 200, catalogs[settings.LocaleChinese]["sync_error"], catalogs[settings.LocaleChinese]["empty_feed_warning"])
	body, err = os.ReadFile("../ical/testdata/airbnb.ics")
	if err != nil {
		t.Fatal(err)
	}
	a.request(t, "POST", syncPath, nil, 303)
	f, err := ical.Get(t.Context(), a.database, feeds[0].ID)
	if err != nil || f.LastError != "" {
		t.Fatalf("recovery=%+v err=%v", f, err)
	}
}
