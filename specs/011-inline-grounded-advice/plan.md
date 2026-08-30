# Implementation Plan: Inline Grounded Advice

**Spec**: [spec.md](./spec.md) · **Status**: Ready for tasks/implementation

## Technical Context

Additive widening of specs/009-business-insight-advisor: a second, question-initiated path into the existing `internal/advisor` package. Three touch points — a new boolean signal out of the gate, a new advisor entry point with a dynamically-assembled prompt, and an inline invocation in `HandleAsk` after a successful narration. One migration (extend the ledger's `kind` CHECK), one new response field, one small frontend render block. The 009 teaser path is not modified.

**Language/stack**: Go (backend), React/TypeScript (frontend), PostgreSQL via the existing `sqlc` + `golang-migrate` pipeline, Anthropic API via the shared `internal/llmclient`. No new dependencies.

## Constitution Check

- **Principle I (deterministic/probabilistic split)**: Preserved and load-bearing. WHETHER advice runs is decided by two deterministic facts (the gate's typed `advice_requested` signal gated behind `status == answered`, and `len(toolInvocations) > 0`); WHAT grounding it sees is exactly the tool JSON the deterministic layer computed for this answer; WHICH guidance sections enter the prompt is a plain Go map lookup over tool NAMES. The model computes nothing and selects nothing. ✅
- **Principle II (refuse rather than guess)**: The ungroundable-advice refusal is an explicit FR with a live test, not a hope. `AdviseOnQuestion` returns `(nil, err)` on empty grounding exactly like `Advise`. An advisor failure degrades to the data answer, never to fabricated advice. ✅
- **Principle III (typed tools only, timeouts)**: No new tool. The grounding comes from the existing 8 typed MCP tools via `internal/explain`'s existing budgeted, middleware-guarded loop — this feature adds zero new tool-calling code paths. The advisor call itself is one bounded `llmclient` call (shared 30s timeout, `MaxOutputTokens` 400). ✅
- **Principle IV (provenance)**: The data answer keeps its full provenance untouched. The advice's provenance is the grounding JSON persisted in its ledger row, plus the on-screen disclaimer — the same honest equivalent 009 established. ✅
- **Principle V (tests first / honest omission)**: Fires-and-does-not-fire tests for the gate signal, the prompt builder, the inline invocation, the degradation path, and the unchanged 009 path. ✅
- **Principle VI (instrument every model call)**: The inline call writes the same dedicated `business_insight_interaction` ledger 009 writes (new kind `question_advice`) and appears as its own `interactions` entry. Nothing is netted or hidden. ✅

No violations requiring justification.

## Design

### 1. Trigger detection (`internal/ambiguity`)

`gateResponse` gains `"advice_requested": true|false`; `Decision` gains `AdviceRequested bool`. The system prompt's existing "Mixed data-plus-advice questions" section is extended into the general rule: any question asking for a suggestion/recommendation/how-to that has a data-groundable core is classified `answerable` **with** `advice_requested: true`; a pure-data question is `answerable` with `false`; an ungroundable advice question stays `unanswerable` exactly as before. The signal is deliberately a **separate field, not a fourth classification** — the three-way vocabulary is load-bearing across `instrumentation.GateResult`, the cache, and every existing test, and "advice requested" is orthogonal to answerability (a question can be answerable and want advice, answerable and not, or unanswerable and want advice).

Structural guarantee carried over from the writer-pass design: `writerResponse` has no `advice_requested` field and `refineIfNeeded` never assigns it, so the prose-upgrade call cannot flip the signal any more than it can flip the classification. A missing field in the model's JSON parses as `false` (Go zero value) — the conservative default: no signal, no advice call, no spend.

### 2. Grounding (no new mechanism)

`internal/explain`'s existing loop already selects and calls the right tools for the question ("how can I improve my margin?" → margin/period tools; "delivery or dine-in?" → platform-economics tools) under the existing call budget and timeouts. This feature reuses that loop's output (`Result.ToolInvocations`) as the advisor's entire grounding — the exact "reuse the existing tool-calling mechanism" requirement, and the reason no second loop, planner, or data-fetch path exists here.

One narration-prompt adjustment (FR-011): `explain.AdviceHandoffNote` — a deterministic Go constant the handler appends to the narration input only when an advisor is configured and the gate flagged the request — tells the narration step a separate advisor will handle the advice part, so it presents the data without declining what is about to be delivered. The system prompt's mixed-question rule gains the matching exception sentence. Without a configured advisor, behavior is byte-identical to today.

### 3. Dynamic prompt assembly (`internal/advisor/question_advice.go`)

New entry point `AdviseOnQuestion(ctx, question, results)` alongside the untouched `Advise(ctx, kind, results)`:

- `questionBaseSystemPrompt`: the 009 base's hard rules **verbatim where it matters** (rules 1–2 word-for-word: never a restaurant fact not literally in the JSON; never an invented statistic/source), reframed from "the ONE computed pattern you are shown" to "the owner's own question plus the computed data gathered to answer it", plus one new hard rule: if part of the question asks about something the shown data cannot ground (staffing, hiring, suppliers not in the data), say so plainly in one sentence rather than improvising.
- `toolGuidance map[string]string`: one researched-practice section per MCP tool name (all 8 covered). `BuildQuestionSystemPrompt(results)` walks the grounding set's tool names in a fixed canonical order, deduplicates, and appends each matched section — a deterministic function of which tools actually ran. `kindGuidance` (the five 009 templates) is not consulted on this path at all.
- Error contract identical to `Advise`: `(nil, err)` on empty question, empty grounding, transport error, model refusal, or empty reply.

### 4. Inline invocation (`internal/httpapi`)

`Deps` gains two optional fields (`QuestionAdviser`, `InsightStore`) — nil keeps today's behavior exactly, the same optionality pattern `Cache`/`ParaphraseMatcher` established. In `HandleAsk`, after the answered response is assembled and only when `decision.AdviceRequested && len(result.ToolInvocations) > 0`:

1. one `AdviseOnQuestion` call over the resolved question and the invocations;
2. on success: `resp.Advice = &InlineAdviceView{Text, Disclaimer: BusinessInsightDisclaimer, Interaction}`, the cost appended to `resp.Interactions`, and one ledger row written (kind `question_advice`) via the same store-or-warn discipline `logBusinessInsightOrWarn` uses;
3. on failure: log loudly, serve the answer unchanged (FR-009).

`POST /api/business-insight` is untouched: `KnownKind` still recognizes only the five kinds, so `question_advice` is rejected there by existing validation (spec User Story 3, scenario 2).

### 5. Migration

`000013_question_advice_kind`: drop and re-create `business_insight_interaction`'s `kind` CHECK to add `'question_advice'`. The 009 migration's own comment says adding a kind "should cost a migration plus a reviewed prompt" — this is that migration, and `question_advice.go` is that reviewed prompt.

### 6. Frontend

`AskResponse.advice` → `AnswerChatMessage.advice` → a render block after the teaser section in `ChatPanel`, wearing the same dashed-warning "AI suggestion" visual language `BusinessInsightChip` established (advice never blends into computed facts). The advisor call's cost reaches the cost panel through `interactions` exactly like the gate/explain calls, so no separate cost line is drawn inside the advice block. The teaser chip continues to render independently for the 009 path.

## Research grounding (Sourced vs. Judgment, per docs/product-strategy.md's tagging discipline)

New sections only — the five 009 kinds' sources (commission tiers, reconciliation mechanics, cost-cadence practice) are unchanged and re-used where a tool overlaps a kind's topic.

- **[Sourced] Prime cost as the standard margin-decomposition lens**: Restaurant365, "How to Calculate Restaurant Prime Cost" — prime cost (food+beverage cost plus labor) of a sustainable restaurant runs ≈60% of sales, with full-service typically 60–65% and quick-service 55–60%; tracked weekly rather than monthly as the actionable practice. Used qualitatively ("prime cost is the lever pair owners track first"), with the published band stated as the published band, never as a computed fact about this restaurant.
- **[Sourced] Menu engineering as the standard sales-mix practice**: Kasavana & Smith, *Menu Engineering* (Michigan State University, 1982) — the popularity×contribution-margin matrix (stars/plowhorses/puzzles/dogs) and its standard actions (protect stars, re-price or re-cost plowhorses, promote puzzles, retire dogs). A dated, checkable, foundational source; used as the named framework it is.
- **[Sourced] Direct-channel steering for delivery-mix questions**: Toast, "The Complete Guide to Online Ordering for Restaurants" and ChowNow's direct-ordering guidance — orders on a restaurant's own channel carry no marketplace commission, and the documented steering tactics are visibility (packaging, signage, link placement) plus loyalty/repeat incentives. Combined with the already-sourced 15–30% marketplace commission band from spec 009's OPA! Link/CloudKitchens research.
- **[Judgment — labeled as such]**: which guidance section maps to which tool name (e.g. `get_period_totals` → prime-cost/menu-mix framing; `compare_platform_economics` → channel-mix framing) is this project's own editorial mapping; no source prescribes it. The mapping is a reviewed constant in Go, visible in one place.
- **Not used**: Toast's "3x more profit on direct orders" and "56% more direct sales" marketing figures — vendor-published, not independently checkable, and unnecessary; the commission-free nature of direct channels makes the point without them. Same discipline as 009's deliberately-absent "~52% take-home" figure.

## Non-goals

- No general-purpose business consultant: advice with no tool-computed grounding is refused, full stop.
- No new MCP tool, no second tool-calling loop, no model-driven data fetching for advice.
- No change to the five 009 kinds' detection, thresholds, prompts, endpoint, or UI.
- No streaming/multi-turn advice conversation; one bounded call per advice-requesting answered question.

## Testing strategy

- **Gate (offline)**: prompt-content tests for the new section (pattern: `TestBuildSystemPrompt_MixedDataAdviceQuestionsAreNotFlatlyUnanswerable`); parse tests for `advice_requested` true/false/absent; a structural test that the writer pass cannot set it.
- **Advisor (offline)**: `BuildQuestionSystemPrompt` includes exactly the matched sections in canonical order, deduplicated; contains the verbatim non-fabrication rules; contains no 009 kind template; `AdviseOnQuestion` refuses empty question/grounding with zero API calls (counting fake).
- **httpapi (offline)**: inline call fires exactly once with the real invocations when flagged; response carries `advice` + the extra interaction; ledger write observed via counting fake store; no advisor call when the gate refuses (ungroundable), when unflagged, when nil-configured, when narration is incomplete, or when zero tools ran; advisor failure still serves the answer.
- **explain (offline)**: system prompt carries the handoff exception; `AdviceHandoffNote` content test.
- **Live (own isolated instance, real key)**: SC-001/SC-002/SC-003 exactly as written in spec.md.
