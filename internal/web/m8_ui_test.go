package web

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/settings"
)

func TestM8OptionalGuestLabelsAndCalendarTitles(t *testing.T) {
	for _, locale := range settings.Locales() {
		t.Run(locale, func(t *testing.T) {
			a := newM3App(t, locale)
			var ids []int64
			for _, dates := range [][2]string{{"2026-10-30", "2026-10-31"}, {"2026-10-31", "2026-11-02"}, {"2026-11-02", "2026-11-04"}} {
				id, err := booking.Create(t.Context(), a.database, booking.Details{
					UnitID: a.unitID, Kind: booking.KindStay, Channel: booking.ChannelDirect, CheckIn: dates[0], CheckOut: dates[1],
				})
				if err != nil {
					t.Fatal(err)
				}
				ids = append(ids, id)
			}
			unit, err := property.GetUnit(t.Context(), a.database, a.unitID)
			if err != nil {
				t.Fatal(err)
			}
			namedUnit, err := property.AddUnit(t.Context(), a.database, unit.PropertyID, "Guest unit")
			if err != nil {
				t.Fatal(err)
			}
			guest := `A very long guest name with <tags> & "quotes"`
			_, err = booking.Create(t.Context(), a.database, booking.Details{
				UnitID: namedUnit, Kind: booking.KindStay, Channel: booking.ChannelDirect,
				CheckIn: "2026-10-31", CheckOut: "2026-11-01", NewGuest: &person.Details{Name: guest},
			})
			if err != nil {
				t.Fatal(err)
			}
			a.request(t, "GET", "/?tab=in", nil, http.StatusOK, "Guest unit · "+html.EscapeString(guest))
			for _, test := range []struct {
				tab string
				id  int64
			}{{"out", ids[0]}, {"in", ids[1]}, {"soon", ids[2]}} {
				body := a.request(t, "GET", "/?tab="+test.tab, nil, http.StatusOK)
				want := fmt.Sprintf(`href="/bookings/%d" data-booking-id="%d" title="Beach house · Whole home"`, test.id, test.id)
				if !strings.Contains(body, want) || !strings.Contains(body, catalogs[locale]["unnamed_guest"]) {
					t.Errorf("guestless card: want %s in %s", want, body)
				}
			}

			dangling := regexp.MustCompile(`·\s*(</a>|</td>|</p>|&quot;|")`)
			for _, path := range []string{"/", "/bookings", "/calendar?month=2026-10", "/calendar?month=2026-11"} {
				body := a.request(t, "GET", path, nil, http.StatusOK)
				if dangling.MatchString(body) {
					t.Errorf("%s has a dangling separator: %s", path, dangling.FindString(body))
				}
				if !strings.HasPrefix(path, "/calendar") {
					continue
				}
				bars := regexp.MustCompile(`<a class="bar [^"]*" href="/bookings/\d+" title="([^"]*)"`).FindAllStringSubmatch(body, -1)
				if len(bars) == 0 {
					t.Fatal("calendar has no booking bars")
				}
				for _, bar := range bars {
					title := html.UnescapeString(bar[1])
					if !strings.HasPrefix(title, "#") || !strings.Contains(title, catalogs[locale]["stay"]) || !strings.Contains(title, " → ") {
						t.Errorf("calendar title %q must include booking identity, kind and dates", title)
					}
				}
				if !strings.Contains(body, html.EscapeString(guest)+" · 2026-10-31 → 2026-11-01") {
					t.Error("calendar title lost the full escaped guest name and dates")
				}
			}
		})
	}
}

func TestM8TodayFeedErrorDetailsPreserveRawError(t *testing.T) {
	for _, test := range []struct{ locale, summary string }{{settings.LocaleChinese, "技术详情"}, {settings.LocaleEnglish, "Technical details"}} {
		t.Run(test.locale, func(t *testing.T) {
			a := newM3App(t, test.locale)
			feedServer := httptest.NewServer(http.NotFoundHandler())
			t.Cleanup(feedServer.Close)
			id, err := ical.Add(t.Context(), a.database, a.unitID, booking.ChannelOther, feedServer.URL+"/calendar.ics")
			if err != nil {
				t.Fatal(err)
			}
			body := a.request(t, "GET", "/", nil, http.StatusOK, catalogs[test.locale]["sync_stale"])
			if strings.Contains(body, "<summary>"+test.summary+"</summary>") {
				t.Fatal("never-synced feed has no error to expand")
			}
			// A closed network endpoint produces a real Go client error without DNS or internet access.
			feedServer.Close()
			syncErr := ical.NewService(a.database, feedServer.Client(), time.Now).Sync(t.Context(), id)
			if syncErr == nil {
				t.Fatal("closed endpoint unexpectedly synced")
			}
			feed, err := ical.Get(t.Context(), a.database, id)
			if err != nil || feed.LastError != syncErr.Error() {
				t.Fatalf("raw error was not stored: feed=%+v err=%v", feed, err)
			}
			body = a.request(t, "GET", "/", nil, http.StatusOK, "<summary>"+test.summary+"</summary>")
			details := regexp.MustCompile(`<details\b[^>]*><summary>` + test.summary + `</summary>([\s\S]*?)</details>`).FindStringSubmatch(body)
			if details == nil || strings.Contains(strings.SplitN(details[0], ">", 2)[0], "open") {
				t.Fatal("error details must be collapsed by default")
			}
			raw := html.EscapeString(feed.LastError)
			if !strings.Contains(details[1], raw) || strings.Count(body, raw) != 1 {
				t.Fatal("raw error must appear once, inside details")
			}
			feed, err = ical.Get(t.Context(), a.database, id)
			if err != nil || feed.LastError != syncErr.Error() {
				t.Fatal("rendering changed the stored error")
			}
		})
	}
}
