package httpapi

// Tests for how HandleAsk records a refusal decided by the ambiguity
// package's deterministic date-range pre-check: no model ran, so the
// interaction must be logged under ambiguity.PrecheckModelLabel with
// genuinely zero token/cost figures — never as a phantom zero-token call
// attributed to the gate's real model.

import (
	"testing"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ambiguity"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/instrumentation"
)

func TestDeterministicPrecheckRefusalIsRecordedAsNoModel(t *testing.T) {
	h := newAskHarness(t)
	h.gate.decision = ambiguity.Decision{
		Result:                instrumentation.GateUnanswerable,
		RefusalReason:         `This product has reconciled data for 2026-08-01 through 2026-08-14 only — "July 2023" falls entirely outside that range, so there is no data to answer this from.`,
		DeterministicPrecheck: true,
		// All token/cost/latency fields deliberately zero — no call happened.
	}

	response := h.ask(t, "What was our margin in July 2023?")

	if response.Status != "refused" {
		t.Fatalf("status = %q, want refused", response.Status)
	}
	if h.explainer.calls != 0 {
		t.Errorf("explanation step ran %d times, want 0 — a refused question never reaches explain", h.explainer.calls)
	}

	if len(h.instrumentation.records) != 1 {
		t.Fatalf("instrumentation records = %d, want exactly 1", len(h.instrumentation.records))
	}
	record := h.instrumentation.records[0]
	if record.ModelUsed != ambiguity.PrecheckModelLabel {
		t.Errorf("record.ModelUsed = %q, want %q — a pre-check refusal must never be attributed to a model", record.ModelUsed, ambiguity.PrecheckModelLabel)
	}
	if record.InputTokens != 0 || record.OutputTokens != 0 || record.EstimatedCostUSD != 0 {
		t.Errorf("record tokens/cost = %d/%d/$%f, want all zero — no model call happened",
			record.InputTokens, record.OutputTokens, record.EstimatedCostUSD)
	}
	if !record.RefusalFired {
		t.Error("record.RefusalFired = false, want true")
	}

	if len(response.Interactions) != 1 {
		t.Fatalf("response interactions = %d, want exactly 1", len(response.Interactions))
	}
	if got := response.Interactions[0].ModelUsed; got != ambiguity.PrecheckModelLabel {
		t.Errorf("interaction model_used = %q, want %q", got, ambiguity.PrecheckModelLabel)
	}
	if response.Interactions[0].EstimatedCostUSD != 0 {
		t.Errorf("interaction cost = %f, want 0", response.Interactions[0].EstimatedCostUSD)
	}
}

// A model-decided refusal (DeterministicPrecheck false) must keep its
// existing attribution to the gate's model — the honest label is only for
// the path that truly made no call.
func TestModelDecidedRefusalKeepsGateModelAttribution(t *testing.T) {
	h := newAskHarness(t)
	h.gate.decision = ambiguity.Decision{
		Result:           instrumentation.GateUnanswerable,
		RefusalReason:    "No tool ranks days by total expenses.",
		InputTokens:      500,
		OutputTokens:     60,
		EstimatedCostUSD: 0.0016,
		LatencyMs:        800,
	}

	response := h.ask(t, "What single calendar date had the highest expenses?")

	if response.Status != "refused" {
		t.Fatalf("status = %q, want refused", response.Status)
	}
	if len(h.instrumentation.records) != 1 {
		t.Fatalf("instrumentation records = %d, want exactly 1", len(h.instrumentation.records))
	}
	if got := h.instrumentation.records[0].ModelUsed; got == ambiguity.PrecheckModelLabel {
		t.Errorf("record.ModelUsed = %q — a real model refusal must not carry the no-model label", got)
	}
}
