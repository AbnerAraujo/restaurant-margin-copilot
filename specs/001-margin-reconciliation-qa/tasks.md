# Tasks: Daily Margin & Growth Copilot

**Input**: Design documents from `/specs/001-margin-reconciliation-qa/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md — all present.

**Tests**: Included and REQUIRED, not optional — Constitution Principle V mandates the deterministic core be proven with tests before any LLM call exists. Write each story's tests first; they must fail before implementation.

**Organization**: Tasks are grouped by user story (US1–US4, priority order from spec.md), so each can be implemented and validated independently, matching the constitution's fixed build order.

## Phase 1: Setup

- [x] T001 Create `backend/` and `frontend/` directories per plan.md's Project Structure; `go mod init` in `backend/`
- [x] T002 Add Go dependencies to `backend/go.mod`: `mark3labs/mcp-go`, `jackc/pgx/v5`, `stretchr/testify`, `anthropic-sdk-go`
- [x] T003 [P] Initialize React app in `frontend/` with Vite + TypeScript; add Vitest, React Testing Library, Tailwind, shadcn/ui
- [x] T004 [P] Configure `golangci-lint` for `backend/` and ESLint/Prettier for `frontend/`

**Checkpoint**: Both project skeletons compile/run empty.

---

## Phase 2: Foundational (Blocking Prerequisites)

**⚠️ CRITICAL**: No user story work begins until this phase is complete.

- [x] T005 Write `docker-compose.yml` for local PostgreSQL
- [x] T006 Write initial `golang-migrate` migration in `backend/migrations/` for `daily_reconciliation`, `promotion_roi_record`, `question_interaction` tables per `data-model.md`
- [x] T007 [P] Configure `sqlc.yaml` and base queries in `backend/internal/storage/`
- [x] T008 [P] Hand-author the test CSVs (delivery-platform, POS, cost-sheet, promotion exports — today the dataset's opening window, `backend/cmd/gendata/opening/`), including the deliberate mess: duplicate order, refund, missing day, inconsistent date format, one promotion with incomplete attribution — per Constitution Principle V, this MUST exist before any reconciliation code is written
- [x] T009 Implement shared Anthropic API client wrapper in `backend/internal/llmclient/client.go`
- [x] T010 [P] Implement instrumentation writer (tokens, cost, latency, refusal/clarification flags) in `backend/internal/instrumentation/log.go`

**Checkpoint**: DB migrated, test data exists, LLM client and instrumentation ready — user stories can now proceed.

---

## Phase 3: User Story 1 - See today's reconciled margin (Priority: P1) 🎯 MVP

**Goal**: Ingest the source files and produce a provenanced daily margin figure.

**Independent Test**: Run ingestion against the hand-authored test data; the resulting margin matches an independently hand-computed value, with the duplicate/refund/missing-day cases handled correctly.

### Tests for User Story 1 ⚠️ write first, confirm they fail

- [x] T011 [P] [US1] Table-driven ingestion tests (duplicate order, refund, missing day, inconsistent date format) in `backend/internal/ingest/ingest_test.go`
- [x] T012 [P] [US1] Table-driven reconciliation tests (margin calc, commission/refund netting, discrepancy flags) in `backend/internal/reconcile/reconcile_test.go`

### Implementation for User Story 1

- [x] T013 [US1] Implement delivery/POS/cost-sheet CSV parsing in `backend/internal/ingest/ingest.go` (make T011 pass)
- [x] T014 [US1] Implement `DailyReconciliation` computation in `backend/internal/reconcile/reconcile.go` (make T012 pass; depends on T013)
- [x] T015 [US1] Implement discrepancy flags + anomaly threshold in `backend/internal/reconcile/discrepancies.go`
- [x] T016 [US1] Implement `sqlc`-generated persistence for `DailyReconciliation` in `backend/internal/storage/reconciliation.go`
- [x] T017 [US1] Wire `cmd/server/main.go`: ingest → reconcile → persist pipeline, runnable via CLI flag per `quickstart.md`

**Checkpoint**: User Story 1 fully functional and testable independently — no LLM call exists yet.

---

## Phase 4: User Story 2 - Ask natural-language questions (Priority: P2)

**Goal**: Answer NL questions grounded in the reconciled data via typed MCP tools.

**Independent Test**: With US1 data persisted, ask the accuracy-evaluation questions and confirm answers match, each with provenance.

### Tests for User Story 2

- [x] T018 [P] [US2] Unit tests for `get_daily_summary` and `get_margin_delta` tool logic in `backend/internal/mcptools/reconciliation_tools_test.go`

### Implementation for User Story 2

- [x] T019 [US2] Implement `get_daily_summary`, `get_margin_delta`, `list_discrepancies` MCP tools in `backend/internal/mcptools/reconciliation_tools.go` per `contracts/mcp-tools.md` (make T018 pass)
- [x] T020 [US2] Register MCP server (`mark3labs/mcp-go`) in `cmd/server/main.go`
- [x] T021 [US2] Implement explanation step (Claude Sonnet 5, tool-calling loop) in `backend/internal/explain/explain.go`
- [x] T022 [US2] Wire instrumentation capture around every `explain` call (depends on T010, T021) — note: the ambiguity gate (T025, Phase 5) is a separate model call that also needs this wiring; T026 must not skip it just because it happens before `explain`
- [x] T023 [US2] Implement `POST /api/ask` endpoint in `cmd/server/main.go`

**Checkpoint**: US1 + US2 both work independently; every answer traces to `get_*` tool output, never model-invented numbers.

---

## Phase 5: User Story 3 - Refuse or clarify instead of guessing (Priority: P3)

**Goal**: Ambiguous or unanswerable questions produce a refusal or clarifying question, never a guess.

**Independent Test**: Ask the refusal-evaluation questions; confirm each produces a refusal or clarification, never a fabricated number.

### Tests for User Story 3

- [x] T024 [P] [US3] Unit tests for ambiguity classification (answerable/ambiguous/unanswerable cases) in `backend/internal/ambiguity/gate_test.go`

### Implementation for User Story 3

- [x] T025 [US3] Implement ambiguity gate (Claude Haiku 4.5) in `backend/internal/ambiguity/gate.go` (make T024 pass)
- [x] T026 [US3] Wire the gate into `/api/ask` before any tool call; implement refusal and clarifying-question response paths, logging the gate's own tokens/cost/latency via `internal/instrumentation` even when the request never reaches `explain` (depends on T022, T023, T025)
- [x] T027 [US3] Implement per-tool-call timeout and per-interaction call cap in `backend/internal/mcptools/`

**Checkpoint**: All three core stories independently functional — the refusal discipline is provably real, not asserted.

---

## Phase 6: User Story 4 - Flag underperforming promotions (Priority: P4)

**Goal**: Flag negative-ROI promotions end-to-end — the KR3 growth lever.

**Independent Test**: Load a promotion whose spend exceeds its attributed incremental revenue; confirm it's flagged with provenance, and one with incomplete attribution is refused, not estimated.

### Tests for User Story 4

- [x] T028 [P] [US4] Table-driven promo-ROI tests (negative ROI, positive ROI, missing attribution → null) in `backend/internal/reconcile/promo_test.go`

### Implementation for User Story 4

- [x] T029 [US4] Implement promotion/ad-spend CSV parsing in `backend/internal/ingest/promo.go`
- [x] T030 [US4] Implement `PromotionRoiRecord` computation in `backend/internal/reconcile/promo.go` (make T028 pass; depends on T029)
- [x] T031 [US4] Implement `get_promotion_roi`, `list_negative_roi_promotions` MCP tools in `backend/internal/mcptools/promo_tools.go`
- [x] T032 [US4] Implement "Clean Close" / "Discrepancy Catcher" badge evaluation (Reconciliation category only — per `docs/product-strategy.md`'s built-now scope) in `backend/internal/badges/badges.go`, exposed via a plain `GET /api/badges` REST endpoint in `cmd/server/main.go` — deliberately NOT an MCP tool, since no FR requires the model to narrate badges; this is deterministic UI state, not something to over-expose to the LLM layer

**Checkpoint**: Product A (KR1–KR4) fully functional end-to-end.

---

## Phase 7: Polish & Cross-Cutting Concerns

- [x] T033 [P] Write `promptfoo` configs (`accuracy.yaml`, `consistency.yaml`, `refusal.yaml`) in `evaluation/promptfoo/`, with independently-computed golden answers in `evaluation/golden/`
- [x] T034 Run the evaluation harness; record real numbers (including failures) into `docs/product-strategy.md` and the reasoning doc — no target asserted in advance, per Principle V
- [x] T035 [P] React chat UI (shadcn AI Elements) in `frontend/src/components/Chat/`
- [x] T036 [P] React provenance display + running cost panel in `frontend/src/components/`
- [x] T037 [P] React badge display (Reconciliation category only) in `frontend/src/components/Badges/`
- [x] T038 Run `quickstart.md` validation end-to-end, including the real-file-compatibility trial
- [x] T039 Log any real mistakes caught during implementation into `docs/plan.md`'s running mistakes log — live, not reconstructed later

---

## Dependencies & Execution Order

- **Setup (Phase 1)** → **Foundational (Phase 2)**: strictly sequential, blocks everything else.
- **US1 (Phase 3)**: no dependency on other stories — the MVP.
- **US2 (Phase 4)**: depends on US1's persisted data existing (reads `DailyReconciliation`).
- **US3 (Phase 5)**: depends on US2's `/api/ask` endpoint existing (the gate wraps it).
- **US4 (Phase 6)**: depends on Foundational only, not on US2/US3 — can be built in parallel with US2/US3 once US1 is done, since it's an independent ingestion+computation+tool-set slice.
- **Polish (Phase 7)**: depends on all four stories being complete.

This order matches the constitution's fixed build order (test data → engine + tests → MCP → model layer → instrumentation → harness → UI) — it is not to be reordered even for parallelization convenience.

### Parallel Opportunities

- T003/T004 (frontend init, lint config) parallel with T001/T002 (backend init).
- T007/T008/T010 parallel with each other in Phase 2.
- **US1 and US4 can run in parallel** once Phase 2 is done (both only need the test data + storage, not each other).
- **US2 and US3 must run sequentially** (US3 wraps US2's endpoint) — not a parallel pair.
- T033/T035/T036/T037 in Phase 7 can run in parallel (independent files).

## Implementation Strategy

**MVP**: Phases 1–3 (Setup, Foundational, US1) — a working, tested, provenanced daily margin figure with zero LLM calls. Stop and validate here before continuing; this is the one slice that must be unimpeachable.

**Incremental delivery**: US1 → US2 → US3 → US4 → Polish, each independently testable per its Independent Test above, matching `docs/plan.md`'s Day 1–4 schedule.
