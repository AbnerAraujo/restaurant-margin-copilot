package httpapi

import (
	"strings"
	"testing"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/explain"
)

// Shared data bounds for every test below, matching this product's real
// fixture coverage window (frontend/src/components/Chat/exampleQuestions.ts'
// COVERAGE_PERIOD) — every generated suggestion in these tests is checked
// against these exact bounds.
const (
	testDataStart = "2026-08-01"
	testDataEnd   = "2026-08-14"
)

// dailySummaryAug14JSONFixture and periodTotals fixtures below are local to
// this file: visualization_test.go's fixtures don't cover a day at the very
// end of the data range (needed to exercise a FULL 7-day zoom-out) or
// get_period_totals at all (visualization.go has no get_period_totals
// rendering to test against).
const (
	dailySummaryAug14JSONFixture = `{"date":"2026-08-14"}`

	periodTotalsJSONFixture = `{"start":"2026-08-01","end":"2026-08-14",
		"best_day":{"date":"2026-08-07","margin":"375.82"},
		"worst_day":{"date":"2026-08-08","margin":"152.50"}}`

	periodTotalsSingleDayJSONFixture = `{"start":"2026-08-05","end":"2026-08-05",
		"best_day":{"date":"2026-08-05","margin":"200.00"},
		"worst_day":{"date":"2026-08-05","margin":"200.00"}}`
)

func TestDeriveFollowUpSuggestionsCoversAllSevenTools(t *testing.T) {
	tests := []struct {
		name          string
		invocations   []explain.ToolInvocation
		askedQuestion string
		wantCount     int
		wantContains  []string // every one of these substrings must appear in SOME suggestion
	}{
		{
			name:         "get_daily_summary mid-range: zoom-in only, zoom-out week would fall before data start",
			invocations:  []explain.ToolInvocation{inv("get_daily_summary", dailySummaryAug07JSONFixture)},
			wantCount:    1,
			wantContains: []string{"2026-08-07"},
		},
		{
			name:        "get_daily_summary at the end of the range: zoom-in and a full week-over-week zoom-out",
			invocations: []explain.ToolInvocation{inv("get_daily_summary", dailySummaryAug14JSONFixture)},
			wantCount:   2,
			wantContains: []string{
				"2026-08-14",
				"2026-08-01 to 2026-08-07",
				"2026-08-08 to 2026-08-14",
			},
		},
		{
			name:        "get_margin_delta grounds all three suggestions in period_b, not period_a",
			invocations: []explain.ToolInvocation{inv("get_margin_delta", marginDeltaJSONFixture)},
			wantCount:   3,
			wantContains: []string{
				"2026-08-08", "2026-08-14",
			},
		},
		{
			name:        "list_discrepancies with flagged days grounds suggestions in the latest flagged date",
			invocations: []explain.ToolInvocation{inv("list_discrepancies", discrepanciesJSONFixture)},
			wantCount:   2,
			wantContains: []string{
				"2026-08-08",
			},
		},
		{
			name:        "list_discrepancies clean period yields nothing — no date to ground a suggestion in",
			invocations: []explain.ToolInvocation{inv("list_discrepancies", cleanDiscrepanciesJSONFixture)},
			wantCount:   0,
		},
		{
			name:        "get_promotion_roi grounds suggestions in the first promotion's real period",
			invocations: []explain.ToolInvocation{inv("get_promotion_roi", promotionsJSONFixture)},
			wantCount:   3,
			wantContains: []string{
				"2026-08-01", "2026-08-07", "iFood", "Just Eat Takeaway",
			},
		},
		{
			name:        "list_negative_roi_promotions uses the identical template",
			invocations: []explain.ToolInvocation{inv("list_negative_roi_promotions", promotionsJSONFixture)},
			wantCount:   3,
			wantContains: []string{
				"2026-08-01", "2026-08-07",
			},
		},
		{
			name:        "compare_platform_economics grounds suggestions in its own period",
			invocations: []explain.ToolInvocation{inv("compare_platform_economics", platformComparisonJSONFixture)},
			wantCount:   3,
			wantContains: []string{
				"2026-08-01", "2026-08-14",
			},
		},
		{
			name:        "get_period_totals points at the real best/worst day it already computed",
			invocations: []explain.ToolInvocation{inv("get_period_totals", periodTotalsJSONFixture)},
			wantCount:   3,
			wantContains: []string{
				"2026-08-07", "2026-08-08", "2026-08-01", "2026-08-14",
			},
		},
		{
			name:        "get_period_totals over a single day skips the duplicate best/worst suggestion",
			invocations: []explain.ToolInvocation{inv("get_period_totals", periodTotalsSingleDayJSONFixture)},
			wantCount:   2,
			wantContains: []string{
				"2026-08-05",
			},
		},
		{
			name:        "no tool calls at all yields an empty slice",
			invocations: nil,
			wantCount:   0,
		},
		{
			name:        "an unrecognized tool yields an empty slice",
			invocations: []explain.ToolInvocation{inv("some_future_tool", `{"whatever":1}`)},
			wantCount:   0,
		},
		{
			name:        "malformed tool JSON is skipped rather than crashing",
			invocations: []explain.ToolInvocation{inv("get_margin_delta", `{not json`)},
			wantCount:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveFollowUpSuggestions(tt.invocations, tt.askedQuestion, testDataStart, testDataEnd)
			if len(got) != tt.wantCount {
				t.Fatalf("got %d suggestions %v, want %d", len(got), got, tt.wantCount)
			}
			joined := strings.Join(got, " | ")
			for _, want := range tt.wantContains {
				if !strings.Contains(joined, want) {
					t.Errorf("suggestions %v: expected some suggestion to contain %q", got, want)
				}
			}
		})
	}
}

func TestDeriveFollowUpSuggestionsNeverExceedsThree(t *testing.T) {
	got := deriveFollowUpSuggestions(
		[]explain.ToolInvocation{inv("get_margin_delta", marginDeltaJSONFixture)},
		"",
		testDataStart, testDataEnd,
	)
	if len(got) > MaxFollowUpSuggestions {
		t.Fatalf("got %d suggestions, want at most %d", len(got), MaxFollowUpSuggestions)
	}
}

func TestDeriveFollowUpSuggestionsFixedPriorityMatchesVisualization(t *testing.T) {
	// Same doctrine as TestDeriveVisualizationPlatformComparisonOutranksPromotions:
	// a question that reached compare_platform_economics is about platform
	// economics even if a promotion tool also ran for context.
	got := deriveFollowUpSuggestions(
		[]explain.ToolInvocation{
			inv("get_promotion_roi", promotionsJSONFixture),
			inv("compare_platform_economics", platformComparisonJSONFixture),
		},
		"",
		testDataStart, testDataEnd,
	)
	joined := strings.Join(got, " | ")
	if !strings.Contains(joined, "negative ROI") {
		t.Errorf("expected compare_platform_economics' own template (mentions negative ROI as a follow-up), got %v", got)
	}
}

func TestDeriveFollowUpSuggestionsClampsOutOfRangePeriodToRealBounds(t *testing.T) {
	// platformComparisonZeroSalesJSONFixture's period (1999-04-01..1999-04-01)
	// is entirely before the real data range — every generated date must be
	// clamped forward into [testDataStart, testDataEnd], never left at 1999.
	got := deriveFollowUpSuggestions(
		[]explain.ToolInvocation{inv("compare_platform_economics", platformComparisonZeroSalesJSONFixture)},
		"",
		testDataStart, testDataEnd,
	)
	if len(got) == 0 {
		t.Fatal("expected clamped suggestions, got none")
	}
	for _, s := range got {
		if strings.Contains(s, "1999") {
			t.Errorf("suggestion %q leaked an out-of-range date instead of clamping to %s", s, testDataStart)
		}
		if !strings.Contains(s, testDataStart) {
			t.Errorf("suggestion %q was not clamped to the real data start %s", s, testDataStart)
		}
	}
}

func TestDeriveFollowUpSuggestionsNeverRepeatsTheQuestionJustAsked(t *testing.T) {
	baseline := deriveFollowUpSuggestions(
		[]explain.ToolInvocation{inv("get_margin_delta", marginDeltaJSONFixture)},
		"",
		testDataStart, testDataEnd,
	)
	if len(baseline) < 2 {
		t.Fatalf("need at least 2 baseline suggestions to test exclusion, got %v", baseline)
	}
	asked := baseline[0]

	got := deriveFollowUpSuggestions(
		[]explain.ToolInvocation{inv("get_margin_delta", marginDeltaJSONFixture)},
		// Loose, case/whitespace-insensitive match per this file's documented
		// policy — not the exact same casing as the generated suggestion.
		"  "+strings.ToUpper(asked)+"  ",
		testDataStart, testDataEnd,
	)
	for _, s := range got {
		if strings.EqualFold(s, asked) {
			t.Fatalf("suggestion %q repeats the question just asked (%q); it must be excluded", s, asked)
		}
	}
	if len(got) != len(baseline)-1 {
		t.Fatalf("expected exactly one suggestion excluded, got %d (baseline had %d): %v", len(got), len(baseline), got)
	}
}

func TestDeriveFollowUpSuggestionsInvalidDataBoundsYieldsEmpty(t *testing.T) {
	got := deriveFollowUpSuggestions(
		[]explain.ToolInvocation{inv("get_daily_summary", dailySummaryAug07JSONFixture)},
		"",
		"not-a-date", testDataEnd,
	)
	if len(got) != 0 {
		t.Fatalf("expected no suggestions with an unparseable data start, got %v", got)
	}
}

func TestFinalizeSuggestionsDedupesAndCapsAtThree(t *testing.T) {
	raw := []string{
		"Question A?",
		"question a?", // case-insensitive duplicate of the above
		"Question B?",
		"Question C?",
		"Question D?", // beyond the cap
	}
	got := finalizeSuggestions(raw, "")
	if len(got) != MaxFollowUpSuggestions {
		t.Fatalf("got %d suggestions, want %d: %v", len(got), MaxFollowUpSuggestions, got)
	}
	want := []string{"Question A?", "Question B?", "Question C?"}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("got[%d] = %q, want %q", i, got[i], w)
		}
	}
}

// TestDeriveFollowUpSuggestionsIncludesFlagBasedFollowUpWhenDiscrepancyFlagsPresent
// covers spec 008 FR-002: a get_daily_summary result carrying a real
// discrepancy flag must offer a "why is this different from usual?"
// suggestion, grounded in that real flagged date, competing for one of the
// existing MaxFollowUpSuggestions slots rather than being added on top.
func TestDeriveFollowUpSuggestionsIncludesFlagBasedFollowUpWhenDiscrepancyFlagsPresent(t *testing.T) {
	got := deriveFollowUpSuggestions(
		[]explain.ToolInvocation{inv("get_daily_summary", dailySummaryAug08JSONFixture)},
		"",
		testDataStart, testDataEnd,
	)
	if len(got) > MaxFollowUpSuggestions {
		t.Fatalf("got %d suggestions, want at most %d", len(got), MaxFollowUpSuggestions)
	}
	joined := strings.Join(got, " | ")
	if !strings.Contains(joined, "Why is 2026-08-08 different from usual?") {
		t.Errorf("expected a flag-based follow-up naming the real flagged date 2026-08-08, got %v", got)
	}
}

// TestDeriveFollowUpSuggestionsOmitsFlagBasedFollowUpWhenNoFlags covers
// spec 008 FR-002's acceptance scenario 4: a clean day (no discrepancy
// flags) must never manufacture a flag-based question.
func TestDeriveFollowUpSuggestionsOmitsFlagBasedFollowUpWhenNoFlags(t *testing.T) {
	got := deriveFollowUpSuggestions(
		[]explain.ToolInvocation{inv("get_daily_summary", dailySummaryAug07JSONFixture)},
		"",
		testDataStart, testDataEnd,
	)
	joined := strings.Join(got, " | ")
	if strings.Contains(joined, "different from usual") {
		t.Errorf("expected no flag-based follow-up for a clean day (no discrepancy_flags), got %v", got)
	}
}

// TestFlagBasedFollowUpUsesLatestFlaggedDateAcrossMultipleResults proves the
// same "latest wins" convention dailySummaryFollowUps already uses when
// several get_daily_summary calls happened in one interaction (e.g. a
// multi-day chart) and more than one carries a flag.
func TestFlagBasedFollowUpUsesLatestFlaggedDateAcrossMultipleResults(t *testing.T) {
	earlierFlagged := `{"date":"2026-08-03","discrepancy_flags":[{"type":"commission_mismatch","detail":"stated 8.10, recomputed 8.35"}]}`
	got := flagBasedFollowUp([]string{dailySummaryAug08JSONFixture, earlierFlagged})
	want := "Why is 2026-08-08 different from usual?"
	if got != want {
		t.Errorf("flagBasedFollowUp() = %q, want %q", got, want)
	}
}

// TestFlagBasedFollowUpEmptyOnNoResults proves the zero-invocation case
// returns "" (never a placeholder), matching this file's own "empty slice,
// never nil-treated-specially" discipline.
func TestFlagBasedFollowUpEmptyOnNoResults(t *testing.T) {
	if got := flagBasedFollowUp(nil); got != "" {
		t.Errorf("flagBasedFollowUp(nil) = %q, want empty", got)
	}
}
