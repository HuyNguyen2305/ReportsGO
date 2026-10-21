package model

import (
	"errors"
	"fmt"
	"strings"
)

// Money is an amount in cents. Integer cents keep sums exact; the database stores numeric(12,2).
type Money int64

// ErrInvalidMoney is returned by ParseMoney for malformed amounts.
var ErrInvalidMoney = errors.New("invalid money amount")

// maxMoneyDigits keeps parsed amounts far from int64 overflow (cents) while exceeding numeric(12,2).
const maxMoneyDigits = 15

// String formats the amount with exactly two decimals, e.g. "148.56" or "-0.05".
func (m Money) String() string {
	sign := ""
	cents := int64(m)
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

// ParseMoney parses "148.56", "148.5", "148" or "-1.20". More than two decimals, signs other than a
// leading "-", exponents, separators and empty input are rejected.
func ParseMoney(s string) (Money, error) {
	orig := s
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")

	whole, frac, hasFrac := strings.Cut(s, ".")
	if whole == "" || len(whole)+len(frac) > maxMoneyDigits || (hasFrac && (frac == "" || len(frac) > 2)) {
		return 0, fmt.Errorf("%w: %q", ErrInvalidMoney, orig)
	}
	if !allDigits(whole) || !allDigits(frac) {
		return 0, fmt.Errorf("%w: %q", ErrInvalidMoney, orig)
	}

	var cents int64
	for _, r := range whole + (frac + "00")[:2] {
		cents = cents*10 + int64(r-'0')
	}
	if negative {
		cents = -cents
	}
	return Money(cents), nil
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
