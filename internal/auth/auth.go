// Package auth owns the single owner account and hashed session credentials.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// Sessions last seven days, balancing daily use with a bounded login lifetime.
const sessionDuration = 7 * 24 * time.Hour

const (
	minPasswordCharacters = 8
	maxPasswordBytes      = 72 // bcrypt rejects passwords above this byte limit.
)

var (
	ErrSetupComplete  = errors.New("owner already exists")
	ErrInvalidAccount = errors.New("invalid email or password")
	ErrCredentials    = errors.New("incorrect email or password")
	ErrNoOwner        = errors.New("owner does not exist")
)

type Service struct {
	database *sql.DB
	queries  *Queries
}

// NewService uses a database opened by db.Open so setup transactions are IMMEDIATE.
func NewService(database *sql.DB) *Service {
	return &Service{database: database, queries: New(database)}
}

// HasOwner reports whether first-run setup has completed.
func (s *Service) HasOwner(ctx context.Context) (bool, error) {
	return s.queries.hasOwner(ctx)
}

// Setup creates the only owner with a password of at least 8 characters and at most 72 bytes.
// Concurrent attempts return ErrSetupComplete.
func (s *Service) Setup(ctx context.Context, email, password string) error {
	email = strings.TrimSpace(email)
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return ErrInvalidAccount
	}
	hash, err := passwordHash(password)
	if err != nil {
		return err
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	queries := s.queries.WithTx(tx)
	exists, err := queries.hasOwner(ctx)
	if err != nil {
		return err
	}
	if exists {
		return ErrSetupComplete
	}
	if err := queries.createOwner(ctx, createOwnerParams{Email: email, PasswordHash: hash}); err != nil {
		return err
	}
	return tx.Commit()
}

// ResetPassword replaces the owner's password and invalidates all sessions atomically.
// It uses the same password rule as Setup and returns ErrNoOwner before setup.
func (s *Service) ResetPassword(ctx context.Context, password string) error {
	hash, err := passwordHash(password)
	if err != nil {
		return err
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	queries := s.queries.WithTx(tx)
	count, err := queries.resetOwnerPassword(ctx, hash)
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNoOwner
	}
	if err := queries.deleteAllSessions(ctx); err != nil {
		return err
	}
	return tx.Commit()
}

func passwordHash(password string) (string, error) {
	if utf8.RuneCountInString(password) < minPasswordCharacters || len(password) > maxPasswordBytes {
		return "", ErrInvalidAccount
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

// Login removes expired sessions and issues a new opaque token after verifying credentials.
// Only the token's SHA-256 credential hash is persisted.
func (s *Service) Login(ctx context.Context, email, password string) (token string, expires time.Time, err error) {
	// The IMMEDIATE lock serializes credential verification with password resets.
	// A login using the old hash cannot issue a session after a reset commits.
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return "", time.Time{}, err
	}
	defer tx.Rollback()
	queries := s.queries.WithTx(tx)
	user, err := queries.findUser(ctx, strings.TrimSpace(email))
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, ErrCredentials
	}
	if err != nil {
		return "", time.Time{}, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return "", time.Time{}, ErrCredentials
		}
		return "", time.Time{}, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", time.Time{}, err
	}
	token = base64.RawURLEncoding.EncodeToString(secret)
	hash := sha256.Sum256([]byte(token))
	now := time.Now().UTC()
	if err := queries.deleteExpiredSessions(ctx, now.Format(time.RFC3339)); err != nil {
		return "", time.Time{}, err
	}
	expires = now.Add(sessionDuration)
	if err := queries.createSession(ctx, createSessionParams{TokenHash: hash[:], UserID: user.ID, ExpiresAt: expires.Format(time.RFC3339)}); err != nil {
		return "", time.Time{}, err
	}
	if err := tx.Commit(); err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

// Authenticated accepts only an unexpired session token.
func (s *Service) Authenticated(ctx context.Context, token string) (bool, error) {
	hash := sha256.Sum256([]byte(token))
	return s.queries.sessionExists(ctx, sessionExistsParams{TokenHash: hash[:], ExpiresAt: time.Now().UTC().Format(time.RFC3339)})
}

// Logout invalidates the token; repeating logout is harmless.
func (s *Service) Logout(ctx context.Context, token string) error {
	hash := sha256.Sum256([]byte(token))
	return s.queries.deleteSession(ctx, hash[:])
}
