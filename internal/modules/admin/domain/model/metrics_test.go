package model

import (
	"math"
	"testing"
)

// This file pins the dashboard's growth and percentage arithmetic: growth is
// nullable on a zero base, rounded to two decimals and keeps its sign; percentage
// is rounded to one decimal and a zero denominator answers zero rather than NaN
// (FR-011, FR-013, FR-024).

func assertPercent(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}

// FR-011, US2 acceptance 3: a zero base has no meaningful percentage, so growth
// is nil rather than 0 or 100.
func TestGrowthPercentIsNilOnAZeroBase(t *testing.T) {
	if got := GrowthPercent(1000, 0); got != nil {
		t.Fatalf("growth on a zero base must be nil, got %v", *got)
	}
	if got := GrowthPercent(0, 0); got != nil {
		t.Fatalf("growth on a zero base must be nil even when current is zero, got %v", *got)
	}
}

func TestGrowthPercentComputesAndRoundsToTwoDecimals(t *testing.T) {
	cases := []struct {
		name          string
		current, last int64
		want          float64
	}{
		{"flat", 100, 100, 0},
		{"doubling", 200, 100, 100},
		{"two-decimal rounding", 1000, 300, 233.33},
		{"negative keeps its sign", 50, 100, -50},
		{"negative rounding", 1234, 5678, -78.27},
		{"zero current is fully negative", 0, 100, -100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := GrowthPercent(tc.current, tc.last)
			if got == nil {
				t.Fatalf("growth of %d over %d must not be nil", tc.current, tc.last)
			}
			assertPercent(t, "growth", *got, tc.want)
		})
	}
}

// FR-024: percentage is the part over the whole, rounded to one decimal.
func TestPercentageRoundsToOneDecimal(t *testing.T) {
	cases := []struct {
		name        string
		part, whole int64
		want        float64
	}{
		{"a third", 1, 3, 33.3},
		{"two thirds", 2, 3, 66.7},
		{"half", 50, 100, 50},
		{"whole", 100, 100, 100},
		{"tenth", 1, 10, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertPercent(t, "percentage", Percentage(tc.part, tc.whole), tc.want)
		})
	}
}

// FR-024: a zero-revenue denominator answers zero, never NaN or +Inf.
func TestPercentageHasAZeroDenominator(t *testing.T) {
	got := Percentage(1234, 0)
	if got != 0 {
		t.Fatalf("percentage over a zero whole must be zero, got %v", got)
	}
	if math.IsNaN(got) || math.IsInf(got, 0) {
		t.Fatalf("percentage must never be NaN or Inf, got %v", got)
	}
}
