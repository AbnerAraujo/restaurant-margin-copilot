# Implementation Plan: Cross-Platform Economics Comparator

**Spec**: [spec.md](./spec.md) · **Status**: Ready for tasks/implementation

## Technical Context

A new read-only computation over already-ingested data — no new ingestion, no new source data, no schema migration required. The two platforms already exist as distinct `gross_sales_by_source` keys (`ifood`, `just_eat_takeaway`) with genuinely different real commission rates in the ingested data (23% vs 20% flat, per `backend/cmd/gendata/opening/README.md`).

## Constitution Check

- **Principle I**: Pure Go computation from persisted rows — no model involvement in computing the comparison itself. ✅
- **Principle III (typed tools only)**: New tool `compare_platform_economics` added to the fixed MCP tool set, following the exact pattern of the existing five tools (`contracts/mcp-tools.md` gains a sixth entry). ✅
- **Principle IV (provenance)**: Every figure in the comparison carries the same `source_row_refs` provenance convention as every other tool result. ✅

No violations requiring justification.

## MCP tool contract addition

`compare_platform_economics(period: {start, end})` → `PlatformComparisonResult`:

```
{
  period: {start, end},
  platforms: [
    { source: "ifood", gross_sales, commission_paid, effective_rate, promo_spend, combined_cost, combined_effective_rate, source_row_refs },
    { source: "just_eat_takeaway", ... same shape ... }
  ]
}
```

- `effective_rate = commission_paid / gross_sales` (zero-safe: a platform with zero gross sales in the period reports `effective_rate: null`, not a divide-by-zero or a fabricated 0%, per FR-003's "real zero, never omitted" requirement — the *sales* are a real zero; a *rate* over zero sales is undefined, and the two must not be conflated).
- `promo_spend` sourced from existing `PromotionRoiRecord` rows filtered by platform, summed the same way `list_negative_roi_promotions` already aggregates.
- `combined_cost = commission_paid + promo_spend`; `combined_effective_rate = combined_cost / gross_sales` (same zero-safety rule).

## Chart-type integration

Extends `backend/internal/httpapi/visualization.go`'s existing tool-name-to-chart-shape mapping: `compare_platform_economics` → a 2-series bar chart (one bar pair per platform: commission-only vs. combined-with-promo), reusing `CategoryBarChart` on the frontend — no new chart component needed, this tool's result shape fits the existing bar-chart contract.

## Frontend changes

- New `/platforms` route (or a section on the existing Promotions page — implementation's call, spec does not mandate placement) rendering the comparison via the existing `CategoryBarChart`/`DataGrid` components.
- No new chart component required (per above) — this is primarily a new backend tool plus a new thin page wiring existing chart components to it.

## Testing strategy

Table-driven Go tests against the existing hand-authored data, with expected values computed independently (matching this project's existing double-verification discipline — hand-compute from the raw CSVs, do not derive the test's expected values from the implementation being tested). Explicit test case for the zero-activity-platform edge case (FR-003) confirming `effective_rate: null`, not `0`.
