# Specification Quality Checklist: Platform Connector Proxy

**Purpose**: Definition of Ready — validate specification completeness and quality before implementation begins
**Created**: 2026-08-30
**Feature**: [spec.md](../spec.md) · [plan.md](../plan.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs) in spec.md — all package/type decisions live in plan.md
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain — every gap resolved as a documented Assumption instead
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Success criteria are technology-agnostic
- [x] All acceptance scenarios are defined
- [x] Edge cases are identified
- [x] Scope is clearly bounded (Assumptions name what is deliberately excluded; plan.md carries an explicit Non-goals section)
- [x] Dependencies and assumptions identified

## Honesty (feature-specific — the requirement this feature can fail worst at)

- [x] Every surface that can display connector-sourced numbers is required by an FR to disclose the simulation **before** the numbers (FR-013)
- [x] Disclosure is redundant, not single-point: label, banner, per-platform row, provenance string — any one removed still leaves it stated (FR-009, FR-013, plan.md "Frontend changes")
- [x] The API response itself carries the simulation marker, so a client that ignores the UI still cannot render this data undisclosed (plan.md, `simulated: true`)
- [x] Provenance identifiers are required to be self-evidently synthetic, never a plausible-looking export filename (FR-009, SC-004)
- [x] No FR permits copy that would read as a real integration (FR-014)

## Constitutional alignment

- [x] Deterministic/probabilistic split preserved: FR-015 forbids any model call in this feature's path
- [x] "Refuse rather than estimate" has concrete FRs, not just a principle reference (FR-010, FR-011, FR-012)
- [x] "Explicit cap on loop iterations" is a stated requirement, not left to implementation taste (FR-012)
- [x] Provenance survives to every number shown (FR-009)
- [x] No new MCP tool — the model cannot trigger a data-mutating sync (plan.md Constitution Check, Principle III)
- [x] Determinism-for-repeatability precedent from `cmd/gendata` is followed, and the deviation (per-key seed vs. single stream) is justified in writing (FR-005, plan.md "Determinism")

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification
- [x] The reconciliation engine is provably untouched — stated as a verifiable success criterion (SC-005), not an intention
- [x] Existing callers of the changed pipeline function have a stated compatibility path (plan.md: `RunIngestionPipeline` keeps its signature)
- [x] Concurrency with the pre-existing cost-sheet commit is addressed rather than discovered later (FR-017, plan.md "Serialization across features")

## Notes

Two items were resolved as Assumptions rather than blocking clarifications, per the spec-kit guidance to make the smallest reasonable documented assumption:

1. **"Random value"** was read as *deterministic-per-day pseudorandom*, not fresh-random-per-call. Fresh randomness would make the demo unrepeatable and every test flaky, and would contradict `cmd/gendata`'s established discipline. Recorded in spec.md Assumptions and FR-005.
2. **Where the connected-platforms surface lives** was read as a second tab on the existing `/upload` page, from the requester's own phrasing ("when I do the reconciliation in the upload file it gets the data from the proxy"). Recorded in spec.md Assumptions.

Ready for implementation.
