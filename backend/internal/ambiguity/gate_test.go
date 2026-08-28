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

// TestParseWriterResponse_* are pure unit tests over the second-pass
// writing call's model-independent parsing (parseWriterResponse), mirroring
// TestParseGateResponse_* above. writerResponse has no "classification"
// field at all — these tests confirm that an extra "classification" key in
// the raw JSON is simply ignored by encoding/json (Go's default behavior
// for unknown fields) rather than somehow smuggled through, which is the
// structural half of "Sonnet cannot override Haiku's classification" this
// package's doc comment describes.

func TestParseWriterResponse_AmbiguousRewrite(t *testing.T) {
	wr, err := parseWriterResponse(`{"clarifying_question": "Does \"the weekend\" mean Friday through Sunday, or just Saturday and Sunday?", "clarifying_options": ["Friday through Sunday", "Saturday and Sunday only"], "reason": ""}`)
	require.NoError(t, err)
	require.Equal(t, `Does "the weekend" mean Friday through Sunday, or just Saturday and Sunday?`, wr.ClarifyingQuestion)
	require.Equal(t, []string{"Friday through Sunday", "Saturday and Sunday only"}, wr.ClarifyingOptions)
	require.Empty(t, wr.Reason)
}

func TestParseWriterResponse_UnanswerableRewrite(t *testing.T) {
	wr, err := parseWriterResponse(`{"clarifying_question": "", "clarifying_options": [], "reason": "This product only has reconciled data from 2026-08-01 through 2026-08-14 — September 2026 isn't in range yet."}`)
	require.NoError(t, err)
	require.Empty(t, wr.ClarifyingQuestion)
	require.Contains(t, wr.Reason, "2026-08-01")
}

// A malicious or confused model response including an extra
// "classification" key must not break parsing, and — critically —
// writerResponse has nowhere for that value to land: this test proves the
// parsed struct carries only prose fields, never a classification.
func TestParseWriterResponse_IgnoresAnyClassificationField(t *testing.T) {
	wr, err := parseWriterResponse(`{"classification": "answerable", "clarifying_question": "Which week — this one or last?", "clarifying_options": [], "reason": ""}`)
	require.NoError(t, err)
	require.Equal(t, "Which week — this one or last?", wr.ClarifyingQuestion)
	// writerResponse (see its type definition) has exactly three fields:
	// ClarifyingQuestion, ClarifyingOptions, Reason. There is no field a
	// "classification" key could have populated, by construction.
}

func TestParseWriterResponse_RejectsNonJSON(t *testing.T) {
	_, err := parseWriterResponse("Sure, here's a better clarifying question.")
	require.Error(t, err)
	require.Contains(t, err.Error(), "did not contain a JSON object")
}

func TestParseWriterResponse_RejectsMalformedJSON(t *testing.T) {
	_, err := parseWriterResponse(`{"reason": "missing data", }`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not valid JSON")
}

// TestComposeAnswerFollowUp_* are pure unit tests over the ANSWER-side
// follow-up composition (the counterpart to ask_clarification_test.go's
// TestComposeFollowUpIsDeterministicAndInertodWithoutContext, which covers
// ComposeFollowUp), asserting the EXACT composed text rather than just
// checking it's non-empty — this string is what the gate classifies, what
// explain narrates, and what the answer cache keys on, so its exact shape
// is load-bearing, not incidental.

func TestComposeAnswerFollowUp_NilPreviousReturnsQuestionUnchanged(t *testing.T) {
	require.Equal(t, "and the day before?", ComposeAnswerFollowUp("and the day before?", nil))
}

func TestComposeAnswerFollowUp_EmptyAnswerTextTreatedAsNoContext(t *testing.T) {
	previous := &PreviousExchange{Question: "What was our margin on 2026-08-05?", AnswerText: "   "}
	require.Equal(t, "and the day before?", ComposeAnswerFollowUp("and the day before?", previous))
}

func TestComposeAnswerFollowUp_ComposesTheExactExpectedText(t *testing.T) {
	previous := &PreviousExchange{
		Question:   "What was our margin on 2026-08-05?",
		AnswerText: "Margin on 2026-08-05 was $612.40 on $2,180.00 in gross sales.",
	}
	got := ComposeAnswerFollowUp("and the day before?", previous)
	want := "and the day before?" +
		"\n\n[Previous exchange context] The user previously asked: \"What was our margin on 2026-08-05?\" and was told: \"Margin on 2026-08-05 was $612.40 on $2,180.00 in gross sales.\"" +
		"\nThe text above this block is what they have now said. This may be a follow-up to that previous exchange, or it may be a brand new, unrelated question — decide which from its content. If it is a follow-up, classify its resolved meaning (the new text interpreted in light of the previous question and answer) as the question to classify."
	require.Equal(t, want, got)
}

func TestComposeAnswerFollowUp_TrimsWhitespaceInPreviousFields(t *testing.T) {
	previous := &PreviousExchange{Question: "  orig question  ", AnswerText: "  the answer  "}
	got := ComposeAnswerFollowUp("why?", previous)
	require.Contains(t, got, `"orig question"`)
	require.Contains(t, got, `"the answer"`)
	require.NotContains(t, got, "  orig question  ")
}

func TestComposeAnswerFollowUp_IsDeterministic(t *testing.T) {
	previous := &PreviousExchange{Question: "orig", AnswerText: "answer"}
	require.Equal(t, ComposeAnswerFollowUp("reply", previous), ComposeAnswerFollowUp("reply", previous),
		"composition must be deterministic — it is the cache key")
}

// A follow-up to a real ANSWER must never be confused, at the composition
// layer, with a reply to a CLARIFYING question — the two produce visibly
// different marker text ("[Previous exchange context]" vs. "[Follow-up
// context]") so the gate's system prompt sections can never be misapplied
// to the wrong case.
func TestComposeAnswerFollowUp_UsesADistinctMarkerFromClarificationFollowUp(t *testing.T) {
	previous := &PreviousExchange{Question: "orig", AnswerText: "answer"}
	got := ComposeAnswerFollowUp("why?", previous)
	require.Contains(t, got, "[Previous exchange context]")
	require.NotContains(t, got, "[Follow-up context]")
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
	_, err := g.Classify(context.Background(), "   ", nil, nil)
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
		d, err := g.Classify(ctx, "What was our reconciled margin on 2026-08-03?", nil, nil)
		require.NoError(t, err)
		require.Equal(t, instrumentation.GateAnswerable, d.Result)
		require.Greater(t, d.InputTokens, int64(0))
		require.GreaterOrEqual(t, d.EstimatedCostUSD, 0.0)
		t.Logf("answerable smoke test: %d in / %d out tokens, $%.6f", d.InputTokens, d.OutputTokens, d.EstimatedCostUSD)
	})

	t.Run("unanswerable", func(t *testing.T) {
		d, err := g.Classify(ctx, "How much did we spend with our cheese supplier in September 2026?", nil, nil)
		require.NoError(t, err)
		require.Equal(t, instrumentation.GateUnanswerable, d.Result)
		require.NotEmpty(t, d.RefusalReason)
		// The non-negotiable this feature adds: the second-pass Sonnet call
		// may rewrite d.RefusalReason's PROSE, but it can never change
		// d.Result away from what Haiku classified — asserted directly here
		// against a real live response, not just by code inspection.
		require.Equal(t, instrumentation.GateUnanswerable, d.Result, "Sonnet's writing pass must never change the gate's classification")
		if d.Writer != nil {
			require.Equal(t, llmclient.ModelExplanation, d.Writer.ModelUsed)
			require.Greater(t, d.Writer.InputTokens, int64(0))
			t.Logf("unanswerable writer pass: %d in / %d out tokens, $%.6f", d.Writer.InputTokens, d.Writer.OutputTokens, d.Writer.EstimatedCostUSD)
		}
		t.Logf("unanswerable smoke test: %d in / %d out tokens, $%.6f, reason=%q", d.InputTokens, d.OutputTokens, d.EstimatedCostUSD, d.RefusalReason)
	})

	t.Run("ambiguous", func(t *testing.T) {
		d, err := g.Classify(ctx, "How did we do over the weekend?", nil, nil)
		require.NoError(t, err)
		require.Equal(t, instrumentation.GateAmbiguous, d.Result)
		require.True(t, d.ClarifyingQuestion != "" || d.AssumptionStated != "")
		// Same non-negotiable as the unanswerable case above: a real writer
		// pass (only fires when a clarifying question, not an assumption,
		// was chosen) must never move d.Result off "ambiguous".
		require.Equal(t, instrumentation.GateAmbiguous, d.Result, "Sonnet's writing pass must never change the gate's classification")
		if d.ClarifyingQuestion != "" && d.Writer != nil {
			require.Equal(t, llmclient.ModelExplanation, d.Writer.ModelUsed)
			require.Greater(t, d.Writer.InputTokens, int64(0))
			t.Logf("ambiguous writer pass: %d in / %d out tokens, $%.6f", d.Writer.InputTokens, d.Writer.OutputTokens, d.Writer.EstimatedCostUSD)
		}
		t.Logf("ambiguous smoke test: %d in / %d out tokens, $%.6f, clarify=%q assume=%q", d.InputTokens, d.OutputTokens, d.EstimatedCostUSD, d.ClarifyingQuestion, d.AssumptionStated)
	})

	t.Run("answerable question costs exactly the gate's single Haiku call", func(t *testing.T) {
		d, err := g.Classify(ctx, "What was our reconciled margin on 2026-08-05?", nil, nil)
		require.NoError(t, err)
		require.Equal(t, instrumentation.GateAnswerable, d.Result)
		require.Nil(t, d.Writer, "an answerable question must never trigger the second Sonnet writing pass — no cost change for the common case")
	})
}

// TestGate_Classify_DateGroundingRegression reproduces, live against Claude
// Haiku 4.5, the exact "date-year grounding defect" recorded in
// docs/plan.md's mistakes log and docs/product-strategy.md's "Real
// evaluation results" section: a bare, no-year, relative-date question
// ("this week") whose classification varied unprompted across repeated
// calls with IDENTICAL input — sometimes answering correctly, sometimes
// asking a clarifying question about the year, and in the worst observed
// case confidently inventing a wrong year ("no data for ..., 2024") and
// refusing against that fabricated premise.
//
// The fix (buildSystemPrompt's "Date grounding" paragraph) tells the gate
// explicitly that its only "today" is dataEnd (2026-08-14), not the real
// wall-clock date, so this question should now resolve unambiguously and
// IDENTICALLY every time — this test calls Classify 3 times with the exact
// same input specifically to catch a regression to the old
// sometimes-right-sometimes-wrong behavior, not just check a single lucky
// pass. Skipped, not faked, when ANTHROPIC_API_KEY isn't set.
func TestGate_Classify_DateGroundingRegression(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set; skipping live Claude Haiku 4.5 regression test")
	}

	g := New(llmclient.New(), testDataStart, testDataEnd)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		t.Run(fmt.Sprintf("run_%d", i), func(t *testing.T) {
			d, err := g.Classify(ctx, "How did we do this week?", nil, nil)
			require.NoError(t, err)

			// The specific worst-case failure: inventing a year the data
			// doesn't cover, anywhere in the gate's own output.
			for _, s := range []string{d.ClarifyingQuestion, d.AssumptionStated, d.RefusalReason} {
				require.NotContains(t, s, "2024")
				require.NotContains(t, s, "2025")
				require.NotContains(t, s, "2027")
			}

			// There IS reconciled data for the trailing week ending
			// 2026-08-14 — a bare "this week" must never be refused as
			// unanswerable.
			require.NotEqual(t, instrumentation.GateUnanswerable, d.Result,
				"must not refuse a bare 'this week' question — the trailing week ending %s has real data", testDataEnd)

			// Asking about the boundary of "week" is a legitimate ambiguous
			// case (and is exactly what the systemPrompt's own example
			// covers by stating a default assumption instead) — but asking
			// WHICH YEAR is the specific bug this test guards against.
			require.NotContains(t, strings.ToLower(d.ClarifyingQuestion), "year",
				"must not ask which year — dataEnd grounds this unambiguously to 2026")

			t.Logf("run %d: classification=%s clarify=%q assume=%q reason=%q", i, d.Result, d.ClarifyingQuestion, d.AssumptionStated, d.RefusalReason)
		})
	}
}
