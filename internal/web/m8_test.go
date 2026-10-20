package web_test

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/web"
)

func TestSessionCookieHTTPSProxy(t *testing.T) {
	for _, test := range []struct {
		name    string
		proxy   bool
		tls     bool
		forward bool
		secure  bool
	}{
		{name: "plain HTTP"},
		{name: "TLS", tls: true, secure: true},
		{name: "HTTPS proxy", proxy: true, secure: true},
		{name: "untrusted forwarded header", forward: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := newApp(t)
			a.setup(t)
			handler, err := web.New(a.database, ical.NewService(a.database, nil, time.Now), web.Config{DataDir: t.TempDir(), BehindHTTPSProxy: test.proxy})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "http://wingmanager.example/login", strings.NewReader(account().Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if test.tls {
				request.TLS = &tls.ConnectionState{}
			}
			if test.forward {
				request.Header.Set("X-Forwarded-Proto", "https")
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			cookies := response.Result().Cookies()
			if response.Code != http.StatusSeeOther || len(cookies) != 1 || cookies[0].Secure != test.secure {
				t.Fatalf("login: status=%d cookies=%v", response.Code, cookies)
			}
			request = httptest.NewRequest(http.MethodPost, "http://wingmanager.example/logout", nil)
			request.AddCookie(cookies[0])
			if test.tls {
				request.TLS = &tls.ConnectionState{}
			}
			response = httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			cookies = response.Result().Cookies()
			if response.Code != http.StatusSeeOther || len(cookies) != 1 || cookies[0].Secure != test.secure || cookies[0].MaxAge != -1 {
				t.Fatalf("logout: status=%d cookies=%v", response.Code, cookies)
			}
		})
	}
}
