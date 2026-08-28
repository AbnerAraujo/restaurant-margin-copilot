# Fixture Data

Two weeks (2026-08-01 through 2026-08-14) of synthetic daily data for a
single independent restaurant/bar, covering the four `FixtureDataSource`
shapes in `data-model.md`. All amounts are in USD. Restaurant/platform names
are fictional; "iFood" and "Just Eat Takeaway" are used as the two delivery
platforms per the example in `data-model.md`'s `PromotionRoiRecord` entity.

Every number below was computed independently of any code in this repo (by
hand, then cross-checked with a throwaway script) so it can serve as the
first source of golden values for `internal/ingest`/`internal/reconcile`
tests (T011/T012) and, later, `evaluation/golden/`.

## Files

- `delivery_platform_export.csv` — 54 rows, both platforms combined, one row
  per settlement line. Dates are ISO `YYYY-MM-DD`. Columns: `platform,
  order_id, order_date, order_time, subtotal, commission_rate_pct,
  commission_amount, net_payout, status, refund_date, campaign_id, notes`.
  `commission_amount`/`net_payout` are pre-computed in the fixture (iFood
  23%, Just Eat Takeaway 20%, flat rates) — the reconciliation engine should
  still be able to recompute and cross-check them from `subtotal` and
  `commission_rate_pct`.
- `pos_export.csv` — 56 rows, in-house POS (dine-in/takeaway/phone — not
  delivery-platform orders). Dates are `DD/MM/YYYY`, deliberately
  inconsistent with the delivery export's `YYYY-MM-DD` (see below).
- `supplier_cost_sheet.csv` — 12 invoices from 4 suppliers, ISO dates,
  irregular cadence (produce ~every 3 days, protein weekly, beverage weekly,
  packaging ~every 4-5 days) — real supplier billing is not daily, so the
  reconciliation engine needs an explicit allocation policy for days without
  an invoice.
- `promotion_ad_spend_export.csv` — 4 campaigns across both platforms.
  `attributed_incremental_orders`/`attributed_incremental_revenue` are
  **not** in this file on purpose: per `spec.md`'s Assumptions, attribution
  is a deterministic tag join, not a pre-baked number. A campaign's
  incremental revenue must be computed by summing the `subtotal` of
  `delivery_platform_export.csv` rows whose `campaign_id` matches (after
  deduplication — see the BOOST01 case below), restricted to `status =
  completed`.

## Deliberate irregularities (Constitution Principle V)

Each is a single, isolated fact — deliberately not layered with other
anomalies — so a test can target it precisely.

1. **Duplicate order** — `delivery_platform_export.csv`, order_id
   `IFOOD-20260803-0011` (2026-08-03, 12:05, subtotal 24.00) appears twice,
   byte-for-byte identical. Represents a platform export/webhook-retry
   glitch. Reconciliation must count it once.

2. **Refund from a prior period** — `delivery_platform_export.csv`, order_id
   `IFOOD-20260802-0007` was placed 2026-08-02 (subtotal 34.50, completed)
   and reversed by a second row with the same order_id, negative amounts
   (subtotal -34.50, commission -7.94, net_payout -26.56), `status=refunded`,
   `refund_date=2026-08-09` — one week later, crossing into the following
   week. Net effect on 2026-08-02 revenue for this order should be zero.

3. **Missing day** — `delivery_platform_export.csv` has **zero** rows for
   `2026-08-08` (a Saturday — normally the busiest day). `pos_export.csv`
   and `supplier_cost_sheet.csv` both have entries for that date (POS rows
   `POS-1028`–`POS-1031`; invoice `INV-3007`). The system must state plainly
   that the delivery-platform source is missing for that day, not silently
   omit it from the margin figure (spec Acceptance Scenario US1.3).

4. **Inconsistent date format between two files** — `delivery_platform_export.csv`
   uses ISO `YYYY-MM-DD` throughout; `pos_export.csv` uses `DD/MM/YYYY`
   throughout (not a one-off typo — a systematic difference between the two
   export systems). `supplier_cost_sheet.csv` and
   `promotion_ad_spend_export.csv` both use ISO, matching the delivery
   export.

5. **Promotion with incomplete attribution** — `promotion_ad_spend_export.csv`
   campaign `IFOOD-CAMP-WEEKEND` (iFood, Featured Placement, 2026-08-08 to
   2026-08-09, spend 95.00) has **no** orders in
   `delivery_platform_export.csv` tagged with `campaign_id =
   IFOOD-CAMP-WEEKEND` — partly because 2026-08-08 itself has no delivery
   data at all (irregularity #3), and none were tagged on 2026-08-09 either.
   Incremental revenue is therefore unattributable from available data; per
   FR-013 the system MUST return `roi: null` / refuse to assert an ROI
   figure, not estimate one. The `notes` column states the (fictional) real
   -world reason: iFood's dashboard export omitted the attribution
   breakdown for this campaign.

## Independently-computed reference values

For hand-verification against `internal/reconcile`'s output.

### Promotion ROI (campaign_id -> attributed incremental revenue, from
deduplicated, completed, campaign-tagged delivery orders)

| campaign_id | platform | spend | attributed incremental revenue | net | ROI verdict |
|---|---|---|---|---|---|
| `IFOOD-CAMP-BOOST01` | iFood | 180.00 | 214.00 (6 orders: 42.00 + 38.00 + 24.00 [dedup'd] + 29.00 + 36.00 + 45.00) | +34.00 | positive — do not flag |
| `JET-CAMP-LUNCHFIX` | Just Eat Takeaway | 220.00 | 55.00 (2 orders: 22.00 + 33.00) | -165.00 | **negative — flag** |
| `IFOOD-CAMP-WEEKEND` | iFood | 95.00 | unattributable (0 tagged orders) | n/a | **refuse — FR-013** |
| `JET-CAMP-NEWMENU` | Just Eat Takeaway | 60.00 | 79.50 (3 orders: 26.00 + 24.50 + 29.00) | +19.50 | positive — do not flag |

### Daily gross delivery subtotal (`status = completed` rows only, exact
duplicate of order 0011 counted once; `status = refunded` rows excluded from
this table — see the note below on netting)

| date | iFood | Just Eat Takeaway | delivery total | notes |
|---|---:|---:|---:|---|
| 2026-08-01 | 69.50 | 76.25 | 145.75 | |
| 2026-08-02 | 72.50 | 81.75 | 154.25 | includes order 0007's original 34.50 — its refund is a separate row, excluded here (see below) |
| 2026-08-03 | 55.25 | 65.25 | 120.50 | duplicate of order 0011 counted once |
| 2026-08-04 | 62.50 | 63.00 | 125.50 | |
| 2026-08-05 | 64.50 | 71.25 | 135.75 | |
| 2026-08-06 | 58.25 | 72.50 | 130.75 | |
| 2026-08-07 | 74.25 | 65.50 | 139.75 | |
| 2026-08-08 | — | — | **missing** | no delivery-platform data this day |
| 2026-08-09 | 61.00 | 65.25 | 126.25 | |
| 2026-08-10 | 62.50 | 74.00 | 136.50 | |
| 2026-08-11 | 62.50 | 68.00 | 130.50 | |
| 2026-08-12 | 66.50 | 62.50 | 129.00 | |
| 2026-08-13 | 62.00 | 69.00 | 131.00 | |
| 2026-08-14 | 66.75 | 73.75 | 140.50 | |

**On netting the 2026-08-02 refund**: the refund row for order
`IFOOD-20260802-0007` (subtotal -34.50, commission -7.94, net_payout -26.56)
carries `order_date=2026-08-02` (the original order) and
`refund_date=2026-08-09` (when it settled). Whether `internal/reconcile`
nets it against the 2026-08-02 total, the 2026-08-09 total, or both (gross
then adjustment) is a design decision for that package, not prescribed by
this fixture — the table above is deliberately the *pre-netting* gross figure
so it stays true regardless of which convention is chosen. Gross minus this
one refund's subtotal is 154.25 - 34.50 = 119.75 for 2026-08-02, if netted
against the original order date.

### POS gross by day (DD/MM/YYYY as stored; ISO shown for cross-reference)

Total across all 14 days: **3472.75**. 2026-08-08 (POS: 145.00 + 132.50 +
168.00 + 42.00 = 487.50) is the year's single largest POS day in this
fixture set — deliberately, so the missing delivery data on the same date is
a meaningful gap, not a quiet one.

### Supplier cost sheet total

12 invoices, **4335.75** total, across Fresh Fields Produce Co. (produce),
Coastal Meat & Poultry (protein), Blue Wave Beverage Distributors (beverage),
and PackRight Disposables (packaging). No invoice lands on 2026-08-07 or
2026-08-10 — a normal gap between deliveries, not a data-quality issue.
