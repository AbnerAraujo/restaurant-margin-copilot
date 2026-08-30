---

description: "Task list for Business Insight Advisor"
---

# Tasks: Business Insight Advisor

**Input**: Design documents from `specs/009-business-insight-advisor/` (spec.md, plan.md)

**Tests**: plan.md's Testing strategy requires a fires-case AND a does-not-fire case for every trigger, fake-Adviser handler tests, a live-Postgres round-trip for the new table, and frontend component tests including the zero-fetch-on-render assertion — test tasks are included throughout, not optional.

**Organization**: Three user stories (US1 teaser, US2 on-demand advice, US3 visual distinctness). US2 depends on US1's teaser types (the endpoint re-derives the teaser); US3 is frontend-only and depends on both. Ordered accordingly rather than fully parallel.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: US1 / US2 / US3, per spec.md
- File paths are exact, from a codebase recon pass done immediately before writing this file.

---

## Phase 1: Setup

- [x] T001 Confirm a clean baseline on `feature/009-business-insight-advisor`: `cd backend && go build ./... && go vet ./... && go test ./...` (with `DATABASE_URL` set against the running `margin-copilot-postgres` container), `cd frontend && npx tsc -b --noEmit && npm test -- --run`.

---

## Phase 2: Foundational (blocking prerequisites for US2)

- [x] T002 Add migration `backend/migrations/000010_business_insight_interaction.up.sql` (+ `.down.sql`): the new dedicated ledger table — `kind` CHECK-constrained to the five insight kinds, `grounding_tool_calls JSONB NOT NULL`, `advice_text TEXT NOT NULL`, `model_used`, token counts, `estimated_cost_usd NUMERIC(12,6)`, `latency_ms`, `created_at`, a `created_at` index, and a table COMMENT stating the four-distinct-ledgers rationale — following `000005_paraphrase_match.up.sql`'s commenting style. Apply it to the local database (`migrate -path backend/migrations -database "$DATABASE_URL" up`).
- [x] T003 Add `backend/internal/storage/queries/business_insight_interaction.sql` (`CreateBusinessInsightInteraction :one`, `SumBusinessInsightCost :one`, `CountBusinessInsightInteractions :one`) and run `sqlc generate` from `backend/` to regenerate `internal/storage` (models, querier, new `.sql.go`).
- [x] T004 Add `ModelBusinessInsight = "claude-sonnet-5"` to `backend/internal/llmclient/cost.go` with a doc comment in the file's established style: why an advisory/reasoning task gets Sonnet 5 rather than Haiku, and why it shares the existing `"claude-sonnet-5"` pricing map entry rather than duplicating the key (the `ModelExplanation` precedent).

---

## Phase 3: User Story 1 - Deterministic teaser (Priority: P1) 🎯 MVP

**Goal**: Every answered question gets a zero-cost, in-Go teaser decision; matching data yields exactly one `{kind, title}`; clean data yields nothing.

### Tests for User Story 1

- [x] T005 [US1] Add `backend/internal/httpapi/business_insight_test.go`: table-driven `deriveBusinessInsightTeaser` coverage — each of the five kinds' fires case AND does-not-fire case (clean data, below-threshold rate, non-outlier pattern, improving margin), threshold boundary cases (exactly 20.00%, exactly 1.5×, exactly −5%), the fixed priority order when multiple tools ran, and nil for no/unknown/unparseable tool results — real tool-result-shaped JSON samples, `suggestions_test.go`'s style.
- [x] T006 [P] [US1] Extend `backend/internal/httpapi/ask_tool_calls_test.go`'s sibling coverage (new test in `business_insight_test.go` using `newAskHarness`): an answered question whose fake explain result carries a flagged `get_daily_summary` gets `business_insight` populated on the response; a clean one gets the field omitted; a refusal never carries it; `interactions` is unchanged in all cases (SC-001).

### Implementation for User Story 1

- [x] T007 [US1] Add `backend/internal/httpapi/business_insight.go`: `BusinessInsightTeaser{Kind, Title}`, the five `InsightKind*` constants, the documented threshold constants (each doc comment tagging Sourced anchor vs. Judgment cut per plan.md), narrow per-tool parse structs, and `deriveBusinessInsightTeaser` with the fixed priority order.
- [x] T008 [US1] Add `BusinessInsight *BusinessInsightTeaser \`json:"business_insight,omitempty"\`` to `AskResponse` in `backend/internal/httpapi/ask.go`, populated only at the `Status: "answered"` construction site alongside `SuggestedFollowUps`/`ToolCalls`.

**Checkpoint**: US1 independently testable — teaser appears/omits correctly with zero model calls.

---

## Phase 4: User Story 2 - On-demand advice (Priority: P1)

**Goal**: Tapping a teaser makes exactly one real, instrumented Sonnet 5 call and returns grounded, general advice plus its real cost; every call lands in the new ledger.

### Tests for User Story 2

- [x] T009 [US2] Handler tests in `backend/internal/httpapi/business_insight_handler_test.go` with a fake `Adviser` and recording insight store: success (advice text + real interaction figures returned, exactly one ledger write with matching figures and the posted grounding JSON), kind-mismatch → typed `insight_not_supported` error with NO adviser call, unknown kind → `invalid_input`, wrong method → 405, adviser failure → 502 with no ledger write.
- [x] T010 [P] [US2] Live-Postgres round-trip test for the new sqlc queries (skip without `DATABASE_URL`, `reconciliation_tools_test.go`'s gating pattern): insert via `CreateBusinessInsightInteraction`, read back count/sum.

### Implementation for User Story 2

- [x] T011 [US2] Add `backend/internal/advisor/advisor.go` (+ `advisor_test.go` for the pure prompt-composition helpers): `Advisor` over the shared `*llmclient.Client`, `Advise(ctx, kind, toolResults)`, per-kind system prompts embedding plan.md's researched practice with the shared never-fabricate base, `MaxOutputTokens`, `(nil, err)`-on-every-failure error contract (documented against the `ambiguity.Gate.Classify` precedent).
- [x] T012 [US2] Add `HandleBusinessInsight(deps)` to `backend/internal/httpapi/business_insight.go` (or a sibling file): decode `{kind, tool_calls}`, validate kind, re-derive the teaser from the posted tool results and refuse on mismatch, call the `Adviser`, write the ledger row (log-loudly-on-failure), respond `{kind, advice_text, disclaimer, interaction}`.
- [x] T013 [US2] Wire `POST /api/business-insight` in `backend/cmd/server/main.go`, sharing one `llmclient.New()` instance with `buildAskDeps`.

**Checkpoint**: US2 independently testable — the endpoint works end-to-end against fakes, and the table round-trips live.

---

## Phase 5: User Story 3 - Distinct, opt-in frontend bubble (Priority: P1)

### Tests for User Story 3

- [x] T014 [US3] Extend `frontend/src/components/Chat/ChatPanel.test.tsx`: chip renders (title only, suggestion-labeled) when `businessInsight` is present; absent when not; ZERO resolver calls on render; tap → loading state → advice text + disclosure + real cost; collapse/re-expand without a second resolver call; rejected resolver → visible error state.

### Implementation for User Story 3

- [x] T015 [US3] Add `frontend/src/components/Chat/BusinessInsightChip.tsx`: title-only chip with lightbulb icon and explicit "AI suggestion" label, dashed/warning-tinted visual treatment (never the answer card's `border-border bg-card`), tap-to-fetch with loading/error states, inline expansion showing advice text + disclosure + `$X.XXX · model` cost line, no re-fetch on re-expand.
- [x] T016 [US3] Wire it through `frontend/src/components/Chat/ChatPanel.tsx`: `businessInsight` on `AnswerChatMessage`, an optional `resolveBusinessInsight` prop on `ChatPanelProps` (the `resolveAnswer` dependency-injection shape), rendered in `AnswerBubble` after the follow-ups block.
- [x] T017 [US3] Wire the live resolver in `frontend/src/components/Ask/AskPage.tsx`: map `business_insight` from `AskApiResponse` onto the message, implement `resolveBusinessInsight` over `postJson('/api/business-insight')` posting the answer's own `tool_calls`, and log the returned interaction into the shared cost panel.

---

## Phase 6: Polish & Cross-Cutting

- [x] T018 [P] Update `docs/openapi.yaml` (new `business_insight` field on `AskResponse`, new `/api/business-insight` path + schemas) and regenerate the embedded spec in `docs/api.html` (same sync technique as the sonnet-5-gate-sync commits).
- [x] T019 [P] Add the PRD roadmap entry (`docs/prd.md` §11), the DoR entry (`docs/dor.md`, "009 — Business Insight Advisor: ✅ Ready"), and the Technical RFC section (`docs/technical-rfc.md`) covering the deterministic-trigger/probabilistic-content split, the new table, model constant, and endpoint.
- [x] T020 Full verification: `cd backend && go build ./... && go vet ./... && go test ./...` (live Postgres), `cd frontend && npx tsc -b --noEmit && npm test -- --run`, then a live end-to-end check — restart the backend with real `ANTHROPIC_API_KEY`, ask a question that should trigger a teaser, confirm the field, hit `POST /api/business-insight` with the real tool calls, confirm sensible advice + real cost figures + one ledger row. Done 2026-08-29: full Go suite green against the live dev Postgres; 243 frontend tests + `tsc -b` clean. Live run against the real Anthropic API: "How did iFood and Just Eat Takeaway compare on commission between 2026-07-05 and 2026-07-11?" answered with `business_insight: {kind: high_commission}` (iFood's real 23.00% effective rate ≥ the 20.00% threshold) and unchanged gate+explain interactions; `POST /api/business-insight` with that answer's own `tool_calls` returned grounded advice restating only figures present in the JSON (23.00%, 19.00%, 1460.59 — no fabricated statistics), with real measured cost `claude-sonnet-5 · 1516 in / 340 out · $0.006432 · 4978ms`, and exactly one matching `business_insight_interaction` row; a deliberately mismatched request (`negative_promo_roi` claimed over platform-comparison data) was refused live with 422 `insight_not_supported` and no model call.

---

## Dependencies & Execution Order

- Phase 2 (migration/sqlc/model constant) blocks US2's persistence and model call; US1 (Phase 3) needs none of it and can run alongside Phase 2.
- US2 depends on US1's teaser derivation (the endpoint re-derives it) and Phase 2.
- US3 depends on US1's response field (and, for its live resolver, US2's endpoint) — component tests run against fakes and only need the types.
- Docs (Phase 6) come last, after the shapes stop moving.
