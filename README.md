# My Business Steward (Restaurant Margin Copilot)

A daily-close and margin-alert copilot for an independent restaurant. The
owner gets sales exports from delivery platforms (iFood, Just Eat Takeaway)
and the in-house POS, plus supplier cost sheets — and nobody reconciles them
daily because it's tedious. Margin slippage is usually discovered when the
month closes, too late to act on. This ingests those files, reconciles them
deterministically, and answers plain-language questions about the day and
the week — flagging what changed, why, and refusing rather than guessing
when it doesn't know.

Built as a take-home prototype for a Prosus/Toqan Technical PM interview
challenge. `CLAUDE.md` in this repo is the original brief and constitution the
whole build follows.

- **Live presentation** (26-slide deck, arrow-key navigable): https://claude.ai/code/artifact/17a46fdf-c587-45c6-b1d6-904f1a03bc70 — checked in at [`docs/presentation.html`](docs/presentation.html)
- **Live architecture diagram** (design system, reconciliation engine, full system): https://claude.ai/code/artifact/dcda16f7-44d7-4160-8f72-d8593f432441 — checked in at [`docs/architecture.html`](docs/architecture.html)
- **Live API docs** (interactive Swagger UI, every backend endpoint): https://claude.ai/code/artifact/6781bd96-bfa1-4fd7-821a-fe35cd3ac764 — checked in at [`docs/api.html`](docs/api.html), generated from the spec at [`docs/openapi.yaml`](docs/openapi.yaml)

## Contents

- [What's real right now](#whats-real-right-now)
- [The core idea: deterministic engine, probabilistic narrator](#the-core-idea-deterministic-engine-probabilistic-narrator)
- [Getting started](#getting-started)
- [One dataset: hand-authored opening, generated history](#one-dataset-hand-authored-opening-generated-history)
- [Documentation map](#documentation-map)
- [User stories and specs](#user-stories-and-specs-spec-driven-development)
- [Real evaluation results](#real-evaluation-results)
- [Stack](#stack)
- [The 7 MCP tools and the Claude Code skills used to build this](#the-7-mcp-tools-and-the-claude-code-skills-used-to-build-this)
- [Non-goals](#non-goals)

## What's real right now

Queried live against the running Postgres database on 2026-08-29:

| | |
|---|---|
| Reconciled days in the database | **759** — one continuous dataset, 2024-08-01 through 2026-08-29 (today), whose first 14 days are hand-authored ground truth — see [below](#one-dataset-hand-authored-opening-generated-history) |
| Logged model interactions | **635**, cumulative real Anthropic API spend **$7.3698**, per-call in `question_interaction` |
| Accuracy on the eval harness's known-answer questions | **15/15 (100%)**, grounded in the dataset's hand-authored opening window — see [Real evaluation results](#real-evaluation-results) |
| MCP tools exposed to the model | **7** typed, read-only tools — no open SQL, no free-form computation |
| Frontend pages | Home, Ask (chat), Close, Promotions, Platforms, Points, Upload, Settings |

Shipped since the original take-home submission, in addition to the core
product: a **points-payment feature** (fund a promotion's spend with earned
Steward points instead of cash, at a fixed 10¢/point rate — see
`backend/internal/httpapi/promotions_create.go`'s `payment_method` field), a
**deterministic capability-question path** (answers "what do you do?" with a
hand-written, tool-grounded answer, before any model call runs, at zero
cost), a warmer chat tone (green refusal styling instead of red, warm
narration), a **Settings page**, and the two-year synthetic dataset described
below. A larger v2 spec (`specs/008-dashboard-chat-intelligence-v2`,
comparisons and proactive chat guidance built on top of that new history) is
drafted but not yet implemented — see the [spec table](#user-stories-and-specs-spec-driven-development).

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
docker compose up -d                                          # Postgres
cd backend && go run ./cmd/gendata -out data/live && cd ..    # generate the dataset (opening days + synthetic history)
go run ./backend/cmd/server -ingest backend/data/live         # reconcile + persist every day
go run ./backend/cmd/server -ingest-promo backend/data/live   # reconcile + persist promotion/ad-spend data
go run ./backend/cmd/server -serve :8080                      # backend API
cd frontend && npm install && npm run dev                     # frontend (Vite)
```

How that dataset is put together — and why its first 14 days are special —
is described in
[One dataset: hand-authored opening, generated history](#one-dataset-hand-authored-opening-generated-history) below.

### Installing it as a Mac app

The frontend is a real PWA (`vite-plugin-pwa`, a real manifest and service
worker — check with both backend and frontend above running):

1. Open `http://localhost:5173` in Chrome.
2. Click the install icon (⊕) at the right of the address bar, or Chrome
   menu → **Cast, save, and share** → **Install My Business Steward…**.
3. It opens in its own window and lands in Launchpad/Spotlight as
   **Steward**, with a real Dock icon.

The installed app still talks to `localhost:8080` — the backend needs to be
running (`go run ./backend/cmd/server -serve :8080`) whenever you open it,
same as any other local-first tool in this repo. The service worker only
caches the app's own JS/CSS/icons; it never caches `/api/*`, so every
number you see always comes from a live request, never a stale cache.

## One dataset: hand-authored opening, generated history

The product serves exactly one dataset — `backend/data/live/` (git-ignored,
regenerated by `backend/cmd/gendata`), 759 days from **2024-08-01 through
today (2026-08-29)**, one timeline, one ingestion path. Two different
origins inside it, by design:

- **The hand-authored opening window** (2024-08-01 through 2024-08-14) —
  checked into git at `backend/cmd/gendata/opening/` and emitted verbatim at
  the top of every generated CSV. These 14 days carry the deliberate
  messiness the brief requires (a byte-identical duplicate order, a refund
  settling a week after its order, a missing delivery-platform day, a
  systematically inconsistent date format between two export systems) at the
  same realistic dollar scale as everything after them, and every reference
  value for them was computed by hand and cross-checked with a throwaway
  script **before** being compared against the Go engine
  (`opening/README.md`). This window is the ground truth the evaluation
  harness and the reconciliation golden tests grade against — an accuracy
  test whose expected answers came from the system's own output could never
  catch a reconciliation bug, only a narration bug.
- **The generated history** (2024-08-15 through today) — synthesized by the
  same `cmd/gendata` run on a deterministic seed: a logistic growth curve
  (~$34k/month gross at the start, ~$125k/month at the end), weekly
  seasonality, real cost-shock days, and six research-backed monthly loss
  regimes (see `cmd/gendata`'s own source ledger and
  `docs/product-strategy.md`), continuous with the opening window across the
  seam.

The live app, the backend test suite, and the evaluation harness all read
this one dataset from the same Postgres database — there is no separate
evaluation dataset, database, or scale anywhere.

## Documentation map

Everything here was produced through a real spec-driven process — Definition
of Ready → PRD → Technical RFC → spec/plan per feature — not written after the
fact.

| Doc | What it covers |
|---|---|
| [`docs/dor.md`](docs/dor.md) | Definition of Ready — problem framing, scope, and what had to be true before build started |
| [`docs/prd.md`](docs/prd.md) | Product Requirements Document — user stories, KRs, success criteria |
| [`docs/technical-rfc.md`](docs/technical-rfc.md) | Technical RFC — stack choices, architecture decisions, alternatives considered and rejected |
| [`docs/rfc-multi-tenant.md`](docs/rfc-multi-tenant.md) | Standalone RFC for multi-tenant support — status: **proposed, not approved for implementation**, kept as a design exercise |
| [`docs/product-strategy.md`](docs/product-strategy.md) | Product strategy, roadmap, and the real, honest evaluation-results writeup, dated entry by entry (numbers on this page are sourced from here and from a live database query) |
| [`docs/plan.md`](docs/plan.md) | The full build log — every phase, every mistake made and how it was fixed, in order |
| [`docs/test-plan.md`](docs/test-plan.md) | Test strategy across unit, integration, and live-API-gated tests |
| [`docs/live-integration-test-scenarios.md`](docs/live-integration-test-scenarios.md) | Scenarios that exercise the real Anthropic API and real Postgres, not mocks |
| [`docs/tooling.md`](docs/tooling.md) | Toolchain and dependency choices |
| [`docs/why-ai.md`](docs/why-ai.md) | Why this problem is a good fit for an LLM layer, and where it deliberately isn't used |
| [`docs/brand.md`](docs/brand.md) | Visual identity / design tokens used across the app and docs |
| [`docs/frontend.md`](docs/frontend.md) | Frontend design system and architecture reference — real file paths, real consumer counts, real bugs found and fixed |
| [`docs/openapi.yaml`](docs/openapi.yaml) + [`docs/api.html`](docs/api.html) (also live ↗ above) | OpenAPI 3.0 spec for every backend endpoint, grounded against real live responses, rendered as an interactive Swagger UI page |
| [`docs/mcp-and-skills.md`](docs/mcp-and-skills.md) | The MCP typed-tool layer (all 7 tools, the timeout/call-cap middleware) and a fact-checked inventory of the Claude Code skills used to build this, including the two this project created itself |

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
| [`specs/008-dashboard-chat-intelligence-v2`](specs/008-dashboard-chat-intelligence-v2/spec.md) | Chat/dashboard follow-ups, comparisons, and other deterministic-only enhancements built on the new 2-year dataset | Spec drafted (2026-08-29) — not yet planned or built |

## Real evaluation results

Measured against the live backend with real Anthropic API calls
(`evaluation/promptfoo/{accuracy,consistency,refusal}.yaml`, 35 questions
total, all grounded in the dataset's hand-authored opening window — the only
slice whose correct answers were computed independently of this system's own
code), reported honestly including the failures — see
`docs/product-strategy.md`'s dated fix sections for the full breakdown,
root-cause analysis, and every before/after re-run, including the 2026-08-29
harness rebuild after the dataset unification.

| Metric | Result (2026-08-29 run, sequential, fresh cache) |
|---|---|
| Accuracy | 15/15 (100%) — the two long-standing failures (A7, a grading-regex false negative; A15, refund-by-source attribution) were fixed by the harness rebuild and an earlier tool-contract fix respectively |
| Consistency (5 questions × 3 phrasings each) | 15/15 after two grading-artifact fixes; the first pass scored 10/15, with all 5 failures hand-verified as correct answers tripping over-strict assertions (documented in `docs/product-strategy.md`) |
| Refusal correctness (5 unanswerable questions) | 5/5 (100%) |
| Cost per interaction, this eval run | ~$0.025/question average |
| Cumulative real API spend, all activity to date | $7.3698 across 635 logged interactions, queried live from `question_interaction` (2026-08-29) |

The deterministic reconciliation/ingestion/MCP-tool layer showed **zero
defects** across the full run — every failure traced to the model layer's
date-grounding and tool/entity-selection behavior, which is the specific
boundary this architecture's Go/model split is designed to contain.

**Why refusal correctness is the only pre-committed target here.** Accuracy
and consistency are measured and reported honestly, failures included, but
neither was promised in advance — only refusal correctness was, at 100%.
That's deliberate, not an easy target picked to look good: this product's
riskiest bet (`docs/product-strategy.md`'s Hypothesis H1) is that an
independent restaurant owner will trust a system that openly refuses or asks
a clarifying question over one that always answers confidently. Constitution
Principle II states the reasoning directly — "a confidently wrong margin
figure is a worse outcome than a refusal" — because in a margin-tracking
tool, a plausible-looking wrong number can drive a real business decision
(cutting a shift, dropping a supplier), where an honest "I don't have that
data" costs nothing. The 5/5 result is scored against
`evaluation/promptfoo/refusal.yaml`'s five deliberately unanswerable-or-
ambiguous questions (a missing delivery-source day, an unattributable
promo's ROI, a data source that doesn't exist, a date before the dataset
begins, and a genuinely ambiguous "how was the weekend") — each one
engineered against a real gap the ambiguity gate has to catch *before* any
tool call runs, not a softball.

## Stack

- **Backend**: Go, `sqlc` + `pgx/v5` + `golang-migrate` over PostgreSQL, fixed-point cents arithmetic — no floats near money
- **MCP layer**: `mark3labs/mcp-go`, a fixed set of typed tools, no open SQL
- **Model**: Anthropic API direct (no agent framework) — Claude Sonnet 5 for the ambiguity gate and narration, Claude Haiku 4.5 for the paraphrase-match cache classifier. The gate moved from Haiku 4.5 to Sonnet 5 on 2026-08-29 after Haiku proved unreliable at multi-year date comparison once the live dataset grew past a single year (see `internal/llmclient/cost.go`)
- **Frontend**: React 19 + TypeScript + Vite + Tailwind v4 + shadcn/ui, installable as a PWA (`vite-plugin-pwa`)
- **Evaluation**: `promptfoo` harness, real numbers above
- **Docs/skills**: built with GitHub Spec Kit (SDD), and Claude Code skills for data visualization, presentation design, and UX review

## The 7 MCP tools and the Claude Code skills used to build this

The model never has open SQL or a free-form computation tool — only these seven typed, read-only tools (`backend/internal/mcptools/`), each refusing rather than estimating when the data it needs isn't there:

| Tool | Answers |
|---|---|
| `get_daily_summary` | One day's full reconciliation: sales by source, commissions, refunds, input costs, margin, flags |
| `get_margin_delta` | Margin delta between two periods (e.g. week-over-week) |
| `list_discrepancies` | Discrepancy flags for a date or period |
| `get_promotion_roi` | ROI for one campaign, by id or fuzzy name |
| `list_negative_roi_promotions` | Every campaign losing money in a period |
| `compare_platform_economics` | iFood vs. Just Eat Takeaway commission/promo cost, side by side |
| `get_period_totals` | A whole period's totals, averages, and best/worst day in one call |

Full contracts (inputs, refusal conditions, provenance shape): [`specs/001-margin-reconciliation-qa/contracts/mcp-tools.md`](specs/001-margin-reconciliation-qa/contracts/mcp-tools.md); the fact-checked build-time inventory below is [`docs/mcp-and-skills.md`](docs/mcp-and-skills.md).

Claude Code skills actually used, by name (not a generic "AI helped" claim — each is named in a real commit message):

- **GitHub Spec Kit / SDD** — the whole `constitution → specify → plan → tasks → analyze → implement` flow, all ten `speckit-*` commands, across the core spec and follow-on specs
- **`dataviz`** — chart-type selection and the categorical/sequential/diverging palette rules
- **`design-review`, `redesign`, `apply-aesthetic`, `design-component`** — the frontend visual revamp
- **`make-slide`** — this presentation deck
- **`ux-writing`** — the chat's refusal, clarification, and error copy (including the later red→green retone)
- **`skill-creator`** — used to build this project's own two skills: **`question-recovery-design`** (refusal/clarification UX, generalized from this codebase) and **`proactive-guidance-design`** (proactive capability surfacing and follow-up suggestions, its sibling)
- **`inspired-product`** (Marty Cagan's *Inspired*/*Empowered* framework) — opportunity-assessment and OKR/vision rigor applied to this deck's own strategy slides

## Non-goals

- Not a general restaurant assistant, not an open-ended chat box
- Not a multi-agent architecture for its own sake
- Not a production system — a prototype built to demonstrate judgment, meant to be opened and used by someone else
- No authentication or multi-tenancy — single-owner, single-restaurant by design; multi-tenant is spec'd and RFC'd (see the table above) but explicitly not approved for implementation
- No non-English input — Portuguese-language input was scored and deliberately deferred (`specs/008-dashboard-chat-intelligence-v2`'s Assumptions) rather than risk regressing the tuned English ambiguity-gate/explain prompts this close to the interview date
