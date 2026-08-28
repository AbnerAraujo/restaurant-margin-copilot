-- Schema for the three core entities in data-model.md:
-- DailyReconciliation, PromotionRoiRecord, QuestionInteraction.
--
-- Every table that carries a computed number also carries source_row_refs /
-- provenance_refs (Constitution Principle IV) and is written only by the
-- deterministic reconcile/instrumentation layers, never by the model layer
-- (Constitution Principle I).

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- One row per restaurant per day. Deterministic output of internal/reconcile/.
CREATE TABLE daily_reconciliation (
    date                    DATE PRIMARY KEY,
    gross_sales_by_source   JSONB NOT NULL DEFAULT '{}'::jsonb,
    commissions             NUMERIC(12, 2) NOT NULL,
    refunds                 NUMERIC(12, 2) NOT NULL,
    input_costs             NUMERIC(12, 2) NOT NULL,
    margin                  NUMERIC(12, 2) NOT NULL,
    discrepancy_flags       JSONB NOT NULL DEFAULT '[]'::jsonb,
    source_row_refs         JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE daily_reconciliation IS
    'Deterministic daily margin computation. margin MUST be reproducible by '
    're-running reconciliation against source_row_refs; never written by the '
    'model layer (Constitution Principle I).';

-- One row per promotion/campaign per period. Deterministic output of
-- internal/reconcile/.
CREATE TABLE promotion_roi_record (
    id                              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    platform                        TEXT NOT NULL,
    campaign_id                     TEXT NOT NULL,
    period                          DATERANGE NOT NULL,
    spend                           NUMERIC(12, 2) NOT NULL,
    attributed_incremental_orders   INTEGER,
    attributed_incremental_revenue  NUMERIC(12, 2),
    roi                             NUMERIC(12, 4),
    flagged_negative                BOOLEAN NOT NULL DEFAULT false,
    source_row_refs                 JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at                      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                      TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- FR-013 / data-model.md: attribution missing => roi MUST be null, never
    -- estimated. The tool layer also enforces this; the DB is a second gate.
    CONSTRAINT roi_requires_attribution CHECK (
        attributed_incremental_revenue IS NOT NULL OR roi IS NULL
    ),
    -- flagged_negative is only meaningful once roi is known.
    CONSTRAINT flagged_negative_requires_roi CHECK (
        NOT flagged_negative OR roi IS NOT NULL
    )
);

CREATE UNIQUE INDEX promotion_roi_record_platform_campaign_period_idx
    ON promotion_roi_record (platform, campaign_id, period);

CREATE INDEX promotion_roi_record_flagged_negative_idx
    ON promotion_roi_record (flagged_negative)
    WHERE flagged_negative;

COMMENT ON TABLE promotion_roi_record IS
    'roi is NULL when incremental revenue cannot be attributed (FR-013) — '
    'never estimated. Enforced by roi_requires_attribution.';

-- One row per user question, written by internal/instrumentation/.
CREATE TABLE question_interaction (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    question_text           TEXT NOT NULL,
    resolved_period         DATERANGE,
    ambiguity_gate_result   TEXT NOT NULL
        CHECK (ambiguity_gate_result IN ('answerable', 'ambiguous', 'unanswerable')),
    clarification_fired     BOOLEAN NOT NULL DEFAULT false,
    refusal_fired           BOOLEAN NOT NULL DEFAULT false,
    answer_text             TEXT,
    provenance_refs         JSONB NOT NULL DEFAULT '[]'::jsonb,
    model_used              TEXT NOT NULL,
    input_tokens            INTEGER NOT NULL,
    output_tokens           INTEGER NOT NULL,
    estimated_cost_usd      NUMERIC(12, 6) NOT NULL,
    latency_ms              INTEGER NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- data-model.md: a refusal never carries a fabricated citation or answer.
    CONSTRAINT refusal_has_no_answer_or_provenance CHECK (
        NOT refusal_fired OR (answer_text IS NULL AND provenance_refs = '[]'::jsonb)
    )
);

CREATE INDEX question_interaction_created_at_idx
    ON question_interaction (created_at);

COMMENT ON TABLE question_interaction IS
    'Per-interaction instrumentation (Constitution Principle VI): tokens, '
    'cost, latency, and whether refusal/clarification fired, from the first '
    'API call — refusal_has_no_answer_or_provenance enforces Principle II.';
