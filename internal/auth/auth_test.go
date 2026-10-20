package auth_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gaw008/wingmanager/internal/auth"
	"github.com/gaw008/wingmanager/internal/db"
	"golang.org/x/crypto/bcrypt"
)

func newService(t *testing.T) (*auth.Service, *sql.DB) {
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
	return auth.NewService(database), database
}

func TestSetupPasswordPolicy(t *testing.T) {
	for _, test := range []struct {
		name     string
		password string
		accepted bool
	}{
		{"seven characters", "1234567", false},
		{"eight characters", "12345678", true},
		{"seven multibyte characters", strings.Repeat("密", 7), false},
		{"eight multibyte characters", strings.Repeat("密", 8), true},
		{"72 bytes", strings.Repeat("密", 24), true},
		{"73 bytes", strings.Repeat("密", 24) + "a", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, database := newService(t)
			err := service.Setup(t.Context(), "owner@example.com", test.password)
			if test.accepted {
				if err != nil {
					t.Fatalf("valid password rejected: %v", err)
				}
				if _, _, err := service.Login(t.Context(), "owner@example.com", test.password); err != nil {
					t.Fatalf("login after setup: %v", err)
				}
			} else {
				if !errors.Is(err, auth.ErrInvalidAccount) {
					t.Fatalf("invalid password: got %v, want ErrInvalidAccount", err)
				}
				var count int
				if err := database.QueryRow(`SELECT count(*) FROM users`).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != 0 {
					t.Fatal("invalid password created an owner")
				}
			}
		})
	}
}

func TestLoginDeletesExpiredSessions(t *testing.T) {
	service, database := newService(t)
	if err := service.Setup(t.Context(), "owner@example.com", "correct password"); err != nil {
		t.Fatal(err)
	}
	expired, _, err := service.Login(t.Context(), "owner@example.com", "correct password")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(expired))
	if _, err := database.Exec(`UPDATE sessions SET expires_at = ? WHERE token_hash = ?`, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339), digest[:]); err != nil {
		t.Fatal(err)
	}
	active, _, err := service.Login(t.Context(), "owner@example.com", "correct password")
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow(`SELECT count(*) FROM sessions WHERE token_hash = ?`, digest[:]).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("new login left an expired session row")
	}
	if _, _, err := service.Login(t.Context(), "owner@example.com", "correct password"); err != nil {
		t.Fatal(err)
	}
	valid, err := service.Authenticated(t.Context(), active)
	if err != nil || !valid {
		t.Fatalf("login removed an active session: valid=%v err=%v", valid, err)
	}
}

func TestConcurrentSetup(t *testing.T) {
	service, database := newService(t)
	owner, err := service.HasOwner(t.Context())
	if err != nil || owner {
		t.Fatalf("fresh instance: owner=%v err=%v", owner, err)
	}
	start := make(chan struct{})
	var group sync.WaitGroup
	results := make([]error, 2)
	for i := range 2 {
		group.Go(func() {
			<-start
			results[i] = service.Setup(t.Context(), []string{"one@example.com", "two@example.com"}[i], "correct password")
		})
	}
	close(start)
	group.Wait()
	var count int
	if err := database.QueryRow(`SELECT count(*) FROM users`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("users=%d", count)
	}
	successes, conflicts := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, auth.ErrSetupComplete):
			conflicts++
		default:
			t.Fatalf("setup returned %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	owner, err = service.HasOwner(t.Context())
	if err != nil || !owner {
		t.Fatalf("after setup: owner=%v err=%v", owner, err)
	}
	if err := service.Setup(t.Context(), "third@example.com", "correct password"); !errors.Is(err, auth.ErrSetupComplete) {
		t.Fatalf("repeated setup: %v", err)
	}
}

func TestLoginSessionExpiryAndLogout(t *testing.T) {
	service, database := newService(t)
	const email, password = "owner@example.com", "correct password"
	if err := service.Setup(t.Context(), email, password); err != nil {
		t.Fatal(err)
	}
	var hash string
	if err := database.QueryRow(`SELECT password_hash FROM users`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash == password || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		t.Fatal("password was not stored as bcrypt")
	}
	for _, credentials := range []struct{ email, password string }{
		{email, "wrong"},
		{"unknown@example.com", password},
	} {
		token, expires, err := service.Login(t.Context(), credentials.email, credentials.password)
		if !errors.Is(err, auth.ErrCredentials) || token != "" || !expires.IsZero() {
			t.Fatalf("wrong credentials accepted: token=%q expires=%v err=%v", token, expires, err)
		}
	}
	token, expires, err := service.Login(t.Context(), email, password)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(secret) != 32 {
		t.Fatalf("invalid token: bytes=%d error=%v", len(secret), err)
	}
	if !expires.After(time.Now()) {
		t.Fatalf("invalid expiry: %v", expires)
	}
	var stored []byte
	if err := database.QueryRow(`SELECT token_hash FROM sessions`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256([]byte(token))
	if string(stored) != string(expected[:]) {
		t.Fatal("session did not store only the credential hash")
	}
	valid, err := service.Authenticated(t.Context(), token)
	if err != nil || !valid {
		t.Fatalf("new session: valid=%v err=%v", valid, err)
	}
	if _, err := database.Exec(`UPDATE sessions SET expires_at = ?`, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	valid, err = service.Authenticated(t.Context(), token)
	if err != nil || valid {
		t.Fatalf("expired session: valid=%v err=%v", valid, err)
	}
	token, _, err = service.Login(t.Context(), email, password)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Logout(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	var sessions int
	digest := sha256.Sum256([]byte(token))
	if err := database.QueryRow(`SELECT count(*) FROM sessions WHERE token_hash = ?`, digest[:]).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatal("logout left the session row")
	}
	valid, err = service.Authenticated(t.Context(), token)
	if err != nil || valid {
		t.Fatalf("logged-out session: valid=%v err=%v", valid, err)
	}
	if err := service.Logout(t.Context(), token); err != nil {
		t.Fatalf("repeated logout: %v", err)
	}
}
