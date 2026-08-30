-- specs/011-inline-grounded-advice: add 'question_advice' to the
-- business_insight_interaction kind CHECK.
--
-- Migration 000010's own comment set the bar for this change: "adding a
-- sixth kind should cost a migration plus a reviewed prompt, not a
-- free-text insert." This is that migration, and
-- backend/internal/advisor/question_advice.go is that reviewed prompt.
--
-- question_advice is the INLINE, question-initiated advice path — the
-- owner explicitly asked an advice-shaped question, the gate flagged it
-- deterministically, and one advisor call ran grounded in the tool
-- results that same answer computed. It is a ledger kind only: POST
-- /api/business-insight's five-kind teaser validation (advisor.KnownKind)
-- still rejects it, so the closed teaser set stays closed.

ALTER TABLE business_insight_interaction
    DROP CONSTRAINT business_insight_interaction_kind_check;

ALTER TABLE business_insight_interaction
    ADD CONSTRAINT business_insight_interaction_kind_check CHECK (kind IN (
        'discrepancy_pattern',
        'negative_promo_roi',
        'high_commission',
        'day_of_month_expense_spike',
        'margin_decline',
        'question_advice'
    ));
