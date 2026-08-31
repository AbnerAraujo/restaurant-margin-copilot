// daterange.go is the deterministic date-range pre-check that runs BEFORE
// any model call this package makes.
//
// Why it exists: comparing an explicit, fully-specified date ("July 2026",
// "2026-08-05", "August 9, 2025", a bare "2023") against the data's known
// min/max coverage window is date ARITHMETIC — exactly the class of work
// Constitution Principle I says a probabilistic model must never be
// responsible for. For a while it was the model's job anyway: the gate's
// system prompt asked the model to "do the actual comparison explicitly and
// carefully", and once the live dataset grew to a multi-year span, Claude
// Haiku 4.5 was caught live getting that comparison wrong across a year
// boundary (refusing "What was our margin for July 2026?" inside a
// 2024-08-01..2026-08-14 window). The 2026-08-29 response was to swap the
// gate to Claude Sonnet 5 — which made the comparison succeed, but treated
// the symptom: a range-inclusion verdict on a parseable date should never
// have been delegated to ANY model, cheap or expensive. This file is the
// actual fix.
//
// What it does, and deliberately does not do:
//   - It parses a conservative, closed set of EXPLICIT date forms out of the
//     user's own question text (ISO dates, month-name+year with or without a
//     day, unambiguous numeric d/m/y|m/d/y|y/m/d, and a word-bounded bare
//     year). If every explicit reference it finds falls wholly outside the
//     known data window, the question is refused in Go — zero model calls,
//     zero tokens, a refusal reason composed deterministically from the real
//     facts.
//   - If it finds explicit references that ARE in range (or a mix), it does
//     not refuse; it hands the model each reference's already-computed
//     verdict as settled fact, so the model never re-derives range inclusion
//     for a date Go could parse.
//   - Everything genuinely linguistic stays the model's job: relative
//     phrasing ("last month", "the weekend", "recently"), year-less dates
//     ("August 3rd" — WHICH August is a resolution rule, applied against
//     dataEnd), pronouns, follow-up composition, and any exotic date form
//     this parser deliberately does not attempt ("the seventh month of
//     2026"). A form the parser cannot recognize with certainty is left
//     alone rather than guessed at — a false deterministic refusal would be
//     worse than one more model classification.
//
// Ambiguous numeric forms (14/08/2026 vs 08/14/2026) are handled by keeping
// EVERY calendar-valid reading: a mention is out of range only if all of its
// readings are. That keeps this check refusal-safe without pretending the
// format ambiguity doesn't exist.
package ambiguity

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// dateInterval is one candidate reading of a date mention, as an inclusive
// [Start, End] day range: a full date is a one-day interval, "July 2026" is
// that month, a bare "2023" is that whole year.
type dateInterval struct {
	Start time.Time
	End   time.Time
}

// dateMention is one explicit date reference found in question text, with
// every calendar-valid reading of it.
type dateMention struct {
	Text     string
	Readings []dateInterval
}

// mentionVerdict is the deterministic range verdict for one mention:
// InRange is true when ANY reading overlaps the data window.
type mentionVerdict struct {
	Text    string
	InRange bool
}

// dateRangeCheck is the outcome of the pre-check over one question.
type dateRangeCheck struct {
	Verdicts []mentionVerdict
	// AllOutOfRange is true when at least one explicit date mention was
	// found and every one of them falls wholly outside the data window —
	// the only case that authorizes a deterministic refusal.
	AllOutOfRange bool
}

var monthsByName = map[string]time.Month{
	"jan": time.January, "january": time.January,
	"feb": time.February, "february": time.February,
	"mar": time.March, "march": time.March,
	"apr": time.April, "april": time.April,
	"may": time.May,
	"jun": time.June, "june": time.June,
	"jul": time.July, "july": time.July,
	"aug": time.August, "august": time.August,
	"sep": time.September, "sept": time.September, "september": time.September,
	"oct": time.October, "october": time.October,
	"nov": time.November, "november": time.November,
	"dec": time.December, "december": time.December,
}

const monthNamePattern = `(?:jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|jun(?:e)?|jul(?:y)?|aug(?:ust)?|sep(?:t(?:ember)?)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)`

// The extraction passes, in priority order. Earlier passes consume their
// matched span so a later, looser pass (the bare-year pass especially)
// cannot re-match a fragment of an already-recognized date.
var (
	// 2026-08-05, 2026/08/05, 2026.08.05
	reISO = regexp.MustCompile(`\b(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})\b`)
	// August 9, 2025 · Aug 9th 2025 · August 9 of 2025
	reMonthDayYear = regexp.MustCompile(`(?i)\b(` + monthNamePattern + `)\.?\s+(\d{1,2})(?:st|nd|rd|th)?\s*(?:,|of)?\s*(\d{4})\b`)
	// 9 August 2025 · 9th of August, 2025
	reDayMonthYear = regexp.MustCompile(`(?i)\b(\d{1,2})(?:st|nd|rd|th)?\s+(?:of\s+)?(` + monthNamePattern + `)\.?,?\s+(\d{4})\b`)
	// July 2026 · July, 2026 · July of 2026
	reMonthYear = regexp.MustCompile(`(?i)\b(` + monthNamePattern + `)\.?\s*(?:,|of)?\s*(\d{4})\b`)
	// 14/08/2026 · 08/14/2026 · 14.08.2026 — day-first and month-first are
	// both kept when both are calendar-valid.
	reNumericDMY = regexp.MustCompile(`\b(\d{1,2})[/.](\d{1,2})[/.](\d{4})\b`)
	// A bare calendar year. Word boundaries are checked manually (RE2 has no
	// lookarounds) so "#2024", "$2025", or "v2.2026" never match.
	reBareYear = regexp.MustCompile(`(?:19|20)\d{2}`)
)

// findExplicitDateMentions extracts every explicit, fully-specified date
// reference from question. Pure and deterministic: same input, same output,
// no clock, no model.
func findExplicitDateMentions(question string) []dateMention {
	consumed := make([]bool, len(question))
	var mentions []dateMention

	claim := func(start, end int) bool {
		for i := start; i < end; i++ {
			if consumed[i] {
				return false
			}
		}
		for i := start; i < end; i++ {
			consumed[i] = true
		}
		return true
	}

	addMention := func(start, end int, readings []dateInterval) {
		if len(readings) == 0 || !claim(start, end) {
			return
		}
		mentions = append(mentions, dateMention{Text: question[start:end], Readings: readings})
	}

	// Pass 1: ISO-ordered numeric dates (year first).
	for _, m := range reISO.FindAllStringSubmatchIndex(question, -1) {
		y, mo, d := atoi(question[m[2]:m[3]]), atoi(question[m[4]:m[5]]), atoi(question[m[6]:m[7]])
		if iv, ok := dayInterval(y, mo, d); ok {
			addMention(m[0], m[1], []dateInterval{iv})
		}
	}

	// Pass 2: month-name forms with a day.
	for _, re := range []*regexp.Regexp{reMonthDayYear, reDayMonthYear} {
		dayFirst := re == reDayMonthYear
		for _, m := range re.FindAllStringSubmatchIndex(question, -1) {
			a, b := question[m[2]:m[3]], question[m[4]:m[5]]
			year := atoi(question[m[6]:m[7]])
			var monthName string
			var day int
			if dayFirst {
				day, monthName = atoi(a), b
			} else {
				monthName, day = a, atoi(b)
			}
			month, ok := monthsByName[strings.ToLower(monthName)]
			if !ok {
				continue
			}
			if iv, ok := dayInterval(year, int(month), day); ok {
				addMention(m[0], m[1], []dateInterval{iv})
			}
		}
	}

	// Pass 3: month-name + year (whole-month interval).
	for _, m := range reMonthYear.FindAllStringSubmatchIndex(question, -1) {
		month, ok := monthsByName[strings.ToLower(question[m[2]:m[3]])]
		if !ok {
			continue
		}
		year := atoi(question[m[4]:m[5]])
		addMention(m[0], m[1], []dateInterval{monthInterval(year, month)})
	}

	// Pass 4: numeric day/month/year — keep every calendar-valid reading.
	for _, m := range reNumericDMY.FindAllStringSubmatchIndex(question, -1) {
		first, second, year := atoi(question[m[2]:m[3]]), atoi(question[m[4]:m[5]]), atoi(question[m[6]:m[7]])
		var readings []dateInterval
		if iv, ok := dayInterval(year, second, first); ok { // day-first
			readings = append(readings, iv)
		}
		if first != second {
			if iv, ok := dayInterval(year, first, second); ok { // month-first
				readings = append(readings, iv)
			}
		}
		addMention(m[0], m[1], readings)
	}

	// Pass 5: bare years, with manual word-boundary checks.
	for _, m := range reBareYear.FindAllStringIndex(question, -1) {
		if !bareYearBoundariesOK(question, m[0], m[1]) {
			continue
		}
		year := atoi(question[m[0]:m[1]])
		addMention(m[0], m[1], []dateInterval{{
			Start: time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(year, time.December, 31, 0, 0, 0, 0, time.UTC),
		}})
	}

	return mentions
}

// bareYearBoundariesOK rejects a 4-digit match that is glued to characters
// making it something other than a standalone calendar year: more digits
// (200008), an id/currency sigil (#2024, $2025), a decimal or version dot
// (v2.2026), or a date separator this parser already handles elsewhere.
func bareYearBoundariesOK(s string, start, end int) bool {
	if start > 0 {
		switch prev := s[start-1]; {
		case prev >= '0' && prev <= '9':
			return false
		case prev >= 'a' && prev <= 'z', prev >= 'A' && prev <= 'Z':
			return false
		case prev == '#' || prev == '$' || prev == '.' || prev == '-' || prev == '/' || prev == '_':
			return false
		}
	}
	if end < len(s) {
		switch next := s[end]; {
		case next >= '0' && next <= '9':
			return false
		case next >= 'a' && next <= 'z', next >= 'A' && next <= 'Z':
			return false
		case next == '-' || next == '/' || next == '_':
			return false
		}
	}
	return true
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// dayInterval builds a one-day interval, rejecting calendar-invalid
// combinations (February 30th, month 14) by round-tripping through
// time.Date's normalization and refusing anything that moved.
func dayInterval(year, month, day int) (dateInterval, bool) {
	if year < 1000 || year > 9999 || month < 1 || month > 12 || day < 1 || day > 31 {
		return dateInterval{}, false
	}
	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if t.Year() != year || t.Month() != time.Month(month) || t.Day() != day {
		return dateInterval{}, false
	}
	return dateInterval{Start: t, End: t}, true
}

func monthInterval(year int, month time.Month) dateInterval {
	start := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	return dateInterval{Start: start, End: start.AddDate(0, 1, -1)}
}

// checkExplicitDateRange runs the full pre-check: parse explicit date
// mentions out of question and compare each against the inclusive
// [dataStart, dataEnd] window (YYYY-MM-DD, internal/storage
// .LoadDataDateRange's format). Returns nil when there is nothing for the
// pre-check to decide — no parseable mentions, or an unparseable window
// (in which case the model path proceeds exactly as before; degrading to
// the old behavior is safer than refusing on a malformed bound).
func checkExplicitDateRange(question, dataStart, dataEnd string) *dateRangeCheck {
	windowStart, err1 := time.Parse("2006-01-02", dataStart)
	windowEnd, err2 := time.Parse("2006-01-02", dataEnd)
	if err1 != nil || err2 != nil || windowEnd.Before(windowStart) {
		return nil
	}

	mentions := findExplicitDateMentions(question)
	if len(mentions) == 0 {
		return nil
	}

	check := &dateRangeCheck{AllOutOfRange: true}
	for _, mention := range mentions {
		inRange := false
		for _, r := range mention.Readings {
			// Inclusive-interval overlap: the mention touches the window
			// unless it ends before the window starts or starts after it
			// ends.
			if !r.End.Before(windowStart) && !r.Start.After(windowEnd) {
				inRange = true
				break
			}
		}
		check.Verdicts = append(check.Verdicts, mentionVerdict{Text: mention.Text, InRange: inRange})
		if inRange {
			check.AllOutOfRange = false
		}
	}
	return check
}

// precheckRefusalReason composes the deterministic refusal text for an
// all-out-of-range question. Plain Go string assembly over already-known
// facts — the exact discipline Principle I asks for, and why this refusal
// needs no writer-pass polish: the specific dates and the real window ARE
// the message.
func precheckRefusalReason(verdicts []mentionVerdict, dataStart, dataEnd string) string {
	quoted := make([]string, 0, len(verdicts))
	for _, v := range verdicts {
		quoted = append(quoted, fmt.Sprintf("%q", v.Text))
	}
	var listed string
	switch len(quoted) {
	case 1:
		listed = quoted[0] + " falls"
	case 2:
		listed = quoted[0] + " and " + quoted[1] + " both fall"
	default:
		listed = strings.Join(quoted[:len(quoted)-1], ", ") + ", and " + quoted[len(quoted)-1] + " all fall"
	}
	return fmt.Sprintf(
		"This product has reconciled data for %s through %s only — %s entirely outside that range, so there is no data to answer this from. Ask about a date inside that window and I can give you exact, reconciled numbers.",
		dataStart, dataEnd, listed,
	)
}

// precheckFactNote renders the in-range (or mixed) verdicts as a block
// appended to the text the gate's model call classifies: each explicit
// date's range verdict arrives as an already-computed fact, so the model
// never performs the comparison itself for any date Go could parse.
func precheckFactNote(verdicts []mentionVerdict, dataStart, dataEnd string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[Deterministic date-range check — computed in Go, not by you] The data window is %s through %s inclusive. Each explicit date reference below has ALREADY been compared against that window deterministically. Treat these verdicts as settled fact — never re-derive, second-guess, or contradict them:\n", dataStart, dataEnd)
	for _, v := range verdicts {
		verdict := "IN RANGE"
		if !v.InRange {
			verdict = "OUT OF RANGE"
		}
		fmt.Fprintf(&b, "- %q: %s\n", v.Text, verdict)
	}
	b.WriteString("Never classify this question as unanswerable on date-range grounds for a reference marked IN RANGE, and never treat a reference marked OUT OF RANGE as having data.")
	return b.String()
}

// ExplicitDatesConflict reports whether a and b each name at least one
// explicit, fully-specified date or period, and NONE of what a names agrees
// with anything b names — e.g. "August 2026" versus "July 2026". It exists
// as a deterministic guardrail against the paraphrase-match cache's model
// classifier confidently matching two questions that ask about different
// periods (found live: "margin total for August 2026" paraphrase-matched
// to a cached "average daily input cost in July 2026" answer, served as
// answered — see httpapi.serveFromParaphraseMatch). A real semantic-match
// bug in a model call needs a deterministic backstop the same way
// arithmetic does; this is that backstop, scoped to what Go can actually
// verify without a second model call.
//
// Returns false whenever EITHER string has no extractable explicit date —
// this is a narrow, targeted check for the one failure mode it exists to
// catch (two named, calendar-comparable periods that share no time at all),
// not a general "these two questions differ" detector. A question with no
// explicit date at all ("how did last week compare to the week before")
// is unaffected and falls through to trusting the classifier as before.
//
// Compatibility is OVERLAP, not exact equality: "August 1, 2026" (a
// day-level reading) must NOT conflict with a cached "August 2026"
// (month-level) question, since the day sits inside that month — only
// genuinely disjoint periods (August vs July) count as a conflict.
func ExplicitDatesConflict(a, b string) bool {
	mentionsA := findExplicitDateMentions(a)
	mentionsB := findExplicitDateMentions(b)
	if len(mentionsA) == 0 || len(mentionsB) == 0 {
		return false
	}
	for _, ma := range mentionsA {
		for _, mb := range mentionsB {
			for _, ra := range ma.Readings {
				for _, rb := range mb.Readings {
					// dateInterval bounds are inclusive on both ends (see
					// dayInterval/monthInterval above), so overlap is
					// "each starts no later than the other ends".
					if !ra.Start.After(rb.End) && !rb.Start.After(ra.End) {
						return false // at least one reading overlaps -> compatible
					}
				}
			}
		}
	}
	return true // both named explicit periods, and none of them overlap
}
