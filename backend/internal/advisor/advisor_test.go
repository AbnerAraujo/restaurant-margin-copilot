package advisor

// Tests for the pure, model-independent parts of the advisor package —
// the same split paraphrase's own tests use: prompt composition and kind
// validation are plain functions/data, testable with zero API calls,
// while the one real model call is exercised by the live end-to-end
// verification pass (and its handler-level behavior by internal/httpapi's
// fake-Adviser tests).

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestKnownKindAcceptsExactlyTheFiveInsightKinds(t *testing.T) {
	for _, kind := range []string{
		KindDiscrepancyPattern,
		KindNegativePromoROI,
		KindHighCommission,
		KindDayOfMonthExpenseSpike,
		KindMarginDecline,
	} {
		if !KnownKind(kind) {
			t.Errorf("KnownKind(%q) = false, want true", kind)
		}
	}
	for _, kind := range []string{"", "unknown", "DISCREPANCY_PATTERN", "margin-decline"} {
		if KnownKind(kind) {
			t.Errorf("KnownKind(%q) = true, want false", kind)
		}
	}
}

func TestEveryKindGuidanceForbidsNothingAndGroundsInJSON(t *testing.T) {
	// Structural check on the prompt data itself: every kind must have
	// non-empty guidance that anchors the model to the JSON it was shown
	// (the load-bearing grounding rule of spec FR-011). A kind whose
	// guidance forgot to point back at the data would let the model drift
	// into fully generic content unmoored from the computed pattern.
	for kind, guidance := range kindGuidance {
		if strings.TrimSpace(guidance) == "" {
			t.Errorf("kindGuidance[%q] is empty", kind)
		}
		if !strings.Contains(guidance, "JSON") {
			t.Errorf("kindGuidance[%q] never anchors the advice back to the JSON it was shown", kind)
		}
	}
}

func TestComposeUserMessageCarriesKindAndEveryToolResultVerbatim(t *testing.T) {
	msg := composeUserMessage(KindNegativePromoROI, []ToolResult{
		{Name: "list_negative_roi_promotions", ResultJSON: `{"promotions":[{"campaign_id":"IF-PROMO-002","flagged_negative":true}]}`},
		{Name: "get_period_totals", ResultJSON: `{"start":"2026-08-01","end":"2026-08-07"}`},
	})

	for _, want := range []string{
		"Insight kind: negative_promo_roi",
		"list_negative_roi_promotions",
		`{"promotions":[{"campaign_id":"IF-PROMO-002","flagged_negative":true}]}`,
		"get_period_totals",
		`{"start":"2026-08-01","end":"2026-08-07"}`,
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("composed user message missing %q:\n%s", want, msg)
		}
	}
}

func TestAdviseRefusesUnknownKindWithoutAnyAPICall(t *testing.T) {
	// A nil client would panic if Advise ever reached CreateMessage — the
	// error must fire before any API interaction is even attempted.
	a := New(nil)
	_, err := a.Advise(context.Background(), "not_a_kind", []ToolResult{{Name: "t", ResultJSON: "{}"}})
	if !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("err = %v, want ErrUnknownKind", err)
	}
}

func TestAdviseRefusesEmptyGroundingWithoutAnyAPICall(t *testing.T) {
	a := New(nil)
	_, err := a.Advise(context.Background(), KindMarginDecline, nil)
	if !errors.Is(err, ErrNoToolResults) {
		t.Fatalf("err = %v, want ErrNoToolResults", err)
	}
}
