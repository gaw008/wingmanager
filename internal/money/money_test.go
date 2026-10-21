package money_test

import (
	"github.com/gaw008/wingmanager/internal/money"
	"math"
	"testing"
)

func TestDecimalMoney(t *testing.T) {
	for raw, want := range map[string]int64{"0": 0, "1.2": 120, "123.45": 12345, "92233720368547758.07": 9223372036854775807} {
		got, err := money.Parse(raw)
		if err != nil || got != want {
			t.Fatalf("%q %d %v", raw, got, err)
		}
	}
	for _, raw := range []string{"", "-1", "1.234", "1e2", "NaN", "1.", ".20", "92233720368547758.08", "92233720368547759", "+1"} {
		if _, err := money.Parse(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	if money.Format(-125) != "-1.25" || money.Format(1) != "0.01" {
		t.Fatal("formatting")
	}
}

func TestFormatIntegerLimits(t *testing.T) {
	for cents, want := range map[int64]string{math.MinInt64: "-92233720368547758.08", math.MaxInt64: "92233720368547758.07", 0: "0.00"} {
		if got := money.Format(cents); got != want {
			t.Fatalf("%d: %s != %s", cents, got, want)
		}
	}
}

func TestFormatCurrency(t *testing.T) {
	for _, test := range []struct {
		currency string
		cents    int64
		want     string
	}{
		{"USD", 337500, "$3,375.00"}, {"USD", 337525, "$3,375.25"}, {"EUR", -864001, "-€8,640.01"},
		{"CAD", 225000, "$2,250.00"}, {"AUD", -225000, "-$2,250.00"},
		{"GBP", 123456, "£1,234.56"}, {"CNY", 123456, "¥1,234.56"},
		{"JPY", 123456, "¥1,235"}, {"JPY", -123450, "-¥1,235"},
		{"JPY", 49, "¥0"}, {"JPY", -49, "¥0"}, {"JPY", 0, "¥0"},
		{"EUR", 0, "€0.00"}, {"CHF", -123456, "-1,234.56 CHF"},
		{"USD", 0, "$0.00"}, {"CHF", 100000, "1,000.00 CHF"}, {"USD", -9223372036854775808, "-$92,233,720,368,547,758.08"},
	} {
		if got := money.FormatCurrency(test.currency, test.cents); got != test.want {
			t.Errorf("FormatCurrency(%s,%d)=%s want %s", test.currency, test.cents, got, test.want)
		}
	}
}

func TestDisplayCurrenciesPositiveNegativeZero(t *testing.T) {
	for _, tt := range []struct{ currency, positive, negative, zero string }{
		{"USD", "$1,234.56", "-$1,234.56", "$0.00"},
		{"CAD", "$1,234.56", "-$1,234.56", "$0.00"},
		{"AUD", "$1,234.56", "-$1,234.56", "$0.00"},
		{"EUR", "€1,234.56", "-€1,234.56", "€0.00"},
		{"GBP", "£1,234.56", "-£1,234.56", "£0.00"},
		{"CNY", "¥1,234.56", "-¥1,234.56", "¥0.00"},
		{"JPY", "¥1,235", "-¥1,235", "¥0"},
		{"XYZ", "1,234.56 XYZ", "-1,234.56 XYZ", "0.00 XYZ"},
	} {
		for cents, want := range map[int64]string{123456: tt.positive, -123456: tt.negative, 0: tt.zero} {
			if got := money.FormatCurrency(tt.currency, cents); got != want {
				t.Errorf("FormatCurrency(%s,%d)=%q want %q", tt.currency, cents, got, want)
			}
		}
	}
	for cents, want := range map[int64]string{math.MinInt64: "-¥92,233,720,368,547,758", math.MaxInt64: "¥92,233,720,368,547,758", 50: "¥1", -50: "-¥1"} {
		if got := money.FormatCurrency("JPY", cents); got != want {
			t.Errorf("JPY %d=%q want %q", cents, got, want)
		}
	}
}
