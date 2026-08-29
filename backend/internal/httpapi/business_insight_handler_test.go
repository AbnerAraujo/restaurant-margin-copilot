package httpapi

// Tests for POST /api/business-insight (specs/009-business-insight-advisor
// User Story 2), against a counting fake Adviser and a recording ledger
// store — the same counting-double discipline ask_cache_test.go documents:
// "the advice call is opt-in and re-verified" is only proven by observing
// the Adviser NOT being invoked on the refusal paths, never by the
// response shape alone.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/advisor"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/llmclient"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

type countingAdviser struct {
	calls       int
	lastKind    string
	lastResults []advisor.ToolResult
	advice      advisor.Advice
	err         error
}

func (a *countingAdviser) Advise(_ context.Context, kind string, results []advisor.ToolResult) (*advisor.Advice, error) {
	a.calls++
	a.lastKind = kind
	a.lastResults = results
	if a.err != nil {
		return nil, a.err
	}
	advice := a.advice
	return &advice, nil
}

type recordingInsightStore struct {
	rows []storage.CreateBusinessInsightInteractionParams
	err  error
}

func (s *recordingInsightStore) CreateBusinessInsightInteraction(_ context.Context, arg storage.CreateBusinessInsightInteractionParams) (storage.BusinessInsightInteraction, error) {
	if s.err != nil {
		return storage.BusinessInsightInteraction{}, s.err
	}
	s.rows = append(s.rows, arg)
	return storage.BusinessInsightInteraction{}, nil
}

type insightHarness struct {
	handler http.HandlerFunc
	adviser *countingAdviser
	store   *recordingInsightStore
}

func newInsightHarness(t *testing.T) *insightHarness {
	t.Helper()
	adviser := &countingAdviser{advice: advisor.Advice{
		Text:             "Restaurants in this situation typically reconcile daily and dispute invalid deductions promptly.",
		InputTokens:      1420,
		OutputTokens:     190,
		EstimatedCostUSD: 0.00474,
		LatencyMs:        2100,
	}}
	store := &recordingInsightStore{}
	return &insightHarness{
		handler: HandleBusinessInsight(BusinessInsightDeps{Adviser: adviser, Store: store}),
		adviser: adviser,
		store:   store,
	}
}

func (h *insightHarness) post(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/business-insight", strings.NewReader(body))
	h.handler(recorder, request)
	return recorder
}

// flaggedInsightRequestBody grounds a discrepancy_pattern request in the
// same flagged daily-summary fixture the derivation tests use.
func flaggedInsightRequestBody() string {
	return `{"kind":"discrepancy_pattern","tool_calls":[{"name":"get_daily_summary","result_json":` + flaggedDailySummaryJSON + `}]}`
}

func TestBusinessInsightSuccessReturnsAdviceWithRealCostAndLedgersIt(t *testing.T) {
	h := newInsightHarness(t)

	recorder := h.post(t, flaggedInsightRequestBody())

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", recorder.Code, recorder.Body.String())
	}
	var resp BusinessInsightResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.Kind != advisor.KindDiscrepancyPattern {
		t.Errorf("kind = %q, want %q", resp.Kind, advisor.KindDiscrepancyPattern)
	}
	if resp.AdviceText != h.adviser.advice.Text {
		t.Errorf("advice_text = %q, want the adviser's real text", resp.AdviceText)
	}
	if resp.Disclaimer != BusinessInsightDisclaimer {
		t.Errorf("disclaimer = %q, want the shared disclosure constant", resp.Disclaimer)
	}
	// The call's real measured cost, never zero or a placeholder.
	if resp.Interaction.ModelUsed != llmclient.ModelBusinessInsight ||
		resp.Interaction.InputTokens != 1420 ||
		resp.Interaction.OutputTokens != 190 ||
		resp.Interaction.EstimatedCostUSD != 0.00474 ||
		resp.Interaction.LatencyMs != 2100 {
		t.Errorf("interaction = %+v, want the adviser's real measured figures", resp.Interaction)
	}

	if h.adviser.calls != 1 {
		t.Fatalf("adviser calls = %d, want exactly 1", h.adviser.calls)
	}
	if h.adviser.lastKind != advisor.KindDiscrepancyPattern {
		t.Errorf("adviser was given kind %q, want %q", h.adviser.lastKind, advisor.KindDiscrepancyPattern)
	}
	if len(h.adviser.lastResults) != 1 || h.adviser.lastResults[0].Name != "get_daily_summary" || h.adviser.lastResults[0].ResultJSON != flaggedDailySummaryJSON {
		t.Errorf("adviser grounding = %+v, want the posted tool result verbatim", h.adviser.lastResults)
	}

	// Constitution Principle VI: exactly one dedicated ledger row, with
	// the same figures the response reported.
	if len(h.store.rows) != 1 {
		t.Fatalf("ledger rows = %d, want exactly 1", len(h.store.rows))
	}
	row := h.store.rows[0]
	if row.Kind != advisor.KindDiscrepancyPattern ||
		row.AdviceText != h.adviser.advice.Text ||
		row.ModelUsed != llmclient.ModelBusinessInsight ||
		row.InputTokens != 1420 || row.OutputTokens != 190 || row.LatencyMs != 2100 {
		t.Errorf("ledger row = %+v, want the same real figures the response carried", row)
	}
	if !strings.Contains(string(row.GroundingToolCalls), "get_daily_summary") {
		t.Errorf("grounding_tool_calls = %s, want the posted tool calls", row.GroundingToolCalls)
	}
	cost, err := row.EstimatedCostUsd.Float64Value()
	if err != nil || !cost.Valid || cost.Float64 != 0.00474 {
		t.Errorf("ledger estimated_cost_usd = %+v (err %v), want 0.00474", cost, err)
	}
}

func TestBusinessInsightRefusesKindUnsupportedByPostedDataWithoutAnyAdviserCall(t *testing.T) {
	h := newInsightHarness(t)

	// A flagged daily summary grounds discrepancy_pattern, not
	// high_commission — a stale/tampered/mismatched claim must be refused
	// BEFORE any tokens are spent (spec SC-005).
	recorder := h.post(t, `{"kind":"high_commission","tool_calls":[{"name":"get_daily_summary","result_json":`+flaggedDailySummaryJSON+`}]}`)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (body: %s)", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "insight_not_supported") {
		t.Errorf("body = %s, want the typed insight_not_supported error", recorder.Body.String())
	}
	if h.adviser.calls != 0 {
		t.Errorf("adviser calls = %d, want 0 — no tokens spent on a refused request", h.adviser.calls)
	}
	if len(h.store.rows) != 0 {
		t.Errorf("ledger rows = %d, want 0 — nothing ran, nothing to ledger", len(h.store.rows))
	}
}

func TestBusinessInsightRefusesCleanDataForAnyKind(t *testing.T) {
	h := newInsightHarness(t)

	recorder := h.post(t, `{"kind":"discrepancy_pattern","tool_calls":[{"name":"get_daily_summary","result_json":`+cleanDailySummaryJSON+`}]}`)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 for clean data (body: %s)", recorder.Code, recorder.Body.String())
	}
	if h.adviser.calls != 0 {
		t.Errorf("adviser calls = %d, want 0", h.adviser.calls)
	}
}

func TestBusinessInsightValidatesInputBeforeAnything(t *testing.T) {
	h := newInsightHarness(t)

	cases := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{"unknown kind", `{"kind":"astrology","tool_calls":[{"name":"get_daily_summary","result_json":{}}]}`, http.StatusBadRequest, "invalid_input"},
		{"missing tool_calls", `{"kind":"discrepancy_pattern"}`, http.StatusBadRequest, "invalid_input"},
		{"invalid JSON", `{not json`, http.StatusBadRequest, "invalid_input"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := h.post(t, tc.body)
			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", recorder.Code, tc.wantStatus, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), tc.wantCode) {
				t.Errorf("body = %s, want error code %q", recorder.Body.String(), tc.wantCode)
			}
		})
	}
	if h.adviser.calls != 0 {
		t.Errorf("adviser calls = %d, want 0 across every validation refusal", h.adviser.calls)
	}

	recorder := httptest.NewRecorder()
	h.handler(recorder, httptest.NewRequest(http.MethodGet, "/api/business-insight", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want 405", recorder.Code)
	}
}

func TestBusinessInsightAdviserFailureIs502WithNoLedgerRow(t *testing.T) {
	h := newInsightHarness(t)
	h.adviser.err = errors.New("anthropic api unavailable")

	recorder := h.post(t, flaggedInsightRequestBody())

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (body: %s)", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "advice_failed") {
		t.Errorf("body = %s, want the typed advice_failed error", recorder.Body.String())
	}
	if len(h.store.rows) != 0 {
		t.Errorf("ledger rows = %d, want 0 — advisor.Advise's (nil, err) contract leaves no partial usage to log", len(h.store.rows))
	}
}

func TestBusinessInsightLedgerFailureNeverDeniesTheAdvice(t *testing.T) {
	// The same logOrWarn discipline the ask handler applies: a failed
	// instrumentation write is a loud log line, not a reason to eat an
	// answer the owner already paid real tokens for.
	h := newInsightHarness(t)
	h.store.err = errors.New("postgres down")

	recorder := h.post(t, flaggedInsightRequestBody())

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 despite the ledger failure (body: %s)", recorder.Code, recorder.Body.String())
	}
	var resp BusinessInsightResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.AdviceText == "" {
		t.Error("advice_text is empty — the owner paid for this call")
	}
}
