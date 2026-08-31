package explain

// Tests for the post-narration numeric check Explain runs through
// internal/answerverify: a money figure the model STATES must appear in
// what the tools RETURNED.
//
// Same white-box style and the same doubles as explain_internal_test.go
// (fakeLLM, newFakeMCPClient, textBlock/toolUseBlock) — newFakeMCPClient's
// one tool returns {"value": "42.00", ...}, so "$42.00" is a grounded
// figure in these tests and anything else is not.

import (
	"context"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/answerverify"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/llmclient"
)

// twoTurnNarration scripts the shape every test below needs: turn 0 calls
// the fake tool, turn 1 narrates answerText as the final answer.
func twoTurnNarration(t *testing.T, answerText string) *fakeLLM {
	t.Helper()
	return &fakeLLM{
		responses: []*llmclient.MessageResult{
			{
				ContentBlocks: []anthropic.ContentBlockUnion{toolUseBlock(t, "tu-1", "test_tool", map[string]any{})},
				StopReason:    anthropic.StopReasonToolUse,
				InputTokens:   100,
				OutputTokens:  20,
			},
			{
				Text:          answerText,
				ContentBlocks: []anthropic.ContentBlockUnion{textBlock(t, answerText)},
				StopReason:    anthropic.StopReasonEndTurn,
				InputTokens:   200,
				OutputTokens:  40,
			},
		},
	}
}

// TestExplain_NarratedFigureNotInToolResultIsRefused is the whole point of
// the check: the model calls the tool correctly, is handed 42.00, and then
// states a completely different number. Every other guard in explain.go
// passes this — a tool ran, provenance was collected, the response wasn't
// truncated — and the owner would have been shown a fabricated figure.
func TestExplain_NarratedFigureNotInToolResultIsRefused(t *testing.T) {
	llm := twoTurnNarration(t, "Your margin that day was $999.99.")
	e := newTestExplainer(llm, newFakeMCPClient(t))

	result, err := e.Explain(context.Background(), "What was our margin?", "", "")

	require.NoError(t, err)
	require.NotEmpty(t, result.IncompleteReason, "a stated figure absent from every tool result must be refused")
	require.Empty(t, result.AnswerText)
	require.Equal(t, answerverify.RefusalReason, result.IncompleteReason)
	// Real, billed usage across both turns is still reported — a refusal
	// never discards spend that actually happened.
	require.Equal(t, int64(300), result.InputTokens)
	require.Equal(t, int64(60), result.OutputTokens)
}

// TestExplain_SubtlyAlteredFigureIsRefused is the case the pre-existing
// zero-tool-call guard could never reach and the one that actually costs a
// restaurant owner money: not an obviously invented number, but the right
// number off by cents. Nothing about the shape of this answer looks wrong.
func TestExplain_SubtlyAlteredFigureIsRefused(t *testing.T) {
	llm := twoTurnNarration(t, "That comes to $42.05 for the day.")
	e := newTestExplainer(llm, newFakeMCPClient(t))

	result, err := e.Explain(context.Background(), "What was our margin?", "", "")

	require.NoError(t, err)
	require.NotEmpty(t, result.IncompleteReason, "$42.05 is not $42.00 — a cents-level alteration must not pass")
	require.Empty(t, result.AnswerText)
}

// TestExplain_GroundedFigureIsServed is the other half of the guarantee:
// the check must not become a tax on correct answers.
func TestExplain_GroundedFigureIsServed(t *testing.T) {
	answer := "That day came to $42.00, per the reconciliation."
	llm := twoTurnNarration(t, answer)
	e := newTestExplainer(llm, newFakeMCPClient(t))

	result, err := e.Explain(context.Background(), "What was our margin?", "", "")

	require.NoError(t, err)
	require.Empty(t, result.IncompleteReason)
	require.Equal(t, answer, result.AnswerText)
	require.Len(t, result.ToolInvocations, 1)
}

// TestExplain_RoundedRestatementOfAGroundedFigureIsServed guards the single
// biggest false-refusal risk this check carries, and the one the real
// evaluation corpus actually exhibits: narrations routinely restate a
// grounded figure at lower precision ("it cost $610 and only drove about
// $159 in extra sales", against tool values of 610.00 and 159.25). A
// cents-exact matcher would refuse correct answers like these. Verified at
// the precision the sentence was written in, "$42" is a claim about
// dollars and 42.00 satisfies it.
func TestExplain_RoundedRestatementOfAGroundedFigureIsServed(t *testing.T) {
	answer := "Roughly $42 for the day."
	llm := twoTurnNarration(t, answer)
	e := newTestExplainer(llm, newFakeMCPClient(t))

	result, err := e.Explain(context.Background(), "What was our margin?", "", "")

	require.NoError(t, err)
	require.Empty(t, result.IncompleteReason, "a rounded restatement of a grounded figure must not be refused")
	require.Equal(t, answer, result.AnswerText)
}

// TestExplain_AnswerWithNoFiguresIsUntouched: an answer that states no
// money at all has nothing for this check to disagree with, and must pass
// through exactly as before (the duplicate-order and missing-data answers
// the consistency suite grades are often shaped like this).
func TestExplain_AnswerWithNoFiguresIsUntouched(t *testing.T) {
	answer := "Yes — that order appeared twice in the export and was counted only once."
	llm := twoTurnNarration(t, answer)
	e := newTestExplainer(llm, newFakeMCPClient(t))

	result, err := e.Explain(context.Background(), "Was there a duplicate order?", "", "")

	require.NoError(t, err)
	require.Empty(t, result.IncompleteReason)
	require.Equal(t, answer, result.AnswerText)
}

// TestExplain_PriorAnswerFigureIsNotRefused pins the fix for a live
// failure: a natural follow-up ("what about the previous day?") restated
// the immediately preceding turn's own already-verified margin figure
// ($3,225.06) and was refused, because that figure appears in THIS turn's
// own tool results nowhere -- only in the PRECEDING turn's. The fix widens
// answerverify's allowed set with priorAnswerText, since a figure this
// exact product already verified and served last turn is not a new,
// untrusted claim.
func TestExplain_PriorAnswerFigureIsNotRefused(t *testing.T) {
	// The tool this turn returns 42.00 (newFakeMCPClient's fixed fixture);
	// the narration restates $3,225.06, which is grounded ONLY in the
	// prior turn's own answer, never in this turn's tool result.
	llm := twoTurnNarration(t, "The day before that, margin was $3,225.06 -- $42.00 today by comparison.")
	e := newTestExplainer(llm, newFakeMCPClient(t))

	result, err := e.Explain(context.Background(), "what about the day before?", "", "Margin on 2026-08-29 was $3,225.06.")

	require.NoError(t, err)
	require.Empty(t, result.IncompleteReason, "restating a figure the immediately preceding turn already verified and served must not be refused")
	require.Equal(t, "The day before that, margin was $3,225.06 -- $42.00 today by comparison.", result.AnswerText)
}

// TestExplain_UnrelatedFigureStillRefusedEvenWithAPriorAnswer proves the
// widening above is not a general loosening of the check: a figure that
// matches NEITHER this turn's tool results NOR the prior answer is still
// refused exactly as before.
func TestExplain_UnrelatedFigureStillRefusedEvenWithAPriorAnswer(t *testing.T) {
	llm := twoTurnNarration(t, "Your margin that day was $999.99.")
	e := newTestExplainer(llm, newFakeMCPClient(t))

	result, err := e.Explain(context.Background(), "what about the day before?", "", "Margin on 2026-08-29 was $3,225.06.")

	require.NoError(t, err)
	require.NotEmpty(t, result.IncompleteReason, "a figure grounded in neither this turn's tools nor the prior answer must still be refused")
}

// TestExplain_NumericRefusalCopyStaysOwnerFacing applies the exact
// discipline TestExplain_ZeroToolCallCurrencyAnswerIsRefused already holds
// the other refusal path to: internal/httpapi forwards IncompleteReason
// verbatim as AskResponse.RefusalReason and the frontend renders it
// verbatim in the chat bubble, so this sentence is read by a restaurant
// owner and must never carry the vocabulary of how the check works.
func TestExplain_NumericRefusalCopyStaysOwnerFacing(t *testing.T) {
	for _, jargon := range []string{
		"MCP",
		"tool call",
		"provenance",
		"deterministic layer",
		"validator",
		"validation",
		"canonical",
		"JSON",
	} {
		require.NotContains(t, strings.ToLower(answerverify.RefusalReason), strings.ToLower(jargon),
			"refusal message must never leak this internal implementation term to a restaurant owner")
	}
	require.Contains(t, strings.ToLower(answerverify.RefusalReason), "wrong number is worse than no answer",
		"the copy should state the product's own principle plainly, the way the existing refusal paths do")
}
