# RFC: Multi-Tenant Support

**Status**: Proposed — not approved for implementation. This document is the review gate `specs/005-multi-tenant/spec.md` requires before any task is created.

## Context

Every table, query, typed MCP tool, and REST handler in this product today assumes exactly one restaurant's data exists in the database. `docs/prd.md` named multi-tenancy as explicitly out of scope for the original build. `specs/005-multi-tenant/spec.md` defines what multi-tenancy must guarantee; this document proposes how.

The stakes are asymmetric with every other feature built so far. A bug in, say, the Growth badge computation (spec 002) shows a wrong badge — annoying, incorrect, but privately wrong to the one restaurant using the product. A bug in tenant isolation shows Restaurant A a number that belongs to Restaurant B — Restaurant B's real revenue, margin, or promotion strategy, disclosed to a competitor or stranger. That is not a bug class this document treats the same way as the others.

## Goals

- Define a tenant-isolation architecture that fails closed, not open — a bug in a specific feature's code should not be able to leak cross-tenant data through the typed-tool layer, because the isolation is enforced at a layer beneath every feature, not inside each one.
- Keep the existing deterministic/probabilistic split (Constitution Principle I) and typed-tool-only rule (Principle III) intact — tenant scoping is an additional constraint on those tools, not a reason to weaken them.
- Migrate the existing single-tenant data with zero loss and zero behavior change, verified by the existing test suite and evaluation harness.

## Non-goals

- Billing, plans, or per-tenant pricing.
- Cross-tenant benchmarking features ("restaurants like yours average X margin") — a legitimate future idea, explicitly not this RFC's problem, and one that would need its own careful design given it deliberately aggregates across the isolation boundary this RFC exists to build.
- Multi-location-per-owner support (one account, several restaurants).
- A specific authentication provider/mechanism — treated as a swappable implementation detail behind a stable interface, not a design decision this RFC needs to settle.

## Proposed design

### Isolation architecture: row-level `tenant_id`, enforced at the query-construction layer — not application-logic-only

Three architectures were considered:

| Approach | Isolation strength | Operational cost | Fit for this project |
|---|---|---|---|
| **Separate database per tenant** | Strongest (no query can ever cross a connection boundary) | High — connection routing, per-tenant migrations, backup/restore multiplied by tenant count | Overkill for a prototype proving the model works; revisit if/when tenant count and compliance requirements justify it |
| **Separate schema per tenant, same database** | Strong | Medium — `search_path` management, migrations still run per-schema | Better than row-level in isolation strength, worse in query/tooling simplicity; a reasonable production evolution, not a reasonable first step |
| **Row-level `tenant_id` column, enforced centrally** | Depends entirely on discipline — this is the architecture where "someone forgot a WHERE clause" is the entire threat model | Low — one schema, one connection pool, straightforward migrations | **Recommended**, with the enforcement mechanism below doing the real work |

Row-level is recommended **only** paired with a structural enforcement mechanism, not "remember to filter by tenant_id in every query":

1. **Every `internal/storage` query-generating function is regenerated (via `sqlc`) to require a `tenant_id` parameter as part of its function signature, not as an optional filter.** `sqlc` already generates typed Go functions from SQL with named parameters — adding `tenant_id` to every query's `WHERE` clause makes omitting it a compile error (a missing required argument), not a runtime data leak. This converts "a developer forgot the tenant filter" from a data-breach class of bug into a code class that does not compile — the same philosophy Principle III already applies to raw SQL access generally (typed tools only, no open SQL) extended one level deeper.
2. **The typed MCP tool layer (`internal/mcptools`) receives the caller's tenant identity from the authenticated request context, never from a client-supplied parameter.** A tool signature that took `tenant_id` as caller input would let a malicious or buggy client simply pass a different tenant's ID; instead, tenant identity flows in from `internal/httpapi`'s authentication middleware, through the request context, the same way Go's standard `context.Context` already threads request-scoped values through this codebase's existing call chains.
3. **The answer cache (`internal/answercache`) gets `tenant_id` added to its cache key and its invalidation query** — today's global `DELETE FROM answer_cache` on ingestion becomes `DELETE FROM answer_cache WHERE tenant_id = $1`, closing the specific cross-tenant timing side-channel named in spec 005's Acceptance Scenario 3 (one tenant's ingestion should never affect, or reveal information about, another tenant's cache state).
4. **An adversarial integration test suite is added specifically for this feature** (`backend/internal/storage/tenant_isolation_test.go` or similar): for every existing query function, assert that calling it with Tenant A's context and Tenant B's data present in the same database never returns a Tenant B row. This is a different kind of test than this project's existing table-driven tests — its entire purpose is trying to break isolation, not verify a feature works, and it should be treated as a release gate, not a nice-to-have.

### Data model changes

- New `tenant` table: `id`, `name`, `created_at`. Minimal — this RFC's non-goals exclude billing/plan fields.
- New `account` table: `id`, `tenant_id` (FK), authentication fields (shape depends on the chosen auth mechanism, deliberately deferred).
- Every existing table (`daily_reconciliation`, `promotion_roi_record`, `question_interaction`, `answer_cache`, `answer_cache_hit`, and the new tables from specs 002–004 once built) gains a `tenant_id` column, `NOT NULL`, foreign-keyed to `tenant`.
- Every existing unique constraint that assumed a single tenant (e.g. `daily_reconciliation`'s date as an implicit primary key) becomes a composite `(tenant_id, date)` constraint — two tenants can both have a reconciliation row for the same calendar date, correctly.

### Request flow change

`internal/httpapi` gains an authentication middleware in front of every existing handler, resolving the caller's `tenant_id` from their session/token and placing it in the request context before any handler runs. Every existing handler signature changes to read `tenant_id` from that context rather than assuming a single global tenant — this is a mechanical, wide-reaching change (every handler, every query, every tool) but not a conceptually complex one, which is exactly why the adversarial test suite matters more here than the line-by-line diff review does: the risk is a single missed call site, not a misunderstood requirement.

### Migration path for existing data

1. Create the `tenant`/`account` tables.
2. Insert exactly one `tenant` row representing the current prototype's restaurant.
3. Add `tenant_id` columns to every existing table as nullable, backfill every existing row with that one tenant's ID, then alter to `NOT NULL`.
4. Re-run the full existing test suite and the 35-question evaluation harness; every result must be byte-identical to pre-migration, per spec 005's Success Criteria SC-003.

## Alternatives considered

- **Do isolation only in the application/handler layer, not the query layer**: rejected. This is the "remember to filter" architecture the design above specifically avoids — it makes every future feature's author personally responsible for not introducing a breach, forever, which does not survive contact with a fast-moving codebase (or, honestly, with an AI agent implementing a feature under time pressure without this RFC's specific constraint in front of it).
- **Postgres Row-Level Security (RLS) policies as the enforcement mechanism instead of sqlc-generated required parameters**: a legitimate alternative worth a real trade-off study before final implementation — RLS enforces isolation at the database level regardless of application-code correctness, which is stronger in one sense (a bug in Go code literally cannot bypass it) but introduces a new operational surface (policy management, `SET ROLE`/session-variable-based tenant context per connection) this document does not fully design. Recommended as a **defense-in-depth addition** on top of the sqlc-parameter approach, not a replacement for it, if implementation time allows both.

## Risks

| Risk | Impact | Mitigation |
|---|---|---|
| A single missed call site leaks cross-tenant data | Critical — this is the one unacceptable outcome this whole document exists to prevent | The adversarial test suite (not manual review) is the actual gate; sqlc's compile-time-required parameter makes the most common failure mode (a forgotten filter) a build failure, not a runtime one |
| Migration corrupts or loses existing prototype data | High — would break the evaluation harness, the presentation, and every existing test that depends on the current fixture data | Migration is additive and reversible (nullable column → backfill → NOT NULL, not a destructive rewrite); full harness re-run is a hard gate before considering the migration done |
| Scope creep into billing/auth-provider bikeshedding delays the actual isolation work | Medium | Non-goals section above is deliberately narrow; authentication mechanism is explicitly deferred as a swappable detail |
| This RFC itself is implemented under the same time pressure that makes the "remember to filter" anti-pattern tempting | High | This is the reason implementation is explicitly gated on review of this document first, not folded into the same pass as specs 002–004 |

## Rollout

Not scheduled. Per `specs/005-multi-tenant/spec.md`'s Assumptions, implementation tasks are not created from this feature until this RFC is explicitly reviewed and approved — this document is the artifact requesting that review, not a plan already in motion.
