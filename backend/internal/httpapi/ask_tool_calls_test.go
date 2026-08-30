package httpapi

// Tests for spec 008 FR-003 ("show your work"): AskResponse.ToolCalls must
// carry the exact tool name(s) and raw JSON result(s) already present in
// result.ToolInvocations for an answered question — no new tool call, no
// re-computation — and must be empty/omitted when no tool ran at all.

import (
	"testing"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/explain"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/instrumentation"
)

func TestAnsweredQuestionCarriesRealToolCallsInResponse(t *testing.T) {
	h := newAskHarness(t)
	h.explainer.result.ToolInvocations = []explain.ToolInvocation{
		{Name: "get_daily_summary", ResultJSON: `{"date":"2026-08-07","margin":"375.82"}`},
	}

	response := h.ask(t, "How did we do on 2026-08-07?")

	if response.Status != "answered" {
		t.Fatalf("status = %q, want answered", response.Status)
	}
	if len(response.ToolCalls) != 1 {
		t.Fatalf("tool_calls = %d entries, want 1: %+v", len(response.ToolCalls), response.ToolCalls)
	}
	if response.ToolCalls[0].Name != "get_daily_summary" {
		t.Errorf("tool_calls[0].name = %q, want get_daily_summary", response.ToolCalls[0].Name)
	}
	if string(response.ToolCalls[0].ResultJSON) != `{"date":"2026-08-07","margin":"375.82"}` {
		t.Errorf("tool_calls[0].result_json = %s, want the exact raw JSON explain already returned", response.ToolCalls[0].ResultJSON)
	}
}

func TestAnsweredQuestionWithMultipleToolCallsCarriesAllOfThemInOrder(t *testing.T) {
	h := newAskHarness(t)
	h.explainer.result.ToolInvocations = []explain.ToolInvocation{
		{Name: "get_period_totals", ResultJSON: `{"start":"2026-08-01","end":"2026-08-07"}`},
		{Name: "list_discrepancies", ResultJSON: `{"days_checked":7,"days":[]}`},
	}

	response := h.ask(t, "What were the totals for the first week of August?")

	if len(response.ToolCalls) != 2 {
		t.Fatalf("tool_calls = %d entries, want 2: %+v", len(response.ToolCalls), response.ToolCalls)
	}
	if response.ToolCalls[0].Name != "get_period_totals" || response.ToolCalls[1].Name != "list_discrepancies" {
		t.Errorf("tool_calls order = [%q, %q], want the exact invocation order", response.ToolCalls[0].Name, response.ToolCalls[1].Name)
	}
}

func TestNoToolCallsYieldsOmittedToolCallsField(t *testing.T) {
	h := newAskHarness(t)
	// newAskHarness's default explainer.result has no ToolInvocations set —
	// the zero value, matching an answer that (in principle) needed no tool.

	response := h.ask(t, "How did we do on 2026-08-07?")

	if len(response.ToolCalls) != 0 {
		t.Errorf("tool_calls = %+v, want empty/omitted when no tool ran", response.ToolCalls)
	}
}

func TestRefusedQuestionNeverCarriesToolCalls(t *testing.T) {
	h := newAskHarness(t)
	h.gate.decision.Result = instrumentation.GateUnanswerable
	h.gate.decision.RefusalReason = "This data isn't in the dataset."

	response := h.ask(t, "What was our margin in the year 3000?")

	if response.Status != "refused" {
		t.Fatalf("status = %q, want refused", response.Status)
	}
	if len(response.ToolCalls) != 0 {
		t.Errorf("tool_calls = %+v, want empty on a refusal that never reached a tool", response.ToolCalls)
	}
}
