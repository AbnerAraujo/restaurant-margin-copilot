# Plan — now through Tuesday

Interview: **Tuesday, Sept 1, 2026**. Today is Thursday, Aug 27. That's 4 working
days plus the interview morning itself — tight, not "plenty of days." Sequencing
follows the constitution's fixed build order (Principle V): fixtures → engine +
tests → MCP tools → model layer → instrumentation → harness → UI. That order
does not move, even under time pressure — reordering it is the one mistake the
whole project is built to avoid.

## Day 0 — Thursday Aug 27 (done)

- [x] Dev environment: Homebrew, Go, Node, Docker, `gh`
- [x] Private GitHub repo created and pushed
- [x] GitHub Spec Kit installed, constitution ratified (v1.0.0)
- [x] 30 Claude Code skills installed (Go, React, clean code, architecture, product strategy) + `promptfoo`/`sqlc`/`golang-migrate`
- [x] `docs/product-strategy.md` — vision, North Star, KPIs, tagged hypotheses

## Day 1 — Friday Aug 28

- [ ] `/speckit-specify` — formal baseline spec from `product-strategy.md` + `CLAUDE.md`
- [ ] `/speckit-plan` → `/speckit-tasks` — technical plan and task breakdown
- [ ] Fixture data: delivery-platform export, POS export, supplier cost sheet — with the deliberate mess (duplicate order, refund, missing day, inconsistent date format)
- [ ] Go module scaffolded (`backend/`, per `golang-project-layout`); reconciliation engine skeleton with tests written first (Principle V) — no LLM call exists yet by end of day

## Day 2 — Saturday Aug 29

- [ ] Reconciliation engine complete: parsing, matching, margin calc, week-over-week deltas, anomaly thresholds — full test coverage (testify, table-driven)
- [ ] Postgres schema + migrations (`golang-migrate`), `sqlc` queries
- [ ] MCP tool layer (`mark3labs/mcp-go`) wrapping the engine as typed tools — no open SQL, timeouts on every call

## Day 3 — Sunday Aug 30

- [ ] Ambiguity gate (cheap model) — answerable/ambiguous check before any tool call
- [ ] OpenAI explanation step (stronger model), direct API calls against the MCP tools — no agent framework
- [ ] Instrumentation from the first real API call: tokens, cost, latency, refusal/clarify flags, logged to Postgres
- [ ] Refusal path fully wired and tested

## Day 4 — Monday Aug 31

- [ ] Evaluation harness: ~15–20 accuracy questions, 5×3 consistency phrasings, ~5 refusal-correctness questions — run it, record real numbers, including failures
- [ ] Fix what's fixable from harness results; log what isn't as a known limitation (not silently)
- [ ] React frontend: chat UI, provenance display, running cost panel — functional over polished
- [ ] Stop new feature work by end of day regardless of state — Day 5 is writing and rehearsal, not coding

## Day 5 — Tuesday Sept 1 (interview day)

- [ ] One-page reasoning doc (`one-pager-prd` skill), built from real harness numbers: job chosen and why, deterministic/probabilistic boundary, hard/soft limits, evaluation numbers including failures, cost per interaction, what was deliberately not built and why
- [ ] The "where the model got it wrong during the build and how I caught it" passage — from the real running log kept during Days 1–4, not reconstructed from memory
- [ ] Demo recorded or rehearsed live, including at least one on-screen refusal
- [ ] Final read-through against the constitution and the hard-truth rules on background claims before walking in

## Running log of real mistakes (fill in as we go — do not backfill from memory later)

- —
