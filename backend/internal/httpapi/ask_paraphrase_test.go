package httpapi

// TestParaphraseMatch_* prove specs/004-semantic-cache's wiring into
// HandleAsk using a FAKE ParaphraseClassifier — the same "counting double"
// pattern ask_cache_test.go already uses for Classifier/Narrator — so these
// tests assert the orchestration (when the classifier is/isn't consulted,
// what gets served, what gets recorded) deterministically and at zero cost,
// without depending on the real model's judgment. The real model's
// judgment itself (does it actually recognize a paraphrase, does it
// actually refuse a meaningfully-different pair) is proven separately by
// internal/paraphrase's own live smoke tests.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ambiguity"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/answercache"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/explain"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/instrumentation"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/paraphrase"
)

// fakeParaphraseMatcher is a controllable stand-in for *paraphrase.Matcher —
// it never touches the Anthropic API, so these tests are free and
// deterministic. Its whole job is to prove HandleAsk reacts correctly to
// each of the three outcomes internal/paraphrase.Classify can produce
// (verified match / no match / an error), not to prove the real
// classification judgment itself.
type fakeParaphraseMatcher struct {
	calls          int
	decision       paraphrase.Decision
	err            error
	lastQuestion   string
	lastCandidates []answercache.Candidate
}

func (f *fakeParaphraseMatcher) Classify(_ context.Context, question string, candidates []answercache.Candidate) (*paraphrase.Decision, error) {
	f.calls++
	f.lastQuestion = question
	f.lastCandidates = candidates
	if f.err != nil {
		return nil, f.err
	}
	d := f.decision
	return &d, nil
}

// paraphraseHarness mirrors askHarness (ask_cache_test.go) but also wires a
// fakeParaphraseMatcher into Deps — kept as its own small harness rather
// than extending askHarness, so every existing cache test keeps proving
// this feature is OFF by default (Deps.ParaphraseMatcher nil) unless a test
// explicitly opts in.
type paraphraseHarness struct {
	handler         http.HandlerFunc
	gate            *countingGate
	explainer       *countingExplainer
	instrumentation *recordingInstrumentationStore
	cacheStore      *memoryCacheStore
	cache           *answercache.Cache
	matcher         *fakeParaphraseMatcher
}

func newParaphraseHarness(t *testing.T) *paraphraseHarness {
	t.Helper()

	gate := &countingGate{decision: ambiguity.Decision{
		Result:           instrumentation.GateAnswerable,
		InputTokens:      420,
		OutputTokens:     18,
		EstimatedCostUSD: 0.00051,
		LatencyMs:        310,
	}}
	explainer := &countingExplainer{result: explain.Result{
		AnswerText:       "Margin on 2026-08-07 was $375.82.",
		ProvenanceRefs:   []string{"data/live/daily_reconciliation.csv:7"},
		InputTokens:      1180,
		OutputTokens:     240,
		EstimatedCostUSD: 0.00476,
		LatencyMs:        1420,
	}}
	instrumentationStore := &recordingInstrumentationStore{}
	cacheStore := newMemoryCacheStore()
	cache := answercache.New(cacheStore)
	matcher := &fakeParaphraseMatcher{}

	return &paraphraseHarness{
		handler: HandleAsk(Deps{
			Gate:              gate,
			Explainer:         explainer,
			Logger:            instrumentation.NewLogger(instrumentationStore),
			Cache:             cache,
			ParaphraseMatcher: matcher,
		}),
		gate:            gate,
		explainer:       explainer,
		instrumentation: instrumentationStore,
		cacheStore:      cacheStore,
		cache:           cache,
		matcher:         matcher,
	}
}

func (h *paraphraseHarness) ask(t *testing.T, question string) AskResponse {
	t.Helper()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/ask",
		strings.NewReader(`{"question":`+quote(question)+`}`))
	h.handler(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, "body: %s", recorder.Body.String())
	var response AskResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

// TestParaphraseMatch_EmptyCacheNeverConsultsTheClassifier proves plan.md's
// "on a miss, if the cache is non-empty" condition: the very first question
// of a session has nothing to possibly paraphrase, so the classifier must
// never even be called — no candidates to check means no reason to spend a
// classification call on a comparison against nothing.
func TestParaphraseMatch_EmptyCacheNeverConsultsTheClassifier(t *testing.T) {
	h := newParaphraseHarness(t)

	h.ask(t, "What was our margin on 2026-08-07?")

	require.Equal(t, 0, h.matcher.calls, "an empty cache must never trigger a classification call")
	require.Equal(t, 1, h.gate.calls)
	require.Equal(t, 1, h.explainer.calls)
}

// TestParaphraseMatch_VerifiedMatchIsServedWithRealClassificationCostAndAvoidedCostSeparate
// is User Story 1's core claim end-to-end through the handler: a paraphrase
// of an already-cached question is served from cache without a second
// gate+explain cycle, AND the wire response distinguishes the real (small)
// classification cost it spent from the (larger) cost it avoided — the two
// numbers spec FR-005 requires never be netted together.
func TestParaphraseMatch_VerifiedMatchIsServedWithRealClassificationCostAndAvoidedCostSeparate(t *testing.T) {
	h := newParaphraseHarness(t)
	original := "What was our margin on 2026-08-07?"

	first := h.ask(t, original)
	require.Nil(t, first.Cache, "the first, uncached answer must not claim a cache hit")
	var originalTotalCost float64
	for _, i := range first.Interactions {
		originalTotalCost += i.EstimatedCostUSD
	}
	recordsAfterFirstAsk := len(h.instrumentation.records)

	// The classifier claims the matched candidate is exactly the entry that
	// really is in the cache — the "verified" case.
	h.matcher.decision = paraphrase.Decision{
		Matched: true,
		MatchedCandidate: answercache.Candidate{
			NormalizedQuestion: answercache.Normalize(original),
			OriginalQuestion:   original,
		},
		InputTokens:      480,
		OutputTokens:     6,
		EstimatedCostUSD: 0.00051,
		LatencyMs:        280,
	}

	paraphraseText := "How did we do on August 7th?"
	second := h.ask(t, paraphraseText)

	require.Equal(t, 1, h.matcher.calls)
	require.Equal(t, original, h.matcher.lastCandidates[0].OriginalQuestion, "the classifier must be offered the real cached question as a candidate")
	require.Equal(t, paraphraseText, h.matcher.lastQuestion)

	// Neither the gate nor explain ran a second time — this is the whole
	// point of the feature.
	require.Equal(t, 1, h.gate.calls, "the ambiguity gate must not re-run for a recognized paraphrase")
	require.Equal(t, 1, h.explainer.calls, "explain must not re-run for a recognized paraphrase")

	// The served answer is the ORIGINAL cached answer.
	require.Equal(t, first.AnswerText, second.AnswerText)

	require.NotNil(t, second.Cache)
	require.True(t, second.Cache.Hit)
	require.Equal(t, "paraphrase", second.Cache.MatchKind, "a paraphrase hit must be distinguishable from an exact-text hit (FR-004)")
	require.Equal(t, originalTotalCost, second.Cache.CostAvoidedUSD, "CostAvoidedUSD must be the full original gate+explain spend")
	require.NotEmpty(t, second.Cache.Note)

	// The REAL classification cost is reported as the one interaction that
	// actually ran this request — never zero, and never merged with the
	// avoided cost.
	require.Len(t, second.Interactions, 1, "exactly one real call — the classification — ran for this request")
	require.Equal(t, 0.00051, second.Interactions[0].EstimatedCostUSD)
	require.NotEqual(t, second.Interactions[0].EstimatedCostUSD, second.Cache.CostAvoidedUSD,
		"the real cost incurred and the cost avoided must be different, never-netted numbers (FR-005)")

	// The ledger row records both numbers distinctly too.
	require.Len(t, h.cacheStore.paraphraseMatches, 1)
	row := h.cacheStore.paraphraseMatches[0]
	require.Equal(t, paraphraseText, row.NewQuestion)
	require.Equal(t, answercache.Normalize(original), row.MatchedNormalizedQuestion)

	// A paraphrase hit is not a real model interaction the way the gate or
	// explain are — it must not add a fabricated question_interaction row
	// (same non-interaction discipline as an exact-text hit). The first ask
	// legitimately wrote 2 rows (gate + explain for a REAL fresh answer);
	// the second, paraphrase-served ask must add zero more.
	require.Len(t, h.instrumentation.records, recordsAfterFirstAsk, "a paraphrase hit must not write any additional question_interaction row")
}

// TestParaphraseMatch_HallucinatedClaimIsNeverServed is the non-negotiable
// defensive requirement at the HTTP-orchestration layer: even if
// internal/paraphrase's own verification somehow let through a claim that
// does not actually exist in the live cache (simulated here directly via
// the fake, to isolate this layer's OWN re-verification from that
// package's), HandleAsk must re-check against the real cache before
// serving anything, and fall through to a fresh answer when it doesn't
// verify — never crash, never serve garbage.
func TestParaphraseMatch_HallucinatedClaimIsNeverServed(t *testing.T) {
	h := newParaphraseHarness(t)
	h.ask(t, "What was our margin on 2026-08-07?") // seed one real cache entry

	h.matcher.decision = paraphrase.Decision{
		Matched: true,
		MatchedCandidate: answercache.Candidate{
			// This normalized question was never actually cached — a
			// hallucination/corruption that the candidate-list check
			// (internal/paraphrase's own resolveMatch) is meant to catch,
			// but this test proves the SECOND, independent live-cache
			// check here catches it too, defense in depth.
			NormalizedQuestion: "what was our margin on 2099-01-01?",
			OriginalQuestion:   "What was our margin on 2099-01-01?",
		},
		InputTokens:      400,
		OutputTokens:     6,
		EstimatedCostUSD: 0.0004,
		LatencyMs:        200,
	}

	resp := h.ask(t, "A totally different but supposedly-matched question")

	require.Equal(t, 2, h.gate.calls, "an unverifiable claim must fall through to a fresh gate+explain cycle")
	require.Equal(t, 2, h.explainer.calls)
	require.Equal(t, "answered", resp.Status)
	require.Nil(t, resp.Cache, "a hallucinated match must never be reported as a cache hit")
	require.Empty(t, h.cacheStore.paraphraseMatches, "no ledger row may be written for a match that never verified")
}

// TestParaphraseMatch_NoMatchFallsThroughNormally proves the plain "NONE"
// path (spec Acceptance Scenario 2: superficially similar but meaningfully
// different) at the wiring layer: when the classifier reports no match,
// HandleAsk must run the full gate+explain cycle exactly as if this feature
// did not exist.
func TestParaphraseMatch_NoMatchFallsThroughNormally(t *testing.T) {
	h := newParaphraseHarness(t)
	h.ask(t, "What was our margin on 2026-08-07?")

	h.matcher.decision = paraphrase.Decision{Matched: false}

	resp := h.ask(t, "What was our margin on 2026-08-08?")

	require.Equal(t, 1, h.matcher.calls)
	require.Equal(t, 2, h.gate.calls, "no match means a full, fresh answer")
	require.Equal(t, 2, h.explainer.calls)
	require.Nil(t, resp.Cache)
	require.Equal(t, "answered", resp.Status)
}

// TestParaphraseMatch_ClassifierErrorDegradesToFullPrice proves a broken
// classification call fails the same way a broken exact-match cache
// already does (see TestHandlerWithoutACacheBehavesExactlyAsBefore's
// sibling coverage in ask_cache_test.go): it never fails the request, it
// just costs full price for that one question.
func TestParaphraseMatch_ClassifierErrorDegradesToFullPrice(t *testing.T) {
	h := newParaphraseHarness(t)
	h.ask(t, "What was our margin on 2026-08-07?")

	h.matcher.err = context.DeadlineExceeded

	resp := h.ask(t, "How did we do on August 7th, roughly?")

	require.Equal(t, 2, h.gate.calls)
	require.Equal(t, 2, h.explainer.calls)
	require.Equal(t, "answered", resp.Status)
	require.Nil(t, resp.Cache)
}

// TestParaphraseMatch_ExactHitStillReportsMatchKindExact is FR-004's other
// half: an exact-text hit must report its own distinct MatchKind, so a
// client can tell fresh / exact / paraphrase apart as three states, never
// two.
func TestParaphraseMatch_ExactHitStillReportsMatchKindExact(t *testing.T) {
	h := newParaphraseHarness(t)
	question := "What was our margin on 2026-08-07?"

	h.ask(t, question)
	second := h.ask(t, "  what WAS our margin on 2026-08-07?  ")

	require.NotNil(t, second.Cache)
	require.Equal(t, "exact", second.Cache.MatchKind)
	require.Equal(t, 0, h.matcher.calls, "an exact-match hit must short-circuit before the paraphrase classifier is ever consulted")
}

// askWithBody posts a raw JSON body through a paraphraseHarness, mirroring
// askHarness.askWithBody (ask_clarification_test.go) — needed here because
// pending_clarification/previous_exchange aren't expressible through
// paraphraseHarness.ask's bare-question convenience method.
func (h *paraphraseHarness) askWithBody(t *testing.T, body string) AskResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	h.handler(recorder, httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(body)))
	require.Equal(t, http.StatusOK, recorder.Code, "body: %s", recorder.Body.String())
	var response AskResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

// TestParaphraseMatch_SkippedOnClarificationReply pins the fix for a live
// failure: a clarification reply's `resolved` text is
// ambiguity.ComposeFollowUp(question, pending), which TEXTUALLY CONTAINS the
// original ambiguous question — so paraphrase-matching it reliably matched
// straight back onto that same cached (clarification-required) entry,
// re-serving the identical clarifying question forever. The fix is to never
// consult the paraphrase classifier at all when a request carries
// pending_clarification: a reply always deserves a fresh classification.
func TestParaphraseMatch_SkippedOnClarificationReply(t *testing.T) {
	h := newParaphraseHarness(t)
	// Even if the classifier WOULD claim a match, it must never be asked —
	// so an aggressive fake decision here proves the skip, not the
	// classifier's own judgment.
	h.matcher.decision = paraphrase.Decision{Matched: true, MatchedCandidate: answercache.Candidate{
		NormalizedQuestion: "anything",
		OriginalQuestion:   "anything",
	}}

	h.askWithBody(t, `{
		"question": "The iFood promotion",
		"pending_clarification": {
			"original_question": "How did the promotion do?",
			"clarifying_question": "Which promotion — iFood or Just Eat Takeaway?"
		}
	}`)

	require.Equal(t, 0, h.matcher.calls, "a clarification reply must never be paraphrase-matched")
	require.Equal(t, 1, h.gate.calls, "a clarification reply must always reach a fresh gate classification")
}

// TestParaphraseMatch_SkippedOnAnswerFollowUp is the bare-follow-up sibling
// of the test above ("and the day before?" following a real answer, rather
// than a reply to a clarifying question) — same composed-text risk, same
// fix, same must-never-consult-the-classifier assertion.
func TestParaphraseMatch_SkippedOnAnswerFollowUp(t *testing.T) {
	h := newParaphraseHarness(t)
	h.matcher.decision = paraphrase.Decision{Matched: true, MatchedCandidate: answercache.Candidate{
		NormalizedQuestion: "anything",
		OriginalQuestion:   "anything",
	}}

	h.askWithBody(t, `{
		"question": "and the day before?",
		"previous_exchange": {
			"question": "What was our margin on 2026-08-07?",
			"answer_text": "Margin on 2026-08-07 was $375.82."
		}
	}`)

	require.Equal(t, 0, h.matcher.calls, "a bare answer follow-up must never be paraphrase-matched")
	require.Equal(t, 1, h.gate.calls, "a follow-up must always reach a fresh gate classification")
}

// TestParaphraseMatch_ReportsItsOwnModelNotTheGates pins a display-only bug
// found live: a paraphrase-hit interaction's ModelUsed was hardcoded to
// llmclient.ModelAmbiguityGate (the gate's model, which did not run on this
// request) instead of llmclient.ModelParaphraseMatch (Haiku 4.5, the model
// that actually ran) — the real call and its real cost were always correct,
// only the reported model name was wrong.
func TestParaphraseMatch_ReportsItsOwnModelNotTheGates(t *testing.T) {
	h := newParaphraseHarness(t)
	original := "What was our margin on 2026-08-07?"
	h.ask(t, original)

	h.matcher.decision = paraphrase.Decision{
		Matched: true,
		MatchedCandidate: answercache.Candidate{
			NormalizedQuestion: answercache.Normalize(original),
			OriginalQuestion:   original,
		},
		EstimatedCostUSD: 0.00051,
	}
	second := h.ask(t, "How did we do on August 7th?")

	require.Len(t, second.Interactions, 1)
	require.Equal(t, "claude-haiku-4-5", second.Interactions[0].ModelUsed,
		"a paraphrase hit's reported interaction must name the classifier's own model, never the gate's")
}

// TestParaphraseMatch_ConflictingExplicitDatesNeverServed pins the live
// failure that motivated ambiguity.ExplicitDatesConflict: "Give me the
// exact margin total for August 2026" was paraphrase-matched to a cached
// "What was my average daily input cost in July 2026?" answer and served
// as answered — a real, well-formed cached candidate, just the wrong month.
// The classifier here claims exactly that (a verifiable, non-hallucinated
// match) to prove the guardrail catches a WRONG match, not only an invented
// one — the two existing defensive checks (candidate-list, live re-lookup)
// both pass this case, which is exactly why it needed its own check.
func TestParaphraseMatch_ConflictingExplicitDatesNeverServed(t *testing.T) {
	h := newParaphraseHarness(t)
	original := "What was my average daily input cost in July 2026?"
	h.ask(t, original)

	h.matcher.decision = paraphrase.Decision{
		Matched: true,
		MatchedCandidate: answercache.Candidate{
			NormalizedQuestion: answercache.Normalize(original),
			OriginalQuestion:   original,
		},
	}
	resp := h.ask(t, "Give me the exact margin total for August 2026.")

	require.Equal(t, 1, h.matcher.calls, "the classifier is still consulted -- the guardrail runs on its output")
	require.Equal(t, 2, h.gate.calls, "a conflicting-period match must fall through to a fresh, real answer")
	require.Nil(t, resp.Cache, "the mismatched-month cached answer must never be served")
}
