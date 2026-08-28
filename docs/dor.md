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

## DOR: Roadmap expansion (specs 002–005)

Four roadmap items named in `docs/product-strategy.md` moved from "named, not built" to real specs, each checked against readiness before implementation:

- **002 — Badge expansion (Growth/Engagement/Campaign-Creation)**: ✅ Ready. Spec, plan, and data model additions written (`specs/002-badge-expansion/`); no external dependency; Campaign-Creation deliberately reframed (see spec's Background) to what's actually buildable without a real Prosus/ToqanClaw API, confirmed with the person requesting this build before writing the spec — not assumed unilaterally.
- **003 — Cross-platform economics comparator**: ✅ Ready. Uses only already-ingested data (`specs/003-platform-comparator/plan.md` confirms both platforms' real, distinct commission rates already exist in the fixtures); no new fixture engineering needed, unlike the original 5-products comparison's assessment of this idea.
- **004 — Paraphrase-aware answer cache**: ✅ Ready, with one real constraint resolved during planning rather than left open: Anthropic has no first-party embeddings API, so the plan uses a bounded Haiku-classification mechanism instead of embeddings, keeping the project on its single-vendor constitution rather than reopening that decision for a cost-optimization feature.
- **005 — Multi-tenant support**: ❌ **Not ready for implementation, by design.** Spec and a dedicated RFC (`docs/rfc-multi-tenant.md`) are written, but per the spec's own Assumptions, tasks are not generated until that RFC is explicitly reviewed and approved — the risk profile (a tenant-isolation bug is a data breach, not a UX defect) is treated as categorically different from the other three, which is itself the DOR judgment being recorded here.
- **007 — Cost sheet upload**: ✅ Ready. Spec and plan written (`specs/007-cost-sheet-upload/`); reuses `internal/ingest.ParseCostSheet` and `internal/pipeline.RunIngestionPipeline` unchanged, so no new validation logic needed reviewing — the only new risk (accidentally writing to the git-tracked `backend/fixtures/`) is closed by construction via a compile-time-anchored live-data path (`internal/livedata`), not a runtime check, per the plan's own Constitution Check.
