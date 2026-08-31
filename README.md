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

- **Live presentation** (24-slide deck, arrow-key navigable): https://claude.ai/code/artifact/17a46fdf-c587-45c6-b1d6-904f1a03bc70 — checked in at [`docs/presentation.html`](docs/presentation.html)
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
- [The 8 MCP tools and the Claude Code skills used to build this](#the-8-mcp-tools-and-the-claude-code-skills-used-to-build-this)
- [Non-goals](#non-goals)

## What's real right now

Queried live against the running Postgres database on 2026-08-30:

| | |
|---|---|
| Reconciled days in the database | **760** — one continuous dataset, 2024-08-01 through 2026-08-30, whose first 14 days are hand-authored ground truth — see [below](#one-dataset-hand-authored-opening-generated-history) |
| Logged model calls | **1,166**, cumulative real Anthropic API spend **$14.87** — $14.7741 across 1,129 rows in `question_interaction`, $0.0401 across 29 in `paraphrase_match`, $0.0558 across 8 in `business_insight_interaction` |
| Measured cost per question | **$0.0313** — 70 questions through the full uncached gate+explain path, 142 model calls, $2.1931 (KR4's bar is $0.05) |
| Accuracy on the eval harness's known-answer questions | **14/15**, twice, with the cache disabled — the one failure is a real tool-contract gap, described in [Real evaluation results](#real-evaluation-results) |
| Earned Steward points | **12,370** (1,000 spent, 11,370 available) from 776 badges — 458 Clean Close, 302 Discrepancy Catcher, 16 Growth; live from `GET /api/badges` |
| MCP tools exposed to the model | **8** typed, read-only tools — no open SQL, no free-form computation |
| Frontend pages | Home, Ask (chat), Close, Promotions, Platforms, Points, Upload, Profile, Settings, Help |
| Delivery-revenue sources | **2** — an uploaded CSV export, or the simulated platform connector proxy (iFood + Just Eat Takeaway), both producing the identical record type |

A note on units, because the earlier version of this table blurred them: a
row in `question_interaction` is one **model call**, not one question. An
answered question writes two (the ambiguity gate, then the explanation), so
the per-question figure above — the one KR4 is stated over — is measured
over a known question count, not by averaging the ledger.

Shipped since the original take-home submission, in addition to the core
product: a **points-payment feature** (fund a promotion's spend with earned
Steward points instead of cash, at a fixed 10¢/point rate — see
`backend/internal/httpapi/promotions_create.go`'s `payment_method` field), a
**deterministic capability-question path** (answers "what do you do?" with a
hand-written, tool-grounded answer, before any model call runs, at zero
cost), a warmer chat tone (green refusal styling instead of red, warm
narration), a **Settings page**, the two-year synthetic dataset described
below, and a **platform connector proxy** — one internal interface over two
deliberately incompatible mock partner APIs (iFood and Just Eat Takeaway),
normalizing both into the same record the CSV parser produces so
`internal/reconcile` has a zero-line diff from it. **Both connectors are
simulated**: this project has no partner-API credentials for either platform,
and the emulation is stated in the tab label, a persistent notice, each
platform row, every API response body, and the `simulated://` provenance on
every record it writes. A larger v2 spec (`specs/008-dashboard-chat-intelligence-v2`,
comparisons and proactive chat guidance built on top of that new history) is
shipped — see the [spec table](#user-stories-and-specs-spec-driven-development).

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

Requires Go 1.27+, Node 20+, Docker, and the
[`golang-migrate`](https://github.com/golang-migrate/migrate) CLI
(`brew install golang-migrate`) for the schema step below.
[`docs/SETUP.md`](docs/SETUP.md) is a dated, machine-bootstrap doc (installing
Homebrew/Go/Node/Docker/`gh` themselves on a machine that starts with none of
them) — it predates this app and does not cover running it, so it isn't
linked as a second source of truth here. Everything needed once those tools
are present is below — every command runs top-to-bottom, in order, from the
repo root unless a `cd` says otherwise (QA finding: an earlier version of
this block ran `go run ./backend/cmd/server ...` from the repo root, which
fails immediately with "cannot find main module" — the Go module root is
`backend/`, not the repo root — and never exported `DATABASE_URL` or applied
the schema migration before the first `-ingest`, which fails with
"relation ... does not exist" on a genuinely fresh database):

```bash
cp .env.example .env                                            # then fill in ANTHROPIC_API_KEY
set -a && source .env && set +a                                 # export DATABASE_URL/ANTHROPIC_API_KEY for the commands below
docker compose up -d                                             # Postgres
migrate -path backend/migrations -database "$DATABASE_URL" up    # apply the schema — required once, before any ingest
cd backend
go run ./cmd/gendata -out data/live                              # generate the dataset (opening days + synthetic history)
go run ./cmd/server -ingest data/live                             # reconcile + persist every day
go run ./cmd/server -ingest-promo data/live                       # reconcile + persist promotion/ad-spend data
go run ./cmd/server -serve :8080                                  # backend API
cd ../frontend && npm install && npm run dev                     # frontend (Vite)
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
running (`go run ./cmd/server -serve :8080`, from `backend/`) whenever you
open it, same as any other local-first tool in this repo. The service worker only
caches the app's own JS/CSS/icons; it never caches `/api/*`, so every
number you see always comes from a live request, never a stale cache.

## One dataset: hand-authored opening, generated history

The product serves exactly one dataset — `backend/data/live/` (git-ignored,
regenerated by `backend/cmd/gendata`), 760 days from **2024-08-01 through
today (2026-08-30)**, one timeline, one ingestion path. Two different
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
fact. (Two exceptions, disclosed rather than smoothed over: `specs/014` and
`specs/015` were built directly from conversation and only specced
retroactively, after the code shipped — see each one's plan.md for why, and
why that's stated here instead of quietly backfilled.)

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
| [`docs/mcp-and-skills.md`](docs/mcp-and-skills.md) | The MCP typed-tool layer (all 8 tools, the timeout/call-cap middleware) and a fact-checked inventory of the Claude Code skills used to build this, including the two this project created itself |

## User stories and specs (spec-driven development)

Each feature was scoped through GitHub Spec Kit's `specify → plan → tasks →
analyze → implement` flow — except `014` and `015`, whose code shipped first
and were specced afterward (flagged in their own `Status` row below and
explained in their `plan.md`). `spec.md` in each directory holds the user
stories, acceptance criteria, and functional requirements; most also have a
`plan.md` (technical design) and a `checklists/requirements.md` (spec-quality
gate).

| Spec | Feature | Status |
|---|---|---|
| [`specs/001-margin-reconciliation-qa`](specs/001-margin-reconciliation-qa/spec.md) | Daily Margin & Growth Copilot — the core product (ingestion, reconciliation, chat Q&A, MCP tools, instrumentation) | Shipped |
| [`specs/002-badge-expansion`](specs/002-badge-expansion/spec.md) | Badge system expansion — Growth, Engagement, and Campaign-Creation gamification categories | Shipped |
| [`specs/003-platform-comparator`](specs/003-platform-comparator/spec.md) | Cross-platform economics comparator — real per-platform commission breakdown | Shipped |
| [`specs/004-semantic-cache`](specs/004-semantic-cache/spec.md) | Paraphrase-aware answer cache — skip the LLM on a re-asked question, even reworded | Shipped |
| [`specs/005-multi-tenant`](specs/005-multi-tenant/spec.md) | Multi-tenant support (Segment 2 expansion) | Spec + RFC only — not built, deliberately gated |
| [`specs/007-cost-sheet-upload`](specs/007-cost-sheet-upload/spec.md) | Cost-sheet upload UI — validation, preview, template, commit-and-reconcile | Shipped |
| [`specs/008-dashboard-chat-intelligence-v2`](specs/008-dashboard-chat-intelligence-v2/spec.md) | Chat/dashboard follow-ups, comparisons, and other deterministic-only enhancements built on the new 2-year dataset | Shipped (41/42 tasks checked in `tasks.md`; the one remaining item is a pre-work baseline check, not a feature gap) |
| [`specs/009-business-insight-advisor`](specs/009-business-insight-advisor/spec.md) | Business Insight Advisor — a deterministic Go-derived teaser plus an opt-in, separately-ledgered Claude Sonnet 5 advice call | Shipped |
| [`specs/010-platform-connector-proxy`](specs/010-platform-connector-proxy/spec.md) | Platform Connector Proxy — one internal interface over two **simulated** iFood and Just Eat Takeaway partner APIs, normalizing both into the CSV path's own record type | Shipped (connectors emulated — no real partner-API access) |
| [`specs/011-inline-grounded-advice`](specs/011-inline-grounded-advice/spec.md) | Inline Grounded Advice — widens spec 009's advisor with a second avenue triggered by an explicit ask inside the question itself ("how can I improve my margin?"), still grounded exclusively in that turn's own tool results, no new MCP tool | Shipped |
| [`specs/012-pos-connector-dedup`](specs/012-pos-connector-dedup/spec.md) | POS connector plus deterministic cross-source deduplication — an integrated POS records a delivery platform's orders as its own tickets, so the same order arrives twice; a two-tier matcher resolves what it can and **refuses to guess** at the rest, because a wrong merge deletes real revenue as surely as a missed one double-counts it | Shipped (POS emulated — no real terminal access) |
| [`specs/013-bff-layer`](specs/013-bff-layer/spec.md) | A named BFF boundary for the owner app — `internal/bff` declares the whole API surface as one route table, so the CORS preflight, the 405 policy, and the startup log are all *derived* from it instead of hand-maintained beside it (the bug that motivated this: `PUT /api/profile`'s preflight never advertised PUT, so it failed silently from the browser but worked via `curl`); also unifies file-upload and simulated-connector ingestion into one `GET /api/sources` vocabulary | Shipped |
| [`specs/014-connector-variance-and-upload-sync`](specs/014-connector-variance-and-upload-sync/spec.md) | Cost-sheet upload can pull in the matching simulated platform revenue for its own invoice date range and commit both through one atomic pipeline run, and simulated connector days gained a seven-condition weighted trading model (severe weather, kitchen equipment failure, a short-staffed shift, an aggregator outage, and more) so they can go genuinely negative instead of staying uniformly healthy | Shipped — **spec written retroactively**, after the code (see plan.md) |
| [`specs/015-column-header-filters`](specs/015-column-header-filters/spec.md) | Excel/Sheets-style per-column header filters (categorical/text/numeric) on the two upload preview tables, composing additively with each page's existing filter bar and deliberately withheld from chart-fallback and capped-row tables; plus every search box switched from narrowing on each keystroke to narrowing only on Enter or a click | Shipped — **spec written retroactively**, after the code (see plan.md) |

## Real evaluation results

Measured against the live backend with real Anthropic API calls
(`evaluation/promptfoo/{accuracy,consistency,refusal}.yaml`, 35 questions
total, all grounded in the dataset's hand-authored opening window — the only
slice whose correct answers were computed independently of this system's own
code), reported honestly including the failures — see
`docs/product-strategy.md`'s dated fix sections for the full breakdown,
root-cause analysis, and every before/after re-run.

**These numbers replaced a higher, wrong set on 2026-08-30.** The harness
runs against its own backend on `:8092`, but that instance still shared the
product's `answer_cache` table, so a re-run was being served largely from
the *previous* run's cached answers — 25 of 35 questions, two suites
finishing in 0s. It was grading the cache, not the model. `cmd/server` now
takes `-eval-no-answer-cache`, and everything below is measured with it on,
so every graded answer is a real gate + explain round trip. Because the
model layer is not deterministic, an initial two-run measurement (14/15 and
14/15 accuracy, 13/15 and 15/15 consistency, 5/5 and 4/5 refusal) was
superseded on 2026-08-31 by a third full run — two data points aren't
enough to call a pattern — and all three runs are reported rather than the
best one.

| Metric | Run A | Run B | Run C |
|---|---|---|---|
| Accuracy (15 known-answer questions) | 14/15 | 15/15 | 14/15 |
| Consistency (5 questions × 3 phrasings) | 14/15 | 15/15 | 15/15 |
| Refusal correctness (5 unanswerable questions) | 4/5 | 4/5 | 4/5 |

Aggregate across all three suites, all three runs: **99/105 (94.3%)**. Cost:
70 questions across the first two runs · 142 model calls · **$2.1931** ·
**$0.0313/question** (a separate ledger measurement, not re-taken in the
third run).

Every failure was hand-read from the raw JSON. None was a wrong number:

- **A15 — "Delivery revenue on 2024-08-02, net of the refund?" (the
  recurring accuracy miss — failed in 2 of the 3 uncached runs).** The
  model returns gross $446.25 and the $62.25
  refund, both with provenance, and then explicitly declines to net them to
  $384.00 because no tool returns a net-of-refund delivery figure and it
  will not present its own arithmetic as a computed result. That is
  Constitution Principle I behaving exactly as designed. The real defect is
  a **gap in the tool contract**, not in the model: `get_daily_summary`
  exposes gross and refunds separately and nothing exposes the net. This is
  the most useful thing the re-measurement found, and it is open, not fixed.
- **R1 — the missing-delivery day.** The answer states the source is
  missing and explicitly disclaims the `$0.00` as an absence rather than a
  real zero, which is the required behavior, but trips `refusal.yaml`'s
  blanket "no `$0.00` anywhere" guard. That same guard was diagnosed and
  removed from `consistency.yaml`'s C5 on 2026-08-29; `refusal.yaml` never
  got the matching fix. It is **deliberately left in place** — tuning a
  grader after watching it fail is how a number stops meaning anything.
- **C1a (run A).** One of three phrasings asked a clarifying question
  ("both platforms, or one?") instead of answering. A genuine consistency
  divergence — precisely what this suite exists to catch.
- **C3 (run A).** A correct, provenanced answer (−$450.75 on $610.00 spend
  against $159.25 attributed revenue) that says "lost money", which the
  assertion's `negative|not profitable|loss|underperform` vocabulary does
  not match. Also left untouched, for the same reason as R1.

A fifth failure was found and **fixed**: on the pre-fix runs, "How was the
weekend?" returned a 502 twice in four attempts because the ambiguity
gate's response hit its 1536-token output cap mid-clarification. An
ambiguous verdict is the gate's true worst case — it must classify, then
write both a clarifying question and its options in one budget. The cap is
now 2560 (`internal/ambiguity/gate.go`, the third such measured raise), and
the truncation has not recurred.

The deterministic reconciliation/ingestion/MCP-tool layer showed **zero
defects** across every run — as before, every failure sits in the model
layer or in the grader, which is the specific boundary this architecture's
Go/model split is designed to contain. KR2 and KR3 are no longer taken on
trust either: `TestOpeningWindow_PersistedWithZeroSilentDataLoss`
(`internal/storage`) and
`TestListNegativeRoiPromotions_RealDataset_FlagsLunchfixWithProvenance`
(`internal/mcptools`) assert both against the real persisted dataset.

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
data" costs nothing. The result is scored against
`evaluation/promptfoo/refusal.yaml`'s five deliberately unanswerable-or-
ambiguous questions (a missing delivery-source day, an unattributable
promo's ROI, a data source that doesn't exist, a date before the dataset
begins, and a genuinely ambiguous "how was the weekend") — each one
engineered against a real gap the ambiguity gate has to catch *before* any
tool call runs, not a softball.

And it is the one target where the honest answer is now **5/5 and 4/5, not
a flat 5/5**. The single miss is R1's grader, described above: the system
refused correctly and said why, and the assertion rejected the wording. The
pre-committed claim was 100% refusal *correctness*, and on a hand-read of
all ten graded answers across both runs it holds — but the number printed
by the harness is 9/10, and that is the number reported, because a target
you get to re-score yourself is not a target.

## Stack

- **Backend**: Go, `sqlc` + `pgx/v5` + `golang-migrate` over PostgreSQL, fixed-point cents arithmetic — no floats near money
- **MCP layer**: `mark3labs/mcp-go`, a fixed set of typed tools, no open SQL
- **Model**: Anthropic API direct (no agent framework) — Claude Sonnet 5 for the ambiguity gate and narration, Claude Haiku 4.5 for the paraphrase-match cache classifier. The gate moved from Haiku 4.5 to Sonnet 5 on 2026-08-29 after Haiku proved unreliable at multi-year date comparison once the live dataset grew past a single year (see `internal/llmclient/cost.go`)
- **Frontend**: React 19 + TypeScript + Vite + Tailwind v4 + shadcn/ui, installable as a PWA (`vite-plugin-pwa`)
- **Evaluation**: `promptfoo` harness, real numbers above
- **Docs/skills**: built with GitHub Spec Kit (SDD), and Claude Code skills for data visualization, presentation design, and UX review

## The 8 MCP tools and the Claude Code skills used to build this

The model never has open SQL or a free-form computation tool — only these eight typed, read-only tools (`backend/internal/mcptools/`), each refusing rather than estimating when the data it needs isn't there:

| Tool | Answers |
|---|---|
| `get_daily_summary` | One day's full reconciliation: sales by source, commissions, refunds, input costs, margin, flags |
| `get_margin_delta` | Margin delta between two periods (e.g. week-over-week) |
| `list_discrepancies` | Discrepancy flags for a date or period |
| `get_promotion_roi` | ROI for one campaign, by id or fuzzy name |
| `list_negative_roi_promotions` | Every campaign losing money in a period |
| `compare_platform_economics` | iFood vs. Just Eat Takeaway commission/promo cost, side by side |
| `get_period_totals` | A whole period's totals, averages, and best/worst day in one call |
| `get_expense_pattern_by_day_of_month` | Which position in the month (1st–31st) runs highest/lowest on average expense, across every month in a period |

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
