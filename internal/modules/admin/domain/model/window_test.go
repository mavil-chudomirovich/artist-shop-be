package model

import (
	"testing"
	"time"
)

// This file pins the shop-timezone window helpers: the current month-to-date, the
// fair previous-month same-period window and its clamp to the previous month's
// end, the Monday week and the trailing trend window with its zero-fill date list
// (FR-007, FR-008, FR-009, FR-019, research D4).

func shopLocation(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatalf("load the shop timezone: %v", err)
	}
	return loc
}

func assertTime(t *testing.T, name string, got, want time.Time) {
	t.Helper()
	if !got.Equal(want) {
		t.Fatalf("%s = %s, want %s", name, got, want)
	}
}

// FR-008: currentMonth starts at 00:00 on the first day of the current month in
// the shop timezone and ends at the request instant.
func TestCurrentMonthStartsAtTheLocalFirstDay(t *testing.T) {
	loc := shopLocation(t)
	now := time.Date(2026, time.October, 17, 4, 0, 0, 0, time.UTC) // 11:00 local

	got := CurrentMonth(now, loc)

	assertTime(t, "from", got.From, time.Date(2026, time.October, 1, 0, 0, 0, 0, loc))
	assertTime(t, "to", got.To, now)
}

// FR-008, US2 acceptance 2: the previous window keeps the current elapsed
// duration, so a dashboard opened on the 17th compares 17 days with 17 days.
func TestPreviousMonthSamePeriodKeepsTheElapsedDuration(t *testing.T) {
	loc := shopLocation(t)
	now := time.Date(2026, time.October, 17, 4, 0, 0, 0, time.UTC)

	got := PreviousMonthSamePeriod(now, loc)

	start := time.Date(2026, time.September, 1, 0, 0, 0, 0, loc)
	assertTime(t, "from", got.From, start)
	elapsed := now.Sub(time.Date(2026, time.October, 1, 0, 0, 0, 0, loc))
	assertTime(t, "to", got.To, start.Add(elapsed))
}

// FR-008: when the elapsed duration would run past the previous month's end the
// window is clamped to the current month's first day. March has 31 days and
// February 28, so the 31st of March must clamp.
func TestPreviousMonthSamePeriodClampsAtThePreviousMonthEnd(t *testing.T) {
	loc := shopLocation(t)
	now := time.Date(2026, time.March, 31, 12, 0, 0, 0, loc)

	got := PreviousMonthSamePeriod(now, loc)

	assertTime(t, "from", got.From, time.Date(2026, time.February, 1, 0, 0, 0, 0, loc))
	assertTime(t, "to", got.To, time.Date(2026, time.March, 1, 0, 0, 0, 0, loc))
}

// FR-008: the clamp follows the real calendar, so a leap February (29 days) is
// still clamped one day earlier than a 31-day March would be.
func TestPreviousMonthSamePeriodHandlesALeapFebruary(t *testing.T) {
	loc := shopLocation(t)
	now := time.Date(2028, time.March, 30, 12, 0, 0, 0, loc)

	got := PreviousMonthSamePeriod(now, loc)

	assertTime(t, "from", got.From, time.Date(2028, time.February, 1, 0, 0, 0, 0, loc))
	assertTime(t, "to", got.To, time.Date(2028, time.March, 1, 0, 0, 0, 0, loc))
}

// FR-009: the week starts on Monday 00:00 local, so a Saturday and the following
// Sunday share the same Monday-to-now window.
func TestCurrentWeekStartsMonday(t *testing.T) {
	loc := shopLocation(t)
	// 2026-10-17 is a Saturday.
	now := time.Date(2026, time.October, 17, 4, 0, 0, 0, time.UTC)

	got := CurrentWeek(now, loc)

	monday := time.Date(2026, time.October, 12, 0, 0, 0, 0, loc)
	assertTime(t, "from", got.From, monday)
	if got.From.Weekday() != time.Monday {
		t.Fatalf("the week must start on Monday, got %s", got.From.Weekday())
	}
	assertTime(t, "to", got.To, now)

	sunday := time.Date(2026, time.October, 18, 4, 0, 0, 0, time.UTC)
	assertTime(t, "sunday from", CurrentWeek(sunday, loc).From, monday)
}

// FR-019, FR-020: the trend window is the trailing `days` calendar days ending
// today (inclusive); Dates() enumerates them oldest first as YYYY-MM-DD, exactly
// `days` of them, which is the zero-fill list the application iterates.
func TestTrailingDaysEndsTodayAndListsEveryDay(t *testing.T) {
	loc := shopLocation(t)
	now := time.Date(2026, time.October, 17, 4, 0, 0, 0, time.UTC) // 11:00 local, Oct 17

	for _, days := range []int{7, 30} {
		got := TrailingDays(now, loc, days)

		today := time.Date(2026, time.October, 17, 0, 0, 0, 0, loc)
		assertTime(t, "from", got.From, today.AddDate(0, 0, -(days-1)))
		assertTime(t, "to", got.To, now)

		dates := got.Dates()
		if len(dates) != days {
			t.Fatalf("days=%d: expected %d dates, got %d (%v)", days, days, len(dates), dates)
		}
		if dates[0] != today.AddDate(0, 0, -(days-1)).Format("2006-01-02") {
			t.Fatalf("days=%d: first date = %q, want the oldest day", days, dates[0])
		}
		if dates[len(dates)-1] != "2026-10-17" {
			t.Fatalf("days=%d: last date = %q, want today", days, dates[len(dates)-1])
		}
		for i := 1; i < len(dates); i++ {
			if dates[i-1] >= dates[i] {
				t.Fatalf("days=%d: dates must be oldest first, got %v", days, dates)
			}
		}
	}
}

// The clamp boundary itself: when the elapsed duration is exactly the previous
// month's length (28 days in a non-leap February) the window ends exactly at the
// current month's first day, with no extra day added.
func TestPreviousMonthSamePeriodAtTheExactMonthLength(t *testing.T) {
	loc := shopLocation(t)
	now := time.Date(2026, time.March, 29, 0, 0, 0, 0, loc)

	got := PreviousMonthSamePeriod(now, loc)

	assertTime(t, "from", got.From, time.Date(2026, time.February, 1, 0, 0, 0, 0, loc))
	assertTime(t, "to", got.To, time.Date(2026, time.March, 1, 0, 0, 0, 0, loc))
}
