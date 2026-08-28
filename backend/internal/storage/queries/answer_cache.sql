-- name: GetAnswerCacheEntry :one
-- Exact lookup on the normalized question key. Writer/reader:
-- internal/answercache only.
SELECT * FROM answer_cache
WHERE normalized_question = $1;

-- name: UpsertAnswerCacheEntry :one
-- Re-answering the same normalized question overwrites the entry rather than
-- keeping the older one: the newer response was computed against whatever
-- data is current, so it is the one worth serving next time.
INSERT INTO answer_cache (
    normalized_question,
    original_question,
    response,
    origin_cost_usd
) VALUES ($1, $2, $3, $4)
ON CONFLICT (normalized_question) DO UPDATE SET
    original_question = EXCLUDED.original_question,
    response          = EXCLUDED.response,
    origin_cost_usd   = EXCLUDED.origin_cost_usd,
    created_at        = now()
RETURNING *;

-- name: DeleteAllAnswerCacheEntries :exec
-- Invalidation. Called at the START of every ingestion run: new source data
-- can change any cached answer, and there is no cheap way to know which, so
-- the whole cache is dropped. Correctness after new data beats cache
-- retention, always.
DELETE FROM answer_cache;

-- name: CreateAnswerCacheHit :one
-- One row per answer served from cache — a non-interaction, recorded in its
-- own ledger rather than as a fabricated question_interaction.
INSERT INTO answer_cache_hit (
    normalized_question,
    question_text,
    estimated_cost_avoided_usd
) VALUES ($1, $2, $3)
RETURNING *;

-- name: SumAnswerCacheCostAvoided :one
-- Total model spend the cache has avoided so far.
SELECT COALESCE(SUM(estimated_cost_avoided_usd), 0)::numeric AS total FROM answer_cache_hit;

-- name: CountAnswerCacheHits :one
SELECT COUNT(*)::bigint AS total FROM answer_cache_hit;

-- name: ListRecentDistinctCachedQuestions :many
-- The bounded candidate set specs/004-semantic-cache's paraphrase classifier
-- checks a new question against: the most-recently-cached questions first
-- (LIMIT applies the plan's cap — 20 at the call site — before the
-- classification prompt is built, per plan.md's "Candidate-set cap" section).
-- Already distinct by construction: normalized_question is answer_cache's
-- primary key, so this can never return two rows for the same question.
SELECT normalized_question, original_question FROM answer_cache
ORDER BY created_at DESC
LIMIT $1;
