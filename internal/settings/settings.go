// Package settings owns the instance's single row of business settings.
package settings

import (
	"context"
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

// Save validates user-editable settings and updates the singleton row atomically.
func (q *Queries) Save(ctx context.Context, value GetRow) error {
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
	return q.save(ctx, saveParams(value))
}
