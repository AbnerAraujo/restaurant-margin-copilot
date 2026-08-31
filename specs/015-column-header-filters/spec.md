# Feature Specification: Excel-style column-header filters, and search boxes that apply on explicit action

**Feature Branch**: `feature/column-header-filters`

**Created**: 2026-08-30 (retroactively — see plan.md's "How this spec came to exist")

**Status**: Shipped (spec written after the fact)

**Input**: Two related, conversational requests bundled into one branch and
one commit: (1) the product owner asked for a second, additive filtering
surface on data tables — a small filter icon in a column header,
Excel/Google-Sheets-style, narrowing by just that column — on top of the
filter bars several pages already had; (2) the product owner separately
reported that the existing search boxes (`FilterSearchInput`) narrowed a
grid on every keystroke, which they found jumpy, and asked that a search
only apply on an explicit action.

## Spec number

`015`. See `specs/014-connector-variance-and-upload-sync/spec.md`'s own
"Spec number" section for the numbering rationale shared by both
retroactive specs — `013-bff-layer` was the highest number in `specs/`
when this pass started, and 014/015 were assigned in the order the two
write-ups were done, not in the order the underlying branches merged.
(`feature/column-header-filters` in fact merged to `main` earlier, as PR
#3, than `feature/connector-variance-and-upload-sync`'s PR #12 — the spec
numbers don't track that.)

## Background

Four pages already had `useTableFilter`-backed filter bars (a search box,
a dropdown, status/ROI-sign chips) sitting above their tables:
`PromotionsPage`, `PlatformsPage`, `HomePage`, `PointsPage`. That surface
narrows a whole table by one query or a small set of chip toggles. It does
not let someone narrow by a *specific column's* values the way a
spreadsheet's own column-header filter does, and the product owner wanted
that second, more granular affordance — additive to the existing bar, not
a replacement for it.

Separately, every one of those same search boxes updated the visible
results on every keystroke. That is a common enough UI pattern that it
usually goes unquestioned, but here it was reported as an actual complaint:
narrowing a financial table mid-keystroke, before the owner has finished
typing what they meant, reads as jumpy rather than responsive. The fix
requested was explicit-action search (Enter, or clicking the search
button) — not a debounce, which still applies automatically and only adds
a delay, which is not what was asked for.

Both changes shipped in one commit (`03be232`, "Add Excel-style column
header filters, and make search apply on Enter") because they are the same
underlying judgment call — a grid should narrow when the user says so, at
whatever granularity they said so at — applied to two different controls.

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Narrow a table by one column's values (Priority: P1)

An owner reviewing a preview table (a parsed cost sheet, a connector sync
preview) wants to see only the rows from one supplier, or only rows above
a dollar amount, without leaving the page or constructing a query in a
search box. They click a small filter icon in that column's header, pick
or type what they want, and only matching rows remain — while any
existing page-level search or chip filter keeps applying too.

**Why this priority**: it is the requested feature, and it is the
granularity the existing filter bars could not offer.

**Independent Test**: On `CostSheetTab`'s preview table, open the Supplier
column filter, check one supplier, and confirm the row count narrows to
exactly that supplier's rows; clear the filter and confirm every row
returns.

**Acceptance Scenarios**:

1. **Given** a table column configured as `categorical`, **When** the
   owner opens its filter and checks a value, **Then** only rows whose
   cell in that column equals a checked value remain, and the filter
   applies immediately (no separate confirm step).
2. **Given** a table column configured as `text`, **When** the owner types
   a query and presses Enter (or clicks Apply), **Then** rows are narrowed
   to those whose cell in that column contains the query,
   case-insensitively — and typing alone, without pressing Enter or
   Apply, narrows nothing yet.
3. **Given** a table column configured as `numeric`, **When** the owner
   enters a minimum, a maximum, or both and applies, **Then** rows are
   narrowed to those whose cell — parsed as a number after stripping
   currency formatting — falls within the entered bounds; a cell that
   cannot be parsed as a number is excluded from the results rather than
   guessed at.
4. **Given** two active column filters on the same table, **When** both
   are applied, **Then** a row must satisfy both to remain (AND, not OR).
5. **Given** a table with its own existing search box or chip filter
   already narrowing it, **When** a column filter is also applied,
   **Then** both narrow the same result set together — the column filter
   composes with, and never replaces, the existing surface.
6. **Given** an active column filter, **When** the header is rendered,
   **Then** the active state is shown by more than color alone (an icon
   plus a small dot plus an `aria-label` naming the active value or
   range).
7. **Given** a `DataGrid` caller that does not pass `columnFilters` at
   all, **When** it renders, **Then** it looks and behaves exactly as it
   did before this feature — no filter icon appears anywhere.

---

### User Story 2 — Only some tables get this, on stated reasoning (Priority: P2)

A reviewer can see, for any table in the app, either the column-filter
affordance or a documented reason it was deliberately withheld — never an
unexplained gap.

**Why this priority**: the affordance is real added surface area and real
added complexity per table; applying it everywhere "on reflex" would be
exactly the kind of unscoped, undifferentiated feature-add this project's
own skills (`dataviz`, `ux-writing`) argue against, and several existing
tables have properties that make it actively unhelpful.

**Acceptance Scenarios**:

1. **Given** `CostSheetTab`'s and `ConnectedPlatformsTab`'s preview
   tables, **When** they render, **Then** each carries column filters —
   both are standalone `DataGrid` tables with no chart to stay in sync
   with, both can grow to dozens of rows, and both have genuine
   categorical or numeric dimensions worth narrowing by.
2. **Given** `HomePage`'s "Recent closes" table, **When** it is reviewed
   for inclusion, **Then** it is excluded — capped at a small fixed row
   count with its one categorical dimension already exposed as visible
   toggle chips one line above it.
3. **Given** `PlatformsPage`'s side-by-side comparison `DataGrid`, **When**
   it is reviewed, **Then** it is excluded — row count is bounded by how
   many delivery platforms exist, the platform name is each row's own
   identity and already searchable, and the component's own doc comment
   states it is deliberately plain so every number on it can be trusted
   without first understanding a control.
4. **Given** every "View as table" fallback rendered beside a chart
   (`MarginTrendChart`, `CategoryBarChart`, `CompositionPieChart`,
   `EffectiveRateTrendChart`, `PromoRoiChart`'s embedded table) and the
   chat answer's own `DataGrid`, **When** they are reviewed, **Then** all
   are excluded as one class — each is an accessibility-parity twin of a
   chart or a compact answer citation, and letting a fallback table filter
   independently of the chart or answer it mirrors would let the two
   disagree about what is on screen.
5. **Given** `PointsPage`'s fixed 5-row rules table and its redemption
   history list, **When** they are reviewed, **Then** both are excluded —
   too small to need narrowing, and the redemption history is not even a
   `<table>` with headers to hang the affordance off.

---

### User Story 3 — A search box narrows a grid only when the owner says so (Priority: P2)

An owner typing into any of this app's search boxes sees their own text
update immediately, but the table underneath does not narrow until they
press Enter or click the search control.

**Why this priority**: reported directly by the product owner as a real
usability complaint, independent of the column-filter request, but fixed
by the same underlying discipline.

**Acceptance Scenarios**:

1. **Given** any of the four existing search boxes ("Search campaigns",
   "Search redemption history", "Search platforms", Home's "Search recent
   closes by date"), **When** the owner types, **Then** the input's own
   visible text updates every keystroke and the table below does not
   narrow.
2. **Given** the same input, **When** the owner presses Enter or clicks
   the (now-clickable) search icon, **Then** the table narrows to the
   applied query.
3. **Given** an applied search, **When** something outside the owner's own
   typing changes the applied value (a "Clear filters" action, a browser
   back/forward restoring a different query from the URL), **Then** the
   input's displayed text re-syncs to match the new applied value.
4. **Given** the same discipline, **When** a column's `text` or `numeric`
   filter is used, **Then** it behaves identically — draft-then-apply, not
   per-keystroke — for consistency; a column's `categorical` checklist
   filter is exempted and still applies immediately, matching the
   existing status/ROI-sign chips' own behavior, because a single
   discrete choice does not warrant a confirm step.

### Edge Cases

- **A numeric column filter given a cell that cannot be parsed as a
  number** (an em dash standing in for "no value", a refused figure). Excluded
  from the filtered result rather than treated as a match or a non-match by
  guesswork.
- **A categorical filter's option list**, when the underlying table has
  fewer than two distinct values in that column. Still renders — a
  single-value column trivially "filters" to itself, and the affordance
  costs nothing to leave in place.
- **Reloading `/upload` mid-preview.** Column-filter state (and page-level
  search/chip state on this page) is discarded along with the staged
  `File` object — this is consistent with the whole page's state model,
  not a regression column filters introduce.
- **A future table that already syncs its `useTableFilter` state to the
  URL.** This feature's column-filter state is deliberately local
  (`useState`), not URL-synced. A future caller with existing URL-synced
  search state should extend that same discipline to its own column
  filters rather than copy this feature's local-state approach verbatim —
  recorded as guidance, not enforced by any code in this feature.

## Requirements *(mandatory)*

### Functional Requirements — column-header filters

- **FR-001**: `DataGrid` MUST accept an optional `columnFilters` prop keyed
  by column index, each entry specifying one of three filter types
  (`categorical`, `text`, `numeric`). Omitting the prop MUST render the
  grid exactly as it rendered before this feature existed.
- **FR-002**: A `categorical` filter MUST present a checklist of every
  distinct value actually present in that column's data, in first-seen row
  order, and MUST apply each checkbox's state immediately.
- **FR-003**: A `text` filter MUST match a cell as a case-insensitive
  substring of the applied query, and MUST NOT narrow results until the
  query is applied (Enter, or an explicit Apply action).
- **FR-004**: A `numeric` filter MUST parse a cell's displayed string
  (stripping currency/formatting characters) into a number for comparison
  against an applied min and/or max bound, MUST treat an unparseable cell
  as excluded from the filtered result (never guessed into either side),
  and MUST NOT narrow results until applied.
- **FR-005**: Multiple active column filters on one table MUST compose
  with AND semantics.
- **FR-006**: A column filter MUST compose additively with any existing
  page-level filter surface (a search box, a dropdown, chips) already
  narrowing the same table — never replace or bypass it.
- **FR-007**: An active column filter's indicator MUST NOT rely on color
  alone — the trigger's accessible name MUST state that it is active plus
  a plain-language summary of the active value(s) or range, in addition to
  any visual (color, dot) treatment.
- **FR-008**: The affordance MUST be reusable — implemented once
  (`useColumnFilters` for state/matching, `ColumnFilterButton` for the
  header trigger and popover) and consumed by any `DataGrid` caller that
  opts in, not reimplemented per table.
- **FR-009**: Column-filter state MUST be local component state, not
  synced to the URL or persisted across a page reload.
- **FR-010**: `CostSheetTab` and `ConnectedPlatformsTab` MUST each opt in
  with filter types matching their real data (`CostSheetTab`: text on
  Invoice ID, categorical on Supplier and Category; `ConnectedPlatformsTab`:
  categorical on Source, numeric range on Orders).
- **FR-011**: `HomePage`'s Recent Closes table, `PlatformsPage`'s
  side-by-side comparison `DataGrid`, `PointsPage`'s rules table and
  redemption history, every chart's "View as table" fallback, and chat's
  answer `DataGrid` MUST NOT gain column filters, per the stated reasoning
  in User Story 2.

### Functional Requirements — explicit-action search

- **FR-012**: Every existing `FilterSearchInput` instance MUST update its
  own displayed text on every keystroke while deferring the callback that
  actually narrows the table until Enter is pressed or the search control
  is clicked.
- **FR-013**: The search trigger MUST be a real, keyboard-operable button,
  not a decorative icon.
- **FR-014**: When the applied search value changes from outside the
  owner's own typing (a clear action, a URL-driven restore), the input's
  displayed draft MUST re-sync to the new applied value.
- **FR-015**: This behavior MUST NOT be implemented as a debounce — a
  debounce still applies automatically, merely delayed, which is a
  different behavior than what was requested and reported.

### Key Entities

- **Column filter spec**: A per-column declaration of filter type
  (`categorical` | `text` | `numeric`) that a `DataGrid` caller opts a
  specific column index into.
- **Column filter state**: One column's current filter value(s) —
  a selected-value set, an applied text query, or an applied numeric
  range — held locally per table instance.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: `useColumnFilters.test.ts` (12 cases) and `DataGrid.test.tsx`
  cover option ordering, AND-composition, numeric parsing with exclusion
  of unparseable cells, clear-one vs. clear-all, keyboard reachability, and
  the empty state — independent of any specific page.
- **SC-002**: `CostSheetTab.test.tsx` and `ConnectedPlatformsTab.test.tsx`
  each gained at least one integration case proving a column filter
  narrows their real preview table.
- **SC-003**: `HomePage.test.tsx`, `PlatformsPage.test.tsx`,
  `PointsPage.test.tsx`, and `PromotionsPage.test.tsx` — every test that
  previously asserted immediate narrowing on `userEvent.type` was updated
  to press Enter first; `filter-bar.test.tsx` gained explicit cases for
  "typing alone doesn't apply," "Enter applies," "clicking applies," and
  "re-syncs on an externally changed value."
- **SC-004**: Verified live against the real backend: uploading a real
  5-row cost sheet and applying a Supplier checklist filter narrowed 5 rows
  to 2; previewing a real 347-row simulated connector sync and applying a
  Platform checklist filter narrowed 14 preview rows to 7, further narrowed
  by an Orders numeric-range filter; typing an unmatched query into
  Promotions' "Search campaigns" left all 30 campaigns visible until Enter
  was pressed, at which point the real "No campaigns match these filters"
  empty state rendered.
- **SC-005**: Every `DataGrid` caller that does not pass `columnFilters`
  (chat's answer grid, `PlatformsPage`'s comparison grid) renders
  byte-for-byte as it did before this feature.

## Assumptions

Chosen rather than clarified.

- **Local state, not URL sync, for column filters.** Both wired-in tables
  are one-shot, preview-before-commit surfaces with no route of their own;
  reloading the page already discards the staged upload regardless of what
  a URL might remember, so persisting column-filter choices there would
  promise something the flow can't keep.
- **Categorical filters apply immediately; text and numeric require an
  explicit apply.** A single discrete checkbox toggle is a complete
  decision the instant it's made — the same reasoning the existing
  status/ROI-sign chips already followed — while a partially-typed query
  or a half-entered numeric bound is not yet a decision.
- **No debounce anywhere in this feature.** A debounce was considered and
  rejected for the search-box fix specifically because it does not answer
  the complaint that was raised — it still narrows without being asked,
  merely later.
- **The exclusion list is a judgment call, not a rule with no exceptions.**
  Each excluded table is excluded for its own stated reason (capped rows,
  chart-fallback duplication, no real categorical dimension), not by a
  blanket "charts and small tables never get this" policy that might not
  hold for a future table.
