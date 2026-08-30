// weekend.go is the second deterministic pre-check that runs BEFORE any
// model call this package makes — daterange.go's sibling, built to the
// same pattern for the same reason.
//
// Why it exists: "the weekend" carries TWO independent unknowns, and this
// product's gate prompt only ever settled one of them.
//
//   - WHICH weekend. That is date resolution against dataEnd, and the
//     prompt's "Date grounding" section answers it: relative language is
//     resolved as an offset from the last date this product has data for.
//   - WHICH DAYS. Friday through Sunday, or Saturday and Sunday only?
//     Nothing in this product defines that. No tool defines it, no
//     convention in the data defines it, and the two readings produce
//     genuinely different numbers.
//
// Those two rules are logically compatible — anchoring the week is
// necessary, and it is not sufficient — but the prompt's own phrasing
// ("a vague date range like 'the weekend' without a clear anchor once
// resolved per the date-grounding rule above") reads, to a model, as if
// anchoring DISPOSES of the ambiguity. The live smoke test for exactly
// this question (TestGate_Classify_LiveSmokeTest's "ambiguous" case)
// flaked accordingly: sometimes ambiguous, sometimes answerable, on
// identical input. The prompt wording is now explicit that anchoring is
// necessary but not sufficient (see systemPromptTemplate) — and this file
// is the part that does not depend on the model reading it correctly.
//
// Which days count as a weekend is a DEFINITION the product does not have,
// not a judgment call: exactly the kind of thing Constitution Principle I
// says must not be delegated to a probabilistic step, and exactly what
// CLAUDE.md names as the canonical ambiguity example ("how was the
// weekend" — does it include Friday?). So it is decided here, in Go, with
// zero model calls and a clarifying question composed from constants.
//
// What it deliberately does NOT do — the lexicon is one word, on purpose:
//
//   - It only fires on a standalone "weekend". A campaign id that happens
//     to contain the word (IFOOD-CAMP-WEEKEND, a real campaign in this
//     dataset, and a graded refusal-suite question) is NOT a vague period
//     and must keep going straight to the tools. Go's \b would match
//     inside it, so boundaries are checked manually, the same way
//     daterange.go's bareYearBoundariesOK rejects "$2025" and "v2.2026".
//     The plural "weekends" ("how do weekends compare to weekdays?") is
//     excluded by the same rule.
//   - It does not fire when the question already pins the days down by
//     naming them ("the weekend — Friday through Sunday?"), nor when it
//     carries an explicit date daterange.go can parse ("the weekend of
//     August 9, 2025"), which is a fully specified reference.
//   - It does not fire on a reply to a clarifying question (see Classify),
//     which would loop the owner straight back into the question they just
//     answered.
//   - It was deliberately not widened to "recently", "lately", or any
//     other vague-period word. "The weekend" has exactly two candidate
//     readings, which is what makes a deterministic clarifying question
//     and its options composable from constants at all. "Recently" has no
//     bounded set of readings, so a deterministic clarification for it
//     would have to invent the choices — which is guessing, in the one
//     place this file exists to stop guessing. That case stays with the
//     model, where a judgment call belongs.
package ambiguity

import "strings"

// weekendClarifyingQuestion and weekendClarifyingOptions are the fixed
// text this pre-check returns. Composed from constants, never from a
// model: the two readings ARE the message, exactly as daterange.go's
// precheckRefusalReason composes a refusal from the real dates and the
// real window.
//
// Owner-facing copy discipline, same as every other message this package
// and internal/explain put in front of a restaurant owner: plain language,
// no internal step names, and options that are complete replies the owner
// can send as-is (Decision.ClarifyingOptions' contract — each is re-posted
// and re-classified from scratch, never treated as established fact).
const weekendClarifyingQuestion = "Which days should I count as the weekend?"

var weekendClarifyingOptions = []string{
	"Friday through Sunday",
	"Saturday and Sunday only",
}

// WeekendPrecheckModelLabel is what this pre-check's clarification records
// as its "model used" — the counterpart to PrecheckModelLabel, and an
// honest sentinel for the same reason: no model ran, no tokens were spent,
// and instrumentation must say so rather than logging a zero-token call
// against a real model name.
const WeekendPrecheckModelLabel = "none (deterministic weekend pre-check)"

// weekdayNames are the day names that, if the question already uses one,
// mean the owner has already told us which days they mean — there is
// nothing left for this pre-check to ask about. Matched as standalone
// words (see mentionsStandaloneWord), so "sat" never fires on "satisfied".
var weekdayNames = []string{
	"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday",
	"mon", "tue", "tues", "wed", "thu", "thur", "thurs", "fri", "sat", "sun",
}

// needsWeekendClarification reports whether question refers to "the
// weekend" in a way this product genuinely cannot resolve. Pure and
// deterministic: same input, same answer, no clock, no model.
func needsWeekendClarification(question string) bool {
	lower := strings.ToLower(question)

	if !mentionsStandaloneWord(lower, "weekend") {
		return false
	}
	// Already pinned down by naming the days.
	for _, name := range weekdayNames {
		if mentionsStandaloneWord(lower, name) {
			return false
		}
	}
	// Already pinned down by an explicit, parseable date reference — the
	// same parser daterange.go uses, so "the weekend of August 9, 2025"
	// is treated as the fully specified reference it is rather than being
	// forced into a clarification.
	if len(findExplicitDateMentions(question)) > 0 {
		return false
	}
	return true
}

// mentionsStandaloneWord reports whether word appears in lower (already
// lowercased) as its own word, not glued into an identifier. RE2 has no
// lookarounds and Go's \b treats "-" as a boundary, so "IFOOD-CAMP-WEEKEND"
// would match a naive \bweekend\b — the exact false positive this function
// exists to prevent (that campaign is a graded question in
// evaluation/promptfoo/refusal.yaml and must reach the tools, not a
// clarifying question). Boundaries are therefore checked by hand, the same
// way daterange.go's bareYearBoundariesOK does.
func mentionsStandaloneWord(lower, word string) bool {
	for i := 0; ; {
		idx := strings.Index(lower[i:], word)
		if idx < 0 {
			return false
		}
		start := i + idx
		end := start + len(word)
		if wordBoundariesOK(lower, start, end) {
			return true
		}
		i = start + 1
	}
}

// wordBoundariesOK rejects a match glued to characters that make it part
// of something else: a letter or digit on either side (WEEKEND01,
// weekends, satisfied), or an id separator (IFOOD-CAMP-WEEKEND,
// weekend_total).
func wordBoundariesOK(s string, start, end int) bool {
	if start > 0 && !isWordSeparator(s[start-1]) {
		return false
	}
	if end < len(s) && !isWordSeparator(s[end]) {
		return false
	}
	return true
}

func isWordSeparator(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return false
	case c == '-' || c == '_':
		return false
	default:
		return true
	}
}
