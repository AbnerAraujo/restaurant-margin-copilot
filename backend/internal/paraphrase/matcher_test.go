package paraphrase

// TestResolveMatch_* are pure unit tests over this package's
// model-independent verification logic — hand-crafted strings standing in
// for what Claude Haiku 4.5 could plausibly (or implausibly) reply with,
// covering the plan's required boundary: verified match -> cache served,
// hallucinated/unverifiable match -> treated as a miss, NONE -> normal
// flow. Same pattern as internal/ambiguity's TestParseGateResponse_* tests:
// zero Anthropic API calls, zero cost.
//
// TestMatcher_Classify_LiveSmokeTest and
// TestMatcher_Classify_MeaningfullyDifferentQuestionNeverMatches are the
// separate, deliberately small live-API tests proving the real behavior
// against Claude Haiku 4.5 — skipped, never faked, when ANTHROPIC_API_KEY
// isn't set.

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/answercache"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/llmclient"
)

func testCandidates() []answercache.Candidate {
	return []answercache.Candidate{
		{NormalizedQuestion: "what was our margin on 2026-08-07?", OriginalQuestion: "What was our margin on 2026-08-07?"},
		{NormalizedQuestion: "how did we do this week?", OriginalQuestion: "How did we do this week?"},
	}
}

func TestResolveMatch_VerifiedMatchIsServed(t *testing.T) {
	matched, candidate := resolveMatch("What was our margin on 2026-08-07?", testCandidates())
	require.True(t, matched)
	require.Equal(t, "what was our margin on 2026-08-07?", candidate.NormalizedQuestion)
}

func TestResolveMatch_VerifiedMatchToleratesWhitespaceAndCaseDifferences(t *testing.T) {
	// The model is free to reply with slightly different casing/whitespace
	// than the candidate had; Normalize() is what makes this still count as
	// the SAME verified candidate, not a hallucination.
	matched, candidate := resolveMatch("  WHAT was our   margin on 2026-08-07?  ", testCandidates())
	require.True(t, matched)
	require.Equal(t, "what was our margin on 2026-08-07?", candidate.NormalizedQuestion)
}

func TestResolveMatch_ToleratesSurroundingQuotes(t *testing.T) {
	matched, candidate := resolveMatch(`"What was our margin on 2026-08-07?"`, testCandidates())
	require.True(t, matched)
	require.Equal(t, "what was our margin on 2026-08-07?", candidate.NormalizedQuestion)
}

func TestResolveMatch_NoneMeansNoMatch(t *testing.T) {
	matched, _ := resolveMatch("NONE", testCandidates())
	require.False(t, matched)

	matched, _ = resolveMatch("none", testCandidates())
	require.False(t, matched, "NONE must be matched case-insensitively")
}

func TestResolveMatch_EmptyReplyMeansNoMatch(t *testing.T) {
	matched, _ := resolveMatch("   ", testCandidates())
	require.False(t, matched)
}

// TestResolveMatch_HallucinatedMatchIsNeverTrusted is the non-negotiable
// defensive requirement, asserted directly: a model claiming a match that
// was never in the candidate list it was actually given must NEVER be
// treated as a real match, no matter how plausible-sounding the claimed
// text is.
func TestResolveMatch_HallucinatedMatchIsNeverTrusted(t *testing.T) {
	matched, candidate := resolveMatch("What was our margin last Tuesday?", testCandidates())
	require.False(t, matched, "a question not present in the candidate list must never be trusted as a match")
	require.Equal(t, answercache.Candidate{}, candidate)
}

func TestResolveMatch_CorruptedOrPartialEchoIsNotTrusted(t *testing.T) {
	// A near-miss (truncated, or with extra invented words) must not
	// fuzzy-match — resolveMatch requires the normalized text to be
	// EXACTLY equal to a real candidate, never "close enough".
	matched, _ := resolveMatch("What was our margin", testCandidates())
	require.False(t, matched)

	matched, _ = resolveMatch("What was our margin on 2026-08-07? Also how's the weather?", testCandidates())
	require.False(t, matched)
}

func TestResolveMatch_EmptyCandidateListNeverMatches(t *testing.T) {
	matched, _ := resolveMatch("What was our margin on 2026-08-07?", nil)
	require.False(t, matched)
}

// --- Classify boundary (no live call needed for these) --------------------

func TestClassify_RejectsEmptyQuestion(t *testing.T) {
	m := New(llmclient.New())
	_, err := m.Classify(context.Background(), "   ", testCandidates())
	require.ErrorIs(t, err, ErrEmptyQuestion)
}

// TestClassify_EmptyCandidateListMakesNoAPICall proves the "on a miss, if
// the cache is non-empty" condition is free: Classify must short-circuit to
// Matched=false without ever constructing a request, so an empty cache
// never costs a wasted classification call. This is provable without a live
// API key: a real client with no candidates must still return instantly and
// without error (a real API call to a bogus/no key would instead fail).
func TestClassify_EmptyCandidateListMakesNoAPICall(t *testing.T) {
	m := New(llmclient.New(llmclient.WithAPIKey("not-a-real-key-if-this-is-called-the-test-should-fail")))
	decision, err := m.Classify(context.Background(), "Any question at all?", nil)
	require.NoError(t, err, "an empty candidate list must short-circuit before any API call is attempted")
	require.False(t, decision.Matched)
	require.Zero(t, decision.EstimatedCostUSD)
}

// testDataStart/testDataEnd mirror internal/ambiguity's own test constants
// — this package doesn't use them for date grounding, but keeping the same
// literal values documents which data window the live tests below assume.
const (
	testDataStart = "2026-08-01"
	testDataEnd   = "2026-08-14"
)

// TestMatcher_Classify_LiveSmokeTest makes a small number of real Claude
// Haiku 4.5 calls proving the real request/response shape works: a genuine
// paraphrase is recognized, and an unrelated new question with no candidate
// at all correctly resolves to NONE. Skipped, not faked, when
// ANTHROPIC_API_KEY isn't set — same allowance internal/ambiguity's own
// live smoke test uses.
func TestMatcher_Classify_LiveSmokeTest(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set; skipping live Claude Haiku 4.5 smoke test")
	}

	m := New(llmclient.New())
	ctx := context.Background()
	candidates := []answercache.Candidate{
		{NormalizedQuestion: "what was our margin on 2026-08-07?", OriginalQuestion: "What was our margin on 2026-08-07?"},
		{NormalizedQuestion: "how did we do this week?", OriginalQuestion: "How did we do this week?"},
	}

	t.Run("genuine paraphrase is recognized", func(t *testing.T) {
		decision, err := m.Classify(ctx, "How did we do on August 7th?", candidates)
		require.NoError(t, err)
		require.True(t, decision.Matched, "a same-date, same-metric paraphrase must be recognized as a match")
		require.Equal(t, "what was our margin on 2026-08-07?", decision.MatchedCandidate.NormalizedQuestion)
		require.Greater(t, decision.InputTokens, int64(0))
		require.GreaterOrEqual(t, decision.EstimatedCostUSD, 0.0)
		t.Logf("paraphrase smoke test: matched=%v %d in / %d out tokens, $%.6f", decision.Matched, decision.InputTokens, decision.OutputTokens, decision.EstimatedCostUSD)
	})

	t.Run("an unrelated question does not match anything", func(t *testing.T) {
		decision, err := m.Classify(ctx, "What was our promotion ROI on the Just Eat lunch campaign?", candidates)
		require.NoError(t, err)
		require.False(t, decision.Matched, "an unrelated question must not match any candidate")
		t.Logf("unrelated smoke test: matched=%v $%.6f", decision.Matched, decision.EstimatedCostUSD)
	})
}

// TestMatcher_Classify_MeaningfullyDifferentQuestionNeverMatches is the
// non-negotiable safety test the task requires: a pair of questions that
// are superficially similar (same shape, same metric, near-identical
// wording) but meaningfully different (a different date) must NOT be
// classified as a match — spec FR-002 "a false cache hit is a worse outcome
// than a missed one". Run 3 times against the live model rather than once,
// because a single lucky pass would not be convincing evidence of the
// safety property this test exists to guard.
func TestMatcher_Classify_MeaningfullyDifferentQuestionNeverMatches(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set; skipping live Claude Haiku 4.5 safety test")
	}

	m := New(llmclient.New())
	ctx := context.Background()
	candidates := []answercache.Candidate{
		{NormalizedQuestion: "what was our margin on 2026-08-07?", OriginalQuestion: "What was our margin on 2026-08-07?"},
	}

	differentDate := "What was our margin on 2026-08-08?"
	differentScope := "What was our margin this week?"

	for i := 1; i <= 3; i++ {
		t.Run("different date, run "+string(rune('0'+i)), func(t *testing.T) {
			decision, err := m.Classify(ctx, differentDate, candidates)
			require.NoError(t, err)
			require.False(t, decision.Matched, "a one-day-apart date must never be classified as the same question")
		})
	}

	decision, err := m.Classify(ctx, differentScope, candidates)
	require.NoError(t, err)
	require.False(t, decision.Matched, `"this week" must never match a specific single date`)
}
