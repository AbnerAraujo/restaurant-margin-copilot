# Implementation Plan: Excel-style column-header filters, and search boxes that apply on explicit action

**Spec**: [spec.md](./spec.md) · **Status**: Already implemented and shipped — this plan documents it after the fact

## How this spec came to exist (read this first)

**This spec and plan were written after the code they describe was already
built, reviewed, and merged to `main`.** Like `specs/014`, this feature was
requested and built directly from conversation, bypassing this project's
own front door
(`/speckit-specify` → `/speckit-plan` → `/speckit-tasks` → `/speckit-implement`).
`README.md` currently states "everything here was produced through a real
spec-driven process ... not written after the fact"; for this feature, and
for spec 014, that sentence was not accurate at the time it was written,
and this plan (along with the README edit made alongside it) is how that
gap is being closed rather than left standing.

This document describes the actual shipped implementation — read from the
merged diff (`git show 03be232 --stat`), the real CHANGELOG entry, and the
code itself — organized into a plan.md's shape. It is not a design that
was reviewed before implementation began; no such review happened. Per
Constitution Principle V ("report what happened ... rather than hidden"),
that is stated here plainly rather than implied away by writing this
document as though it had come first. Producing a plan that read as
contemporaneous when it is not would be fabricating provenance about the
project's own process — the same category of dishonesty this codebase
works hard to keep out of its numbers (`internal/answerverify`,
`platformconnector`'s disclosure requirements), applied here to how the
project describes its own history.

## Technical Context

One reusable pair of frontend primitives, consumed by two existing tables,
plus a behavioral change to one shared search component:

1. `frontend/src/lib/useColumnFilters.ts` — filter state and row-matching
   logic, operating on `DataGrid`'s existing plain `columns: string[]` /
   `rows: string[][]` shape.
2. `frontend/src/components/ui/column-filter.tsx` — `ColumnFilterButton`,
   the header trigger and its popover, built on Radix `Popover`.
3. `frontend/src/components/Charts/DataGrid.tsx` — an opt-in
   `columnFilters` prop wiring the above two together per column.
4. `frontend/src/components/Upload/CostSheetTab.tsx` and
   `ConnectedPlatformsTab.tsx` — the two tables that opted in.
5. `frontend/src/components/ui/filter-bar.tsx`'s `FilterSearchInput` — the
   shared search box, changed to apply on Enter/click rather than per
   keystroke.

Backend: zero-line diff. This is a frontend-only feature.

## Constitution Check

- **Deterministic core**: untouched. This feature filters already-rendered
  strings client-side; it computes no financial figure and reads no new
  data from the API. ✅
- **Refuse rather than guess**: `parseNumericCell` returns `null` for a
  cell it cannot parse, and that cell is excluded from a numeric filter's
  results rather than coerced to 0 or otherwise guessed at — the same
  refuse-over-estimate posture this codebase applies to financial
  computation, applied here to a client-side UI filter. ✅
- **Typed tools / provenance**: not implicated — no MCP tool, no number
  shown to the owner, no provenance claim made or altered by this feature.

No violations. (As with spec 014, no Constitution Check ran before this
code was written; this section states what a contemporaneous check would
have found, verified against the actual shipped code.)

## Scope decision: which tables got this, and why

This is the part of the plan most worth stating carefully, because the
temptation with a reusable UI affordance is to apply it everywhere on
reflex. What actually happened was a deliberate survey of every `<table>`
under `frontend/src/components/**`, not just the four `useTableFilter`
pages, followed by a per-table judgment call:

| Table | Decision | Reasoning actually used |
|---|---|---|
| `CostSheetTab.tsx` preview | **Included** — text (Invoice ID), categorical (Supplier, Category) | Standalone `DataGrid`, no chart to stay in sync with; a real cost sheet can run to dozens of line items; genuine categorical dimensions |
| `ConnectedPlatformsTab.tsx` preview | **Included** — categorical (Source), numeric (Orders) | Same standalone shape; up to 31 days × every connected platform can interleave into 60+ rows |
| `HomePage`'s "Recent closes" | **Excluded** | Capped at `RECENT_CLOSE_ROWS` (7); its one categorical dimension (Status) is already visible chips one line above the table — a column checklist for the same 2 values adds a second control for no new narrowing power |
| `PlatformsPage`'s side-by-side `DataGrid` | **Excluded** | Row count bounded by how many delivery platforms this restaurant has (2); the platform name is each row's identity and already the page's own search target; `DataGrid`'s own doc comment states it is "deliberately plain: no sorting, no filtering ... every interactive affordance added here would be a control the reader has to understand before trusting the number" |
| `PointsPage`'s rules table (5 fixed rows) | **Excluded** | Too small and non-growing, no real categorical dimension — the rule name *is* the row |
| `PointsPage`'s redemption history | **Excluded** | Not a `<table>` at all (a `<ul>` of flex rows) — no header to hang the affordance off; its one categorical dimension is already covered by the existing filter bar's dropdown |
| Every chart's "View as table" fallback (`MarginTrendChart`, `CategoryBarChart`, `CompositionPieChart`, `EffectiveRateTrendChart`, `PromoRoiChart`'s embedded table) | **Excluded**, as one class | Accessibility-parity twins of a chart, not independent explorable grids — filtering the table without also filtering the chart it mirrors would let the two disagree about what's on screen, which is the exact "two renderings of one number that can drift" failure this product's provenance discipline exists to prevent elsewhere |
| Chat's `AnswerVisualizationView` `DataGrid` | **Excluded** | Renders a handful of rows scoped to one answer, by explicit design — not a table anyone is meant to explore independently of the answer it's citing |

This table is reproduced from spec.md's User Story 2 because it is the
actual design artifact — the judgment calls, not the code, are what makes
this feature defensible as scoped rather than reflexive.

## The reusable pair, as actually built

### `useColumnFilters`

Three filter-state shapes (`categorical`, `text`, `numeric`), each with its
own `isColumnStateActive` and `matchesColumnState` logic, composed by index
into the same `columns`/`rows` arrays `DataGrid` already holds. Options for
a `categorical` column are computed by a first-seen scan over `rows` —
never a hardcoded list, so a filter can never offer a value that isn't
actually present in that grid's data. `parseNumericCell` strips everything
but digits, sign, and decimal point before parsing, because `DataGrid`
cells arrive pre-formatted ("$1,234.56"), never as raw numbers.

The hook deliberately does not read or write the URL — see spec.md's
Assumptions for why, and the doc comment at the top of the file itself for
the same reasoning left in the code for the next person who extends it.

### `ColumnFilterButton`

Built on Radix `Popover` — already a project dependency via
`ui/tooltip.tsx`'s `Tooltip`, not a new one — for the accessible-popover
plumbing this feature would otherwise have had to hand-roll: focus moves
into the panel on open, `Escape` closes it and returns focus to the
trigger, a click outside closes it. Three interchangeable panel bodies
(`CategoricalPanel`, `TextPanel`, `NumericPanel`), matching
`useColumnFilters`'s three state shapes one-to-one.

The categorical panel applies each checkbox immediately (`onToggle` fires
straight through to `useColumnFilters`); the text and numeric panels stage
a local `draft` and only call `onApply` on Enter or the panel's own Apply
button — the same explicit-apply discipline as Part 2 below, applied
consistently to the column filters themselves.

### `DataGrid`'s `columnFilters` prop

Opt-in, keyed by column index (`ColumnFilterSpecs = Partial<Record<number,
ColumnFilterType>>`). A caller that omits it gets `specs = {}`, and every
downstream branch in `DataGrid` that renders a filter trigger is gated on
a given column index actually being present in that map — so an
unconfigured caller's render output is provably unchanged, not merely
visually similar.

## Part 2: search boxes apply on explicit action

### The mechanism, as actually built

`FilterSearchInput` already held the applied `value` as a prop from its
caller's `useTableFilter` state. The change adds a local `draft` string,
rendered in the `<input>` instead of `value` directly, updated on every
`onChange`. The callback that actually narrows the table (`onChange` to
the parent) now fires only from an Enter keydown or a click on the search
icon — which became a real `<button>` rather than a decorative `<svg>`.

### Re-sync without an effect

When `value` changes from *outside* the user's own typing — a "Clear
filters" action, a browser back/forward restoring a different query from
the URL — `draft` has to catch up. This is done by comparing `value`
against a `lastSeenValue` captured in state and reconciling **during
render** (the same render-time state-adjustment pattern React's own docs
describe for "adjust state when a prop changes"), not inside a
`useEffect`. Two reasons, both real: an effect would cost an extra render
on every apply (render with the stale draft, commit, effect fires, state
updates, render again), and this repo's `react-hooks/set-state-in-effect`
lint rule already flags "reset local state when a prop changes" as the
wrong tool for the job.

The identical pattern is applied to the column-header text/numeric panels'
own draft state, for the same reason and for consistency between the two
surfaces this feature touches.

### Why not a debounce

Recorded because it is the obvious alternative and it was explicitly
rejected: a debounce still narrows the table automatically, only delayed
by a fixed interval. The complaint was that the table narrows without
being asked — a delay does not fix that, it just makes the un-asked-for
narrowing arrive slightly later. Requiring Enter or a click is a different
behavior, not a slower version of the same one.

## Testing, as it actually exists

| Test | File | Proves |
|---|---|---|
| 12 cases (option ordering, AND-composition, numeric parsing/exclusion, clear-one/clear-all) | `useColumnFilters.test.ts` | The hook's matching logic, independent of any page |
| Opens/filters/clears/keyboard/empty-state | `DataGrid.test.tsx` | `ColumnFilterButton` + `DataGrid` wiring |
| Integration case per table | `CostSheetTab.test.tsx`, `ConnectedPlatformsTab.test.tsx` | A real column filter narrows a real preview table |
| Updated to press Enter before asserting | `HomePage.test.tsx`, `PlatformsPage.test.tsx`, `PointsPage.test.tsx`, `PromotionsPage.test.tsx` | The four existing search boxes no longer narrow on keystroke |
| "typing alone doesn't apply," "Enter applies," "clicking applies," "re-syncs externally" | `filter-bar.test.tsx` | FR-012 through FR-014 directly |

**Live verification** (SC-004): a real 5-row cost sheet CSV, a Supplier
checklist filter (5 → 2 rows); a real 347-row simulated connector sync
preview, a Platform checklist filter (14 preview rows → 7) plus an Orders
numeric-range filter; an unmatched Promotions search query left unapplied
until Enter, then showing the real "No campaigns match these filters"
empty state.

## Documentation

`CHANGELOG.md` carries the dated entry ("Excel-style per-column header
filters, and the search boxes that stopped filtering on every keystroke")
this plan is sourced from. `docs/frontend.md` was extended in the same
commit to describe the new reusable pair and the search-box change.
`docs/openapi.yaml` was **not** touched — correctly, since this feature
adds no new API surface (it is a pure client-side filter over data the API
already returns).
