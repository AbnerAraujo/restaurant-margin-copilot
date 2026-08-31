# Implementation Plan: Connector trading-day variance, and cost-sheet upload triggering a connector sync

**Spec**: [spec.md](./spec.md) · **Status**: Already implemented and shipped — this plan documents it after the fact

## How this spec came to exist (read this first)

**This spec and plan were written after the code they describe was already
built, reviewed, and merged to `main`.** The feature was requested and
built directly from a conversational ask, without going through this
project's own front door
(`/speckit-specify` → `/speckit-plan` → `/speckit-tasks` → `/speckit-implement`)
that every other numbered spec in `specs/` claims to have followed. That is
a real, undisclosed-until-now gap between this project's stated practice —
README.md: "everything here was produced through a real spec-driven
process ... not written after the fact" — and what actually happened for
this feature.

This document is being written now, retroactively, to close that gap
honestly rather than to quietly backfill it. Two things follow from that:

1. **This is not a plan someone read before writing code.** It is a
   description of the actual shipped implementation, written by reading
   the merged diff, the CHANGELOG entry, and the code itself, then
   organized into the shape a plan.md takes. Any place below that sounds
   like a forward-looking design decision is, in fact, a decision that was
   already made and is being explained after the fact.
2. **This is reported as a process fact, not hidden as an implementation
   detail.** Constitution Principle V requires the development workflow
   and its results — including failures — to be reported rather than
   hidden; nothing in that principle is scoped to code bugs specifically,
   and the same standard is applied here to a *process* deviation. Writing
   this spec as though it had existed on 2026-08-30 before the code did
   would be fabricating provenance about the project's own process, which
   is exactly the category of dishonesty this codebase spends real effort
   guarding against everywhere else (`internal/answerverify`'s numeric
   validation, `platformconnector`'s five-fold simulation disclosure,
   spec 010's "no source here quantifies X precisely" style of tagged
   assumption). A retroactive spec that pretended otherwise would be a
   smaller version of the same failure.

The content below is accurate to what actually shipped, sourced from the
real code in `backend/internal/platformconnector/seed.go` and
`backend/internal/httpapi/ingest_cost_sheet.go`, the real frontend in
`frontend/src/components/Upload/CostSheetTab.tsx`, and the real CHANGELOG.md
entry dated 2026-08-30 ("One upload, one reconciliation: cost-sheet commit
can pull its own dates' revenue, and simulated days stop being uniformly
profitable"). It is not idealized, and it does not add scope the shipped
code doesn't have.

## Technical Context

Two additions layered on top of spec 010 (the connector proxy) and spec 007
(cost-sheet upload), touching three files directly and one indirectly:

1. `backend/internal/httpapi/ingest_cost_sheet.go` — the commit handler
   gains an opt-in connector fetch, composed with the existing cost-sheet
   write into one pipeline run.
2. `backend/internal/platformconnector/seed.go` — the day-generation model
   gains a weighted trading-condition table and a full weekday demand
   curve, replacing a flat healthy band and a flat weekend lift.
3. `frontend/src/components/Upload/CostSheetTab.tsx` — a pre-ticked,
   clearly-labeled checkbox in the preview panel.
4. `internal/reconcile` / `internal/pipeline` — unchanged in shape;
   `ConnectorOverlay` and `RunIngestionPipelineWithConnectorOverlay`
   already existed from spec 012 and are reused, not extended.

Zero model involvement anywhere in this feature.

## Constitution Check

- **Principle I (deterministic core)**: No arithmetic is delegated to a
  model anywhere in this feature. The trading-condition draw is a weighted
  table lookup seeded from the date (`seededRNG`), not a probabilistic
  scoring function in the "estimate" sense — it is a deterministic,
  reproducible function of `(namespace, date)`, and its output is
  documented as a modelling choice rather than a measurement. ✅
- **Principle II (refuse rather than guess)**: FR-003's atomic-refusal
  behavior is a direct application of this principle to a two-part write:
  an over-wide range or an unavailable connector refuses the *whole*
  commit rather than half-completing it and reporting the failure in a
  field nobody reads. ✅
- **Principle III (typed tools)**: No MCP tool touched. The sync is an
  owner-initiated HTTP action behind an explicit opt-in, as spec 010
  established for the standalone sync endpoint. ✅
- **Principle IV (provenance)**: The commit response's `connector_sync`
  field is populated or `null`, never a synthesized "empty sync" — a real
  absence is reported as absence. The simulated nature of the pulled
  revenue is disclosed a sixth time (beyond spec 010's five) at the exact
  point of the new opt-in, per FR-007. ✅
- **Honesty (project-wide, not a numbered principle but enforced
  throughout)**: the trading-condition model's every constant is commented
  as a stated choice, following spec 010's and `cmd/gendata`'s own
  precedent of tagging judgment calls as judgment calls rather than
  presenting them as sourced facts.

No violations requiring justification. (No Constitution Check ran before
this code was written, because no plan existed before this code was
written — see "How this spec came to exist" above. This section states
what a contemporaneous check would have found, checked against the actual
shipped code.)

## Part 1 — the upload-triggered connector sync

### The composition decision, as actually built

The connector fetch is orchestrated inside
`internal/httpapi.HandleCommitCostSheet`, not inside `internal/pipeline`.
`internal/pipeline` already accepted a `ConnectorOverlay` (from spec 012)
and `internal/platformconnector` already produced one; entangling the two
packages so a cost-sheet ingest could reach into a connector fetch directly
would make the deterministic core depend on the integration layer that
feeds it — backwards — and would put "did the user tick a box" inside a
package whose job is arithmetic. Orchestrating two existing pipelines from
one request is exactly what `internal/httpapi` is for (per `CLAUDE.md`:
"request shaping, orchestration, and rendering ... no arithmetic, no
domain rules").

### Ordering, and why it's load-bearing

The actual code (`HandleCommitCostSheet`) runs the connector fetch
**before** `ingestMu` is taken and **before** a single byte of the upload is
written to `livedata.Dir`. This is not incidental — it is what makes
FR-003's refusal atomic. The alternative (commit the cost sheet first, then
attempt the sync and report a failure in a response field) would
half-perform a combined action the owner asked for as one thing, in the
direction that changes financial numbers — the one direction this
product's own stated bar (`CLAUDE.md`: "a confidently wrong report is
worse than a refusal") treats as worst.

### `wantsConnectorSync` — the opt-in/default split, as it actually reads

```go
func wantsConnectorSync(r *http.Request) bool {
    switch strings.ToLower(strings.TrimSpace(r.FormValue("sync_connectors"))) {
    case "true", "1", "yes", "on":
        return true
    default:
        return false
    }
}
```

Absent-means-false at the API, pre-ticked-and-visible at the UI. The
function's own doc comment (quoted here because it is the clearest
statement of the reasoning that exists anywhere in the codebase) argues
that "does not require a second trip" and "happens without being asked"
are different properties, and that the difference matters more here than
usual because the revenue being pulled in is simulated. The two layers are
not a compromise between two conflicting instincts — they are the same
"consent must be current, not merely available somewhere" decision, applied
at the layer each is honest at: an API has no banner to read; a UI does.

### `costSheetDateRange` — deriving the sync window from the upload itself

```go
func costSheetDateRange(records []ingest.CostInvoiceRecord) (from, to time.Time)
```

A min/max scan over every parsed row's `invoice_date`. A cost sheet is not
one date — `ingest.ParseCostSheet`'s own contract is one invoice per row on
a supplier's own irregular cadence (produce every few days, protein
weekly) — so deriving the range from the rows themselves, rather than
accepting one from the client, means the sync covers exactly what the file
covers and cannot be pointed at a different range than the one actually
being committed.

### One pipeline run, not two

```go
pipeline.RunIngestionPipelineWithConnectorOverlay(livedata.Dir, store, overlay)
```

`overlay` is `nil` when the sync wasn't requested — the exact call this
handler made before this feature existed. When it was requested, the same
call carries the fetched connector data, so the cost-sheet write and the
connector revenue land in the database from a single reconciliation pass.
Two sequential runs (write costs, reconcile; then write connector overlay,
reconcile again) were rejected because they would durably persist an
intermediate state — new costs against old revenue — that is a real
reconciliation of nothing that was ever true, briefly readable by a
concurrent request and durably readable if the second run then failed.

### What this part does not touch

- `POST /api/connectors/sync` and the standalone Connected Platforms tab
  flow — unchanged, still the only way to sync a range with no cost-sheet
  upload attached.
- `ingest.ParseCostSheet` and the cost-sheet validation path — unchanged;
  FR-007's re-validation-from-scratch guarantee (spec 007) is untouched by
  this feature.
- `internal/reconcile`'s dedup/flag machinery from spec 012 — reused
  as-is, not modified.

### Testing, as it actually exists

| Test | File | Proves |
|---|---|---|
| `TestCostSheetDateRange_SpansEveryRow` | `ingest_cost_sheet_test.go` | Range derivation covers the whole file, not just the first/last row |
| `TestCostSheetDateRange_HandlesASingleDaySheet` | `ingest_cost_sheet_test.go` | The min/max collapse correctly when every invoice shares a date |
| `TestWantsConnectorSync_IsOffUnlessAsked` | `ingest_cost_sheet_test.go` | FR-001's absent/false-value table, and the true-value table |
| `TestHandleCommitCostSheet_RefusesAnOverWideConnectorRangeBeforeTouchingDisk` | `ingest_cost_sheet_test.go` | FR-003's atomicity for the range-cap failure mode |
| `TestHandleCommitCostSheet_RefusesTheSyncWhenNoConnectorsAreWired` | `ingest_cost_sheet_test.go` | FR-006 |

**Honestly noted gap**: there is no Go unit test exercising the *successful*
sync-and-commit path end-to-end with a fake connector proxy — that path was
verified live instead (SC-006's smoke test, against a real throwaway
Postgres and a freshly regenerated dataset), not by an automated test in
CI. A forward-looking plan written before this code existed would likely
have called for one; this retroactive one records that it does not exist
rather than implying otherwise.

## Part 2 — trading-day variance

### The mechanism, as actually built

`seed.go` gains a `dayCondition` type and a `dayConditions()` table of
seven weighted entries (`ordinary` 520/1000 down to `severeWeather`
45/1000), each carrying a label, a weekday-eligibility predicate, and
independent delivery/in-house demand multipliers plus a refund-rate
multiplier. `conditionForDate` draws one condition per calendar date from
a seed namespaced `"trading-condition"` — separate from the demand and
per-order seed namespaces already in use, so that the day's *condition*,
each platform's *demand*, and each order's *detail* are three independent
draws that cannot leak information into each other through shared RNG
state.

The condition is a pure function of the date alone, not of
`(platform, date)` — iFood, Just Eat Takeaway, and the POS all have to
agree about whether today was a storm, because weather is a property of
the restaurant's day, not of one platform's feed.

### Why this couldn't just copy `cmd/gendata`'s regime model

`cmd/gendata` plans a whole multi-year dataset up front and can afford to
tag calendar *months*, then allocate a fixed count of shock days inside
each month. A connector fetch is random access — an owner might sync one
date, or three, in any order, or the same date twice — so there is no
"up front" in which to allocate a month's budget. The unit here is
therefore the *day*, drawn from a per-date weighted table rather than
assigned from a pre-planned month.

The other forced difference: `cmd/gendata`'s own ledger records that a
demand dip alone can never flip one of *its* months negative, because
every cost it models scales with revenue, so it pairs each slump with a
matching cost-side shock. That pairing does not exist here, and the reason
is the whole mechanism — the connector supplies **revenue only**. The
costs a connector-synced day reconciles against come from the supplier
cost sheet, which is fixed: the produce was ordered, delivered, and
invoiced before anyone knew what the day would do. A demand collapse
against already-committed input costs is how a real restaurant actually
loses money on an ordinary Tuesday, and it is the most honest lever
available on this path — no cost-side fiction (an invented invoice, a
fabricated markdown) is introduced anywhere.

### The weekday curve

The old `weekendLift` (a flat multiplier on Friday/Saturday only) is
replaced by `dayShape`, a full seven-value curve (Monday 0.82 through
Saturday 1.30, Thursday pinned at the 1.00 anchor). Its mean (1.026) is
close to the old shape's implied mean (1.071) by design — the change
reshapes a simulated week, it does not resize the simulated business.

### The versioned seed salt

`connectorSeedSalt` moved from `"...v1"` to `"...v2"`, exactly the kind of
change its own doc comment said would be a "deliberate, visible act" if it
ever happened. Every previously-synced connector date now reconciles to a
different number than it did before this feature shipped. `cmd/gendata`
is unaffected — it seeds its own independent stream from its own
constant and imports nothing from `platformconnector` — verified by
`go list -deps ./cmd/gendata` returning only itself, and by the four
regenerated historical CSVs being byte-identical (same MD5s) before and
after.

### Testing, as it actually exists

| Test | File | Proves |
|---|---|---|
| `TestDayConditions_WeightsSumToTotal` | `seed_test.go` | The table can't silently make its last condition unreachable |
| `TestDayConditions_LabelDisciplineHolds` | `seed_test.go` | The ordinary condition carries no label; every other condition does |
| `TestConditionForDate_IsDeterministic` | `seed_test.go` | Same date, same condition, every call |
| `TestConditionForDate_IsPlatformIndependent` | `seed_test.go` | FR-009 — no platform can see a different condition than another for the same date |
| `TestConditionForDate_RespectsWeekdayEligibility` | `seed_test.go` | FR-011 — an ineligible weekday collapses to ordinary |
| `TestSimulatedDays_ProduceBothProfitableAndLosingDays` | `seed_test.go` | SC-003/SC-004's "measured, not assumed" losing-day share |

## Documentation

`CHANGELOG.md` carries the dated entry this plan is sourced from.
`docs/frontend.md`, `docs/openapi.yaml`, and `docs/api.html` were updated in
the same commit (the new `sync_connectors` field, `connector_sync` response
shape, and `trading_note`). `docs/prd.md` and `docs/architecture.html` were
**not** extended for this feature at the time it shipped — noted here
rather than silently left implied as done, since a retroactive plan's job
is to describe what actually happened, not what a complete rollout would
ideally have included.
