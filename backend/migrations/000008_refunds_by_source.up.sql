-- Schema addition closing A15 (docs/product-strategy.md), a real measured
-- evaluation failure: "Delivery revenue on 2026-08-02, net of the refund?"
-- could not be answered per-platform because daily_reconciliation.refunds
-- is one NUMERIC total across every delivery platform for the day —
-- internal/reconcile summed refundsCents across all refunded rows
-- regardless of platform before ever handing a number to this schema, the
-- exact same gap 000004_platform_commission_breakdown.up.sql already fixed
-- for commissions.
--
-- Verified against the real ingestion/reconciliation code before adding
-- this column (not assumed): every refunded row in
-- internal/ingest.DeliveryRecord carries its own Platform field
-- (backend/internal/ingest/types.go), so internal/reconcile.computeOneDay
-- already has the normalized source key in scope at the exact point it
-- accumulates a refund (backend/internal/reconcile/reconcile.go) — it was
-- simply summing into a single scalar instead of also keying by source, the
-- same class of gap total_delivery_gross_sales and commissions_by_source
-- were both added to fix. This is real per-order data, not an estimate:
-- fixtures/README.md's only refund in the 14-day fixture window
-- (IFOOD-20260802-0007, subtotal 34.50) is unambiguously iFood's.
--
-- refunds_by_source is computed and persisted the same way
-- commissions_by_source already is (internal/reconcile.ComputeDailyReconciliations,
-- one map entry per normalized delivery-platform source key, "pos" never
-- appearing since POS has no refunded status in this reconciliation), so a
-- period sum over it is exactly as trustworthy as the existing per-day
-- `refunds` total it is derived alongside.
ALTER TABLE daily_reconciliation
    ADD COLUMN refunds_by_source JSONB NOT NULL DEFAULT '{}'::jsonb;

COMMENT ON COLUMN daily_reconciliation.refunds_by_source IS
    'Per-source (delivery-platform) breakdown of the day''s refunds, '
    'summing to the same total as the refunds column — added to close A15 '
    '(docs/product-strategy.md), a real measured eval gap: refunds could '
    'not previously be attributed to a specific platform even though '
    'internal/ingest.DeliveryRecord always carries the Platform of the '
    'refunded row.';
