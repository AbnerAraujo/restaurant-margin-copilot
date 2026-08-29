package httpapi

// Tests for specs/009-business-insight-advisor's DETERMINISTIC half:
// deriveBusinessInsightTeaser must fire on exactly the five documented
// patterns (including their exact threshold boundaries), stay silent on
// clean/below-threshold/unparseable data, resolve multi-tool answers
// through the fixed priority order, and — through the real ask handler —
// ride AskResponse.business_insight without changing anything else about
// the response (SC-001: zero added model calls, zero added cost).
//
// Fixtures are real tool-result-shaped JSON, suggestions_test.go's style —
// never mocks of internal state.

import (
	"strconv"
	"testing"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/advisor"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/explain"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/instrumentation"
)

// --- fixtures ----------------------------------------------------------

func platformComparisonJSONWithRates(ifoodRate, jetRate string) string {
	return `{"period":{"start":"2026-08-01","end":"2026-08-07"},"days_included":7,"platforms":[` +
		`{"source":"ifood","display_name":"iFood","gross_sales":"1000.00","commission_paid":"225.40","effective_rate":"` + ifoodRate + `","promo_spend":"0.00","combined_cost":"225.40","combined_effective_rate":"` + ifoodRate + `","source_row_refs":[]},` +
		`{"source":"just_eat_takeaway","display_name":"Just Eat Takeaway","gross_sales":"800.00","commission_paid":"156.88","effective_rate":"` + jetRate + `","promo_spend":"0.00","combined_cost":"156.88","combined_effective_rate":"` + jetRate + `","source_row_refs":[]}]}`
}

const flaggedDailySummaryJSON = `{"date":"2026-08-03","margin":"-120.26","discrepancy_flags":[{"type":"duplicate_order_removed","detail":"Removed duplicate order IFOOD-20260803-0011."}]}`

const cleanDailySummaryJSON = `{"date":"2026-08-04","margin":"375.82","discrepancy_flags":[]}`

func TestDeriveBusinessInsightTeaser_HighCommission(t *testing.T) {
	cases := []struct {
		name      string
		ifoodRate string
		want      bool
	}{
		{"above threshold fires", "22.54%", true},
		{"exactly at the 20.00% threshold fires (documented >= cut)", "20.00%", true},
		{"just below threshold stays silent", "19.99%", false},
		{"entry-tier rate stays silent", "15.00%", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			teaser := deriveBusinessInsightTeaser([]explain.ToolInvocation{
				{Name: "compare_platform_economics", ResultJSON: platformComparisonJSONWithRates(tc.ifoodRate, "15.00%")},
			})
			if tc.want {
				if teaser == nil || teaser.Kind != advisor.KindHighCommission {
					t.Fatalf("teaser = %+v, want kind %q", teaser, advisor.KindHighCommission)
				}
				if teaser.Title == "" {
					t.Error("teaser title is empty")
				}
			} else if teaser != nil {
				t.Fatalf("teaser = %+v, want nil for rate %s", teaser, tc.ifoodRate)
			}
		})
	}
}

func TestDeriveBusinessInsightTeaser_HighCommissionIgnoresNullRates(t *testing.T) {
	// effective_rate: null is compare_platform_economics' documented
	// "undefined over zero sales" state — never treated as a number.
	teaser := deriveBusinessInsightTeaser([]explain.ToolInvocation{
		{Name: "compare_platform_economics", ResultJSON: `{"platforms":[{"source":"ifood","display_name":"iFood","effective_rate":null}]}`},
	})
	if teaser != nil {
		t.Fatalf("teaser = %+v, want nil for a null rate", teaser)
	}
}

func TestDeriveBusinessInsightTeaser_NegativePromoROI(t *testing.T) {
	flagged := `{"promotions":[{"platform":"ifood","campaign_id":"IF-PROMO-002","period":{"start":"2026-08-01","end":"2026-08-07"},"spend":"120.00","attributed_incremental_orders":8,"attributed_incremental_revenue":"64.00","roi":"-0.47","flagged_negative":true,"source_row_refs":[]}]}`
	clean := `{"promotions":[{"platform":"ifood","campaign_id":"IF-PROMO-001","period":{"start":"2026-08-01","end":"2026-08-07"},"spend":"50.00","attributed_incremental_orders":40,"attributed_incremental_revenue":"400.00","roi":"7.00","flagged_negative":false,"source_row_refs":[]}]}`

	for _, tool := range []string{"list_negative_roi_promotions", "get_promotion_roi"} {
		teaser := deriveBusinessInsightTeaser([]explain.ToolInvocation{{Name: tool, ResultJSON: flagged}})
		if teaser == nil || teaser.Kind != advisor.KindNegativePromoROI {
			t.Fatalf("tool %s: teaser = %+v, want kind %q", tool, teaser, advisor.KindNegativePromoROI)
		}
	}

	if teaser := deriveBusinessInsightTeaser([]explain.ToolInvocation{{Name: "get_promotion_roi", ResultJSON: clean}}); teaser != nil {
		t.Fatalf("teaser = %+v, want nil for a positive-ROI promotion", teaser)
	}
}

func TestDeriveBusinessInsightTeaser_DiscrepancyPattern(t *testing.T) {
	flaggedList := `{"days_checked":7,"days":[{"date":"2026-08-03","flags":[{"type":"duplicate_order_removed","detail":"dup"}],"source_row_refs":[]}]}`
	cleanList := `{"days_checked":7,"days":[]}`

	if teaser := deriveBusinessInsightTeaser([]explain.ToolInvocation{{Name: "list_discrepancies", ResultJSON: flaggedList}}); teaser == nil || teaser.Kind != advisor.KindDiscrepancyPattern {
		t.Fatalf("teaser = %+v, want kind %q from list_discrepancies", teaser, advisor.KindDiscrepancyPattern)
	}
	if teaser := deriveBusinessInsightTeaser([]explain.ToolInvocation{{Name: "list_discrepancies", ResultJSON: cleanList}}); teaser != nil {
		t.Fatalf("teaser = %+v, want nil for a clean discrepancy result", teaser)
	}

	// The additive daily-summary check (mirroring flagBasedFollowUp): a
	// flag on a supporting get_daily_summary fires when nothing narrower
	// matched; a clean day never does.
	if teaser := deriveBusinessInsightTeaser([]explain.ToolInvocation{{Name: "get_daily_summary", ResultJSON: flaggedDailySummaryJSON}}); teaser == nil || teaser.Kind != advisor.KindDiscrepancyPattern {
		t.Fatalf("teaser = %+v, want kind %q from a flagged daily summary", teaser, advisor.KindDiscrepancyPattern)
	}
	if teaser := deriveBusinessInsightTeaser([]explain.ToolInvocation{{Name: "get_daily_summary", ResultJSON: cleanDailySummaryJSON}}); teaser != nil {
		t.Fatalf("teaser = %+v, want nil for a clean day", teaser)
	}
}

func TestDeriveBusinessInsightTeaser_MarginDecline(t *testing.T) {
	marginDelta := func(periodAMargin, delta string) string {
		return `{"period_a":{"start":"2026-07-01","end":"2026-07-07","days_included":7,"margin_total":"` + periodAMargin + `"},"period_b":{"start":"2026-08-01","end":"2026-08-07","days_included":7,"margin_total":"900.00"},"delta_margin_total":"` + delta + `"}`
	}

	cases := []struct {
		name    string
		periodA string
		delta   string
		want    bool
	}{
		{"material decline fires", "1000.00", "-100.00", true},
		{"exactly 5% of period A fires (documented >= cut)", "1000.00", "-50.00", true},
		{"just under 5% stays silent", "1000.00", "-49.99", false},
		{"improvement stays silent", "1000.00", "100.00", false},
		{"flat stays silent", "1000.00", "0.00", false},
		{"any decline from a zero baseline fires", "0.00", "-1.00", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			teaser := deriveBusinessInsightTeaser([]explain.ToolInvocation{
				{Name: "get_margin_delta", ResultJSON: marginDelta(tc.periodA, tc.delta)},
			})
			if tc.want {
				if teaser == nil || teaser.Kind != advisor.KindMarginDecline {
					t.Fatalf("teaser = %+v, want kind %q", teaser, advisor.KindMarginDecline)
				}
			} else if teaser != nil {
				t.Fatalf("teaser = %+v, want nil", teaser)
			}
		})
	}
}

func TestDeriveBusinessInsightTeaser_PeriodLoss(t *testing.T) {
	loss := `{"start":"2026-08-01","end":"2026-08-07","days_included":7,"margin_total":"-50.00","best_day":{"date":"2026-08-02"},"worst_day":{"date":"2026-08-03"}}`
	profit := `{"start":"2026-08-01","end":"2026-08-07","days_included":7,"margin_total":"1200.00","best_day":{"date":"2026-08-02"},"worst_day":{"date":"2026-08-03"}}`

	if teaser := deriveBusinessInsightTeaser([]explain.ToolInvocation{{Name: "get_period_totals", ResultJSON: loss}}); teaser == nil || teaser.Kind != advisor.KindMarginDecline {
		t.Fatalf("teaser = %+v, want kind %q for a loss-making period", teaser, advisor.KindMarginDecline)
	}
	if teaser := deriveBusinessInsightTeaser([]explain.ToolInvocation{{Name: "get_period_totals", ResultJSON: profit}}); teaser != nil {
		t.Fatalf("teaser = %+v, want nil for a profitable period", teaser)
	}
}

func TestDeriveBusinessInsightTeaser_DayOfMonthExpenseSpike(t *testing.T) {
	pattern := func(highestAvg string, highestOccurrences int) string {
		return `{"start":"2026-05-01","end":"2026-07-31","days_included":92,"pattern":[` +
			`{"day_of_month":1,"avg_expense":"` + highestAvg + `","occurrences":` + strconv.Itoa(highestOccurrences) + `},` +
			`{"day_of_month":2,"avg_expense":"100.00","occurrences":3},` +
			`{"day_of_month":3,"avg_expense":"100.00","occurrences":3}],` +
			`"highest_expense_day":{"day_of_month":1,"avg_expense":"` + highestAvg + `","occurrences":` + strconv.Itoa(highestOccurrences) + `},` +
			`"lowest_expense_day":{"day_of_month":2,"avg_expense":"100.00","occurrences":3},"source_row_refs":[]}`
	}

	cases := []struct {
		name        string
		highest     string
		occurrences int
		want        bool
	}{
		{"clear spike fires", "200.00", 3, true},
		{"exactly 1.5x the median fires (documented >= cut)", "150.00", 3, true},
		{"just under 1.5x stays silent", "149.99", 3, false},
		{"single occurrence is a calendar accident, not a pattern", "200.00", 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			teaser := deriveBusinessInsightTeaser([]explain.ToolInvocation{
				{Name: "get_expense_pattern_by_day_of_month", ResultJSON: pattern(tc.highest, tc.occurrences)},
			})
			if tc.want {
				if teaser == nil || teaser.Kind != advisor.KindDayOfMonthExpenseSpike {
					t.Fatalf("teaser = %+v, want kind %q", teaser, advisor.KindDayOfMonthExpenseSpike)
				}
			} else if teaser != nil {
				t.Fatalf("teaser = %+v, want nil", teaser)
			}
		})
	}

	// An all-equal pattern (highest == median) must never trigger — the
	// strict > median guard.
	flat := `{"pattern":[{"day_of_month":1,"avg_expense":"100.00","occurrences":3},{"day_of_month":2,"avg_expense":"100.00","occurrences":3}],"highest_expense_day":{"day_of_month":1,"avg_expense":"100.00","occurrences":3}}`
	if teaser := deriveBusinessInsightTeaser([]explain.ToolInvocation{{Name: "get_expense_pattern_by_day_of_month", ResultJSON: flat}}); teaser != nil {
		t.Fatalf("teaser = %+v, want nil for a flat pattern", teaser)
	}
}

func TestDeriveBusinessInsightTeaser_PriorityOrderAndFallThrough(t *testing.T) {
	// Both a high commission rate AND a flagged supporting daily summary:
	// the narrower platform insight wins the single teaser slot.
	teaser := deriveBusinessInsightTeaser([]explain.ToolInvocation{
		{Name: "compare_platform_economics", ResultJSON: platformComparisonJSONWithRates("22.54%", "19.61%")},
		{Name: "get_daily_summary", ResultJSON: flaggedDailySummaryJSON},
	})
	if teaser == nil || teaser.Kind != advisor.KindHighCommission {
		t.Fatalf("teaser = %+v, want the higher-priority kind %q", teaser, advisor.KindHighCommission)
	}

	// A platform comparison that RAN but shows unremarkable rates must
	// fall through to the flagged daily summary rather than swallowing
	// the derivation — the reason this is a sequence, not a switch.
	teaser = deriveBusinessInsightTeaser([]explain.ToolInvocation{
		{Name: "compare_platform_economics", ResultJSON: platformComparisonJSONWithRates("15.00%", "16.00%")},
		{Name: "get_daily_summary", ResultJSON: flaggedDailySummaryJSON},
	})
	if teaser == nil || teaser.Kind != advisor.KindDiscrepancyPattern {
		t.Fatalf("teaser = %+v, want fall-through to %q", teaser, advisor.KindDiscrepancyPattern)
	}
}

func TestDeriveBusinessInsightTeaser_NilForUnrecognizedEmptyAndUnparseable(t *testing.T) {
	if teaser := deriveBusinessInsightTeaser(nil); teaser != nil {
		t.Fatalf("teaser = %+v, want nil for no invocations", teaser)
	}
	if teaser := deriveBusinessInsightTeaser([]explain.ToolInvocation{{Name: "get_daily_summary", ResultJSON: `not json at all`}}); teaser != nil {
		t.Fatalf("teaser = %+v, want nil for unparseable JSON", teaser)
	}
	if teaser := deriveBusinessInsightTeaser([]explain.ToolInvocation{{Name: "some_future_tool", ResultJSON: `{"flagged_negative":true}`}}); teaser != nil {
		t.Fatalf("teaser = %+v, want nil for an unrecognized tool", teaser)
	}
}

// --- through the real ask handler (SC-001) ------------------------------

func TestAnsweredQuestionCarriesBusinessInsightTeaserWithoutChangingCost(t *testing.T) {
	h := newAskHarness(t)
	h.explainer.result.ToolInvocations = []explain.ToolInvocation{
		{Name: "get_daily_summary", ResultJSON: flaggedDailySummaryJSON},
	}

	response := h.ask(t, "How did we do on 2026-08-03?")

	if response.Status != "answered" {
		t.Fatalf("status = %q, want answered", response.Status)
	}
	if response.BusinessInsight == nil || response.BusinessInsight.Kind != advisor.KindDiscrepancyPattern {
		t.Fatalf("business_insight = %+v, want kind %q", response.BusinessInsight, advisor.KindDiscrepancyPattern)
	}
	if response.BusinessInsight.Title == "" {
		t.Error("business_insight.title is empty")
	}
	// SC-001: the teaser added no model call — exactly the same gate +
	// explain pair, and exactly one call each, as any other answer.
	if len(response.Interactions) != 2 {
		t.Errorf("interactions = %d entries, want the unchanged gate+explain pair", len(response.Interactions))
	}
	if h.gate.calls != 1 || h.explainer.calls != 1 {
		t.Errorf("gate calls = %d, explainer calls = %d, want 1 and 1", h.gate.calls, h.explainer.calls)
	}
}

func TestAnsweredQuestionOverCleanDataOmitsBusinessInsight(t *testing.T) {
	h := newAskHarness(t)
	h.explainer.result.ToolInvocations = []explain.ToolInvocation{
		{Name: "get_daily_summary", ResultJSON: cleanDailySummaryJSON},
	}

	response := h.ask(t, "How did we do on 2026-08-04?")

	if response.BusinessInsight != nil {
		t.Fatalf("business_insight = %+v, want omitted for clean data", response.BusinessInsight)
	}
}

func TestRefusedQuestionNeverCarriesBusinessInsight(t *testing.T) {
	h := newAskHarness(t)
	h.gate.decision.Result = instrumentation.GateUnanswerable
	h.gate.decision.RefusalReason = "This data isn't on file."

	response := h.ask(t, "What will margin be next year?")

	if response.Status != "refused" {
		t.Fatalf("status = %q, want refused", response.Status)
	}
	if response.BusinessInsight != nil {
		t.Fatalf("business_insight = %+v, want omitted on a refusal", response.BusinessInsight)
	}
}
