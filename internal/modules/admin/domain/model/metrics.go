package model

import "math"

// This file holds the dashboard's growth and percentage arithmetic. Both take the
// integer minor-unit amounts the rest of the system stores — money is never a
// float (Constitution II) — and return the single ratio the response shows, so the
// rounding rules live in one tested place (FR-011, FR-013, FR-024).

// GrowthPercent returns the period-over-period growth of two integer amounts as a
// percentage rounded to two decimals: (current-last)/last*100. It returns nil
// when last is zero, because a zero base has no meaningful percentage: the
// response must show "no comparison" rather than 0 or 100 (FR-011, FR-013). A
// negative result keeps its sign (FR "growth âm phải giữ dấu").
func GrowthPercent(current, last int64) *float64 {
	if last == 0 {
		return nil
	}
	growth := round(float64(current-last)/float64(last)*100, 2)
	return &growth
}

// Percentage returns part as a share of whole, as a percentage rounded to one
// decimal. A zero whole answers zero rather than a NaN, so an empty period never
// produces a broken share (FR-024).
func Percentage(part, whole int64) float64 {
	if whole == 0 {
		return 0
	}
	return round(float64(part)/float64(whole)*100, 1)
}

// round rounds value to the given number of decimal places.
func round(value float64, places int) float64 {
	factor := math.Pow(10, float64(places))
	return math.Round(value*factor) / factor
}
