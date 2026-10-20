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
