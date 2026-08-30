---

description: "Task list for Platform Connector Proxy (simulated iFood + Just Eat Takeaway)"
---

# Tasks: Platform Connector Proxy

**Input**: Design documents from `specs/010-platform-connector-proxy/` (spec.md, plan.md, checklists/requirements.md)

**Tests**: plan.md's Testing strategy table is the contract. The three tests that carry the feature's actual risk — both adapters converging on one record shape, determinism across fetch orders, and the refund sign convention — are written before their implementation, not after. Live-Postgres handler tests follow `ingest_cost_sheet_test.go`'s existing pattern.

**Organization**: Three user stories (US1 sync path, US2 honest labeling, US3 demonstrable heterogeneity). US2 is not a separate phase — it is a constraint threaded through every surface US1 and US3 create, so its tasks live where the surfaces do and are tagged `[US2]` there.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on an incomplete task)
- File paths are exact, from a recon pass done immediately before writing this file.

---

## Phase 1: Setup

- [x] T001 Confirm a clean baseline on `feature/010-platform-connector-proxy` in an isolated worktree with its own Postgres (never the shared dev database on :5432): `cd backend && go build ./... && go vet ./... && go test ./...`, `cd frontend && npx tsc -b --noEmit && npm test -- --run`.

---

## Phase 2: Foundational — the connector package (blocking everything else)

- [x] T002 [US1] `backend/internal/platformconnector/platform.go`: the `Platform` type, `PlatformIFood` / `PlatformJustEatTakeaway`, `ParsePlatform`, and `DisplayName()` returning exactly the strings `reconcile.normalizeSourceName` maps to `ifood` / `just_eat_takeaway` — with a doc comment naming that coupling, since getting it wrong silently splits a platform's revenue into a third bucket.
- [x] T003 [US1] `backend/internal/platformconnector/client.go`: the `Client` interface (`Platform`, `Describe`, `FetchDeliveryRevenue`), the `Description` struct (wire-format facts for the UI), and the package doc comment stating the emulation up front.
- [x] T004 [US1] `backend/internal/platformconnector/seed.go`: `dayRNG(platform, date)` — FNV-64a over a fixed salt + platform key + `YYYY-MM-DD`, seeding a per-call `*rand.Rand` — plus the shared `simulatedOrder` day model (order count, ticket sizes, refund selection) using `cmd/gendata`'s own scale constants. Doc comment must justify the per-key seed over `cmd/gendata`'s single-stream seed (random access vs. sequential generation).

### Tests for the connector package (written before T007-T008)

- [x] T005 [P] [US1] `backend/internal/platformconnector/proxy_test.go`: `TestFetchDeliveryRevenue_IsDeterministicPerPlatformDay` — the same (platform, date) fetched twice, and reached through two different range orders, yields identical records. Mirrors `cmd/gendata`'s own determinism discipline.
- [x] T006 [P] [US3] `backend/internal/platformconnector/mock_shapes_test.go`: `TestMockUpstreams_EmitGenuinelyDifferentWireShapes` — asserts on the **raw JSON bytes** of both mocks (envelope key, pagination key, field names, money representation, timestamp type, status vocabulary), so the heterogeneity cannot silently converge over time.

### Implementation

- [x] T007 [US1] `backend/internal/platformconnector/ifood_mock.go`: the simulated iFood upstream (page-numbered envelope, `snake_case`, RFC 3339 timestamps, decimal-string money, nested commission with `rate_percent`, `CONCLUDED`/`CANCELLED`, positive amounts on a cancellation) and its adapter, including the negation of cancelled amounts into this repo's negative-refund convention.
- [x] T008 [US1] `backend/internal/platformconnector/jet_mock.go`: the simulated Just Eat Takeaway upstream (cursor envelope, `camelCase`, epoch-millis timestamps, integer minor units, **no** commission rate, `DELIVERED`/`REFUNDED`, already-negative refunds) and its adapter, including deriving `CommissionRateBps` from the reported minor units.
- [x] T009 [US1] `backend/internal/platformconnector/proxy.go`: `Proxy`, `NewProxy`, `NewSimulatedProxy`, `FetchRange`, the six contract checks, `maxSyncDays`/`maxPagesPerDay` caps, and refusals for an inverted range, an unknown platform, an empty platform list, and any single-platform failure.
- [x] T010 [P] [US1] `backend/internal/platformconnector/normalize_test.go`: `TestBothAdaptersConvergeOnOneRecordShape` (same logical order → identical field semantics from both platforms), `TestRefundNormalization_*` (both platforms' refunds land negative with a non-nil refund date), and `TestProxy_RefusesContractViolation` (a deliberately broken adapter is caught at the boundary).
- [x] T011 [P] [US1] `backend/internal/platformconnector/proxy_test.go`: refusal tests for over-cap range, inverted range, unknown platform, and partial-platform failure — each asserting on the specific message, not just that an error occurred.

---

## Phase 3: US1 — the pipeline overlay

- [x] T012 [US1] `backend/internal/pipeline/pipeline.go`: add `DeliveryOverlay` and `RunIngestionPipelineWithDeliveryOverlay`; reduce `RunIngestionPipeline` to a nil-overlay delegation so `cmd/server -ingest` and `HandleCommitCostSheet` are untouched. Doc comment must state the range-replacement semantics and why the whole day is recomputed from all three sources.
- [x] T013 [US1] `backend/internal/pipeline/overlay_test.go`: `TestApplyDeliveryOverlay_*` — in-range CSV rows replaced, out-of-range rows kept verbatim, boundary dates inclusive, nil overlay a no-op.

---

## Phase 4: US1 + US2 — the HTTP surface

- [x] T014 [US1][US2] `backend/internal/httpapi/connector_sync.go`: `HandleConnectorPlatforms` (GET, static, each platform carrying `simulated: true` and its wire-format phrase), `HandleConnectorSyncPreview` (POST, persists nothing), `HandleConnectorSync` (POST, before-snapshot → `EnsureReady` → cache clear → overlay pipeline → after-snapshot). Every response body carries a top-level `simulated: true`.
- [x] T015 [US1] `backend/internal/httpapi/ingest_cost_sheet.go`: lift the handler-scoped `commitMu` to a package-level `ingestMu` shared with the connector sync, with a doc comment explaining that the interleaving it guards is now two endpoints wide, not one.
- [x] T016 [US1] `backend/cmd/server/main.go`: register the three routes and extend the startup log line.
- [x] T017 [P] [US1][US2] `backend/internal/httpapi/connector_sync_test.go`: preview persists nothing; a sync changes the affected days' margin and leaves other days untouched; the response carries `simulated: true`; persisted `SourceRowRefs` for a synced day all begin `simulated://`; a bad date range is refused with a specific message.

---

## Phase 5: US1 + US2 + US3 — the frontend

- [x] T018 [US1] `frontend/src/components/Upload/CostSheetTab.tsx`: extract the existing cost-sheet flow verbatim out of `UploadPage.tsx`, with no behavioral change.
- [x] T019 [US1][US2][US3] `frontend/src/components/Upload/ConnectedPlatformsTab.tsx`: the platform list, the date-range picker, preview, and sync; the persistent non-dismissible simulation notice above the controls; per-platform "simulated" markers; each platform's wire-format phrase (US3 acceptance 2).
- [x] T020 [US1][US2] `frontend/src/components/Upload/UploadPage.tsx`: the accessible tab strip (`role="tablist"`/`tab"`/`tabpanel`, arrow-key roving focus), page header updated to cover both sources, and the simulation qualifier present in the tab label itself.
- [x] T021 [P] [US2] `frontend/src/components/Upload/ConnectedPlatformsTab.test.tsx`: the simulation notice renders before any figure and cannot be dismissed; each platform row carries its own marker; preview renders per-platform totals; a failed sync surfaces the server's specific message.
- [x] T022 [P] [US1] `frontend/src/components/Upload/UploadPage.test.tsx`: existing cost-sheet assertions still pass after the extraction; tab switching preserves each tab's state.

---

## Phase 6: Polish & cross-cutting

- [x] T023 [P] `docs/prd.md`: new section — the problem (no partner-API access), the solution, why the emulation is honest rather than deceptive, and explicit out-of-scope.
- [x] T024 [P] `docs/architecture.html`: add the Platform Connector Proxy and its two mocked upstreams on the **deterministic** side of the split, with copy that cannot be misread as a new AI feature.
- [x] T025 [P] `docs/openapi.yaml`: the three endpoints, with the simulation stated in each description.
- [x] T026 [P] `README.md`: the connected-platforms flow in the feature list and the demo script.
- [x] T027 `CHANGELOG.md`: an entry in this project's root-cause-explaining style.
- [x] T028 Full verification: backend build/vet/test, frontend `tsc -b --noEmit` and `npm test -- --run`, plus one end-to-end exercise of the flow against an isolated backend + frontend.

---

## Dependencies & Execution Order

- Phase 2 blocks everything. Within it: T002-T004 first, then T005-T006 (tests, red), then T007-T011.
- Phase 3 depends only on T002 (the record contract), not on the mocks — the overlay is source-agnostic by design.
- Phase 4 depends on Phases 2 and 3.
- Phase 5 depends on Phase 4's response shapes.
- Phase 6's T023-T026 can run in parallel with Phase 5; T027-T028 come last.
