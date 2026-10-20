package auth_test

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/gaw008/wingmanager/internal/auth"
)

func TestResetPasswordPolicy(t *testing.T) {
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
			service, _ := newService(t)
			const email, oldPassword = "owner@example.com", "original password"
			if err := service.Setup(t.Context(), email, oldPassword); err != nil {
				t.Fatal(err)
			}
			token, _, err := service.Login(t.Context(), email, oldPassword)
			if err != nil {
				t.Fatal(err)
			}
			err = service.ResetPassword(t.Context(), test.password)
			if test.accepted {
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err := service.Login(t.Context(), email, test.password); err != nil {
					t.Fatalf("new password failed: %v", err)
				}
			} else {
				if !errors.Is(err, auth.ErrInvalidAccount) {
					t.Fatalf("invalid password: %v", err)
				}
				if _, _, err := service.Login(t.Context(), email, oldPassword); err != nil {
					t.Fatalf("invalid reset changed password: %v", err)
				}
			}
			valid, err := service.Authenticated(t.Context(), token)
			if err != nil || valid == test.accepted {
				t.Fatalf("old session valid=%v err=%v", valid, err)
			}
		})
	}
}

func TestResetPasswordInvalidatesAllSessions(t *testing.T) {
	service, _ := newService(t)
	const email, oldPassword, newPassword = "owner@example.com", "original password", "replacement password"
	if err := service.Setup(t.Context(), email, oldPassword); err != nil {
		t.Fatal(err)
	}
	var tokens []string
	for range 2 {
		token, _, err := service.Login(t.Context(), email, oldPassword)
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, token)
	}
	for range 2 {
		if err := service.ResetPassword(t.Context(), newPassword); err != nil {
			t.Fatal(err)
		}
		for _, token := range tokens {
			valid, err := service.Authenticated(t.Context(), token)
			if err != nil || valid {
				t.Fatalf("old session valid=%v err=%v", valid, err)
			}
		}
		if _, _, err := service.Login(t.Context(), email, oldPassword); !errors.Is(err, auth.ErrCredentials) {
			t.Fatalf("old password: %v", err)
		}
		token, _, err := service.Login(t.Context(), email, newPassword)
		if err != nil {
			t.Fatalf("new password: %v", err)
		}
		tokens = append(tokens, token)
	}
}

func TestResetPasswordMissingOwner(t *testing.T) {
	service, _ := newService(t)
	if err := service.ResetPassword(t.Context(), "replacement password"); !errors.Is(err, auth.ErrNoOwner) {
		t.Fatalf("empty instance: %v", err)
	}
}

func TestResetPasswordRollsBackOnSessionDeleteFailure(t *testing.T) {
	service, database := newService(t)
	const email, oldPassword = "owner@example.com", "original password"
	if err := service.Setup(t.Context(), email, oldPassword); err != nil {
		t.Fatal(err)
	}
	token, _, err := service.Login(t.Context(), email, oldPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TRIGGER reject_session_delete BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT, 'delete failed'); END`); err != nil {
		t.Fatal(err)
	}
	if err := service.ResetPassword(t.Context(), "replacement password"); err == nil {
		t.Fatal("reset succeeded after session deletion failed")
	}
	valid, err := service.Authenticated(t.Context(), token)
	if err != nil || !valid {
		t.Fatalf("failed reset invalidated session: valid=%v err=%v", valid, err)
	}
	if _, _, err := service.Login(t.Context(), email, oldPassword); err != nil {
		t.Fatalf("failed reset changed password: %v", err)
	}
}

func TestResetPasswordConcurrentLogin(t *testing.T) {
	service, _ := newService(t)
	const email, oldPassword, newPassword = "owner@example.com", "original password", "replacement password"
	if err := service.Setup(t.Context(), email, oldPassword); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var group sync.WaitGroup
	var resetErr, loginErr error
	var token string
	group.Go(func() {
		<-start
		resetErr = service.ResetPassword(t.Context(), newPassword)
	})
	group.Go(func() {
		<-start
		token, _, loginErr = service.Login(t.Context(), email, oldPassword)
	})
	close(start)
	group.Wait()
	if resetErr != nil {
		t.Fatal(resetErr)
	}
	if loginErr != nil && !errors.Is(loginErr, auth.ErrCredentials) {
		t.Fatalf("concurrent login: %v", loginErr)
	}
	if token != "" {
		valid, err := service.Authenticated(t.Context(), token)
		if err != nil || valid {
			t.Fatalf("concurrent old-password session survived reset: valid=%v err=%v", valid, err)
		}
	}
	if _, _, err := service.Login(t.Context(), email, newPassword); err != nil {
		t.Fatalf("new password after concurrent login: %v", err)
	}
}
