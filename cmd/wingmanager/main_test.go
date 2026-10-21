package main

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gaw008/wingmanager/internal/auth"
	"github.com/gaw008/wingmanager/internal/db"
	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/web"
)

func TestVersionBuildInjection(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "wingmanager")
	build := exec.Command("go", "build", "-ldflags=-X main.version=v0.1-test", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	output, err := exec.Command(binary, "version").CombinedOutput()
	if err != nil || string(output) != "v0.1-test\n" {
		t.Fatalf("version: %q, %v", output, err)
	}
	var local bytes.Buffer
	if err := run([]string{"version"}, nil, &local); err != nil || local.String() != "dev\n" {
		t.Fatalf("local version: %q, %v", local.String(), err)
	}
}

func TestUsageAndArguments(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"serve", "--help"}, {"backup", "--help"}, {"reset-password", "--help"}, {"seed-demo", "--help"}, {"version", "--help"}} {
		var output bytes.Buffer
		if err := run(args, nil, &output); err != nil {
			t.Fatal(err)
		}
		if args[0] == "--help" {
			for _, command := range []string{"serve", "backup", "reset-password", "seed-demo", "version"} {
				if !strings.Contains(output.String(), command) {
					t.Fatalf("missing command %s: %s", command, &output)
				}
			}
		} else if args[0] != "version" && !strings.Contains(output.String(), `default "./data"`) {
			t.Fatalf("missing data directory default: %s", &output)
		}
		if args[0] == "serve" && (!strings.Contains(output.String(), "127.0.0.1:8080") || !strings.Contains(output.String(), "behind-https-proxy")) {
			t.Fatal(output.String())
		}
	}
	for _, args := range [][]string{nil, {"unknown"}, {"version", "extra"}, {"backup"}, {"backup", "one", "two"}, {"reset-password"}, {"serve", "--unknown"}} {
		if err := run(args, nil, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted invalid arguments: %q", args)
		}
	}
}

func TestCommandsSeedBackupReset(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	var output bytes.Buffer
	if err := run([]string{"seed-demo", "--data-dir", dir}, nil, &output); err != nil {
		t.Fatal(err)
	}
	fields := strings.Split(output.String(), "\n")
	if len(fields) < 4 || fields[0] != "Demo credentials:" {
		t.Fatal(output.String())
	}
	email := strings.TrimPrefix(fields[1], "Email: ")
	password := strings.TrimPrefix(fields[2], "Password: ")
	database, err := db.Open(filepath.Join(dir, "wingmanager.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	handler, err := web.New(database, ical.NewService(database, nil, time.Now), web.Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"email": {email}, "password": {password}}
	request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, request)
	if login.Code != http.StatusSeeOther || len(login.Result().Cookies()) != 1 {
		t.Fatalf("printed credentials failed HTTP login: %d %s", login.Code, login.Body)
	}
	request = httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(login.Result().Cookies()[0])
	today := httptest.NewRecorder()
	handler.ServeHTTP(today, request)
	if today.Code != http.StatusOK {
		t.Fatalf("Today status: %d", today.Code)
	}
	for _, want := range []string{"Demo departing guest", "Demo overdue tenant", `id="ytd-net">$625`} {
		if !strings.Contains(today.Body.String(), want) {
			t.Fatalf("Today missing %s", want)
		}
	}
	request = httptest.NewRequest(http.MethodGet, "/?tab=in", nil)
	request.AddCookie(login.Result().Cookies()[0])
	arrivals := httptest.NewRecorder()
	handler.ServeHTTP(arrivals, request)
	if arrivals.Code != http.StatusOK || !strings.Contains(arrivals.Body.String(), "Demo arriving guest") || strings.Contains(arrivals.Body.String(), "Demo departing guest") {
		t.Fatalf("Today arrivals tab: %d %s", arrivals.Code, arrivals.Body)
	}
	service := auth.NewService(database)
	token, _, err := service.Login(t.Context(), email, password)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "backup")
	if err := run([]string{"backup", "--data-dir", dir, dest}, nil, &output); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "wingmanager.db")); err != nil {
		t.Fatal(err)
	}
	newPassword := "  new password with spaces  "
	if err := run([]string{"reset-password", "--data-dir", dir, "--password-stdin"}, strings.NewReader(newPassword+"\r\n"), &output); err != nil {
		t.Fatal(err)
	}
	if valid, err := service.Authenticated(t.Context(), token); err != nil || valid {
		t.Fatalf("old session: %v %v", valid, err)
	}
	if _, _, err := service.Login(t.Context(), email, newPassword); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"seed-demo", "--data-dir", dir}, nil, &output); !errors.Is(err, auth.ErrSetupComplete) {
		t.Fatalf("second seed: %v", err)
	}
}
