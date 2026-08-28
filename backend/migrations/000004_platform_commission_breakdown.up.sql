-- Schema addition for specs/003-platform-comparator: compare_platform_economics
-- needs per-platform commission, not just the day's single combined total.
--
-- daily_reconciliation.gross_sales_by_source already breaks gross sales down
-- per source ("ifood", "just_eat_takeaway", "pos"), but `commissions` is one
-- NUMERIC total across every delivery platform for the day — internal/reconcile
-- sums recomputeCommissionCents() across all delivery rows regardless of
-- platform before ever handing a number to this schema. There is no way to
-- recover "how much commission did iFood alone cost" from what is persisted
-- today. compare_platform_economics needs exactly that, per-platform, summed
-- over a period (FR-001: "derived from already-ingested per-order records —
-- never estimated or looked up from a hardcoded rate table"), so multiplying
-- a platform's gross_sales_by_source entry by its nominal flat rate would
-- violate that requirement outright (and would also be wrong: fixtures'
-- IFOOD-20260802-0007 refund nets a completed order's commission against its
-- own reversal, so iFood's TRUE effective rate over the 14-day fixture period
-- is 22.06%, not the nominal 23% — a fact only recoverable from real
-- per-order data, which is exactly the point of this column).
--
-- commissions_by_source is computed and persisted the same way
-- gross_sales_by_source already is (internal/reconcile.ComputeDailyReconciliations,
-- one map entry per normalized delivery-platform source key), so a period sum
-- over it is exactly as trustworthy as the existing per-day `commissions`
-- total it is derived alongside.
ALTER TABLE daily_reconciliation
    ADD COLUMN commissions_by_source JSONB NOT NULL DEFAULT '{}'::jsonb;

COMMENT ON COLUMN daily_reconciliation.commissions_by_source IS
    'Per-source (delivery-platform) breakdown of the day''s commission, '
    'summing to the same total as the commissions column — added for '
    'specs/003-platform-comparator''s compare_platform_economics tool, which '
    'needs a platform''s real per-order commission, never a flat-rate '
    'estimate (FR-001).';
