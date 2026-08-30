package httpapi

// Handler-level tests for specs/011-inline-grounded-advice: the inline
// advice call fires exactly when its deterministic preconditions hold
// (gate flag + answered + ≥1 real tool invocation + configured deps), is
// grounded in the SAME invocations the answer carries, surfaces its cost,
// writes its ledger row, and — on any failure — degrades to the unchanged
// computed answer. Counting fakes throughout; zero Anthropic API calls
// and zero Postgres (the same discipline every other HandleAsk test in
// this package follows).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/advisor"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ambiguity"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/explain"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/instrumentation"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/llmclient"
)

// adviceFlaggingGate classifies answerable with AdviceRequested set as
// configured — the typed signal ask.go's inline-advice branch keys on.
type adviceFlaggingGate struct {
	adviceRequested bool
}

func (g adviceFlaggingGate) Classify(_ context.Context, _ string, _ *ambiguity.PendingClarification, _ *ambiguity.PreviousExchange) (*ambiguity.Decision, error) {
	return &ambiguity.Decision{
		Result:           instrumentation.GateAnswerable,
		AdviceRequested:  g.adviceRequested,
		InputTokens:      10,
		OutputTokens:     2,
		EstimatedCostUSD: 0.0001,
		LatencyMs:        5,
	}, nil
}

// refusingAdviceGate refuses — the ungroundable-advice case ("what should
// I pay my staff?"): unanswerable, even though advice was requested.
type refusingAdviceGate struct{}

func (refusingAdviceGate) Classify(_ context.Context, _ string, _ *ambiguity.PendingClarification, _ *ambiguity.PreviousExchange) (*ambiguity.Decision, error) {
	return &ambiguity.Decision{
		Result:           instrumentation.GateUnanswerable,
		AdviceRequested:  true,
		RefusalReason:    "This product has no staffing or wage data — it computes margin, sales, cost, and promotion figures only.",
		InputTokens:      10,
		OutputTokens:     2,
		EstimatedCostUSD: 0.0001,
		LatencyMs:        5,
	}, nil
}

// invocationExplainer narrates a fixed answer with the given invocations
// and records the question text it was handed, so tests can assert the
// handoff note's presence/absence on the explain INPUT.
type invocationExplainer struct {
	invocations  []explain.ToolInvocation
	lastQuestion string
}

func (e *invocationExplainer) Explain(_ context.Context, question, _ string) (*explain.Result, error) {
	e.lastQuestion = question
	return &explain.Result{
		AnswerText:       "Here is what the data shows.",
		ToolInvocations:  e.invocations,
		ToolCallsMade:    len(e.invocations),
		InputTokens:      500,
		OutputTokens:     100,
		EstimatedCostUSD: 0.002,
		LatencyMs:        800,
	}, nil
}

// countingQuestionAdviser records every AdviseOnQuestion call and returns
// a fixed Advice (or a scripted error).
type countingQuestionAdviser struct {
	calls        int
	lastQuestion string
	lastResults  []advisor.ToolResult
	err          error
}

func (a *countingQuestionAdviser) AdviseOnQuestion(_ context.Context, question string, results []advisor.ToolResult) (*advisor.Advice, error) {
	a.calls++
	a.lastQuestion = question
	a.lastResults = results
	if a.err != nil {
		return nil, a.err
	}
	return &advisor.Advice{
		Text:             "Owners in this situation typically review their largest cost line first.",
		InputTokens:      900,
		OutputTokens:     150,
		EstimatedCostUSD: 0.0093,
		LatencyMs:        1200,
	}, nil
}

var adviceTestInvocations = []explain.ToolInvocation{
	{Name: "get_period_totals", ResultJSON: `{"start":"2026-08-01","end":"2026-08-14","margin_total":"1234.56"}`},
	{Name: "compare_platform_economics", ResultJSON: `{"platforms":[]}`},
}

func adviceTestDeps(gate Classifier, adviser QuestionAdviser, insightStore BusinessInsightStore, invocations []explain.ToolInvocation) (Deps, *invocationExplainer) {
	explainer := &invocationExplainer{invocations: invocations}
	return Deps{
		Gate:            gate,
		Explainer:       explainer,
		Logger:          instrumentation.NewLogger(&recordingInstrumentationStore{}),
		QuestionAdviser: adviser,
		InsightStore:    insightStore,
	}, explainer
}

func TestInlineAdviceFiresOnceGroundedInTheAnswersOwnInvocations(t *testing.T) {
	adviser := &countingQuestionAdviser{}
	insightStore := &recordingInsightStore{}
	deps, explainer := adviceTestDeps(adviceFlaggingGate{adviceRequested: true}, adviser, insightStore, adviceTestInvocations)

	recorder, body := doAsk(t, deps, "how can I improve my margin overall?")
	require.Equal(t, http.StatusOK, recorder.Code)

	require.Equal(t, 1, adviser.calls, "exactly one advisor call per flagged answered question")
	// Grounding is the SAME invocations the answer carries — name and raw
	// JSON, verbatim (spec FR-003).
	require.Len(t, adviser.lastResults, len(adviceTestInvocations))
	for i, inv := range adviceTestInvocations {
		require.Equal(t, inv.Name, adviser.lastResults[i].Name)
		require.Equal(t, inv.ResultJSON, adviser.lastResults[i].ResultJSON)
	}

	var resp AskResponse
	require.NoError(t, json.Unmarshal([]byte(body), &resp))
	require.Equal(t, "answered", resp.Status)
	require.NotNil(t, resp.Advice)
	require.Equal(t, "Owners in this situation typically review their largest cost line first.", resp.Advice.Text)
	require.Equal(t, BusinessInsightDisclaimer, resp.Advice.Disclaimer,
		"the inline path must carry the SAME standing disclaimer the teaser path established (spec FR-007)")

	// The call's real cost: its own interactions entry (gate + explain +
	// advice = 3) AND the advice block's embedded interaction agree.
	require.Len(t, resp.Interactions, 3)
	adviceInteraction := resp.Interactions[2]
	require.Equal(t, llmclient.ModelBusinessInsight, adviceInteraction.ModelUsed)
	require.Equal(t, int64(900), adviceInteraction.InputTokens)
	require.Equal(t, adviceInteraction, resp.Advice.Interaction)

	// The dedicated ledger row (spec FR-008), kind question_advice.
	require.Len(t, insightStore.rows, 1)
	require.Equal(t, advisor.KindQuestionAdvice, insightStore.rows[0].Kind)
	require.Equal(t, llmclient.ModelBusinessInsight, insightStore.rows[0].ModelUsed)
	require.Equal(t, int32(900), insightStore.rows[0].InputTokens)

	// The handoff note reached the narration input (spec FR-011)...
	require.Contains(t, explainer.lastQuestion, explain.AdviceHandoffNote)
	// ...and the advisor got the RESOLVED question, not the noted one —
	// the note is narration-only steering, never advice grounding.
	require.NotContains(t, adviser.lastQuestion, explain.AdviceHandoffNote)
	require.Contains(t, adviser.lastQuestion, "how can I improve my margin overall?")
}

func TestInlineAdviceDoesNotFireWhenTheGateDidNotFlagIt(t *testing.T) {
	adviser := &countingQuestionAdviser{}
	deps, explainer := adviceTestDeps(adviceFlaggingGate{adviceRequested: false}, adviser, &recordingInsightStore{}, adviceTestInvocations)

	_, body := doAsk(t, deps, "what was my margin last week?")

	require.Equal(t, 0, adviser.calls, "a pure data question must never trigger an advice call")
	var resp AskResponse
	require.NoError(t, json.Unmarshal([]byte(body), &resp))
	require.Nil(t, resp.Advice)
	require.Len(t, resp.Interactions, 2, "gate + explain only — no advice cost")
	require.NotContains(t, explainer.lastQuestion, explain.AdviceHandoffNote,
		"the handoff note must not steer the narration when no advice will follow")
}

func TestUngroundableAdviceQuestionIsRefusedWithZeroAdviserCalls(t *testing.T) {
	adviser := &countingQuestionAdviser{}
	deps, _ := adviceTestDeps(refusingAdviceGate{}, adviser, &recordingInsightStore{}, adviceTestInvocations)

	recorder, body := doAsk(t, deps, "what should I pay my staff?")

	require.Equal(t, http.StatusOK, recorder.Code)
	var resp AskResponse
	require.NoError(t, json.Unmarshal([]byte(body), &resp))
	require.Equal(t, "refused", resp.Status)
	require.NotEmpty(t, resp.RefusalReason)
	require.Nil(t, resp.Advice)
	require.Equal(t, 0, adviser.calls,
		"an ungroundable advice question must decline plainly — never a generic ungrounded suggestion (spec FR-006)")
}

func TestInlineAdviceSkippedWhenNoToolInvocationsExist(t *testing.T) {
	adviser := &countingQuestionAdviser{}
	deps, _ := adviceTestDeps(adviceFlaggingGate{adviceRequested: true}, adviser, &recordingInsightStore{}, nil)

	_, body := doAsk(t, deps, "any advice on out-of-range stuff?")

	require.Equal(t, 0, adviser.calls, "no grounding means no advice — never an ungrounded call")
	var resp AskResponse
	require.NoError(t, json.Unmarshal([]byte(body), &resp))
	require.Nil(t, resp.Advice)
}

func TestInlineAdviceFailureDegradesToTheUnchangedAnswer(t *testing.T) {
	adviser := &countingQuestionAdviser{err: errors.New("simulated transport failure")}
	insightStore := &recordingInsightStore{}
	deps, _ := adviceTestDeps(adviceFlaggingGate{adviceRequested: true}, adviser, insightStore, adviceTestInvocations)

	recorder, body := doAsk(t, deps, "how can I improve my margin overall?")

	require.Equal(t, http.StatusOK, recorder.Code, "a broken suggestion must never fail the answer (spec FR-009)")
	var resp AskResponse
	require.NoError(t, json.Unmarshal([]byte(body), &resp))
	require.Equal(t, "answered", resp.Status)
	require.Equal(t, "Here is what the data shows.", resp.AnswerText)
	require.Nil(t, resp.Advice)
	require.Len(t, resp.Interactions, 2, "no advice interaction — the failed call has no partial usage to report")
	require.Empty(t, insightStore.rows)
}

func TestInlineAdviceSkippedEntirelyWhenNoAdviserConfigured(t *testing.T) {
	deps, explainer := adviceTestDeps(adviceFlaggingGate{adviceRequested: true}, nil, nil, adviceTestInvocations)

	recorder, body := doAsk(t, deps, "how can I improve my margin overall?")

	require.Equal(t, http.StatusOK, recorder.Code)
	var resp AskResponse
	require.NoError(t, json.Unmarshal([]byte(body), &resp))
	require.Equal(t, "answered", resp.Status)
	require.Nil(t, resp.Advice)
	require.NotContains(t, explainer.lastQuestion, explain.AdviceHandoffNote,
		"with no adviser configured the narration must keep its pre-011 plain-decline behavior — no note, no promise")
}

// The closed five-kind teaser endpoint stays closed: question_advice is a
// ledger kind, never a postable teaser kind (spec FR-010 / User Story 3).
func TestBusinessInsightEndpointRejectsQuestionAdviceKind(t *testing.T) {
	require.False(t, advisor.KnownKind(advisor.KindQuestionAdvice))
}
