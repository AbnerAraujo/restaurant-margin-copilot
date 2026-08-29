package httpapi

import "time"

// derivePriorPeriod computes the immediately preceding period of the same
// real-world length as [start, end] — spec 008 FR-004's "Compare to last
// period". This is calendar-aware, not a fixed-day shift: a full calendar
// month's prior period is the prior CALENDAR month (28-31 days, whatever
// that month actually has), and a full calendar year's prior period is the
// prior CALENDAR year (accounting for leap years) — never a naive "shift
// back by (end-start) days", which would drift a monthly comparison onto
// the wrong day-of-month within a few periods.
//
// Any other range (a week, a custom span) falls back to the one rule that
// is always correct regardless of calendar alignment: the same number of
// days, immediately before start, with no gap and no overlap.
func derivePriorPeriod(start, end time.Time) (time.Time, time.Time) {
	if isFullCalendarMonth(start, end) {
		priorMonthEnd := start.AddDate(0, 0, -1)
		priorMonthStart := time.Date(priorMonthEnd.Year(), priorMonthEnd.Month(), 1, 0, 0, 0, 0, time.UTC)
		return priorMonthStart, priorMonthEnd
	}
	if isFullCalendarYear(start, end) {
		priorYear := start.Year() - 1
		return time.Date(priorYear, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(priorYear, time.December, 31, 0, 0, 0, 0, time.UTC)
	}

	days := int(end.Sub(start).Hours()/24) + 1
	priorEnd := start.AddDate(0, 0, -1)
	priorStart := priorEnd.AddDate(0, 0, -(days - 1))
	return priorStart, priorEnd
}

// isFullCalendarMonth reports whether [start, end] is exactly one calendar
// month: the 1st through the real last day of that same month (28-31,
// whatever that month has).
func isFullCalendarMonth(start, end time.Time) bool {
	if start.Day() != 1 || start.Year() != end.Year() || start.Month() != end.Month() {
		return false
	}
	lastDay := time.Date(start.Year(), start.Month()+1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
	return end.Year() == lastDay.Year() && end.Month() == lastDay.Month() && end.Day() == lastDay.Day()
}

// isFullCalendarYear reports whether [start, end] is exactly one calendar
// year: January 1st through December 31st of the same year.
func isFullCalendarYear(start, end time.Time) bool {
	return start.Month() == time.January && start.Day() == 1 &&
		end.Month() == time.December && end.Day() == 31 &&
		start.Year() == end.Year()
}
