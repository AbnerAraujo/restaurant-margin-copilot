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

## Cross-cutting contract rules

- Every tool response that includes a number includes `source_row_refs`.
- No tool accepts a raw SQL fragment, column list, or free-form filter — all
  inputs are typed and enumerable ahead of time.
- A tool that cannot fulfill a request returns a typed error object, never a
  best-guess value.
