# Technical RFC: Daily Margin & Growth Copilot

**Status:** Accepted · **Author:** Jair Abner de Araujo · **Date:** 2026-08-28

## Context

Product requirements: `docs/prd.md`. Full spec: `specs/001-margin-reconciliation-qa/spec.md`. This RFC is the engineering-audience companion — how it's built, and why, not what or for whom.

## Goals

- Deterministic margin reconciliation and promotion-ROI computation, provably separate from any LLM call (Constitution Principle I).
- A fixed, typed MCP tool boundary as the *only* path from the LLM to real data (Principle III).
- Refuse rather than guess; provenance on every number (Principles II, IV).
- Full cost/token/latency instrumentation from the first API call (Principle VI).
- Real-file-compatible ingestion, not hardcoded column parsing.

## Non-goals

- No agent framework (LangChain-style orchestration) — direct Anthropic API calls with defined tools only.
- No multi-tenant support, no deployment pipeline, no Kubernetes — single-tenant local prototype.
- No semantic-memory/caching/LLMOps harness — explicitly deferred as a Phase 2 vision (see `docs/product-strategy.md`), not because it's a bad idea, but because it's a different, much larger system than a 4-day prototype.
- No fine-tuning or model training of any kind — MCP is a tool-calling protocol at inference time, not a training mechanism.

## Proposed design

### Stack

Go 1.27 (backend), PostgreSQL (storage), React + TypeScript (frontend), Anthropic API via `anthropic-sdk-go` (Claude Sonnet 5 for the ambiguity gate and explanation, Claude Haiku 4.5 for the paraphrase-match cache classifier — the gate itself moved from Haiku 4.5 to Sonnet 5 on 2026-08-29 after a multi-year date-comparison bug; the honest postscript to that swap is that the failing comparison was range arithmetic that should never have been any model's job, and it has since been hoisted into a deterministic Go pre-check (`internal/ambiguity/daterange.go`) that refuses clearly-out-of-range explicit dates before any model call and hands the model precomputed verdicts for the rest — the swap stands only for the genuinely linguistic residual; see `internal/llmclient/cost.go`), `mark3labs/mcp-go` (MCP tool layer, in-process), `sqlc` + `pgx/v5` + `golang-migrate` (typed Postgres access), `promptfoo` (evaluation harness). Full rationale and alternatives evaluated for each: `research.md`.

### Module architecture — ports & adapters

```
internal/ingest, cmd/server  ──▶  internal/reconcile (pure domain core, zero outgoing calls)
                                        │
                                        ▼
                                 internal/storage (sqlc + pgx)
                                        │
                                        ▼
                                 internal/mcptools (typed tool port) ◀── Principle III boundary ──▶
                                                                          internal/explain (Sonnet 5)
                                                                          internal/ambiguity (Sonnet 5 gate, Haiku 4.5 paraphrase match, no data access at all)
                                        both log to → internal/instrumentation
```

Full diagram with labeled dependency arrows: `docs/architecture.html` (published artifact). The load-bearing property: `internal/reconcile` has zero outgoing calls — it does not import Postgres, MCP, or Claude packages, so nothing forces the domain core to know these things exist. `internal/ambiguity` has no import path to `internal/mcptools` at all, since it only classifies question text.

### Data model

Three persisted entities (`daily_reconciliation`, `promotion_roi_record`, `question_interaction`), each carrying source-row provenance references and, for interactions, a nullable `roi`/`answer_text` that is *required* to be null when attribution/refusal fires — enforced at the type level, not just by convention. Full schema: `data-model.md`.

### MCP tool contracts

Seven fixed tools (`get_daily_summary`, `get_margin_delta`, `list_discrepancies`, `get_promotion_roi`, `list_negative_roi_promotions`, `compare_platform_economics`, `get_period_totals`), each read-only, each timeout-bounded, each returning a typed error object rather than a best-guess value on failure. `get_period_totals` sums and ranks an entire period (per-source gross, commissions, refunds, input costs, margin, and the best/worst day by margin) in one call, closing a gap where a period-total or "which day was best" question otherwise called `get_daily_summary` once per day until the per-interaction tool-call budget ran out. No open-SQL or free-form-filter tool exists. Full contracts: `contracts/mcp-tools.md`.

### Request flow

1. Daily (batch): `ingest` → `reconcile` → `storage` — no LLM involved.
2. Per question: a deterministic Go pre-check (`internal/ambiguity/daterange.go`) parses any explicit, fully-specified dates in the question and compares them against the known data window (`storage.LoadDataDateRange`, resolved once at startup) — if every explicit date is out of range, the question is refused right here, with zero model calls and zero tokens, instrumented as a no-model interaction → otherwise the `ambiguity` gate classifies (answerable/ambiguous/unanswerable), receiving any in-range date verdicts as precomputed facts it may not re-derive → if answerable, `explain` calls `mcptools` → `storage` → narrates the typed result. If not, refusal/clarification returns directly, bypassing `explain` entirely.

**Gate/explain prompt discipline, added post-launch against a real measured eval failure (full before/after numbers in `docs/product-strategy.md`):** the two-step summary above elides three refinements worth naming. The gate classifies a subjective-sounding word ("underperforming", "losing money") as answerable, not ambiguous, whenever a typed tool already defines it deterministically (`list_negative_roi_promotions`, `get_period_totals`), rather than asking the user to define a threshold the product can already compute. `explain`'s prompt separately bans reconstructing a missing period aggregate by calling `get_daily_summary` once per day — the real cause of an earlier turn/token-budget blowup — now that `get_period_totals` answers that shape in one call. And a bare follow-up to the previous *answer* (not a reply to a clarifying question) — "and the day before?", "why?" — is resolved via `ambiguity.ComposeAnswerFollowUp` against exactly one prior exchange (`PreviousExchange{Question, AnswerText}`), deliberately never an accumulating transcript, before either prompt classifies or narrates it.

## Alternatives considered

Full per-decision alternatives (LLM vendor, MCP transport, DB access layer, eval harness, frontend UI library): `research.md`. Two worth restating here since they were genuinely close calls:

- **Anthropic's native MCP connector** (`mcp_servers` + `mcp_toolset`, URL-based) instead of in-process `mark3labs/mcp-go`: would require the MCP server to be independently HTTP-reachable, an unneeded deployment step for a local prototype. Noted as a real simplification opportunity if this ever moves toward a hosted deployment.
- **A hand-rolled Go evaluation harness** instead of `promptfoo`: viable, but `promptfoo`'s assertions/repeat/redteam features map close to 1:1 onto the three required evaluation axes (accuracy, consistency, refusal), so building one from scratch would be pure cost with no capability gain.

## Build-process trade-off (worth recording as it happened)

**Decision**: implementation itself is executed by autonomous agents (Claude Code `Agent`/`Workflow` tooling) against `tasks.md`, not written by hand line-by-line.

**Trade-off encountered**: `Workflow` (deterministic multi-agent pipelining) was disabled for the session at the start of the build. Rather than block, Phase 1 (Setup+Foundational) and Phase 3 (US1) were executed as manually-chained background `Agent` dispatches — functionally similar, but I (the orchestrator) had to sequence each phase myself off completion notifications instead of the runtime handling dependency order. Once `Workflow` was enabled mid-build (`/config` → Dynamic workflows), remaining phases (US2+US3 ∥ US4, Integration, Polish) switched to a real `Workflow` script. **Consequence**: the two approaches produce equivalent code, but the chained-agent phases have less structured intermediate verification (no schema-typed agent returns) than the Workflow-orchestrated phases. Not a defect, just a documented inconsistency in how the build was produced, for anyone auditing the process later.

**Live API cost control**: real Anthropic API testing (US2 explain, US3 ambiguity gate, and the `promptfoo` harness) is run cost-consciously against $20 of Console credit — every call's `response.usage`-derived cost is logged, and cumulative spend is checked before scaling up. Mechanism: a small number of smoke-test calls verify the code path first, with the full evaluation harness run as a separate, monitored step afterward. This is a real operational practice, not a design principle — recorded here because it directly shaped how integration testing was sequenced (smoke tests before full harness, not the other way around).

## Design decision: refund-netting convention

The hand-authored test data deliberately left open whether a refund nets
against the original order's date or its settlement date (today's case in
`backend/cmd/gendata/opening/README.md`: order 0006, placed 2024-08-02,
refunded 2024-08-09). **Decision: net against the original order date**
(accrual convention) — 2024-08-02's delivery total is 446.25 - 62.25 =
**384.00**, not the gross 446.25. **Rationale**: the
product's whole premise is a same-day "did we make money today" figure
(Vision, `product-strategy.md`); accrual-basis attributes economic reality to
the day the sale actually happened, matching what an owner intuitively means
by "how did Tuesday go" — cash-basis netting at settlement date would let a
day look artificially good until a refund from a future date retroactively
never gets reflected back into it. **Alternative considered**: cash-basis
netting at settlement date — rejected because it would require re-opening
and restating a closed day's figure whenever a late refund settles, which
contradicts the "trustworthy, same-day" framing this product is built around.

## Risks

| Risk | Mitigation |
|---|---|
| Model call inside `internal/explain` accidentally computes rather than narrates a number | Caught by table-driven tests on `internal/reconcile` proving the number independent of any model output; code review checks `explain.go` never does arithmetic on tool results, only formats them |
| Ambiguity gate cost/latency goes unlogged when a request never reaches `explain` | Caught by `/speckit-analyze` during planning (see `tasks.md` T022/T026 note); fixed before implementation started |
| Real restaurant export files don't match this project's own column assumptions | `research.md`'s real-file-compatibility decision: generic column-name matching, never exact-header-only; failures logged to `docs/plan.md`'s mistakes log rather than silently patched |

## Rollout

Not applicable — this is a local prototype with no deployment. Post-launch rollout into Segment 1 (Prosus/ToqanClaw customers) is described at the product level in `docs/product-strategy.md`, not here.

## Extension: Business Insight Advisor (specs/009)

The first feature to deliberately produce probabilistic content, so its architecture is the deterministic/probabilistic split applied INSIDE one feature rather than between features:

**Deterministic trigger, zero cost, every answered question.** `internal/httpapi.deriveBusinessInsightTeaser` inspects the same raw tool-result JSON the answer already carries (the exact inputs `deriveFollowUpSuggestions` and `deriveVisualization` already read) and returns at most one `{kind, title}` teaser — or, for most answers, nothing. Five closed-set kinds, each with a documented trigger: a real discrepancy flag (`discrepancy_pattern`); a `flagged_negative` promotion (`negative_promo_roi`); an effective commission rate ≥ 20.00% (`high_commission` — the 15–30%/entry-tier-~15% band is sourced from published marketplace pricing, the exact cut labeled judgment); a day-of-month average expense ≥ 1.5× the median with ≥ 2 occurrences (`day_of_month_expense_spike`, labeled judgment); a margin decline ≥ 5% of the prior period or a real period loss (`margin_decline`). Unlike the follow-up derivation's single-source `switch`, this is a fall-through priority sequence: a tool that ran but shows a clean pattern must not swallow a narrower real one. The teaser rides `AskResponse.business_insight` (`omitempty`), populated only on `status: "answered"`.

**Probabilistic content, opt-in, re-verified, individually ledgered.** `POST /api/business-insight` (the second and only other model-backed endpoint) takes the tapped kind plus the same `tool_calls` payload the client already received — no server-side per-answer state — and, BEFORE any tokens are spent, re-derives the teaser from the posted data through the identical Go derivation, refusing (`insight_not_supported`, 422) on any mismatch: the same never-act-on-a-client-claim-unverified discipline the paraphrase cache's live re-verification established. The call itself is one bounded Claude Sonnet 5 request (`internal/advisor`, through the shared `internal/llmclient` — same timeout, same instrumentation shape) under its own model constant, `ModelBusinessInsight`, whose `cost.go` doc comment records the model choice (an advisory/reasoning task with a per-tap rather than per-question cost profile, so "cheapest model that clears the bar" lands on Sonnet, not Haiku). Prompts embed the researched practice per kind and hard-forbid restaurant-specific facts beyond the posted JSON and fabricated statistics; the response carries an explicit disclaimer string and the call's real measured cost.

**A fourth interaction ledger, not a column.** Every advice call lands in a new `business_insight_interaction` table (migration 000010: CHECK-constrained kind, the grounding tool-call JSON, advice text, model, tokens, NUMERIC cost, latency). It is deliberately not squeezed into `question_interaction` (this call is neither the gate nor explain — that table's CHECK constraint requires a gate result no advice call has), `answer_cache_hit` (not free), or `paraphrase_match` (avoids nothing — it IS the spend): the same distinct-ledgers rule that already keeps those three apart, extended to a fourth state so no cost is ever netted or hidden.

**Frontend segregation.** The teaser renders as a dashed, warning-tinted chip labeled "AI suggestion" after the answer's own content — never the answer card's neutral treatment — showing only the title. The advice is fetched exclusively on tap (component tests assert zero fetches on render), expands inline with the disclaimer and the call's real `$`/model/token figures, is kept in memory so re-expanding never re-bills, and its returned interaction feeds the same shared cost panel `/api/ask` calls already do.

## Extension RFCs

Four roadmap items each got their own spec/plan rather than being retrofitted into this document: `specs/002-badge-expansion/`, `specs/003-platform-comparator/`, and `specs/004-semantic-cache/` extend this architecture directly (same tool layer, same deterministic/probabilistic split, same instrumentation discipline). `specs/005-multi-tenant/` plus the standalone `docs/rfc-multi-tenant.md` propose a real architectural change to this RFC's Module architecture section (every data-access function gains a required `tenant_id`) — treated as its own gated decision, not assumed approved by virtue of being written down. `specs/007-cost-sheet-upload/` extends the Module architecture with one new package (`internal/livedata`) and three new HTTP endpoints, no MCP tool and no model call anywhere in the request path — a zero-model feature by construction, per its own plan's Constitution Check.
