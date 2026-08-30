---

description: "Task list for the POS connector and cross-source order deduplication"
---

# Tasks: POS connector, and cross-source order deduplication

**Input**: Design documents from `specs/012-pos-connector-dedup/` (spec.md, plan.md, checklists/requirements.md)

**Tests**: plan.md's Testing strategy table is the contract. The three tests carrying this feature's actual financial risk — a real duplicate caught, a real non-duplicate **not** caught, and an ambiguous pair refused rather than guessed — are written before the matcher, not after.

**Organization**: Four user stories (US1 POS as a source, US2 no double counting, US3 uncertainty disclosed, US4 the mock is a real exercise). US3 is not a separate phase; it is a property of the matcher and its tasks live with it.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on an incomplete task)
- File paths are exact, from a recon pass done immediately before writing this file.

---

## Phase 1: Setup

- [x] T001 Confirm a clean baseline on `feature/012-pos-connector-dedup` in an isolated worktree with its own Postgres (never the shared dev database on :5432, backend on :8080, or frontend on :5173): `cd backend && go build ./... && go vet ./... && go test ./...`.

---

## Phase 2: Foundational — the POS upstream

- [x] T002 [US1] `backend/internal/platformconnector/platform.go`: add `PlatformPOS`, extend `AllPlatforms`, extend `DisplayName()` to return `"POS"` (which `reconcile.normalizeSourceName` maps to the `pos` key the CSV path already produces), and update the package doc to describe three upstreams and the dedup pass.
- [x] T003 [US1] `backend/internal/platformconnector/client.go`: factor the shared `connector` interface (`Platform`, `Describe`), keep `Client` unchanged, add `POSClient` and the `POSOrder` wrapper carrying the two matching signals `ingest.POSRecord` has no field for. Document, in the interface's own doc comment, why the POS is **not** forced through `Client`.
- [x] T004 [US1][US4] `backend/internal/platformconnector/seed.go`: add the POS day model — in-house ticket volume derived from `cmd/gendata`'s `posShare`, the echo selection that reuses `simulateDay(PlatformIFood, …)` so echoed tickets are the *same* orders, the 75/25 reference-presence split, and the campaign-discount gross-up. Every modelling constant carries a doc comment saying it is a choice.

### Tests written before the mock (red first)

- [x] T005 [P] [US4] `backend/internal/platformconnector/mock_shapes_test.go`: extend `TestMockUpstreams_EmitGenuinelyDifferentWireShapes` with the POS — NDJSON with no envelope, no pagination key, pt-BR decimal amounts, zone-less timestamps, `PAID`/`VOID`.
- [x] T006 [P] [US4] `backend/internal/platformconnector/pos_mock_test.go`: `TestPOSAdapter_PtBRAmountsNormalize` (including the `"1.234,56"` → `123456` case and a refusal on a malformed amount), `TestPOSAdapter_TicketTimeIsReadInTheMerchantZone`, `TestPOSMock_EchoesRealIFoodOrdersAndNeverJETOrders`, `TestPOSMock_MajorityOfTicketsAreInHouse`.

### Implementation

- [x] T007 [US1][US4] `backend/internal/platformconnector/pos_mock.go`: the simulated POS terminal upstream (NDJSON, `service_type`, nested `delivery_partner`, pt-BR money, zone-less local timestamps, `PAID`/`VOID`, an explicit ticket cap) and its adapter.

---

## Phase 3: US2 + US3 — the matcher

### Tests written before the matcher (red first — these are the feature's risk)

- [x] T008 [P] [US2] `backend/internal/platformconnector/dedup_test.go`: `TestDedup_ReferencedDuplicateIsRemovedAndDeliveryRecordSurvives`.
- [x] T009 [P] [US2] `backend/internal/platformconnector/dedup_test.go`: `TestDedup_ChannelTaggedDuplicateMatchesOnExactAmountAndWindow`.
- [x] T010 [P] [US2] `backend/internal/platformconnector/dedup_test.go`: `TestDedup_InHouseTicketSharingAmountAndTimeIsNotRemoved` — **the false-positive bar**, with a dine-in ticket at the same cents and one minute away from a delivery order.
- [x] T011 [P] [US3] `backend/internal/platformconnector/dedup_test.go`: `TestDedup_TwoTicketsContestingOneOrderMergeNothing` and `TestDedup_TicketWithTwoEqualCandidatesMergesNothing` — **the ambiguity bar**.
- [x] T012 [P] [US3] `backend/internal/platformconnector/dedup_test.go`: `TestDedup_UnresolvableReferenceIsFlaggedAndNotAmountMatched`, `TestDedup_AmountMismatchOnAConfirmedMatchIsReported`, `TestDedup_IsIndependentOfInputOrder`, `TestDedup_TicketForAPlatformOutsideTheFetchIsLeftAlone`.

### Implementation

- [x] T013 [US2][US3] `backend/internal/platformconnector/dedup.go`: `DedupDecision`, `DedupKind`, and `dedupeAcrossSources` — pass 1 (reference identity), pass 2 (channel + exact cents + ±15 min + symmetric uniqueness), and a `Detail` string on every decision naming both sides with their provenance.
- [x] T014 [US1][US2] `backend/internal/platformconnector/proxy.go`: register POS clients, fetch POS inside `FetchRange`, run the matcher **before** computing totals, and extend `FetchResult` / `PlatformDayTotals` with the POS records, the decisions, and the per-day duplicate counts.

---

## Phase 4: US1 + US2 — carrying it to the day

- [x] T015 [US2] `backend/internal/reconcile/types.go` + `reconcile.go`: three new flag constants and `ComputeDailyReconciliationsWithFlags`, with `ComputeDailyReconciliations` reduced to a nil-map delegate.
- [x] T016 [US1][US2] `backend/internal/pipeline/pipeline.go`: `ConnectorOverlay` with `DeliveryActive` / `POSActive`, `RunIngestionPipelineWithConnectorOverlay`, decision→flag translation, and `RunIngestionPipelineWithDeliveryOverlay` kept as a delegate.
- [x] T017 [P] [US1] `backend/internal/pipeline/overlay_test.go`: a delivery-only sync leaves POS rows untouched; a POS-active sync replaces in-range POS rows only; decisions land as flags on the right day.

---

## Phase 5: US1 + US3 — the HTTP surface

- [x] T018 [US1][US3] `backend/internal/httpapi/connector_sync.go`: POS in the platform list, dedup counts and a plain-language decision list in both response bodies, the notice extended to name all three sources.
- [x] T019 [P] [US1] `backend/internal/httpapi/connector_sync_test.go`: a three-source sync's gross equals the raw totals minus the removed duplicates; the response reports the removals; POS provenance is `simulated://`.

---

## Phase 6: US1 — the frontend

- [x] T020 [US1][US3] `frontend/src/components/Upload/ConnectedPlatformsTab.tsx`: the duplicates column, the dedup summary, copy naming all three sources.
- [x] T021 [P] `frontend/src/components/Upload/ConnectedPlatformsTab.test.tsx`: the POS row renders as simulated; the dedup summary renders.

---

## Phase 7: Polish & cross-cutting

- [x] T022 [P] `docs/prd.md`: extend section 12 with the POS source and the dedup rule, including the false-positive reasoning.
- [x] T023 [P] `docs/architecture.html`: the connector section only — the third upstream and the matcher. The competitive-positioning narrative is not touched.
- [x] T024 [P] `docs/openapi.yaml`: the new response fields.
- [x] T025 `CHANGELOG.md`: an entry in this project's root-cause-explaining style.
- [x] T026 Full verification: backend build/vet/test against an isolated Postgres, frontend `tsc -b --noEmit` and `npm test -- --run`, plus a live end-to-end three-source sync reporting real before/after numbers.

---

## Dependencies & Execution Order

- Phase 2 blocks Phase 3. Within it: T002-T004, then T005-T006 (red), then T007.
- Phase 3's T008-T012 are written red before T013.
- Phase 4 depends on Phase 3's `DedupDecision` shape.
- Phase 5 depends on Phase 4. Phase 6 depends on Phase 5's response shapes.
- Phase 7's T022-T024 can run alongside Phase 6; T025-T026 come last.
