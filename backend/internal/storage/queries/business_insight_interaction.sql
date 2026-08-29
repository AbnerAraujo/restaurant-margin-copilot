-- name: CreateBusinessInsightInteraction :one
-- One row per on-demand business-insight advice call
-- (specs/009-business-insight-advisor) — the owner tapped a
-- deterministically-derived teaser and a real Claude Sonnet 5 call ran.
-- Writer: internal/httpapi's business-insight handler only, alongside
-- internal/advisor's model call (Constitution Principle VI).
INSERT INTO business_insight_interaction (
    kind,
    grounding_tool_calls,
    advice_text,
    model_used,
    input_tokens,
    output_tokens,
    estimated_cost_usd,
    latency_ms
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: SumBusinessInsightCost :one
-- The real total this product's advice skill has spent so far — its own
-- ledger's sum, never mixed into question_interaction's running total.
SELECT COALESCE(SUM(estimated_cost_usd), 0)::numeric AS total FROM business_insight_interaction;

-- name: CountBusinessInsightInteractions :one
SELECT COUNT(*)::bigint AS total FROM business_insight_interaction;
