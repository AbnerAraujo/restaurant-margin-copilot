// Package answerverify is the deterministic, post-narration check that
// every money figure a narrated answer STATES actually traces back to a
// number the typed MCP tools RETURNED.
//
// Why it exists: Constitution Principle I says the model never produces a
// number — it narrates one the Go layer computed. Until this package,
// nothing enforced that at the sentence level. internal/explain's system
// prompt asks for it, and internal/explain's one structural guard
// (looksLikeCurrencyAmount) catches only the crudest violation: a
// currency-shaped answer produced with ZERO tool calls. A model that calls
// get_daily_summary correctly, receives $374.75, and then narrates $999.99
// — or, far more insidiously, $374.57 — sailed straight through. That is
// exactly the "confidently wrong margin figure" CLAUDE.md names as worse
// than a refusal.
//
// # What this package is, and deliberately is not
//
// It is a MATCHER, not a calculator. It never computes a business figure,
// never decides what a day's margin should be, and never rewrites an
// answer. It extracts the money and percentage figures a narration states,
// builds the set of values the tool results actually contain, and reports
// which stated figures have no counterpart. internal/explain turns a
// non-empty report into a refusal.
//
// It is pure: no clock, no database, no model, no I/O. Same answer text
// plus same tool JSON in, same report out.
//
// # The calibration problem, and how the policy answers it
//
// The whole risk of a check like this is the FALSE refusal: killing an
// answer that was right, because the matcher was stricter than the
// language. This policy was not designed against hypotheticals — it was
// designed against the 35 real narrations the evaluation harness produced
// (evaluation/promptfoo/{accuracy,consistency,refusal}.yaml, run live
// against the seeded dataset), which show three narration habits any
// workable policy has to survive:
//
//  1. Rounded restatement. "it cost $610 and only drove about $159 in
//     extra sales" — where the tool returned "610.00" and "159.25"; and
//     "a net loss of roughly $451" against a tool value of "450.75".
//     A cents-exact matcher refuses all three, and every one of them is
//     a correct, honest answer.
//     -> Answer: a figure is verified AT THE PRECISION IT WAS STATED.
//     "$159" is a claim about dollars, not cents, and is satisfied by any
//     tool value within one dollar of it; "$374.75" is a claim about
//     cents and is satisfied only by 374.75 exactly. A fabricated figure
//     still has to land within a dollar of a real one to pass, and a
//     subtly altered full-precision figure ($374.57 for $374.75) still
//     fails outright — which is the case that matters.
//
//  2. Sign carried by words, not symbols. "a net loss of $450.75" states
//     a value the tool returned as "-450.75".
//     -> Answer: matching is on ABSOLUTE value. Direction is prose ("lost
//     money", "down from"), and this package does not grade prose.
//
//  3. One-step comparisons and per-unit figures the tools never emit.
//     Three of these were caught live, each one refusing an answer whose
//     headline figure was completely correct:
//     "iFood brought in $187.25 ... with Just Eat Takeaway at $180.50 —
//     iFood was $6.75 ahead" (a subtraction);
//     "$610 in spend across 4 orders — about $153 an order" (a division);
//     "for every $1 spent it brought back 26 cents" (a rhetorical unit,
//     which is 610.00/610.00 and grounded in the most literal way there
//     is).
//     -> Answer: the allowed money set includes every SUM, DIFFERENCE and
//     QUOTIENT of two tool-returned values, recomputed HERE, in Go. The
//     invariant this package actually defends is "every figure the owner
//     reads is reproducible from the deterministic layer" — a figure Go
//     can rederive in one operation satisfies that, and the model's
//     arithmetic is checked against Go's rather than trusted.
//     Deliberately ONE step, over the values the tools RETURNED and never
//     over each other's output: a figure needing two or more combinations
//     is not cheaply reproducible and is not admitted.
//     What this costs is measured, not assumed. Replaying the evaluation
//     corpus with each answer's headline figure deliberately corrupted,
//     the check still catches 29 of 29 cent-level alterations and 29 of
//     29 wholesale fabrications; a $5.00 alteration slips past in 1 of 29.
//     The cent-level case is the one that matters, because a figure this
//     product states is stated to the cent.
//
//  4. Percentages the tools never emit. "iFood brought in $196.50 of the
//     total $374.75 — that's about 52% of your delivery revenue." No tool
//     returns 52%; the model divided.
//     -> Same answer, one operator over: the allowed percentage set
//     includes every ratio of two tool-returned money values, recomputed
//     in Go. This is honestly a weaker check than the currency one — with
//     N values there are up to N*(N-1) admissible ratios — and it is
//     scoped that way on purpose: percentages in this product are
//     context, money figures are the product.
//
// Both derivations are computed over the values the tools RETURNED, never
// over each other's output, so the admissible set stays bounded and one
// step wide. See maxValuesForDerivation.
//
// Dates are extracted and reported too, but ADVISORY ONLY — never a
// reason to refuse. Legitimate date phrasing varies far more than money
// phrasing ("Aug 1-14, 2024", "the 2nd of August"), and a refusal on a
// date variance would cost real answers to catch a class of error that
// provenance already surfaces.
package answerverify

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Kind is which family of figure a Mismatch concerns.
type Kind string

const (
	// KindCurrency is a money figure. Mismatches here are BLOCKING.
	KindCurrency Kind = "currency"
	// KindPercent is a percentage figure. Mismatches here are BLOCKING.
	KindPercent Kind = "percent"
	// KindDate is a fully-specified calendar date. Mismatches here are
	// ADVISORY — reported and logged, never refused on (see package doc).
	KindDate Kind = "date"
)

// Mismatch is one figure a narration stated that the tool results do not
// support. Text is the figure exactly as it appeared in the answer, so a
// log line or a test failure names something a human can find by eye.
type Mismatch struct {
	Kind Kind
	Text string
}

func (m Mismatch) String() string { return string(m.Kind) + " " + strconv.Quote(m.Text) }

// Report is the outcome of one verification.
type Report struct {
	// Currency and Percent are the blocking mismatches.
	Currency []Mismatch
	Percent  []Mismatch
	// Dates is advisory only (see package doc).
	Dates []Mismatch

	// CheckedCurrency/CheckedPercent/CheckedDates are how many figures of
	// each kind were extracted and actually compared. Reported so a caller
	// (or a test) can tell "verified clean" from "there was nothing to
	// verify" — two states that must never be confused, the same
	// distinction internal/httpapi's CacheInfo draws between "zero cost
	// because nothing ran" and "zero cost because measurement failed".
	CheckedCurrency int
	CheckedPercent  int
	CheckedDates    int

	// PercentSkipped records that percentage checking was deliberately
	// skipped for this answer because the tool results carried more money
	// values than maxValuesForDerivation — deriving admissible ratios over
	// a set that large is both slow and so permissive it proves nothing.
	// Skipping is the conservative choice: it can only ever fail to refuse,
	// never refuse wrongly.
	PercentSkipped bool
}

// Blocking reports whether this answer must be refused rather than served.
func (r Report) Blocking() bool { return len(r.Currency) > 0 || len(r.Percent) > 0 }

// BlockingMismatches is Currency followed by Percent, for logging.
func (r Report) BlockingMismatches() []Mismatch {
	out := make([]Mismatch, 0, len(r.Currency)+len(r.Percent))
	out = append(out, r.Currency...)
	out = append(out, r.Percent...)
	return out
}

// Summary renders the blocking mismatches as one log-safe line.
func (r Report) Summary() string {
	parts := make([]string, 0, len(r.Currency)+len(r.Percent))
	for _, m := range r.BlockingMismatches() {
		parts = append(parts, m.String())
	}
	return strings.Join(parts, ", ")
}

// RefusalReason is the owner-facing text internal/explain serves when a
// narration states a figure the tool results do not support.
//
// It is deliberately written in the same plain, first-person, no-jargon
// voice as the existing refusal copy in internal/explain and
// internal/ambiguity: no internal step names ("validator", "tool call",
// "provenance", "the deterministic layer"), no apology padding, and a
// concrete next step. A restaurant owner reads this sentence in a chat
// bubble; it has to make sense to them, not to this package.
const RefusalReason = "One of the figures in that answer didn't line up with the reconciled numbers behind it, so I've held it back rather than show you something that might be off by a little — a wrong number is worse than no answer. Ask me again, or point me at one specific day or period, and I'll rebuild it from that day's actual records."

// maxValuesForDerivation caps how many distinct money values a set of tool
// results may carry before the O(n^2) sum/difference and ratio derivations
// are skipped. get_expense_pattern_by_day_of_month alone can return 31
// rows of figures, so the cap is real, not theoretical; it is set well
// above every tool result this product actually produces for one answer,
// so the ordinary case is always fully derived.
//
// Past it, the two families degrade in OPPOSITE directions, deliberately.
// Money keeps matching against the literal tool values (stricter — but the
// literal values are always admissible, so a correct verbatim restatement
// still passes). Percentages are skipped entirely (Report.PercentSkipped),
// because without derived ratios almost every legitimate percentage would
// be refused. Each degradation is the one that cannot manufacture a false
// refusal out of a large result.
const maxValuesForDerivation = 150

// Verify checks answerText against the raw JSON results the MCP tools
// returned during this interaction. toolResults may include the payloads
// of ERRORED tool calls: a typed no_data/invalid_input object is still
// deterministic output the narration is entitled to quote from, and
// including it can only widen the allowed set, never narrow it.
//
// A Verify over zero tool results reports every money figure in the
// answer as a mismatch — correctly, since by this system's own definition
// there is then nothing a number could have come from. Callers that must
// not act on that (internal/explain already refuses the zero-tool-call
// case separately, with its own wording) should not call Verify at all.
func Verify(answerText string, toolResults []string) Report {
	allowed := newAllowedSet(toolResults)
	var report Report

	for _, found := range findCurrencyFigures(answerText) {
		report.CheckedCurrency++
		if !allowed.matchesMoney(found) {
			report.Currency = append(report.Currency, Mismatch{Kind: KindCurrency, Text: found.Text})
		}
	}

	if allowed.tooLargeForRatios {
		report.PercentSkipped = true
	} else {
		for _, found := range findPercentFigures(answerText) {
			report.CheckedPercent++
			if !allowed.matchesPercent(found) {
				report.Percent = append(report.Percent, Mismatch{Kind: KindPercent, Text: found.Text})
			}
		}
	}

	for _, found := range findDateFigures(answerText) {
		if len(allowed.dates) == 0 {
			// No tool result carried a date at all — there is nothing to
			// compare against, so reporting a "mismatch" would be noise.
			continue
		}
		report.CheckedDates++
		if _, ok := allowed.dates[found.ISO]; !ok {
			report.Dates = append(report.Dates, Mismatch{Kind: KindDate, Text: found.Text})
		}
	}

	return report
}

// figure is one numeric claim lifted out of a narration: its canonical
// value in hundredths of a unit (cents for money, hundredths of a
// percentage point for percentages), the number of decimal places it was
// WRITTEN with, and the literal text it came from.
type figure struct {
	Hundredths int64
	Decimals   int
	Text       string
}

// dateFigure is one fully-specified calendar date lifted out of a
// narration, normalized to YYYY-MM-DD.
type dateFigure struct {
	ISO  string
	Text string
}

// allowedSet is every value the tool results actually contain, in the two
// canonical forms this package compares against.
type allowedSet struct {
	// money is the sorted, deduplicated set of admissible ABSOLUTE money
	// values in hundredths (cents): the ones the tools returned literally,
	// plus every one-step sum and difference of two of them, recomputed
	// here in Go (see package doc, calibration point 3).
	money []int64
	// percent is the sorted, deduplicated set of admissible ABSOLUTE
	// percentage values, in hundredths of a percentage point: the ones the
	// tools stated literally, plus every ratio between two tool-returned
	// money values, recomputed here in Go.
	percent []int64
	// dates is every YYYY-MM-DD string the tool results carried.
	dates map[string]struct{}
	// tooLargeForRatios is set when the literal money set exceeded
	// maxValuesForDerivation and percentage checking was skipped.
	tooLargeForRatios bool
}

// tolerance returns the half-open window, in hundredths, within which a
// value stated with `decimals` decimal places is considered a restatement
// of an allowed value: one unit of the last place the narration actually
// wrote. "$159" (0 decimals) admits anything within a dollar; "$374.75"
// (2 decimals) admits only 374.75 itself.
//
// Never stricter than the tools' own two-decimal output: three or more
// stated decimals collapse to the two-decimal (exact) window rather than
// demanding a precision no tool in this product ever emits.
func tolerance(decimals int) int64 {
	switch {
	case decimals <= 0:
		return 100
	case decimals == 1:
		return 10
	default:
		return 1
	}
}

func (a allowedSet) matchesMoney(f figure) bool {
	return withinAny(a.money, f.Hundredths, tolerance(f.Decimals))
}

func (a allowedSet) matchesPercent(f figure) bool {
	return withinAny(a.percent, f.Hundredths, tolerance(f.Decimals))
}

// withinAny reports whether sorted contains a value strictly within tol of
// want. Binary search rather than a scan: the ratio-derived percentage set
// can hold tens of thousands of entries.
func withinAny(sorted []int64, want, tol int64) bool {
	lo := want - tol + 1
	hi := want + tol - 1
	i := sort.Search(len(sorted), func(i int) bool { return sorted[i] >= lo })
	return i < len(sorted) && sorted[i] <= hi
}

// newAllowedSet walks every tool result and collects what the model was
// actually shown.
func newAllowedSet(toolResults []string) allowedSet {
	moneySeen := map[int64]struct{}{}
	percentSeen := map[int64]struct{}{}
	dates := map[string]struct{}{}

	for _, raw := range toolResults {
		collectFromJSON(raw, moneySeen, percentSeen, dates)
	}

	set := allowedSet{dates: dates}

	// literal is what the tools actually returned. Both derivations below
	// run over THIS set, never over each other's output, so the admissible
	// set stays exactly one step wide.
	literal := sortedKeys(moneySeen)

	if len(literal) > maxValuesForDerivation {
		set.tooLargeForRatios = true
		set.money = literal
		set.percent = sortedKeys(percentSeen)
		return set
	}

	// Every one-step sum, difference and quotient, computed HERE, in Go —
	// the deterministic counterpart to the model's own arithmetic (package
	// doc, calibration point 3). literal holds absolute values, so a+b and
	// |a-b| over every unordered pair covers both directions; quotients
	// need every ORDERED pair, since a/b and b/a are different figures.
	derivedMoney := make(map[int64]struct{}, len(moneySeen))
	for _, v := range literal {
		derivedMoney[v] = struct{}{}
	}
	for i, a := range literal {
		for _, b := range literal[i:] {
			derivedMoney[a+b] = struct{}{}
			derivedMoney[abs64(a-b)] = struct{}{}
		}
	}
	for _, b := range literal {
		if b == 0 {
			continue
		}
		for _, a := range literal {
			// a and b are in hundredths, so the per-unit figure a/b
			// expressed in hundredths is a*100/b.
			derivedMoney[roundedDiv(a*100, b)] = struct{}{}
		}
	}
	set.money = sortedKeys(derivedMoney)

	// Every ratio between two tool-returned money values, likewise
	// (calibration point 4). a and b are in hundredths, so a/b*100 percent
	// expressed in hundredths of a point is a*10000/b.
	for _, b := range literal {
		if b == 0 {
			continue
		}
		for _, a := range literal {
			percentSeen[roundedDiv(a*10000, b)] = struct{}{}
		}
	}
	set.percent = sortedKeys(percentSeen)
	return set
}

// roundedDiv divides with round-half-away-from-zero on non-negative
// inputs, so a derived ratio lands on the same integer a human (or a
// model) writing it down would.
func roundedDiv(n, d int64) int64 {
	if d == 0 {
		return 0
	}
	return (n + d/2) / d
}

func sortedKeys(m map[int64]struct{}) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// isoDatePattern matches a bare YYYY-MM-DD string leaf.
var isoDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// jsonNumberInText matches any number embedded in a JSON string leaf —
// discrepancy flag details carry real, computed figures inside prose
// ("gross revenue 901.75 deviates 28.9% from the trailing 3-day average
// 1268.16"), and a narration is entitled to quote them.
var jsonNumberInText = regexp.MustCompile(`\d[\d,]*(?:\.\d+)?`)

// collectFromJSON walks one tool result's raw JSON, folding every numeric
// leaf — JSON numbers, numeric strings, and numbers embedded inside string
// prose — into the allowed sets. Unparseable output is skipped rather than
// treated as fatal, matching internal/explain.collectProvenance's own
// best-effort discipline: this is a check layered on top of an answer, and
// a malformed tool payload must not itself become a refusal.
func collectFromJSON(raw string, money, percent map[int64]struct{}, dates map[string]struct{}) {
	dec := json.NewDecoder(strings.NewReader(raw))
	// Numbers stay as their literal source text, never round-tripped
	// through float64: this package exists to compare money to the cent.
	dec.UseNumber()

	var v any
	if err := dec.Decode(&v); err != nil {
		// Not every "source" this package is handed is a tool-result JSON
		// payload — Explain also widens the allowed set with the
		// immediately preceding turn's own served answer text (a natural
		// sentence, e.g. "Margin on 2026-08-29 was $3,225.06."), so that a
		// follow-up restating a figure THIS PRODUCT already verified and
		// served last turn isn't refused for repeating it. A plain-text
		// source gets its figures lifted the same way the NARRATION itself
		// does — findCurrencyFigures/findPercentFigures/findDateFigures —
		// rather than being silently dropped for not being valid JSON.
		collectFromText(raw, money, percent, dates)
		return
	}
	walk(v, money, percent, dates)
}

// collectFromText is collectFromJSON's fallback for a plain-text source
// (see its call site's doc comment). It reuses the exact figure-extraction
// the narration under check is itself scanned with, so a figure allowed
// via this path is held to the same recognition rules as one asserted.
func collectFromText(raw string, money, percent map[int64]struct{}, dates map[string]struct{}) {
	for _, f := range findCurrencyFigures(raw) {
		money[f.Hundredths] = struct{}{}
	}
	for _, f := range findPercentFigures(raw) {
		percent[f.Hundredths] = struct{}{}
	}
	for _, d := range findDateFigures(raw) {
		dates[d.ISO] = struct{}{}
	}
}

// provenanceKey is the one subtree deliberately excluded from the allowed
// set. Every tool result in this product carries a "source_row_refs" array
// of {file, row} pairs (contracts/mcp-tools.md's cross-cutting provenance
// rule), and those row indices are small integers — 2, 3, 4, ... — that no
// narration ever states as money. Admitting them was measurably harmful,
// not theoretically: with one-step derivation on, every real figure plus
// or minus $2..$9 became admissible, and an adversarial replay over the
// evaluation corpus caught only 19 of 29 five-dollar alterations. Skipping
// the subtree took that back to 29 of 29 with no effect on any real
// answer.
const provenanceKey = "source_row_refs"

func walk(v any, money, percent map[int64]struct{}, dates map[string]struct{}) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if k == provenanceKey {
				continue
			}
			walk(val, money, percent, dates)
		}
	case []any:
		for _, item := range t {
			walk(item, money, percent, dates)
		}
	case json.Number:
		if h, _, ok := parseHundredths(t.String()); ok {
			money[abs64(h)] = struct{}{}
		}
	case string:
		collectFromString(t, money, percent, dates)
	}
}

func collectFromString(s string, money, percent map[int64]struct{}, dates map[string]struct{}) {
	if isoDatePattern.MatchString(s) {
		// A date is not a money value. Recorded as a date, and deliberately
		// NOT decomposed into 2024/08/01 — injecting a bare year into the
		// money set would let a fabricated "$2,024" pass.
		dates[s] = struct{}{}
		return
	}
	for _, loc := range jsonNumberInText.FindAllStringIndex(s, -1) {
		lit := s[loc[0]:loc[1]]
		h, _, ok := parseHundredths(lit)
		if !ok {
			continue
		}
		money[abs64(h)] = struct{}{}
		// A number written with a percent sign in tool output is a
		// percentage the tools stated themselves (an anomaly flag's
		// "deviates 28.9% ... (threshold 20%)"), so it is admissible as a
		// percentage directly, not only as a derived ratio.
		if loc[1] < len(s) && s[loc[1]] == '%' {
			percent[abs64(h)] = struct{}{}
		}
	}
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// parseHundredths converts a decimal literal (commas and surrounding
// whitespace tolerated) into hundredths of a unit, together with how many
// decimal places it was written with. Values with more than two decimals
// are rounded to hundredths — the precision every tool in this product
// emits — rather than rejected.
func parseHundredths(lit string) (hundredths int64, decimals int, ok bool) {
	s := strings.ReplaceAll(strings.TrimSpace(lit), ",", "")
	neg := false
	switch {
	case strings.HasPrefix(s, "-"):
		neg, s = true, s[1:]
	case strings.HasPrefix(s, "+"):
		s = s[1:]
	}
	if s == "" {
		return 0, 0, false
	}

	intPart, fracPart, hasFrac := strings.Cut(s, ".")
	if intPart == "" || !allDigits(intPart) {
		return 0, 0, false
	}
	if hasFrac && (fracPart == "" || !allDigits(fracPart)) {
		return 0, 0, false
	}

	whole, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	// Guard the *100 below against overflow. A money figure this large is
	// not something this product computes; treating it as unparseable is
	// safer than wrapping around into a wrong value.
	if whole > (1<<62)/100 {
		return 0, 0, false
	}

	decimals = len(fracPart)
	value := whole * 100
	if decimals > 0 {
		padded := fracPart
		if decimals < 2 {
			padded = fracPart + strings.Repeat("0", 2-decimals)
		}
		cents, err := strconv.ParseInt(padded[:2], 10, 64)
		if err != nil {
			return 0, 0, false
		}
		value += cents
		// Round the third decimal place up into hundredths, so "33.756"
		// canonicalizes the way a human would write it.
		if decimals > 2 && padded[2] >= '5' {
			value++
		}
	}
	if neg {
		value = -value
	}
	return value, decimals, true
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Narration-side extraction.
//
// currencyInAnswer matches a money figure written either with a currency
// symbol ("$374.75", "R$ 50", "-$450.75") or as a bare cents-precision
// decimal ("374.75"), mirroring the shape internal/explain's own
// looksLikeCurrencyAmount already treats as currency-shaped. The bare form
// deliberately requires exactly two decimals so a plain calendar date
// ("2024-08-01") and a one-decimal percentage ("28.9") can never match it.
var currencyInAnswer = regexp.MustCompile(`\p{Sc}\s?\d[\d,]*(?:\.\d+)?|\b\d[\d,]*\.\d{2}\b`)

// dollarsWordInAnswer and centsWordInAnswer catch money spelled out in
// words rather than with a symbol — "375 dollars", "26 cents" — which
// currencyInAnswer's two forms both miss: there is no symbol, and a whole
// number ("375") or a bare integer cents count ("26") has no decimal point
// for the bare-decimal branch to require. Found live: a narration is free
// to drop the "$" this product's own tool output always carries, and
// currencyInAnswer's silence on that phrasing was not a refusal — it was
// this package extracting NOTHING for that figure, so a wrong number
// stated this way would have sailed through unchecked. See
// findCurrencyFigures for why "cents" needs its own unit conversion rather
// than reusing parseHundredths as-is.
var dollarsWordInAnswer = regexp.MustCompile(`(?i)\b\d[\d,]*(?:\.\d+)?\s+dollars?\b`)
var centsWordInAnswer = regexp.MustCompile(`(?i)\b\d[\d,]*(?:\.\d+)?\s+cents?\b`)

// percentInAnswer matches a percentage figure: a number immediately
// followed by "%", the word "percent", or "percentage point(s)" — a
// week-over-week or platform-comparison delta is routinely phrased as
// points ("margin improved by 3 percentage points"), and "percent\b"
// alone never matches "percentage" (no word boundary follows "percent" in
// it), so that entire phrasing was previously extracted as nothing at all.
var percentInAnswer = regexp.MustCompile(`(?i)\d[\d,]*(?:\.\d+)?\s?(?:%|percentage\s+points?|percent\b)`)

// findCurrencyFigures lifts every money figure out of a narration.
//
// The leading sign is deliberately ignored: this product's narrations
// carry direction in words ("a net loss of $450.75", "down from"), not in
// a symbol, so matching is on absolute value (package doc, calibration
// point 2).
func findCurrencyFigures(text string) []figure {
	var out []figure
	for _, loc := range currencyInAnswer.FindAllStringIndex(text, -1) {
		match := text[loc[0]:loc[1]]
		// A bare decimal immediately followed by "%" is a percentage that
		// happens to be written to two places, not a money figure.
		if loc[1] < len(text) && text[loc[1]] == '%' {
			continue
		}
		// A bare decimal glued to a preceding digit or dot is a fragment of
		// something else (a version string, an id), not a money figure —
		// the same manual boundary discipline
		// internal/ambiguity/daterange.go applies to bare years, since RE2
		// has no lookbehind.
		if !hasCurrencySymbol(match) && loc[0] > 0 {
			if prev := text[loc[0]-1]; prev == '.' || (prev >= '0' && prev <= '9') {
				continue
			}
		}
		// A bare decimal immediately followed by "cents" is about to be
		// picked up by the dedicated cents-word loop below, which converts
		// units correctly (26 cents is $0.26, not $26.00) — leaving it to
		// this loop too would additionally, and wrongly, admit $26.00.
		if !hasCurrencySymbol(match) {
			if rest := strings.TrimLeft(text[loc[1]:], " "); strings.HasPrefix(strings.ToLower(rest), "cent") {
				continue
			}
		}
		digits := stripToNumber(match)
		h, decimals, ok := parseHundredths(digits)
		if !ok {
			continue
		}
		out = append(out, figure{Hundredths: abs64(h), Decimals: decimals, Text: match})
	}

	// Word-denominated whole/decimal dollars ("375 dollars", "12.50 USD"):
	// same unit as the symbol-prefixed form, so the numeral converts with
	// the same parseHundredths this loop already uses elsewhere.
	for _, loc := range dollarsWordInAnswer.FindAllStringIndex(text, -1) {
		match := text[loc[0]:loc[1]]
		h, decimals, ok := parseHundredths(stripToNumber(match))
		if !ok {
			continue
		}
		out = append(out, figure{Hundredths: abs64(h), Decimals: decimals, Text: match})
	}

	// Word-denominated cents ("26 cents"): the numeral IS the hundredths-
	// of-a-dollar count already, not a dollar amount to multiply by 100 —
	// parseHundredths(digits) returns digits*100 under a dollars
	// assumption, so dividing that back down by 100 (rounding the same way
	// roundedDiv rounds a derived ratio) recovers the true cent value.
	// Always exact (2-decimal) precision: "26 cents" is as precise a claim
	// as "$0.26", never a rounded-dollar approximation.
	for _, loc := range centsWordInAnswer.FindAllStringIndex(text, -1) {
		match := text[loc[0]:loc[1]]
		asIfDollars, _, ok := parseHundredths(stripToNumber(match))
		if !ok {
			continue
		}
		out = append(out, figure{Hundredths: abs64(roundedDiv(asIfDollars, 100)), Decimals: 2, Text: match})
	}
	return out
}

// findPercentFigures lifts every percentage figure out of a narration.
func findPercentFigures(text string) []figure {
	var out []figure
	for _, match := range percentInAnswer.FindAllString(text, -1) {
		h, decimals, ok := parseHundredths(stripToNumber(match))
		if !ok {
			continue
		}
		out = append(out, figure{Hundredths: abs64(h), Decimals: decimals, Text: match})
	}
	return out
}

func hasCurrencySymbol(s string) bool {
	for _, r := range s {
		if r >= 0x20A0 && r <= 0x20BF { // currency symbols block
			return true
		}
		switch r {
		case '$', '¢', '£', '¤', '¥':
			return true
		}
	}
	return false
}

// stripToNumber reduces a matched figure to the bare decimal literal
// inside it, dropping currency symbols, the "%"/"percent" suffix, and any
// whitespace between them.
func stripToNumber(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '.' || r == ',' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Date extraction (advisory only — see package doc).
//
// Deliberately narrow: an ISO date, or a month name with BOTH a day and a
// four-digit year. A partial reference ("Aug 5", "Aug 1-14, 2024") is
// skipped rather than guessed at, exactly as
// internal/ambiguity/daterange.go skips a form it cannot recognize with
// certainty — and here the cost of guessing is log noise, which is its own
// kind of harm.
var (
	monthNames = map[string]time.Month{
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
	monthNamePattern = `(?:jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|jun(?:e)?|jul(?:y)?|aug(?:ust)?|sep(?:t(?:ember)?)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)`

	isoInAnswer          = regexp.MustCompile(`\b(\d{4})-(\d{2})-(\d{2})\b`)
	monthDayYearInAnswer = regexp.MustCompile(`(?i)\b(` + monthNamePattern + `)\.?\s+(\d{1,2})(?:st|nd|rd|th)?\s*,?\s*(\d{4})\b`)
	dayMonthYearInAnswer = regexp.MustCompile(`(?i)\b(\d{1,2})(?:st|nd|rd|th)?\s+(?:of\s+)?(` + monthNamePattern + `)\.?,?\s+(\d{4})\b`)
)

func findDateFigures(text string) []dateFigure {
	var out []dateFigure
	seen := map[string]struct{}{}

	add := func(iso, raw string) {
		if iso == "" {
			return
		}
		if _, dup := seen[iso]; dup {
			return
		}
		seen[iso] = struct{}{}
		out = append(out, dateFigure{ISO: iso, Text: raw})
	}

	for _, m := range isoInAnswer.FindAllStringSubmatch(text, -1) {
		if _, err := time.Parse("2006-01-02", m[0]); err == nil {
			add(m[0], m[0])
		}
	}
	for _, m := range monthDayYearInAnswer.FindAllStringSubmatch(text, -1) {
		add(normalizeDate(m[1], m[2], m[3]), m[0])
	}
	for _, m := range dayMonthYearInAnswer.FindAllStringSubmatch(text, -1) {
		add(normalizeDate(m[2], m[1], m[3]), m[0])
	}
	return out
}

func normalizeDate(monthName, day, year string) string {
	month, ok := monthNames[strings.ToLower(monthName)]
	if !ok {
		return ""
	}
	d, err := strconv.Atoi(day)
	if err != nil {
		return ""
	}
	y, err := strconv.Atoi(year)
	if err != nil {
		return ""
	}
	t := time.Date(y, month, d, 0, 0, 0, 0, time.UTC)
	if t.Year() != y || t.Month() != month || t.Day() != d {
		return "" // calendar-invalid (February 30th) — not a date claim
	}
	return fmt.Sprintf("%04d-%02d-%02d", y, int(month), d)
}
