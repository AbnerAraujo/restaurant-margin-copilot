-- Business-insight advice ledger for POST /api/business-insight
-- (specs/009-business-insight-advisor).
--
-- Sits alongside question_interaction / answer_cache_hit / paraphrase_match
-- as a FOURTH table, not a column bolted onto any of them, for the same
-- reason those three are already split (see internal/storage/models.go's
-- doc comments): an advice call is not the ambiguity gate or explain
-- running (question_interaction's CHECK constraint requires an
-- ambiguity_gate_result no advice call ever has), it is not free
-- (answer_cache_hit's whole point is cost avoided with nothing spent), and
-- it is not a cache match (paraphrase_match records a classification that
-- avoided a full cycle — this call avoids nothing, it IS the spend). A
-- dedicated table keeps all four states distinguishable and no cost is
-- ever netted or hidden.
--
-- kind is CHECK-constrained to the five insight kinds
-- internal/httpapi/business_insight.go derives deterministically — a
-- closed set on purpose: advice is the most sensitive content category
-- this product produces, and adding a sixth kind should cost a migration
-- plus a reviewed prompt, not a free-text insert.
--
-- grounding_tool_calls is the EXACT tool-result JSON the advice call was
-- shown — the probabilistic content's provenance record (Constitution
-- Principle IV extended to advice: not "which rows computed this figure",
-- since advice is not a figure, but "which computed figures this advice
-- was grounded in", which is the honest equivalent).
--
-- estimated_cost_usd is the real, deterministic Go-computed cost of the
-- one Claude Sonnet 5 call (llmclient.EstimateCostUSD) — always > 0 in
-- practice, since reaching this table means the call actually ran.

CREATE TABLE business_insight_interaction (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind                  TEXT NOT NULL CHECK (kind IN (
                              'discrepancy_pattern',
                              'negative_promo_roi',
                              'high_commission',
                              'day_of_month_expense_spike',
                              'margin_decline'
                          )),
    grounding_tool_calls  JSONB NOT NULL,
    advice_text           TEXT NOT NULL,
    model_used            TEXT NOT NULL,
    input_tokens          INTEGER NOT NULL,
    output_tokens         INTEGER NOT NULL,
    estimated_cost_usd    NUMERIC(12, 6) NOT NULL,
    latency_ms            INTEGER NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX business_insight_interaction_created_at_idx
    ON business_insight_interaction (created_at);

COMMENT ON TABLE business_insight_interaction IS
    'One row per on-demand business-insight advice call '
    '(specs/009-business-insight-advisor) — a real, owner-initiated Claude '
    'Sonnet 5 call whose kind was first derived deterministically in Go and '
    're-verified against the posted tool results before any tokens were '
    'spent. Kept out of question_interaction (this is not the gate or '
    'explain running), answer_cache_hit (this is not free), and '
    'paraphrase_match (this avoids nothing — it IS the spend) so all four '
    'interaction states stay distinguishable and no cost is ever netted or '
    'hidden.';
