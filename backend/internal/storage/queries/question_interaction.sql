-- name: CreateQuestionInteraction :one
-- Writer: internal/instrumentation/ only, alongside whichever of
-- internal/ambiguity/ or internal/explain/ ran (Constitution Principle VI).
INSERT INTO question_interaction (
    question_text,
    resolved_period,
    ambiguity_gate_result,
    clarification_fired,
    refusal_fired,
    answer_text,
    provenance_refs,
    model_used,
    input_tokens,
    output_tokens,
    estimated_cost_usd,
    latency_ms
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
)
RETURNING *;

-- name: ListRecentQuestionInteractions :many
-- Backs an instrumentation/history view in the frontend cost panel.
SELECT * FROM question_interaction
ORDER BY created_at DESC
LIMIT $1;

-- name: SumEstimatedCostUSD :one
-- Backs the running cost total the UI must show for 100% of interactions (FR-009).
SELECT COALESCE(SUM(estimated_cost_usd), 0)::numeric AS total_cost_usd
FROM question_interaction;
