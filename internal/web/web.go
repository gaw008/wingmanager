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
	"github.com/gaw008/wingmanager/internal/person"
	"github.com/gaw008/wingmanager/internal/property"
	"github.com/gaw008/wingmanager/internal/settings"
)

//go:embed templates/*.html static/*
var assets embed.FS

const sessionCookie = "wingmanager_session"

type server struct {
	auth      *auth.Service
	database  *sql.DB
	templates *template.Template
	now       func() time.Time
}

type pageData struct {
	Page          string
	Settings      settings.Settings
	Locales       []string
	SignedIn      bool
	Error         string
	Properties    []property.Property
	Property      property.Property
	Units         []property.Unit
	People        []person.Person
	Person        person.Person
	FirstUnitName string
	UnitName      string
	ShowArchived  bool
	Search        string
	bookingPage
}

// New serves setup/login/static publicly and requires a session for every other page.
// Cross-origin protection wraps all routes, including the unauthenticated forms.
func New(database *sql.DB) (http.Handler, error) {
	return newHandler(database, time.Now)
}

func newHandler(database *sql.DB, now func() time.Time) (http.Handler, error) {
	templates, err := template.New("pages").Funcs(template.FuncMap{
		"calendarColumns": func(days int) int { return days + 1 },
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
	s := &server{auth: auth.NewService(database), database: database, templates: templates, now: now}
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
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		s.render(w, r, pageData{Page: "today"}, http.StatusOK)
	})
	mux.HandleFunc("GET /settings", s.editSettings)
	mux.HandleFunc("POST /settings", s.editSettings)
	s.registerPropertyAndPersonRoutes(mux)
	s.registerBookingRoutes(mux)
	return http.NewCrossOriginProtection().Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/static/") {
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
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil, Expires: expires})
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
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil, MaxAge: -1})
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
	if data.Page == "new_property" && r.Method == http.MethodGet {
		data.FirstUnitName = catalogs[value.Locale]["default_unit_name"]
	}
	var body bytes.Buffer
	if err := s.templates.ExecuteTemplate(&body, "base", data); err != nil {
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
