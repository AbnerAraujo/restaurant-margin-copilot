package ingest

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// gatherDateStrings collects every non-blank raw value from the given date
// columns across dataRows (a file's rows, header already excluded), for
// per-file format detection (see dateFormatResolver). cols may include -1
// (an absent optional column, e.g. a delivery export with no refund_date
// column), which is skipped.
func gatherDateStrings(dataRows [][]string, cols ...int) []string {
	var out []string
	for _, row := range dataRows {
		if isBlankRow(row) {
			continue
		}
		for _, c := range cols {
			if c < 0 {
				continue
			}
			if v := get(row, c); v != "" {
				out = append(out, v)
			}
		}
	}
	return out
}

// dateFormat is which convention a file's ambiguous slash-separated dates
// (both parts <= 12) should be read as, once established from that file's
// own unambiguous rows.
type dateFormat int

const (
	dateFormatUnestablished dateFormat = iota
	dateFormatDMY                      // DD/MM/YYYY
	dateFormatMDY                      // MM/DD/YYYY
)

func (f dateFormat) String() string {
	switch f {
	case dateFormatDMY:
		return "DD/MM/YYYY"
	case dateFormatMDY:
		return "MM/DD/YYYY"
	default:
		return "unestablished"
	}
}

// dateFormatResolver parses date strings from a single ingested file,
// resolving ambiguous slash-separated dates (e.g. "01/08/2026", where both
// parts are <= 12) against a format established ONCE from that file's own
// unambiguous rows (any row where one part is > 12) — never per row.
//
// fixtures/README.md irregularity #4 documents the ambiguity as systematic
// per file ("not a one-off typo — a systematic difference between the two
// export systems"): pos_export.csv is DD/MM/YYYY throughout, the other
// fixture files are ISO throughout. Resolving format independently per row
// (the previous behavior) could let a single stray row using the other
// convention silently land on the wrong calendar day instead of being
// caught — the opposite of Constitution Principle II's refuse-rather-than
// -guess discipline. Establishing the format once per file, then rejecting
// any row that unambiguously contradicts it, is the correct application of
// that same principle: a disagreeing row is surfaced as an error, not
// silently reinterpreted.
type dateFormatResolver struct {
	format dateFormat
}

// newDateFormatResolver scans dateStrings (every raw value from a file's
// date column(s), across all rows) for the first unambiguous slash-date and
// establishes the file's format from it. If the file has no unambiguous
// slash-dates at all (e.g. it's pure ISO, or every ambiguous value is
// genuinely ambiguous), the resolver stays unestablished and falls back to
// this project's documented DD/MM default for any ambiguous value it later
// sees — the same default the previous per-row implementation used, so a
// file with no evidence either way behaves exactly as before.
func newDateFormatResolver(dateStrings []string) *dateFormatResolver {
	res := &dateFormatResolver{}
	for _, s := range dateStrings {
		if f, ok := detectUnambiguousSlashFormat(s); ok {
			res.format = f
			break
		}
	}
	return res
}

// detectUnambiguousSlashFormat reports the format a slash-separated date
// string unambiguously implies (one part > 12, so it can only be the day),
// or ok=false if s isn't a well-formed three-part slash date or is itself
// ambiguous (both parts <= 12).
func detectUnambiguousSlashFormat(s string) (dateFormat, bool) {
	a, b, _, ok := splitSlashDate(s)
	if !ok {
		return dateFormatUnestablished, false
	}
	switch {
	case a > 12 && b <= 12:
		return dateFormatDMY, true
	case b > 12 && a <= 12:
		return dateFormatMDY, true
	default:
		return dateFormatUnestablished, false
	}
}

// splitSlashDate parses "P1/P2/YYYY" into its three integer parts without
// judging which of P1/P2 is the day vs. the month.
func splitSlashDate(s string) (a, b, year int, ok bool) {
	parts := strings.Split(s, "/")
	if len(parts) != 3 || len(parts[2]) != 4 {
		return 0, 0, 0, false
	}
	var errA, errB, errY error
	a, errA = strconv.Atoi(parts[0])
	b, errB = strconv.Atoi(parts[1])
	year, errY = strconv.Atoi(parts[2])
	if errA != nil || errB != nil || errY != nil {
		return 0, 0, 0, false
	}
	return a, b, year, true
}

// parse parses a single date string using this resolver's established
// per-file format. It is the direct replacement for the old per-row
// parseDate function, now file-scoped via the resolver.
//
// A date string outside the recognized forms — or one whose unambiguous
// slash format contradicts the format already established for this file —
// is a parse error, never a silent misparse: Constitution Principle II says
// to refuse rather than guess, and a wrong date silently shifting an order
// to another day would be exactly the kind of confidently-wrong result that
// principle exists to prevent.
// Every error returned from here (and from parseSlashDate below) is always
// caught by a caller in ingest.go/promo.go that immediately re-wraps it as
// "ingest: <file> row <n>: <field>: %w" — so these messages carry no
// "ingest: " prefix of their own. They used to, which produced a
// double-prefixed "ingest: costs_bad.csv row 2: invoice_date: ingest:
// unrecognized date format..." once the outer wrap was applied.
func (res *dateFormatResolver) parse(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty date")
	}

	for _, layout := range []string{"2006-01-02", "2006/01/02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}

	if t, err := res.parseSlashDate(s); err == nil {
		return t, nil
	} else if err != errNotSlashDate {
		return time.Time{}, err
	}

	return time.Time{}, fmt.Errorf("unrecognized date format %q (expected YYYY-MM-DD or DD/MM/YYYY)", s)
}

// errNotSlashDate is a sentinel meaning "not shaped like a slash date at
// all" — distinct from a real parse/consistency error, so parse() can fall
// through to its generic "unrecognized format" message instead of
// surfacing an internal sentinel to the caller.
var errNotSlashDate = fmt.Errorf("not a slash-separated date")

func (res *dateFormatResolver) parseSlashDate(s string) (time.Time, error) {
	a, b, year, ok := splitSlashDate(s)
	if !ok {
		return time.Time{}, errNotSlashDate
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
		// a can't be a month, so a is the day: DD/MM/YYYY, unambiguous.
		if res.format != dateFormatUnestablished && res.format != dateFormatDMY {
			return time.Time{}, fmt.Errorf(
				"date %q is unambiguously DD/MM/YYYY, but this file's dates were established as %s from an earlier row (fixtures/README.md irregularity #4: one file uses one format throughout) — refusing rather than guessing which row is wrong",
				s, res.format)
		}
		t, ok := tryDate(b, a)
		if !ok {
			return time.Time{}, fmt.Errorf("invalid date %q", s)
		}
		return t, nil
	case b > 12 && a <= 12:
		// b can't be a month, so a is the month: MM/DD/YYYY, unambiguous.
		if res.format != dateFormatUnestablished && res.format != dateFormatMDY {
			return time.Time{}, fmt.Errorf(
				"date %q is unambiguously MM/DD/YYYY, but this file's dates were established as %s from an earlier row (fixtures/README.md irregularity #4: one file uses one format throughout) — refusing rather than guessing which row is wrong",
				s, res.format)
		}
		t, ok := tryDate(a, b)
		if !ok {
			return time.Time{}, fmt.Errorf("invalid date %q", s)
		}
		return t, nil
	default:
		// Genuinely ambiguous (both <= 12): use this file's established
		// format if one exists, otherwise fall back to the documented
		// DD/MM default (matching this fixture set's POS convention and the
		// international convention outside the US).
		format := res.format
		if format == dateFormatUnestablished {
			format = dateFormatDMY
		}
		var t time.Time
		var ok bool
		if format == dateFormatMDY {
			t, ok = tryDate(a, b)
		} else {
			t, ok = tryDate(b, a)
		}
		if !ok {
			return time.Time{}, fmt.Errorf("invalid date %q", s)
		}
		return t, nil
	}
}
