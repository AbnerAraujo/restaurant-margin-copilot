-- name: UpsertDailyReconciliation :one
-- Writer: internal/reconcile/ only. The model layer never calls this.
INSERT INTO daily_reconciliation (
    date,
    gross_sales_by_source,
    commissions,
    refunds,
    input_costs,
    margin,
    discrepancy_flags,
    source_row_refs
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
ON CONFLICT (date) DO UPDATE SET
    gross_sales_by_source = EXCLUDED.gross_sales_by_source,
    commissions           = EXCLUDED.commissions,
    refunds               = EXCLUDED.refunds,
    input_costs           = EXCLUDED.input_costs,
    margin                = EXCLUDED.margin,
    discrepancy_flags     = EXCLUDED.discrepancy_flags,
    source_row_refs       = EXCLUDED.source_row_refs,
    updated_at            = now()
RETURNING *;

-- name: GetDailyReconciliationByDate :one
-- Backs the get_daily_summary MCP tool contract.
SELECT * FROM daily_reconciliation
WHERE date = $1;

-- name: ListDailyReconciliationsInPeriod :many
-- Backs get_margin_delta / list_discrepancies, which operate over a range.
SELECT * FROM daily_reconciliation
WHERE date >= $1 AND date <= $2
ORDER BY date;

-- name: GetDataDateRange :one
-- Backs the date-grounding fix (docs/plan.md mistakes log: "date-year
-- grounding defect"): the actual inclusive min/max date this product has
-- ANY reconciled data for, resolved once at process start
-- (cmd/server/main.go) and handed into internal/ambiguity's gate and
-- internal/explain's system prompt as plain strings, so relative date
-- language ("today", "this week", a year-less date) resolves against the
-- real data's own range instead of the host machine's wall-clock date or
-- a hardcoded literal that could drift from the fixtures actually loaded.
SELECT MIN(date)::date AS min_date, MAX(date)::date AS max_date FROM daily_reconciliation;
