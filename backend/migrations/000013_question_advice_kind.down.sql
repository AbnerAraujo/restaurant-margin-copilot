-- Revert specs/011-inline-grounded-advice's kind widening. Any
-- question_advice rows must be removed first or the narrowed CHECK
-- cannot be re-installed — deleting them is the honest down-migration
-- (the feature that wrote them is being removed), not data loss hidden
-- behind a failed migration.

DELETE FROM business_insight_interaction WHERE kind = 'question_advice';

ALTER TABLE business_insight_interaction
    DROP CONSTRAINT business_insight_interaction_kind_check;

ALTER TABLE business_insight_interaction
    ADD CONSTRAINT business_insight_interaction_kind_check CHECK (kind IN (
        'discrepancy_pattern',
        'negative_promo_roi',
        'high_commission',
        'day_of_month_expense_spike',
        'margin_decline'
    ));
