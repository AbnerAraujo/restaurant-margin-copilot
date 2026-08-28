# Definition of Ready — Daily Margin & Growth Copilot

Checked honestly against what's actually true right now, not what's aspirational.

## Product & strategy

- [x] Problem statement, vision, OKR Objective, and 4 Key Results defined (`docs/product-strategy.md`)
- [x] Alternatives considered and explicitly rejected, with reasons (5 problems, 5 products)
- [x] Target user and market sizing defined, segmented (Prosus vs. non-Prosus customers)
- [x] Success metrics defined, both build-time (KR1–KR4) and post-launch (attach rate, retention, profitability lift)
- [x] What's explicitly out of scope is named, not silently assumed (badges roadmap, LLMOps harness, Segment 2)

## Specification

- [x] Feature spec written and validated against its own quality checklist, zero `[NEEDS CLARIFICATION]` markers (`spec.md`)
- [x] 4 prioritized, independently-testable user stories with acceptance scenarios and edge cases
- [x] 13 functional requirements, 3 key entities, 6 success criteria

## Technical plan

- [x] Stack, model choice, and library decisions made with documented rationale, not defaulted (`research.md`)
- [x] Constitution Check passed with zero violations (`plan.md`)
- [x] Data model defined with provenance fields on every entity (`data-model.md`)
- [x] MCP tool contracts defined as a fixed, typed set — no open computation (`contracts/mcp-tools.md`)
- [x] Module architecture (ports & adapters) diagrammed, showing the domain core has zero outgoing dependencies (`docs/architecture.html`)
- [x] `/speckit-analyze` run against spec+plan+tasks; 2 findings caught and fixed (instrumentation coverage gap, missing badge transport) — 100% FR/SC task coverage confirmed

## Tasks

- [x] 39 tasks generated, organized by user story, dependency-ordered, with parallel opportunities marked (`tasks.md`)
- [x] Tests specified first in every phase, per Constitution Principle V

## Resolved since first written

- [x] **`ANTHROPIC_API_KEY`** — set and verified working with a live models-list call (cost: $0).
- [x] **Local Postgres** — running via `colima` (Docker Desktop was never actually installed; colima is a lighter drop-in daemon), migration applied, schema verified to match `data-model.md` exactly, including the DB-level CHECK constraints.
- [x] **Fixture data** (T008) — generated: two weeks (2026-08-01–14), four source files, every deliberate irregularity documented with exact row IDs and independently-verified reference sums in `backend/fixtures/README.md`.
- [x] **`Workflow` (multi-agent orchestration)** — enabled via `/config` → Dynamic workflows.

## Still NOT ready

- [ ] **No real restaurant/bar export files have been obtained yet** for the real-file compatibility goal — the quickstart's final validation step has nothing to run against until one is provided.
- [ ] **Live API spend is capped at $5 for this build session** (against $20 Console credit) — the full evaluation harness (T034) must be run as a single monitored step with cost tracked against this ceiling, not run repeatedly during debugging.

## Verdict

All four original blockers are resolved. Ready to proceed through US2/US3/US4/Integration/Polish now, with one live operational constraint carried forward: bound real Anthropic API calls tightly (smoke-test first, full harness as one monitored run) to stay under the $5 ceiling.
