---

description: "Task list for Dashboard & Chat Intelligence v2"
---

# Tasks: Dashboard & Chat Intelligence v2

**Input**: Design documents from `specs/008-dashboard-chat-intelligence-v2/` (spec.md, plan.md)

**Tests**: plan.md's own "Testing strategy" section explicitly requests tests for every new derivation and every FR-013 omission path — test tasks are included throughout, not optional here.

**Organization**: All four user stories in this spec are Priority P1 (spec.md names no P2/P3 within this batch — the 19 deferred ideas are out of scope entirely, tracked only in spec.md's Assumptions). Stories are ordered as in spec.md and are independently implementable and testable; no story depends on another's code.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: US1 / US2 / US3 / US4, per spec.md
- File paths are exact, from a codebase recon pass done immediately before writing this file — where a detail depends on a choice made during implementation (confirmed with an inline note), the task says so explicitly rather than presenting a guess as fact.

---

## Phase 1: Setup

- [ ] T001 Confirm a clean baseline on `feature/008-dashboard-chat-intelligence-v2` before starting any story: `cd backend && go build ./... && go test ./...`, `cd frontend && npx tsc -b --noEmit && npm test -- --run`. All four stories below assume this passes first.

---

## Phase 2: Foundational

No cross-story blocking prerequisite exists — each user story below touches a distinct set of files (US1: `suggestions.go` + `ask.go` + chat/chart components; US2: a new comparison-period file + Home/Platforms; US3: Home/Promotions derivations only; US4: Promotions/Points only). Proceed directly to User Story 1.

---

## Phase 3: User Story 1 - Proactive guidance in the chat, in the moment (Priority: P1) 🎯 MVP

**Goal**: Chart clicks submit real follow-up questions, discrepancy-flagged answers offer a "why is this different?" chip, and any answer can reveal the exact tool call(s) and raw data behind it.

**Independent Test**: Per spec.md — click a chart's worst-day bar and confirm a real follow-up question is submitted; ask about a day with a real discrepancy flag and confirm the chip appears (and does NOT appear when no flag exists); expand "show your work" on any answered question and confirm the real tool name + raw JSON render.

### Tests for User Story 1

- [x] T002 [P] [US1] Add `TestDeriveFollowUpSuggestionsIncludesFlagBasedFollowUpWhenDiscrepancyFlagsPresent` and `TestDeriveFollowUpSuggestionsOmitsFlagBasedFollowUpWhenNoFlags` to `backend/internal/httpapi/suggestions_test.go`, using real `get_daily_summary`-shaped JSON fixtures (this file's existing style — no mocks), asserting the flag-based candidate competes for one of the existing 3 slots rather than being added on top of the cap.
- [x] T003 [P] [US1] Add a test (new or in an existing `ask_*_test.go` sibling) proving `AskResponse.ToolCalls` is populated with the real tool name(s) and raw JSON already present in `result.ToolInvocations` for an answered question, and is empty/omitted (`omitempty`) when no tool ran (e.g. a cache hit or an unanswerable refusal before any tool call). **Implementation note**: new sibling `backend/internal/httpapi/ask_tool_calls_test.go`.

### Implementation for User Story 1

- [x] T004 [US1] In `backend/internal/httpapi/suggestions.go`, add a narrow parse struct (e.g. `discrepancyFlagsJSON`) matching the existing `platformComparisonPeriodJSON` pattern (~line 241), to pull `discrepancy_flags` out of the raw `get_daily_summary`/`get_period_totals`/`list_discrepancies` JSON already sitting in `byTool` — the package's existing `dailySummaryJSON` type (`visualization.go:554`) has no such field, so this cannot reuse it as-is.
- [x] T005 [US1] Add `flagBasedFollowUp(invocations []explain.ToolInvocation) []string` to `backend/internal/httpapi/suggestions.go`, producing a "why is this different from usual?"-style question for each grounding day that carries at least one real discrepancy flag.
- [x] T006 [US1] Restructure `deriveFollowUpSuggestions`'s single-source `switch` (`suggestions.go:82-95`) so the flag-based candidates from T005 are appended to whichever source's `raw` list the switch already selected, BEFORE `finalizeSuggestions`'s existing 3-cap truncation (`suggestions.go:403`) runs. This is a real structural change — today only one case's `raw` ever populates the list at all, so a flag-based source cannot simply be "one more case." **Choice made**: the flag-based candidate is PREPENDED (not appended) to `raw`, so it reliably wins one of the 3 slots even when the primary tool's own template already fills all 3 — an anomaly worth surfacing is treated as higher priority than one more generic date-range follow-up.
- [x] T007 [US1] Add a `ToolCallView{Name string; ResultJSON json.RawMessage}` type and a `ToolCalls []ToolCallView` field (`json:"tool_calls,omitempty"`) to `AskResponse` in `backend/internal/httpapi/ask.go` (struct at ~lines 134-171), populated from `result.ToolInvocations` (`internal/explain/explain.go:58-61`, already `{Name, ResultJSON string}`) at the existing response-construction site (`ask.go:501-508`). `omitempty` alone satisfies "gated, no default payload bloat" — no new request flag needed.
- [x] T008 [P] [US1] Update `docs/openapi.yaml` (and its embedded copy in `docs/api.html`, same patch technique used for the 2026-08-29 doc-sync — see git history) to document the new `tool_calls` field on the `POST /api/ask` response schema.
- [x] T009 [P] [US1] Add an `onDataPointClick` handler prop to `frontend/src/components/Charts/MarginTrendChart.tsx`'s bar `<rect>` elements — no click handler exists on this chart today, this is net new, not an extension of an existing hook.
- [x] T010 [P] [US1] Add the same `onDataPointClick` handler prop to `frontend/src/components/Charts/PromoRoiChart.tsx`'s bar `<rect>` elements (also net new).
- [x] T011 [US1] Add `buildChartFollowUpQuestion(datum)` — one small pure function per chart type (period-totals, promo ROI) — to a new `frontend/src/components/Charts/chartFollowUpQuestion.ts`, producing the same self-contained question shape `deriveFollowUpSuggestions` already produces server-side (e.g. "What happened on 2028-08-05?").
- [x] T012 [US1] Wire the new chart click handlers (T009/T010) — from wherever each chart is actually rendered (`ClosePage.tsx` for `MarginTrendChart`, `PromotionsPage.tsx` for `PromoRoiChart`) — to call `ChatPanel.tsx`'s existing `submitQuestion` (`ChatPanel.tsx:1013`). Confirm during implementation whether a shared context/hook already exposes `submitQuestion` across pages, or whether `ChatPanel` needs a small new exported hook to make this callable from outside the chat surface itself. **Choice made**: no shared context exists across routes at all — `/close`, `/promotions`, and `/ask` are fully separate routes. A chart click now `navigate()`s to `/ask` carrying the built question as router state (`AskPageNavigationState`), and a new `ChatPanel` prop, `autoSubmitQuestion`, submits it exactly once on mount. This is a real, better answer than the task's own "exported hook" framing — no hook can call into a component that isn't mounted.
- [x] T013 [US1] Add a "show your work" expandable affordance under `AnswerBubble` in `ChatPanel.tsx`, modeled directly on `frontend/src/components/Provenance/ProvenanceTag.tsx`'s existing pattern (a `useState`-driven toggle button with `aria-expanded`/`aria-controls` opening an absolutely-positioned panel — a button+panel pattern, not an HTML `<details>` element), rendering `response.tool_calls`'s tool name(s) and formatted JSON.
- [x] T014 [P] [US1] Add/extend `frontend/src/components/Chat/ChatPanel.test.tsx`: the flag-based follow-up chip renders when a flag is present and is absent when it is not; chart click-to-ask submits the exact expected question text; "show your work" expands to show the real tool name + JSON and is collapsed by default.
- [x] T015 [P] [US1] Add/extend `frontend/src/components/Charts/MarginTrendChart.test.tsx` and `PromoRoiChart.test.tsx` for the new click handlers (correct datum passed on click, no handler firing on non-data chart chrome).

**Checkpoint**: User Story 1 is fully functional and independently testable/demoable.

---

## Phase 4: User Story 2 - Comparisons finally worth having (Priority: P1)

**Goal**: A one-tap "compare to last period" action on any answered period question, a year-over-year tile on Home (shown only when real data supports it), and a real effective-rate trend on Platforms.

**Independent Test**: Per spec.md — tap "Compare to last period" on a period answer and confirm a real, correctly-derived comparison question is asked through the normal `/api/ask` path; load Home and confirm the YoY tile appears only with a full prior-year period, omitted otherwise; load Platforms and confirm the trend chart plots only real, non-refused periods.

### Tests for User Story 2

- [x] T016 [P] [US2] Add `backend/internal/httpapi/comparison_period_test.go` with table-driven tests for `derivePriorPeriod`: a calendar month (prior CALENDAR month, not a fixed 30-day shift), a full year, an arbitrary custom range, and a period whose derived prior period falls fully or partially outside a given data min/max (so the caller can detect and refuse it per FR-005).

### Implementation for User Story 2

- [x] T017 [US2] Add `derivePriorPeriod(start, end time.Time) (time.Time, time.Time)` to a new `backend/internal/httpapi/comparison_period.go` — calendar-aware per plan.md (a month's "last period" is the prior calendar month, a year's is the prior calendar year, not a fixed-day shift in either case).
- [x] T018 [US2] Confirm which existing `AskResponse` field already carries an answered period/daily-summary question's real resolved start/end dates (likely inside `Visualization` or `ProvenanceRefs` — read the current struct before assuming); if none does today, add the minimal field needed so the frontend can build a comparison question from real resolved dates rather than re-parsing the original question text (spec.md's edge case: comparisons must derive from the answered question's actual resolved dates, never from raw text).
- [x] T019 [US2] Add a "Compare to last period" button in `ChatPanel.tsx` on an answered period/daily-summary message. On click, derive the prior period (client-side date math mirroring T017's calendar-aware logic, using the resolved dates from T018) and submit a self-contained comparison question through the existing `submitQuestion`/`/api/ask` path — no new endpoint, no gate bypass (FR-004).
- [x] T020 [US2] Verify FR-005 behavior end-to-end with a definite acceptance check (not a conditional one): when the derived prior period falls outside the real data date range, submit the resulting comparison question through the EXISTING ambiguity gate and assert the response is a refusal or clarification whose text plainly names the range being out of bounds (e.g. mentions the available date range) — never a silent partial comparison and never a generic refusal that doesn't explain why. If the gate's current out-of-range wording does not already read clearly for a comparison-flavored question, that is itself a finding to fix here, not a case to skip.
- [x] T021 [US2] Add a year-over-year derivation to `frontend/src/components/Home/HomePage.tsx`, computed client-side from the `days` array `GET /api/reconciliation` already returns (`DaySummaryApi{date, margin, discrepancy_flags}`) — no new backend endpoint, since neither `get_margin_delta` nor `get_period_totals` has a REST wrapper today and adding one for a single tile is not justified per plan.md's own "prefer assembling from existing endpoints" guidance. Render the tile ONLY when a full prior-year period of equal length exists in `days`; omit entirely otherwise (FR-006/FR-013).
- [x] T022 [US2] Add `backend/internal/httpapi/platforms_trend.go` — a new small, fully deterministic (no model call) handler wrapping `mcptools.ComparePlatformEconomics` across a bounded trailing window (e.g. the trailing 6 calendar months), returning an array of per-period results and SKIPPING (never fabricating or zero-padding) any period the tool refuses with `insufficient_data`. Wire it as `GET /api/platforms/trend` in `backend/cmd/server/main.go`'s route list, alongside the existing `/api/platforms` registration.
- [x] T023 [US2] Add a new line-chart component, `frontend/src/components/Charts/EffectiveRateTrendChart.tsx` — net new; no line-chart pattern exists in this codebase to extend (`MarginTrendChart.tsx` and `PromoRoiChart.tsx` are both bar charts). Follow this project's already-established dataviz-skill conventions: one hue, thin marks, a hover tooltip, selective direct labeling, `overflow-x: auto` if the trend window can grow.
- [x] T024 [US2] Wire `EffectiveRateTrendChart` into `frontend/src/components/Platforms/PlatformsPage.tsx`, fetching `GET /api/platforms/trend` (T022).
- [x] T025 [P] [US2] Add tests: `backend/internal/httpapi/platforms_trend_test.go` (real fixture-backed; asserts `insufficient_data` periods are skipped, never zero-padded); `frontend/src/components/Home/HomePage.test.tsx` (YoY tile present/absent cases); `frontend/src/components/Platforms/PlatformsPage.test.tsx` (trend chart present/absent cases); extend `frontend/src/components/Chat/ChatPanel.test.tsx` ("Compare to last period" button, including the out-of-range case from T020).

**Checkpoint**: User Story 2 is fully functional and independently testable/demoable, without requiring US1.

---

## Phase 5: User Story 3 - Steward-style proactive insight, without being asked (Priority: P1)

**Goal**: Home volunteers a real trend arrow and a "biggest win/catch this week" card; Promotions marks a negative-ROI campaign that's never been replaced as needing action — all without the owner asking.

**Independent Test**: Per spec.md — load Home with ≥2 reconciled days and confirm a real directional indicator and a scoped win/catch card; load Promotions with an un-replaced negative-ROI campaign and confirm it's visually marked.

### Implementation for User Story 3

- [x] T026 [US3] Add `deriveMarginTrend(days: DaySummaryApi[])` to a new `frontend/src/components/Home/homeInsights.ts`, comparing the latest day's margin against yesterday's (the simpler, defensible choice from plan.md's two options; document this choice inline in the commit — do not leave it ambiguous in code, per plan.md's own open question). **Choice made**: "yesterday" = the immediately preceding entry in the `days` array, not calendar-yesterday — `days` can have real gaps (this project's fixture has a known missing day), so comparing against a fixed calendar offset would silently produce no result across a gap even though a perfectly good prior data point exists one slot back.
- [x] T027 [P] [US3] Add `deriveBiggestWinCatch(days: DaySummaryApi[])` to the same `homeInsights.ts`, scoping to however many of the trailing 7 reconciled days actually exist (never padding a missing day with a placeholder), returning the real best/worst day by margin.
- [x] T028 [US3] Render a trend-arrow indicator beside `HomePage.tsx`'s existing "Latest margin" `Stat` (T026), and a new "Biggest win / biggest catch this week" `Panel` (T027) — both MUST render nothing at all (FR-013) when fewer than 2 reconciled days exist, never a zero or placeholder.
- [x] T029 [P] [US3] Add a `needsAction` derivation in `frontend/src/components/Promotions/PromotionsPage.tsx`: a campaign needs action iff `campaign.flagged_negative && !promotions.some(p => p.replaces_campaign_id === campaign.id)` — pure frontend, using `PromotionApi` fields already fetched today (`flagged_negative`, `replaces_campaign_id`); no backend change.
- [x] T030 [US3] Add a visual "needs action" mark (badge/indicator) to the campaign row in `PromotionsPage.tsx` wherever T029's derivation is true. **Implementation note**: rendered as a standalone Panel above the chart (not a per-row mark inside `PromoRoiChart.tsx`'s table), since that chart component is User Story 1's territory in this same implementation pass — kept the diff conflict-free by not touching a file another story owns.
- [x] T031 [P] [US3] Add `frontend/src/components/Home/homeInsights.test.ts`: the trend arrow's direction on real data, its omission with fewer than 2 days; the biggest-win/catch card correctly scoping to 1-6 available days, and its omission with zero days.
- [x] T032 [P] [US3] Extend `frontend/src/components/Promotions/PromotionsPage.test.tsx`: a `flagged_negative` campaign with no replacement is marked; one already referenced by a `replaces_campaign_id` is not marked.

**Checkpoint**: User Story 3 is fully functional and independently testable/demoable, without requiring US1 or US2.

---

## Phase 6: User Story 4 - Everyday usability polish on Promotions and Points (Priority: P1)

**Goal**: An aggregate ROI-by-platform stat and a sort-by-ROI control on Promotions; a real, honest redemption history on Points.

**Independent Test**: Per spec.md — load Promotions with campaigns on both platforms and confirm the aggregate matches a manual sum, and sorting reorders correctly with unattributable campaigns consistently at one end; load Points after a points-paid promotion exists and confirm it appears with the correct campaign, date, and points amount.

### Implementation for User Story 4

- [x] T033 [P] [US4] Update the `PromotionApi` TypeScript interface (in `frontend/src/components/Promotions/PromotionsPage.tsx`) to include `payment_method` and `points_spent` — `GET /api/promotions` already returns both on the wire (`internal/mcptools/promo_tools.go`'s `PromotionRoiView`), the frontend type just hasn't caught up yet.
- [x] T034 [US4] Add an aggregate-ROI-by-platform derivation to `PromotionsPage.tsx`: group the already-fetched campaigns by `platform`, sum `roi` where `roi !== null`, explicitly EXCLUDING (never zero-substituting) unattributable/not-yet-attributed campaigns from the sum (FR-011); render as a small per-platform summary stat.
- [x] T035 [US4] Add a sort-by-ROI toggle (ascending/descending) to `PromotionsPage.tsx`'s campaign list. **Implemented differently than this task originally suggested**: rather than a single comparator with a `-Infinity` sort key (which puts nulls at OPPOSITE ends depending on direction — first when ascending, last when descending, which is not actually "one end"), `sortPromotionsByRoi` sorts only the attributed campaigns by direction and always appends the unattributable/not-yet-attributed ones after, so they stay at the same end regardless of which direction the owner picks — this is what FR-012's "sorted consistently to one end" actually requires. Documented inline in `PromotionsPage.tsx`.
- [x] T036 [P] [US4] Add a fetch of `GET /api/promotions` to the Points page. **Decision**: `PointsPage.tsx` owns this fetch (a new `useRedemptionHistory` hook, local to that file), not `PointsCard.tsx` — `PointsCard` is reused on Home, Settings, and `LogReplacementForm`, and none of those surfaces need a promotions fetch on every mount.
- [x] T037 [US4] Add a "Redemption history" section to the Points page (T036's data), filtering to `payment_method === 'points'`, sorted newest-first by date, showing campaign id, date, and points amount (FR-014) — with an explicit, honest empty state (e.g. "No points redemptions yet") when none exist, never a bare empty table with no explanation. Uses `period.start` as the display/sort date since the API carries no separate redeemed-at timestamp.
- [x] T038 [P] [US4] Add/extend `frontend/src/components/Promotions/PromotionsPage.test.tsx`: the aggregate-by-platform sum matches a manual sum of visible rows and excludes unattributable campaigns; the sort toggle reorders correctly both directions, with unattributable campaigns consistently at one end.
- [x] T039 [P] [US4] Add tests for the Points page's redemption history: a populated case (correct campaign/date/points, newest first) and the explicit empty-state case (FR-014's last acceptance scenario).

**Checkpoint**: All four user stories are now independently functional. MVP = User Story 1 alone; each subsequent story adds value without touching the others' files.

---

## Phase 7: Polish & Cross-Cutting Concerns

- [x] T040 [P] Add a `docs/product-strategy.md` entry recording the real implementation decisions made along the way (the Home trend-arrow comparison point chosen in T026, the `-Infinity` sort-key convention introduced in T035, whether `platforms_trend.go`'s trailing-window size needed adjusting) — per this project's standing discipline of documenting engineering decisions as they are made.
- [x] T041 [P] Update `README.md` and/or `docs/architecture.html` if the new `GET /api/platforms/trend` route or the new `AskResponse.tool_calls` field changes anything those docs currently claim about the system's routes or response shape (per the project's own "update docs when the app evolves" standing instruction).
- [x] T042 Run the full verification pass before merging `feature/008-dashboard-chat-intelligence-v2` back into `develop`: backend `go build ./... && go test ./...`, frontend `npx tsc -b --noEmit && npm test -- --run`, and a manual/Playwright check of each of the 4 user stories' golden path AND their FR-013 omission edge case (insufficient data → clean omission, not a placeholder). Done: backend suite green against the live dev Postgres (222 frontend + full Go suite, fresh non-cached run, including a live-DB test that caught and led to fixing a real pre-existing bug — see platform_comparison_test.go); frontend `tsc -b --noEmit` clean; live curl verification of the new deterministic `GET /api/platforms/trend` endpoint and `payment_method`/`points_spent` fields on `GET /api/promotions` against the real running backend. No live `/api/ask` model call was possible in this verification pass (no `ANTHROPIC_API_KEY` in this shell) — the model-dependent paths (chart click-to-ask, show-your-work, compare-to-last-period) are instead verified by each story's own automated tests, which explicitly cover both the golden path and the FR-013 omission path.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — run first.
- **Foundational (Phase 2)**: Empty for this spec — no cross-story blocker exists.
- **User Stories (Phase 3-6)**: Each depends only on Phase 1's clean baseline. All four are mutually independent — implement in priority order (US1 → US2 → US3 → US4) for the cleanest incremental demo, or in parallel across agents/developers since none shares a file with another (verified during recon: US1 touches `suggestions.go`/`ask.go`/Chat+Chart components; US2 touches a new comparison-period file + Home/Platforms; US3 touches Home/Promotions derivations only; US4 touches Promotions/Points only — the only shared file across stories is `PromotionsPage.tsx`, touched by US3 (T029/T030) and US4 (T033-T035), so those two stories' Promotions edits should not run as literally simultaneous agent tasks against that one file).
- **Polish (Phase 7)**: Depends on however many of the four stories are actually implemented in this pass.

### Parallel Opportunities

- All `[P]`-marked tasks within a story touch different files and can run concurrently.
- US1, US2, and US3 can be implemented fully in parallel (no shared files). US4 should follow or coordinate with US3 specifically because both touch `PromotionsPage.tsx`.
- Test tasks marked `[P]` within a story can run alongside that story's own implementation tasks once the function/field they test exists.

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1 (baseline check).
2. Complete Phase 3 (User Story 1).
3. **STOP and VALIDATE** independently per spec.md's Independent Test for US1.
4. Demo if ready — this alone delivers SC-001 and SC-004.

### Incremental Delivery

1. Phase 1 → Phase 3 (US1) → validate → demo (MVP).
2. Add Phase 4 (US2) → validate → demo (unlocks SC-002).
3. Add Phase 5 (US3) → validate → demo (proactive insight, no new SC but the persona-completing piece).
4. Add Phase 6 (US4) → validate → demo (SC-005, the lowest-risk polish).
5. Phase 7 (Polish) once as many stories as the night allows are done.

### Parallel-Agent Strategy (per this project's standing "use subagents for independent work" practice)

Given the file-independence confirmed above: US1, US2, and US3 can each be handed to a separate agent/session to implement fully in parallel. US4 should be sequenced after (or carefully coordinated with) US3, since both edit `PromotionsPage.tsx`.
