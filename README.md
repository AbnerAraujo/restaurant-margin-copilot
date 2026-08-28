# My Business Steward (Restaurant Margin Copilot)

A daily-close and margin-alert copilot for an independent restaurant. The owner
gets sales exports from delivery platforms (iFood, JET) and the in-house POS,
plus supplier cost sheets — and nobody reconciles them daily because it's
tedious. Margin slippage is usually discovered when the month closes, too late
to act on. This ingests those files, reconciles them deterministically, and
answers plain-language questions about the day and the week — flagging what
changed, why, and refusing rather than guessing when it doesn't know.

Built as a take-home prototype for a Prosus/Toqan Technical PM interview
challenge. `CLAUDE.md` in this repo is the original brief and constitution the
whole build follows.

- **Live presentation** (22-slide deck, arrow-key navigable): https://claude.ai/code/artifact/17a46fdf-c587-45c6-b1d6-904f1a03bc70 — also checked in at [`docs/presentation.html`](docs/presentation.html)
- **Live architecture diagram** (design system, reconciliation engine, full system): https://claude.ai/code/artifact/dcda16f7-44d7-4160-8f72-d8593f432441 — also checked in at [`docs/architecture.html`](docs/architecture.html)
- **Live API docs** (interactive Swagger UI, every backend endpoint): https://claude.ai/code/artifact/6781bd96-bfa1-4fd7-821a-fe35cd3ac764 — spec checked in at [`docs/openapi.yaml`](docs/openapi.yaml)

## The core idea: deterministic engine, probabilistic narrator

**Every number is computed in Go, never by a model.** Ingestion, reconciliation,
margin math, week-over-week deltas, and ROI are all deterministic, unit-tested
Go code. The Anthropic model's only job is to interpret the owner's question,
call a fixed set of **typed** MCP tools backed by that Go engine, and narrate
the result in plain language. It never does arithmetic and it never sees raw
SQL — no open-ended computation tool exists for it to reach for.

A pre-processing gate classifies every question — `answerable` / `ambiguous` /
`unanswerable` — **before** any expensive reasoning call runs. When data is
missing or a question can't be grounded, the system refuses or asks a
clarifying question instead of estimating. A confidently wrong margin figure
is worse than an honest "I don't have that."

Every number shown carries provenance — which file, which rows, which period —
and every model interaction is logged with tokens, cost, latency, and whether
a refusal or clarification fired.

## Getting started

See [`docs/SETUP.md`](docs/SETUP.md) for full local setup (Go, Postgres via
`docker-compose.yml`, Node/Vite, environment variables in `.env.example`).
Quick shape of it:

```bash
docker compose up -d                              # Postgres
go run ./backend/cmd/server -ingest backend/fixtures        # seed + reconcile fixture data
go run ./backend/cmd/server -serve :8080                    # backend API
cd frontend && npm install && npm run dev                   # frontend (Vite)
```

## Documentation map

Everything here was produced through a real spec-driven process — Definition
of Ready → PRD → Technical RFC → spec/plan per feature — not written after the
fact.

| Doc | What it covers |
|---|---|
| [`docs/dor.md`](docs/dor.md) | Definition of Ready — problem framing, scope, and what had to be true before build started |
| [`docs/prd.md`](docs/prd.md) | Product Requirements Document — user stories, KRs, success criteria |
| [`docs/technical-rfc.md`](docs/technical-rfc.md) | Technical RFC — stack choices, architecture decisions, alternatives considered and rejected |
| [`docs/rfc-multi-tenant.md`](docs/rfc-multi-tenant.md) | Standalone RFC for multi-tenant support — explicitly **not approved for implementation**, kept as a design exercise |
| [`docs/product-strategy.md`](docs/product-strategy.md) | Product strategy, roadmap, and the real, honest evaluation-results writeup (numbers below are sourced from here) |
| [`docs/plan.md`](docs/plan.md) | The full build log — every phase, every mistake made and how it was fixed, in order |
| [`docs/test-plan.md`](docs/test-plan.md) | Test strategy across unit, integration, and live-API-gated tests |
| [`docs/live-integration-test-scenarios.md`](docs/live-integration-test-scenarios.md) | Scenarios that exercise the real Anthropic API and real Postgres, not mocks |
| [`docs/tooling.md`](docs/tooling.md) | Toolchain and dependency choices |
| [`docs/why-ai.md`](docs/why-ai.md) | Why this problem is a good fit for an LLM layer, and where it deliberately isn't used |
| [`docs/brand.md`](docs/brand.md) | Visual identity / design tokens used across the app and docs |
| [`docs/frontend.md`](docs/frontend.md) | Frontend design system and architecture reference — real file paths, real consumer counts, real bugs found and fixed |
| [`docs/openapi.yaml`](docs/openapi.yaml) + **[live API docs ↗](https://claude.ai/code/artifact/6781bd96-bfa1-4fd7-821a-fe35cd3ac764)** | OpenAPI 3.0 spec for every backend endpoint, grounded against real live responses, rendered as an interactive Swagger UI page |
| [`docs/mcp-and-skills.md`](docs/mcp-and-skills.md) | The MCP typed-tool layer (all 6 tools, the timeout/call-cap middleware) and a fact-checked inventory of the Claude Code skills used to build this, including the one this project created itself |

## User stories and specs (spec-driven development)

Each feature was scoped through GitHub Spec Kit's `specify → plan → tasks →
analyze → implement` flow. `spec.md` in each directory holds the user stories,
acceptance criteria, and functional requirements; most also have a `plan.md`
(technical design) and a `checklists/requirements.md` (spec-quality gate).

| Spec | Feature | Status |
|---|---|---|
| [`specs/001-margin-reconciliation-qa`](specs/001-margin-reconciliation-qa/spec.md) | Daily Margin & Growth Copilot — the core product (ingestion, reconciliation, chat Q&A, MCP tools, instrumentation) | Shipped |
| [`specs/002-badge-expansion`](specs/002-badge-expansion/spec.md) | Badge system expansion — Growth, Engagement, and Campaign-Creation gamification categories | Shipped |
| [`specs/003-platform-comparator`](specs/003-platform-comparator/spec.md) | Cross-platform economics comparator — real per-platform commission breakdown | Shipped |
| [`specs/004-semantic-cache`](specs/004-semantic-cache/spec.md) | Paraphrase-aware answer cache — skip the LLM on a re-asked question, even reworded | Shipped |
| [`specs/005-multi-tenant`](specs/005-multi-tenant/spec.md) | Multi-tenant support (Segment 2 expansion) | Spec + RFC only — not built, deliberately gated |
| [`specs/007-cost-sheet-upload`](specs/007-cost-sheet-upload/spec.md) | Cost-sheet upload UI — validation, preview, template, commit-and-reconcile | Shipped |

## Real evaluation results

Measured against the live backend with real Anthropic API calls
(`evaluation/promptfoo/{accuracy,consistency,refusal}.yaml`, 35 questions
total), reported honestly including the failures — see
`docs/product-strategy.md`'s "Real evaluation results" and "Fix verification:
before/after" sections for the full breakdown and root-cause analysis.

| Metric | Result |
|---|---|
| Accuracy | 9/15 (60%) |
| Consistency (5 questions × 3 phrasings each) | 2/5 sets fully agree (promptfoo-strict); 3/5 agree in substance on manual read |
| Refusal correctness (5 unanswerable questions) | 5/5 (100%) |
| Cost per interaction | ~$0.0152/question average |
| Cumulative real API spend, this build | ~$0.96, against a self-imposed $5 cap |

The deterministic reconciliation/ingestion/MCP-tool layer showed **zero
defects** across the full run — every failure traced to the model layer's
date-grounding and tool/entity-selection behavior, which is the specific
boundary this architecture's Go/model split is designed to contain.

## Stack

- **Backend**: Go, `sqlc` + `pgx/v5` + `golang-migrate` over PostgreSQL, fixed-point cents arithmetic — no floats near money
- **MCP layer**: `mark3labs/mcp-go`, a fixed set of typed tools, no open SQL
- **Model**: Anthropic API direct (no agent framework) — Claude Haiku 4.5 for cheap classification (ambiguity gate, paraphrase matching), Claude Sonnet 5 for narration and harder judgment calls
- **Frontend**: React + TypeScript + Vite + Tailwind v4 + shadcn/ui
- **Evaluation**: `promptfoo` harness, real numbers above
- **Docs/skills**: built with GitHub Spec Kit (SDD), and Claude Code skills for data visualization, presentation design, and UX review

## Non-goals

- Not a general restaurant assistant, not an open-ended chat box
- Not a multi-agent architecture for its own sake
- Not a production system — a prototype built to demonstrate judgment, meant to be opened and used by someone else
