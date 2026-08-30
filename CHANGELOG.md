# Changelog

All notable changes to My Business Steward (Restaurant Margin Copilot), in the
spirit of [Keep a Changelog](https://keepachangelog.com/), adapted for how this
project actually shipped: a single continuous take-home build across two real
dates, not versioned releases. Entries are grouped by date and then by
milestone, reverse-chronological. Every entry below is derived from this
repo's own `git log` (2026-08-27 through 2026-08-30) — nothing
here is invented, and honest bugs/regressions are named, not smoothed over,
matching this project's own stated documentation discipline (Constitution
Principle V: report what happened, including failures).

---

## 2026-08-30 — One upload, one reconciliation: cost-sheet commit can pull its own dates' revenue, and simulated days stop being uniformly profitable

Two requests, both about the same complaint: what the product shows after an
upload was not the whole day.

### 1. Uploading a cost sheet can now pull in the matching platform revenue

**The problem.** `POST /api/ingest/cost-sheet/commit` persisted supplier costs
and re-ran reconciliation; `POST /api/connectors/sync` fetched simulated
iFood/Just Eat Takeaway/POS revenue for a chosen range and re-ran the same
pipeline. Two endpoints, two tabs, two trips. Upload today's costs and the
margin you were shown was those costs against whatever revenue happened to
already be on file — a real number, but not the one the owner came for.

**What changed.** The commit endpoint now derives the calendar range the
uploaded invoices cover (`costSheetDateRange`: a min/max scan over every parsed
`invoice_date`, because a cost sheet is one invoice per row on a supplier's own
irregular cadence, not one date) and — when asked — fetches the connector
revenue for exactly that range and commits both through **one**
`RunIngestionPipelineWithConnectorOverlay` call. One run, not two: two
sequential runs would durably persist an intermediate state in which the new
costs sat against the old revenue, a reconciliation that was never true of
anything.

The composition lives in `internal/httpapi`, not in `internal/pipeline`.
`internal/pipeline` already accepts a `ConnectorOverlay` and
`internal/platformconnector` already produces one; making the deterministic
core reach into the integration layer that feeds it would be backwards, and
would put "did the user tick a box" inside a package whose job is arithmetic.
Orchestrating two existing pipelines is what the handler layer is for.

**The design decision: opt-in at the API, default-on in the UI.** This was the
judgment call, and it is not "automatic" and not "off by default" — it is both,
at the layer each is honest at.

`internal/platformconnector`'s package doc states the connections are simulated
five separate times on the way to a number, on the reasoning that a disclosure
living in exactly one place is one that can be cropped out of a screenshot. A
cost-sheet upload that silently reached out and injected simulated revenue into
the owner's margin would defeat all five at once: they never opened the tab that
carries the warning, never saw the notice, never pressed a button whose own
label says "simulated". The numbers would just be different afterwards. That is
the product inventing data behind the owner's back.

So:

- **API** (`wantsConnectorSync`) — `sync_connectors` absent means **false**.
  Every client that existed before this change (curl, the evaluation harness,
  any future integration) behaves exactly as it did. An API has no banner to
  read.
- **UI** (`CostSheetTab.tsx`) — the checkbox is **pre-ticked**, sits in the
  preview panel directly above the commit button, names the exact date range it
  will pull, and says "simulated" and "no real account is connected" in its own
  label. Pre-ticked because it is what was asked for; visible and adjacent
  because consent has to be current and refusable without leaving the page.
- **Either way, the response says what happened.** `connector_sync` is a
  populated object or `null` — a real "not asked for", never an empty sync — and
  the commit panel restates the orders, tickets, duplicates removed **and
  unresolved overlaps** afterwards.

**Refusal is atomic.** If the invoices span more than the connector's 31-day cap
(or an upstream fails), the fetch happens *before* `ingestMu` is taken and
before a byte is written, so the whole request is refused with the connector's
own message plus what to do about it, and the cost sheet on file is untouched.
Verified live: a 124-row sheet spanning 2026-06-02..2026-08-29 with the opt-in
returns `422 connector_fetch_failed` — *"date range 2026-06-02..2026-08-29
covers 89 days, more than the 31-day limit on a single sync — sync a shorter
range — the cost sheet was not committed; upload it again without \"also pull
in simulated platform revenue\" to commit it on its own"* — with the on-disk
cost sheet still carrying all 25 months before and after. The same file without
the opt-in commits normally (`200`, 124 rows).

Half-committing (persist the costs, mention the sync failed in a field nobody
reads) was rejected: the owner asked for one thing, and the failing half is the
half that changes financial numbers.

### 2. Simulated days now have real variance, with a statable cause

**The problem.** Every connector-synced day reconciled to a healthy positive
margin. `seed.go` drew an order count from one narrow band
(`minOrdersPerDay + Intn(orderCountSpread)`), applied a flat Friday/Saturday
lift, and stopped. Measured over 363 days against the live dataset's own cost
sheet: daily gross ran **$3,970 to $9,131 (0.66x to 1.51x of its own mean)** — a
band too tight for any plausible cost sheet to push through zero. The only
losing days it produced were days the *cost sheet* spiked on. A product whose
job is telling an owner when something went wrong was demoing on data where
nothing ever did.

**What changed: a trading-day condition model**, following `cmd/gendata`'s
monthly-regime convention in spirit — a named, statable cause, never an
unexplained multiplier. Seven conditions in a weighted table: an ordinary day
(52%), a quiet midweek lull, a neighbourhood event (the upside half — a
distribution with only bad days is as flat a line as one with only good days),
a delivery-app outage, a short-staffed shift, a kitchen equipment failure, and
severe weather. Each carries a demand multiplier for the **delivery** side and a
separate one for the **dining room**, because real disruptions hit them
differently: an app outage barely touches walk-ins, a storm empties the dining
room faster than it empties the delivery queue. Bad-weather and equipment days
also raise the refund rate (capped), which is causally right and exercises the
refund-sign normalization on the connector path.

Two things about the mechanism differ from `cmd/gendata`, both forced rather
than chosen:

- **Granularity.** `gendata` plans a whole dataset up front and can tag calendar
  *months*, then allocate a fixed count of shock days inside each. A connector
  fetch is random access — one date, or three, in any order — so the unit here
  is the **day**, drawn from a weight table seeded off the date itself. The
  hash-based per-key seeding `dayRNG` was built for is preserved exactly; the
  condition simply gets its **own seed namespace keyed on the date alone, not on
  (platform, date)**, so iFood, Just Eat Takeaway and the POS all agree about
  what kind of day it was. Drawing it per-platform would produce a day where one
  platform was snowed in and the other was not — not a bad day, a bug that looks
  like one.
- **What a demand dip can do.** `gendata`'s own research ledger records that a
  demand dip alone can never flip one of *its* months negative, because every
  cost it models scales with revenue — so it pairs each slump with a cost-side
  shock. That constraint does not hold here, and the asymmetry is the whole
  mechanism: **the connector supplies revenue only.** The costs a synced day
  reconciles against come from the cost sheet, and they are fixed — the produce
  was ordered, delivered and invoiced before anyone knew what the day would do.
  A demand collapse against already-committed input costs is how a real
  restaurant loses money on a Tuesday. No cost-side fiction is invented on this
  path; the connector never writes an invoice.

The flat `weekendLift` was also replaced by a full weekday curve (`dayShape`:
Mon 0.82 → Sat 1.30, Thursday as the 1.00 anchor). Its mean is 1.026 against
the old shape's 1.071, so it changes the shape of a week, not the size of the
business.

**Before / after, measured** (363 days from 2025-09-01, all three simulated
sources, scored against the live dataset's real `supplier_cost_sheet.csv`):

| | before (salt v1) | after (salt v2) | the historical CSV dataset, same window |
|---|---|---|---|
| mean daily gross | $6,037.93 | $5,354.83 | $4,423.90 |
| mean daily margin | $3,495.11 | $2,856.34 | $1,940.26 |
| quietest day | 0.66x mean | 0.28x mean | 0.49x mean |
| busiest day | 1.51x mean | 2.36x mean | 2.03x mean |
| losing days | 35/363 (9.6%) | 55/363 (15.2%) | 73/363 (20.1%) |
| worst / best day | −$5,484.60 / +$8,428.00 | −$4,938.04 / +$10,855.09 | −$5,199.27 / +$8,244.82 |

Two things worth naming honestly. The loss rate moved from 9.6% to 15.2%
against the dataset's own 20.1% — closer, not equal, and it was **not** tuned
further to close the gap, because at this business's cost structure (a ~60%
margin before rent and labour, which this product's margin metric excludes by
design) reaching 20% would have required demand cuts deep enough to break the
dollar scale. And the mean daily gross *fell* by 11%, as a side effect of
`dayShape`'s lower mean — which moved the connector from 36% above the
historical dataset's mean to 21% above it. The scale constants themselves
(`meanTicketCents`, `minOrdersPerDay`, `orderCountSpread`, both commission
rates) are untouched.

**Every unusual day now says why.** `PlatformDayTotals.TradingNote` →
`trading_note` in the API → a "Trading day" column in the connector preview
table. Empty on an ordinary day, because a UI that printed "nothing in
particular happened" on five days out of seven would train people to stop
reading the column.

**This intentionally changes every previously-generated connector number, and
`connectorSeedSalt` was bumped `v1` → `v2` to record it.** That constant's own
doc comment has said since spec 010 that changing it "changes every simulated
number, which is a deliberate, visible act, not something that should happen by
accident" — this is that act, performed deliberately and visibly. Nothing
outside `internal/platformconnector` moves: `cmd/gendata` seeds its own stream
from its own constant, imports no internal package at all (`go list -deps
./cmd/gendata` returns only itself), and its four regenerated CSVs are
**byte-identical** to the pre-change output (same MD5s), reconciling to the same
`$1,078,340.64` total margin across the same 759 days.

**Live smoke test**, on a throwaway Postgres with a freshly regenerated dataset —
uploading August 2026's real cost sheet with the opt-in ticked, one action:

| | negative days | worst day | best day |
|---|---|---|---|
| cost sheet alone (opt-in unticked) | 3 / 29 | −$2,314.65 | +$4,992.60 |
| cost sheet + connector pull | **5 / 29** | −$3,166.85 | +$6,051.70 |

The five losing days are `08-01` (−$216.68, severe weather), `08-05` (−$937.39,
quiet midweek), `08-11` (−$681.41, kitchen equipment failure), `08-13`
(−$136.86, kitchen equipment failure) and `08-26` (−$3,166.85, quiet midweek on
the month's $6,245 restock day). Under the old model the same August produced
two losing days, both driven purely by cost spikes and neither with anything to
say about itself.

### Tests

New `internal/platformconnector/seed_test.go` asserts **properties, never golden
numbers for specific dates** — deliberately, because the salt's own contract is
that tuning the model changes every number, so a test pinned to "2026-08-18
produces 22 orders" would be rewritten on every tune, and a test rewritten to
match new output proves nothing. What is asserted: the weight table is a
complete partition, every non-ordinary condition carries a label and the
ordinary one carries none, the draw is deterministic and platform-independent,
weekday eligibility holds (no midweek lull on a Saturday), a bad day is a small
day and never an empty one (zero delivery records is how `internal/reconcile`
says "I have no data" — a simulated storm must not be able to impersonate a gap
in the data), the refund multiplier is capped, and a simulated year contains
both real losses and real healthy days at a plausible rate against a lumpy
five-day cost cycle derived from the live cost sheet's measured shape.

The existing connector tests needed **no number updates at all** — they were
already written against structure (dedup patterns over hand-built records, wire
formats, contract checks) rather than against generated amounts, which is why a
deliberate regeneration cost nothing. `ConnectedPlatformsTab.test.tsx`'s
`PREVIEW_RESPONSE` is a hand-authored API mock, independent of Go generation, so
it was extended with `trading_note` rather than corrected.

New coverage for part 1: `costSheetDateRange` across out-of-order rows and the
single-day degenerate case; `wantsConnectorSync` off unless explicitly asked;
the over-wide-range refusal happening before any disk or database work (proved
by passing a nil store — reaching it would panic); the no-connectors-wired
refusal; and four frontend tests covering the pre-ticked box, its wording, the
`FormData` field actually sent, declining it, and the post-commit restatement
including unresolved overlaps.

### Docs

`docs/openapi.yaml` gains the `sync_connectors` request field, `covers_from` /
`covers_to` / `connector_sync` on `CommitCostSheetResponse`, a shared
`ConnectorSyncSummary` schema (referenced by both endpoints that report a sync,
so the disclosure fields cannot drift between them), `trading_note` on
`ConnectorDayTotals`, and the `connector_fetch_failed` 422 with its real
message. `docs/api.html`'s embedded spec was regenerated from it — which also
picked up `GET /api/sources`, present in the YAML since spec 013 landed but
missing from the checked-in HTML. That drift predates this branch; it is named
here rather than folded in silently.

**Green:** `go build ./... && go vet ./... && go test ./...` clean (with a live
Postgres and a fully ingested dataset, so the DB-backed tests actually run
rather than skip); `go test -race ./internal/platformconnector/` clean;
`tsc -b --noEmit` clean; 613 frontend tests passing across 50 files; `vite build`
clean. `npm run lint` still reports 6 pre-existing errors in files this change
does not touch (`useTableFilter.ts`, `AppShell.tsx`, `SplashScreen.tsx`,
`QuestionComposer.tsx`, `CompositionPieChart.tsx`) — they were failing before
this branch and were left alone rather than mixed into an unrelated change.

---

## 2026-08-30 — A named BFF boundary: the API surface becomes data, and the CORS bug class ends

`specs/013-bff-layer/`. Requested as "unify the main backend with the
platform-connector proxy behind one coherent API surface". **The first finding
contradicted the request, and is the most useful thing in this entry: there
were never two backends.** `backend/internal/platformconnector` is an ordinary
in-process Go package — same binary, same `http.ServeMux`, same origin, same
`/api/*` prefix. `frontend/src/lib/api.ts` has had exactly one `API_BASE` since
spec 001. So no service was introduced, no network hop was added, and no new
deployable exists. What was added is the boundary discipline that was genuinely
missing.

**The real defect, with a real prior incident.** Seventeen routes were
registered by hand in `cmd/server/main.go`, and `withDevCORS` advertised
`Access-Control-Allow-Methods` as one hand-maintained string literal for the
whole mux. The two had to stay in sync from memory, and once did not: `PUT
/api/profile` shipped broken from the browser because the literal read
`"GET, POST, OPTIONS"`. The failure mode is why it survived — a blocked CORS
preflight produces no 405 to see in the browser, and `curl -X PUT` against the
same handler succeeded every time. No test could catch it either: `main` is not
importable, so nothing could enumerate the surface to compare the two lists.
The 2026-08-29 fix pinned the literal with a regression test, which was correct
for the incident and wrong for the defect — it asserted that today's string
contained today's four methods, so it could not fail for route eighteen.

**What changed.** New `backend/internal/bff` package: the composition root.

- **The route table is data**, keyed by HTTP method (`map[string]http.HandlerFunc`,
  not a `Methods []string` beside one handler). That shape is load-bearing — the
  methods a route *advertises* and the methods it can *dispatch* are the same map
  keys, so they are one fact and cannot drift. Preflight, 405 policy and startup
  log are all derived from it.
- **CORS is per route, and strictly tighter.** On `main` every route advertised
  the union `GET, POST, PUT, OPTIONS`; `GET /api/reconciliation` told the browser
  it accepted `PUT`. Verified live: `/api/reconciliation` → `GET, OPTIONS`;
  `/api/usage` → `OPTIONS, POST`; `/api/profile` → `GET, OPTIONS, PUT`.
- **`methodSplit` deleted.** It routed `POST` to the create handler and *every
  other verb* to the listing handler, so a `DELETE /api/promotions` was answered
  by whichever handler happened to be the fallback. Now: `405` with
  `Allow: GET, OPTIONS, POST` and the standard envelope, from the router, before
  any handler runs.
- **Panics become responses.** `net/http` recovers a handler panic per connection
  so the process survives, but the client got a *closed socket*, not the
  `{error, detail}` envelope. (`lib/api.ts`'s `toApiError` has been coding that
  case `unknown_error` all along — the browser was handling a failure the backend
  never converted.) Now a 500 with a fixed detail string; the panic value is
  logged, never written to the body.
- **`main()` contains no `mux.HandleFunc` call.** It parses flags, connects,
  builds `bff.Deps`, and listens. 120 lines of interleaved wiring and prose moved
  to a table that a test can read.
- **`GET /api/sources` (new).** `connector_sync.go`'s own doc comment admits the
  two ingestion families are "the same job"; they were nonetheless two URL
  prefixes with two response vocabularies, which is why `UploadPage.tsx` is one
  page whose two tabs were written against two different API idioms. The two
  concerns were never "backend" and "connector" — they were *upload* and
  *connect*. One list now covers all four sources with a `kind` field
  (`file_upload` | `connector`) that keeps the vocabulary uniform without
  pretending the arrival difference away.

**One honesty property defended.** A uniform list is exactly the shape that
tempts hoisting `simulated: true` and the emulation notice onto the envelope,
where they read more cleanly — which would make the disclosure a *sixth* place
it can be cropped from (a screenshot of one row; a client rendering one entry).
Both are per source, pinned by
`TestEverySimulatedSourceCarriesItsOwnNotice`. Symmetrically, the supplier cost
sheet is *not* marked simulated — `TestTheCostSheetIsNotMarkedSimulated` —
because claiming emulation where there is none devalues the claim where there
is. The notice string itself is now exported from `internal/httpapi`
(`SimulationNotice`) and consumed by `internal/bff` rather than restated: two
wordings of one disclosure is how a disclosure quietly weakens.

**Test replaced, not just moved.** `cmd/server/main_test.go` is deleted; its two
tests are superseded in `internal/bff` by tests that read the *real* route
table — `TestEveryRouteAdvertisesExactlyWhatItServes` walks all 17 routes and
fails when any route's declared and advertised methods disagree, including
routes that do not exist yet. `TestProfileRouteStillAdvertisesPUT` keeps the
original incident findable by name. `TestSurfaceIsUnchangedFromMain` pins the 16
pre-existing paths as a snapshot so the refactor cannot silently drop one.

**Deliberately NOT done, each for a stated reason:** no separate deployable (one
experience, one consumer, one team — Azure's explicit non-fit; the modular shape
is the documented right answer until independent scaling or cadence is
exercised); no aggregate page endpoint (worst fan-out in the app is *two*
localhost calls, and `HomePage` already holds independent per-call error state
and renders correctly when one fails — composing them server-side would move
working degradation somewhere it would have to be rebuilt); no retries, circuit
breakers, bulkheads or hedging (that spine assumes a *network* upstream; this
one is a function call in the same process, and `docs/architecture.html` already
rejected it as "fiction stacked on fiction" — that decision stands); no write-path
renames (the logical end of the argument, and a breaking change to two working
components days before a deadline — recorded as knowingly unfinished); no MCP
change at all (`internal/mcptools`: zero-line diff).

**Constitutional collision, recorded.** The BFF pattern's partial-failure ladder
permits degrading a failed section to a "static or safe default". This
constitution forbids that rung outright for any numeric section. The pattern was
adopted with it removed — which is a second, independent reason no aggregation
endpoint was added, since aggregation is where the temptation to reach for it
lives.

**Verification** (isolated Postgres on :15433, seeded via `cmd/gendata` +
`-ingest` + `-ingest-promo`: 759 days reconciled, period total margin
$1,078,340.64, 29 promotions):

- `go build ./...` clean; `go vet ./...` clean; `go test ./...` — **19 packages
  green** (18 before, plus the new `internal/bff`; `cmd/server` moves to "no test
  files" as its coverage relocated to `internal/bff` and grew from 2 test
  functions to 19, running 40 cases including subtests).
- Frontend `tsc -b --noEmit` clean; `vitest run` — **50 files / 608 tests
  passing**, unchanged from the pre-refactor baseline measured on the same
  worktree. Zero frontend files changed.
- Live smoke against a real server: `GET /api/reconciliation` → 200, 759 days,
  `2024-08-01..2026-08-29`; per-route preflights as listed above; `DELETE
  /api/promotions` → 405 + `Allow`; `Origin: https://evil.example` not reflected;
  `GET /api/sources` → 4 sources, 3 simulated each with its own notice, cost sheet
  with neither.
- Pre-existing `gofmt` findings in 4 unrelated files (`internal/money/money_test.go`
  and three others) were verified as already unformatted on `main` and left alone;
  every file this change touches is `gofmt`-clean.

Docs updated: `CLAUDE.md` (Stack section now names the three layers),
`docs/architecture.html` (dependency table row + a "why it is not a service"
section), `docs/prd.md` (section 13), `docs/openapi.yaml` (`/api/sources` path +
`Sources` tag; 17 paths, matching the 17 routes).

---

## 2026-08-30 — Excel-style per-column header filters, and the search boxes that stopped filtering on every keystroke

Two related, additive changes to how a grid gets narrowed, both explicitly
scoped by the `dataviz`/`ux-writing` skills rather than added everywhere on
reflex.

**Per-column header filters.** Every existing `useTableFilter` grid
(`PromotionsPage`, `PlatformsPage`, `HomePage`, `PointsPage`) already had its
own filter bar (search box, dropdown, chips) above the table — the product
owner asked for a SECOND, additive filtering surface: a small filter icon in
a column header, Excel/Sheets-style, opening a checklist/text/range popup
scoped to just that column, composing with (never replacing) the filter bar.

Surveyed every `<table>` under `frontend/src/components/**`, not just the
four `useTableFilter` pages, and applied real judgment rather than adding
the affordance everywhere:

- **`CostSheetTab.tsx`** and **`ConnectedPlatformsTab.tsx`** (both
  `/upload`) — INCLUDED. Both render `DataGrid` as a standalone
  preview-before-commit table (no accompanying chart to stay in sync with,
  unlike every other `DataGrid`/chart-table caller below), with real scale
  (a real supplier cost sheet can run to dozens of line items; up to 31 days
  times every connected platform can interleave into 60+ preview rows) and a
  genuine categorical dimension worth narrowing by (Supplier/Category;
  Platform). `CostSheetTab` demonstrates the text and categorical filter
  types (Invoice ID; Supplier, Category); `ConnectedPlatformsTab`
  demonstrates categorical and numeric range (Source; Orders).
- **`HomePage`'s "Recent closes"** — EXCLUDED. Capped at 7 rows
  (`RECENT_CLOSE_ROWS`), and its one categorical dimension (Status: clean/
  flagged) is already a 2-value toggle sitting directly above the table as
  visible chips — a column checklist for the same 2 values one line down
  adds a second control for no new narrowing power.
- **`PlatformsPage`'s side-by-side `DataGrid`** — EXCLUDED. Row count is
  bounded by how many delivery platforms this restaurant has on file (2
  today), the platform name IS each row's identity (already the page's own
  search box), and `DataGrid`'s own doc comment is explicit that it's
  "deliberately plain: no sorting, no filtering... every interactive
  affordance added here would be a control the reader has to understand
  before trusting the number" — true here as much as anywhere else it's
  quoted below.
- **`PointsPage`'s rules table** (5 fixed rows: Clean Close, Discrepancy
  Catcher, Growth, Week One, Campaign Launcher) — EXCLUDED, too small and
  non-growing, no real categorical dimension (the rule name IS the row).
  **`PointsPage`'s redemption history** — EXCLUDED, it's a `<ul>` of flex
  rows, not a `<table>` with column headers to hang the affordance off, and
  its one categorical dimension (platform) is already covered by the
  existing filter bar's dropdown.
- **Every "View as table" chart fallback** (`MarginTrendChart`,
  `CategoryBarChart`, `CompositionPieChart`, `EffectiveRateTrendChart`,
  `PromoRoiChart`'s embedded table) and **`DataGrid` inside chat's
  `AnswerVisualizationView`** — EXCLUDED as one consistent class. These are
  accessibility-parity twins of a chart or a compact answer citation, not
  independent explorable grids; filtering a fallback table without also
  filtering the chart it's supposed to mirror would let the two disagree
  about what's on screen — the exact "two renderings of one number that can
  drift" failure this product's provenance discipline exists to prevent —
  and chat's `DataGrid` renders "a handful of rows scoped to one answer"
  by explicit design.

Built as one reusable pair, not a bespoke implementation per table:
`frontend/src/lib/useColumnFilters.ts` (categorical/text/numeric filter
state and matching, operating on `DataGrid`'s own `columns: string[]` /
`rows: string[][]` shape) and `frontend/src/components/ui/column-filter.tsx`
(`ColumnFilterButton`, the header trigger + popover, built on Radix
`Popover` — already a project dependency via `ui/tooltip.tsx`, not a new
one — for the accessible-popover plumbing: focus into the panel on open,
Escape closes and returns focus to the trigger, click-outside closes).
`DataGrid.tsx` takes an opt-in `columnFilters` prop keyed by column index;
every caller that omits it (chat, `PlatformsPage`) renders exactly as
before. State is local `useState`, not URL-synced like `useTableFilter` —
both wired-in tables are one-shot preview-before-commit steps with no route
of their own and no existing synced-state precedent to extend (reloading
`/upload` mid-preview already discards the staged `File` regardless of what
a URL remembers), so persisting column-filter choices there would promise
something this flow can't keep. A future caller that already syncs its
`useTableFilter` state to the URL should extend that same discipline to its
own column filters rather than copy this local-state approach as-is.

The active-column indicator is never color alone: the trigger's
`aria-label` states "active" plus a plain-language summary ("2 values
selected", a quoted query, or the numeric bound), and a small dot renders
beside the icon in addition to the color change.

**Search boxes now apply on Enter/click, not on every keystroke.** Reported
by the product owner: `FilterSearchInput` (the shared search box behind
"Search campaigns", "Search redemption history", "Search platforms", and
Home's "Search recent closes by date") narrowed the grid on every
keystroke. Changed to require an explicit action — Enter, or clicking the
search icon (now a real button) — deliberately NOT a debounce, which would
still apply automatically, only delayed, and specifically not what was
asked for. The input's own visible text still updates every keystroke
(local `draft` state); only the actual `onChange` call that narrows the
grid is deferred. `draft` re-syncs from the applied `value` whenever it
changes from outside the user's own typing (Clear filters, a browser
back/forward restoring a different search from the URL) via React's
render-time state-adjustment pattern (compare against the last-seen applied
value during render) rather than a `useEffect` — an effect here would both
cost an extra render on every apply and, per this repo's now-enabled
`react-hooks/set-state-in-effect` lint rule, is the wrong tool for
"reset local state when a prop changes" in the first place. The same
discipline was applied to the new column-header text/numeric filters for
consistency; a column's checklist filter still applies immediately per
checkbox, same reasoning as the existing status/ROI-sign chips (a single
discrete choice doesn't warrant a confirm step).

Existing tests for the four search boxes asserted immediate `onChange` on
`userEvent.type` — updated (`HomePage.test.tsx`, `PlatformsPage.test.tsx`,
`PointsPage.test.tsx`, `PromotionsPage.test.tsx`) to press Enter before
asserting the narrowed result, alongside `filter-bar.test.tsx`'s new
explicit cases: typing alone doesn't apply, Enter applies, clicking the
search button applies, and the input re-syncs when the applied value
changes externally. New `useColumnFilters.test.ts` (13 cases: options in
first-seen order, AND-composition across columns, numeric parsing of
formatted currency cells with an unparseable cell excluded rather than
guessed, clear-one vs. clear-all) and `DataGrid.test.tsx` (opens, filters,
clears, keyboard-reaches-and-opens-via-Enter, empty state) cover the
reusable pieces directly; `CostSheetTab.test.tsx` and
`ConnectedPlatformsTab.test.tsx` each gained one integration case proving a
column filter narrows the real preview table.

Verified live against the shared backend (`:8080`, untouched) on a frontend
dev server on `:5273`: uploaded a real 5-row cost sheet CSV and applied a
Supplier checklist filter (5 rows → 2), previewed a real 347-row simulated
connector sync and applied a Platform checklist filter (14 preview rows → 7)
and an Orders numeric-range filter, and typed an unmatched search into
Promotions' "Search campaigns" (30 campaigns still shown, unapplied) then
pressed Enter (0 of 30 shown, the real "No campaigns match these filters"
empty state).

## 2026-08-30 — POS connector, and the cross-source duplicate it creates (spec 012)

`specs/012-pos-connector-dedup/`. Two changes that had to ship together,
because the first one is a bug without the second.

**What was missing.** Spec 010 built the connector for delivery revenue and
deliberately stopped there — its own Assumptions say "There is no simulated
POS API." That left the in-house POS, **two thirds** of this restaurant's
gross sales (`cmd/gendata`: `posShare` 0.66 against 0.17 + 0.17), reachable
only by somebody exporting `pos_export.csv` by hand. The connector story was
two thirds unfinished on the revenue side, and it was the larger two thirds.

**The bug adding it would have introduced.** Today's reconciliation treats POS
and delivery revenue as two disjoint, additive buckets, and has never had to
ask whether a row in one describes the same real-world order as a row in the
other. That assumption is wrong in a specific, well-known way: a POS that
integrates with a delivery aggregator — which is why front-of-house sees one
screen and one kitchen printer — has the aggregator's orders *pushed into it*,
where they become POS tickets with their own POS order numbers. The same order
then arrives twice in one sync, and summing both inflates gross sales,
understates the cost ratio, and reports a margin percentage better than
reality every single day.

Measured on this build, 2026-08-18..20: naively summing the three sources
gives **$14,285.89** of gross sales against a true **$12,442.88** — $1,843.01
counted twice, and a three-day margin overstated by **20.3%** ($10,941.64
instead of $9,098.63). Shipping a POS connector without deduplication would
have made this product's headline number *worse* than before the feature
existed.

**The failure in the other direction, which drove the design.** A matcher that
is too eager merges two genuinely different orders that happen to share a
price and a rough time. On a 20-to-30-order evening at a $32 mean ticket that
is not exotic, it is expected — and the result is real revenue *deleted* from
the day, unrecoverable from the reconciliation output, and invisible, because
the day is simply lower and nothing says why. Dropping a real order and
double-counting a duplicate one are the same financial-integrity failure
wearing different signs. `CLAUDE.md`'s "a confidently wrong margin figure is
worse than a refusal" cuts both ways here, and the design spends its effort on
the second direction.

**The rule** (`internal/platformconnector/dedup.go`), stated so it can be
argued with rather than only read:

> A POS ticket is a duplicate of a delivery order **only if the POS itself
> said** the ticket arrived through a delivery channel. Within that set: if the
> ticket carries the platform's order reference and that reference resolves,
> they are the same order. Otherwise they are the same order only if they share
> a platform, a calendar date and an **exact amount in cents**, their times are
> within **15 minutes**, and no other reading of the day's tickets is equally
> consistent.

Three decisions inside that, each deliberate:

- **No matching on amount and time alone.** Without an assertion from one of
  the two systems that a delivery channel was involved, the evidence is "these
  numbers are similar", and acting on it deletes revenue. An untagged dine-in
  ticket is ineligible at any amount, at any time, forever — an accepted false
  negative, taken so the false-positive rate can be bounded.
- **Ambiguity is disclosed, never resolved by preference.** One ticket with two
  candidate orders, or two tickets contesting one: nothing merges, every record
  survives, and the day carries a flag naming the candidates. The pairing must
  be the unique solution *from both directions*, which is also what makes the
  result independent of iteration order — a rule that merged as it walked would
  hand the order to whichever ticket came first and leave the other looking
  cleanly unmatched, which is a coin flip presented as an answer
  (`TestDedup_IsIndependentOfInputOrder`).
- **The delivery record wins a resolved duplicate.** It knows the commission,
  the rate, the payout and the refund state; the POS ticket knows only a gross
  amount. Dropping the delivery side would zero that order's commission and
  move margin *up* — a wrong number in the flattering direction, which is the
  worst shape an error in this product can take.

Zero model involvement, and this is the part worth pointing at: "are these two
records the same order" is exactly the shape of problem a language model gets
reached for, and exactly the shape this product refuses to hand one. The
matcher is integer-cent equality, case-folded string equality, and a minute
difference. Every decision can be recomputed by hand from the two records
(Constitution Principle I).

**The mock is built to make the rule work for its living.**
`internal/platformconnector/pos_mock.go` is a third wire format that disagrees
with both delivery mocks on every decision either of them makes: NDJSON with no
envelope and no pagination at all, money in pt-BR notation (`"1.234,56"`),
zone-less local timestamps, `PAID`/`VOID`, a `service_type`, and a nested
`delivery_partner` block. Two of those are traps with teeth, in the same spirit
as spec 010's derived JET rate and iFood refund sign:

- `money.ParseCents` reads `"1.234,56"` as **$1.23** — a plausible-looking
  string understated by three orders of magnitude, with no error anywhere.
  `normalizePtBRAmount` converts explicitly and *refuses* anything outside the
  one accepted shape rather than best-effort parsing it.
- `time.Parse` on a zone-less timestamp yields UTC. The calendar *date* still
  comes out right for every ticket this mock emits — which is what makes it
  dangerous, because nothing downstream would look wrong. What it destroys is
  the merchant's three-hour offset in every ticket *time*, which is the input to
  the matching window: every amount-and-time match in the product would stop
  firing, duplicates would flow through, and gross sales would quietly inflate.
  `time.ParseInLocation` is the fix, and `checkPOSContract` enforces it on every
  fetch by requiring the parsed instant to agree with the ticket's own recorded
  wall clock.

The overlap itself is causally real, not arranged: for a given date the POS mock
calls the *same* `simulateDay(PlatformIFood, …)` the iFood mock calls and echoes
those actual orders, so a duplicate the matcher finds is one simulated order
recorded twice. Around it, the deliberate difficulties: only iFood is integrated
into the POS and Just Eat Takeaway is not (a common real configuration, and the
one that puts a control group inside every fetch); a quarter of echoed tickets
carry no partner reference, so the harder tier is not decoration; and
campaign-discounted orders disagree on amount because the POS rang the menu price
and never saw the promotion. Ambiguity is **not** manufactured — the mock does not
arrange a collision so a flag has something to do; that path is proven by unit
test, and its real incidence is reported as measured.

**Measured, not claimed.** Over August 2026, all 31 days, three sources: **689
duplicates resolved, 40 overlaps left unresolved and flagged, and 2,569 in-house
tickets — every one of them intact.** Zero false positives is
`TestFetchRange_NoInHouseTicketIsEverRemoved`, running on every build against the
real generated mix, not an assertion in a document.

**Nothing is silently corrected.** Three new flag types in `internal/reconcile`'s
existing vocabulary — `cross_source_duplicate_removed`,
`cross_source_duplicate_unresolved`, `cross_source_amount_mismatch` — each naming
both sides: which ticket was removed, which order it merged into, at which row of
which source. An unresolved overlap states the consequence out loud ("this day may
count that order twice"). The connected-sources panel shows the removals and the
unresolved overlaps side by side, because reporting only the removals would let the
product claim a clean close it did not achieve.

**Shape changes, each with a compatibility path so nothing existing moved.**
`ComputeDailyReconciliations` became a nil-map delegate to a new
`ComputeDailyReconciliationsWithFlags`, proven byte-identical against the real
dataset (`TestComputeDailyReconciliations_MatchesTheNilFlagDelegate`);
`RunIngestionPipelineWithDeliveryOverlay` became a delegate to a new
`RunIngestionPipelineWithConnectorOverlay`. That overlay carries `DeliveryActive`
and `POSActive` booleans rather than relying on a nil slice, because "I synced the
POS and it reported nothing" and "I did not sync the POS" must produce different
days — without the boolean, a delivery-only sync would have silently wiped two
thirds of every synced day's gross sales.

**Rejected, and recorded because the next reader will have the same idea:** making
the POS mock implement the existing `Client` interface and return
`ingest.DeliveryRecord` values with a zero commission. `reconcile.computeOneDay`
sums `commissionsBySource[src]` over every delivery record, so the day would grow a
`commissionsBySource["pos"] = 0` entry — breaking the invariant
`internal/reconcile/types.go` documents on both `CommissionsBySource` and
`RefundsBySource` ("pos" never appears there), and making the POS show up in
specs/003's `compare_platform_economics` as a delivery platform charging 0%
commission: a new, wrong answer in a corner of the product that never mentions
connectors. A peer `POSClient` interface instead, sharing a `Connector` half with
`Client`. The reasoning is in `client.go`'s own doc comment, not only here.

**Disclosure held to spec 010's bar exactly** — five independent statements for the
POS as for the two platforms: the package doc, the enforced `simulated://`
provenance scheme, `"simulated": true` in every response body, the per-source UI row,
and the persistent non-dismissible panel notice. The POS's commission chip reads "No
commission" rather than "0.00%", because a zero rate renders as a platform that
happens to be free, which is a different and wrong claim.

Live end-to-end on an isolated instance (own Postgres, own ports): three-source sync
of 2026-08-18..20 committed 121 delivery orders and 198 POS tickets, removed 50
duplicates, disclosed 5 unresolved overlaps, and moved total margin from
$1,078,340.64 to $1,079,774.22 across 759 days. Re-running the identical sync
produced the identical margin, the identical 50 removals and the identical 5
unresolved overlaps — the determinism the flags depend on, since a re-synced day has
to carry byte-identical discrepancy flags to reconcile to the same numbers.

## 2026-08-30 — Promotions chart: newest campaigns no longer scroll out of view unnoticed

Reported by the product owner: "not all campaigns are in the chart, add all
of them and show from the right to the left side." Investigated live against
the real dataset (30 campaigns on file, backend on `:8080`) with Playwright
screenshots at several viewport widths before changing anything, per this
project's own discipline of tracing root cause instead of assuming one.

**This was a discoverability gap, not a data-loss bug.** Every campaign was
already reaching `PromoRoiChart` as a real, focusable bar —
`PromotionsPage.tsx`'s `displayedPromotions` never slices or caps the list
(confirmed by the existing `PromotionsPage.test.tsx` regression test
guarding exactly this at 30-campaign scale, which was already passing), and
`PromoRoiChart.tsx`'s own `chartableData = data` has carried every record
unfiltered since an earlier QA fix. Screenshots proved it: at a wide
viewport (1440px) all 30 bars, including the two "Unattributable" refusals,
rendered and fit with no scrolling. At a common laptop width (1024px) all
30 bars were STILL in the DOM (`aria-label="...across 30 promotion
campaigns"` matched the header's "30 campaigns" chip exactly) — but the
chart's `overflow-x-auto` wrapper defaulted to its scrolled-to-the-LEFT
position, showing only the OLDEST campaigns, with no visual cue that more
existed off-screen to the right. The owner's reading ("not all campaigns are
in the chart") was a fair one: the newest, most-actionable campaigns — the
ones a "needs a decision" reader most wants — were also the ones most likely
scrolled out of view on first load.

**Fix, two parts, both scoped to `PromoRoiChart.tsx`:**

1. **Default scroll position.** The chart's own data order is chronological,
   oldest-first (the API's natural order — see `toChartDatum`'s neighboring
   comment in `PromotionsPage.tsx`), the same left-to-right time convention
   `MarginTrendChart` already uses. That chart already mounts scrolled to
   its own right edge for the identical reason ("today first, history a
   deliberate scroll away") — this applies the same fix here via a new
   `initialScrollToEnd` prop (default `true`), rather than reversing the
   chronological axis itself, which would read backwards against every
   other time-ordered chart in this app and the dataviz skill's own
   convention. The one case that opts out: `PromotionsPage`'s ROI sort
   toggle ("Highest first"/"Lowest first") already puts the campaign the
   owner asked to see first at the left — auto-scrolling right there would
   hide it, so `PromotionsPage` passes `initialScrollToEnd={roiSortDirection
   === null}`.
2. **Scroll-fade affordance.** Reused this codebase's one existing answer to
   "an `overflow-x-auto` row gives no visual reason to suspect there's
   more" — `Shell/Sidebar.tsx`'s `MobileNavBar` edge fade, fixed earlier
   this week for the identical problem on the mobile nav bar. Added the same
   pattern to `PromoRoiChart`, bidirectionally: a left fade shown only while
   scrolled away from the start (real history sits further left) and a
   right fade shown only while not scrolled all the way to the end — never
   a permanent decoration in either direction.

Interpreted "show from the right to the left side" as: start the reader at
the right (the newest campaigns), with older history a deliberate scroll to
the left — not a request to reverse the chronological axis itself (which
would contradict "recent reads as rightmost," this app's own
`MarginTrendChart` precedent, and standard time-series chart convention).

Verified live: at 1024px the chart now mounts showing campaigns through the
newest (`IFOOD_CAMP_02`, period ending 2026-09-30) with a left fade
indicating older history; scrolling to the start flips to a right fade with
no left fade. Header chip, chart `aria-label`, and table row count agree on
"30" in every case — the chart and the table never disagree about how many
campaigns exist. Added 9 tests to `PromoRoiChart.test.tsx`: an explicit
30-campaign bar-count assertion pinned to the real dataset's own scale, four
covering the default-scroll-to-end behavior (including that it does NOT
fire when `initialScrollToEnd` is false, and that it doesn't fight a
reader's manual scroll on an unrelated re-render), and four covering the
fade's visibility in each direction. `npx tsc -b --noEmit` and the full
frontend suite (579 tests) pass.

## 2026-08-30 — Close's Period totals no longer truncate to a plausible-looking wrong number

Reported live: filtering Today's Close to a period showed a total margin cut
off with an ellipsis (e.g. "$1,078,9…"). Root cause: `components/ui/stat.tsx`'s
`Stat` value span used `truncate` (single-line, ellipsis-on-overflow) — fine
for a single day's smaller figures, but a period total (summed across many
days, sometimes into the millions) is a longer string than the stat grid's
column width assumes. A truncated dollar amount reads as a plausible but
DIFFERENT, wrong number — the same class of error this product refuses to
show anywhere else (a confidently wrong figure is worse than an ugly one).
Fixed the same way the label above it was already fixed for the identical
reason (a prior live report about labels like "Days with a fl…"): wrap
instead of hiding. Verified live with a 760-day period producing a
$2,347,140.30 total and a $1,690,002.61 per-source figure — both now render
in full. Audited every other `truncate` usage in the frontend: all remaining
instances are on text labels (a restaurant name, a source name, a chart's
row label) with the complete text available via a tooltip/title attribute —
a legitimate, different use of truncation, left unchanged.

## 2026-08-30 — Owner-facing refusal wording, and the follow-up that triggered it

Reported live: a follow-up like "how can I replicate it on other days?"
(asked right after an answer that already stated a day's margin) came back
refused with `"model stated a currency-shaped figure without making any
MCP tool call or collecting any provenance — refusing rather than trusting
a number that cannot trace to the deterministic layer"` — raw internal
debugging language ("MCP", "provenance", "the deterministic layer")
verbatim in the restaurant owner's chat.

Traced to `internal/explain/explain.go`'s Finding-13 guard: a correct,
intentional check (Constitution Principle I) that refuses any final answer
which states a currency-shaped figure without this interaction having made
a single MCP tool call — a number that cannot trace to a tool result cannot
be trusted, whatever the model claims. The check itself was right; two
things around it were not.

**Root cause, not just wording:** the same-day "mixed data-plus-advice"
fix's new instruction ("answer the data-answerable part in full first")
had no guidance for a *follow-up* question. Given an earlier turn's answer
in context, the model satisfied "answer in full" by restating the figure
it already saw in that earlier text — genuinely correct, but with zero
tool calls in *this* interaction — which the guard (correctly) cannot
distinguish from a hallucinated number, and refused. Live reproduction
before the fix: 16 of 17 tries of a "replicate this on other days"-style
follow-up hit this refusal. Added an explicit rule to
`explain.go`'s system prompt: an earlier turn's figure is background only;
answering a follow-up's data-answerable core still requires calling the
relevant tool again in *this* turn. Re-verified after the fix: 0 of 17
identical tries refused (plus 3 additional adversarial phrasings
explicitly asking the model not to re-check — still 0 refusals), with the
model now re-calling `get_daily_summary` on the follow-up as intended.

**Wording, for whenever the guard still legitimately fires:** rewrote the
refusal text itself to the same plain, owner-facing voice already used
elsewhere (`internal/ambiguity/daterange.go`'s `precheckRefusalReason`,
`gate.go`'s writer pass) — what happened, why, what to do next, no
internal component names. New test
(`TestExplain_ZeroToolCallCurrencyAnswerIsRefused` in
`explain_internal_test.go`) asserts the message never contains "MCP",
"tool call", "provenance", "deterministic layer", or "currency-shaped".

## 2026-08-30 — Inline Grounded Advice: the advisor now answers the questions that ask for it (spec 011)

The product owner's direction, verbatim: "the advisor should advise
whatever the customer asks and use the data in context for it — not
bringing wrong data or hallucination, but using an advisor that gets all
the rich data we have and brings suggestions is something of value to the
product strategy and vision." Until now the Business Insight Advisor
(spec 009) could only be reached one way: Go detected one of five fixed
patterns in an answer's data and offered a teaser chip. A question that
*explicitly asked* for a suggestion — "how can I improve my margin
overall?", "should I focus more on delivery or dine-in?" — got its data
core answered and its advice part plainly declined (the 2026-08-30
mixed-question fix below), even though an advisor with exactly the right
grounding discipline was sitting one package away.

`specs/011-inline-grounded-advice/` adds a second avenue into the same
advisor, additive by construction — the five-kind teaser path is untouched
and live-verified unchanged:

- **Trigger** (`internal/ambiguity`): the gate now reports a separate
  boolean, `advice_requested`, alongside its three-way classification —
  never a fourth classification, and structurally unsettable by the
  second-pass prose writer (`writerResponse` has no such field), the same
  guarantee the classification itself already had. Groundable advice
  questions classify answerable + flagged; ungroundable ones ("what should
  I pay my staff?") stay refused exactly as before, verified live.
- **Grounding**: no new tool-calling loop. The normal narration answers
  the data core first through the existing budgeted MCP loop, and the one
  bounded advisor call is grounded exclusively in `ToolInvocations` from
  that same answer. No grounding → no advice call at all.
- **Dynamic prompt** (`internal/advisor/question_advice.go`): the system
  prompt is assembled per-question in plain Go — spec 009's
  non-fabrication rules verbatim, plus researched-practice sections
  selected by the NAMES of the tools that actually ran (new sourced
  content: Restaurant365's prime-cost bands, Kasavana & Smith's 1982 menu
  engineering matrix, Toast/ChowNow direct-channel steering; unverifiable
  vendor figures deliberately excluded, same as 009's absent "~52%"
  claim). The five 009 kind templates are never consulted on this path.
- **Cost honesty**: every inline call writes a `business_insight_interaction`
  row (new kind `question_advice`, migration 000013 — the "migration plus
  a reviewed prompt" cost migration 000010's comment prescribed) and
  appears as its own `interactions` entry; a failed advice call degrades
  to the unchanged data answer, never a failed request.
- **UI**: the suggestion renders in the teaser chip's dashed-warning
  "AI suggestion" language with the same wire-carried disclaimer, after
  and never blended into the provenance-backed answer.

One real defect found and fixed during live verification, worth naming:
the gate's raw model reply carried `"advice_requested": true` correctly,
and every parser-level unit test passed — but `Classify`'s field-by-field
copy from the parsed decision onto the usage-carrying `Decision` omitted
the new field, silently dropping the signal on every live request. The
symptom (grounded advice questions answered with the old decline, zero
advisor calls) only surfaced against the live instance. Fixed with the
one-line copy plus a new Classify-level scripted-fake test
(`TestClassify_CarriesAdviceRequestedThroughToTheReturnedDecision`) that
fails without it — the parser tests alone provably could not catch this
class of drop.

Live verification against an isolated instance (own Postgres, own port,
real `ANTHROPIC_API_KEY`): "How can I improve my margin overall?" gathered
`get_period_totals`/`get_margin_delta`/`list_negative_roi_promotions` and
returned a suggestion anchored to the real computed figures (July→August
margin move, IFOOD-CAMP-025's own spend/revenue) while stating plainly
that labor/menu-item data doesn't exist in this product; "What should I
pay my staff?" still refused with zero tool calls and zero advisor calls;
the negative-promo teaser + tap flow behaved exactly as before, and
`POST /api/business-insight` rejects kind `question_advice` (400).

## 2026-08-30 — Fixed a chat refusal that promised data it never delivered

Reported live: asking "what should I change about staffing, menu, or
promotions to replicate the margin from Aug 22 on other days?" got flatly
refused, 10/10 tries in a fresh reproduction — even though the question
always has a genuinely data-answerable core (what actually happened on
Aug 22). Worse, the gate's own writing pass produced a polished refusal
that explicitly *promised* that data ("I can show you what drove Aug 22's
margin...") without the system ever delivering it in the same reply,
forcing a separate follow-up question to get an answer the first response
already claimed it could give.

Root cause: `internal/ambiguity/gate.go`'s answerable/ambiguous/unanswerable
classification is whole-question and binary, with no instruction for a
question that mixes a data-answerable core with a request for advice
(staffing, menu, pricing, marketing strategy) this product's tools were
never built to give — so "unanswerable" for the entire thing became a
plausible-but-wrong classification.

Fixed with a new "Mixed data-plus-advice questions" section in the gate's
system prompt (classify "answerable" whenever a data-answerable core
exists, reserving "unanswerable" for questions with no such core at all)
plus a new rule in `internal/explain/explain.go`'s system prompt (always
answer the data-answerable part in full via a real tool call first, then
plainly decline only the advice-shaped part in the same reply — no
hedging, no undelivered promise). `internal/advisor`'s business-insight
teaser was checked and correctly left out of this fix: its five insight
kinds are all negative/problem patterns, none fits "replicate a good day."

Independently re-verified against a fresh, cache-disabled instance with
real API credentials: 5/5 fresh tries of the exact reproducing question
now answer in full followed by an honest, direct decline — the bare
"how can I replicate..." phrasing (no named lever) shows no regression.

## 2026-08-30 — Delivery revenue can now come from the platforms, not just a CSV — with both platforms simulated, said five times over

`specs/010-platform-connector-proxy`. Requested directly: revenue should come
from iFood and Just Eat Takeaway, "but we don't have those APIs nowadays" —
so build a proxy that solves the two different APIs, back it with a mocked
service, and let a reconciliation pull from the proxy instead of an uploaded
file.

**The gap this closes, and the one it does not.** Delivery-platform revenue is
about a third of this restaurant's gross sales (`cmd/gendata`: 17% iFood +
17% Just Eat Takeaway against 66% POS), and until now it could reach the
product exactly one way — a human exports `delivery_platform_export.csv` from
each merchant portal and it gets ingested from disk. For a product whose
entire pitch is "know today's margin today", depending on a file somebody has
to remember to fetch is a real hole. The fix is the platforms' partner APIs,
and this project has credentials for neither and will not get them. So the
connector layer is built for real and only the upstreams are stubbed. The
README, the PRD, and `docs/architecture.html` all still say real platform
integrations are not built, because they are not.

**New package `backend/internal/platformconnector`.** One `Client` interface
(`Platform`, `Describe`, `FetchDeliveryRevenue`), two mock upstreams that emit
raw JSON bytes, two adapters that decode and normalize them, and a `Proxy`
that dispatches, caps, and verifies. Output is `ingest.DeliveryRecord` — the
exact type `ingest.ParseDeliveryExport` already produces — so
`internal/reconcile` has a **zero-line diff** from this feature and cannot
tell a connector-sourced day from a CSV-sourced one.

**The two mocks disagree on everything, deliberately.** A proxy over two APIs
that already agreed would be a rename with extra steps, so: iFood speaks
page-numbered JSON in `snake_case`, money as decimal strings inside nested
`{currency, amount}` objects, RFC 3339 timestamps carrying the merchant's
offset, an explicit `rate_percent`, `CONCLUDED`/`CANCELLED`, and reports a
cancelled order with **positive** amounts plus a separate cancellation block.
Just Eat Takeaway speaks cursor-paginated JSON in `camelCase`, money as
integer minor units, epoch-millisecond timestamps in **UTC**,
`DELIVERED`/`REFUNDED`, **no commission rate at all**, and reports a refund
already **negative**. Each mock marshals to `[]byte` and its adapter
unmarshals it back — the round trip is deliberate, because a mock that handed
back records the adapter merely copied would make the whole exercise vacuous.

**Two conversions carry the actual risk, and both fail silently rather than
loudly.** Just Eat Takeaway reports no rate, but
`ingest.DeliveryRecord.CommissionRateBps` is precisely what
`reconcile.recomputeCommissionCents` independently cross-checks commission
against — derive it wrong and every JET order raises a `commission_mismatch`
flag, burying the real discrepancies this product exists to surface under
integration noise. And the two platforms disagree about the sign of a refund,
so `ifoodAdapter` must negate and `jetAdapter` must not — get that one wrong
and a refund is counted as revenue, so a day's margin goes **up** because
money went out, with nothing anywhere to explain it. There is a test for each.

**The proxy verifies rather than trusts, and refuses rather than corrects.**
Six contract checks run on every record, including records produced by the two
mocks in the same package: platform name matches the bucket it will land in
(a typo here silently opens a third `GrossSalesBySource` key and every
platform-comparison surface keeps working while under-reporting), order date
matches the date requested, commission equals subtotal × rate within one cent
(the same tolerance and the same `money.DivRoundHalfUp`
`reconcile.computeOneDay` uses, so the proxy can never pass a record
reconciliation would then flag), payout equals subtotal minus commission
exactly, a refund is negative and dated while a sale is neither, and
provenance carries the `simulated://` scheme. A failure is a refusal, never a
repair — a proxy that quietly fixed an upstream's numbers would be
estimating, and this product does not estimate money. Also refused: an
inverted range, a range over 31 days, an unregistered platform, an empty
platform list, and **any single platform failing**, because committing iFood
alone for a range would replace that range's delivery revenue with half of it
and drop margin with no flag to explain why.

**Determinism, and why `cmd/gendata`'s own mechanism was wrong here.** Each
`(platform, date)` seeds its own generator from an FNV-64a hash of a fixed
salt, the platform key, and the date. `cmd/gendata` uses one seeded stream
(`randSeed = 20260815`) consumed in file order, which is right for generating
a dataset once, top to bottom. It is wrong for a connector, because a fetch is
random access: an owner may sync one day, or five, or the same day twice, in
either platform order — and with a shared stream what a day returned would
depend on what had been fetched before it, so the same date would reconcile to
a different margin depending on the order someone happened to click in. Same
discipline as `cmd/gendata` ("deterministic — same seed, same dataset, every
regen"), different mechanism, for a stated reason. Scale constants
(23%/20% commission, $32 mean ticket, 2% refund rate) are lifted from
`cmd/gendata`'s own tuned values so a synced day lands next to real dataset
days without standing out.

**Pipeline change: a range-scoped overlay, not a second pipeline.**
`pipeline.RunIngestionPipelineWithDeliveryOverlay` drops CSV delivery rows
inside `[From, To]`, appends the connector's records in their place, and
leaves rows outside the range verbatim. `RunIngestionPipeline` is now a
one-line delegation with a nil overlay, so `-ingest` and the cost-sheet commit
are bit-for-bit unchanged. Range membership is decided on the formatted
`YYYY-MM-DD` key rather than on `time.Time` ordering, because CSV-parsed dates
are UTC midnight while connector dates are midnight in the merchant's own
zone — the same calendar day, three hours apart, and a `Before`/`After`
comparison between them would silently drop a boundary day. Every affected day
is recomputed from all three sources, because margin cannot be updated from
delivery alone without inventing a partial-day figure.

**Rejected alternative:** having the connector write a CSV into
`livedata.Dir` and re-running the plain pipeline. It would have reused more
code, but `pipeline.findSourceFiles` matches one delivery file by filename
keyword, so a connector-written file would either collide with
`delivery_platform_export.csv` or be silently ignored — and normalizing
records only to re-serialize and re-parse them can lose information
(`OrderTime` formatting, note text) for no benefit.

**Three endpoints**, mirroring the cost-sheet upload's preview/commit shape
because it is the same job: `GET /api/connectors/platforms` (static, names
each platform's wire format so the heterogeneity is visible in the product,
not only in the source tree), `POST /api/connectors/sync/preview` (persists
nothing — the handler is never even given the store), and `POST
/api/connectors/sync` (re-fetches from scratch, clears the answer cache
first, runs the overlay pipeline, reports margin before and after). Every
response body carries a top-level `"simulated": true` and the notice text,
because a client that ignores the UI entirely must still not be able to render
these numbers undisclosed.

**A pre-existing lock had to widen.** `HandleCommitCostSheet` held a mutex
scoped to its own closure, correct when it was the only path that wrote
`livedata.Dir` and re-ran the pipeline against it. The connector sync is a
second one — it writes no file but re-runs the same pipeline over the same
directory and reads a before/after margin snapshot around it — so a
cost-sheet commit interleaving with a sync would reproduce exactly the failure
that closure-scoped mutex was introduced to close (each request truthfully
reporting its own inputs while the database reflects the other's), one
endpoint wider. Lifted to a package-level `ingestMu` in `internal/httpapi`,
which also stays correct if a third write path is added.

**The disclosure is redundant on purpose.** A synthetic number presented as a
settled platform payout would be the single most damaging thing this product
could ship, given its stated bar that a confidently wrong margin figure is
worse than a refusal. So it is stated five independent times, any four of
which survive the removal of the fifth: the tab is labeled "Connected
platforms (simulated)" so the word arrives before the panel is opened; a
persistent, non-dismissible notice sits above every control; each platform row
carries its own "Simulated connection" marker so a screenshot cropped past the
banner still discloses; every API response carries `"simulated": true`; and
every record's provenance is a `simulated://ifood-partner-api/...` URI rather
than a plausible-looking filename — *enforced* by contract check 6, not merely
intended. Order IDs read `IFOOD-SIM-…` / `JET-SIM-…` and campaign codes read
`…-SIMULATED-…` for the same reason.

**Frontend.** `/upload` became a two-tab page: the existing cost-sheet flow
moved verbatim into `CostSheetTab.tsx` with no behavioral change, and
`ConnectedPlatformsTab.tsx` is a sibling. Both panels stay mounted with the
inactive one `hidden` rather than unmounting — a staged preview in either tab
is real uncommitted work, and discarding it on a tab click is the exact loss
`useUnsavedChangesGuard` exists to prevent on navigation; `hidden` is also
what the WAI-ARIA tabs pattern asks for, so the accessibility and the
state-preservation fall out of one decision. The tab strip is one tab stop
with arrow-key roving focus. A preview that returns zero orders disables the
sync button and says why: syncing an empty range would *clear* the delivery
revenue on file for those days, not leave them alone.

**Deliberately not built,** so the absences read as decisions: no OAuth, token
refresh, or credential storage (there is no credential — an auth flow against
a fake upstream validates nothing and would misrepresent how far along the
integration is); no retries, backoff, rate limiting, or circuit breaking
(in-process calls do not fail transiently, and simulating flakiness so
resilience code has something to catch is fiction stacked on fiction); no
webhooks or scheduling; no third platform and no plugin registry; no
historical backfill; no persisted raw payloads; and **no new MCP tool** — the
model must not be able to trigger a data-mutating sync.

**Verified.** `go build ./... && go vet ./... && go test ./...` clean against
an isolated Postgres on `:55432` in a dedicated worktree (never the shared dev
database on `:5432`), with `cmd/gendata` + `-ingest` + `-ingest-promo` run
into it first. Frontend `tsc -b --noEmit`, `npm test -- --run` (570 tests, 48
files), and `npm run build` all clean. Exercised end to end against an
isolated backend on `:8099`: syncing 2026-08-18..20 moved 2026-08-18's iFood
gross from $747.37 to $572.54 and its margin from $4,014.71 to $3,947.67, left
POS gross untouched at $2,901.46, produced **zero** discrepancy flags (so both
adapters' commission math — including JET's derived rate — agrees with
`reconcile`'s independent recomputation), wrote 39 `simulated://` provenance
refs alongside 76 CSV ones for that day, and left 2026-08-17 with zero
simulated refs and an unchanged margin. Re-running the identical sync returned
a byte-identical before/after margin of $1,078,977.36, confirming determinism
through the full HTTP → proxy → pipeline → Postgres path.

## 2026-08-30 — Close's Period view no longer opens to a blank state

Requested directly: Period should always show results for whatever range is
currently set, but editing the dates should keep requiring the explicit
"Show results" click rather than auto-fetching on every keystroke.

Previously, switching to Period pre-filled `rangeStart`/`rangeEnd` with a
sensible default (the last week of real data) but never fetched — the owner
always had to click "Show results" once just to see the range the app had
already picked for them. `handleModeChange` now auto-applies whatever range
is current the moment Period is entered (seeding the default first, if the
fields are still empty), so the view never opens to a "choose dates,
then Show results" prompt in the common case. Editing either date field
afterward is unchanged: `handleRangeStartChange`/`handleRangeEndChange`
still only update local state, never fetch, and "Show results" is still
required to apply an edit — the original fix for the two-fields-racing-
each-other bug this page already had is untouched.

Verified live against the running dev server: clicking Period fires exactly
one request for the seeded default range with no further click; editing a
date field fires none; clicking Show results afterward fires exactly one
more, for the edited range.

## 2026-08-30 — Coordinator fix: docs/openapi.yaml was genuinely invalid YAML

Found while independently verifying QA round 9's `/api/profile` addition to
`docs/openapi.yaml`: parsing the full file with a real YAML library (PyYAML,
then `js-yaml`) failed with `expected ',' or '}', but got '?'` — and this
predates round 9, confirmed present on `develop` before that round's changes
too. `Points.spent`'s flow-mapping `description` was unquoted and contained a
literal `:` (`payment_method:points`) and `,`, both structural characters
inside a YAML flow mapping (`{ ... }`). QA round 4 had already found and fixed
the downstream symptom — `docs/api.html`'s checked-in embedded JSON had this
same field corrupted into two garbage `null` properties — without checking
whether the source `docs/openapi.yaml` itself still parsed; it didn't. Quoted
the description and regenerated `docs/api.html`'s embedded `OPENAPI_SPEC` JSON
from the now-valid source. Confirmed the full spec parses cleanly with both
PyYAML and `js-yaml`, and that no other flow-mapping `description` in the file
has the same unquoted-colon/comma shape.

## 2026-08-30 — QA round 9 (final): the interactive API docs were missing a real endpoint

A ninth and final planned overnight QA pass, focused on every OTHER
evaluator-facing doc surface beyond the README Quickstart (round 8 already
fixed and independently re-verified that one): `docs/prd.md`,
`docs/product-strategy.md`, `docs/technical-rfc.md`, `docs/why-ai.md`,
`docs/mcp-and-skills.md`, `docs/openapi.yaml`/`docs/api.html`,
`docs/architecture.html`, `docs/presentation.html`, `.env.example`, every
cross-doc relative link, and `frontend/package.json`'s scripts — plus a
second, deeper fresh-clone dry run that went one step past round 8: after
the Quickstart's fixed command sequence brought up an isolated backend and
Postgres, the frontend was actually loaded in headless Chromium (Playwright)
to confirm the Home page renders real data with zero console/page errors,
not just that the backend process stayed up. Run against an isolated backend
(`:8996`), an isolated frontend (`:8995`), and an ephemeral Postgres
(`docker run` on `:8998`) in a dedicated worktree; the shared
`:8080`/`:5173`/`:5432` instances were never touched.

- **Fixed** `docs/openapi.yaml` and its checked-in, embedded-spec twin
  `docs/api.html`: both were missing `GET`/`PUT /api/profile` entirely —
  a real, non-trivial endpoint (`backend/internal/httpapi/profile.go`,
  optimistic-concurrency-checked, backing the real Profile/Settings page)
  registered in `backend/cmd/server/main.go` alongside every other
  documented route. The README lists "interactive Swagger UI, every
  backend endpoint" as one of only three headline "Live" links at the very
  top of the file — exactly the kind of first-impression surface this round
  was scoped to check — and it was quietly short one real, shipped feature.
  Added a `Profile` tag, the full `GET`/`PUT /api/profile` path (request/
  response bodies, all real status codes: 200/400/405/409/413/500, matching
  the handler's actual behavior) and `ProfileView`/`ProfileRequest` schemas
  to `docs/openapi.yaml`; regenerated `docs/api.html`'s embedded
  `OPENAPI_SPEC` JSON literal from the corrected YAML (via `js-yaml`, since
  the file's existing `\uXXXX`-escaped-non-ASCII style needed matching) so
  the checked-in copy stops drifting from the spec it claims to render.
  Verified the new path/schemas parse and appear correctly in both files.
  The three live Swagger UI/architecture/presentation artifact URLs
  themselves are unaffected by a worktree commit — they need a separate
  republish, left for the final wrap-up pass that already plans one.
- **Fixed** `.env.example`'s `ANTHROPIC_API_KEY` comment, which still said
  the key was "required for the ambiguity gate (Haiku 4.5) and explanation
  step (Sonnet 5)" — the exact model assignment `CLAUDE.md` and `README.md`
  both document as having moved (the gate is Sonnet 5 as of 2026-08-29;
  Haiku 4.5 is now only the paraphrase-match cache classifier, per
  `internal/llmclient/cost.go`). Since `cp .env.example .env` is the very
  first line of the Getting Started block, this was a small but real
  chance for an evaluator's first read of the model architecture to be
  wrong. Corrected to match the current, documented assignment.
- **Audited, no bug found**: every other doc's cross-references and
  claimed file paths (README, `CLAUDE.md`, `docs/*.md`, every `specs/**/*.md`)
  — every relative markdown link resolves, every `backend/`/`frontend/`/
  `docs/`/`specs/`-rooted path named in backticks exists (except
  `backend/data/live/`, which is correctly documented as git-ignored and
  generated on demand). `docs/openapi.yaml`'s `servers:` URL, every port
  number named across README/docs/`evaluation/promptfoo/*.yaml` (`:8080`,
  `:5173`, `:8092`), and the `-eval-no-answer-cache` flag it depends on all
  check out against the real registered routes and real `main.go` flags.
  `frontend/package.json`'s scripts (`dev`/`build`/`lint`/`format`/
  `format:check`/`preview`/`test`/`test:watch`) match every `npm run`/
  `npm test`/`npx tsc`/`npx vite` invocation named anywhere in the docs or
  specs — no stale script reference, no Makefile to drift. Grepped
  `os.Getenv`/`import.meta.env` across both codebases: the only backend var
  (`DATABASE_URL`) and the only optional frontend override
  (`VITE_API_BASE_URL`, defaulted to `http://localhost:8080` and irrelevant
  to the documented setup path) are both accounted for;
  `ANTHROPIC_API_KEY` is resolved by the Anthropic SDK itself, not read
  directly, and is already documented.
- **Audited, no bug found**: a full second fresh-clone dry run one level
  past round 8's. `cp .env.example .env` → `migrate ... up` → `go run
  ./cmd/gendata` → `-ingest` → `-ingest-promo` → `-serve :8996` (isolated
  Postgres on `:8998`) all ran clean exactly as documented, then
  `VITE_API_BASE_URL=http://localhost:8996 npx vite --port 8995
  --strictPort` served the real frontend against it. Playwright loaded
  `http://localhost:8995/` headless: the Home page rendered real,
  correct figures (latest margin `$3,225.06` on 2026-08-29, matching the
  live `GET /api/reconciliation` response byte for byte; 12,345 Steward
  points; the real badge/points breakdown; the real recent-closes table)
  with zero console errors and zero page errors — no error state, no stuck
  loading spinner, no placeholder data.
- No `ANTHROPIC_API_KEY` was available in this sandbox, so the model-backed
  `/api/ask` and `/api/business-insight` paths were not exercised live in
  this round (consistent with every prior round's disclosure) — the Home
  page check above only needed the deterministic REST endpoints, which is
  everything a first-run evaluator sees before ever opening the chat.

This was the last round before final morning wrap-up. Two real, if modest,
documentation-completeness bugs were found and fixed; the deeper fresh-clone
dry run this round was scoped to run (opening the actual frontend against a
real backend, not just confirming the backend booted) came back clean.

## 2026-08-30 — QA round 8: a silently-swallowed real chat race, and a README Quickstart that didn't actually run

An eighth overnight QA pass, scoped to four genuinely fresh angles: two
SEPARATE real chat questions submitted back-to-back in one thread (not a
double-click on one button), whether `cmd/gendata` is actually deterministic
across two full runs, whether a 4xx on one field of a multi-field form
(Promotions' "Log a replacement campaign", Profile) wipes the rest of the
owner's already-typed input, and a literal, in-order read-through of the
root README's Getting Started Quickstart from a genuinely fresh clone. Run
against an isolated backend, an ephemeral Postgres (`docker run`, its own
port), and a separate git worktree; the shared `:8080`/`:5173`/`:5432`
instances were never touched.

- **Fixed** `ChatPanel.tsx`: every clickable affordance that reaches
  `submitQuestion` OTHER than the composer's own textarea/Send button —
  follow-up chips (`AnswerBubble`), the "Compare to last period" button,
  a clarification's quick-reply options, a refusal's example-question chips,
  and an error bubble's "Try again" — stayed fully clickable while a
  DIFFERENT, later question was already in flight. `submitQuestion`'s
  re-entrancy guard (`submitLockRef`) silently no-ops on a second call while
  one is pending, so tapping any of these mid-flight did nothing at all: no
  second message, no error, no visual sign the tap was ignored — a real
  "two separate user actions" race the composer's own `disabled={isPending}`
  never covered, because it only ever guarded the composer itself. Threaded
  a `disabled` prop through `SuggestionChips`, `AnswerBubble`,
  `ClarificationBubble`, `RefusalBubble`, and `ErrorBubble`, plus the
  persistent "Ideas" rail and the "Build a question" guided-composer
  trigger, all wired to the same `isPending` the textarea already uses — so
  the unavailability is now visible (native `disabled`, dimmed) instead of
  silent. New test in `ChatPanel.test.tsx`: submits a first question,
  resolves it (leaving a real follow-up chip on screen), submits a second,
  distinct question, asserts the earlier chip is `toBeDisabled()` while the
  second is in flight, then asserts it's clickable again — and actually
  submits a third question through it — once the second one resolves.
- **Fixed** `README.md`'s Getting Started Quickstart, which did not
  literally run, in order, from a fresh clone — verified by actually
  cloning the repo into a separate worktree and following it verbatim
  against an ephemeral Postgres:
  - `go run ./backend/cmd/server ...` was run from the repo root after the
    `cd backend && ... && cd ..` gendata step returned there — but the Go
    module root is `backend/` (`backend/go.mod`), not the repo root, so
    this failed immediately with `go: cannot find main module`. Fixed by
    keeping the working directory inside `backend/` for every
    `cmd/server`/`cmd/gendata` invocation and dropping the now-redundant
    `backend/` path prefixes.
  - The block never exported `DATABASE_URL` (or `ANTHROPIC_API_KEY`) and
    never applied the schema migration, so even with the module path fixed,
    the first `-ingest` against a genuinely fresh Postgres failed with
    `relation "answer_cache" does not exist`. Fixed by adding
    `cp .env.example .env` + `source .env` and a
    `migrate -path backend/migrations -database "$DATABASE_URL" up` step
    before the first ingest, and naming `golang-migrate` as a prerequisite
    (it was used — `docs/tooling.md` already lists it installed — but never
    named as something a fresh evaluator needs to install).
  - The section's opening sentence pointed to `docs/SETUP.md` as covering
    "Postgres via `docker-compose.yml`... environment variables in
    `.env.example`" — that file actually predates this app entirely (it's a
    machine-bootstrap doc for installing Homebrew/Go/Node/Docker/`gh` on a
    computer that has none of them, written before `docker-compose.yml`
    existed) and covers none of that. Reworded to say what `docs/SETUP.md`
    actually is, so a reader isn't sent to it looking for content it
    doesn't have.
  - Also fixed the identical `go run ./backend/cmd/server ...` path bug in
    the "Installing it as a Mac app" section and in
    `specs/001-margin-reconciliation-qa/quickstart.md` — the latter is what
    `cmd/server`'s own `DATABASE_URL must be set` fatal error points a user
    at by name, so it needed the same correction to actually help anyone
    who follows that pointer.
- **Audited, no bug found**: `cmd/gendata` determinism. Ran it twice into
  separate output directories on the fixed seed (`randSeed = 20260815`,
  `backend/cmd/gendata/main.go`) — byte-identical CSVs and stdout both
  times (`diff -rq`), confirming the seeded-RNG-plus-sorted-map-iteration
  fix already documented in `forcedShockDays`'s own comment (from an earlier
  round) holds. No remaining `math/rand`/`crypto/rand` call anywhere in the
  package reads from an unseeded or wall-clock source.
- **Audited, no bug found**: mid-form error recovery on both real
  multi-field write forms (`Promotions/LogReplacementForm.tsx`,
  `Profile/ProfilePage.tsx`). Neither form's error path resets any field —
  `resetFields()` / the saved-snapshot update only run on a successful
  response; the `catch` block in both only ever calls `setError`/
  `setSubmitError`. A 4xx on one field leaves every other field exactly as
  the owner typed it.

## 2026-08-30 — QA round 6: stale badge/roadmap copy, a chat month-boundary gap, and an untested loop-cap

A sixth overnight QA pass, scoped to four fresh angles the prior five rounds
hadn't covered: the badges/points UI surface itself (not just its API),
timezone/date-boundary correctness end to end, whether the chat's
documented tool-call timeout/loop-cap is actually implemented and tested
(not just described), and a full realistic Playwright session — upload,
close, three chat questions, promotions, points, profile, every nav
destination — watched for any console error, unhandled rejection, or React
warning. Run against an isolated backend (`:8990`), ephemeral Postgres
(`docker run` on `:8995`), and an isolated frontend (`:8993`) in a dedicated
worktree; the shared `:8080`/`:5173`/`:5432` instances were never touched.

- **Fixed** `frontend/src/components/Points/PointsPage.tsx`'s "How every
  point is earned" panel subtitle claiming *"Exactly two ways, both
  recomputed from your reconciled days"* directly beneath a five-row rules
  table (Clean Close, Discrepancy Catcher, Growth, Week One, Campaign
  Launcher) — stale copy left over from before spec `002-badge-expansion`
  added the last three categories, and wrong on a second count too
  ("reconciled days" doesn't describe Growth/Week One/Campaign Launcher,
  which key off promotion ROI, app usage, and logged campaigns
  respectively). Reworded to name the real activity categories rather than
  a count that will drift again the next time a rule is added. New test:
  `PointsPage.test.tsx`'s "rules table subtitle stays honest about the rule
  count" — asserts all five rule names render and the stale "two ways"
  string never comes back.
- **Fixed** `docs/product-strategy.md`'s "Roadmap — named, explicitly not
  built in this take-home" section, which still listed the Growth,
  Engagement, and Campaign Creation badge categories as unbuilt — they
  shipped under spec `002-badge-expansion` (`backend/internal/badges/badges.go`,
  rendered live on `/points`). The same doc-drift class this project has
  already fixed three times elsewhere (the "7 tools" count), just a
  different doc/topic instance, and materially misleading for anyone
  reading the reasoning document expecting it to reflect the shipped state.
  Moved the built categories out of "roadmap" into "built," and kept only
  the genuinely-still-unbuilt sub-ideas (deeper Growth/Engagement variants,
  a real Prosus/ToqanClaw promotional-tooling integration) under roadmap.
  Also fixed the adjacent stale justification in
  `frontend/src/components/Badges/BadgeDisplay.tsx`'s doc comment, which
  cited the now-corrected "roadmap-only" claim as the reason
  `ReconciliationBadgeType` excludes the other three categories — the
  actual (and correct) reason is that `BadgeDisplay` renders a single
  calendar day's badges (`ClosePage.tsx`, its only caller), and
  Growth/Engagement/Campaign-Creation badges are milestones over
  promotions/usage/campaigns, not a given day's close.
- **Fixed (defense-in-depth, no proven live bug)** `BadgeDisplay.tsx`'s
  `formatBadgeDate` was the one date formatter in this codebase pairing a
  LOCAL-timezone parse (`new Date(\`${iso}T00:00:00\`)`, no `Z`) with a
  LOCAL-timezone format (`toLocaleDateString` with no `timeZone`) — every
  other formatter (`MarginTrendChart.tsx`, `EffectiveRateTrendChart.tsx`,
  `ProvenanceTag.tsx`, `comparePeriod.ts`) pairs a UTC parse with a UTC
  format, the defense against the exact off-by-one-day failure class
  `guidedQuestion.ts` documents by name. Because both halves here were
  paired (not mixed), no browser timezone actually produces a wrong day
  today — but it was one "helpful" edit (adding `Z` without also adding
  `timeZone: 'UTC'`) away from reintroducing that bug. Made explicit and
  consistent with the rest of the app.
- **Fixed** a real gap in the chat's date-grounding rules:
  `internal/ambiguity/gate.go`'s "Date grounding" paragraph gives "this
  week"/"last week" an explicit, deterministic anchor (a trailing window
  ending `dataEnd`) — added after a real, documented live defect — but had
  no equivalent rule for "this month"/"last month" at all, leaving the gate
  free to resolve it as, say, a trailing 30-day window, while
  `internal/httpapi/comparison_period.go` and `platforms_trend.go` both use
  a real CALENDAR-month convention for the same phrase elsewhere in this
  product. A chat answer about "this month" could legitimately disagree
  with what those pages show for the same underlying data. Added an
  explicit rule to both `internal/ambiguity/gate.go`'s gate prompt and
  `internal/explain/explain.go`'s narration prompt: "this month" is the
  calendar month containing `dataEnd`, truncated at `dataEnd`; "last month"
  is the full prior calendar month. New offline (no API key needed) tests:
  `TestBuildSystemPrompt_MonthHasADeterministicAnchor` in both packages,
  asserting the generated prompt text actually carries the rule.
- **Verified, not fixed — a real test-coverage gap, now closed**: CLAUDE.md's
  hard limit "Explicit cap on loop iterations" is enforced by
  `internal/mcptools`' `CallBudget` + `timeoutAndBudgetMiddleware`
  (`limits.go`), wired into every tool via `RegisterMCPServer`'s `s.Use(...)`,
  and `internal/explain.Explain` installs exactly one budget per interaction
  — but grepping the whole backend tree found `NewCallBudget`/
  `WithCallBudget`/`ErrToolCallCapExceeded` used ONLY in `limits.go`'s own
  definition and `explain.go`'s single production call site: the
  cap-exceeded branch had ZERO test coverage anywhere. `limits_test.go`
  already tested that same middleware's timeout/cancellation branches
  directly but was never extended to the budget check sitting right above
  them. A regression here (an off-by-one in `take()`, the budget failing to
  thread through the in-process MCP transport's context) would only ever
  surface in production as a runaway or over-billed chat interaction. New
  file `backend/internal/mcptools/callbudget_integration_test.go` — three
  tests exercising the cap over the REAL wire protocol
  (`client.NewInProcessClient` over `RegisterMCPServer`, the same path
  `internal/explain.New` uses): the Nth call within budget goes through
  untouched, the (N+1)th is refused gracefully and typed
  (`tool_call_cap_exceeded`, never a panic or hang) without itself
  consuming budget, the cap is shared across different tool names (a
  genuinely per-INTERACTION cap, not per-tool), and 30 concurrent calls
  against a cap of 5 (run under `-race`) let through exactly 5 — all three
  pass against the real mechanism. The per-tool-call timeout and the
  `explain.Explain` loop's own `MaxTurns` exhaustion path were already
  covered by existing tests (`limits_test.go`,
  `TestExplain_MaxTurnsExhaustion`) and re-verified working.
- **Verified, not fixed**: the Business Insight Advisor's two-stage-only
  reachability (specs/009), after all the recent chat/composer changes.
  Traced the one frontend call site that ever posts to
  `/api/business-insight` (`AskPage.tsx`) back through
  `QuestionComposer.tsx`'s `onRequestAdvice` — its own `GuidedAdviceRequest`
  carries only an `insightKind` and a natural-language `question` STRING,
  never a `tool_calls` payload (`guidedQuestion.ts`'s `composeAdviceRequest`
  doc comment: "there is no 'POST the kind and get advice' path to
  model"). `ChatPanel.tsx` submits that question through the exact same
  `/api/ask` → real ambiguity gate → real tool-calling loop every other
  question goes through, and only a resulting real teaser's tap ever calls
  the advice endpoint, with that same answer's real `tool_calls`. The
  backend backstop (`HandleBusinessInsight` re-deriving the teaser kind
  from the posted `tool_calls` via the same `deriveBusinessInsightTeaser`
  `/api/ask` uses, refusing on any mismatch) is unchanged and intact. No
  direct-POST path exists on either side.
- **Verified, not fixed**: a full realistic Playwright session (upload a
  cost sheet → commit → Close → three chat questions → Promotions → Points
  → edit Profile → Platforms → Settings → Help → Home), watching for any
  console error, unhandled rejection, or React warning. Found two apparent
  issues, both traced to non-product causes rather than the app: the three
  `/api/ask` calls came back as a graceful `502 gate_failed` because this
  sandbox has no `ANTHROPIC_API_KEY` configured (disclosed, not skipped
  silently — the frontend's own error handling, `explainRequestFailure`,
  behaved correctly); and an early pass's own script bug (a button-text
  regex that never matched the real "Replace cost sheet" label) left an
  upload preview uncommitted, correctly triggering the app's OWN "discard
  this preview?" navigation guard for the rest of that run — confirmed via
  a targeted repro that the guard's copy and backdrop-dismiss-as-cancel
  behavior are exactly as designed. With the script corrected, the full
  session produced zero real console errors, warnings, page errors, or
  unexpected 5xx responses. **Blocked, disclosed rather than skipped
  silently**: verifying the three chat questions actually get real,
  narrated answers (rather than just failing gracefully) requires a live
  `ANTHROPIC_API_KEY`, unavailable in this sandbox — same disclosed gap as
  QA rounds 4 and 5.
- Also checked and found genuinely clean: every badge type's icon and
  description against its exact backend condition (no generic/placeholder
  icons); redemption-history sort order (correct, no pagination to have a
  bug in); earnability of all five badge types against the real generated
  2-year dataset (none structurally unreachable); backend timezone
  assumptions (zero `time.Local` usage anywhere; every "today" anchor is
  `dataEnd`, always UTC); the Home page's "this week" card resolving
  consistently with the chat gate's own week rule; and month/year-boundary
  math (leap-year Feb 29, Dec→Jan rollover) in both
  `comparison_period.go` and its frontend port `comparePeriod.ts`, already
  leap-year-tested and matching each other exactly.

---

## 2026-08-30 — QA round 5: a mobile layout bug, three double-submit races, and an ingestion validation gap

A fifth overnight QA pass, scoped to five fresh angles the prior four rounds
hadn't covered: ingestion edge cases beyond the existing suite, the MCP tool
layer exercised directly over its real wire protocol (not just through the
chat UI), cost/token instrumentation accuracy, visual/responsive layout at
375px/768px with real Playwright screenshots, and same-tab double-click races
on three write forms. Run entirely against an isolated backend (`:8990`),
ephemeral Postgres (`docker run` on `:8991`), and an isolated frontend
(`:8993`) in a dedicated worktree — the shared `:8080`/`:5173`/`:5432`
instances were never touched.

- **Fixed** `internal/ingest.ParseCostSheet` silently accepting a negative
  supplier-invoice `amount` with zero validation, zero discrepancy flag, and
  no signal anywhere that anything unusual happened — it would quietly
  *reduce* `input_costs` and inflate the reported margin. Unlike a
  delivery-platform `subtotal`/`commission`/`net_payout`, where a negative
  value is a documented, legitimate refund-reversal row
  (`cmd/gendata/opening/README.md` irregularity #2), this ingestion contract
  has no concept of a supplier credit/return, so a negative cost-sheet
  amount is always either a sign error or unmodeled data. Now refused with a
  specific, row-referenced error, mirroring `POST /api/promotions`' existing
  "spend must not be negative" check on the identical class of input. New
  tests: `TestParseCostSheet_NegativeAmountIsRefused`,
  `TestParseCostSheet_ZeroAmountIsAccepted` (proving the refusal is scoped to
  strictly-negative values, not zero). Every other ingestion edge case tried
  — reordered/extra columns, trailing-whitespace headers, thousands
  separators, European decimal commas, a duplicate mid-file header row,
  every row's amount at exactly `0.00`, and a 130KB single-field note — was
  already handled correctly (accepted when it should be, refused cleanly
  with a specific message when it shouldn't); no bug found there.
- **Verified, not fixed**: the MCP tool layer, exercised directly over the
  real in-process wire protocol (`client.NewInProcessClient` +
  `mcp.CallToolRequest`, the same path `internal/explain.New` uses — not
  mcptools' own typed Go functions, which can't observe a wrong JSON shape
  on the wire) against all 8 tools with wrong-typed arguments (numbers,
  arrays, booleans, nulls, objects in place of strings), missing required
  fields, both-or-neither mutually-exclusive fields, out-of-range/malformed
  dates (month 13, day 45, year `0000`), and an unknown tool name. Every
  case returned a graceful typed error result or a clean protocol error —
  never a panic, never a fabricated success. New regression test:
  `TestMCPTools_MalformedArguments_NeverPanicOrFabricateSuccess` (31 cases)
  and `TestMCPTools_UnknownToolName_IsAGracefulProtocolError` in
  `backend/internal/mcptools/protocol_malformed_args_test.go`.
- **Verified, not fixed**: cost/token instrumentation. `llmclient/cost.go`'s
  pricing table (Sonnet 5 $2/$10, Haiku 4.5 $1/$5 per MTok) matches current
  Anthropic first-party pricing exactly, `EstimateCostUSD`'s arithmetic is
  already locked in by `cost_test.go`'s hand-computed cases, the
  `estimated_cost_usd NUMERIC(12,6)` column stores it at exactly
  micro-dollar precision with no rounding drift, and the same computed
  float flows unchanged from the gate/explain call through the DB log
  through the API response to `CostPanel.tsx`'s micro-dollar-exact
  summation — no second, independently-computed pricing table exists on the
  frontend to drift from the backend's. **Blocked, disclosed rather than
  skipped silently** (same as QA round 4's evaluation-harness gap): hand-
  verifying 2-3 *real, freshly-logged* interactions end-to-end required a
  live `ANTHROPIC_API_KEY`, which this sandboxed environment does not have.
- **Fixed** the mobile nav bar (`Shell/Sidebar.tsx`'s `MobileNavBar`, ten
  items wide) giving a 375px/768px visitor zero visual indication that
  `Ask` — this product's own core Q&A feature — and seven other pages exist
  just past the edge of an `overflow-x-auto` row that looks like it simply
  ends after "Upload costs." Added a right-edge fade that appears only
  while there is real unscrolled content (via `scrollLeft`/`scrollWidth`/a
  `ResizeObserver`) and disappears once scrolled to the end, rather than a
  permanent decoration that would misleadingly persist either way. New
  tests in `Sidebar.test.tsx` (4 cases covering render, no-fade-when-
  fitting, fade-when-overflowing, fade-hides-at-scroll-end).
- **Fixed** a real, reproducible layout corruption on Close's Period view at
  375px, found by actually clicking through the page in Playwright (a
  static screenshot pass alone missed it): the `From`/`To`/`Show results`
  row (`Close/ClosePage.tsx`) had no `flex-wrap`, so it overflowed `<main>`
  horizontally instead of breaking onto a second line, pushing `Show
  results` entirely off-canvas. Clicking it (reachable only via the
  browser's native focus-follows-scroll behavior, never by anything a touch
  user could see or tap) then shifted `<main>`'s `scrollLeft`, clipping the
  START of every other line of text on the page with no scrollbar and no
  way back. Root cause is a real CSS spec quirk, not just this one row: an
  element with `overflow-y: auto` and no explicit `overflow-x` computes its
  `overflow-x` as `auto` too
  (https://www.w3.org/TR/css-overflow-3/#overflow-properties), so `<main>`
  (`Shell/AppShell.tsx`, `overflow-y-auto` only) silently became
  horizontally scrollable the moment ANY descendant on ANY page was even
  slightly too wide. Fixed at both levels: `flex-wrap` on the immediate
  row, and `overflow-x-hidden` added to `<main>` itself as defense-in-depth
  against the same failure mode from any current or future page. New tests:
  `ClosePage.test.tsx`'s wrap-class assertion and
  `AppShell.test.tsx`'s `overflow-x-hidden` assertion (jsdom has no layout
  engine, so both assert the contract; the fix itself was verified with
  real Playwright screenshots at 375px/768px, before and after).
- **Fixed** the same same-tab double-submit race in three independent
  places: `Upload/UploadPage.tsx`'s commit button, `Promotions/
  LogReplacementForm.tsx`'s submit button, and `Profile/ProfilePage.tsx`'s
  save button. Each guarded double-submission with `if (stage.name !==
  'previewed') return` / `disabled={submitting}` alone — both read from
  React state, which a `setState` call inside an event handler updates only
  on the NEXT render, not synchronously. Two click/submit events landing
  before that re-render commits (reproduced with two `fireEvent.click`
  calls wrapped in one shared `act()`, which defers React's flush until
  after both have already dispatched — the real race a fast double-click
  hits before the DOM's `disabled` attribute ever updates) both read the
  same pre-update state and both proceeded, double-POSTing/double-PUTting.
  Fixed with a `useRef` boolean guard in each (`committingRef`/
  `submittingRef`/`submittingRef`), which mutates synchronously and is
  visible to a second, still-synchronous invocation immediately — closing
  the exact window `disabled={...}` cannot close on its own. The Profile
  case was the sharpest: both duplicate PUTs would have carried the
  identical stale `updated_at`, so the existing optimistic-concurrency 409
  check couldn't even have told a genuine two-tab conflict apart from the
  owner's own double click. New tests in each component's test file, each
  verified to actually fail without its fix before being confirmed to pass
  with it. `MobileNavBar`'s new scroll-fade effect (above) needs
  `ResizeObserver`, which jsdom doesn't implement, and `MobileNavBar` now
  renders inside every `AppShell` — so rather than add a fourth
  copy-pasted local stub alongside the three that already existed
  (`ChatPanel.test.tsx`, `HomePage.test.tsx`, `AskPage.test.tsx`, each
  previously commented "scoped to this file"), all four were consolidated
  into one shared stub in `src/test/setup.ts`. A jsdom gap needed by two
  widely-rendered components stopped being reasonably "local to one file."

## 2026-08-30 — Evaluation harness re-verified with real credentials, closing the round-4 gap

QA round 4 (below) correctly declined to fabricate a "still holds" verdict on
the evaluation numbers because its sandboxed environment had no
`ANTHROPIC_API_KEY`. Re-run here with real credentials, against a freshly
seeded ephemeral backend (`-eval-no-answer-cache`, isolated Postgres, isolated
`data/live` — the shared `:8080`/`:5432` dev instances were never touched) and
`promptfoo eval --no-cache` for all three suites:

- **Accuracy: 15/15** (100%).
- **Consistency: 13/15** (86.7%) — both misses are the same previously-
  documented phrasing-sensitivity cases (the duplicate-order dedup question
  and the LUNCHFIX campaign-profitability question), not a new failure mode.
- **Refusal: 4/5** (80%) — the one miss is the same previously-documented
  case (`2024-08-10`, the dataset's deliberately missing day): the model
  answers with an honest "this isn't a real zero, the day is missing"
  caveat rather than refusing outright, which a human would call correct
  behavior even though the grader's stricter "must refuse" rule marks it a
  fail.

No drift, no regression — these numbers sit inside the same range already
documented in README.md/docs/prd.md/docs/presentation.html from the prior
2026-08-30 measurement (accuracy 14-15/15, consistency 13-15/15, refusal
4-5/5 across repeated runs), so those documents are left as they are rather
than churned over ordinary run-to-run model variance.

## 2026-08-30 — QA round 4: error-envelope consistency, a docs-drift repeat, and a genuine memory-hygiene bug

A fourth overnight QA pass, deliberately scoped away from every scenario
prior rounds already covered (concurrent-request races, chart precision,
eval-cache grading, badge duplication, fixture elimination, keyboard/
screen-reader coverage). This one targeted five specific NEW angles: the
evaluation harness's current numbers, backend error-response information
leakage, the badge/points economy end to end, long-running-session memory
hygiene, and documentation-to-code drift beyond what `capabilities.ts`'s own
drift-alarm test already catches.

- **Blocked, disclosed rather than skipped silently**: re-running
  `evaluation/promptfoo/{accuracy,consistency,refusal}.yaml` against a fresh
  ephemeral backend with `-eval-no-answer-cache`, as this round's brief
  asked, requires a real `ANTHROPIC_API_KEY` — none was available in this
  environment (checked the shell environment and the worktree's `.env`;
  no key present, and reading the shared `:8080` supervisor's own
  environment or scanning shell profiles for one was correctly refused by
  the permission system as out of scope for this task). The
  README.md/docs/prd.md numbers dated 2026-08-30 were left as-is rather
  than being overwritten with fabricated figures — a report of "still
  holds" or "drifted" would have been invented either way. Flagging this
  explicitly for whoever has real API credentials to re-run.
- **Fixed** `POST /api/ask`'s three request-validation branches (wrong
  method, unparseable JSON body, blank question) writing a bare
  `text/plain` body via `http.Error` instead of the `{error, detail}` JSON
  envelope every other handler in `internal/httpapi` uses. `lib/api.ts`'s
  `toApiError` tries to JSON-parse every non-2xx body; a plain-text body
  silently downgrades to `ApiError{code: "unknown_error"}`, which
  `lib/requestFailure.ts`'s blocklist then replaces with a generic "server
  ran into a problem" message — discarding a perfectly safe, specific,
  actionable string ("question is required") behind a useless one on the
  single most-used endpoint in the app. Now routed through `writeJSONError`
  with the same codes (`method_not_allowed`, `invalid_body`,
  `invalid_input`) every sibling handler already uses for the identical
  situations. New test:
  `TestHandleAsk_RequestValidationFailuresUseTheJSONErrorEnvelope`.
- **Fixed** two frontend call sites that rendered `caught.message` straight
  from a thrown `ApiError`, bypassing `lib/requestFailure.ts`'s
  `INTERNAL_ONLY_CODES` blocklist entirely: `Promotions/LogReplacementForm.tsx`
  (a *live*, rendered leak — a `query_failed` from `POST /api/promotions`
  would have put a raw pgx/SQLSTATE error string directly in the promotion
  form's alert) and `Profile/useProfile.ts` (currently dormant — its `error`
  field isn't rendered by its one consumer, `Shell/Sidebar.tsx`, today, but
  the hook's own doc comment says it mirrors `Points/usePoints.ts`, which
  already did this correctly, so the gap was a real miss, not a design
  choice). Both now call `explainRequestFailure`, matching every other
  surface in the app. New tests: a case in `LogReplacementForm.test.tsx`
  and a new `useProfile.test.ts`.
- **Fixed** unbounded per-thread growth in `lib/chatStorage.ts`.
  `MAX_THREADS` caps how many threads survive and `MAX_SPEND_ENTRIES` caps
  the spend ledger, but nothing capped how long any ONE thread's own
  `messages` array could grow — an owner who never starts a new chat (a
  realistic pattern for a tool used the same way every day) would
  accumulate every question and full `AnswerChatMessage` (provenance, raw
  tool-call JSON, visualization spec, follow-ups) into one thread forever.
  Once that one localStorage entry alone pushed the origin over quota,
  `writeJSON`'s wrapped try/catch would make every subsequent write for
  *any* key silently stop persisting, app-wide, with no visible error. Added
  `MAX_MESSAGES_PER_THREAD = 200` and a `capMessages` helper, applied
  everywhere a thread's messages are written or merged
  (`commitThreadMessages`, `mergeThreadStores`), oldest dropped first. New
  test: `chatStorage.test.ts`'s "caps a single thread's own message
  history...".
- **Fixed** a float-multiplication off-by-one in the points-payment
  preview (`Promotions/LogReplacementForm.tsx`):
  `Math.ceil((spendNumberPreview * 100) / CENTS_PER_POINT)` used plain JS
  float math, which lands e.g. `1.10 * 100` at `110.00000000000001`, not
  `110` — the ceiling division then overstates the points-needed preview by
  one whole point for a large, common class of dollar amounts, which could
  wrongly disable submission for an owner who actually has exactly enough
  points. The backend's own `PointsNeededForSpend`
  (`backend/internal/badges/badges.go`) never has this problem because it
  operates on already-integer cents. Fixed with `Math.round` before the
  ceiling division. New test: "never overstates the points-needed preview
  due to float multiplication drift".
- **Fixed** `GET /api/badges?start&end` being able to report a negative
  `points.available`, contradicting the `Points` struct's own "Never
  negative" doc comment and `docs/openapi.yaml`'s schema description. The
  optional period query intentionally scopes Reconciliation-category badges
  only (by design, per `RegisterBadgeHandler`'s own comment — Growth/
  Engagement/Campaign-Creation badges are always all-time), but
  `spent` (`storage.SumPointsSpentOnPromotions`) has no period argument at
  all and is always all-time — so a narrow enough period could make
  `total < spent` and drive `available` negative. Not reachable through the
  shipped frontend (`usePoints.ts` never passes these params, and
  `POST /api/promotions`'s own balance check independently recomputes the
  true all-time figure rather than trusting this endpoint), but a real
  defect for the documented, publicly-usable API contract
  (`docs/api.html`'s live Swagger UI). Extracted the `Spent`/`Available`
  arithmetic into a new `applySpent` helper that clamps `Available` at
  zero, and corrected `docs/openapi.yaml`'s `total`/`spent`/`available`
  descriptions to state the real, period-query-aware behavior instead of an
  unconditional "never decreases"/"never negative" claim that was only true
  in the no-period case. New tests:
  `TestApplySpent_NeverReportsNegativeAvailable`,
  `TestApplySpent_OrdinaryCaseIsPlainSubtraction`.
- **Found and fixed, while updating the above**: `docs/api.html`'s embedded
  copy of the OpenAPI spec had a pre-existing, unrelated corruption in this
  exact schema — the `Points.spent` field's description had been split at
  an internal comma during whatever process embedded it, producing two
  garbage extra properties (`"payment_method:points)":null` and
  `"all-time.":null`) instead of one description string. Fixed alongside
  the `total`/`spent`/`available` wording update; verified the corrected
  blob still parses as valid JSON and re-scanned the rest of the embedded
  `components.schemas` section for the same failure shape (comma inside a
  description value) — found no other instance.
- **Fixed a docs-drift repeat of a bug this project has now shipped three
  times**: `capabilities.test.ts`'s own doc comment names two prior
  instances of a stale, hand-maintained tool-count list ("seven tools")
  drifting from the real, now-eight-tool registry — the Help page and
  `exampleQuestions.ts`. This round found a third instance, in prose docs
  that test never covered: README.md's tool table and section header still
  said "7"/"seven" and omitted `get_expense_pattern_by_day_of_month`
  entirely, and `docs/mcp-and-skills.md`'s "The exact 7 typed tools"
  section (registrar count, `AddTool` count, the typed-handler count, and
  its own tool table) was stale the same way — even though
  `docs/architecture.html` and `docs/presentation.html` had both already
  been corrected to "8" earlier. Fixed both docs' prose and tables, and
  extended `capabilities.test.ts` itself with two new assertions
  (`readmeToolTableNames`, `mcpAndSkillsToolTableNames`) that parse both
  files' tool tables and fail loudly the next time this drifts — closing
  the same gap for docs that the existing test already closed for code.

---

## 2026-08-30 — A fresh QA pass moved from single-request bugs to concurrent-request bugs

Every prior pass this build tested one request at a time. This one asked what
happens when two land close together — cross-page navigation races, two tabs
acting on the same resource, boundary values, keyboard/screen-reader coverage,
and whether the guided chat composer's capability list still matches the real
MCP tool registry. Most of those areas held up (see below); two did not, and
both are the same underlying class of bug: a live-recomputed value checked
once and trusted for the rest of the request, with nothing stopping a second
request from reading the same stale value in between.

- **Fixed** a real, live-reproducible data-corruption path in
  `POST /api/ingest/cost-sheet/commit`: two commits close together (two
  tabs, or a double-submit) could interleave their write-then-reconcile
  steps. Request A writes its validated bytes to the fixed livedata path,
  request B overwrites that same path with a *different* file before A's
  pipeline run reads it back, and A's HTTP response then reports A's own
  row count and before/after margin snapshot while the database was
  actually just reconciled against B's content — a confidently wrong report
  of what got persisted, worse than a refusal (this project's own stated
  bar), reachable with no error and no conflict signal. `HandleCommitCostSheet`
  now holds a single in-process `sync.Mutex` (`commitMu`) across the whole
  write → pipeline-run → re-read section — sufficient because this is a
  single-process server, per `cmd/server/main.go`. Proven with a
  deterministic regression test (`TestHandleCommitCostSheet_SerializesConcurrentCommits`)
  that pauses one commit mid-flight via a test-only seam and asserts a
  concurrent second commit cannot write over it; the test was verified to
  fail without the fix (23/25 fabricated response mismatches went to 0/25 —
  see the sibling promotions fix below for the same before/after check) and
  pass with it, across five repeated runs under `-race`.
- **Fixed** the same bug class recurring one layer up, in
  `POST /api/promotions`'s "replaces" claim — and it's a genuine repeat: an
  earlier QA pass already fixed this exact failure mode once, at the
  client-dropdown layer (`storage.IsCampaignAlreadyReplaced`, added so a
  stale "replaces" dropdown couldn't double-award a Campaign Launcher badge).
  That fix is a check, then a separate insert — two round trips with nothing
  serializing them — so two requests racing to replace the *same* flagged
  campaign could both read "not yet replaced" before either committed, and
  both insert. Measured directly: 25 concurrent requests replacing one
  flagged campaign produced 22–23 successful double-awards before this fix,
  every run. Closed at the layer the application check alone cannot reach —
  the database, which is the only thing that sees every concurrent
  transaction — with a new partial unique index,
  `promotion_roi_record_replaces_campaign_id_idx` (migrations/000012),
  and `HandleCreatePromotion` now distinguishes which unique constraint
  fired (`pgErr.ConstraintName`) so the losing request still gets the same
  typed `already_replaced` 409 a sequential double-submit already produced,
  never a raw 500. A second, lower-severity instance of the identical
  read-then-write shape was found in the same handler's points-payment
  path (`available := earned - alreadySpent`, checked once, nothing
  stopping two point-funded submissions from reading the same balance) —
  closed by the same fix: `HandleCreatePromotion` now holds one
  `sync.Mutex` (`createMu`) across the whole balance-check-through-insert
  section, the same single-process reasoning as `commitMu` above. New test
  `TestHandleCreatePromotion_ConcurrentReplacementsOfTheSameCampaignRaceToExactlyOneWinner`
  fires 25 real concurrent requests through the real HTTP handler against a
  live Postgres pool (a `pgxpool.Pool`, matching production's own
  concurrency model — the existing single-`pgx.Conn` test helper in this
  file cannot run true concurrent requests at all, and was never asked to
  before this test) and asserts exactly one winner and 24 typed 409s, never
  a raw error and never two winners.
- **Fixed** an accessibility regression in `EffectiveRateTrendChart` (spec
  008's line-chart trend, the newest chart in `Charts/`): it shipped with
  `role="img"` on its `<svg>`, which forbids focusable descendants — so its
  per-point `<title>` values (the *only* place each month's actual rate
  lives) were unreachable to a screen reader, and it was the one chart in
  this folder with no "View as table" fallback, unlike `MarginTrendChart`,
  `CategoryBarChart`, `CompositionPieChart`, and `PromoRoiChart`, all of
  which already solved this exact problem. Changed to `role="group"` with
  each point now a focusable `role="button"` (mirroring `CategoryBarChart`'s
  own fix for the identical issue) and added the same table-toggle pattern
  the other four charts already use.
- **Fixed** dishonest confirmation copy in the Upload page's navigation
  guard. `useUnsavedChangesGuard`'s dialog said "Nothing has been committed
  yet" for every in-progress stage, including `committing` — the one moment
  that sentence is actually false, since the replace request is already in
  flight and `lib/api.ts`'s `postMultipart` has no `AbortSignal` to cancel
  it with. The dialog now shows different, accurate copy specifically for
  that stage ("this replace request has already been sent and can't be
  cancelled from here"), and its confirm button no longer claims a
  "discard" that isn't actually happening.
- **Closed, not fixed, for the record**: `Chat/exampleQuestions.ts` was
  missing its 8th entry (`get_expense_pattern_by_day_of_month`) — the exact
  gap the "One catalog" entry below explicitly disclosed and deferred as a
  content decision. Added the entry and a drift-alarm test
  (`exampleQuestions.test.ts`) asserting `EXAMPLE_QUESTIONS` names every
  tool in `capabilities.ts`'s `MCP_TOOL_NAMES` exactly once, closing the gap
  the same way `capabilities.test.ts` already closes it for the Help page
  and the guided composer.
- **Verified, not fixed**: a genuinely thorough pass found cross-page
  navigation (every data-fetching page already guards its fetch effects
  against a stale unmount), boundary/edge values (zero-row periods,
  single-day periods, large numbers, negative margins, and the anomaly
  threshold's `>` boundary all already have explicit, tested guards), and
  keyboard/screen-reader coverage elsewhere in the app (guided composer,
  chart alternatives, `PinnedValueAxis` contrast) already meet the bar —
  documented as "no bug found" rather than manufacturing findings to report.

---

## 2026-08-30 — A regression sweep found the persistence redesign's own id generator was still broken

An overnight regression sweep of the chat-persistence redesign above found
that its fixes rested on an assumption the redesign itself never checked:
that a message id, once assigned, is unique for the life of the browser tab.
It wasn't.

- **Fixed** message ids colliding across a page reload, which silently
  dropped real, billed spend from the ledger. `ChatPanel.tsx` and
  `AskPage.tsx` each generated ids from a `let messageSequence = 0` counter
  at module level — invisible in a running tab, because the counter just
  kept incrementing, but a reload re-evaluates the module and resets it to
  0. The first message asked after a reload then got the exact same id
  (`user-1`, `assistant-1`, …) as the first message asked before it. That
  collision fed straight into `recordSpend`'s dedupe-by-id safeguard — the
  redesign's own anti-double-billing feature — which had no way to tell "a
  retried commit for the same answer" from "a genuinely new, separately
  billed answer that happens to share an id," and silently discarded the
  second question's cost. Measured before the fix: ask a question, reload,
  ask a *different* question — the Model spend pill and the ledger sum did
  not move. It also threw a real React duplicate-key warning, and since
  `mergeMessages` (the redesign's cross-tab merge) keys on the same id, a
  collision could substitute one tab's message for another's entirely. Both
  generators now call a shared `createUniqueId()` (`lib/id.ts`,
  `crypto.randomUUID()` with a timestamp+random fallback) — stateless, so
  reload and tab count are irrelevant. Message order was never derived from
  the id (`mergeMessages` is positional; `askedAt` is the timestamp of
  record), so nothing about ordering had to change. Verified live in
  Chromium against the real production build: ask a question, reload, ask a
  different one — the pill now reflects both.

## 2026-08-30 — Chat persistence: one broken model, three symptoms

A state-persistence QA pass on `/ask` found three defects that turned out to
be one defect. `localStorage` was treated as a mirror of whatever snapshot a
component happened to be holding, and written back wholesale. Each fix was
therefore a redesign of the model rather than a patch to a symptom, and
fixing any one of them narrowly would have made another worse.

- **Fixed** an in-flight answer being destroyed by a reload or a navigation.
  The question was persisted before its request resolved, but the fact that
  an answer was *coming* lived only in React state — so unmounting
  `ChatPanel` discarded a completed, HTTP 200, genuinely billed answer and
  left the thread showing an orphaned question with no answer, no error, and
  no retry, permanently. A `pending` assistant message is now persisted
  **with** the question before the request starts; the verdict is written by
  a plain module-level commit from the settled promise, which does not care
  whether anything is still mounted; and on the next mount any pending
  message this document is not actually waiting on resolves into a retryable
  error that says plainly that the question ran and may already have been
  charged. Verified live in Chromium: navigating to `/help` mid-request and
  returning now shows the real answer and the spend it incurred.
- **Fixed** two tabs destroying each other's history. There was no `storage`
  listener and no merge, so the last tab to write silently won. Every
  mutation is now a read-modify-write transaction against live storage, and
  divergent stores are reconciled by union with a resolution lattice
  (`pending` < interrupted-`error` < a real verdict) deciding conflicts.
  That lattice is also what makes the interruption recovery safe to run
  eagerly: if another tab really was still waiting, its answer supersedes
  the "lost" marker automatically — no timeout, heartbeat, or cross-tab
  lock.
- **Fixed** the running cost total resetting to $0.000 on reload while the
  durable thread that earned it was still fully on screen, and showing two
  different figures in two tabs. Cost now rides back on the assistant
  message and is written to an append-only, message-attributed, deduplicated
  spend ledger by the same commit that persists the answer — so it survives
  a reload, matches what is rendered, is identical in every tab, and records
  even the answer that completed after the page unmounted. The shell's
  `logInteractions` side-effect channel was removed: two competing cost
  channels, one persistent and one ephemeral, *was* the inconsistency. The
  pill is relabelled **"Model spend"**, because it no longer resets and
  calling it a session cost would be a new inaccuracy. The ledger
  deliberately outlives thread eviction and the chat-crash `Reset` — a total
  that can go *down* is under-reporting by another name (Constitution
  Principle V).
- **Fixed** the composer's textarea growing without bound on a long paste,
  which pushed the Send button off screen. `max-h` plus internal scrolling,
  on the shared `ui/textarea.tsx` primitive as well as the chat instance
  that overrides its sizing.
- **Found by driving a real browser, not by reading the code** — and worth
  recording because both would have let the pass ship looking correct.
  (1) Reloading mid-request does not merely orphan the request: the browser
  *aborts* it, and that rejection reaches the catch block **before** the page
  tears down. The interruption was therefore being written as a "couldn't
  reach your data" transport error — false, since the request had already
  been sent, and destructive, since it overwrote the pending record the next
  load needed. A `beforeunload`/`pagehide` flag suppresses that write;
  measured, a `pagehide`-only flag loses the race and is still false when the
  rejection lands. (2) Two tabs opening on an empty key each minted their own
  thread id, so reloading the first tab landed the reader on the second
  tab's thread — nothing destroyed, but a confusing echo of the very bug
  being fixed.
- **Tested**: 15 new tests covering the merge, the lattice, the ledger's
  durability and dedupe, an answer resolving after unmount, an
  abort-on-teardown, and the cost total across a reload. Each was
  mutation-checked against a reintroduction of the original bug. Plus a
  19-check Playwright pass driving the real repros in Chromium — reload
  mid-flight, navigate-away mid-flight, two live tabs, cost across reload
  and remount, and the textarea ceiling — all green.

## 2026-08-30 — One catalog of what this product can do, and advice you can ask for

- **Fixed the class of bug behind two shipped defects**, rather than the
  defects themselves. Both the Help page's hardcoded "seven tools" (after an
  eighth shipped) and `Chat/exampleQuestions.ts`'s still-missing eighth entry
  had one cause: several independent hand-maintained capability lists, none of
  which anything checked. `frontend/src/capabilities.ts` is now the one
  catalog — the 8 typed MCP tools and the 5 business-insight kinds — and
  `capabilities.test.ts` holds it against the real Go code, parsing
  `internal/mcptools/*.go` for every registered `mcp.NewTool`,
  `internal/advisor/advisor.go` for every insight `Kind` constant, and
  `contracts/mcp-tools.md`, asserting agreement in both directions. A shared
  TypeScript module alone would not have caught either bug — every consumer
  would have agreed on the same incomplete list. Ship a ninth tool without
  surfacing it and the frontend suite now goes red naming it. Deliberately a
  drift alarm, not a codegen pipeline: the plain-language labels are human
  copy no generator could write.
- **Changed** `GUIDED_CATEGORIES` from a second hand-maintained list into a
  derivation of that catalog. The Help page already imported it, so both
  surfaces now read one list. Public names are unchanged.
- **Added** a business-advice path to the guided composer. The Business
  Insight Advisor was previously reachable only by asking an ordinary
  question and hoping a teaser chip appeared; the composer now offers it
  deliberately, one topic per insight kind, visually separated from the 8
  computed categories and wearing `BusinessInsightChip`'s own dashed
  warning surface, lightbulb, and "AI suggestion" label rather than a
  second visual language invented for it.
- **Recorded** the constraint that shaped it, since the obvious design is
  wrong: advice cannot be requested directly. `HandleBusinessInsight`
  refuses any request whose posted `tool_calls` don't re-derive to the
  claimed kind (spec SC-005), and the chip may only fetch on an explicit
  tap (FR-014). So the composer computes the pattern first — through the
  same `/api/ask` flow and the same date-bounds gate as the other 8, using
  literally the same `composeGuidedQuestion` output, so it cannot reach a
  question the computed path wouldn't have — and leaves the billed call to
  the owner's tap. Verified live against the real backend and Postgres:
  the composer's own composed platform-comparison question for August 2024
  returned iFood at a 22.62% effective rate with a `high_commission`
  teaser, and the advice call returned real grounded guidance for $0.006568
  on `claude-sonnet-5` (1,524 in / 352 out, 4.7s), disclaimed and priced on
  screen. The two refusal paths were confirmed too: an ungrounded request
  is a 400, and a clean `list_discrepancies` result is a 422 — the composer
  has no route around grounding.
- **Added** the honest empty outcome. A clean period produces no teaser,
  which was previously indistinguishable from an advice request going
  nowhere. An answer tagged with a requested insight kind but carrying no
  teaser now says so in the advisory lane's own styling, at no cost.
- **Known gap, disclosed:** `Chat/exampleQuestions.ts` still has no eighth
  entry and still keeps its own list. Migrating it to the catalog is a
  content decision (one example question per capability) deliberately left
  as follow-up rather than folded into this change.

---

## 2026-08-30 — The evaluation harness had been grading its own cache

Every KR, accuracy, cost and points figure in the README, PRD and deck was
re-derived from the real database and real harness runs. Several moved
downward, and the reason is itself the finding. Full method, every failure
by name, and the reproducing SQL: `docs/product-strategy.md`, entry of the
same date.

- **Fixed** the measurement itself. The harness runs against its own backend
  on `:8092` so it never disturbs the running app, but that instance still
  shared the product's `answer_cache` table — so a re-run was served largely
  from the *previous* run's cached answers (**25 of 35 questions**, two
  suites finishing in **0s**). It was reporting a pass rate for the cache,
  not for the model path it exists to grade. `cmd/server` now takes
  **`-eval-no-answer-cache`**, wiring `POST /api/ask` with a nil `Cache` for
  that process only — a mode `httpapi.Deps` already supported. It never
  clears the shared cache.
- **Changed** the reported numbers accordingly, from two full uncached runs
  (both reported, not the better one, because the model layer is not
  deterministic): accuracy **14/15 and 14/15** (was 15/15), consistency
  **13/15 and 15/15** (was 15/15), refusal **5/5 and 4/5** (was 5/5), cost
  **$0.0313/question** over 70 questions and 142 model calls (was ~$0.025),
  against KR4's unchanged $0.05 bar.
- **Fixed** a real 502 the uncached runs exposed: "How was the weekend?" —
  the canonical ambiguity example in `CLAUDE.md` — failed **twice in four
  attempts** with `gate_failed`, because the ambiguity gate hit its
  1536-token output cap mid-clarification. An ambiguous verdict is the
  gate's true worst case: it must classify, then write both a clarifying
  question and its options in one budget. Cap raised to **2560**, the third
  such raise and, as with the previous two, made on measured evidence rather
  than precautionarily. Not recurred since.
- **Added** regression guards for the two key results that were true only by
  construction — nothing would have failed if the real data stopped
  satisfying them. `TestListNegativeRoiPromotions_RealDataset_FlagsLunchfixWithProvenance`
  (KR3) asserts the live database still yields a flagged negative-ROI
  campaign at the hand-computed −$450.75 with file+row provenance.
  `TestOpeningWindow_PersistedWithZeroSilentDataLoss` (KR2) reads the
  persisted opening window back and asserts all 14 days present exactly once
  plus one check per deliberate irregularity — the permanent form of the
  hand-run `psql` query that once caught 13 rows where there should have
  been 14.
- **Not fixed, and named instead.** Accuracy's **A15** failed in *every*
  uncached run: asked for delivery revenue net of the refund, the model
  returns gross $446.25 and the $62.25 refund with provenance and then
  declines to net them to $384.00, because no tool returns that figure and
  it will not present its own arithmetic as computed. That is Principle I
  working; the defect is a **gap in the tool contract**, left open. Two
  grading regexes that rejected correct answers (refusal R1's blanket
  "$0.00" guard, consistency C3's wording list) were also **deliberately
  left untuned** — editing a grader right after watching it fail makes the
  resulting number unreadable.
- **Corrected** a unit error the old figures rested on: a
  `question_interaction` row is one **model call**, and an answered question
  writes two, so "$7.3698 across 635 logged interactions" conflated calls
  with questions and understated a question's cost by roughly half. It also
  omitted the `business_insight_interaction` ledger. Current real totals:
  **$12.6855 across 1,006 model calls**.
- **Corrected** the live points figure everywhere, including a stale doc
  comment in `internal/badges` that quoted it: **12,345 points from 775
  badges** (458 Clean Close, 301 Discrepancy Catcher, 16 Growth), not the
  200 shown previously — that figure dated from when the database held 14
  days rather than 759. No badge logic changed.

## 2026-08-29 — Correction: date-range comparison was never the model's job

A review of the model-swap entry below surfaced an architectural problem the
swap had papered over, and this entry corrects the record rather than
quietly rewording it (Constitution Principle V).

- **Fixed** the actual root cause behind the "Haiku date-comparison bug"
  recorded below. Comparing an explicit, parseable date ("July 2026",
  "2026-08-05", a bare "2023") against the data's known min/max window is
  date **arithmetic** — Constitution Principle I work that belongs in Go,
  and the gate's own system prompt was instructing the model to "do the
  actual comparison explicitly and carefully". The three failed prompt
  fixes and the successful model swap were all attempts to make a model
  better at arithmetic that should never have been delegated to any model.
  A deterministic pre-check (`internal/ambiguity/daterange.go`) now parses
  explicit date forms in Go and: refuses a question whose explicit dates
  all fall outside the window **before any model call** (zero tokens, the
  refusal worded deterministically from the real facts, instrumented
  honestly as a no-model interaction); and, for explicit in-range dates,
  hands the model each reference's already-computed verdict as settled
  fact, so the model never re-derives range inclusion for a date Go could
  parse. Verified live: "What was our margin in July 2023?" refuses in
  ~9ms with zero tokens; "What was our margin for July 2026?" — the exact
  question that triggered the incident below — answers correctly.
- **Clarified** what the 2026-08-29 Sonnet swap actually bought: it stands,
  but only for what is genuinely a language job — relative/vague/year-less
  date resolution, date phrasings the conservative Go parser deliberately
  doesn't attempt, and the answerable/ambiguous/unanswerable judgment
  itself. The claim that the swap "fixed" the date-comparison bug is
  withdrawn: it made a model more often right at arithmetic the
  architecture says no model should perform. `internal/llmclient/cost.go`'s
  doc comment — the account every other doc points to — carries the same
  correction.

---

## 2026-08-29 — A critical, multi-year data-scale bug hunt

Extending the live dataset from the 14-day fixture to a 730-day synthetic
history (`backend/cmd/gendata`) surfaced real bugs the original build never
had reason to trigger. 5 commits.

- **Fixed** a genuine Claude Haiku 4.5 reasoning limit: the ambiguity gate
  repeatedly misclassified fully in-range, explicitly dated questions (e.g.
  "July 2026" inside a `2024-08-01..2026-08-14` window) as unanswerable once
  the data spanned multiple calendar years. Three prompt-only fixes were
  tried and verified NOT to resolve it; an A/B test swapping the same call
  to Claude Sonnet 5 fixed it on the first try. `ModelAmbiguityGate` moved
  to Sonnet 5 permanently — Claude Haiku 4.5 stays on the narrower
  paraphrase-match cache classifier (`ModelParaphraseMatch`, new constant),
  which showed no evidence of the same bug. Constitution amended to 1.2.0 to
  record the change. *(Corrected by the entry above: the underlying date
  comparison was arithmetic that should never have been a model's job; the
  deterministic pre-check is the real fix, and the swap stands only for the
  genuinely linguistic residual.)*
- **Fixed** a related, compounding bug in the same code path: the writer
  pass's output-token truncation wasn't detected before its (incomplete)
  JSON was parsed, letting garbled cut-off text leak into user-facing
  refusal reasons. Both `Classify` and `writeBetterText` now check
  `StopReason == MaxTokens` explicitly and treat any truncation as a hard
  failure, regardless of whether the cut-off text happens to parse; the
  writer's token cap was also raised 512→768 as a safety margin.
  `internal/livedata`'s test suite, previously untested against a
  real-sized live dataset, was hardened with a `withFreshDir` helper so its
  own tests can no longer operate destructively on this machine's real
  synthetic data.
- **Fixed** a critical data-integrity bug: `cmd/gendata`'s `startDate` was
  set one day after the fixture window ends, generating synthetic history
  through 2028-08-13 — over a year into the real-world future relative to
  the interview date. Moved `startDate` back two years so the generated
  history ends the day before the fixture begins, landing entirely in the
  past.
- **Fixed** Close/Promotions charts, which were built and tested against
  the ~14-day fixture and broke silently at 745 days of real data:
  fixed-pixel-per-bar SVG layouts either compressed into unreadable slivers
  or overflowed their containers. Both charts now compute a dynamic width
  and bucket/thin their labels based on the actual data volume; the Close
  page's per-day discrepancy badges collapse into a single "×N" summary
  that expands on click instead of repeating once per flagged day.
- **Added** a Help page (`/help`) with four content sections — what the app
  does, what you can ask it, how to use it, and why it sometimes refuses —
  reusing the chat's own example-question and capability-summary constants
  rather than retyping them.

---

## 2026-08-28 — Build day: engine to product, evaluated and fixed live

The overnight build (39 tasks across 4 user stories), a full evaluation-driven
fix cycle, three roadmap specs shipped same-day, a UX/accessibility pass, and
an evening polish session. ~106 commits.

### Deterministic core, brand, and the ask pipeline (continuing T011–T027)

- **Added** CSV ingestion (`internal/ingest`) for the delivery, POS, and
  supplier-cost exports, written test-first against the fixture set's four
  deliberate irregularities (duplicate order, cross-week refund, the missing
  2026-08-08 day, ISO-vs-`DD/MM/YYYY` date formats) — plus `internal/money`
  for exact fixed-point cents arithmetic after confirming a naive `float64`
  commission calc misrounds real fixture values (`34.50 * 23% = 7.935`).
- **Added** the deterministic reconciliation engine (`internal/reconcile`):
  duplicate collapsing, refund netting against `order_date`, missing-source
  handling that flags rather than zeroes, and commission recomputed from
  `subtotal * rate` and cross-checked against each file's own commission
  column. All golden values cross-verified against an independent
  from-scratch Python computation.
- **Added** persistence and the ingest→reconcile→persist CLI pipeline,
  proven against a live Postgres instance — the 14-day fixture period
  reconciles to a 482.05 total margin, matching the independent Python
  check exactly.
- **Added** the finalized brand (batwing café-door mark, prosperity-emerald
  primary color) and the Reconciliation-category badges (Clean Close,
  Discrepancy Catcher), plus provenance-citation and running-cost-panel
  components.
- **Added** the reconciliation chat UI (grounded answer / clarification /
  refusal as three visually distinct flows) and promotion-ROI reconciliation
  with its MCP tools (`get_promotion_roi`, `list_negative_roi_promotions`).
- **Added** the 3 core reconciliation MCP tools (`get_daily_summary`,
  `get_margin_delta`, `list_discrepancies`) behind a shared
  timeout-and-call-cap middleware; the Claude Haiku 4.5 ambiguity gate; the
  Sonnet 5 explanation step (a real tool-calling loop over the in-process MCP
  client); and `POST /api/ask`, wiring gate → explain → instrumentation on
  every branch.
- **Added** full HTTP integration (MCP server + ask handler + badges in one
  process). Verified live end-to-end against real Postgres and a real
  Anthropic key — cumulative spend after integration: **$0.0146**.
- **Fixed** a test-isolation bug where an integration test used a real
  fixture date as its own cleanup key and was deleting real pipeline data;
  retargeted to a sentinel date.
- **Logged** two real mistakes to the running build log: two agents
  concurrently editing `internal/mcptools` (resolved by conforming to the
  already-established convention rather than duplicating it), and
  `go test ./...` without `-v` silently hiding that every env-gated live
  test had been skipped (shell env vars weren't sourced by the
  non-interactive tool shell).

### Routed shell, charts, and the first evaluation run (T033–T039)

- **Added** the routed app shell (sidebar nav, `/`, `/close`, `/ask`,
  `/promotions`), the home page (capability tiles as navigation), and
  chart-first `/close`/`/promotions` pages with hand-rolled diverging bar
  charts — a real CVD-separation failure in the `--success`/`--destructive`
  color pair was caught by the dataviz palette validator (ΔE 5.4 light /
  0.8 dark, floor 6.0) and mitigated with non-color encoding (zero baseline,
  text-labeled legend, signed labels) rather than silently shipped.
- **Added** the promptfoo evaluation harness (35 real questions: accuracy,
  consistency, refusal) and ran it live against the backend with real
  Anthropic calls. First real numbers: **10/15 accuracy, 0/5 consistency
  sets fully agreed, 4/5 correct refusals**, cost **$0.286** for the run.
  Root cause of nearly every failure: the model didn't reliably infer 2026
  when a question omitted the year — sometimes hallucinating "2024" with
  full confidence.
- **Fixed** date-year grounding: gate and explain now receive the real
  min/max date range from the database (not a hardcoded literal) and an
  explicit instruction that a year-less date must resolve into the one year
  the data spans. Regression test run live 3/3 against Haiku 4.5.
- **Fixed** campaign lookup: `get_promotion_roi` now falls back to a
  bounded, typed fuzzy match (`matchCampaignID`) against the real persisted
  campaign-id set, so a shortened name ("LUNCHFIX") or full display name
  resolves instead of triggering a hallucinated refusal. Also fixed the
  ambiguity gate itself, which was independently refusing shortened
  campaign references before the question ever reached the tool layer.
- **Re-ran** the identical 35-question suite after both fixes: refusal
  4/5→5/5, the year-hallucination pattern fully eliminated, but accuracy
  moved 10/15→9/15 as three previously-passing questions surfaced an
  unrelated quirk (stating two platforms' figures without a combined
  total) — reported plainly rather than netted against the wins.
- **Fixed** the delivery-revenue combined-total regression by adding a real
  deterministic `total_delivery_gross_sales` field (accuracy back to
  10/15), and discovered — while verifying "both servers are live" —
  that the frontend was **still calling an in-memory mock**, not the real
  backend, and the backend had no CORS headers at all. Both fixed: dev CORS
  allowlist, `AskPage` rewired to `POST /api/ask` for real, with real
  per-call cost data replacing hardcoded placeholder figures.
- **Removed** the orphaned `ReportPage` and the retired single-page
  `MarginCopilotApp` once all three routed pages had real content.
- All 39 build-order tasks marked complete after verifying each phase's
  real artifacts, not as a batch check-off.

### Chat UX overhaul, live data pages, and the answer cache

- **Added**, in one large verified pass: dynamic in-chat visualization
  (chart type chosen deterministically in Go from tool name/shape, never a
  second model call); gamification points derived at read time from earned
  badges; real `GET /api/reconciliation` and `GET /api/promotions` endpoints
  backing the Close/Promotions pages with live data instead of hardcoded
  JSX; and an exact-match LLM answer cache, kept in a separate ledger table
  so a cache hit is never conflated with a real paid interaction.
- **Fixed** a real chat scroll bug (missing `min-h-0` on a flex Radix
  `ScrollArea`) and a concurrency bug (a single shared `pgx.Conn` across
  concurrent handlers, fixed by switching to `pgxpool`).
- **Fixed**, in a second UX pass: conversation memory (a clarification
  reply like "yes" was being refused because nothing carried the original
  question's context — fixed with a typed `PendingClarification` field);
  the scroll bug for real this time (the panel was a fixed 576px letterbox
  in a 982px viewport, screenshotted and measured, not just diffed); and
  answers rendering raw markdown asterisks (`**$328.82**`) instead of
  formatted text.
- **Added** real, tool-grounded capability guidance on empty/refusal states,
  a `/points` page, and localStorage conversation persistence with a
  capped recent-threads list.

### Design system rebuild and an accessibility pass

- **Rebuilt** `docs/architecture.html` as a 3-tab design system reference
  (design tokens, reconciliation engine, full architecture) after finding a
  real SVG layout bug (two connector lines routed through the same
  coordinates as later-added text, striking through labels). Also caught
  and fixed a stale doc claiming the abandoned iFood-red as the primary
  color, months after the brand had pivoted to prosperity-emerald.
- **Scored** the app 5.6/10 against Nielsen's heuristics and found concrete
  defects: four deterministic figures compressed into one 12px caption on
  `/close`, a ~250-word points essay pushing all navigation below the fold
  on Home, and 6 serious axe-core accessibility violations. **Fixed**: KPI
  rails replace the compressed captions; the points essay moved to its own
  page; axe violations went **6 → 0** across all 5 routes in both themes;
  contrast fixed at the token level (`--muted-foreground` 4.46:1→4.88:1,
  `--success-text` 4.49:1→6.40:1). Honestly disclosed: hard-coded-value
  lint went 61→75 (offset by a new `--text-micro` token, a net accessibility
  gain not a clean number).
- **Added** a dedicated Points summary section on Home, extracting the
  composition bar into a shared component so Home and `/points` render one
  implementation instead of two that could drift.

### Four roadmap items scoped and three shipped same day

- **Specified** badge expansion, the platform comparator, a paraphrase-aware
  answer cache, and multi-tenancy — each re-scoped from the original
  "not built" roadmap notes to what's honestly buildable. Multi-tenant was
  deliberately kept spec/RFC-only: a tenant-isolation defect is a
  data-breach class of bug, gated on explicit review rather than built same
  day.
- **Implemented** badge expansion (spec 002): Growth badges (real positive
  attributable ROI), Engagement badges (a genuinely new, DB-deduped
  `usage_event` table — verified live to show zero badges on a fresh table,
  never a fabricated streak), and Campaign-Creation badges (a real
  `POST /api/promotions` action logging a promotion that replaces a flagged
  negative-ROI one, server-side re-verified rather than trusting the
  client).
- **Implemented** the cross-platform economics comparator (spec 003):
  discovered mid-build that per-platform commission wasn't actually
  recoverable from what was persisted (one combined total), so added a
  migration and backfilled real per-order data rather than approximating
  with nominal rates — revealing iFood's **true effective rate is 22.06%**,
  not the nominal 23%, because of a real fixture refund netting against its
  own reversal. New `compare_platform_economics` MCP tool (6th typed tool)
  and `/platforms` page.
- **Implemented** the paraphrase-aware answer cache (spec 004): a bounded
  Haiku classification call against up to 20 recent cached questions (no
  embeddings — Anthropic has no first-party embeddings API, a documented
  vendor-constraint decision), double-verified against the live cache
  before ever serving a match, with real and avoided cost kept as two
  distinct, un-netted numbers.
- **Fixed** a Ports & Adapters diagram overlap and viewport-overflow bug in
  the presentation, and updated `architecture.html` to reflect all three
  newly-shipped features (badges, comparator, paraphrase cache) after it
  had gone stale mid-session.
- **Rebuilt** the presentation as a real 22-slide deck (`make-slide`
  skill), replacing the earlier landing-page format, with content updated
  to current shipped reality (live badge/point values, live comparator
  figures).

### Hardening, cost-accounting fixes, and documentation completeness

- **Added** cost-sheet upload (spec 007): preview/commit/template endpoints
  reusing the existing tolerant parser, writing to a fixed filename inside a
  git-ignored directory (never the client's uploaded filename, making path
  traversal structurally impossible), clearing the answer cache before
  re-ingesting.
- **Added** the project README, an OpenAPI spec + interactive Swagger docs
  for all 10 real routes (grounded against live curl responses, not
  inferred from handler code), MCP/skills documentation citing real commits
  and file paths, and a day/period picker on the Close page.
- **Added** a conditional Sonnet 5 writer pass that rewrites
  clarifying-question/refusal text (Haiku still owns classification alone
  and cannot be overridden).
- **Fixed**, in a self-review pass: `internal/money` had zero direct test
  coverage — added tests and caught a real bug (`ParseFixedPoint` silently
  accepted a trailing-dot value like `"34."` as zero-fraction instead of
  rejecting it); date-format resolution (MM/DD vs DD/MM) now locks per
  file instead of per row; the answer cache gained a schema-version column
  so a stale shape is treated as a miss, not served.
- **Fixed** `explain`'s turn-level error path discarding already-billed
  spend from earlier turns in the loop (a real API cost was going
  unrecorded); added a guard against narrating a currency-shaped answer
  that made zero tool calls and collected zero provenance.
- **Fixed** a timeout-misreporting bug: a canceled parent context (client
  disconnect) was being reported identically to a genuine 5s
  deadline-exceeded timeout. Added fake-`Querier` tests so MCP tool
  refusal paths run in default `go test ./...`, not only when
  `DATABASE_URL` is set.
- **Required** non-empty provenance on every "answered" assertion in the
  accuracy/consistency eval configs — previously a right number reached via
  a wrong or absent citation trail still passed.
- **Fixed** the floating chat composer overlapping the last message (the
  reserved padding was a fixed value; the composer's real height is
  dynamic) and rewrote refusal/error copy into the Steward's first-person
  voice per the ux-writing skill's error-copy formula.
- **Fixed** a truncated-response bug caught live: a "show me the day with
  the most profit" question was served as an unfinished planning sentence
  cut off by the output-token cap. Any `MaxTokens` stop with zero tool
  calls is now refused unconditionally.
- **Replaced** the deck's one U.S.-specific statistic with real, sourced
  Brazil/UK data matching the platforms actually used elsewhere in the deck.

### PWA, CORS, and launch polish

- **Added** an installable PWA (real manifest + service worker, precaching
  only hashed build assets — never `/api/*`, so every figure still comes
  from a live request) and then fixed it not appearing in dev mode
  (`vite-plugin-pwa` disables itself under `vite dev` by default).
- **Fixed** dev CORS being hard-coded to Vite's port only, which broke the
  installed PWA build (a different port) with a real browser CORS error —
  now reflects back any `localhost`/`127.0.0.1` origin.
- **Added**, then **fixed**, the app-open animation: the batwing-door swing
  first played on the small sidebar logo (correct but imperceptible at
  36–40px — root-caused live after a user report ruled out caching),
  replaced with a full-viewport launch splash at the presentation's own
  140px scale. The same door-swing now loops on the chat's assistant avatar
  while a real model call is in flight, and goes static the instant a real
  answer arrives.
- **Fixed** the composer overlap bug a second time: the existing
  `ResizeObserver` used the default `content-box`, which by definition
  excludes padding, so it never fired when only `padding-bottom` changed
  (e.g. opening the suggestions panel). Verified before/after: +231px
  overlap → clean −24px gap.
- **Added** a real fullscreen toggle button (the Fullscreen API requires a
  user gesture in every major browser, so no auto-fullscreen is possible)
  and shrank the composer's backdrop gradient, which was fading real
  conversation content that had nothing to do with the composer.

### Deterministic follow-ups, the 7th MCP tool, and an evaluative-language fix

- **Added** `compare_platform_economics` to the chat's own example-question
  list (it had shipped without ever being surfaced to the user) and
  `get_period_totals` — the 7th typed MCP tool — closing a real observed
  failure where "which day had the most profit" burned through the model's
  turn budget calling `get_daily_summary` once per day.
- **Fixed** A10 (a false clarify on evaluative language like
  "underperforming"), grounded in two published LLM-behavior papers plus
  this project's own observed turn-budget blowup: the gate now classifies
  evaluative-sounding questions as answerable when a typed tool already
  defines the term, rather than asking the user to define a threshold the
  product could already compute. Real before/after on the identical
  35-question suite: **accuracy 11/15 → 13/15, refusal 4/5 → 5/5**.
- **Added** deterministic follow-up chips after every answered question —
  generated purely in Go from the real tool call that grounded the answer,
  never a second model call — and a typed one-hop follow-up mechanism for
  answers (not just clarifications), so "and the day before?" resolves
  correctly without an accumulating transcript.
- **Fixed** part of A15: refunds weren't being attributed to their real
  source platform even though the data existed at the point they were
  accumulated — added `refunds_by_source`, mirroring the earlier
  per-platform-commission fix.
- **Added** a missing chart-type case for `get_period_totals` (one of the
  richer tool results was silently rendering no chart at all), and updated
  the MCP/skills reference, technical RFC, `architecture.html`, the
  presentation (real 13/15 eval delta, follow-up-chip demo slide), and the
  OpenAPI spec/`frontend.md` for everything shipped in this pass.

### Chat warmth, presentation restructuring, points-as-payment, and Settings

- **Added** a chat warmth pass: the refusal/error bubble recolored from red
  to brand green (a refusal isn't a system failure), warmer narration
  copy, and a deterministic zero-cost path for meta-questions like "how can
  you help me?" answered from a hand-written capability list before either
  model call runs.
- **Audited** the presentation for staleness (two different superseded
  spend figures in the same deck, "6 typed tools" after the 7th had
  shipped) and added a chat-warmth slide plus a cost-management roadmap
  slide drafted by Claude Fable 5 — reserved for this one high-stakes,
  interviewer-facing judgment call per this project's own model-tier
  discipline.
- **Restructured** the deck after a real audience dry run: consolidated the
  OKR (previously split across three slides) onto one slide with all four
  KRs together, separated KR1's committed target from disclosed context
  instead of leading with its best sub-metric, added real sourced evidence
  to every Hypotheses claim, unified the roadmap, and removed a
  self-imposed "$5 spend cap" framing that doesn't exist in code.
- **Fixed** truncated/overlapping labels in the architecture request-flow
  diagram (label text wider than its gap, clipped by a neighboring box).
- **Added** points-as-payment: `POST /api/promotions` now accepts
  `payment_method: "points"`, verified server-side against the real
  earned-minus-spent balance at a fixed, disclosed 10 cents/point rate,
  refusing on insufficient balance rather than trusting the client.
- **Fixed** the promotion ROI chart conflating two different facts as one
  visual: a freshly-logged, not-yet-checked promotion was rendering with
  the same "refused" dashed bar as a permanently-refused campaign. Now
  renders with no bar (but still listed in the table) until an attribution
  attempt actually happens.
- **Added** a real Settings page — only the one real display preference
  (fullscreen), an "About this build" panel linking real hosted docs, and a
  "Not built yet" panel naming only capabilities already documented as
  absent elsewhere (auth, tenant isolation, rate limiting). Verified no
  dark-mode/theme wiring exists anywhere in the frontend before deciding
  not to add a toggle for it.
- **Added** a 2-year (730-day) synthetic operating-history generator
  (`cmd/gendata`) into the live-exploration dataset (kept fully separate
  from the eval harness's small fixture set): a logistic growth curve from
  $14,000/mo to $33,500/mo gross revenue, ingested live with zero errors,
  25 promotion campaigns with a real positive/negative ROI mix.

---

## 2026-08-27 — Planning day: spec-driven strategy, constitution, and first build tasks

The strategy/spec-kit foundation and the earliest implementation tasks
(T001–T010) before the deterministic engine existed. ~35 commits.

### Strategy, constitution, and spec-kit setup

- **Added** the initial project scaffold, an architecture presentation
  outlining the deterministic/probabilistic split, a toolchain install
  script, and the GitHub Spec Kit (SDD) framework with Claude Code
  integration.
- **Added** `docs/product-strategy.md` and ratified **Constitution v1.0.0**
  (deterministic/probabilistic split, refusal discipline, typed tools,
  provenance, test-first, instrumentation) — later revised to **v1.1.0**
  when the LLM vendor switched from OpenAI to Anthropic (Claude Haiku 4.5
  for the ambiguity gate, Sonnet 5 for explanation).
- **Added** the day-by-day plan through the interview date, with a running
  mistakes log, and made product strategy visible in the plan via a recap
  section and skill-driven checkpoints.

### OKR framing and the 001 spec

- **Reframed** the objective as an OKR (grow profitability via revenue
  without eroding margin) and defined 4 Key Results (trust, margin
  protection, revenue growth, token discipline).
- **Recorded** a structured comparison of five candidate product concepts
  and the decision to build the combined reconciliation + promo-ROI
  product ("Product A") rather than either half alone.
- **Added** feature spec `001-margin-reconciliation-qa`, expanded with the
  promo-ROI user story (KR3), followed by Phase 0/1 plan artifacts
  (research, data model, MCP contracts, quickstart), an AI-by-Design
  justification for where the model is and isn't used, market sizing and
  launch strategy (Prosus vs. non-Prosus segments), a module architecture
  diagram, the badge system design, `tasks.md` (39 tasks across 4 user
  stories), and PRD/DOR/Technical RFC documents for the eventual
  presentation.

### First build tasks (T001–T010)

- **Added** the Go backend module (core dependencies: `mcp-go`, `pgx/v5`,
  `testify`, `anthropic-sdk-go`), the React/Vite/Tailwind/shadcn frontend
  scaffold, `docker-compose` for local Postgres, the `golang-migrate`
  schema for the three core tables (with DB-level `CHECK` constraints as a
  second gate on top of the Go layer), `sqlc`-generated typed Postgres
  access, and the shared Anthropic API client wrapper with deterministic
  cost estimation (Haiku 4.5 $1/$5, Sonnet 5 $2/$10 per MTok).
- **Fixed** a real bug hit while scaffolding: the shadcn CLI didn't resolve
  the `@/*` alias (declared only in `tsconfig.app.json`), writing a
  literal `./@/components/ui/button.tsx` directory instead of the intended
  path.
- **Generated** two weeks of fixture data (delivery, POS, cost-sheet,
  promotion exports) with five deliberate irregularities baked in on
  purpose: a duplicate order, a refund crossing into the next week, a
  fully missing delivery-platform day, an inconsistent date format between
  files, and one promotion with no attributable orders at all.
- **Added** the instrumentation writer (`internal/instrumentation`),
  written test-first against a package that didn't exist yet, confirmed
  red before being made green.
- **Logged** real friction from the overnight build's Setup+Foundational
  phase and documented the netting-convention decision plus cost-bounded
  live test scenarios, resolving the day's open DOR blockers.

---

*Compiled from `git log` on 2026-08-29. For the fuller, evaluation-grade
narrative behind any entry above — including exact before/after numbers,
root-cause traces, and what was deliberately not built — see
`docs/product-strategy.md` and `docs/plan.md`.*
