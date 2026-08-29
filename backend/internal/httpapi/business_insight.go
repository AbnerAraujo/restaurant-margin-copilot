package httpapi

// Business Insight Advisor (specs/009-business-insight-advisor): the
// DETERMINISTIC half of the advisor skill, plus the HTTP handler for the
// on-demand probabilistic half (internal/advisor).
//
// The load-bearing constraint, mirroring suggestions.go's and
// visualization.go's own doc comments exactly: WHETHER a business insight
// is worth offering, and WHAT it is about (its kind and title), is decided
// here, in plain Go, from the raw per-tool JSON already in scope for this
// answer — never by a model call. deriveBusinessInsightTeaser runs on
// EVERY answered question at zero cost, and most answers get nothing:
// clean data, below-threshold rates, improving margins, unrecognized
// tools, and unparseable results all yield nil, never a generic "here's a
// tip" filler. The expensive part — the advice TEXT — is a separate,
// opt-in, billed Claude Sonnet 5 call (POST /api/business-insight below)
// that runs only when the owner explicitly taps the teaser.
//
// Unlike deriveFollowUpSuggestions' single-source switch, the derivation
// here is a sequence of independent checks in a fixed priority order (the
// same "narrowest subject wins" ordering that switch uses): a tool that
// RAN but shows a clean/below-threshold pattern must fall through to the
// next check rather than swallowing the whole derivation — a platform
// comparison with unremarkable rates over a period whose supporting
// daily summary carries a real discrepancy flag should still surface the
// discrepancy insight. At most ONE teaser is ever returned (spec FR-008);
// competing suggestion chips would dilute the one worth acting on.
//
// Threshold provenance (docs/product-strategy.md's Sourced/Judgment
// tagging discipline — full source list in
// specs/009-business-insight-advisor/plan.md): each threshold constant
// below documents separately what published industry material anchors it
// and where the exact numeric cut is this project's own labeled judgment,
// because no source prescribes one.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/advisor"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/explain"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/llmclient"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/money"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// BusinessInsightTeaser is the zero-cost, deterministically-derived
// teaser carried on AskResponse.BusinessInsight: an internal kind tag
// (one of internal/advisor's five closed-set kinds) plus the short
// human-readable title the frontend shows — and nothing else. The full
// advice text does not exist yet at teaser time; it is generated only if
// the owner taps.
type BusinessInsightTeaser struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
}

// --- documented thresholds -------------------------------------------

// highCommissionThresholdBps is the effective commission rate (in basis
// points — 2000 = 20.00%) at or above which a compare_platform_economics
// result triggers the high_commission insight.
//
// Sourced anchor: major delivery marketplaces publish tiered commission
// plans spanning roughly 15-30% — DoorDash Basic/Plus/Premier at
// 15/25/30%, Uber Eats 15-30% (30% standard marketplace), Grubhub 15-25%
// plus marketing fees (OPA! Link 2025 commission-fee guide; CloudKitchens
// 2025 puts the typical band at 15-30% plus 2-4% processing) — and
// commission negotiation/tier review is a real documented practice
// (Orders.co). Judgment cut, labeled as such: no source prescribes a
// specific "worth reviewing" line, so this project draws it at 20.00% —
// five points above the published ~15% entry tiers, i.e. the point where
// the owner is demonstrably paying materially above the cheapest
// published plan and the "confirm your tier, weigh what the premium
// visibility buys, negotiate" advice has real room to act on.
const highCommissionThresholdBps int64 = 2000

// dayOfMonthSpikeRatioNum/Den express the day_of_month_expense_spike
// outlier rule as a ratio (3/2 = 1.5x): the highest day-of-month average
// expense must be at least 1.5x the MEDIAN day-of-month average, with at
// least dayOfMonthSpikeMinOccurrences occurrences behind it.
//
// Sourced anchor: recurring same-day-of-month cost concentration and its
// remedies (par levels, ordering cadence, supplier delivery timing) are
// well-documented practice (Restaurant365's food-cost techniques, Toast's
// inventory-management guidance). Judgment cut, labeled as such: no
// source prescribes a numeric outlier multiple, so 1.5x-the-median with
// >=2 occurrences is this project's own line — median rather than mean so
// one giant day cannot drag the baseline up and hide itself, and >=2
// occurrences so a single unusual calendar day never masquerades as a
// recurring pattern.
const (
	dayOfMonthSpikeRatioNum       int64 = 3
	dayOfMonthSpikeRatioDen       int64 = 2
	dayOfMonthSpikeMinOccurrences       = 2
)

// marginDeclineMaterialityPercent is how big a period-over-period margin
// decline must be, relative to the earlier period's absolute margin,
// before get_margin_delta triggers the margin_decline insight (5 = 5%).
//
// Judgment, labeled as such: the trigger FACT (margin declined) is pure
// computed data, but surfacing every one-dollar wobble would make the
// teaser spammy noise — exactly what spec FR-001/FR-008 rule out — so a
// materiality floor is needed and no researched source prescribes one.
// 5% of the prior period's own margin is this project's line. A decline
// FROM zero-or-negative TO worse always qualifies (the relative test is
// against |period A|, and 5% of nothing is nothing).
const marginDeclineMaterialityPercent int64 = 5

// Teaser titles — short, human-readable, and phrased as suggestions, not
// facts: the computed pattern behind each is already on screen with full
// provenance, so the title's only job is to say what tapping will explain.
const (
	titleDiscrepancyPattern     = "Recurring discrepancies may be preventable — see how"
	titleNegativePromoROI       = "This promotion lost money — what owners typically do next"
	titleHighCommission         = "That commission rate is in the platforms' premium band — options to review"
	titleDayOfMonthExpenseSpike = "One day of the month runs notably more expensive — ways to smooth it"
	titleMarginDecline          = "Margin is down vs. the comparison — levers worth reviewing"
	titlePeriodLoss             = "This period ran at a loss — levers worth reviewing"
)

// deriveBusinessInsightTeaser inspects the SAME raw tool result(s) this
// answer already carries and decides whether ONE business insight is
// worth offering — nil when nothing applies, which is the expected
// outcome for most answers. Zero model calls, by construction.
func deriveBusinessInsightTeaser(invocations []explain.ToolInvocation) *BusinessInsightTeaser {
	byTool := map[string][]string{}
	for _, inv := range invocations {
		byTool[inv.Name] = append(byTool[inv.Name], inv.ResultJSON)
	}

	// Fixed priority order — see the file doc comment for why this is a
	// fall-through sequence rather than deriveFollowUpSuggestions' switch.
	if t := highCommissionTeaser(byTool["compare_platform_economics"]); t != nil {
		return t
	}
	if t := negativePromoTeaser(byTool); t != nil {
		return t
	}
	if t := discrepancyListTeaser(byTool["list_discrepancies"]); t != nil {
		return t
	}
	if t := marginDeclineTeaser(byTool["get_margin_delta"]); t != nil {
		return t
	}
	if t := dayOfMonthSpikeTeaser(byTool["get_expense_pattern_by_day_of_month"]); t != nil {
		return t
	}
	if t := periodLossTeaser(byTool["get_period_totals"]); t != nil {
		return t
	}
	// Additive last check, mirroring flagBasedFollowUp: a get_daily_summary
	// pulled as supporting context for ANY primary tool can carry a real
	// discrepancy flag worth surfacing when nothing narrower matched.
	if t := dailyFlagTeaser(byTool["get_daily_summary"]); t != nil {
		return t
	}
	return nil
}

// --- compare_platform_economics → high_commission ---------------------

// platformRatesJSON captures just the per-platform effective_rate out of
// PlatformComparisonResult — its own narrow parse struct, matching the
// package convention (suggestions.go's platformComparisonPeriodJSON doc
// comment) of parsing only the fields a given consumer needs.
type platformRatesJSON struct {
	Platforms []struct {
		EffectiveRate *string `json:"effective_rate"`
	} `json:"platforms"`
}

func highCommissionTeaser(results []string) *BusinessInsightTeaser {
	for _, raw := range results {
		var parsed platformRatesJSON
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}
		for _, p := range parsed.Platforms {
			// nil is compare_platform_economics' documented "rate undefined
			// over zero sales" state — never treated as a number here.
			if p.EffectiveRate == nil {
				continue
			}
			bps, ok := parseRatePercentBps(*p.EffectiveRate)
			if !ok {
				continue
			}
			if bps >= highCommissionThresholdBps {
				return &BusinessInsightTeaser{Kind: advisor.KindHighCommission, Title: titleHighCommission}
			}
		}
	}
	return nil
}

// parseRatePercentBps parses effectiveRatePercent's "23.00%"-style output
// into basis points without ever touching float64 (money.ParseFixedPoint,
// the same fixed-point discipline the rate was computed with). This is a
// threshold COMPARISON against an already-computed figure, not a new
// arithmetic path.
func parseRatePercentBps(s string) (int64, bool) {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%"))
	bps, err := money.ParseFixedPoint(s, 2)
	if err != nil {
		return 0, false
	}
	return bps, true
}

// --- get_promotion_roi / list_negative_roi_promotions → negative_promo_roi

func negativePromoTeaser(byTool map[string][]string) *BusinessInsightTeaser {
	var raws []string
	raws = append(raws, byTool["list_negative_roi_promotions"]...)
	raws = append(raws, byTool["get_promotion_roi"]...)

	for _, raw := range raws {
		var parsed promotionsJSON
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}
		for _, p := range parsed.Promotions {
			// flagged_negative is the deterministic layer's own verdict
			// (roi computed and negative) — the only trigger used, never a
			// re-computation of ROI here.
			if p.FlaggedNegative {
				return &BusinessInsightTeaser{Kind: advisor.KindNegativePromoROI, Title: titleNegativePromoROI}
			}
		}
	}
	return nil
}

// --- list_discrepancies / get_daily_summary → discrepancy_pattern ------

func discrepancyListTeaser(results []string) *BusinessInsightTeaser {
	for _, raw := range results {
		var parsed discrepanciesJSON
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}
		// ListDiscrepancies only ever includes flagged days (its own doc:
		// "the tool surfaces exceptions, not a full calendar"), so any
		// entry at all is a real flag. A clean result (Days empty) is a
		// good answer with nothing to advise on.
		if len(parsed.Days) > 0 {
			return &BusinessInsightTeaser{Kind: advisor.KindDiscrepancyPattern, Title: titleDiscrepancyPattern}
		}
	}
	return nil
}

func dailyFlagTeaser(results []string) *BusinessInsightTeaser {
	for _, raw := range results {
		// discrepancyFlagsJSON is suggestions.go's existing narrow view of
		// DailySummaryResult — reused, not redeclared.
		var parsed discrepancyFlagsJSON
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}
		if len(parsed.DiscrepancyFlags) > 0 {
			return &BusinessInsightTeaser{Kind: advisor.KindDiscrepancyPattern, Title: titleDiscrepancyPattern}
		}
	}
	return nil
}

// --- get_margin_delta / get_period_totals → margin_decline -------------

func marginDeclineTeaser(results []string) *BusinessInsightTeaser {
	for _, raw := range results {
		var parsed marginDeltaJSON
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}
		deltaCents, err := money.ParseCents(parsed.DeltaMarginTotal)
		if err != nil {
			continue
		}
		periodACents, err := money.ParseCents(parsed.PeriodA.MarginTotal)
		if err != nil {
			continue
		}
		if deltaCents >= 0 {
			continue // improved or flat — nothing to advise on
		}
		// Materiality: |decline| >= marginDeclineMaterialityPercent% of
		// |period A's margin|, in integer cents (no float64). A period A
		// margin of zero makes any decline material by this test, which is
		// the honest reading of "went from breaking even to losing money".
		decline := -deltaCents
		baseline := periodACents
		if baseline < 0 {
			baseline = -baseline
		}
		if decline*100 >= marginDeclineMaterialityPercent*baseline {
			return &BusinessInsightTeaser{Kind: advisor.KindMarginDecline, Title: titleMarginDecline}
		}
	}
	return nil
}

// periodTotalsMarginJSON captures just margin_total out of
// PeriodTotalsResult — suggestions.go's periodTotalsSummaryJSON
// deliberately doesn't carry it (its follow-ups have no use for the
// figure), so this file parses its own narrower view, per the package
// convention.
type periodTotalsMarginJSON struct {
	MarginTotal string `json:"margin_total"`
}

// periodLossTeaser triggers margin_decline for a period that ran at a
// real computed loss — no threshold needed: a negative period margin is a
// deterministic fact, not a judgment call, and always worth the owner's
// attention.
func periodLossTeaser(results []string) *BusinessInsightTeaser {
	for _, raw := range results {
		var parsed periodTotalsMarginJSON
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}
		cents, err := money.ParseCents(parsed.MarginTotal)
		if err != nil {
			continue
		}
		if cents < 0 {
			return &BusinessInsightTeaser{Kind: advisor.KindMarginDecline, Title: titlePeriodLoss}
		}
	}
	return nil
}

// --- get_expense_pattern_by_day_of_month → day_of_month_expense_spike --

// dayOfMonthPatternJSON captures just the fields the spike rule needs out
// of ExpensePatternByDayOfMonthResult.
type dayOfMonthPatternJSON struct {
	Pattern []struct {
		AvgExpense  string `json:"avg_expense"`
		Occurrences int    `json:"occurrences"`
	} `json:"pattern"`
	HighestExpenseDay struct {
		AvgExpense  string `json:"avg_expense"`
		Occurrences int    `json:"occurrences"`
	} `json:"highest_expense_day"`
}

func dayOfMonthSpikeTeaser(results []string) *BusinessInsightTeaser {
	for _, raw := range results {
		var parsed dayOfMonthPatternJSON
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}
		if len(parsed.Pattern) < 2 || parsed.HighestExpenseDay.Occurrences < dayOfMonthSpikeMinOccurrences {
			// One lone day-of-month is not a pattern, and a single
			// occurrence is a calendar accident, not recurrence.
			continue
		}
		highestCents, err := money.ParseCents(parsed.HighestExpenseDay.AvgExpense)
		if err != nil {
			continue
		}
		var avgCents []int64
		parseable := true
		for _, p := range parsed.Pattern {
			cents, err := money.ParseCents(p.AvgExpense)
			if err != nil {
				parseable = false
				break
			}
			avgCents = append(avgCents, cents)
		}
		if !parseable {
			continue
		}
		median := medianCents(avgCents)
		// Strict > median guards the degenerate all-equal (or all-zero)
		// pattern; the ratio test is done in integers (highest/median >=
		// num/den ⇔ highest*den >= median*num), never float64.
		if highestCents > median && highestCents*dayOfMonthSpikeRatioDen >= median*dayOfMonthSpikeRatioNum {
			return &BusinessInsightTeaser{Kind: advisor.KindDayOfMonthExpenseSpike, Title: titleDayOfMonthExpenseSpike}
		}
	}
	return nil
}

// medianCents is the standard median: middle element of the sorted slice,
// or the half-up-rounded mean of the two middles for an even count (the
// same DivRoundHalfUp convention every other ratio in this codebase uses).
func medianCents(values []int64) int64 {
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return sorted[n/2]
	}
	return money.DivRoundHalfUp(sorted[n/2-1]+sorted[n/2], 2)
}

// ---------------------------------------------------------------------
// POST /api/business-insight — the on-demand, billed half
// ---------------------------------------------------------------------

// BusinessInsightRequest is POST /api/business-insight's request body:
// the teaser kind the owner tapped, plus the SAME tool_calls data the
// /api/ask response already handed this client (FR-003's "show your
// work" payload, posted back verbatim). Deliberately no server-side
// per-answer state: the client already holds everything the advice needs.
type BusinessInsightRequest struct {
	Kind      string         `json:"kind"`
	ToolCalls []ToolCallView `json:"tool_calls"`
}

// BusinessInsightResponse is POST /api/business-insight's success body.
// Interaction carries the call's real, just-measured cost — an advice
// call must never look free (the same discipline AskResponse.Interactions
// already enforces for the gate/explain pair).
type BusinessInsightResponse struct {
	Kind       string `json:"kind"`
	AdviceText string `json:"advice_text"`
	// Disclaimer is the wire-carried statement of what this content IS —
	// probabilistic guidance, not a computed fact — so every client
	// renders the disclosure rather than each inventing (or omitting) its
	// own, the same pattern CacheMatchNote/ParaphraseMatchNote establish.
	Disclaimer  string          `json:"disclaimer"`
	Interaction CostInteraction `json:"interaction"`
}

// BusinessInsightDisclaimer states, on the wire, what the advice is and
// is not. Shown by the frontend alongside the advice text itself.
const BusinessInsightDisclaimer = "AI suggestion — general industry practice connected to your computed numbers, not a computed fact about your business. Verify against your own contracts and records before acting."

// Adviser is internal/advisor's Advise call as this handler needs it —
// an interface for the same reason Classifier/Narrator are: tests must
// COUNT and CONTROL advice calls without the live Anthropic API.
// *advisor.Advisor satisfies it directly.
type Adviser interface {
	Advise(ctx context.Context, kind string, results []advisor.ToolResult) (*advisor.Advice, error)
}

// BusinessInsightStore is the one write this handler needs — the
// dedicated ledger row (Constitution Principle VI; see migration 000010
// for why this is its own table). *storage.Queries satisfies it directly.
type BusinessInsightStore interface {
	CreateBusinessInsightInteraction(ctx context.Context, arg storage.CreateBusinessInsightInteractionParams) (storage.BusinessInsightInteraction, error)
}

// BusinessInsightDeps are HandleBusinessInsight's dependencies,
// constructed once at process start (cmd/server/main.go), mirroring Deps.
type BusinessInsightDeps struct {
	Adviser Adviser
	Store   BusinessInsightStore
}

// HandleBusinessInsight implements POST /api/business-insight: validate,
// RE-DERIVE the teaser from the posted tool results (never trust the
// client's claimed kind — the same "re-verify before acting on a claim"
// discipline serveFromParaphraseMatch documents), run the one advice
// call, ledger it, and return the advice with its real cost.
func HandleBusinessInsight(deps BusinessInsightDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}

		var req BusinessInsightRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_input", "invalid JSON body")
			return
		}
		if !advisor.KnownKind(req.Kind) {
			writeJSONError(w, http.StatusBadRequest, "invalid_input", fmt.Sprintf("unknown insight kind %q", req.Kind))
			return
		}
		if len(req.ToolCalls) == 0 {
			writeJSONError(w, http.StatusBadRequest, "invalid_input", "tool_calls is required — the advice must be grounded in the answer's own tool results")
			return
		}

		// Re-derivation gate (spec SC-005): the posted tool results must
		// actually produce this kind through the exact same deterministic
		// derivation /api/ask used. A stale, tampered, or mismatched
		// request is refused rather than answered with advice ungrounded
		// in the data it claims to be about.
		invocations := make([]explain.ToolInvocation, 0, len(req.ToolCalls))
		for _, tc := range req.ToolCalls {
			invocations = append(invocations, explain.ToolInvocation{Name: tc.Name, ResultJSON: string(tc.ResultJSON)})
		}
		teaser := deriveBusinessInsightTeaser(invocations)
		if teaser == nil || teaser.Kind != req.Kind {
			writeJSONError(w, http.StatusUnprocessableEntity, "insight_not_supported", "the posted tool results do not support this insight kind")
			return
		}

		toolResults := make([]advisor.ToolResult, 0, len(req.ToolCalls))
		for _, tc := range req.ToolCalls {
			toolResults = append(toolResults, advisor.ToolResult{Name: tc.Name, ResultJSON: string(tc.ResultJSON)})
		}

		advice, err := deps.Adviser.Advise(r.Context(), req.Kind, toolResults)
		if err != nil {
			// advisor.Advise's error contract is (nil, err) on every
			// failure path — like ambiguity.Gate.Classify, there is no
			// partial Advice to pull tokens out of and log here.
			log.Printf("httpapi: business-insight advice failed (kind=%q): %v", req.Kind, err)
			writeJSONError(w, http.StatusBadGateway, "advice_failed", "the advice call failed; please try again")
			return
		}

		logBusinessInsightOrWarn(r.Context(), deps.Store, req, advice)

		writeJSON(w, http.StatusOK, BusinessInsightResponse{
			Kind:       req.Kind,
			AdviceText: advice.Text,
			Disclaimer: BusinessInsightDisclaimer,
			Interaction: CostInteraction{
				ModelUsed:        llmclient.ModelBusinessInsight,
				InputTokens:      advice.InputTokens,
				OutputTokens:     advice.OutputTokens,
				EstimatedCostUSD: advice.EstimatedCostUSD,
				LatencyMs:        advice.LatencyMs,
			},
		})
	}
}

// logBusinessInsightOrWarn writes the dedicated ledger row and, on
// failure, logs loudly rather than failing the owner's request — the
// exact treatment Deps.logOrWarn already gives a failed
// question_interaction write (Constitution Principle VI: an unlogged
// model call is a real defect worth surfacing, not a reason to eat the
// answer the owner already paid for).
func logBusinessInsightOrWarn(ctx context.Context, store BusinessInsightStore, req BusinessInsightRequest, advice *advisor.Advice) {
	grounding, err := json.Marshal(req.ToolCalls)
	if err != nil {
		log.Printf("httpapi: FAILED to marshal business-insight grounding (kind=%q): %v", req.Kind, err)
		return
	}
	var costNumeric pgtype.Numeric
	if err := costNumeric.Scan(fmt.Sprintf("%.6f", advice.EstimatedCostUSD)); err != nil {
		log.Printf("httpapi: FAILED to encode business-insight cost (kind=%q): %v", req.Kind, err)
		return
	}
	if _, err := store.CreateBusinessInsightInteraction(ctx, storage.CreateBusinessInsightInteractionParams{
		Kind:               req.Kind,
		GroundingToolCalls: grounding,
		AdviceText:         advice.Text,
		ModelUsed:          llmclient.ModelBusinessInsight,
		InputTokens:        int32(advice.InputTokens),
		OutputTokens:       int32(advice.OutputTokens),
		EstimatedCostUsd:   costNumeric,
		LatencyMs:          int32(advice.LatencyMs),
	}); err != nil {
		log.Printf("httpapi: FAILED to write business_insight_interaction row (kind=%q): %v", req.Kind, err)
	}
}
