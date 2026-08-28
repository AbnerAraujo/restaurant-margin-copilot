# Implementation Plan: Badge System Expansion

**Spec**: [spec.md](./spec.md) · **Status**: Ready for tasks/implementation

## Technical Context

Extends `backend/internal/badges` (Reconciliation category already built) with three new categories, each keeping the existing package's core design philosophy where the data allows: badges are computed at read time from real data, never persisted as their own mutable state.

**Language/stack**: Go (backend, unchanged), React/TypeScript (frontend, unchanged) — no new runtime dependency for Growth or Campaign-Creation. Engagement requires one new Postgres table.

## Constitution Check

- **Principle I (deterministic/probabilistic split)**: All three badge categories are pure Go computation over persisted data — no model involvement, same as the existing two categories. ✅
- **Principle III (typed tools only)**: The new promotion-creation flow (User Story 3) is a typed, validated write path (a new `sqlc` query + a new REST handler), not open SQL. ✅
- **Principle V (test-first build order)**: Table-driven tests for each badge category's trigger logic before wiring the API, matching the existing `badges_test.go` pattern. ✅
- **Principle VI (instrument every model interaction)**: Unaffected — none of these three categories involve a model call. ✅

No violations requiring justification.

## Data model additions

- **`usage_event`** (new table): `id`, `occurred_at timestamptz`. No tenant/account column yet (single-tenant prototype, per spec 002's Assumptions) — this table is exactly the shape that gains a `tenant_id` column if/when `specs/005-multi-tenant` is ever approved and implemented; designed with that in mind but not built for it yet.
- **`promotion_roi_record`** (existing table, extended): gains `origin` (`'ingested'` | `'owner_created'`, default `'ingested'` for backward compatibility with all existing rows) and `replaces_campaign_id` (nullable FK-by-value to an existing `campaign_id`, since campaigns are identified by string code, not a surrogate key, per the existing schema).
- **Badge codes**: `internal/badges.Code` gains `CodeGrowth`, `CodeEngagement`, `CodeCampaignCreation` alongside the existing `CodeCleanClose`/`CodeDiscrepancyCatcher`.
- **Point values**: `frontend/src/components/Points/pointValues.ts`'s `POINTS_PER_BADGE` (and the backend equivalent in `internal/badges`) gains real, documented weights for the three new codes — proposed starting values: Growth 15 (between the existing 10/25 — a positive outcome recognized, but less than catching a real problem), Engagement 5 per day-milestone (small, frequent, since it rewards showing up rather than a specific save), Campaign-Creation 30 (the highest of any badge — it is the one category that required the owner to actually act on an insight, the exact behavior `docs/product-strategy.md` says this whole badge system exists to encourage). Final values are auditable on-screen per the existing discipline, not hidden constants — confirm/adjust during implementation review, not fixed unilaterally here.

## MCP tool / API contract additions

- `GET /api/badges`'s response gains entries for the three new codes, distinguishable by `category` (FR-009) — extend the existing response shape, do not introduce a parallel endpoint.
- New `POST /api/promotions` (owner-created promotion): body includes campaign identifier, platform, date range, spend, and an optional `replaces` field. Server-side validation enforces FR-007 (refuses a `replaces` reference to a campaign not already flagged negative-ROI by the existing `list_negative_roi_promotions` computation) before insert.

## Request flow: Campaign-Creation badge (the one net-new write path)

1. Owner views a flagged negative-ROI promotion (existing `list_negative_roi_promotions` surface).
2. Owner submits a new promotion via `POST /api/promotions`, referencing the flagged campaign as `replaces`.
3. Handler re-runs `list_negative_roi_promotions`-equivalent logic to verify the referenced campaign is actually currently flagged (not trusting a stale client-side view) — refuses if not, per FR-007.
4. On success, the new `promotion_roi_record` row is inserted with `origin='owner_created'`.
5. `internal/badges` computes a Campaign-Creation badge for any `owner_created` promotion with a non-null `replaces_campaign_id`, at read time — no separate "badge awarded" event to track.

## Frontend changes

- `frontend/src/components/Home/HomePage.tsx`'s `roadmapCategory` labels for Ask (`'Engagement points'`) and Promotions (`'Campaign points'`) tiles update to real earned-points display once these categories are live (FR-011) — this is a copy/logic change in an existing component, not a new one.
- New minimal "Log a replacement campaign" form on the Promotions page, reachable from a flagged negative-ROI row.
- A real usage-event ping on app load (e.g. a lightweight `POST /api/usage` fired once per session/day, not per page navigation, to avoid inflating the "distinct days used" count with normal in-app navigation).

## Testing strategy

Table-driven Go tests per badge category (mirroring `backend/internal/badges/badges_test.go`'s existing style) using synthetic promotion/usage-event fixtures with known expected badge outputs, plus the FR-007 refusal path tested explicitly (attempting to reference a non-flagged campaign as `replaces`). Frontend component tests for the new form and the updated Home tiles, matching the existing test density in `frontend/src/components/Points/*.test.tsx`.

## Open questions for implementation review (not blocking, but worth confirming)

- Exact point values (proposed above) — a product-taste call, not a technical one; confirm or adjust when reviewing the implementation.
- Whether "Week One" is the only Engagement milestone, or whether further milestones (e.g. "Month One") are worth adding in the same pass — spec 002 only requires the one, more can be added later without a new spec.
