package ambiguity

// TestParseGateResponse_* are pure unit tests over this package's
// model-independent classification logic (tasks.md T024) — hand-crafted
// strings standing in for what Claude Haiku 4.5 could plausibly reply with,
// covering all three classifications plus the malformed-response cases.
// They make zero Anthropic API calls and cost nothing.
//
// TestGate_Classify_LiveSmokeTest is the separate, deliberately small
// (3 calls) live-API test proving the real end-to-end path works
// end-to-end against Claude Haiku 4.5 (tasks.md T025's "a handful of calls
// is plenty" allowance) — skipped when ANTHROPIC_API_KEY isn't set, never
// faked.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/instrumentation"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/llmclient"
)

func TestParseGateResponse_Answerable(t *testing.T) {
	d, err := parseGateResponse(`{"classification": "answerable", "clarifying_question": "", "assumption_stated": "", "reason": ""}`)
	require.NoError(t, err)
	require.Equal(t, instrumentation.GateAnswerable, d.Result)
	require.Empty(t, d.ClarifyingQuestion)
	require.Empty(t, d.AssumptionStated)
	require.Empty(t, d.RefusalReason)
}

func TestParseGateResponse_AmbiguousWithClarifyingQuestion(t *testing.T) {
	d, err := parseGateResponse(`{"classification": "ambiguous", "clarifying_question": "Does \"the weekend\" include Friday?", "assumption_stated": "", "reason": ""}`)
	require.NoError(t, err)
	require.Equal(t, instrumentation.GateAmbiguous, d.Result)
	require.Equal(t, `Does "the weekend" include Friday?`, d.ClarifyingQuestion)
	require.Empty(t, d.AssumptionStated)
}

func TestParseGateResponse_AmbiguousWithStatedAssumption(t *testing.T) {
	d, err := parseGateResponse(`{"classification": "ambiguous", "clarifying_question": "", "assumption_stated": "Assuming \"this week\" means the trailing 7 days ending 2026-08-14.", "reason": ""}`)
	require.NoError(t, err)
	require.Equal(t, instrumentation.GateAmbiguous, d.Result)
	require.Empty(t, d.ClarifyingQuestion)
	require.Contains(t, d.AssumptionStated, "trailing 7 days")
}

func TestParseGateResponse_Unanswerable(t *testing.T) {
	d, err := parseGateResponse(`{"classification": "unanswerable", "clarifying_question": "", "assumption_stated": "", "reason": "No data exists for September 2026; this product only covers 2026-08-01 through 2026-08-14."}`)
	require.NoError(t, err)
	require.Equal(t, instrumentation.GateUnanswerable, d.Result)
	require.Contains(t, d.RefusalReason, "2026-08-01")
}

func TestParseGateResponse_TolerantOfSurroundingProseAndMarkdownFence(t *testing.T) {
	d, err := parseGateResponse("Sure, here's my classification:\n```json\n{\"classification\": \"answerable\", \"clarifying_question\": \"\", \"assumption_stated\": \"\", \"reason\": \"\"}\n```")
	require.NoError(t, err)
	require.Equal(t, instrumentation.GateAnswerable, d.Result)
}

func TestParseGateResponse_RejectsInvalidClassification(t *testing.T) {
	_, err := parseGateResponse(`{"classification": "maybe", "clarifying_question": "", "assumption_stated": "", "reason": ""}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid classification")
}

func TestParseGateResponse_RejectsAmbiguousWithNeitherClarificationNorAssumption(t *testing.T) {
	_, err := parseGateResponse(`{"classification": "ambiguous", "clarifying_question": "", "assumption_stated": "", "reason": ""}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "neither")
}

func TestParseGateResponse_RejectsAmbiguousWithBothClarificationAndAssumption(t *testing.T) {
	_, err := parseGateResponse(`{"classification": "ambiguous", "clarifying_question": "Which week?", "assumption_stated": "Assuming last week.", "reason": ""}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "both")
}

func TestParseGateResponse_RejectsUnanswerableWithNoReason(t *testing.T) {
	_, err := parseGateResponse(`{"classification": "unanswerable", "clarifying_question": "", "assumption_stated": "", "reason": ""}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no reason")
}

func TestParseGateResponse_RejectsNonJSON(t *testing.T) {
	_, err := parseGateResponse("I think this question is answerable.")
	require.Error(t, err)
	require.Contains(t, err.Error(), "did not contain a JSON object")
}

func TestParseGateResponse_RejectsMalformedJSON(t *testing.T) {
	_, err := parseGateResponse(`{"classification": "answerable", }`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not valid JSON")
}

// testDataStart/testDataEnd mirror the real fixture range
// (backend/fixtures/README.md, 2026-08-01..14) — tests that don't care
// about the exact date-grounding text still need a well-formed range for
// New to build a usable system prompt.
const (
	testDataStart = "2026-08-01"
	testDataEnd   = "2026-08-14"
)

func TestClassify_RejectsEmptyQuestion(t *testing.T) {
	g := New(llmclient.New(), testDataStart, testDataEnd)
	_, err := g.Classify(context.Background(), "   ")
	require.ErrorIs(t, err, ErrEmptyQuestion)
}

// TestGate_Classify_LiveSmokeTest makes exactly 3 real Claude Haiku 4.5
// calls — one per classification — to prove the request shape (system
// prompt, JSON response parsing, cost estimation) actually works against
// the live API, not just against hand-crafted strings above. Skipped, not
// faked, when ANTHROPIC_API_KEY isn't set. Full evaluation-harness
// verification of the gate happens in a later phase; this is deliberately
// minimal per tasks.md's "under 10 calls" guidance for this phase.
func TestGate_Classify_LiveSmokeTest(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set; skipping live Claude Haiku 4.5 smoke test")
	}

	g := New(llmclient.New(), testDataStart, testDataEnd)
	ctx := context.Background()

	t.Run("answerable", func(t *testing.T) {
		d, err := g.Classify(ctx, "What was our reconciled margin on 2026-08-03?")
		require.NoError(t, err)
		require.Equal(t, instrumentation.GateAnswerable, d.Result)
		require.Greater(t, d.InputTokens, int64(0))
		require.GreaterOrEqual(t, d.EstimatedCostUSD, 0.0)
		t.Logf("answerable smoke test: %d in / %d out tokens, $%.6f", d.InputTokens, d.OutputTokens, d.EstimatedCostUSD)
	})

	t.Run("unanswerable", func(t *testing.T) {
		d, err := g.Classify(ctx, "How much did we spend with our cheese supplier in September 2026?")
		require.NoError(t, err)
		require.Equal(t, instrumentation.GateUnanswerable, d.Result)
		require.NotEmpty(t, d.RefusalReason)
		t.Logf("unanswerable smoke test: %d in / %d out tokens, $%.6f, reason=%q", d.InputTokens, d.OutputTokens, d.EstimatedCostUSD, d.RefusalReason)
	})

	t.Run("ambiguous", func(t *testing.T) {
		d, err := g.Classify(ctx, "How did we do over the weekend?")
		require.NoError(t, err)
		require.Equal(t, instrumentation.GateAmbiguous, d.Result)
		require.True(t, d.ClarifyingQuestion != "" || d.AssumptionStated != "")
		t.Logf("ambiguous smoke test: %d in / %d out tokens, $%.6f, clarify=%q assume=%q", d.InputTokens, d.OutputTokens, d.EstimatedCostUSD, d.ClarifyingQuestion, d.AssumptionStated)
	})
}
