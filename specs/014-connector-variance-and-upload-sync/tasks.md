---

description: "Task list for the connector trading-day variance and upload-triggered sync feature"
---

# Tasks: Connector trading-day variance, and cost-sheet upload triggering a connector sync

**This task list is retroactive.** See plan.md's "How this spec came to
exist" for the full disclosure: this feature was built and merged before
this file existed. Every task below is checked off because it reflects
work already done, recovered from the real diff
(`git show f778860 --stat`) and the real CHANGELOG entry, not a forward
plan being executed. It is written in spec-kit's checklist format so this
feature's implementation shape is auditable the same way every other
numbered spec's is, not to imply it was tracked this way as it happened.

**Input**: `specs/014-connector-variance-and-upload-sync/` (spec.md,
plan.md), the merged commit `f778860`, and `CHANGELOG.md`'s 2026-08-30
entry.

**Organization**: Two independent halves that shipped in one commit because
they answer the same underlying complaint (see spec.md's Background). Part
A is the upload-triggered sync; Part B is the trading-condition model. They
touch disjoint files and could have shipped as two commits; they did not.

## Format: `[ID] [P?] [Part] Description`

---

## Part A — the upload-triggered connector sync

- [x] T001 [A] `backend/internal/httpapi/ingest_cost_sheet.go`:
  `costSheetDateRange` — derive the inclusive min/max `invoice_date` across
  a parsed cost sheet's rows.
- [x] T002 [A] `backend/internal/httpapi/ingest_cost_sheet.go`:
  `wantsConnectorSync` — read and normalize the `sync_connectors` form
  field, absent-or-unrecognized-means-false.
- [x] T003 [A] `backend/internal/httpapi/ingest_cost_sheet.go`: extend
  `HandleCommitCostSheet` to take a `*platformconnector.Proxy`, run the
  connector fetch (when opted in) before `ingestMu` and before any write,
  and refuse the whole request atomically on a fetch failure or a missing
  proxy.
- [x] T004 [A] `backend/internal/httpapi/ingest_cost_sheet.go`: compose the
  fetched result into a `pipeline.ConnectorOverlay` and call
  `RunIngestionPipelineWithConnectorOverlay` once, carrying both the new
  cost-sheet write and the overlay (nil when no sync was requested).
- [x] T005 [A] `backend/internal/httpapi/ingest_cost_sheet.go`: extend
  `CommitCostSheetResponse` with `ConnectorSync *ConnectorSyncSummaryView`
  (populated-or-null, never an empty object) and `CoversFrom`/`CoversTo`.
- [x] T006 [A] [P] `backend/internal/httpapi/connector_sync.go`: factor
  `summarizeConnectorSync` and `connectorOverlayFor` so the commit handler
  and the standalone `/api/connectors/sync` handler share one rendering of
  a fetch result rather than two implementations that could disagree.
- [x] T007 [A] [P] `backend/internal/httpapi/ingest_cost_sheet_test.go`:
  `TestCostSheetDateRange_SpansEveryRow`,
  `TestCostSheetDateRange_HandlesASingleDaySheet`,
  `TestWantsConnectorSync_IsOffUnlessAsked`,
  `TestHandleCommitCostSheet_RefusesAnOverWideConnectorRangeBeforeTouchingDisk`,
  `TestHandleCommitCostSheet_RefusesTheSyncWhenNoConnectorsAreWired`.
- [x] T008 [A] `frontend/src/components/Upload/CostSheetTab.tsx`: the
  pre-ticked `sync_connectors` checkbox in the preview panel, naming the
  date range and the word "simulated"; wire `postMultipart`'s form fields
  to include it; render the returned `connector_sync` summary after a
  commit.
- [x] T009 [A] [P] `frontend/src/components/Upload/CostSheetTab.test.tsx`:
  integration coverage for the checked/unchecked commit paths and the
  rendered sync summary.
- [x] T010 [A] [P] `frontend/src/lib/api.ts`: extend the typed response
  shape for `CommitCostSheetApi` with the new fields.
- [x] T011 [A] [P] `backend/internal/bff/routes.go`: pass the connector
  proxy dependency through to the cost-sheet commit route.
- [x] T012 [A] [P] `docs/openapi.yaml`, `docs/api.html`, `docs/frontend.md`:
  document the new request field and response shape.

## Part B — trading-day variance

- [x] T013 [B] `backend/internal/platformconnector/seed.go`: `dayCondition`
  type, `dayConditions()` table (seven weighted entries with labels,
  weekday eligibility, and delivery/in-house/refund multipliers), and
  `conditionWeightTotal`.
- [x] T014 [B] `backend/internal/platformconnector/seed.go`:
  `seededRNG`/`conditionForDate` — a per-date (not per-platform-and-date)
  deterministic draw, in its own seed namespace separate from demand and
  per-order draws.
- [x] T015 [B] `backend/internal/platformconnector/seed.go`:
  `TradingNoteForDate` — the exported accessor `internal/httpapi` uses to
  attach a day's cause to its numbers.
- [x] T016 [B] `backend/internal/platformconnector/seed.go`: replace the
  flat `weekendLift` with the seven-value `dayShape` weekday curve; apply
  both the weekday shape and the day's condition multiplier in
  `simulateDay` and `simulatePOSDay`.
- [x] T017 [B] `backend/internal/platformconnector/seed.go`: bump
  `connectorSeedSalt` from `v1` to `v2`, recording the intentional,
  visible change to every previously-generated connector number.
- [x] T018 [B] [P] `backend/internal/platformconnector/seed_test.go`:
  `TestDayConditions_WeightsSumToTotal`,
  `TestDayConditions_LabelDisciplineHolds`,
  `TestConditionForDate_IsDeterministic`,
  `TestConditionForDate_IsPlatformIndependent`,
  `TestConditionForDate_RespectsWeekdayEligibility`,
  `TestSimulatedDays_ProduceBothProfitableAndLosingDays`.
- [x] T019 [B] `backend/internal/platformconnector/proxy.go`: thread
  `TradingNote` through `PlatformDayTotals`.
- [x] T020 [B] `backend/internal/httpapi/connector_sync.go`: render
  `trading_note` in the preview/sync response bodies.
- [x] T021 [B] [P] `frontend/src/components/Upload/ConnectedPlatformsTab.tsx`
  + `.test.tsx`: a "Trading day" column showing `trading_note`, empty on an
  ordinary day.
- [x] T022 [B] [P] `docs/openapi.yaml`: document `trading_note`.

## Cross-cutting

- [x] T023 `CHANGELOG.md`: the dated entry this spec and plan are sourced
  from, including the measured before/after numbers (SC-003 through
  SC-006).
- [x] T024 Full verification, as reported in the CHANGELOG: backend
  build/vet/test, frontend `tsc -b --noEmit` and `vitest run`, a 363-day
  measurement run against the live cost sheet, a `cmd/gendata` byte-identical
  regression check, and a live smoke test uploading a real August cost
  sheet with the sync opt-in ticked.

## Honestly incomplete

- [ ] No automated test exercises the successful sync-and-commit path
  end-to-end with a fake connector proxy (see plan.md's "Honestly noted
  gap"). Left unchecked rather than marked done, because it was not done —
  a genuine gap this retroactive pass is not going to paper over by
  checking a box.
