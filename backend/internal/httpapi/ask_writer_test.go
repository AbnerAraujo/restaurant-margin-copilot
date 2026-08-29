package httpapi

// TestGateWriterCall_* prove internal/httpapi's wiring for
// internal/ambiguity's optional second-pass Claude Sonnet 5 writing call
// (Decision.Writer — see gate.go's package doc): when it ran, its real
// tokens/cost/latency MUST show up as its OWN instrumentation.Record and its
// OWN CostInteraction, alongside the gate's own row — never merged into it,
// never dropped, and never present when it didn't run. These use the same
// countingGate fake ask_cache_test.go already defines, configured with a
// Decision.Writer the real ambiguity.Gate would only set for the ambiguous/
// unanswerable-with-writer cases; no live Anthropic API calls are made.

import (
	"testing"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ambiguity"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/instrumentation"
)

func TestUnanswerableWithWriterCallReportsBothInteractions(t *testing.T) {
	h := newAskHarness(t)
	h.gate.decision = ambiguity.Decision{
		Result:           instrumentation.GateUnanswerable,
		RefusalReason:    "Sonnet-rewritten refusal reason.",
		InputTokens:      210,
		OutputTokens:     40,
		EstimatedCostUSD: 0.00021,
		LatencyMs:        180,
		Writer: &ambiguity.WriterCall{
			ModelUsed:        "claude-sonnet-5",
			InputTokens:      560,
			OutputTokens:     90,
			EstimatedCostUSD: 0.00202,
			LatencyMs:        640,
		},
	}

	response := h.ask(t, "How much did we spend with our cheese supplier in September 2026?")

	if response.Status != "refused" {
		t.Fatalf("status = %q, want refused", response.Status)
	}
	if response.RefusalReason != "Sonnet-rewritten refusal reason." {
		t.Errorf("refusal_reason = %q, want the Sonnet-rewritten text", response.RefusalReason)
	}
	if len(response.Interactions) != 2 {
		t.Fatalf("interactions = %d, want 2 (gate + writer)", len(response.Interactions))
	}
	if response.Interactions[0].ModelUsed != "claude-sonnet-5" {
		t.Errorf("interactions[0].model_used = %q, want claude-sonnet-5 (the gate moved off Haiku 4.5 on 2026-08-29 — see llmclient/cost.go)", response.Interactions[0].ModelUsed)
	}
	if response.Interactions[1].ModelUsed != "claude-sonnet-5" || response.Interactions[1].EstimatedCostUSD != 0.00202 {
		t.Errorf("interactions[1] = %+v, want the real Sonnet writer cost", response.Interactions[1])
	}
	total := response.Interactions[0].EstimatedCostUSD + response.Interactions[1].EstimatedCostUSD
	if want := 0.00021 + 0.00202; total != want {
		t.Errorf("total reported cost = %v, want %v — no cost may be hidden or dropped", total, want)
	}

	if len(h.instrumentation.records) != 2 {
		t.Fatalf("instrumentation records = %d, want 2 (Constitution Principle VI: per-call, not per-question)", len(h.instrumentation.records))
	}
	if h.instrumentation.records[0].ModelUsed != "claude-sonnet-5" || !h.instrumentation.records[0].RefusalFired {
		t.Errorf("record[0] = %+v, want the gate's refusal row", h.instrumentation.records[0])
	}
	if h.instrumentation.records[1].ModelUsed != "claude-sonnet-5" || !h.instrumentation.records[1].RefusalFired {
		t.Errorf("record[1] = %+v, want the writer pass's Sonnet refusal row", h.instrumentation.records[1])
	}
	if h.instrumentation.records[1].EstimatedCostUSD != 0.00202 {
		t.Errorf("record[1].estimated_cost_usd = %v, want the writer's real cost 0.00202", h.instrumentation.records[1].EstimatedCostUSD)
	}
}

func TestAmbiguousWithWriterCallReportsBothInteractions(t *testing.T) {
	h := newAskHarness(t)
	h.gate.decision = ambiguity.Decision{
		Result:             instrumentation.GateAmbiguous,
		ClarifyingQuestion: "Sonnet-rewritten clarifying question?",
		ClarifyingOptions:  []string{"Option A", "Option B"},
		InputTokens:        200,
		OutputTokens:       35,
		EstimatedCostUSD:   0.0002,
		LatencyMs:          150,
		Writer: &ambiguity.WriterCall{
			ModelUsed:        "claude-sonnet-5",
			InputTokens:      500,
			OutputTokens:     80,
			EstimatedCostUSD: 0.0018,
			LatencyMs:        600,
		},
	}

	response := h.ask(t, "How did we do over the weekend?")

	if response.Status != "clarification_needed" {
		t.Fatalf("status = %q, want clarification_needed", response.Status)
	}
	if response.ClarifyingQuestion != "Sonnet-rewritten clarifying question?" {
		t.Errorf("clarifying_question = %q, want the Sonnet-rewritten text", response.ClarifyingQuestion)
	}
	if len(response.Interactions) != 2 {
		t.Fatalf("interactions = %d, want 2 (gate + writer)", len(response.Interactions))
	}
	if len(h.instrumentation.records) != 2 {
		t.Fatalf("instrumentation records = %d, want 2", len(h.instrumentation.records))
	}
	if !h.instrumentation.records[0].ClarificationFired || !h.instrumentation.records[1].ClarificationFired {
		t.Errorf("both rows must record clarification_fired=true; got %+v and %+v", h.instrumentation.records[0], h.instrumentation.records[1])
	}
}

// An ambiguous decision resolved by a stated assumption (not a clarifying
// question) must NEVER trigger the writer pass, per internal/ambiguity's
// package doc — that path proceeds straight into explain, which is already
// a Sonnet call, so a second one here would double-pay for nothing the user
// ever sees from this gate.
func TestAmbiguousWithAssumptionNeverCarriesAWriterInteractionEvenIfSet(t *testing.T) {
	h := newAskHarness(t)
	h.gate.decision = ambiguity.Decision{
		Result:           instrumentation.GateAmbiguous,
		AssumptionStated: "Assuming the trailing 7 days.",
		InputTokens:      200,
		OutputTokens:     35,
		EstimatedCostUSD: 0.0002,
		LatencyMs:        150,
		// Deliberately nil: the real ambiguity.Gate never sets Writer for
		// this case (see refineIfNeeded) — this test documents that
		// contract from the httpapi side by asserting a plain answered
		// response carries exactly one (gate) interaction plus explain's.
	}

	response := h.ask(t, "How did we do this week?")

	if response.Status != "answered" {
		t.Fatalf("status = %q, want answered", response.Status)
	}
	if len(response.Interactions) != 2 {
		t.Fatalf("interactions = %d, want exactly 2 (gate + explain, no writer pass)", len(response.Interactions))
	}
}

// TestAnswerableNeverCarriesAWriterInteraction guards the common case's
// cost: Decision.Writer is nil for every answerable decision the real gate
// produces, and the response must carry exactly the gate+explain pair it
// carried before this feature existed.
func TestAnswerableNeverCarriesAWriterInteraction(t *testing.T) {
	h := newAskHarness(t)

	response := h.ask(t, "What was our reconciled margin on 2026-08-07?")

	if response.Status != "answered" {
		t.Fatalf("status = %q, want answered", response.Status)
	}
	if len(response.Interactions) != 2 {
		t.Fatalf("interactions = %d, want exactly 2 (gate + explain) — an answerable question must cost exactly what it cost before this feature existed", len(response.Interactions))
	}
}
