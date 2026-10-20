// Package settings owns the instance's single row of business settings.
package settings

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	_ "time/tzdata" // Keep timezone validation available on machines without zoneinfo.
)

const (
	LocaleChinese = "zh-CN"
	LocaleEnglish = "en"
)

// Locales is the complete set accepted by settings.locale's CHECK constraint.
func Locales() []string { return []string{LocaleChinese, LocaleEnglish} }

var (
	ErrTimezone = errors.New("invalid timezone")
	ErrLocale   = errors.New("invalid locale")
	ErrCurrency = errors.New("currency is required")
)

// Settings holds the instance's user-editable business settings.
type Settings struct {
	LandlordName string
	Timezone     string
	Currency     string
	Locale       string
}

// Get reads the instance's singleton settings row.
func Get(ctx context.Context, database *sql.DB) (Settings, error) {
	value, err := New(database).get(ctx)
	return Settings(value), err
}

// Save validates user-editable settings and updates the singleton row atomically.
func Save(ctx context.Context, database *sql.DB, value Settings) error {
	if value.Timezone == "" || value.Timezone == "Local" {
		return ErrTimezone
	}
	if _, err := time.LoadLocation(value.Timezone); err != nil {
		return ErrTimezone
	}
	if value.Locale != LocaleChinese && value.Locale != LocaleEnglish {
		return ErrLocale
	}
	value.Currency = strings.TrimSpace(value.Currency)
	if value.Currency == "" {
		return ErrCurrency
	}
	return New(database).save(ctx, saveParams(value))
}
