package ingest

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// parseDate parses a date string from a delivery, POS, or cost-sheet
// export. Real export tools disagree on date format — this fixture set
// alone has ISO (YYYY-MM-DD) from the delivery platform and DD/MM/YYYY from
// the in-house POS (fixtures/README.md irregularity #4: "not a one-off
// typo — a systematic difference between the two export systems") — so
// this tries the unambiguous ISO form first, then resolves slash-separated
// dates by using whichever part is out of range for a month (>12) to
// determine which part is the day. When both parts could be either (e.g.
// "01/08/2026"), it defaults to DD/MM/YYYY, matching this fixture set's POS
// convention and the international convention outside the US — a
// documented assumption, not a guess hidden in the code.
//
// A date string outside these forms is a parse error, never a silent
// misparse: Constitution Principle II says to refuse rather than guess, and
// a wrong date silently shifting an order to another day would be exactly
// the kind of confidently-wrong result that principle exists to prevent.
func parseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("ingest: empty date")
	}

	for _, layout := range []string{"2006-01-02", "2006/01/02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}

	if t, ok := parseSlashDate(s); ok {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("ingest: unrecognized date format %q (expected YYYY-MM-DD or DD/MM/YYYY)", s)
}

func parseSlashDate(s string) (time.Time, bool) {
	parts := strings.Split(s, "/")
	if len(parts) != 3 || len(parts[2]) != 4 {
		return time.Time{}, false
	}
	a, errA := strconv.Atoi(parts[0])
	b, errB := strconv.Atoi(parts[1])
	year, errY := strconv.Atoi(parts[2])
	if errA != nil || errB != nil || errY != nil {
		return time.Time{}, false
	}

	tryDate := func(month, day int) (time.Time, bool) {
		if month < 1 || month > 12 || day < 1 || day > 31 {
			return time.Time{}, false
		}
		t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
		if int(t.Month()) != month || t.Day() != day {
			return time.Time{}, false // e.g. day 31 in a 30-day month
		}
		return t, true
	}

	switch {
	case a > 12 && b <= 12:
		// a can't be a month, so a is the day: DD/MM/YYYY.
		return tryDate(b, a)
	case b > 12 && a <= 12:
		// b can't be a month, so a is the month: MM/DD/YYYY.
		return tryDate(a, b)
	default:
		// Genuinely ambiguous (both <= 12) — default to DD/MM/YYYY per the
		// documented assumption above.
		return tryDate(b, a)
	}
}
