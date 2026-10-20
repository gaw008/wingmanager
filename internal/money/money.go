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
