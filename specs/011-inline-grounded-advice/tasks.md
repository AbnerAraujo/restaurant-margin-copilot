# Tasks: Inline Grounded Advice

**Spec**: [spec.md](./spec.md) · **Plan**: [plan.md](./plan.md)

## Phase 1 — Gate signal (FR-001, FR-006)

- [x] T001: `internal/ambiguity`: add `AdviceRequested bool` to `Decision`, `advice_requested` to `gateResponse`, parse it in `parseGateResponse` (absent → false).
- [x] T002: `internal/ambiguity`: extend the system prompt's mixed data-plus-advice section into the general advice-request rule (groundable advice → answerable + `advice_requested: true`; ungroundable advice → unanswerable, unchanged; pure data → `false`), and add the field to the reply-shape line.
- [x] T003: Tests: prompt-content test for the new section; parse tests (true / false / absent); structural test that `writerResponse`/`refineIfNeeded` cannot set the signal.

## Phase 2 — Dynamic advisor (FR-003, FR-004, FR-005)

- [x] T004: `internal/advisor/question_advice.go`: `questionBaseSystemPrompt` (009's non-fabrication rules verbatim), `toolGuidance` for all 8 tool names (sourced per plan.md), `BuildQuestionSystemPrompt` (canonical order, deduplicated), `composeQuestionUserMessage`, `AdviseOnQuestion` with `Advise`'s exact error contract; `KindQuestionAdvice` constant kept OUT of `kindGuidance`/`KnownKind`.
- [x] T005: Tests (`question_advice_test.go`): builder includes exactly the matched sections; verbatim hard rules present; no 009 kind template on this path; refusals on empty question/grounding with zero API calls; user message carries question + JSON verbatim; `KnownKind("question_advice")` is false.

## Phase 3 — Narration handoff (FR-011)

- [x] T006: `internal/explain`: exported `AdviceHandoffNote`; exception sentence in the mixed-question system-prompt rule.
- [x] T007: Tests (`explain_internal_test.go`): prompt carries the exception; note content pinned.

## Phase 4 — Inline invocation + ledger (FR-002, FR-007..FR-010, FR-012)

- [x] T008: Migration `000013_question_advice_kind` (up/down): extend the `kind` CHECK with `'question_advice'`.
- [x] T009: `internal/httpapi`: `QuestionAdviser` interface, optional `Deps.QuestionAdviser`/`Deps.InsightStore`, `InlineAdviceView` on `AskResponse` (`advice`), post-answer invocation gated on flag + answered + ≥1 invocation, ledger write, interactions entry, degrade-on-failure.
- [x] T010: Wire in `cmd/server/main.go`'s `buildAskDeps` (same `advisor.New(llm)` instance pattern as the 009 endpoint; store passthrough).
- [x] T011: Tests (`ask_advice_test.go`): fires once with real invocations; response + interactions + ledger verified; not fired when unflagged / nil deps / refused / incomplete / zero invocations; failure degrades to unchanged answer; `POST /api/business-insight` rejects `question_advice`.

## Phase 5 — Frontend

- [x] T012: `AskPage.tsx`: map `advice`; `ChatPanel.tsx`: `AnswerChatMessage.advice`, render block after the teaser section in the dashed-warning AI-suggestion language with the wire disclaimer.
- [x] T013: `ChatPanel.test.tsx`: advice block renders with disclaimer; absent when no advice.

## Phase 6 — Docs & verification

- [x] T014: `docs/prd.md` new section (evolution of 009, owner rationale, out-of-scope boundary); `docs/architecture.html` advisor section updated; `docs/presentation.html` checked for now-inaccurate "5 kinds as ceiling" claims.
- [x] T015: `go build ./... && go vet ./... && go test ./...`; `npx tsc -b --noEmit`; `npm test -- --run`.
- [x] T016: Live verification against an isolated instance: SC-001 (open-ended groundable advice), SC-002 (ungroundable refusal), SC-003 (009 teaser unchanged). CHANGELOG.md entry.
