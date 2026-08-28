package httpapi

// TestExplanationFailureLogsPartialSpendAndReturnsASafeMessage and
// TestGateFailureReturnsASafeMessageWithoutLeakingInternals verify Findings
// 1 and 15c of the independent code-quality review at the HandleAsk level
// (internal/explain's own explain_internal_test.go covers Finding 1's
// Explain-level contract directly): a mid-loop internal/explain failure
// must not silently discard the real, billed Anthropic spend it already
// accumulated before failing (Constitution Principle VI — every model
// interaction must be logged), and neither the gate's nor explain's error
// path may leak a raw internal error string into the HTTP response body.
//
// These use their own minimal fakes rather than ask_cache_test.go's shared
// countingGate/countingExplainer (which only ever return a nil error) and
// build Deps directly with no Cache configured, so they exercise exactly
// the gate/explain error branches in ask.go without depending on the
// answer-cache machinery at all.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ambiguity"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/explain"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/instrumentation"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/llmclient"
)

// erroringGate always fails classification, carrying no Decision at all —
// exactly ambiguity.Gate.Classify's own (nil, err) contract on any failure.
type erroringGate struct {
	err error
}

func (g *erroringGate) Classify(_ context.Context, _ string, _ *ambiguity.PendingClarification, _ *ambiguity.PreviousExchange) (*ambiguity.Decision, error) {
	return nil, g.err
}

// fixedAnswerableGate always classifies answerable with a small fixed real
// cost, so tests exercising the EXPLAIN error path can reach it at all.
type fixedAnswerableGate struct{}

func (fixedAnswerableGate) Classify(_ context.Context, _ string, _ *ambiguity.PendingClarification, _ *ambiguity.PreviousExchange) (*ambiguity.Decision, error) {
	return &ambiguity.Decision{
		Result:           instrumentation.GateAnswerable,
		InputTokens:      10,
		OutputTokens:     2,
		EstimatedCostUSD: 0.0001,
		LatencyMs:        5,
	}, nil
}

// explainerReturningPartialResultOnError simulates Finding 1's exact
// scenario: a mid-loop failure whose returned *explain.Result still carries
// real, billed usage accumulated from the turns that succeeded before the
// failure (see explain.go's Explain doc comment).
type explainerReturningPartialResultOnError struct {
	result *explain.Result
	err    error
}

func (e explainerReturningPartialResultOnError) Explain(_ context.Context, _, _ string) (*explain.Result, error) {
	return e.result, e.err
}

func doAsk(t *testing.T, deps Deps, question string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	body, err := json.Marshal(map[string]string{"question": question})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(string(body)))
	HandleAsk(deps)(recorder, request)
	return recorder, recorder.Body.String()
}

func TestExplanationFailureLogsPartialSpendAndReturnsASafeMessage(t *testing.T) {
	store := &recordingInstrumentationStore{}
	partial := &explain.Result{
		IncompleteReason: "model call failed on turn 3: simulated transport failure",
		InputTokens:      3000,
		OutputTokens:     600,
		EstimatedCostUSD: 0.012,
		LatencyMs:        4200,
	}
	underlyingErr := errors.New("dial tcp 10.0.0.5:443: connection reset by peer (key sk-ant-secret123)")
	deps := Deps{
		Gate:      fixedAnswerableGate{},
		Explainer: explainerReturningPartialResultOnError{result: partial, err: underlyingErr},
		Logger:    instrumentation.NewLogger(store),
	}

	recorder, body := doAsk(t, deps, "What was our margin on 2026-08-03?")

	require.Equal(t, http.StatusBadGateway, recorder.Code)
	require.NotContains(t, body, "sk-ant-secret123", "the raw internal error must never reach the HTTP response body")
	require.NotContains(t, body, underlyingErr.Error())

	// The gate's own successful call, plus explain's partial-spend row —
	// both real, billed Anthropic calls, neither dropped.
	require.Len(t, store.records, 2, "the gate's call and explain's partial spend must both be logged")
	explainRecord := store.records[1]
	require.Equal(t, llmclient.ModelExplanation, explainRecord.ModelUsed)
	require.Equal(t, int64(3000), explainRecord.InputTokens)
	require.Equal(t, int64(600), explainRecord.OutputTokens)
	require.Equal(t, 0.012, explainRecord.EstimatedCostUSD)
	require.True(t, explainRecord.RefusalFired)
}

func TestGateFailureReturnsASafeMessageWithoutLeakingInternals(t *testing.T) {
	store := &recordingInstrumentationStore{}
	underlyingErr := errors.New(`postgres: password authentication failed for user "prod"`)
	deps := Deps{
		Gate:   &erroringGate{err: underlyingErr},
		Logger: instrumentation.NewLogger(store),
	}

	recorder, body := doAsk(t, deps, "What was our margin on 2026-08-03?")

	require.Equal(t, http.StatusBadGateway, recorder.Code)
	require.NotContains(t, body, "password authentication failed", "the raw internal error must never reach the HTTP response body")
	// ambiguity.Gate.Classify's error contract is (nil, err) on every
	// failure path, so there is no partial Decision here to log from — see
	// ask.go's comment at this call site.
	require.Empty(t, store.records)
}
