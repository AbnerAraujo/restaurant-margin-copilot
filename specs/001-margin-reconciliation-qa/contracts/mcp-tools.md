# MCP Tool Contracts

The fixed, typed tool set the model may call (Constitution Principle III —
no open SQL, no free-form computation tool). Defined in `internal/mcptools/`
via `mark3labs/mcp-go`, each wrapping a read-only query against
`internal/reconcile/`'s already-computed output.

## `get_daily_summary`

- **Input**: `{ "date": "YYYY-MM-DD" }`
- **Output**: The `DailyReconciliation` row for that date, or an explicit
  `{ "error": "no_data", "missing": [...] }` if a source is missing for that
  date — never a partial/estimated summary.
- **Timeout**: 5s. Counts against the per-interaction tool-call cap.

## `get_margin_delta`

- **Input**: `{ "period_a": {start, end}, "period_b": {start, end} }`
- **Output**: Margin delta between two periods, each side carrying its own
  `source_row_refs`. Returns `{ "error": "insufficient_data" }` if either
  period has missing days rather than computing a delta against partial data.

## `list_discrepancies`

- **Input**: `{ "date": "YYYY-MM-DD" }` or `{ "period": {start, end} }`
- **Output**: List of `discrepancy_flags` entries (duplicates caught, refunds
  netted, anomaly-threshold breaches) with provenance for each.

## `get_promotion_roi`

- **Input**: `{ "campaign_id": "..." }` or `{ "platform": "...", "period": {...} }`
- **Output**: `PromotionRoiRecord` row(s). `roi: null` with
  `{ "reason": "attribution_unavailable" }` when incremental revenue can't be
  attributed — the tool itself enforces FR-013, not just the caller.

## `list_negative_roi_promotions`

- **Input**: `{ "period": {start, end} }`
- **Output**: `PromotionRoiRecord` rows where `flagged_negative = true` —
  backs spec User Story 4 / SC-006 directly.

## `compare_platform_economics`

- **Input**: `{ "period": {start, end} }`
- **Output**: `PlatformComparisonResult` — the same period's iFood and Just
  Eat Takeaway figures side by side: `gross_sales`, `commission_paid`,
  `effective_rate` (commission ÷ gross sales), `promo_spend` (from
  `PromotionRoiRecord`), `combined_cost` (commission + promo spend), and
  `combined_effective_rate`, each carrying its own `source_row_refs`.
  `effective_rate`/`combined_effective_rate` are `null` — never a fabricated
  `"0.00%"` and never a divide-by-zero — for a platform with zero gross
  sales in the period (FR-003): the sales figure is a real zero, but a rate
  over zero sales is undefined, and the two are never conflated. Returns
  `{ "error": "insufficient_data" }` if any calendar day in the period has
  no computed reconciliation, the same policy `get_margin_delta` applies.
  Added by specs/003-platform-comparator; see that spec's FR-001–FR-007 for
  the full requirement set this tool exists to satisfy — most notably
  FR-006: a natural-language platform-comparison question MUST be answered
  by calling this tool, never by the narration model combining two separate
  `get_daily_summary`/`get_promotion_roi` calls itself.

## `get_period_totals`

- **Input**: `{ "period": {start, end} }`
- **Output**: The entire period's reconciled figures summed and ranked in
  one call — `days_included`, `gross_sales_by_source` (summed per source
  across the period), `total_delivery_gross_sales`, `commissions`,
  `refunds`, `input_costs`, `margin_total`, `avg_daily_margin`
  (`margin_total ÷ days_included`, fixed-point round-half-up, never a float
  divide), and `best_day`/`worst_day` (`{date, margin}` — the single
  highest/lowest per-day margin in the period; an exact tie is broken by
  the chronologically earliest date), each carrying `source_row_refs`.
  Returns `{ "error": "insufficient_data" }` if any calendar day in the
  period has no computed reconciliation, the same policy `get_margin_delta`
  and `compare_platform_economics` already apply — never a total or a
  best/worst-day pick computed against partial data. Added to close two
  observed gaps: a period-total question (e.g. "total supplier cost for
  the two-week period") and a "which day had the most profit and why"
  question that previously had no single tool to resolve to, and instead
  called `get_daily_summary` once per day until the per-interaction
  tool-call budget was exhausted. Call this tool directly for any
  period-total or best/worst-day question — never reconstruct one from
  repeated `get_daily_summary` calls.

## `get_expense_pattern_by_day_of_month`

- **Input**: `{ "period": {start, end} }`
- **Output**: Every reconciled day in the period grouped by its DAY-OF-MONTH
  (1st through 31st) and averaged for total expense (commissions + refunds
  + input costs) across however many months in the period actually contain
  that day-of-month — `pattern` (one `{day_of_month, avg_expense,
  occurrences}` entry per day-of-month that occurs at least once, sorted
  ascending), plus `highest_expense_day`/`lowest_expense_day` (same shape;
  an exact tie is broken by the smaller day-of-month number), each
  carrying `source_row_refs`. Returns `{ "error": "insufficient_data" }` if
  any calendar day in the period has no computed reconciliation, the same
  policy every other period-taking tool already applies. Added after a
  real live question ("is the 15th typically my worst day?" / "which day
  of the month costs the most on average?") that `get_period_totals`
  cannot answer — that tool ranks by one specific calendar date within a
  single period, not by a RECURRING position across many months — and the
  gate correctly refused to approximate the grouping itself rather than
  call `get_daily_summary` once per day and average client-side. Call this
  tool directly for any question about an expense pattern by position in
  the month — never reconstruct one from repeated single-day calls.

## Cross-cutting contract rules

- Every tool response that includes a number includes `source_row_refs`.
- No tool accepts a raw SQL fragment, column list, or free-form filter — all
  inputs are typed and enumerable ahead of time.
- A tool that cannot fulfill a request returns a typed error object, never a
  best-guess value.
