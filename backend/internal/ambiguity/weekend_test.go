package ambiguity

// Tests over weekend.go's deterministic vague-weekend pre-check. Same
// shape as daterange_test.go and gate_test.go's pre-check tests: pure unit
// tests over the matcher, plus Classify driven against countingCreator so
// "zero model calls" is proven structurally rather than assumed.

import (
	"context"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/instrumentation"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/llmclient"
)

func TestNeedsWeekendClarification(t *testing.T) {
	cases := []struct {
		name     string
		question string
		want     bool
	}{
		{"the canonical case", "How did we do over the weekend?", true},
		{"bare", "How was the weekend?", true},
		{"mid-sentence", "Was the weekend better than the week before?", true},
		{"capitalized", "Weekend numbers please", true},

		// The regression this pre-check must never cause: IFOOD-CAMP-WEEKEND
		// is a real campaign in this dataset AND a graded question in
		// evaluation/promptfoo/refusal.yaml. Go's \b treats "-" as a word
		// boundary, so a naive \bweekend\b would hijack it into a
		// clarifying question instead of letting the tool report that its
		// ROI is unattributable.
		{"campaign id containing the word", "What was the ROI on the IFOOD-CAMP-WEEKEND campaign?", false},
		{"lowercase campaign id", "roi on ifood-camp-weekend?", false},
		{"snake-cased field name", "show me weekend_total", false},

		// The plural is a different question ("how do weekends compare to
		// weekdays?") and is deliberately out of this lexicon.
		{"plural", "How do weekends compare to weekdays?", false},

		// Already pinned down — nothing left to ask.
		{"days named in full", "How was the weekend, Friday through Sunday?", false},
		{"days named short", "Weekend numbers — Sat and Sun only please", false},
		{"explicit date given", "How was the weekend of August 9, 2025?", false},
		{"explicit ISO date given", "How was the weekend starting 2024-08-03?", false},

		// A weekday name inside another word must not count as naming a day.
		{"weekday abbreviation inside a word", "Are you satisfied with the weekend?", true},

		{"no mention at all", "How did we do last week?", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, needsWeekendClarification(c.question))
		})
	}
}

// The core guarantee, mirroring
// TestClassify_OutOfRangeExplicitDateRefusesWithZeroModelCalls: "the
// weekend" with no days named is clarified by Go alone — zero model
// invocations, zero tokens, zero cost, and a clarifying question composed
// from constants.
//
// This is also what makes the behaviour testable at all. The live smoke
// test used to assert this classification against the real model and
// flaked, returning "answerable" some runs: which days count as a weekend
// is a definition this product does not have, and asking a probabilistic
// step to supply a missing definition gets a different answer each time.
func TestClassify_VagueWeekendClarifiesWithZeroModelCalls(t *testing.T) {
	fake := &countingCreator{}
	g := newGate(fake, testDataStart, testDataEnd)

	d, err := g.Classify(context.Background(), "How did we do over the weekend?", nil, nil)
	require.NoError(t, err)

	require.Equal(t, 0, fake.calls, "which days count as a weekend is a definition, not a judgment — it must never reach a model")
	require.Equal(t, instrumentation.GateAmbiguous, d.Result)
	require.True(t, d.DeterministicPrecheck)
	require.Equal(t, WeekendPrecheckModelLabel, d.PrecheckLabel)
	require.Nil(t, d.Writer, "no writer pass either — the clarification is worded deterministically")
	require.Zero(t, d.InputTokens)
	require.Zero(t, d.OutputTokens)
	require.Zero(t, d.EstimatedCostUSD)

	require.Equal(t, weekendClarifyingQuestion, d.ClarifyingQuestion)
	require.Len(t, d.ClarifyingOptions, 2, "the two candidate readings are exactly what the owner has to choose between")
	require.Contains(t, d.ClarifyingOptions[0], "Friday")
	require.Contains(t, d.ClarifyingOptions[1], "Saturday")
	require.Empty(t, d.AssumptionStated, "exactly one of clarifying question / stated assumption, per FR-006")
}

// TestClassify_VagueWeekendIsStableAcrossRepeatedCalls is the direct
// anti-flake assertion: identical input, identical verdict, every time.
func TestClassify_VagueWeekendIsStableAcrossRepeatedCalls(t *testing.T) {
	fake := &countingCreator{}
	g := newGate(fake, testDataStart, testDataEnd)

	for i := 0; i < 25; i++ {
		d, err := g.Classify(context.Background(), "How did we do over the weekend?", nil, nil)
		require.NoError(t, err)
		require.Equal(t, instrumentation.GateAmbiguous, d.Result, "run %d disagreed", i)
		require.Equal(t, weekendClarifyingQuestion, d.ClarifyingQuestion, "run %d disagreed", i)
	}
	require.Equal(t, 0, fake.calls)
}

// A campaign whose id contains the word must go to the model (and from
// there to the typed tool) exactly as before — this pre-check must not
// widen into a keyword filter.
func TestClassify_CampaignIdContainingWeekendStillReachesTheModel(t *testing.T) {
	fake := &countingCreator{response: &llmclient.MessageResult{
		Text:       `{"classification": "answerable", "clarifying_question": "", "clarifying_options": [], "assumption_stated": "", "reason": ""}`,
		StopReason: anthropic.StopReasonEndTurn,
	}}
	g := newGate(fake, testDataStart, testDataEnd)

	d, err := g.Classify(context.Background(), "What was the ROI on the IFOOD-CAMP-WEEKEND campaign?", nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, fake.calls, "a campaign question must reach the classification pass, not a clarifying question")
	require.Equal(t, instrumentation.GateAnswerable, d.Result)
	require.False(t, d.DeterministicPrecheck)
}

// The loop guard: the owner's REPLY to this very clarification must be
// classified as the resolved question, never bounced back into the same
// clarification it is answering. A reply that repeats the word ("the
// weekend that just finished") is the case that would otherwise loop
// forever.
func TestClassify_ReplyToTheWeekendClarificationIsNotReClarified(t *testing.T) {
	fake := &countingCreator{response: &llmclient.MessageResult{
		Text:       `{"classification": "answerable", "clarifying_question": "", "clarifying_options": [], "assumption_stated": "Counting the weekend as Friday through Sunday, per your reply.", "reason": ""}`,
		StopReason: anthropic.StopReasonEndTurn,
	}}
	g := newGate(fake, testDataStart, testDataEnd)

	d, err := g.Classify(context.Background(), "the weekend that just finished", &PendingClarification{
		OriginalQuestion:   "How did we do over the weekend?",
		ClarifyingQuestion: weekendClarifyingQuestion,
	}, nil)
	require.NoError(t, err)

	require.Equal(t, 1, fake.calls, "a reply to a clarifying question must be classified, not re-clarified")
	require.False(t, d.DeterministicPrecheck)
	require.Equal(t, instrumentation.GateAnswerable, d.Result)
}

// The prompt half of this fix: even if a weekend phrasing this pre-check
// does not recognize reaches the model, the prompt must say plainly that
// anchoring the period does not settle the day range. The wording that
// flaked said the opposite by implication ("without a clear anchor once
// resolved per the date-grounding rule above").
func TestBuildSystemPrompt_SaysAnchoringAWeekendIsNotSufficient(t *testing.T) {
	prompt := buildSystemPrompt(testDataStart, testDataEnd)

	require.Contains(t, prompt, "NOT sufficient",
		"the prompt must state outright that resolving WHICH weekend does not resolve WHICH DAYS")
	require.Contains(t, prompt, "Friday through Sunday or Saturday and Sunday only")
	require.Contains(t, prompt, "Resolving the anchor does not resolve the day range")
	require.NotContains(t, prompt, `like "the weekend" without a clear anchor once resolved`,
		"the old wording read as if anchoring disposed of the ambiguity — it must not come back")
}
