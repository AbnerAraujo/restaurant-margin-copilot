# Implementation Plan: Daily Margin & Growth Copilot

**Branch**: `main` | **Date**: 2026-08-28 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/001-margin-reconciliation-qa/spec.md`

## Summary

Ingest delivery-platform, POS, supplier-cost, and promotion-spend exports for
an independent restaurant/bar; deterministically reconcile daily margin and
flag underperforming promotions in Go against PostgreSQL; expose the
reconciliation engine to an LLM only through a fixed set of typed MCP tools;
answer natural-language questions via an ambiguity gate (Claude Haiku 4.5)
and an explanation step (Claude Sonnet 5) that narrates already-computed
numbers with provenance, refusing rather than guessing when data is missing
or a question is ambiguous; instrument every interaction from the first call;
prove it with a promptfoo-based evaluation harness before polishing the React
frontend. Technical approach and stack were decided earlier this session
(documented in `CLAUDE.md`, the constitution, and `docs/tooling.md`) — this
plan operationalizes those decisions for this specific feature rather than
re-deriving them.

## Technical Context

**Language/Version**: Go 1.27 (backend, reconciliation engine, MCP tool layer), TypeScript/React (frontend)

**Primary Dependencies**: `mark3labs/mcp-go` (MCP tool server, in-process with the engine), `sqlc` + `pgx/v5` (typed Postgres access), `golang-migrate` (schema migrations), `github.com/anthropics/anthropic-sdk-go` (Anthropic API — Claude Haiku 4.5 for the ambiguity gate, Claude Sonnet 5 for explanation), React + Vitest + React Testing Library, shadcn AI Elements (chat/tool-call UI components)

**Storage**: PostgreSQL — raw ingested records, computed daily reconciliations, promotion ROI records, per-interaction instrumentation log

**Testing**: `testify` (Go, table-driven), Vitest + RTL (React), `promptfoo` (LLM evaluation harness — accuracy/consistency/refusal-correctness per FR-004–FR-013 and SC-001–SC-006)

**Target Platform**: Local web app (macOS dev machine), single-tenant, no deployment pipeline in scope (Constitution: Technology & Scope Constraints)

**Project Type**: Web application (Go backend + React frontend, per Option 2 structure below)

**Performance Goals**: Not throughput-sensitive (single-tenant prototype); the meaningful target is the North Star (time-to-reconciled-close, minutes not weeks) and per-interaction latency visible to the user, not a req/s figure

**Constraints**: Every MCP tool call carries a timeout and a per-interaction call cap (Principle III); ingestion parsing targets realistic, generic CSV shapes per source type so a real restaurant/bar's actual export files are plausible inputs, not only the fixture files' exact columns (spec Assumptions)

**Scale/Scope**: One restaurant/bar, one currency, one time zone (spec Assumptions); a few weeks of daily fixture data plus a handful of promotion records — not a scale problem, a correctness problem

## Constitution Check

*GATE: checked against `.specify/memory/constitution.md` v1.1.0 before Phase 0, re-checked after Phase 1.*

| Principle | Check | Status |
|---|---|---|
| I. Deterministic Arithmetic, Probabilistic Narration | All margin/ROI math lives in Go; Claude only narrates typed tool results (data-model.md + contracts confirm no raw-row access is exposed to the model) | PASS |
| II. Refuse Rather Than Guess | Ambiguity gate + FR-006/FR-007/FR-013 refusal paths are first-class flows in data-model.md, not error handling bolted on | PASS |
| III. Typed Tools Only, No Open Computation | MCP tool contracts (contracts/) are a fixed, named set; no free-form query tool defined; timeouts and a call cap are in Technical Context | PASS |
| IV. Provenance on Every Number | Every entity in data-model.md carries source-row references | PASS |
| V. Test-First for the Deterministic Core | Project Structure below sequences fixtures → engine + tests → MCP → model layer, matching `docs/plan.md`'s Day 1–4 order | PASS |
| VI. Instrument From the First API Call | Instrumentation log is a first-class entity in data-model.md, not deferred | PASS |

No violations. Complexity Tracking section below is empty on purpose.

## Project Structure

### Documentation (this feature)

```text
specs/001-margin-reconciliation-qa/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md         # Phase 1 output
├── quickstart.md         # Phase 1 output
├── contracts/            # Phase 1 output — MCP tool contracts
└── tasks.md              # Phase 2 output (/speckit-tasks — not yet created)
```

### Source Code (repository root)

```text
backend/
├── cmd/
│   └── server/           # main package: HTTP server + MCP tool registration
├── internal/
│   ├── ingest/            # CSV parsing for delivery/POS/cost/promo exports
│   ├── reconcile/         # deterministic margin + promo-ROI engine (Principle I core)
│   ├── mcptools/          # typed MCP tool definitions wrapping reconcile/ (Principle III boundary)
│   ├── ambiguity/         # pre-processing gate (Claude Haiku 4.5 call)
│   ├── explain/           # explanation step (Claude Sonnet 5 call, narrates tool results)
│   ├── instrumentation/   # per-interaction logging (tokens, cost, latency, refusal flags)
│   └── storage/           # sqlc-generated Postgres access
├── migrations/            # golang-migrate schema files
└── fixtures/              # generated fixture CSVs live here, referenced by ingest/ and evaluation/

frontend/
├── src/
│   ├── components/        # chat UI (shadcn AI Elements), provenance display, cost panel
│   ├── pages/
│   └── services/          # API client to backend/cmd/server
└── tests/

evaluation/
├── promptfoo/              # accuracy, consistency, refusal-correctness test configs
└── golden/                 # independently-computed correct answers for accuracy questions
```

**Structure Decision**: Option 2 (web application: Go backend + React frontend),
per `CLAUDE.md`'s Stack section and the constitution's Technology & Scope
Constraints — already decided, not re-derived here. `internal/reconcile/` and
`internal/mcptools/` are kept as separate packages specifically to make
Principle III's boundary a package boundary, not just a convention Claude
Code has to remember to respect.

## Complexity Tracking

*No Constitution Check violations — this section is intentionally empty.*
