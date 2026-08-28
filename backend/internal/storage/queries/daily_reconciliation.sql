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
