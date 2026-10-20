package web_test

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gaw008/wingmanager/internal/db"
	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/settings"
	"github.com/gaw008/wingmanager/internal/web"
)

type app struct {
	database *sql.DB
	handler  http.Handler
}

func newApp(t *testing.T) app {
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
	handler, err := web.New(database, ical.NewService(database, nil, time.Now), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return app{database, handler}
}

func (a app) request(method, path string, form url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	a.handler.ServeHTTP(response, request)
	return response
}

func account() url.Values {
	return url.Values{"email": {"owner@example.com"}, "password": {"correct password"}}
}

func (a app) setup(t *testing.T) {
	t.Helper()
	response := a.request("POST", "/setup", account(), nil)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/login" {
		t.Fatalf("setup: %d %s", response.Code, response.Body)
	}
}

func (a app) login(t *testing.T) *http.Cookie {
	t.Helper()
	response := a.request("POST", "/login", account(), nil)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/" {
		t.Fatalf("login: %d %s", response.Code, response.Body)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies=%v", cookies)
	}
	return cookies[0]
}

func TestSetupClosed(t *testing.T) {
	a := newApp(t)
	if response := a.request("GET", "/setup", nil, nil); response.Code != http.StatusOK {
		t.Fatal(response.Code)
	}
	a.setup(t)
	for _, method := range []string{"GET", "POST"} {
		response := a.request(method, "/setup", account(), nil)
		wantStatus := http.StatusSeeOther
		if method == "POST" {
			wantStatus = http.StatusConflict
		}
		if response.Code != wantStatus || strings.Contains(response.Body.String(), `action="/setup"`) {
			t.Fatalf("setup still accessible: %d %s", response.Code, response.Body)
		}
	}
}

func TestSetupPasswordPolicyMessages(t *testing.T) {
	for _, test := range []struct{ locale, message string }{
		{settings.LocaleChinese, "请输入有效邮箱和至少 8 个字符、最多 72 字节的密码。"},
		{settings.LocaleEnglish, "Enter a valid email and a password of at least 8 characters and at most 72 bytes."},
	} {
		t.Run(test.locale, func(t *testing.T) {
			a := newApp(t)
			value, err := settings.Get(t.Context(), a.database)
			if err != nil {
				t.Fatal(err)
			}
			value.Locale = test.locale
			if err := settings.Save(t.Context(), a.database, value); err != nil {
				t.Fatal(err)
			}
			form := account()
			form.Set("password", "1234567")
			response := a.request("POST", "/setup", form, nil)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), test.message) {
				t.Fatalf("password validation message: %d %s", response.Code, response.Body)
			}
			form.Set("password", "12345678")
			response = a.request("POST", "/setup", form, nil)
			if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/login" {
				t.Fatalf("valid password rejected: %d %s", response.Code, response.Body)
			}
		})
	}
}

func TestLoginExpiryAndLogoutHTTP(t *testing.T) {
	a := newApp(t)
	a.setup(t)
	for _, credentials := range []url.Values{
		{"email": {"owner@example.com"}, "password": {"wrong"}},
		{"email": {"unknown@example.com"}, "password": {"correct password"}},
	} {
		response := a.request("POST", "/login", credentials, nil)
		if response.Code != http.StatusUnauthorized || len(response.Result().Cookies()) != 0 {
			t.Fatalf("wrong credentials accepted: %d", response.Code)
		}
	}
	cookie := a.login(t)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || !cookie.Expires.After(time.Now()) {
		t.Fatalf("invalid cookie: %v", cookie)
	}
	if response := a.request("GET", "/", nil, cookie); response.Code != http.StatusOK {
		t.Fatal(response.Code)
	}
	if _, err := a.database.Exec(`UPDATE sessions SET expires_at = ?`, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	response := a.request("GET", "/", nil, cookie)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/login" {
		t.Fatalf("expired session accepted: %d", response.Code)
	}
	cookie = a.login(t)
	response = a.request("POST", "/logout", nil, cookie)
	if response.Code != http.StatusSeeOther || response.Result().Cookies()[0].MaxAge != -1 {
		t.Fatalf("logout: %d", response.Code)
	}
	if response := a.request("GET", "/", nil, cookie); response.Code != http.StatusSeeOther {
		t.Fatal("logged-out session accepted")
	}
}

func TestPagesRequireSession(t *testing.T) {
	a := newApp(t)
	if response := a.request("GET", "/", nil, nil); response.Header().Get("Location") != "/setup" {
		t.Fatal("fresh instance does not show setup")
	}
	a.setup(t)
	for _, path := range []string{"/", "/settings", "/missing"} {
		for _, cookie := range []*http.Cookie{nil, {Name: "wingmanager_session", Value: "forged"}} {
			response := a.request("GET", path, nil, cookie)
			if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/login" {
				t.Fatalf("unprotected page %s: %d", path, response.Code)
			}
		}
	}
	for _, path := range []string{"/static/htmx-2.0.4.min.js", "/static/pico-2.0.6.min.css", "/login"} {
		response := a.request("GET", path, nil, nil)
		if response.Code != http.StatusOK || response.Body.Len() == 0 {
			t.Fatalf("public page %s: %d", path, response.Code)
		}
	}
}

func TestCrossOriginPostsRejected(t *testing.T) {
	a := newApp(t)
	for _, header := range []http.Header{
		{"Origin": {"https://other.example"}},
		{"Sec-Fetch-Site": {"cross-site"}},
	} {
		for _, path := range []string{"/setup", "/login", "/settings", "/logout", "/properties/new", "/properties/1/edit", "/properties/1/archive", "/properties/1/restore", "/properties/1/units", "/units/1/rename", "/units/1/archive", "/units/1/restore", "/units/1/token", "/people/new", "/people/1/edit"} {
			request := httptest.NewRequest("POST", "http://wingmanager.example"+path, strings.NewReader(account().Encode()))
			request.Header = header.Clone()
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response := httptest.NewRecorder()
			a.handler.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("cross-origin %s: %d", path, response.Code)
			}
		}
	}
	var users int
	if err := a.database.QueryRow(`SELECT count(*) FROM users`).Scan(&users); err != nil {
		t.Fatal(err)
	}
	if users != 0 {
		t.Fatal("cross-origin setup changed the database")
	}
	// Same-origin browser forms are allowed without a separate CSRF token.
	request := httptest.NewRequest("POST", "http://wingmanager.example/setup", strings.NewReader(account().Encode()))
	request.Header.Set("Origin", "http://wingmanager.example")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	a.handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("same-origin setup: %d", response.Code)
	}
}

func TestSettingsAndEnglishUI(t *testing.T) {
	a := newApp(t)
	a.setup(t)
	cookie := a.login(t)
	response := a.request("GET", "/", nil, cookie)
	if !strings.Contains(response.Body.String(), "<h1>今天</h1>") {
		t.Fatal(response.Body)
	}
	form := url.Values{"landlord_name": {"Owner"}, "timezone": {"Mars/Olympus"}, "currency": {"EUR"}, "locale": {"en"}}
	response = a.request("POST", "/settings", form, cookie)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid timezone accepted: %d", response.Code)
	}
	var name, zone, currency, locale string
	if err := a.database.QueryRow(`SELECT landlord_name, timezone, currency, locale FROM settings`).Scan(&name, &zone, &currency, &locale); err != nil {
		t.Fatal(err)
	}
	if name != "" || zone != "America/Los_Angeles" || currency != "USD" || locale != "zh-CN" {
		t.Fatal("invalid settings submission partially saved")
	}
	form.Set("timezone", "Europe/London")
	response = a.request("POST", "/settings", form, cookie)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("save: %d %s", response.Code, response.Body)
	}
	response = a.request("GET", "/", nil, cookie)
	for _, want := range []string{`lang="en"`, "<h1>Today</h1>", ">Settings</a>", ">Log out</button>"} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("missing %q: %s", want, response.Body)
		}
	}
	response = a.request("GET", "/settings", nil, cookie)
	for _, want := range []string{`value="Owner"`, `value="Europe/London"`, `value="EUR"`, "Landlord name"} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("missing saved setting %q: %s", want, response.Body)
		}
	}
	// Constructing a new handler preserves the database-backed language.
	handler, err := web.New(a.database, ical.NewService(a.database, nil, time.Now), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a.handler = handler
	if response := a.request("GET", "/login", nil, nil); !strings.Contains(response.Body.String(), "<h1>Log in</h1>") {
		t.Fatal(response.Body)
	}
}

func TestHTTPBrowserFlow(t *testing.T) {
	a := newApp(t)
	server := httptest.NewServer(a.handler)
	defer server.Close()
	response, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.Request.URL.Path != "/setup" || !strings.Contains(string(body), `action="/setup"`) {
		t.Fatal("HTTP flow did not reach first-run setup")
	}
}
