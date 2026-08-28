package answercache

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// fakeStore is an in-memory stand-in for the answer-cache tables. It exists
// so the cache's own semantics (key normalization, miss-is-not-an-error,
// clear-on-ingest, hit accounting) can be asserted without Postgres — the
// live round trip is covered separately by the integration check in
// the live end-to-end check run against the real server.
type fakeStore struct {
	entries map[string]storage.AnswerCache
	hits    []storage.CreateAnswerCacheHitParams
	cleared int
}

func newFakeStore() *fakeStore {
	return &fakeStore{entries: map[string]storage.AnswerCache{}}
}

func (f *fakeStore) GetAnswerCacheEntry(_ context.Context, key string) (storage.AnswerCache, error) {
	entry, ok := f.entries[key]
	if !ok {
		return storage.AnswerCache{}, pgx.ErrNoRows
	}
	return entry, nil
}

func (f *fakeStore) UpsertAnswerCacheEntry(_ context.Context, arg storage.UpsertAnswerCacheEntryParams) (storage.AnswerCache, error) {
	entry := storage.AnswerCache{
		NormalizedQuestion: arg.NormalizedQuestion,
		OriginalQuestion:   arg.OriginalQuestion,
		Response:           arg.Response,
		OriginCostUsd:      arg.OriginCostUsd,
	}
	f.entries[arg.NormalizedQuestion] = entry
	return entry, nil
}

func (f *fakeStore) DeleteAllAnswerCacheEntries(_ context.Context) error {
	f.cleared++
	f.entries = map[string]storage.AnswerCache{}
	return nil
}

func (f *fakeStore) CreateAnswerCacheHit(_ context.Context, arg storage.CreateAnswerCacheHitParams) (storage.AnswerCacheHit, error) {
	f.hits = append(f.hits, arg)
	return storage.AnswerCacheHit{}, nil
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		name    string
		a, b    string
		sameKey bool
	}{
		{name: "identical text is the same key", a: "How did we do?", b: "How did we do?", sameKey: true},
		{name: "surrounding whitespace is ignored", a: "  How did we do?  ", b: "How did we do?", sameKey: true},
		{name: "case is ignored", a: "HOW DID WE DO?", b: "how did we do?", sameKey: true},
		{name: "internal whitespace runs collapse", a: "How   did\twe\ndo?", b: "How did we do?", sameKey: true},
		{
			// The disclosed limitation, asserted rather than merely described:
			// a paraphrase is a different key and will cost full price.
			name: "a paraphrase is deliberately a DIFFERENT key",
			a:    "How did we do on 2026-08-07?",
			b:    "How was 2026-08-07?",
		},
		{
			name: "punctuation still matters — no semantic matching is claimed",
			a:    "How did we do",
			b:    "How did we do?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			same := Normalize(tt.a) == Normalize(tt.b)
			if same != tt.sameKey {
				t.Errorf("Normalize(%q)==Normalize(%q) was %v, want %v", tt.a, tt.b, same, tt.sameKey)
			}
		})
	}
}

func TestLookupMissIsNotAnError(t *testing.T) {
	cache := New(newFakeStore())

	entry, err := cache.Lookup(context.Background(), "never asked before")
	if err != nil {
		t.Fatalf("a miss must not be an error, got %v", err)
	}
	if entry != nil {
		t.Fatalf("expected no entry, got %+v", entry)
	}
}

func TestSaveThenLookupRoundTripsResponseAndCost(t *testing.T) {
	store := newFakeStore()
	cache := New(store)
	ctx := context.Background()

	body := json.RawMessage(`{"status":"answered","answer_text":"Margin was $375.82."}`)
	if err := cache.Save(ctx, "  How Did We Do On 2026-08-07?  ", body, 0.005271); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// A different casing/spacing of the same question must hit.
	entry, err := cache.Lookup(ctx, "how did we   do on 2026-08-07?")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if entry == nil {
		t.Fatal("expected a cache hit for the same question in different casing")
	}
	if string(entry.ResponseJSON) != string(body) {
		t.Errorf("ResponseJSON = %s, want %s", entry.ResponseJSON, body)
	}
	// Cost round-trips through NUMERIC(12,6) at the same precision
	// question_interaction.estimated_cost_usd uses.
	if entry.OriginCostUSD != 0.005271 {
		t.Errorf("OriginCostUSD = %v, want 0.005271", entry.OriginCostUSD)
	}
}

func TestRecordHitWritesItsOwnLedgerRowWithTheCostAvoided(t *testing.T) {
	store := newFakeStore()
	cache := New(store)

	if err := cache.RecordHit(context.Background(), " How did we do? ", 0.005271); err != nil {
		t.Fatalf("RecordHit: %v", err)
	}

	if len(store.hits) != 1 {
		t.Fatalf("len(hits) = %d, want 1", len(store.hits))
	}
	hit := store.hits[0]
	if hit.NormalizedQuestion != "how did we do?" {
		t.Errorf("NormalizedQuestion = %q, want the normalized key", hit.NormalizedQuestion)
	}
	if hit.QuestionText != "How did we do?" {
		t.Errorf("QuestionText = %q, want the question as asked", hit.QuestionText)
	}
	if !hit.EstimatedCostAvoidedUsd.Valid {
		t.Error("EstimatedCostAvoidedUsd must be recorded, not left null")
	}
}

func TestClearDropsEverySoNewDataCannotServeAStaleAnswer(t *testing.T) {
	store := newFakeStore()
	cache := New(store)
	ctx := context.Background()

	for _, question := range []string{"q one", "q two", "q three"} {
		if err := cache.Save(ctx, question, json.RawMessage(`{"status":"answered"}`), 0.001); err != nil {
			t.Fatalf("Save(%q): %v", question, err)
		}
	}
	if len(store.entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3 before clearing", len(store.entries))
	}

	if err := cache.Clear(ctx); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	if len(store.entries) != 0 {
		t.Errorf("len(entries) = %d after Clear, want 0", len(store.entries))
	}
	for _, question := range []string{"q one", "q two", "q three"} {
		entry, err := cache.Lookup(ctx, question)
		if err != nil {
			t.Fatalf("Lookup(%q) after Clear: %v", question, err)
		}
		if entry != nil {
			t.Errorf("Lookup(%q) still hit after Clear — a stale answer could outlive its data", question)
		}
	}
}
