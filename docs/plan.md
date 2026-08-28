# Plan — now through Tuesday

Interview: **Tuesday, Sept 1, 2026**. Today is Thursday, Aug 27. That's 4 working
days plus the interview morning itself — tight, not "plenty of days." Sequencing
follows the constitution's fixed build order (Principle V): fixtures → engine +
tests → MCP tools → model layer → instrumentation → harness → UI. That order
does not move, even under time pressure — reordering it is the one mistake the
whole project is built to avoid.

## Product strategy recap (full detail: `docs/product-strategy.md`)

This is already decided, not still open — restated here so it stays visible
in the plan instead of living only behind a link.

- **Customer problem**: independent restaurant/bar margins average 3–5% net
  [Sourced], delivery commissions run 15–30% + 2–3% processing [Sourced], and
  manual reconciliation across POS/delivery/cost-sheet exports runs ~12
  hrs/week [Sourced] — so nobody does it daily, and margin slippage surfaces
  at month-end, too late to act on.
- **Vision**: a same-day, trustworthy answer to "did we make money today, and
  why" — no bookkeeper, no manual exports, no month-end surprise.
- **North Star Metric**: time-to-reconciled-close (median minutes from data
  available to a trusted, provenanced margin figure), anchored to Prosus'
  own cited proof point of cutting this from weeks to 30 minutes [Sourced].
- **Supporting KPIs**: accuracy rate, consistency rate, refusal-correctness
  rate, cost per interaction (USD) — all measured by the Day 4 harness, not
  asserted.
- **Hypotheses, ranked by risk** (full tagging in the strategy doc):
  1. [Hypothesis, highest risk] Owners trust a system that refuses/clarifies
     over one that always answers confidently — **this is the one being
     tested**, via the refusal-correctness harness slice.
  2. [Hypothesis] Daily (not weekly/monthly) reconciliation surfaces
     anomalies early enough to act on mid-week.
  3. [Assumption] Owners prefer a question box over a dashboard.
  4. [Simulated-as-Prosus] What ToqanClaw's real usage data would show about
     question frequency/category, if we had access — explicitly labeled as
     simulated, not real.
- **What's explicitly not being validated in this build**, and why: see
  "what I decided not to build" in the strategy doc and the reasoning doc
  outline below.

## Day 0 — Thursday Aug 27 (done)

- [x] Dev environment: Homebrew, Go, Node, Docker, `gh`
- [x] Private GitHub repo created and pushed
- [x] GitHub Spec Kit installed, constitution ratified (v1.0.0)
- [x] 30 Claude Code skills installed (Go, React, clean code, architecture, product strategy) + `promptfoo`/`sqlc`/`golang-migrate`
- [x] `docs/product-strategy.md` written: problem, vision, North Star, KPIs, tagged hypotheses (recapped above)

## Day 1 — Friday Aug 28

- [ ] `inspired-product` skill: run the empowered-teams diagnostic against our own approach (score 0–7) before writing more — catches "feature factory" thinking early rather than at the end
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
- [ ] Claude Sonnet 5 explanation step, direct Anthropic API calls against the MCP tools — no agent framework
- [ ] Instrumentation from the first real API call: tokens, cost, latency, refusal/clarify flags, logged to Postgres
- [ ] Refusal path fully wired and tested

## Day 4 — Monday Aug 31

- [ ] Evaluation harness: ~15–20 accuracy questions, 5×3 consistency phrasings, ~5 refusal-correctness questions — run it, record real numbers, including failures
- [ ] Fix what's fixable from harness results; log what isn't as a known limitation (not silently)
- [ ] React frontend: chat UI, provenance display, running cost panel — functional over polished
- [ ] Stop new feature work by end of day regardless of state — Day 5 is writing and rehearsal, not coding

## Day 5 — Tuesday Sept 1 (interview day)

- [ ] Close the loop on the hypotheses: with real harness numbers in hand, state plainly whether Hypothesis 1 (refusal trust) held up, partially held, or failed — this is the actual validation step, not a formality
- [ ] One-page reasoning doc (`one-pager-prd` skill), built from real harness numbers: job chosen and why, deterministic/probabilistic boundary, hard/soft limits, evaluation numbers including failures, cost per interaction, what was deliberately not built and why
- [ ] The "where the model got it wrong during the build and how I caught it" passage — from the real running log kept during Days 1–4, not reconstructed from memory
- [ ] Demo recorded or rehearsed live, including at least one on-screen refusal
- [ ] Final read-through against the constitution and the hard-truth rules on background claims before walking in

## Presentation notes (save for Day 5)

- Present the product-strategy narrative (5 problems → Objective/KRs →
  5 products → Product A decision, all in `docs/product-strategy.md`) framed
  as a **Double Diamond** (Discover → Define → Develop → Deliver): Discover
  = the 5 candidate problems researched from real iFood/JET/industry data;
  Define = the OKR Objective and 4 Key Results; Develop = the 5 candidate
  products scored against the objective; Deliver = the Product A decision
  and what got built. Not built yet — a presentation-design task for later.

## Running log of real mistakes (fill in as we go — do not backfill from memory later)

- **Phase 1 (Setup + Foundational)**: `.gitignore`'s `.env.*` rule was silently blocking `.env.example` (a secret-free template) from ever being committed — fixed with a narrow `!.env.example` exception rather than loosening the actual secret rule.
- **Phase 1**: `go mod tidy` dropped `anthropic-sdk-go`/`mcp-go`/`pgx` as unused indirect deps immediately after `go get` added them, since no code imported them yet — had to re-add once real code existed. Note for later: don't run `go mod tidy` before every dependency is actually wired into code.
- **Phase 1**: the `shadcn` CLI only reads the root `tsconfig.json` for the `@/*` path alias (ours lived in `tsconfig.app.json`) and silently wrote a literal `./@/components/ui/button.tsx` directory instead of resolving the alias — fixed by duplicating the alias into the root tsconfig.
- **Phase 1**: fixture promotion attribution ended up computed as a tag-join over delivery orders rather than pre-baked numbers, reading spec.md's Assumptions more literally than data-model.md's table alone suggests — flagged for whoever implements `internal/reconcile` next.
- **Phase 1**: migrations and sqlc queries were only statically validated at build time (no live Postgres yet) — running them for real afterward against colima-backed Postgres confirmed the schema exactly matches `data-model.md`. No discrepancy found, but verified independently, not just trusted from the agent's own report.
- **Phase 3 (User Story 1, T011-T017)**: the live-Postgres integration test's
  cleanup initially used `defer conn.Close(ctx)` alongside a separate
  `t.Cleanup(func() { conn.Exec(...DELETE...) })` to remove the test row.
  Go runs a function's own `defer`s when the test function body returns,
  but `t.Cleanup` callbacks run afterward — so the connection was already
  closed by the time the delete cleanup fired, the `DELETE` failed silently
  (its error was discarded), and a row was left behind in the live
  `daily_reconciliation` table. Caught by manually querying the table after
  the test run and finding a row that should have been cleaned up. Fixed by
  registering the connection close itself via `t.Cleanup` (registered
  first, so LIFO ordering runs it last, after the delete). Lesson: in a Go
  test, `defer` and `t.Cleanup` are two different queues that interleave in
  a specific order — don't rely on `defer` to outlive `t.Cleanup`-registered
  work in the same test.
- Phase 3: the ingest column-matching normalizer (`internal/ingest/columns.go`)
  initially handled spaces, hyphens, and `#` in real-world header names but
  not `%` — a synthetic "Commission %" column (written specifically to test
  the real-file-compatibility requirement from research.md) failed to match
  any alias for `commission_rate_pct`. Caught immediately by the test written
  for that requirement (`TestParseDeliveryExport_ToleratesRealisticColumnNameVariance`)
  failing on the first implementation pass — exactly what writing that test
  first was for. Fixed by mapping `%` to `_pct` in the header normalizer.
- Phase 3: every commission/margin figure in this phase was independently
  hand-verified twice before being hardcoded into Go tests as golden
  values — once via Python's `Decimal` module with explicit
  `ROUND_HALF_UP` (to avoid Python's own default banker's-rounding
  artifact on exact `.5`-cent cases like 34.50 × 23% = 7.935), and again
  end-to-end against the real fixture files via the actual `go run
  ./cmd/server -ingest` pipeline output. Both independent computations and
  the Go implementation's persisted output agreed exactly on all 14 days
  and the period total (482.05). Recorded here because Principle V's
  "prove it with tests" only means something if the expected values in
  those tests were themselves verified independently of the code being
  tested — a test whose golden values were back-computed from the
  implementation proves nothing.
- **Phase 3, caught in independent verification, not by the agent itself**:
  the agent's own report claimed all 14 days persisted correctly, but a
  direct `psql` query found only 13 rows in `daily_reconciliation` — 2026-08-08
  (margin 152.50) was missing, and the sum of the remaining 13
  ($329.55) was short of the claimed total (482.05) by exactly 152.50.
  Root cause: the live-Postgres integration test used `2026-08-08` as its
  own synthetic test fixture date — the same primary key as the real
  pipeline's legitimately-computed row for that day, in the same shared
  database — and its cleanup (`DELETE WHERE date = '2026-08-08'`) silently
  destroyed the real pipeline output along with its own test row. The
  agent's cleanup-ordering fix (above) was real and correct, but didn't
  cover this second, independent issue, because the agent only checked that
  *its own* test row was removed, not whether the delete had collateral
  damage. Re-running `go run ./cmd/server -ingest fixtures` restored the
  correct 14-row state (verified again by direct query). Lesson: an
  integration test that shares a live database with real pipeline runs
  must use a sentinel key clearly outside the real data's range (e.g. a
  date far outside the fixture period), never a real, in-range value —
  even a well-ordered cleanup can delete more than it created if the key
  collides with something real. This is exactly the kind of gap the
  test-plan.md's "honesty check on the agents' own reports" section warned
  about — verify independently, don't just trust a clean self-report.
