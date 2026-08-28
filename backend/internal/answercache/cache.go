// Package answercache is an exact-match cache in front of POST /api/ask's
// two model calls. A repeated question is served from Postgres without
// invoking either the Claude Haiku 4.5 ambiguity gate or the Claude Sonnet 5
// explanation step — the whole point being that the tokens are not spent at
// all, not that they are spent more cheaply.
//
// # What it does NOT do, and why that WAS a disclosed limitation
//
// The key is the question text, normalized only by trimming, collapsing
// internal whitespace, and lowercasing. "How did we do on 2026-08-07?" and
// "how was 2026-08-07" are DIFFERENT keys and the second one costs full
// price on this exact-match lookup alone. That was a real limitation,
// stated plainly rather than papered over: matching paraphrases needs
// either a model call or an embedding lookup, both of which would put a
// probabilistic step (and, for the model, a cost) in front of what is
// otherwise a deterministic O(1) index probe.
//
// specs/004-semantic-cache narrows — but deliberately does not remove —
// this limitation: internal/paraphrase sits IN FRONT of this exact-match
// cache (checked only on a miss here, never replacing this lookup) and asks
// a bounded Claude Haiku 4.5 call whether a new question means the same
// thing as one of the most-recently-cached ones. This package's own
// exact-match behavior is unchanged by that addition (spec FR-006) — Lookup
// still normalizes the same way, still misses on a paraphrase by itself,
// and still costs nothing when it hits. Candidates and RecordParaphraseMatch
// below exist only to support that other package's I/O — reading the
// bounded candidate set and writing its own ledger row — never to make this
// package's own Lookup fuzzy. A cache that sometimes decides two
// differently-worded questions "mean the same thing" can also decide that
// wrongly, which is exactly why that decision is made by an inspectable,
// instrumented classification call (internal/paraphrase), re-verified
// against this package's real, current contents before anything is served.
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
	// ListRecentDistinctCachedQuestions and CreateParaphraseMatch back
	// specs/004-semantic-cache's paraphrase-aware layer (Candidates,
	// RecordParaphraseMatch below) — added to this same Store port rather
	// than a second one, since both still read/write tables this package
	// alone owns (answer_cache, paraphrase_match).
	ListRecentDistinctCachedQuestions(ctx context.Context, limit int32) ([]storage.ListRecentDistinctCachedQuestionsRow, error)
	CreateParaphraseMatch(ctx context.Context, arg storage.CreateParaphraseMatchParams) (storage.ParaphraseMatch, error)
}

// CurrentSchemaVersion is the version of the httpapi.AskResponse JSON shape
// this build writes into answer_cache.response and expects to read back.
//
// Bump this whenever AskResponse's shape changes in a way that would make an
// old cached blob mean something different once unmarshalled by the new
// code — a new field the frontend now depends on being present (the
// Visualization/Cache/MatchKind additions this project has already made are
// exactly the kind of change that requires a bump), a renamed/removed field,
// or a changed meaning for an existing field. A cosmetic change that leaves
// every existing field's meaning intact does not need one.
//
// migration 000007 added the schema_version column with no default: every
// row written before that migration has NULL there. Lookup treats NULL and
// any version other than this constant identically — a cache miss, not a
// hit — so a stale-shaped response is never served with false confidence
// (the same "invalidate rather than risk serving a stale shape" discipline
// frontend/src/lib/chatStorage.ts's THREADS_VERSION already applies to
// browser-persisted chat threads).
const CurrentSchemaVersion int32 = 1

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

	// A NULL schema_version (a row written before migration 000007) or any
	// version other than CurrentSchemaVersion (a row written by a different
	// build of AskResponse) is treated exactly like a miss, never served —
	// see CurrentSchemaVersion's doc comment. This is a miss, not an error:
	// the caller falls through to a fresh answer exactly as it would for any
	// other cache miss.
	if !row.SchemaVersion.Valid || row.SchemaVersion.Int32 != CurrentSchemaVersion {
		return nil, nil
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
		SchemaVersion:      pgtype.Int4{Int32: CurrentSchemaVersion, Valid: true},
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

// Candidate is one existing cache entry offered to internal/paraphrase's
// classifier as something a new question might mean the same thing as.
// OriginalQuestion is what the classifier reads (natural, as-typed text);
// NormalizedQuestion is what a claimed match is verified against — the same
// key Lookup itself would derive, so "the model's answer exists in the
// cache" means exactly "collides with a real Normalize() key", not some
// looser textual resemblance.
type Candidate struct {
	NormalizedQuestion string
	OriginalQuestion   string
}

// Candidates returns up to limit of the most-recently-cached questions,
// most-recent-first — the bounded set specs/004-semantic-cache's plan.md
// ("Candidate-set cap") checks a new question against instead of every
// question ever cached. An empty cache (or limit <= 0) returns an empty
// slice, never an error: "nothing to compare against" is the expected,
// common case (a fresh cache, or right after an ingestion clears it), and
// the caller (internal/httpapi) uses an empty result to skip the
// classification call entirely rather than paying for a no-op comparison.
func (c *Cache) Candidates(ctx context.Context, limit int) ([]Candidate, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := c.store.ListRecentDistinctCachedQuestions(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("answercache: candidates: %w", err)
	}
	out := make([]Candidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, Candidate{
			NormalizedQuestion: row.NormalizedQuestion,
			OriginalQuestion:   row.OriginalQuestion,
		})
	}
	return out, nil
}

// ParaphraseMatchParams is what RecordParaphraseMatch persists — the real
// classification cost incurred and the full-cycle cost it avoided, kept as
// two distinct fields all the way to the database (spec FR-005: "never
// netted together into a single number that overstates the saving").
type ParaphraseMatchParams struct {
	// NewQuestion is the question actually asked (as typed), matched to an
	// existing entry rather than answered fresh.
	NewQuestion string
	// MatchedNormalizedQuestion identifies which existing cache entry was
	// served — the Candidate.NormalizedQuestion the classifier's claim
	// verified against.
	MatchedNormalizedQuestion  string
	ClassificationInputTokens  int64
	ClassificationOutputTokens int64
	// ClassificationCostUSD is the REAL money this classification call cost
	// — never zero in practice, since a row is only ever written after a
	// real Haiku call ran and returned a verified match.
	ClassificationCostUSD   float64
	ClassificationLatencyMs int64
	// CostAvoidedUSD is the matched entry's own OriginCostUSD — what the
	// full gate+explain cycle that originally produced it cost.
	CostAvoidedUSD float64
}

// RecordParaphraseMatch writes the non-interaction, non-free ledger row for
// a served paraphrase hit (migrations/000005's paraphrase_match table) — the
// third state FR-004 requires be distinguishable from both a fresh model
// call and a zero-cost exact-text hit.
func (c *Cache) RecordParaphraseMatch(ctx context.Context, p ParaphraseMatchParams) error {
	_, err := c.store.CreateParaphraseMatch(ctx, storage.CreateParaphraseMatchParams{
		NewQuestion:                strings.TrimSpace(p.NewQuestion),
		MatchedNormalizedQuestion:  p.MatchedNormalizedQuestion,
		ClassificationInputTokens:  int32(p.ClassificationInputTokens),
		ClassificationOutputTokens: int32(p.ClassificationOutputTokens),
		ClassificationCostUsd:      floatToNumeric(p.ClassificationCostUSD),
		ClassificationLatencyMs:    int32(p.ClassificationLatencyMs),
		CostAvoidedUsd:             floatToNumeric(p.CostAvoidedUSD),
	})
	if err != nil {
		return fmt.Errorf("answercache: record paraphrase match: %w", err)
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
