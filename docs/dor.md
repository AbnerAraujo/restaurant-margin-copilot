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

## NOT ready — real blockers before implementation starts

- [ ] **`ANTHROPIC_API_KEY` for the Console API account is not yet confirmed set** in the build environment — Phase 4 (US2) cannot produce a real explanation-step call without it
- [ ] **Fixture data does not exist yet** (T008) — nothing in Phase 3 onward can start until it does
- [ ] **`docker-compose.yml` / local Postgres is not yet running** (T005) — blocks Phase 2 entirely
- [ ] **No real restaurant/bar export files have been obtained yet** for the real-file compatibility goal — the quickstart's final validation step has nothing to run against until one is provided

## Verdict

Ready to start Phase 1/2 (Setup, Foundational) immediately. **Not** ready to start Phase 3 (US1) until fixtures (T008) and Postgres (T005) exist, and **not** ready to start Phase 4 (US2) until the Anthropic API key is confirmed. These are the actual next actions, not "review more docs."
