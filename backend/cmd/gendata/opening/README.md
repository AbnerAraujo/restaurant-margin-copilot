# The hand-authored opening window (2024-08-01 through 2024-08-14)

The first 14 days of the product's single continuous dataset
(`backend/data/live/`, 2024-08-01 through today) are **hand-authored**, at
the same realistic dollar scale as the synthetically generated days that
follow them (~$34,000/month gross at the growth curve's starting point —
`cmd/gendata` picks up from 2024-08-15 and emits these files verbatim at the
top of its output). Everything in this directory is checked into git and
never regenerated; `cmd/gendata` embeds it via `go:embed`.

Why hand-authored: the evaluation harness (`evaluation/golden/`,
`evaluation/promptfoo/`) needs known-correct answers computed independently
of the Go reconciliation engine it grades — an accuracy test whose expected
values were back-computed from the system's own output could never catch a
reconciliation bug, only a narration bug. Every reference value below was
chosen and summed by hand, then cross-checked with a throwaway script,
BEFORE being compared against `internal/reconcile`'s output. The same
methodology the original 14-day evaluation dataset used from this project's
first commit — now located at the opening of the one continuous timeline
instead of in a separate dataset.

All amounts USD. Platform names ("iFood", "Just Eat Takeaway") follow
`data-model.md`'s `PromotionRoiRecord` entity.

## Files

- `delivery_platform_export.csv` — 107 rows, both platforms, one row per
  settlement line, ISO `YYYY-MM-DD` dates. `commission_amount`/`net_payout`
  are pre-computed at flat rates (iFood 23%, Just Eat Takeaway 20%) — the
  reconciliation engine still recomputes and cross-checks them from
  `subtotal` and `commission_rate_pct`.
- `pos_export.csv` — 84 rows, in-house POS (dine-in/takeaway/phone), dates
  in `DD/MM/YYYY`, deliberately inconsistent with the delivery export's ISO
  format (see irregularity #4).
- `supplier_cost_sheet.csv` — 14 invoices from 4 suppliers, ISO dates,
  irregular real-world cadence — supplier billing is not daily, so days
  without an invoice contribute zero input cost (point allocation,
  `internal/reconcile`'s documented policy).
- `promotion_ad_spend_export.csv` — 4 campaigns across both platforms.
  Attribution is a deterministic tag join over `campaign_id`, never a
  pre-baked number in this file.

## Deliberate irregularities (Constitution Principle V)

Each is a single, isolated fact — deliberately not layered with other
anomalies — so a test can target it precisely.

1. **Duplicate order** — `IFOOD-20240803-0011` (2024-08-03, 12:05, subtotal
   24.00) appears twice, byte-for-byte identical (a platform export/webhook-
   retry glitch). Reconciliation must count it once.

2. **Refund crossing into the following week** — `IFOOD-20240802-0006` was
   placed 2024-08-02 (subtotal 62.25, completed) and reversed by a second
   row with the same order_id, negative amounts (subtotal -62.25, commission
   -14.32, net_payout -47.93), `status=refunded`, `refund_date=2024-08-09`.
   Net delivery revenue for 2024-08-02 is 446.25 - 62.25 = **384.00**.

3. **Missing day** — `delivery_platform_export.csv` has **zero** rows for
   `2024-08-10` (a Saturday — normally the busiest day). `pos_export.csv`
   (the window's single largest POS day, 1,204.50) and
   `supplier_cost_sheet.csv` (invoice `INV-3009`) both have entries for that
   date, so the gap is loud, not quiet. The system must state plainly that
   the delivery-platform source is missing for that day — never report $0.

4. **Inconsistent date format between two files** — delivery export ISO
   `YYYY-MM-DD` throughout; POS export `DD/MM/YYYY` throughout (a systematic
   difference between the two export systems, not a one-off typo). The cost
   sheet and promotion export use ISO.

5. **Promotion with incomplete attribution** — campaign `IFOOD-CAMP-WEEKEND`
   (iFood, 2024-08-09 to 2024-08-10, spend 260.00) has **no** orders tagged
   with its campaign_id — partly because 2024-08-10 has no delivery data at
   all (irregularity #3). Incremental revenue is unattributable; per FR-013
   the system MUST return `roi: null` / refuse, never estimate.

## Independently-computed reference values

Computed by hand and cross-checked with a throwaway script, independent of
all Go code in this repo. `evaluation/golden/` quotes these directly.

### Promotion ROI (deduplicated, completed, campaign-tagged delivery orders)

| campaign_id | platform | spend | attributed incremental revenue | net | verdict |
|---|---|---|---|---|---|
| `IFOOD-CAMP-BOOST01` | iFood | 380.00 | 442.75 (9 orders: 42.00 + 55.25 + 48.50 + 63.50 + 24.00 [dedup'd] + 59.00 + 43.00 + 52.00 + 55.50) | +62.75 | positive — do not flag |
| `JET-CAMP-LUNCHFIX` | Just Eat Takeaway | 610.00 | 159.25 (4 orders: 42.25 + 36.25 + 34.50 + 46.25) | -450.75 | **negative — flag** |
| `IFOOD-CAMP-WEEKEND` | iFood | 260.00 | unattributable (0 tagged orders) | n/a | **refuse — FR-013** |
| `JET-CAMP-NEWMENU` | Just Eat Takeaway | 120.00 | 153.75 (3 orders: 58.00 + 45.75 + 50.00) | +33.75 | positive — do not flag |

### Daily gross delivery subtotal (`status = completed` rows only, the
2024-08-03 duplicate counted once; refunded rows are a separate line — see
the netting note below)

| date | iFood | Just Eat Takeaway | delivery total | notes |
|---|---:|---:|---:|---|
| 2024-08-01 | 196.50 | 178.25 | 374.75 | |
| 2024-08-02 | 231.75 | 214.50 | 446.25 | pre-netting gross; includes order 0006's original 62.25 |
| 2024-08-03 | 262.00 | 239.50 | 501.50 | duplicate of order 0011 counted once |
| 2024-08-04 | 205.25 | 189.75 | 395.00 | |
| 2024-08-05 | 148.50 | 141.00 | 289.50 | |
| 2024-08-06 | 172.25 | 165.50 | 337.75 | |
| 2024-08-07 | 184.75 | 176.25 | 361.00 | |
| 2024-08-08 | 191.50 | 183.75 | 375.25 | |
| 2024-08-09 | 238.25 | 221.50 | 459.75 | |
| 2024-08-10 | — | — | **missing** | no delivery-platform data this day |
| 2024-08-11 | 209.50 | 197.25 | 406.75 | |
| 2024-08-12 | 152.75 | 146.50 | 299.25 | |
| 2024-08-13 | 176.00 | 169.25 | 345.25 | |
| 2024-08-14 | 187.25 | 180.50 | 367.75 | |

**On netting the 2024-08-02 refund**: the refund row carries
`order_date=2024-08-02` (the original order) and `refund_date=2024-08-09`
(settlement). `internal/reconcile` nets it at the original order date, per
`docs/technical-rfc.md`'s accrual-netting decision: 446.25 - 62.25 =
**384.00** net delivery revenue for 2024-08-02.

### POS gross by day (`DD/MM/YYYY` as stored)

Total across all 14 days: **10,667.25**. 2024-08-10 (232.50 + 318.25 +
124.75 + 156.00 + 198.50 + 174.50 = **1,204.50**) is the window's single
largest POS day — deliberately, so the missing delivery data on the same
date is a meaningful gap, not a quiet one.

### Supplier cost sheet total

14 invoices, **5,002.75** total, across Fresh Fields Produce Co. (produce),
Coastal Meat & Poultry (protein), Blue Wave Beverage Distributors
(beverage), and PackRight Disposables (packaging). No invoice lands on
2024-08-08 — a normal gap between deliveries, not a data-quality issue.
