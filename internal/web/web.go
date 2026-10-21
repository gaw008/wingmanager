// Package web serves the embedded UI and coordinates the domain modules.
package web

import (
	"bytes"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gaw008/wingmanager/internal/auth"
	"github.com/gaw008/wingmanager/internal/document"
	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/money"
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/settings"
)

//go:embed templates/*.html static/*
var assets embed.FS

const sessionCookie = "wingmanager_session"

type server struct {
	auth          *auth.Service
	database      *sql.DB
	templates     *template.Template
	now           func() time.Time
	ical          *ical.Service
	documents     *document.Store
	secureCookies bool
}

// Config contains the server's deployment settings.
type Config struct {
	DataDir string
	// BehindHTTPSProxy sets Secure cookies when TLS terminates at a reverse proxy.
	// Forwarded request headers never affect this setting.
	BehindHTTPSProxy bool
}

type pageData struct {
	Navigation     []navigationItem
	AccountInitial string
	Page           string
	Settings       settings.Settings
	Locales        []string
	SignedIn       bool
	Error          string
	Properties     []property.Property
	Property       property.Property
	Units          []property.Unit
	People         []person.Person
	Person         person.Person
	FirstUnitName  string
	UnitName       string
	ShowArchived   bool
	Search         string
	bookingPage
	feedPage
	leasePage
	moneyPage
	taskPage
	todayPage
	documentPage
}

// New serves setup/login/static and tokenized iCal exports publicly. All other
// pages require a session. service must be shared with the scheduler for single-flight.
// Cross-origin protection wraps all routes, including the unauthenticated forms.
// Config.DataDir is the same directory passed to serve; attachments live in its files subdirectory.
func New(database *sql.DB, service *ical.Service, config Config) (http.Handler, error) {
	return newHandler(database, time.Now, service, config)
}

func newHandler(database *sql.DB, now func() time.Time, service *ical.Service, config Config) (http.Handler, error) {
	templates, err := template.New("pages").Funcs(template.FuncMap{
		"money":    money.Format,
		"currency": money.FormatCurrency,
		"syncError": func(locale, message string) string {
			if message == ical.ErrEmptyFeed.Error() {
				return catalogs[locale]["empty_feed_warning"]
			}
			return message
		},
		"t": func(locale, key string) (string, error) {
			value, ok := catalogs[locale][key]
			if !ok {
				return "", fmt.Errorf("missing translation %s/%s", locale, key)
			}
			return value, nil
		},
	}).ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	s := &server{auth: auth.NewService(database), database: database, templates: templates, now: now, ical: service, documents: document.NewStore(database, config.DataDir), secureCookies: config.BehindHTTPSProxy}
	static, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	mux.HandleFunc("GET /setup", s.setup)
	mux.HandleFunc("POST /setup", s.setup)
	mux.HandleFunc("GET /login", s.login)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("GET /{$}", s.today)
	mux.HandleFunc("GET /settings", s.editSettings)
	mux.HandleFunc("POST /settings", s.editSettings)
	s.registerPropertyAndPersonRoutes(mux)
	s.registerBookingRoutes(mux)
	s.registerFeedRoutes(mux)
	s.registerLeaseRoutes(mux)
	s.registerMoneyRoutes(mux)
	s.registerTaskRoutes(mux)
	s.registerDocumentRoutes(mux)
	return http.NewCrossOriginProtection().Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/static/") || strings.HasPrefix(r.URL.Path, "/ical/") {
			mux.ServeHTTP(w, r)
			return
		}
		owner, err := s.auth.HasOwner(r.Context())
		if err != nil {
			s.fail(w, err)
			return
		}
		if r.URL.Path == "/setup" {
			if owner {
				if r.Method == http.MethodPost {
					s.render(w, r, pageData{Page: "login"}, http.StatusConflict)
					return
				}
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
		} else if !owner {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		} else if r.URL.Path != "/login" {
			cookie, err := r.Cookie(sessionCookie)
			if errors.Is(err, http.ErrNoCookie) {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			if err != nil {
				s.fail(w, err)
				return
			}
			valid, err := s.auth.Authenticated(r.Context(), cookie.Value)
			if err != nil {
				s.fail(w, err)
				return
			}
			if !valid {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
		}
		mux.ServeHTTP(w, r)
	})), nil
}

func (s *server) setup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.render(w, r, pageData{Page: "setup"}, http.StatusOK)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.render(w, r, pageData{Page: "setup", Error: "invalid_form"}, http.StatusBadRequest)
		return
	}
	err := s.auth.Setup(r.Context(), r.PostForm.Get("email"), r.PostForm.Get("password"))
	switch {
	case errors.Is(err, auth.ErrSetupComplete):
		s.render(w, r, pageData{Page: "login"}, http.StatusConflict)
	case errors.Is(err, auth.ErrInvalidAccount):
		s.render(w, r, pageData{Page: "setup", Error: "invalid_account"}, http.StatusBadRequest)
	case err != nil:
		s.fail(w, err)
	default:
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.render(w, r, pageData{Page: "login"}, http.StatusOK)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.render(w, r, pageData{Page: "login", Error: "invalid_form"}, http.StatusBadRequest)
		return
	}
	token, expires, err := s.auth.Login(r.Context(), r.PostForm.Get("email"), r.PostForm.Get("password"))
	if errors.Is(err, auth.ErrCredentials) {
		s.render(w, r, pageData{Page: "login", Error: "invalid_credentials"}, http.StatusUnauthorized)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.secureCookies || r.TLS != nil, Expires: expires})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.auth.Logout(r.Context(), cookie.Value); err != nil {
		s.fail(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.secureCookies || r.TLS != nil, MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *server) editSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.render(w, r, pageData{Page: "settings"}, http.StatusOK)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.render(w, r, pageData{Page: "settings", Error: "invalid_form"}, http.StatusBadRequest)
		return
	}
	err := settings.Save(r.Context(), s.database, settings.Settings{
		LandlordName: r.PostForm.Get("landlord_name"), Timezone: r.PostForm.Get("timezone"),
		Currency: r.PostForm.Get("currency"), Locale: r.PostForm.Get("locale"),
	})
	switch {
	case errors.Is(err, settings.ErrTimezone):
		s.render(w, r, pageData{Page: "settings", Error: "invalid_timezone"}, http.StatusBadRequest)
	case errors.Is(err, settings.ErrLocale):
		s.render(w, r, pageData{Page: "settings", Error: "invalid_locale"}, http.StatusBadRequest)
	case errors.Is(err, settings.ErrCurrency):
		s.render(w, r, pageData{Page: "settings", Error: "invalid_currency"}, http.StatusBadRequest)
	case err != nil:
		s.fail(w, err)
	default:
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
	}
}

func (s *server) render(w http.ResponseWriter, r *http.Request, data pageData, status int) {
	value, err := settings.Get(r.Context(), s.database)
	if err != nil {
		s.fail(w, err)
		return
	}
	data.Settings = value
	data.Locales = settings.Locales()
	data.SignedIn = data.Page != "setup" && data.Page != "login"
	data.Navigation = navigation(data.Page)
	data.AccountInitial = accountInitial(value.LandlordName, value.Locale)
	if err := s.loadDocumentPanel(r, &data); err != nil {
		s.fail(w, err)
		return
	}
	if data.Page == "new_property" && r.Method == http.MethodGet {
		data.FirstUnitName = catalogs[value.Locale]["default_unit_name"]
	}
	var body bytes.Buffer
	templateName := "base"
	if data.Page == "new_booking" || data.Page == "edit_booking" {
		w.Header().Add("Vary", "HX-Request, HX-Target")
	}
	if r.Header.Get("HX-Request") == "true" && (r.Header.Get("HX-Target") == "drawer-body" || r.URL.Query().Get("drawer") == "1") && (data.Page == "new_booking" || data.Page == "edit_booking") {
		templateName = "booking_drawer"
	}
	if err := s.templates.ExecuteTemplate(&body, templateName, data); err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := body.WriteTo(w); err != nil {
		log.Printf("write response: %v", err)
	}
}

func (s *server) fail(w http.ResponseWriter, err error) {
	log.Printf("request failed: %v", err)
	http.Error(w, "Internal server error", http.StatusInternalServerError)
}
