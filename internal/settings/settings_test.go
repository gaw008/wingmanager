package settings

import (
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/gaw008/wingmanager/internal/db"
)

func TestLocalesMatchDatabaseConstraint(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.Migrate(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	var schema string
	if err := database.QueryRow(`SELECT sql FROM sqlite_master WHERE name='settings'`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`CHECK \(locale IN \(([^)]+)\)\)`).FindStringSubmatch(schema)
	if len(match) != 2 {
		t.Fatal("locale CHECK not found")
	}
	var values []string
	for _, literal := range regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(match[1], -1) {
		values = append(values, literal[1])
	}
	want := Locales()
	slices.Sort(values)
	slices.Sort(want)
	if !slices.Equal(values, want) {
		t.Fatalf("DB locales=%v Go locales=%v", values, want)
	}
	query := New(database)
	value, err := query.Get(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, zone := range []string{"", "Local", "Invalid/Timezone"} {
		value.Timezone = zone
		if err := query.Save(t.Context(), value); err != ErrTimezone {
			t.Fatalf("zone %q: %v", zone, err)
		}
	}
	value.Timezone = "UTC"
	value.Locale = "fr"
	if err := query.Save(t.Context(), value); err != ErrLocale {
		t.Fatalf("invalid locale: %v", err)
	}
	value.Locale = LocaleEnglish
	value.Currency = " "
	if err := query.Save(t.Context(), value); err != ErrCurrency {
		t.Fatalf("empty currency: %v", err)
	}
}
