---

description: "Task list for Excel-style column-header filters and explicit-action search"
---

# Tasks: Excel-style column-header filters, and search boxes that apply on explicit action

**This task list is retroactive.** See plan.md's "How this spec came to
exist" for the full disclosure: this feature was built and merged
(`03be232`) before this file existed. Every task below is checked off
because it reflects work already done, recovered from the real diff and
the real CHANGELOG entry — not a forward plan being executed.

**Input**: `specs/015-column-header-filters/` (spec.md, plan.md), the
merged commit `03be232`, and `CHANGELOG.md`'s 2026-08-30 entry.

**Organization**: Part A is the reusable column-filter pair and its two
consumers. Part B is the independent search-box behavior change, bundled
into the same commit because both are the same underlying "narrow on an
explicit action, at whatever granularity" judgment call.

## Format: `[ID] [P?] [Part] Description`

---

## Part A — column-header filters

- [x] T001 [A] `frontend/src/lib/useColumnFilters.ts`: the three filter-state
  shapes (`categorical`, `text`, `numeric`), `matchesColumnState`,
  `parseNumericCell` (strip-then-parse, `null` on failure), and the
  `useColumnFilters` hook (`getOptions`, per-type getters/setters,
  `clearColumn`, `clearAll`).
- [x] T002 [A] [P] `frontend/src/lib/useColumnFilters.test.ts`: 12 cases —
  unfiltered pass-through, first-seen option order, single/union
  categorical selection, toggle-off, AND-composition across columns,
  case-insensitive text substring match, blank-query clears, numeric range
  parsing with an unparseable cell excluded, open-ended bounds,
  clear-one vs. clear-all.
- [x] T003 [A] `frontend/src/components/ui/column-filter.tsx`:
  `ColumnFilterButton` on Radix `Popover`, and its three panel bodies
  (`CategoricalPanel` applies immediately; `TextPanel` and `NumericPanel`
  stage a local draft and apply on Enter/click, with the render-time
  re-sync pattern for external value changes).
- [x] T004 [A] `frontend/src/components/Charts/DataGrid.tsx`: the opt-in
  `columnFilters` prop, wiring `useColumnFilters` + `ColumnFilterButton`
  into the existing table header row, gated so an omitted prop renders
  identically to before.
- [x] T005 [A] [P] `frontend/src/components/Charts/DataGrid.test.tsx`: 7
  cases — no affordance when `columnFilters` is omitted, a trigger only on
  configured columns, categorical narrowing, the empty state on no
  matches, "Clear filters," numeric range applying on Enter (not per
  keystroke), and keyboard reachability (Tab to the trigger, Enter opens
  it).
- [x] T006 [A] `frontend/src/components/Upload/CostSheetTab.tsx`: opt into
  `columnFilters` — text on Invoice ID, categorical on Supplier and
  Category.
- [x] T007 [A] [P] `frontend/src/components/Upload/CostSheetTab.test.tsx`:
  one integration case proving a column filter narrows the real preview
  table.
- [x] T008 [A] `frontend/src/components/Upload/ConnectedPlatformsTab.tsx`:
  opt into `columnFilters` — categorical on Source, numeric range on
  Orders.
- [x] T009 [A] [P] `frontend/src/components/Upload/ConnectedPlatformsTab.test.tsx`:
  one integration case proving a column filter narrows the real preview
  table.
- [x] T010 [A] [P] `docs/frontend.md`: document the reusable pair and the
  scope decision (which tables got it, which didn't, and why).

## Part B — explicit-action search boxes

- [x] T011 [B] `frontend/src/components/ui/filter-bar.tsx`:
  `FilterSearchInput` gains local `draft` state, a real `<button>` for the
  search icon, an Enter-key handler, and the render-time
  `lastSeenValue`-comparison re-sync (not a `useEffect`) for externally
  changed applied values.
- [x] T012 [B] [P] `frontend/src/components/ui/filter-bar.test.tsx`: cases
  for "typing alone doesn't apply," "Enter applies," "clicking the search
  button applies," and "the input re-syncs when the applied value changes
  externally."
- [x] T013 [B] [P] `frontend/src/components/Home/HomePage.test.tsx`,
  `PlatformsPage.test.tsx`, `PointsPage.test.tsx`,
  `PromotionsPage.test.tsx`: updated existing search-box assertions to
  press Enter before checking the narrowed result.

## Cross-cutting

- [x] T014 `CHANGELOG.md`: the dated entry this spec and plan are sourced
  from.
- [x] T015 Full verification, as reported in the commit and CHANGELOG:
  `tsc -b --noEmit` and `vitest run` (596/596 passing), plus a live
  verification session against the real backend on `:8080` from a
  frontend dev server on `:5273` — a real cost-sheet upload filtered by
  Supplier, a real connector-sync preview filtered by Platform and by an
  Orders numeric range, and Promotions' search box confirmed to hold
  typed-but-unapplied text until Enter.
