# Feature Specification: Connector trading-day variance, and cost-sheet upload triggering a connector sync

**Feature Branch**: `feature/connector-variance-and-upload-sync`

**Created**: 2026-08-30 (retroactively — see plan.md's "How this spec came to exist")

**Status**: Shipped (spec written after the fact)

**Input**: Conversational request, not a spec-kit `/speckit-specify` run. Two
related complaints raised together: (1) uploading a cost sheet reconciled
against whatever platform revenue already happened to be on file, not the
revenue that actually belongs to the uploaded dates; (2) every simulated
connector day reconciled to a healthy, flat-positive margin, which is not
what a real restaurant's trading looks like and is a weak demo for a product
whose whole job is noticing when something went wrong.

## Spec number

`014`. `013-bff-layer` is the highest spec number that exists in `specs/`
at the time this retroactive spec was written. Both this feature and
`015-column-header-filters` are being specced in the same retroactive pass,
after both were already fully merged to `main` with no spec directory ever
reserved for either — so, unlike specs 010–013, there was no branch-name
race to resolve; `014` and `015` were simply assigned in the order the two
retroactive write-ups were done. `feature/column-header-filters` actually
merged to `main` earlier in real time (PR #3) than
`feature/connector-variance-and-upload-sync` did (PR #12); the spec numbers
do not track that, and nothing depends on them doing so.

## Background

This bundles two changes that shipped together in one branch and one
CHANGELOG entry (2026-08-30, "One upload, one reconciliation..."), because
the second is arguably a bug the first one would otherwise paper over: a
one-click "upload costs and see today's real margin" experience is only
honest if "today's real margin" isn't secretly incapable of being negative.

### Problem 1 — a cost-sheet upload didn't reconcile against its own dates' revenue

Before this feature: `POST /api/ingest/cost-sheet/commit` persisted supplier
costs and re-ran reconciliation; `POST /api/connectors/sync` fetched
simulated iFood/Just Eat Takeaway/POS revenue for a chosen range and re-ran
the same pipeline. Two endpoints, two tabs, two trips. Uploading today's
costs produced a margin computed against whatever revenue happened to
already be on file for those days — a real number, but not the one the
owner came to see.

### Problem 2 — simulated connector days never had a bad day

`platformconnector/seed.go` drew every day's order count from one narrow
band (`minOrdersPerDay + Intn(orderCountSpread)`), applied a flat
Friday/Saturday lift, and stopped. Measured over 363 days against the live
dataset's own cost sheet, daily gross ran $3,970–$9,131 (0.66x–1.51x of its
own mean) — a band too tight for any plausible cost sheet to push through
zero. The only losing days the connector ever produced were days the *cost
sheet itself* spiked on. A product whose entire job is telling an owner when
something went wrong was demoing on a data source where, on the revenue
side, nothing ever did.

These two problems compound: once a cost-sheet upload can pull in connector
revenue for the same dates in one action, that revenue needs to be capable
of the same real variance a CSV-ingested day already has, or the new
one-click flow becomes the easiest way to see a reconciliation that is
artificially incapable of the bad news this product exists to surface.

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Upload costs and get a complete margin in one action (Priority: P1)

An owner uploads their supplier cost sheet for a date range. Without a
second trip to the Connected Platforms tab, the margin they see afterward
already reflects the matching iFood, Just Eat Takeaway, and POS revenue for
those same dates.

**Why this priority**: It is the requested feature — the whole point of
letting an owner upload costs is that they get to see what those costs did
to margin, and "against stale revenue" is not that.

**Acceptance Scenarios**:

1. **Given** a cost sheet whose invoices span 2026-08-01..2026-08-15, **When**
   the owner commits it with the sync option left ticked, **Then** the
   response's `connector_sync` field is a populated object (not null) and
   the `after` margin snapshot reflects both the new costs and the fetched
   revenue, from one pipeline run.
2. **Given** the same upload with the sync option unticked, **When** it
   commits, **Then** `connector_sync` is `null` — a real "not asked for",
   never an empty sync object — and only the cost sheet changes.
3. **Given** a cost sheet whose date span exceeds the connector's existing
   31-day sync cap, **When** the owner commits it with the sync option
   ticked, **Then** the whole request is refused (`422
   connector_fetch_failed`) with the connector's own cap message, and the
   cost sheet on file is **not** replaced — verified live: a 124-row sheet
   spanning 89 days refused with the on-disk cost sheet unchanged before and
   after.
4. **Given** a server started without the platform connectors wired up,
   **When** a commit requests the sync anyway, **Then** it is refused
   (`connectors_unavailable`) rather than silently proceeding cost-only.

---

### User Story 2 — Consent to simulated data is current, not just present (Priority: P1)

The owner sees, at the moment they are about to act, that ticking the sync
box pulls in revenue from three sources with no real account behind them —
not only in a tab they might never visit.

**Why this priority**: `platformconnector`'s package doc already states the
simulated nature of these connections five separate times "on the way to a
number," on the reasoning that a disclosure living in exactly one place is
one that can be cropped out of a screenshot. A cost-sheet upload that
silently pulled in simulated revenue would defeat all five disclosures at
once for an owner who never opened the Connected Platforms tab.

**Acceptance Scenarios**:

1. **Given** a previewed cost sheet with a valid date range, **When** the
   preview panel renders, **Then** a checkbox is shown, pre-ticked, directly
   above the commit button, naming the exact date range it will pull and
   using the word "simulated" in its own label.
2. **Given** the API is called directly (curl, a script, any client that
   predates this feature), **When** `sync_connectors` is absent from the
   request, **Then** the default is **false** — an API has no banner to
   read, so the opt-in default lives at the UI layer, not the wire
   contract.
3. **Given** the owner unticks the checkbox, **When** they commit, **Then**
   only the cost sheet changes, exactly as it did before this feature
   existed.

---

### User Story 3 — Simulated connector days can genuinely go wrong (Priority: P1)

An owner syncing the connector — whether directly or via the new
upload-triggered path — sees days with real, causally-explained bad
trading, not a uniformly healthy line, and can read a plain-language reason
for any day that stands out.

**Why this priority**: without it, User Story 1 makes the *easiest* path
through this product (upload once, see a complete margin) also the path
most likely to hide the exact class of finding — a losing day — the product
exists to surface.

**Acceptance Scenarios**:

1. **Given** any calendar date, **When** its trading condition is computed,
   **Then** the result is one of seven states (ordinary, quiet midweek
   lull, neighbourhood event, aggregator outage, short-staffed shift,
   kitchen equipment failure, severe weather), deterministic for that date,
   and identical regardless of which platform asks or what order platforms
   are fetched in.
2. **Given** a date whose condition is not "ordinary," **When** the
   connector's preview or sync response is read, **Then** a `trading_note`
   names the cause in the owner's own words (e.g., "Kitchen equipment
   failure — limited menu for most of the day").
3. **Given** an ordinary day, **When** the same field is read, **Then** it
   is empty — printing "nothing in particular happened" on five days out of
   seven would train an owner to stop reading the column.
4. **Given** the simulated dataset across a full year, **When** it is
   scored against the live dataset's own real cost sheet, **Then** a
   material share of days (measured, not assumed) reconcile net-negative,
   not just thinner-positive.
5. **Given** a demand-suppressing condition (a lull, a staffing gap, a
   weather event), **When** a day's margin is computed, **Then** the
   day's *costs* are unaffected by the condition — the connector supplies
   revenue only; input costs come from the cost sheet and were already
   committed before the day happened.

### Edge Cases

- **A trading condition drawn for a weekday it doesn't apply to** (e.g. a
  "quiet midweek lull" landing on a Saturday roll). Collapses to the
  ordinary day rather than being re-rolled — re-rolling would make the
  result depend on how many draws it took, which would break the
  determinism this whole model exists to preserve.
- **A demand-suppressing condition landing on an otherwise strong day.**
  Not prevented — the model draws per calendar date, independent of any
  other day's outcome, matching how a real disruption actually arrives.
- **A cost-sheet upload whose date range includes a day the connector has
  never been asked about before.** No different from any other date — the
  condition and the day's revenue are pure functions of the date, not of
  fetch history.
- **A previously-synced date being re-synced after this feature shipped.**
  Its numbers change, because `connectorSeedSalt` moved `v1` → `v2`. This is
  deliberate and disclosed (see plan.md), not a silent drift.
- **The connector fetch failing partway through a multi-day range.** The
  existing spec 010/012 refusal behavior is unchanged: the whole fetch
  refuses, nothing partial is committed. This feature does not weaken that.

## Requirements *(mandatory)*

### Functional Requirements — the upload-triggered sync

- **FR-001**: `POST /api/ingest/cost-sheet/commit` MUST accept an optional
  `sync_connectors` form field. Absent, empty, or any value other than
  `true`/`1`/`yes`/`on` (case-insensitive) MUST be treated as **false**.
- **FR-002**: When the opt-in is set, the system MUST derive the calendar
  range to sync from the uploaded invoices themselves — the minimum and
  maximum `invoice_date` across every parsed row — never from a
  client-supplied range.
- **FR-003**: The connector fetch, when requested, MUST run **before** the
  cost sheet is written to disk and before the reconciliation pipeline
  runs. A fetch failure (an over-wide range, an unavailable connector) MUST
  refuse the entire commit, leaving the previously-committed cost sheet
  untouched.
- **FR-004**: When the fetch succeeds, the cost sheet write and the
  connector revenue MUST be committed through exactly **one** pipeline run
  (`RunIngestionPipelineWithConnectorOverlay`), never two sequential runs —
  an intermediate persisted state of new costs against old revenue must
  never be observable.
- **FR-005**: The commit response's `connector_sync` field MUST be a
  populated object when a sync ran and `null` when it did not — never an
  empty object standing in for "not asked."
- **FR-006**: A commit requesting the sync on a server started without the
  platform connectors wired up MUST be refused with a specific error
  (`connectors_unavailable`), not silently downgraded to a cost-only
  commit.
- **FR-007**: The frontend's sync checkbox MUST default to checked, MUST
  render inside the preview panel adjacent to the commit action, MUST name
  the exact date range it will pull, and MUST say "simulated" in its own
  label.
- **FR-008**: The existing `POST /api/connectors/sync` endpoint and its
  standalone Connected Platforms tab flow MUST remain unchanged and fully
  functional — this feature adds a second path to the same underlying
  connector fetch, not a replacement for the first.

### Functional Requirements — trading-day variance

- **FR-009**: The system MUST model each simulated calendar date as having
  one of a fixed, named set of trading conditions, drawn deterministically
  from the date alone (not from date-and-platform), so that every
  simulated source (iFood, Just Eat Takeaway, POS) agrees about what kind
  of day a given date was.
- **FR-010**: Each trading condition MUST carry independent demand
  multipliers for delivery-channel orders and in-house/dine-in orders,
  because a real disruption does not hit both sides of the business
  equally (an aggregator outage barely touches the dining room; a storm
  empties the dining room faster than it empties the delivery queue).
- **FR-011**: A condition drawn for a weekday it is not eligible on MUST
  collapse to the ordinary condition rather than trigger a re-roll.
- **FR-012**: Every non-ordinary condition MUST carry a stated,
  owner-legible cause (a label), surfaced through the API as
  `trading_note`. The ordinary condition MUST carry no label.
- **FR-013**: A condition that suppresses demand MUST NOT alter the input
  costs a day reconciles against — the connector supplies revenue only; no
  cost-side fiction (an invented invoice, a fabricated discount) may be
  introduced to manufacture a loss.
- **FR-014**: The flat weekday/weekend demand shape used before this
  feature MUST be replaced by a full seven-day demand curve, tuned to
  preserve approximately the same weekly mean as the shape it replaces —
  this changes the *shape* of a simulated week, not the *size* of the
  simulated business.
- **FR-015**: Changing the trading-condition model MUST be recorded as a
  deliberate, visible act (a versioned seed constant), and MUST NOT alter
  any number `cmd/gendata` produces for the historical dataset — the two
  generators must remain provably independent.
- **FR-016**: Every modelling constant in the trading-condition table
  (weights, multipliers, eligibility) MUST be documented as a stated
  choice, not presented as a measured fact.

### Key Entities

- **Trading condition**: One of seven named states a simulated calendar
  date can be in (ordinary, quiet midweek lull, neighbourhood event,
  aggregator outage, short-staffed shift, kitchen equipment failure, severe
  weather), each carrying a label, a weight, weekday eligibility, and
  demand/refund multipliers.
- **Connector sync summary**: The commit response's account of what a
  triggered sync fetched and changed — orders synced, duplicates resolved,
  unresolved overlaps — reusing the same shape `POST /api/connectors/sync`
  already returns.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A cost-sheet commit with the sync option ticked produces
  exactly one persisted reconciliation state, never a briefly-observable
  intermediate one (proven by the single-pipeline-run design, not merely
  asserted).
- **SC-002**: An over-wide connector range, requested during a commit,
  leaves the previously-committed cost sheet byte-identical before and
  after the refused request. Verified live: a 124-row sheet spanning
  2026-06-02..2026-08-29 (89 days, over the 31-day cap) with the opt-in
  returns `422 connector_fetch_failed`, and the on-disk cost sheet still
  carries all 25 months of rows before and after.
- **SC-003**: Measured over 363 simulated days against the live dataset's
  real cost sheet: losing days rose from 35/363 (9.6%, pre-variance model)
  to 55/363 (15.2%, post-variance model) — closer to, though not equal to,
  the historical CSV dataset's own 20.1% over the same window, and not
  further tuned to close that gap once the business's underlying cost
  structure made doing so require unrealistic demand cuts.
- **SC-004**: The same 363-day measurement shows mean daily gross moving
  from $6,037.93 to $5,354.83 (a deliberate side effect of the new demand
  shape's lower mean) and mean daily margin moving from $3,495.11 to
  $2,856.34, with the quietest day falling from 0.66x mean to 0.28x mean
  and the busiest day rising from 1.51x mean to 2.36x mean — evidence of
  real variance, not just a shifted mean.
- **SC-005**: `cmd/gendata`'s four regenerated CSVs are byte-identical
  (same MD5s) before and after this change, reconciling to the same
  $1,078,340.64 total margin across the same 759 days — proof the two
  simulated data sources remain independent.
- **SC-006**: Live smoke test — uploading a real 29-day August cost sheet
  with the sync opt-in ticked moves negative days from 3/29 (cost sheet
  alone) to 5/29 (cost sheet plus connector pull), each of the two new
  losing days carrying a named trading-condition cause.

## Assumptions

Chosen rather than clarified, in the same spirit as spec 010's and spec
012's own Assumptions sections.

- **Opt-in at the API, default-on in the UI, are not in tension.** They are
  the same "consent must be current, not just theoretically available"
  decision applied at the two layers each is honest at — see plan.md for
  the full argument.
- **The loss-rate gap against the historical dataset (15.2% vs. 20.1%) is
  left open rather than closed by further tuning.** Closing it would have
  required demand cuts deep enough to break this simulated business's
  established dollar scale; a partial, honestly-reported improvement was
  preferred to forcing a number to match by construction.
- **One aggregator's outage is modelled as good for the dining room, not
  neutral.** An app being down pushes some of that demand toward walk-ins
  and phone orders in the real world; the `inHouseMult` for
  `conditionAggregatorOutage` is set above 1.0 to reflect that rather than
  leaving the dining room untouched.
- **No fourth trading condition, no per-platform conditions.** Seven states,
  drawn once per date, for the same reason spec 012 fixed the connector
  roster at exactly three sources: the model needs to be small enough that
  a reader can hold the whole table in their head and predict a day's
  outcome from it.
