package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/settings"
	"github.com/gaw008/wingmanager/internal/task"
)

func TestM9TodayEmptyTabsAndLocalGreetings(t *testing.T) {
	for _, locale := range settings.Locales() {
		t.Run(locale, func(t *testing.T) {
			a := newM3App(t, locale)
			for _, v := range []struct {
				hour int
				key  string
			}{{7, "greeting_morning"}, {12, "greeting_afternoon"}, {18, "greeting_evening"}} {
				// UTC can have a different calendar day; greetings and date use settings.
				location, err := time.LoadLocation("America/Los_Angeles")
				if err != nil {
					t.Fatal(err)
				}
				now := func() time.Time { return time.Date(2026, 10, 31, v.hour, 0, 0, 0, location).UTC() }
				a.handler, err = newHandler(a.database, now, ical.NewService(a.database, nil, now), Config{DataDir: t.TempDir()})
				if err != nil {
					t.Fatal(err)
				}
				for _, tab := range []string{"out", "in", "stay", "soon", "invalid"} {
					selected := tab
					if tab == "invalid" {
						selected = "out"
					}
					a.request(t, "GET", "/?tab="+tab, nil, http.StatusOK,
						fmt.Sprintf(catalogs[locale][v.key], catalogs[locale]["landlord"]),
						`datetime="2026-10-31"`, `id="check-in-count">0`, `id="check-out-count">0`, `id="overdue-total">$0`, `id="ytd-net">$0`,
						`href="/?tab=`+selected+`" aria-current="page"`,
						catalogs[locale]["reservations_empty"], catalogs[locale]["no_attention"], `data-vacant-unit-id="`+fmt.Sprint(a.unitID)+`"`)
				}
			}
		})
	}
}

func TestM9TodayStayingCardsAndTaskCompletion(t *testing.T) {
	a := newM3App(t, settings.LocaleEnglish)
	id, err := booking.Create(t.Context(), a.database, booking.Details{UnitID: a.unitID, Kind: booking.KindStay, Channel: booking.ChannelAirbnb, CheckIn: "2026-10-29", CheckOut: "2026-11-03", NewGuest: &person.Details{Name: "Emily Chen"}})
	if err != nil {
		t.Fatal(err)
	}
	body := a.request(t, "GET", "/?tab=stay", nil, http.StatusOK, `data-booking-id="`+fmt.Sprint(id)+`"`, "Emily Chen", "5 nights", ">EC</span>", `class="chip airbnb"`)
	if strings.Contains(body, `data-vacant-unit-id="`+fmt.Sprint(a.unitID)+`"`) {
		t.Fatal("occupied unit shown vacant")
	}
	unit, err := property.GetUnit(t.Context(), a.database, a.unitID)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := task.Create(t.Context(), a.database, task.Details{PropertyID: unit.PropertyID, Kind: task.KindMaintenance, Title: "Repair lock", DueOn: "2026-10-31"})
	if err != nil {
		t.Fatal(err)
	}
	a.request(t, "GET", "/", nil, http.StatusOK, `method="post" action="/tasks/`+fmt.Sprint(taskID)+`/status"`, "Repair lock")
	a.request(t, "POST", fmt.Sprintf("/tasks/%d/status", taskID), url.Values{"status": {task.StatusDone}}, http.StatusSeeOther)
	body = a.request(t, "GET", "/", nil, http.StatusOK)
	if strings.Contains(body, "Repair lock") {
		t.Fatal("completed task still needs attention")
	}
	value, err := task.Get(t.Context(), a.database, taskID)
	if err != nil || value.Status != task.StatusDone {
		t.Fatalf("task=%+v err=%v", value, err)
	}
}
