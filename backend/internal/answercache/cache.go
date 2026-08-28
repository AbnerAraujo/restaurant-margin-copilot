// Package answercache is an exact-match cache in front of POST /api/ask's
// two model calls. A repeated question is served from Postgres without
// invoking either the Claude Haiku 4.5 ambiguity gate or the Claude Sonnet 5
// explanation step — the whole point being that the tokens are not spent at
// all, not that they are spent more cheaply.
//
// # What it does NOT do, and why that is a disclosed limitation
//
// The key is the question text, normalized only by trimming, collapsing
// internal whitespace, and lowercasing. "How did we do on 2026-08-07?" and
// "how was 2026-08-07" are DIFFERENT keys and the second one costs full
// price. That is a real limitation, stated plainly rather than papered over:
// matching paraphrases needs either a model call or an embedding lookup,
// both of which would put a probabilistic step (and, for the model, a cost)
// in front of what is otherwise a deterministic O(1) index probe. A cache
// that sometimes decides two differently-worded questions "mean the same
// thing" can also decide that wrongly, and serving the answer to a question
// nobody asked is a worse failure than paying for the second call.
//
// # Instrumentation (Constitution Principle VI)
//
// A cache hit is the ABSENCE of a model interaction. It is therefore never
// written to question_interaction — doing so would fabricate an API call
// that did not happen and inflate SumEstimatedCostUSD with money nobody
// spent. It is also never left unrecorded, which would make real product
// activity invisible. It gets its own ledger (answer_cache_hit), carrying
// the cost the hit AVOIDED — a saving, reported as a saving, never summed
// into spend.
//
// # Invalidation
//
// Every ingestion run clears the whole cache before it writes (see
// cmd/server/main.go). New source data can change any cached answer and
// there is no cheap way to know which; correctness after new data beats
// retention, so the cache is dropped wholesale rather than reasoned about.
package answercache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// Store is the narrow slice of storage.Querier this package needs. Declared
// here rather than taking the whole Querier so a test can substitute a fake
// without implementing a dozen unrelated reconciliation queries.
type Store interface {
	GetAnswerCacheEntry(ctx context.Context, normalizedQuestion string) (storage.AnswerCache, error)
	UpsertAnswerCacheEntry(ctx context.Context, arg storage.UpsertAnswerCacheEntryParams) (storage.AnswerCache, error)
	DeleteAllAnswerCacheEntries(ctx context.Context) error
	CreateAnswerCacheHit(ctx context.Context, arg storage.CreateAnswerCacheHitParams) (storage.AnswerCacheHit, error)
}

// Cache reads and writes cached answers.
type Cache struct {
	store Store
}

// New builds a Cache over any Store — storage.Queries satisfies it directly.
func New(store Store) *Cache {
	return &Cache{store: store}
}

// Entry is one cached answer, decoded.
type Entry struct {
	// ResponseJSON is the full response body previously served, verbatim.
	// This package deliberately does not know its shape: the response type
	// lives in internal/httpapi, and importing it here would make the cache
	// depend on the handler it sits in front of.
	ResponseJSON json.RawMessage
	// OriginCostUSD is what the model calls that produced this entry cost.
	// Serving from cache avoids exactly this much.
	OriginCostUSD float64
	CachedAt      string
}

const timestampLayout = "2006-01-02T15:04:05Z07:00"

// Normalize derives the cache key from raw question text: trim, collapse all
// internal whitespace runs to a single space, lowercase. Exported because
// the key rule is part of this package's disclosed contract, and a test
// asserting which questions collide with which has to be able to state it.
func Normalize(question string) string {
	return strings.ToLower(strings.Join(strings.Fields(question), " "))
}

// Lookup returns the cached entry for question, or (nil, nil) on a miss.
// A miss is not an error — it is the expected outcome for most questions —
// so callers branch on the nil entry, never on an error value.
func (c *Cache) Lookup(ctx context.Context, question string) (*Entry, error) {
	row, err := c.store.GetAnswerCacheEntry(ctx, Normalize(question))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("answercache: lookup: %w", err)
	}

	cost, err := numericToFloat(row.OriginCostUsd)
	if err != nil {
		return nil, fmt.Errorf("answercache: lookup: origin_cost_usd: %w", err)
	}

	cachedAt := ""
	if row.CreatedAt.Valid {
		cachedAt = row.CreatedAt.Time.UTC().Format(timestampLayout)
	}

	return &Entry{
		ResponseJSON:  json.RawMessage(row.Response),
		OriginCostUSD: cost,
		CachedAt:      cachedAt,
	}, nil
}

// Save stores a freshly-computed response under question's normalized key.
func (c *Cache) Save(ctx context.Context, question string, responseJSON json.RawMessage, originCostUSD float64) error {
	_, err := c.store.UpsertAnswerCacheEntry(ctx, storage.UpsertAnswerCacheEntryParams{
		NormalizedQuestion: Normalize(question),
		OriginalQuestion:   strings.TrimSpace(question),
		Response:           responseJSON,
		OriginCostUsd:      floatToNumeric(originCostUSD),
	})
	if err != nil {
		return fmt.Errorf("answercache: save: %w", err)
	}
	return nil
}

// RecordHit writes the non-interaction ledger row for a served cache hit.
func (c *Cache) RecordHit(ctx context.Context, question string, costAvoidedUSD float64) error {
	_, err := c.store.CreateAnswerCacheHit(ctx, storage.CreateAnswerCacheHitParams{
		NormalizedQuestion:      Normalize(question),
		QuestionText:            strings.TrimSpace(question),
		EstimatedCostAvoidedUsd: floatToNumeric(costAvoidedUSD),
	})
	if err != nil {
		return fmt.Errorf("answercache: record hit: %w", err)
	}
	return nil
}

// Clear drops every cached answer. Called at the start of an ingestion run.
func (c *Cache) Clear(ctx context.Context) error {
	if err := c.store.DeleteAllAnswerCacheEntries(ctx); err != nil {
		return fmt.Errorf("answercache: clear: %w", err)
	}
	return nil
}

// floatToNumeric renders a USD cost into the NUMERIC(12,6) the schema uses.
// Six decimal places is the same precision question_interaction.estimated_cost_usd
// carries, so a cost and the saving it represents round identically.
func floatToNumeric(usd float64) pgtype.Numeric {
	micros := int64(usd*1e6 + 0.5)
	if usd < 0 {
		micros = int64(usd*1e6 - 0.5)
	}
	return pgtype.Numeric{Int: big.NewInt(micros), Exp: -6, Valid: true}
}

func numericToFloat(n pgtype.Numeric) (float64, error) {
	if !n.Valid || n.NaN || n.Int == nil {
		return 0, errors.New("unexpected null or NaN numeric")
	}
	value := new(big.Float).SetInt(n.Int)
	scale := new(big.Float).SetFloat64(1)
	for i := 0; i < -int(n.Exp); i++ {
		scale.Mul(scale, big.NewFloat(10))
	}
	for i := 0; i < int(n.Exp); i++ {
		value.Mul(value, big.NewFloat(10))
	}
	if n.Exp < 0 {
		value.Quo(value, scale)
	}
	out, _ := value.Float64()
	return out, nil
}
