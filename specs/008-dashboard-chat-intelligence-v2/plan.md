# Implementation Plan: Dashboard & Chat Intelligence v2

**Spec**: [spec.md](./spec.md) · **Status**: Ready for tasks/implementation

## Technical Context

An enhancement batch over the already-shipped product (specs 001-007) — no new MCP tool, no new model call, no new persisted entity. Every requirement is a new deterministic Go derivation over data an existing tool already returns, or a frontend presentation change over data an existing endpoint already sends.

**Language/stack**: Go (backend, unchanged), React/TypeScript (frontend, unchanged).

## Constitution Check

- **Principle I (deterministic/probabilistic split)**: Every new derivation (chart-click question text, flag-based follow-up, comparison-period math, aggregate ROI, trend arrows) is plain Go or plain TypeScript over already-computed data — none touch the model layer. ✅
- **Principle III (typed tools only)**: No new MCP tool. "Compare to last period" re-uses the existing `/api/ask` path end-to-end (through the real ambiguity gate), never a bypass. ✅
- **Principle IV (provenance)**: "Show your work" (US1) surfaces provenance/tool data already in the response — it adds a UI affordance, not a new data path. ✅
- **Principle V (refuse rather than guess)**: FR-005/FR-013 make omission-on-missing-data an explicit, tested requirement for every new proactive UI element — the same discipline `Stat`'s `value: null` convention already establishes. ✅
- **Principle VI (instrument every model interaction)**: "Compare to last period" runs a real `/api/ask` request and is logged exactly like any other question — no special-cased, unlogged path. ✅

No violations requiring justification.

## Per-user-story technical approach

### User Story 1 — Proactive guidance in chat

- **Chart click-to-ask (FR-001)**: `frontend/src/components/Charts/*.tsx` chart components already carry per-bar/per-segment data with a real date or identifier (the same data driving their tooltips). Add an `onDataPointClick` (or reuse the existing hover/tooltip data attachment point) that calls a new small helper, `buildChartFollowUpQuestion(datum)` (frontend, pure function, one per chart type: period-totals, promo ROI, platform comparison), producing the same self-contained question shape `deriveFollowUpSuggestions` already produces server-side, then calls the existing `submitQuestion` path in `ChatPanel.tsx` — no new backend endpoint.
- **Flag-derived follow-up (FR-002)**: extend `backend/internal/httpapi/suggestions.go`'s `deriveFollowUpSuggestions` with a new source function, `flagBasedFollowUp(invocations)`, reading `DailyReconciliation.discrepancy_flags` from whichever tool result is already in `byTool` — added to the existing candidate list before the existing 3-cap truncation, so it competes for a slot rather than bypassing the cap.
- **Show-your-work (FR-003)**: `AskResponse` already carries enough (tool invocations are not currently serialized to the client — confirm during implementation whether `result.ToolInvocations` needs a new, explicit, opt-in field on `AskResponse`, e.g. `ToolCalls []ToolCallView`, gated so it doesn't bloat every response's payload by default if the frontend doesn't request it). Frontend: an expandable `<details>`-style affordance under `AnswerBubble` in `ChatPanel.tsx`, matching the existing `ProvenanceTag` expand pattern already in that file.

### User Story 2 — Period comparisons

- **"Compare to last period" (FR-004/FR-005)**: pure Go date math, `derivePriorPeriod(start, end time.Time) (time.Time, time.Time)` (new, small, in `httpapi` alongside `suggestions.go`) — same length, immediately preceding, calendar-aware (a month's "last period" is the prior calendar month, not a fixed 30-day shift). Frontend: a button on an answered period/daily question that calls `/api/ask` again with a self-contained comparison question built from the two periods — reuses the existing ask path completely, including the gate and instrumentation.
- **Year-over-year Home tile (FR-006)**: `internal/httpapi` (or a small new Home-data endpoint, TBD during tasks — check whether `GET /api/reconciliation` already returns enough, or whether a lightweight new aggregation is needed) computes the same-month-last-year period via `get_margin_delta`'s existing insufficient-data refusal; frontend omits the tile entirely on refusal, per FR-013.
- **Platforms effective-rate trend (FR-007)**: `PlatformsPage.tsx` already calls `compare_platform_economics`-backed data for one period; extend to fetch a small number of trailing periods (e.g. trailing 6 months, bounded, not every day) and plot via the existing `dataviz`-skill chart conventions this codebase already uses (check `frontend/src/components/Charts/` for the line-chart pattern already established, if any, or add one following the skill's mark specs).

### User Story 3 — Proactive Home/Promotions insight

- **Trend arrow (FR-008)**: `HomePage.tsx`'s existing `latest` daily figure gains a call to `get_margin_delta` (yesterday vs. today, or same-weekday-last-week vs. today — pick the comparison that's actually meaningful and defensible, decide during tasks) rendered as a small up/down/flat indicator beside the existing "Latest margin" `Stat`.
- **Biggest win/catch card (FR-009)**: a new `Panel` on `HomePage.tsx` calling `get_period_totals` for the trailing 7 reconciled days (not a fixed calendar week — "trailing 7 that exist", per FR-013's degrade-on-missing-data rule), showing `best_day`/`worst_day`.
- **Promotions "needs action" mark (FR-010)**: pure frontend derivation in `PromotionsPage.tsx` — a campaign is "needs action" iff `flagged_negative && !anyOtherCampaign.replaces_campaign_id === this.campaign_id` — no backend change, the data (`replaces_campaign_id`) is already in `GET /api/promotions`'s response.

### User Story 4 — Promotions/Points polish

- **Aggregate ROI by platform (FR-011)** and **sort by ROI (FR-012)**: pure frontend, `PromotionsPage.tsx` — `Array.reduce` over the already-fetched promotion list, grouped by `platform`, summing `roi` where `roi !== null`; a sort toggle over the existing table, with unattributable/not-yet-attributed rows given a stable sort key (e.g. `-Infinity` for "sorts last", never coerced to `0`, which would misrepresent them as a real zero-ROI campaign — this is the same `-Infinity`-not-`0` discipline `buildScale` in `PromoRoiChart.tsx` already applies with its `.filter(value => value !== null)` pattern).
- **Redemption history (FR-014)**: `GET /api/promotions` already returns `payment_method`/`points_spent` per record (shipped this week) — `PointsCard.tsx` or a new small section on the Points page filters the already-fetched (or newly-fetched, if Points doesn't currently fetch promotions) list to `payment_method === 'points'`, sorted newest first. Confirm during tasks whether the Points page already has promotion data in scope or needs its own fetch of `GET /api/promotions`.

## Testing strategy

Backend: table-driven Go tests for `flagBasedFollowUp` and `derivePriorPeriod`, mirroring `suggestions_test.go`'s existing style exactly (real tool-result samples, not mocks of internal state). Frontend: component tests per changed page (`HomePage.test.tsx`, `PromotionsPage.test.tsx`, `ChatPanel.test.tsx`, `PlatformsPage.test.tsx`) asserting both the populated-data case and the FR-013 omission case explicitly — every new proactive element needs a test proving it disappears cleanly on insufficient data, not just a test proving it appears on sufficient data.

## Open questions for implementation review (not blocking, confirm during tasks)

- Whether "show your work" needs a new `AskResponse` field or can be derived from data already present — resolve by reading the current `AskResponse`/`Result` shapes before writing FR-003's tasks.
- Whether the Home page's year-over-year tile and Platforms' trend need a new lightweight backend aggregation endpoint or can be assembled from existing endpoints called in sequence from the frontend — prefer the latter unless it would require more than 2-3 sequential calls, which should instead get a small new Go aggregation to avoid a chatty frontend.
- The exact comparison point for the Home trend arrow (yesterday vs. same-weekday-last-week) — either is defensible; pick one, document the choice in the implementing commit, don't leave it ambiguous in code.
