package web

import (
	"fmt"
	"io/fs"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/gaw008/wingmanager/internal/booking"
	"github.com/gaw008/wingmanager/internal/settings"
)

func TestM9RedirectsAndAuthentication(t *testing.T) {
	a := newM3App(t, settings.LocaleEnglish)
	for _, test := range []struct{ path, target, key, value string }{
		{"/overdue?status=active&property=7&filter=all", "/leases", "filter", "overdue"},
		{"/reports?year=2025&property=7&tab=all", "/money", "tab", "reports"},
	} {
		request := httptest.NewRequest("GET", test.path, nil)
		response := httptest.NewRecorder()
		a.handler.ServeHTTP(response, request)
		if response.Code != 303 || response.Header().Get("Location") != "/login" {
			t.Fatalf("unprotected %s", test.path)
		}
		request.AddCookie(a.cookie)
		response = httptest.NewRecorder()
		a.handler.ServeHTTP(response, request)
		target, err := url.Parse(response.Header().Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		if response.Code != 303 || target.Path != test.target || target.Query().Get(test.key) != test.value || target.Query().Get("property") != "7" {
			t.Fatalf("redirect %s: %d %s", test.path, response.Code, target)
		}
		source, _ := url.Parse(test.path)
		for key, values := range source.Query() {
			if key != test.key && target.Query().Get(key) != values[0] {
				t.Fatalf("lost %s", key)
			}
		}
	}
	a.request(t, "GET", "/logout", nil, 405)
	a.request(t, "GET", "/", nil, 200, `method="post" action="/logout"`)
	a.request(t, "POST", "/logout", nil, 303)
	a.request(t, "GET", "/", nil, 303)
}

func TestM9LayoutNavigationAndLegacyPages(t *testing.T) {
	for _, locale := range settings.Locales() {
		t.Run(locale, func(t *testing.T) {
			a := newM3App(t, locale)
			for _, test := range []struct{ path, current string }{
				{"/", "/"}, {"/calendar", "/calendar"}, {"/bookings", "/bookings"}, {"/bookings/new", "/bookings"},
				{"/leases", "/leases"}, {"/leases?filter=overdue", "/leases"}, {"/leases/new", "/leases"},
				{"/money", "/money"}, {"/money?tab=reports", "/money"}, {"/money/expense", "/money"}, {"/money/income", "/money"},
				{"/properties", ""}, {"/people", ""}, {"/tasks", ""}, {"/settings", ""},
			} {
				body := a.request(t, "GET", test.path, nil, 200, `/static/app.css`, `/static/app.js`, `class="topbar"`, `class="tabbar"`, `class="account-menu"`)
				want := 0
				if test.current != "" {
					want = 2
				}
				navigation := strings.Join(regexp.MustCompile(`<nav class="(?:nav|tabbar)"[^>]*>([\s\S]*?)</nav>`).FindAllString(body, -1), "")
				if strings.Count(navigation, `aria-current="page"`) != want {
					t.Fatalf("%s current state", test.path)
				}
				if want > 0 && strings.Count(navigation, fmt.Sprintf(`href="%s" aria-current="page"`, test.current)) != 2 {
					t.Fatalf("%s wrong navigation", test.path)
				}
				for _, path := range []string{"/properties", "/people", "/tasks", "/settings"} {
					if !strings.Contains(body, `href="`+path+`"`) {
						t.Fatalf("missing menu link %s", path)
					}
				}
			}
		})
	}
}

func TestM9BookingDrawerAndFullPageFallback(t *testing.T) {
	a := newM3App(t, settings.LocaleEnglish)
	id, err := booking.Create(t.Context(), a.database, booking.Details{UnitID: a.unitID, Kind: booking.KindStay, Channel: booking.ChannelDirect, CheckIn: "2026-10-31", CheckOut: "2026-11-02"})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{fmt.Sprintf("/bookings/%d/edit", id), fmt.Sprintf("/bookings/new?unit=%d&date=2026-11-02", a.unitID)} {
		full := a.request(t, "GET", path, nil, 200, `<!doctype html>`, `<form method="post"`)
		if !strings.Contains(full, `/static/app.css`) {
			t.Fatal("full page missing layout")
		}
		request := httptest.NewRequest("GET", path, nil)
		request.AddCookie(a.cookie)
		request.Header.Set("HX-Request", "true")
		request.Header.Set("HX-Target", "drawer-body")
		response := httptest.NewRecorder()
		a.handler.ServeHTTP(response, request)
		body := response.Body.String()
		if response.Code != 200 || strings.Contains(body, "<!doctype") || !strings.Contains(body, `<form method="post"`) || !strings.Contains(body, `value="2026-11-02"`) {
			t.Fatalf("drawer %s: %d %s", path, response.Code, body)
		}
		if strings.Contains(path, "new?") && !strings.Contains(body, fmt.Sprintf(`value="%d" selected`, a.unitID)) {
			t.Fatal("drawer lost unit")
		}
		request = httptest.NewRequest("GET", path, nil)
		request.Header.Set("HX-Request", "true")
		request.Header.Set("HX-Target", "drawer-body")
		response = httptest.NewRecorder()
		a.handler.ServeHTTP(response, request)
		if response.Code != 200 || response.Header().Get("HX-Redirect") != "/login" {
			t.Fatal("unauthenticated drawer")
		}
	}
}

func TestM9AssetsUseOwnDesignSystem(t *testing.T) {
	err := fs.WalkDir(assets, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		content, err := assets.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(strings.ToLower(path), "pico") || strings.Contains(strings.ToLower(string(content)), "pico") {
			t.Errorf("legacy stylesheet reference in %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
