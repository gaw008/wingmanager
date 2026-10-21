// Package money converts decimal amounts and integer cents without floating point.
package money

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

var ErrAmount = errors.New("invalid decimal amount")

// Parse accepts a nonnegative decimal amount with at most two fractional digits.
func Parse(raw string) (int64, error) {
	parts := strings.Split(strings.TrimSpace(raw), ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, ErrAmount
	}
	for _, part := range parts {
		if part == "" {
			return 0, ErrAmount
		}
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return 0, ErrAmount
			}
		}
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole > math.MaxInt64/100 {
		return 0, ErrAmount
	}
	var fraction int64
	if len(parts) == 2 {
		if len(parts[1]) > 2 {
			return 0, ErrAmount
		}
		text := parts[1]
		if len(text) == 1 {
			text += "0"
		}
		fraction, err = strconv.ParseInt(text, 10, 64)
		if err != nil {
			return 0, ErrAmount
		}
	}
	if whole*100 > math.MaxInt64-fraction {
		return 0, ErrAmount
	}
	return whole*100 + fraction, nil
}

// Format renders cents as a decimal amount with exactly two fractional digits.
func Format(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
	}
	whole, fraction := cents/100, cents%100
	if whole < 0 {
		whole = -whole
	}
	if fraction < 0 {
		fraction = -fraction
	}
	return fmt.Sprintf("%s%d.%02d", sign, whole, fraction)
}

// FormatCurrency renders a display amount with grouped thousands and a currency
// label. Whole amounts omit cents; form values should continue to use Format.
func FormatCurrency(currency string, cents int64) string {
	amount := Format(cents)
	sign := ""
	if strings.HasPrefix(amount, "-") {
		sign, amount = "-", amount[1:]
	}
	parts := strings.SplitN(amount, ".", 2)
	whole := parts[0]
	first := len(whole) % 3
	if first == 0 {
		first = 3
	}
	var grouped strings.Builder
	grouped.WriteString(whole[:first])
	for i := first; i < len(whole); i += 3 {
		grouped.WriteByte(',')
		grouped.WriteString(whole[i : i+3])
	}
	if parts[1] != "00" {
		grouped.WriteByte('.')
		grouped.WriteString(parts[1])
	}
	symbol := map[string]string{"USD": "$", "EUR": "€", "GBP": "£", "CNY": "CN¥", "JPY": "JP¥", "CAD": "CA$", "AUD": "A$"}[currency]
	if symbol == "" {
		symbol = currency + " "
	}
	return sign + symbol + grouped.String()
}
