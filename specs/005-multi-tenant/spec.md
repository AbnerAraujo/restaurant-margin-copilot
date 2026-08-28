# Feature Specification: Multi-Tenant Support (Segment 2 Expansion)

**Feature Branch**: `005-multi-tenant`

**Created**: 2026-08-28

**Status**: Draft — RFC-first, implementation deliberately not started

**Input**: User description: "Segment 2 from the market-sizing section (non-Prosus customers, no existing distribution) implies real multi-tenancy: multiple independent restaurants, each with their own data, own login, and no way to see another's numbers. This is a large, security-sensitive undertaking on the scale of everything else combined — write the RFC and spec carefully before any implementation, rather than folding it into the same pass as the other roadmap features."

## Background

Every other feature in this product — including the three just spec'd (002, 003, 004) — assumes exactly one restaurant's data exists in the database. `docs/prd.md` states this explicitly as an original, deliberate exclusion: "Multi-tenant / multi-location support" is named as out of scope, and the constitution's Technology & Scope Constraints never anticipated authentication or tenant isolation at all. Reversing that decision is not "adding a feature" in the same sense as the others — it changes a foundational assumption nearly every existing table, query, and typed tool relies on (implicitly or explicitly) that there is only ever one restaurant's worth of data to read.

This spec exists to define WHAT multi-tenancy must guarantee before any implementation begins, precisely because the failure mode here is the one this entire product's constitution was written to prevent: **a confidently-served, wrong answer** — except here the wrong answer is worse than a bad number, it is *someone else's real financial data*. A tenant-isolation bug is not a UX defect; it is a data breach. This document is deliberately conservative as a result.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A restaurant owner's data is never visible to another restaurant (Priority: P1 — the only requirement that must be perfect, not just good)

Two independent restaurants, Restaurant A and Restaurant B, both use this product. Restaurant A's owner can never see Restaurant B's margin figures, promotions, badges, points, or ask a question that returns Restaurant B's data — under any input, including malformed, unexpected, or adversarial ones.

**Why this priority**: This is not "the most important feature" — it is the one precondition every other feature in this spec depends on being unconditionally true. A multi-tenant product with a P2-quality isolation guarantee is not a smaller multi-tenant product; it is a data-breach engine with the rest of this feature list bolted onto it.

**Independent Test**: For every existing typed MCP tool, every REST endpoint, and the ask/gate/explain path, attempt to retrieve another tenant's data by every input manipulation available to an authenticated user of a different tenant (not just the "happy path" UI) — direct API calls with a different tenant's date range, a crafted question referencing another tenant's specific numbers, a period spanning both tenants' data — and confirm zero leakage in every case, verified by a real, automated, adversarial test suite, not by manual spot-checking.

**Acceptance Scenarios**:

1. **Given** Restaurant A is authenticated, **When** it calls any existing endpoint or tool with any parameters, **Then** every row returned belongs to Restaurant A and none belong to any other tenant, verified at the database query level, not only at the API-response level.
2. **Given** Restaurant A is authenticated, **When** it asks a natural-language question that could plausibly resolve against another tenant's data (e.g. referencing a date range or campaign name that happens to exist for a different tenant), **Then** the answer is grounded only in Restaurant A's own data, or refuses, and never in another tenant's.
3. **Given** an ingestion pipeline run for Restaurant B, **When** it completes, **Then** it invalidates only Restaurant B's cached answers, never Restaurant A's — the existing answer-cache's cache-clear-on-ingestion behavior must become tenant-scoped, not global, or it becomes a cross-tenant information-timing side channel.
4. **Given** a direct, malformed, or adversarial API request attempting to specify another tenant's identifier explicitly, **When** it is received, **Then** it is refused with an authorization error, never served partially or silently corrected to "the right" tenant.

---

### User Story 2 - A new restaurant can start using the product without another restaurant's help or data (Priority: P2)

A new restaurant owner can create an account, ingest their own data, and begin using every existing feature (reconciliation, ask, promotions, badges, points) scoped entirely to their own restaurant, with no manual per-tenant setup step performed by anyone else.

**Why this priority**: Necessary for Segment 2 to be a real, self-serve market (per `docs/product-strategy.md`'s own framing: "cold, no existing distribution" — there is no Prosus relationship doing onboarding for these customers), but meaningless without User Story 1 already holding.

**Independent Test**: Provision a second tenant end-to-end (account creation through first reconciled close) using only the product's own surfaces, with zero manual database intervention.

**Acceptance Scenarios**:

1. **Given** no account exists yet, **When** a new restaurant owner signs up, **Then** a new, empty, fully-isolated tenant is created with no data inherited or visible from any existing tenant.
2. **Given** a newly-created tenant, **When** its owner ingests their own delivery/POS/cost-sheet files, **Then** every existing feature (reconciliation, ask, promotions, badges, points, and the three roadmap categories from spec 002) operates correctly scoped to that tenant alone, with no code path that assumes a single global dataset remaining anywhere in the system.

---

### User Story 3 - An existing single-tenant deployment is not silently broken by this change (Priority: P3)

The current prototype's data (the 14-day fixture period already relied upon by the evaluation harness, the presentation, and every existing test) continues to exist, coherently, as exactly one tenant after this change — not lost, not duplicated, not silently reassigned.

**Why this priority**: A correctness/migration requirement, sequenced last because it is only meaningful once User Stories 1 and 2 define what "a tenant" actually is.

**Independent Test**: Run the migration against the existing database, then run the full existing evaluation harness (35 questions) and confirm every answer is identical to its pre-migration value.

**Acceptance Scenarios**:

1. **Given** the existing single-tenant database, **When** the multi-tenant migration runs, **Then** all existing data is assigned to exactly one real tenant record, and every existing automated test and the evaluation harness continue to pass unchanged.

### Edge Cases

- What happens if the tenant-scoping mechanism itself fails open (e.g., a bug causes a query to omit its tenant filter)? This must be treated as a Critical/Sev-1 class of defect distinct from every other bug category in this project's mistakes log — see the Constitution Amendment proposal in the accompanying RFC (`docs/rfc-multi-tenant.md`) for the specific engineering discipline proposed to make this class of bug structurally hard to write, not just something tests happen to catch.
- What happens to the existing evaluation harness, presentation, and architecture-diagram artifacts, all of which currently describe a single-tenant system? They must be updated to describe the multi-tenant reality once built, not left describing a system that no longer exists — tracked as its own task, not an afterthought.
- What happens to a tenant that deletes their account? Out of scope for this spec's first pass — see Assumptions.
- What happens if two tenants' owners are the same physical person (e.g. someone who owns two restaurants)? Out of scope for this spec's first pass (one account maps to exactly one tenant) — see Assumptions.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST associate every persisted record (reconciliation, promotion, badge-relevant data, usage events, cache entries) with exactly one tenant, with no record ever readable without an explicit, verified tenant match.
- **FR-002**: System MUST require authentication before any tenant-scoped data is accessible, replacing today's assumption of a single implicit owner.
- **FR-003**: System MUST enforce tenant scoping at the data-access layer (the typed MCP tools and REST handlers), not only in the UI — a client-side-only restriction is not isolation.
- **FR-004**: System MUST scope the existing answer cache and its invalidation-on-ingestion behavior per tenant, so one tenant's data changes never affect another tenant's cached answers or reveal timing information about another tenant's activity.
- **FR-005**: System MUST allow a new tenant to be created and fully onboarded (account, ingestion, first reconciled close) without any action performed by or visible to any other tenant.
- **FR-006**: System MUST migrate all existing single-tenant data into exactly one tenant record with zero data loss, verified against the full existing evaluation harness and test suite passing unchanged.
- **FR-007**: System MUST log which tenant every model interaction (gate, explain) belongs to, extending the existing instrumentation rather than replacing it, so per-tenant cost and usage remain individually auditable — a cross-tenant blended cost figure would hide exactly the kind of per-customer economics this product's own thesis says matters.
- **FR-008**: System MUST be verified against a real, adversarial cross-tenant-access test suite (not manual review alone) before this feature is considered complete, covering every existing typed tool and REST endpoint.

### Key Entities

- **Tenant**: One independent restaurant/business. Owns exactly its own reconciliation data, promotions, badges, points, and cache entries.
- **Account**: The authenticated identity that belongs to exactly one Tenant (per Edge Cases, the one-account-one-tenant simplification for this first pass).
- Every existing entity (`DailyReconciliation`, `PromotionRoiRecord`, `QuestionInteraction`, the new `UsageEvent`/`Promotion`-with-replaces from spec 002, the answer cache tables) gains a required Tenant association.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An adversarial cross-tenant access test suite covering 100% of existing typed tools and REST endpoints finds zero data leakage paths.
- **SC-002**: A new tenant can go from account creation to a first successful reconciled close using only the product's own surfaces, with no manual database or support intervention.
- **SC-003**: 100% of existing automated tests and the full 35-question evaluation harness pass unchanged after migrating the existing single-tenant data into the new model.
- **SC-004**: Per-tenant cost and usage are individually reportable — no report requires disaggregating a blended, cross-tenant number after the fact.

## Assumptions

- **This spec intentionally covers isolation and onboarding only** — pricing/billing per tenant, tenant-to-tenant benchmarking (an interesting but separate feature many multi-tenant B2B products build), and account recovery/deletion flows are explicitly out of scope for this first pass and would be their own follow-on specs.
- One account maps to exactly one tenant for this pass; multi-location ownership (one owner, several restaurants) is a real, foreseeable need but is out of scope here to keep the isolation guarantee's blast radius as small as possible while it is first built and proven.
- Authentication mechanism (email/password, OAuth, magic link, etc.) is a planning-level decision, deliberately not specified here.
- This spec assumes the accompanying RFC's recommended isolation architecture (see `docs/rfc-multi-tenant.md`) is reviewed and approved before any implementation task is created — unlike specs 002–004, this feature does not proceed automatically to `/speckit-plan`'s usual next step without that explicit review, given the stated risk.
