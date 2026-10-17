package model

import "time"

// This file holds the pure calendar-window helpers the dashboard uses. Every
// boundary is built in the shop timezone, so a day, week or month edge is the
// operator's edge while the instants themselves stay absolute (FR-007, FR-008,
// FR-009, research D4). The helpers read no clock: the caller passes the single
// instant the request already captured.

// Window is a half-open interval of instants [From, To): From is inclusive, To is
// exclusive. The admin module defines its own value type so the domain stays free
// of the cross-module contract package; the application converts it at the read
// boundary (Constitution I).
type Window struct {
	// From is the inclusive start of the window.
	From time.Time
	// To is the exclusive end of the window.
	To time.Time
}

// CurrentMonth returns the month-to-date window: 00:00 on the first day of the
// current month in the shop timezone up to now.
func CurrentMonth(now time.Time, location *time.Location) Window {
	local := now.In(location)
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
	return Window{From: start, To: now}
}

// PreviousMonthSamePeriod returns the comparable window in the previous month:
// the same elapsed duration as the current month-to-date, starting at 00:00 on
// the first day of the previous month. When that duration would run past the
// previous month's end the end is clamped to the current month's first day, so a
// long month is never compared with more than the previous month actually held
// (FR-008).
func PreviousMonthSamePeriod(now time.Time, location *time.Location) Window {
	current := CurrentMonth(now, location)
	elapsed := now.Sub(current.From)

	start := current.From.AddDate(0, -1, 0)
	end := start.Add(elapsed)
	if end.After(current.From) {
		end = current.From
	}
	return Window{From: start, To: end}
}

// CurrentWeek returns the week-to-date window: 00:00 on Monday of the current
// week in the shop timezone up to now. The week starts on Monday, so a Sunday
// belongs to the week that began the preceding Monday (FR-009).
func CurrentWeek(now time.Time, location *time.Location) Window {
	local := now.In(location)
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	// Go's Weekday puts Sunday first; shift so Monday is offset 0.
	offset := (int(local.Weekday()) + 6) % 7
	return Window{From: dayStart.AddDate(0, 0, -offset), To: now}
}

// TrailingDays returns the trend window: the `days` calendar days ending today
// (inclusive) in the shop timezone, from 00:00 on the oldest day up to now. A
// non-positive length is treated as a single day (FR-019).
func TrailingDays(now time.Time, location *time.Location, days int) Window {
	if days < 1 {
		days = 1
	}
	local := now.In(location)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	return Window{From: today.AddDate(0, 0, -(days - 1)), To: now}
}

// Dates returns the calendar days the window covers in the shop timezone, oldest
// first, as YYYY-MM-DD. Both endpoint days are included, because To is an instant
// inside its day; a trend window therefore yields exactly the days it names, which
// is the zero-fill list the application iterates (FR-019, FR-020).
func (w Window) Dates() []string {
	location := w.From.Location()
	from := w.From.In(location)
	to := w.To.In(location)

	first := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, location)
	last := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, location)

	out := make([]string, 0)
	for day := first; !day.After(last); day = day.AddDate(0, 0, 1) {
		out = append(out, day.Format("2006-01-02"))
	}
	return out
}
