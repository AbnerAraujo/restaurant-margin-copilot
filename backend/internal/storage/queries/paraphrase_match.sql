-- name: CreateParaphraseMatch :one
-- One row per answer served via paraphrase recognition (specs/004-semantic-cache)
-- — a real classification call that avoided a full gate+explain cycle. Both
-- costs are recorded, never netted (spec FR-005).
INSERT INTO paraphrase_match (
    new_question,
    matched_normalized_question,
    classification_input_tokens,
    classification_output_tokens,
    classification_cost_usd,
    classification_latency_ms,
    cost_avoided_usd
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: SumParaphraseMatchClassificationCost :one
-- The real, small cost this project's paraphrase-matching mechanism has
-- itself spent so far (spec SC-003's "small fraction of what it avoids").
SELECT COALESCE(SUM(classification_cost_usd), 0)::numeric AS total FROM paraphrase_match;

-- name: SumParaphraseMatchCostAvoided :one
-- Total full-cycle spend paraphrase recognition has avoided so far.
SELECT COALESCE(SUM(cost_avoided_usd), 0)::numeric AS total FROM paraphrase_match;

-- name: CountParaphraseMatches :one
SELECT COUNT(*)::bigint AS total FROM paraphrase_match;
