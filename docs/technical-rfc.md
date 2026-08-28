# Technical RFC: Daily Margin & Growth Copilot

**Status:** Accepted · **Author:** Jair Abner de Araujo · **Date:** 2026-08-28

## Context

Product requirements: `docs/prd.md`. Full spec: `specs/001-margin-reconciliation-qa/spec.md`. This RFC is the engineering-audience companion — how it's built, and why, not what or for whom.

## Goals

- Deterministic margin reconciliation and promotion-ROI computation, provably separate from any LLM call (Constitution Principle I).
- A fixed, typed MCP tool boundary as the *only* path from the LLM to real data (Principle III).
- Refuse rather than guess; provenance on every number (Principles II, IV).
- Full cost/token/latency instrumentation from the first API call (Principle VI).
- Real-file-compatible ingestion, not fixture-column-hardcoded parsing.

## Non-goals

- No agent framework (LangChain-style orchestration) — direct Anthropic API calls with defined tools only.
- No multi-tenant support, no deployment pipeline, no Kubernetes — single-tenant local prototype.
- No semantic-memory/caching/LLMOps harness — explicitly deferred as a Phase 2 vision (see `docs/product-strategy.md`), not because it's a bad idea, but because it's a different, much larger system than a 4-day prototype.
- No fine-tuning or model training of any kind — MCP is a tool-calling protocol at inference time, not a training mechanism.

## Proposed design

### Stack

Go 1.27 (backend), PostgreSQL (storage), React + TypeScript (frontend), Anthropic API via `anthropic-sdk-go` (Claude Haiku 4.5 for the ambiguity gate, Claude Sonnet 5 for explanation), `mark3labs/mcp-go` (MCP tool layer, in-process), `sqlc` + `pgx/v5` + `golang-migrate` (typed Postgres access), `promptfoo` (evaluation harness). Full rationale and alternatives evaluated for each: `research.md`.

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
                                                                          internal/ambiguity (Haiku 4.5, no data access at all)
                                        both log to → internal/instrumentation
```

Full diagram with labeled dependency arrows: `docs/architecture.html` (published artifact). The load-bearing property: `internal/reconcile` has zero outgoing calls — it does not import Postgres, MCP, or Claude packages, so nothing forces the domain core to know these things exist. `internal/ambiguity` has no import path to `internal/mcptools` at all, since it only classifies question text.

### Data model

Three persisted entities (`daily_reconciliation`, `promotion_roi_record`, `question_interaction`), each carrying source-row provenance references and, for interactions, a nullable `roi`/`answer_text` that is *required* to be null when attribution/refusal fires — enforced at the type level, not just by convention. Full schema: `data-model.md`.

### MCP tool contracts

Five fixed tools (`get_daily_summary`, `get_margin_delta`, `list_discrepancies`, `get_promotion_roi`, `list_negative_roi_promotions`), each read-only, each timeout-bounded, each returning a typed error object rather than a best-guess value on failure. No open-SQL or free-form-filter tool exists. Full contracts: `contracts/mcp-tools.md`.

### Request flow

1. Daily (batch): `ingest` → `reconcile` → `storage` — no LLM involved.
2. Per question: `ambiguity` gate classifies (answerable/ambiguous/unanswerable) → if answerable, `explain` calls `mcptools` → `storage` → narrates the typed result. If not, refusal/clarification returns directly, bypassing `explain` entirely.

## Alternatives considered

Full per-decision alternatives (LLM vendor, MCP transport, DB access layer, eval harness, frontend UI library): `research.md`. Two worth restating here since they were genuinely close calls:

- **Anthropic's native MCP connector** (`mcp_servers` + `mcp_toolset`, URL-based) instead of in-process `mark3labs/mcp-go`: would require the MCP server to be independently HTTP-reachable, an unneeded deployment step for a local prototype. Noted as a real simplification opportunity if this ever moves toward a hosted deployment.
- **A hand-rolled Go evaluation harness** instead of `promptfoo`: viable, but `promptfoo`'s assertions/repeat/redteam features map close to 1:1 onto the three required evaluation axes (accuracy, consistency, refusal), so building one from scratch would be pure cost with no capability gain.

## Risks

| Risk | Mitigation |
|---|---|
| Model call inside `internal/explain` accidentally computes rather than narrates a number | Caught by table-driven tests on `internal/reconcile` proving the number independent of any model output; code review checks `explain.go` never does arithmetic on tool results, only formats them |
| Ambiguity gate cost/latency goes unlogged when a request never reaches `explain` | Caught by `/speckit-analyze` during planning (see `tasks.md` T022/T026 note); fixed before implementation started |
| Real restaurant export files don't match fixture column assumptions | `research.md`'s real-file-compatibility decision: generic column-name matching, not fixture-exact; failures logged to `docs/plan.md`'s mistakes log rather than silently patched |

## Rollout

Not applicable — this is a local prototype with no deployment. Post-launch rollout into Segment 1 (Prosus/ToqanClaw customers) is described at the product level in `docs/product-strategy.md`, not here.
