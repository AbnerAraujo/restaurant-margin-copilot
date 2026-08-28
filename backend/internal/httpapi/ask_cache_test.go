package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ambiguity"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/answercache"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/explain"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/instrumentation"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// --- counting doubles -------------------------------------------------
//
// Each records how many times it actually ran, which is the whole point:
// "the cache saves money" is only proven by the model layer NOT being
// invoked, never by the response looking similar.

type countingGate struct {
	calls        int
	decision     ambiguity.Decision
	lastQuestion string
	lastPending  *ambiguity.PendingClarification
}

func (g *countingGate) Classify(_ context.Context, question string, pending *ambiguity.PendingClarification) (*ambiguity.Decision, error) {
	g.calls++
	g.lastQuestion = question
	g.lastPending = pending
	decision := g.decision
	return &decision, nil
}

type countingExplainer struct {
	calls        int
	result       explain.Result
	lastQuestion string
}

func (e *countingExplainer) Explain(_ context.Context, question, _ string) (*explain.Result, error) {
	e.calls++
	e.lastQuestion = question
	result := e.result
	return &result, nil
}

type recordingInstrumentationStore struct {
	records []instrumentation.Record
}

func (s *recordingInstrumentationStore) SaveQuestionInteraction(_ context.Context, r instrumentation.Record) error {
	s.records = append(s.records, r)
	return nil
}

type memoryCacheStore struct {
	entries map[string]storage.AnswerCache
	hits    []storage.CreateAnswerCacheHitParams
}

func newMemoryCacheStore() *memoryCacheStore {
	return &memoryCacheStore{entries: map[string]storage.AnswerCache{}}
}

func (m *memoryCacheStore) GetAnswerCacheEntry(_ context.Context, key string) (storage.AnswerCache, error) {
	entry, ok := m.entries[key]
	if !ok {
		return storage.AnswerCache{}, pgx.ErrNoRows
	}
	return entry, nil
}

func (m *memoryCacheStore) UpsertAnswerCacheEntry(_ context.Context, arg storage.UpsertAnswerCacheEntryParams) (storage.AnswerCache, error) {
	entry := storage.AnswerCache{
		NormalizedQuestion: arg.NormalizedQuestion,
		OriginalQuestion:   arg.OriginalQuestion,
		Response:           arg.Response,
		OriginCostUsd:      arg.OriginCostUsd,
	}
	m.entries[arg.NormalizedQuestion] = entry
	return entry, nil
}

func (m *memoryCacheStore) DeleteAllAnswerCacheEntries(_ context.Context) error {
	m.entries = map[string]storage.AnswerCache{}
	return nil
}

func (m *memoryCacheStore) CreateAnswerCacheHit(_ context.Context, arg storage.CreateAnswerCacheHitParams) (storage.AnswerCacheHit, error) {
	m.hits = append(m.hits, arg)
	return storage.AnswerCacheHit{}, nil
}

// --- harness ----------------------------------------------------------

type askHarness struct {
	handler         http.HandlerFunc
	gate            *countingGate
	explainer       *countingExplainer
	instrumentation *recordingInstrumentationStore
	cacheStore      *memoryCacheStore
	cache           *answercache.Cache
}

func newAskHarness(t *testing.T) *askHarness {
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
		ProvenanceRefs:   []string{"fixtures/daily_reconciliation.csv:7"},
		InputTokens:      1180,
		OutputTokens:     240,
		EstimatedCostUSD: 0.00476,
		LatencyMs:        1420,
	}}
	instrumentationStore := &recordingInstrumentationStore{}
	cacheStore := newMemoryCacheStore()
	cache := answercache.New(cacheStore)

	return &askHarness{
		handler: HandleAsk(Deps{
			Gate:      gate,
			Explainer: explainer,
			Logger:    instrumentation.NewLogger(instrumentationStore),
			Cache:     cache,
		}),
		gate:            gate,
		explainer:       explainer,
		instrumentation: instrumentationStore,
		cacheStore:      cacheStore,
		cache:           cache,
	}
}

func (h *askHarness) ask(t *testing.T, question string) AskResponse {
	t.Helper()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/ask",
		strings.NewReader(`{"question":`+quote(question)+`}`))
	h.handler(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", recorder.Code, recorder.Body.String())
	}
	var response AskResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decoding response: %v (body: %s)", err, recorder.Body.String())
	}
	return response
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// --- the tests --------------------------------------------------------

func TestIdenticalQuestionAskedTwiceMakesExactlyOneSetOfModelCalls(t *testing.T) {
	h := newAskHarness(t)
	question := "How did we do on 2026-08-07?"

	first := h.ask(t, question)
	second := h.ask(t, question)

	if h.gate.calls != 1 {
		t.Errorf("ambiguity gate ran %d times, want exactly 1 — the cache must sit IN FRONT of the gate, which is itself a billed call", h.gate.calls)
	}
	if h.explainer.calls != 1 {
		t.Errorf("explanation step ran %d times, want exactly 1", h.explainer.calls)
	}

	// The answer itself is identical — a cheaper answer must not be a
	// different or degraded answer.
	if second.AnswerText != first.AnswerText {
		t.Errorf("cached answer = %q, want the original %q", second.AnswerText, first.AnswerText)
	}
	if len(second.ProvenanceRefs) != len(first.ProvenanceRefs) {
		t.Errorf("cached provenance refs = %v, want the original %v", second.ProvenanceRefs, first.ProvenanceRefs)
	}
}

func TestCacheHitReportsZeroSpendAndTheCostItAvoided(t *testing.T) {
	h := newAskHarness(t)
	question := "How did we do on 2026-08-07?"

	first := h.ask(t, question)
	second := h.ask(t, question)

	if first.Cache != nil {
		t.Error("the first, uncached answer must not claim a cache hit")
	}
	if len(first.Interactions) != 2 {
		t.Fatalf("first answer reported %d interactions, want 2 (gate + explain)", len(first.Interactions))
	}

	if second.Cache == nil || !second.Cache.Hit {
		t.Fatal("the second answer must be marked as a cache hit")
	}
	// No model call ran, so no spend may be reported: a client that sums
	// Interactions into a running total must not be charged twice for one
	// real API call.
	if len(second.Interactions) != 0 {
		t.Errorf("cache hit reported %d interactions, want 0 — nothing ran, so nothing was spent", len(second.Interactions))
	}

	var expectedAvoided float64
	for _, interaction := range first.Interactions {
		expectedAvoided += interaction.EstimatedCostUSD
	}
	if second.Cache.CostAvoidedUSD != expectedAvoided {
		t.Errorf("CostAvoidedUSD = %v, want the original spend %v", second.Cache.CostAvoidedUSD, expectedAvoided)
	}
	if second.Cache.Note == "" {
		t.Error("a cache hit must carry the disclosed matching limitation")
	}
}

func TestCacheHitIsInstrumentedAsANonInteractionNotAFabricatedModelCall(t *testing.T) {
	h := newAskHarness(t)
	question := "How did we do on 2026-08-07?"

	h.ask(t, question)
	interactionsAfterFirst := len(h.instrumentation.records)
	h.ask(t, question)

	// Constitution Principle VI: question_interaction is exactly "model calls
	// that really ran". A cache hit adds nothing to it...
	if len(h.instrumentation.records) != interactionsAfterFirst {
		t.Errorf("a cache hit wrote %d extra question_interaction row(s); it must write none — no model call happened",
			len(h.instrumentation.records)-interactionsAfterFirst)
	}
	// ...and is not invisible either: it lands in its own ledger.
	if len(h.cacheStore.hits) != 1 {
		t.Fatalf("answer_cache_hit rows = %d, want 1 — a cache hit must never be silently unrecorded", len(h.cacheStore.hits))
	}
	if h.cacheStore.hits[0].QuestionText != question {
		t.Errorf("recorded question = %q, want %q", h.cacheStore.hits[0].QuestionText, question)
	}
}

func TestWhitespaceAndCaseVariantsHitButAParaphraseDoesNot(t *testing.T) {
	h := newAskHarness(t)

	h.ask(t, "How did we do on 2026-08-07?")

	sameQuestion := h.ask(t, "  how did   WE do on 2026-08-07?  ")
	if sameQuestion.Cache == nil || !sameQuestion.Cache.Hit {
		t.Error("a whitespace/case variant of the same question must hit the cache")
	}
	if h.gate.calls != 1 {
		t.Errorf("gate ran %d times after a normalized-equal question, want 1", h.gate.calls)
	}

	// The disclosed limitation, asserted: a reworded question costs full price.
	paraphrase := h.ask(t, "How was 2026-08-07 for us?")
	if paraphrase.Cache != nil {
		t.Error("a paraphrase must NOT hit the cache — this cache makes no semantic claim")
	}
	if h.gate.calls != 2 {
		t.Errorf("gate ran %d times, want 2 — a paraphrase is a new question", h.gate.calls)
	}
}

func TestNewIngestionClearsStaleEntriesSoTheNextAskRecomputes(t *testing.T) {
	h := newAskHarness(t)
	question := "How did we do on 2026-08-07?"

	h.ask(t, question)
	h.ask(t, question)
	if h.gate.calls != 1 {
		t.Fatalf("precondition: gate ran %d times, want 1", h.gate.calls)
	}

	// What cmd/server/main.go does at the start of every -ingest /
	// -ingest-promo run: new source data invalidates every cached answer.
	if err := h.cache.Clear(context.Background()); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	afterIngestion := h.ask(t, question)
	if afterIngestion.Cache != nil {
		t.Error("an answer served after an ingestion must not come from the pre-ingestion cache")
	}
	if h.gate.calls != 2 || h.explainer.calls != 2 {
		t.Errorf("after invalidation the model ran gate=%d explain=%d, want 2 and 2 — the answer must be recomputed against the new data",
			h.gate.calls, h.explainer.calls)
	}
}

func TestRefusalsAreCachedToo(t *testing.T) {
	h := newAskHarness(t)
	h.gate.decision = ambiguity.Decision{
		Result:           instrumentation.GateUnanswerable,
		RefusalReason:    "no Uber Eats export is on file for that period",
		InputTokens:      400,
		OutputTokens:     22,
		EstimatedCostUSD: 0.00049,
		LatencyMs:        290,
	}

	question := "What was our Uber Eats ROI last week?"
	first := h.ask(t, question)
	second := h.ask(t, question)

	if first.Status != "refused" || second.Status != "refused" {
		t.Fatalf("statuses = %q and %q, want both refused", first.Status, second.Status)
	}
	if second.RefusalReason != first.RefusalReason {
		t.Errorf("cached refusal reason = %q, want %q", second.RefusalReason, first.RefusalReason)
	}
	// Asking the same unanswerable question twice must not cost twice: the
	// refusal is a deterministic consequence of the data on file, and the
	// cache is cleared the moment that data changes.
	if h.gate.calls != 1 {
		t.Errorf("gate ran %d times for a repeated refusal, want 1", h.gate.calls)
	}
	if h.explainer.calls != 0 {
		t.Errorf("explanation step ran %d times on a refusal path, want 0", h.explainer.calls)
	}
}

func TestHandlerWithoutACacheBehavesExactlyAsBefore(t *testing.T) {
	gate := &countingGate{decision: ambiguity.Decision{Result: instrumentation.GateAnswerable}}
	explainer := &countingExplainer{result: explain.Result{AnswerText: "An answer."}}
	handler := HandleAsk(Deps{
		Gate:      gate,
		Explainer: explainer,
		Logger:    instrumentation.NewLogger(&recordingInstrumentationStore{}),
		// Cache deliberately nil.
	})

	for i := 0; i < 2; i++ {
		recorder := httptest.NewRecorder()
		handler(recorder, httptest.NewRequest(http.MethodPost, "/api/ask",
			strings.NewReader(`{"question":"Same question"}`)))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", recorder.Code)
		}
	}

	if gate.calls != 2 || explainer.calls != 2 {
		t.Errorf("without a cache the model must run every time; got gate=%d explain=%d", gate.calls, explainer.calls)
	}
}
