package httpapi

// Follow-up question suggestions for a successfully answered interaction.
//
// The load-bearing constraint, mirroring visualization.go's own doc comment
// exactly: WHICH follow-up questions get offered is decided here, in plain
// Go, from (a) which typed MCP tool actually ran and (b) the shape of the
// deterministic result it returned. It is never a second model call. Asking
// a model "what should the owner ask next?" would be the exact hallucination
// surface exampleQuestions.ts's own doc comment already forbids ("suggesting
// a question the product cannot answer is the same class of lie as
// inventing a number") — a free-text "what next" call has no typed tool
// backing it, so nothing stops it from inventing a question this product
// cannot actually answer. It would also bill a second, unnecessary model
// call on every single answered question, directly against this project's
// cost-discipline KR (CLAUDE.md's instrumentation/cost-panel requirements).
//
// Every suggestion is grounded in a REAL date or period pulled from the
// tool's own JSON result — never a placeholder like "last week" — and
// clamped into [dataStart, dataEnd], the actual min/max date this product
// has reconciled data for (internal/storage.LoadDataDateRange, resolved
// once at process start and threaded through Deps.DataStart/DataEnd). A
// suggestion whose real answer would fall outside that range is never
// offered; see weekOverWeekAround's doc comment for the specific clamp
// policy used for "zoom out to the prior week"-shaped suggestions.
//
// Fixed tool priority, mirroring deriveVisualization's "the narrowest
// subject wins" convention exactly (visualization.go's own doc comment):
// when an interaction called more than one recognized tool, the answer is
// about whichever of these is queried first below, since a question that
// reached (say) a promotions tool is about promotions even if a daily
// summary was also pulled for supporting context. get_period_totals is
// inserted just above get_daily_summary — a period-level summary, but still
// the least specific "just tell me what happened" shape among the seven
// tools, the same role get_daily_summary already played for a single day.
//
// Per-tool suggestion count is 1-3 (never more), and the caller
// (deriveFollowUpSuggestions) caps the final list at MaxFollowUpSuggestions
// regardless of how many a single template table produced. A tool result
// that does not carry enough information to ground a real date/period
// (e.g. list_discrepancies over a clean period — DiscrepanciesResult never
// echoes back the period it was asked about, only which days it flagged)
// produces zero suggestions rather than a vague or placeholder one. That is
// treated as a correct, honest outcome, not a bug to work around.
import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/explain"
)

// MaxFollowUpSuggestions caps how many follow-up chips ever render under one
// answer — matching Perplexity/ChatGPT's follow-up row, never a wall of
// options that would defeat the point of narrowing what to ask next.
const MaxFollowUpSuggestions = 3

// suggestionDateLayout matches every other YYYY-MM-DD convention this
// codebase already uses (internal/mcptools' dateLayout, cmd/server/main.go's
// dateLayout).
const suggestionDateLayout = "2006-01-02"

// deriveFollowUpSuggestions picks at most MaxFollowUpSuggestions natural-
// language follow-up questions for a just-answered interaction, grounded in
// the real tool result(s) it produced. Returns an empty slice — never nil
// treated specially, never a placeholder — when no recognized tool ran or
// the result carries nothing groundable to suggest from.
func deriveFollowUpSuggestions(invocations []explain.ToolInvocation, askedQuestion, dataStart, dataEnd string) []string {
	bounds, ok := newDateBounds(dataStart, dataEnd)
	if !ok {
		return []string{}
	}

	byTool := map[string][]string{}
	for _, inv := range invocations {
		byTool[inv.Name] = append(byTool[inv.Name], inv.ResultJSON)
	}

	var raw []string
	switch {
	case len(byTool["compare_platform_economics"]) > 0:
		raw = platformComparisonFollowUps(byTool["compare_platform_economics"][0], bounds)
	case len(byTool["list_negative_roi_promotions"]) > 0 || len(byTool["get_promotion_roi"]) > 0:
		raw = promotionFollowUps(byTool)
	case len(byTool["list_discrepancies"]) > 0:
		raw = discrepancyFollowUps(byTool["list_discrepancies"], bounds)
	case len(byTool["get_margin_delta"]) > 0:
		raw = marginDeltaFollowUps(byTool["get_margin_delta"][0], bounds)
	case len(byTool["get_period_totals"]) > 0:
		raw = periodTotalsFollowUps(byTool["get_period_totals"][0], bounds)
	case len(byTool["get_daily_summary"]) > 0:
		raw = dailySummaryFollowUps(byTool["get_daily_summary"], bounds)
	}

	return finalizeSuggestions(raw, askedQuestion)
}

// --- get_daily_summary ---------------------------------------------------

func dailySummaryFollowUps(results []string, bounds dateBounds) []string {
	var dates []time.Time
	for _, raw := range results {
		var parsed dailySummaryJSON
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil || parsed.Date == "" {
			continue
		}
		if d, ok := parseSuggestionDate(parsed.Date); ok {
			dates = append(dates, d)
		}
	}
	if len(dates) == 0 {
		return nil
	}
	sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })
	// The most recent day looked up in this interaction is what the answer
	// is actually about, even when several days were pulled (e.g. for a
	// multi-day margin-by-day chart) — matching visualization.go's own
	// dailySummaryVisualization, which renders the LATEST day's revenue mix
	// when only one day is present and treats every day equally only when
	// building the multi-day bar.
	latest := dates[len(dates)-1]
	dateStr := formatSuggestionDate(latest)

	suggestions := []string{
		fmt.Sprintf("Were there any discrepancies on %s?", dateStr),
	}
	if before, including, ok := weekOverWeekAround(latest, bounds); ok {
		suggestions = append(suggestions, fmt.Sprintf(
			"How did %s to %s compare to %s to %s?",
			formatSuggestionDate(before.start), formatSuggestionDate(before.end),
			formatSuggestionDate(including.start), formatSuggestionDate(including.end),
		))
	}
	return suggestions
}

// --- get_margin_delta ------------------------------------------------------

func marginDeltaFollowUps(resultJSON string, bounds dateBounds) []string {
	var parsed marginDeltaJSON
	if err := json.Unmarshal([]byte(resultJSON), &parsed); err != nil {
		return nil
	}
	start, okA := parseSuggestionDate(parsed.PeriodB.Start)
	end, okB := parseSuggestionDate(parsed.PeriodB.End)
	if !okA || !okB {
		return nil
	}
	start, end = bounds.clampPeriod(start, end)

	return []string{
		fmt.Sprintf("Were there any discrepancies between %s and %s?", formatSuggestionDate(start), formatSuggestionDate(end)),
		fmt.Sprintf("What were the full totals for %s to %s?", formatSuggestionDate(start), formatSuggestionDate(end)),
		fmt.Sprintf("How did the delivery platforms compare between %s and %s?", formatSuggestionDate(start), formatSuggestionDate(end)),
	}
}

// --- list_discrepancies ----------------------------------------------------

// discrepancyFollowUps only produces suggestions when at least one flagged
// day is present. A clean result (Days empty) is a real, good answer, but
// DiscrepanciesResult never echoes back the date/period it was actually
// asked about (see discrepanciesJSON in visualization.go) — only which days
// were flagged — so there is no real date to ground a suggestion in, and an
// empty slice is the honest outcome rather than guessing at one.
func discrepancyFollowUps(results []string, bounds dateBounds) []string {
	var dates []time.Time
	for _, raw := range results {
		var parsed discrepanciesJSON
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}
		for _, day := range parsed.Days {
			if d, ok := parseSuggestionDate(day.Date); ok {
				dates = append(dates, d)
			}
		}
	}
	if len(dates) == 0 {
		return nil
	}
	sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })
	latest := dates[len(dates)-1]
	dateStr := formatSuggestionDate(latest)

	suggestions := []string{
		fmt.Sprintf("What was the full daily summary for %s?", dateStr),
	}
	if before, including, ok := weekOverWeekAround(latest, bounds); ok {
		suggestions = append(suggestions, fmt.Sprintf(
			"How did the week of %s to %s compare to %s to %s?",
			formatSuggestionDate(including.start), formatSuggestionDate(including.end),
			formatSuggestionDate(before.start), formatSuggestionDate(before.end),
		))
	}
	return suggestions
}

// --- get_promotion_roi / list_negative_roi_promotions ----------------------

func promotionFollowUps(byTool map[string][]string) []string {
	var raws []string
	raws = append(raws, byTool["list_negative_roi_promotions"]...)
	raws = append(raws, byTool["get_promotion_roi"]...)

	for _, raw := range raws {
		var parsed promotionsJSON
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil || len(parsed.Promotions) == 0 {
			continue
		}
		// The first promotion's own period grounds the suggestions. Every
		// promotion in one result can in principle carry a slightly
		// different period (get_promotion_roi by campaign_id has no period
		// filter at all), but the first is always a real, already-answered
		// period — never a placeholder — which is the bar this file holds
		// itself to.
		p := parsed.Promotions[0]
		start, okA := parseSuggestionDate(p.Period.Start)
		end, okB := parseSuggestionDate(p.Period.End)
		if !okA || !okB {
			continue
		}
		return []string{
			fmt.Sprintf("How did iFood and Just Eat Takeaway compare in commission and promo cost between %s and %s?", formatSuggestionDate(start), formatSuggestionDate(end)),
			fmt.Sprintf("Were there other promotions with negative ROI between %s and %s?", formatSuggestionDate(start), formatSuggestionDate(end)),
			fmt.Sprintf("What were the full totals for %s to %s?", formatSuggestionDate(start), formatSuggestionDate(end)),
		}
	}
	return nil
}

// --- compare_platform_economics --------------------------------------------

// platformComparisonPeriodJSON captures just the period field of
// PlatformComparisonResult — visualization.go's own platformComparisonJSON
// deliberately omits it (its bar chart has no use for the period), so this
// file parses its own narrower view of the same JSON rather than widening a
// shared type for a field only this file needs.
type platformComparisonPeriodJSON struct {
	Period periodJSON `json:"period"`
}

func platformComparisonFollowUps(resultJSON string, bounds dateBounds) []string {
	var parsed platformComparisonPeriodJSON
	if err := json.Unmarshal([]byte(resultJSON), &parsed); err != nil {
		return nil
	}
	start, okA := parseSuggestionDate(parsed.Period.Start)
	end, okB := parseSuggestionDate(parsed.Period.End)
	if !okA || !okB {
		return nil
	}
	start, end = bounds.clampPeriod(start, end)

	return []string{
		fmt.Sprintf("Were there any promotions with negative ROI between %s and %s?", formatSuggestionDate(start), formatSuggestionDate(end)),
		fmt.Sprintf("What were the full totals for %s to %s?", formatSuggestionDate(start), formatSuggestionDate(end)),
		fmt.Sprintf("Were there any discrepancies between %s and %s?", formatSuggestionDate(start), formatSuggestionDate(end)),
	}
}

// --- get_period_totals -------------------------------------------------

// periodTotalsSummaryJSON captures just the fields this file needs out of
// PeriodTotalsResult (mcptools/period_tools.go) — start/end and the
// best/worst day it already computed, so these suggestions can point
// straight at the two days that actually made the period's total what it
// is, rather than at an arbitrary date.
type periodTotalsSummaryJSON struct {
	Start    string           `json:"start"`
	End      string           `json:"end"`
	BestDay  dayMarginRefJSON `json:"best_day"`
	WorstDay dayMarginRefJSON `json:"worst_day"`
}

type dayMarginRefJSON struct {
	Date string `json:"date"`
}

func periodTotalsFollowUps(resultJSON string, bounds dateBounds) []string {
	var parsed periodTotalsSummaryJSON
	if err := json.Unmarshal([]byte(resultJSON), &parsed); err != nil {
		return nil
	}
	start, okA := parseSuggestionDate(parsed.Start)
	end, okB := parseSuggestionDate(parsed.End)
	if !okA || !okB {
		return nil
	}
	start, end = bounds.clampPeriod(start, end)

	suggestions := []string{
		fmt.Sprintf("What happened on %s, the best day in that period?", parsed.BestDay.Date),
	}
	// A single-day period (or an exact margin tie resolved to the same
	// earliest date, per GetPeriodTotals' own documented tie-break) makes
	// best_day and worst_day the same date — asking about it twice would be
	// a near-duplicate suggestion, so it is skipped rather than forced.
	if parsed.WorstDay.Date != "" && parsed.WorstDay.Date != parsed.BestDay.Date {
		suggestions = append(suggestions, fmt.Sprintf("What happened on %s, the worst day in that period?", parsed.WorstDay.Date))
	}
	suggestions = append(suggestions, fmt.Sprintf("How did the delivery platforms compare between %s and %s?", formatSuggestionDate(start), formatSuggestionDate(end)))
	return suggestions
}

// --- shared helpers ----------------------------------------------------

// dateBounds is the real, real [dataStart, dataEnd] range this product has
// reconciled data for — every generated suggestion's date/period is clamped
// into it so nothing offered points at data that cannot exist.
type dateBounds struct {
	start time.Time
	end   time.Time
}

func newDateBounds(dataStart, dataEnd string) (dateBounds, bool) {
	start, okA := parseSuggestionDate(dataStart)
	end, okB := parseSuggestionDate(dataEnd)
	if !okA || !okB || end.Before(start) {
		return dateBounds{}, false
	}
	return dateBounds{start: start, end: end}, true
}

func (b dateBounds) clamp(t time.Time) time.Time {
	if t.Before(b.start) {
		return b.start
	}
	if t.After(b.end) {
		return b.end
	}
	return t
}

// clampPeriod clamps both ends of [start, end] independently into bounds.
// Used defensively on periods already answered by a tool (so they are
// already real and in-range) rather than to legitimize an out-of-range
// period — belt-and-suspenders against this file ever drifting from that
// invariant.
func (b dateBounds) clampPeriod(start, end time.Time) (time.Time, time.Time) {
	return b.clamp(start), b.clamp(end)
}

// weekPeriod is one inclusive 7-day-or-shorter span.
type weekPeriod struct {
	start, end time.Time
}

// weekOverWeekAround computes "the week including date" and "the week
// before it", both clamped into bounds, for a "how did this compare to the
// prior week" follow-up.
//
// Clamp policy (documented per this file's own doc comment, since the task
// leaves the choice to the implementation): the including-week's end is
// clamped forward to bounds.end and its start is clamped forward to
// bounds.start if the naive 7-day window would start earlier. The
// before-week is then the 7 days immediately preceding the (possibly
// clamped) including-week. If the before-week's end already falls before
// bounds.start, it cannot exist in the real data at all — WEEK OMITTED
// (ok=false) rather than clamped into a degenerate single-day or
// zero-length range that would misrepresent "the week before" as something
// shorter than a week. If only the before-week's START needs clamping (it
// started before the covered range but still has some real days in it), it
// is clamped forward to bounds.start, which is a real, honest subset of
// "the week before" rather than an invented span.
func weekOverWeekAround(date time.Time, bounds dateBounds) (before, including weekPeriod, ok bool) {
	includingEnd := bounds.clamp(date)
	includingStart := includingEnd.AddDate(0, 0, -6)
	if includingStart.Before(bounds.start) {
		includingStart = bounds.start
	}

	beforeEnd := includingStart.AddDate(0, 0, -1)
	if beforeEnd.Before(bounds.start) {
		return weekPeriod{}, weekPeriod{}, false
	}
	beforeStart := beforeEnd.AddDate(0, 0, -6)
	if beforeStart.Before(bounds.start) {
		beforeStart = bounds.start
	}

	return weekPeriod{start: beforeStart, end: beforeEnd}, weekPeriod{start: includingStart, end: includingEnd}, true
}

func parseSuggestionDate(s string) (time.Time, bool) {
	t, err := time.Parse(suggestionDateLayout, s)
	return t, err == nil
}

func formatSuggestionDate(t time.Time) string {
	return t.Format(suggestionDateLayout)
}

// finalizeSuggestions applies every cross-cutting rule this file's doc
// comment promises: drop anything blank, drop an exact duplicate of the
// question just asked (case/whitespace-insensitive — an exact string
// comparison is deliberately all this does; fuzzy matching a natural-
// language question against a generated one is not worth the complexity for
// a "don't suggest what was just asked" nicety), drop internal duplicates,
// and cap at MaxFollowUpSuggestions.
func finalizeSuggestions(raw []string, askedQuestion string) []string {
	out := make([]string, 0, MaxFollowUpSuggestions)
	seen := make(map[string]bool, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if strings.EqualFold(s, strings.TrimSpace(askedQuestion)) {
			continue
		}
		key := strings.ToLower(s)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
		if len(out) == MaxFollowUpSuggestions {
			break
		}
	}
	return out
}
