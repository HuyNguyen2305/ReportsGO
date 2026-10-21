package model_test

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
)

func TestMoneyString(t *testing.T) {
	tests := []struct {
		cents model.Money
		want  string
	}{
		{0, "0.00"},
		{5, "0.05"},
		{50, "0.50"},
		{100, "1.00"},
		{14856, "148.56"},
		{26400, "264.00"},
		{242840, "2428.40"},
		{-5, "-0.05"},
		{-100, "-1.00"},
		{-14856, "-148.56"},
		{99999999999, "999999999.99"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			require.Equal(t, tt.want, tt.cents.String())
		})
	}
}

func TestParseMoney(t *testing.T) {
	tests := []struct {
		in   string
		want model.Money
	}{
		{"0", 0},
		{"0.00", 0},
		{"-0", 0},
		{"-0.00", 0},
		{"148", 14800},
		{"148.5", 14850},
		{"148.56", 14856},
		{"0.05", 5},
		{"00.05", 5},
		{"-1.20", -120},
		{"-148.56", -14856},
		{"9999999999999", 999999999999900},
		{"999999999999.99", 99999999999999},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := model.ParseMoney(tt.in)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseMoneyRejectsMalformedInput(t *testing.T) {
	tests := []string{
		"", "-", ".", ".5", "1.", "1.234", "+1", "1e3", "1,000", " 1", "1 ", "abc", "--1", "1.2.3",
		"12a", "١٢", "0x10", "1_000", "-.5", "NaN", "Inf",
		strings.Repeat("9", 16),         // too many digits
		strings.Repeat("9", 14) + ".99", // 16 digits including the fraction
	}

	for _, in := range tests {
		t.Run(in, func(t *testing.T) {
			_, err := model.ParseMoney(in)
			require.ErrorIs(t, err, model.ErrInvalidMoney)
		})
	}
}

func TestMoneyRoundTrip(t *testing.T) {
	values := []model.Money{0, 1, 5, 9, 10, 99, 100, 101, 14856, -1, -99, -100, -14856, 99999999999999, -99999999999999}
	for _, v := range values {
		got, err := model.ParseMoney(v.String())
		require.NoError(t, err, "value %d formatted as %q", int64(v), v.String())
		require.Equal(t, v, got)
	}
}

func TestMoneyStringHandlesMinInt64WithoutPanic(t *testing.T) {
	// -MinInt64 overflows; the result is meaningless but formatting must not panic.
	require.NotPanics(t, func() { _ = model.Money(math.MinInt64).String() })
}
