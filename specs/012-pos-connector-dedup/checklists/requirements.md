# Specification Quality Checklist: POS connector, and cross-source order deduplication

**Purpose**: Definition of Ready — validate specification completeness and quality before implementation begins
**Created**: 2026-08-30
**Feature**: [spec.md](../spec.md) · [plan.md](../plan.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs) in spec.md — every type, package and interface decision lives in plan.md
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
- [x] Scope is clearly bounded (FR-017 and FR-018 name what is deliberately excluded; plan.md carries the interface decision that was rejected and why)
- [x] Dependencies and assumptions identified

## Honesty (feature-specific — the requirement this feature can fail worst at)

- [x] POS is disclosed as simulated at the same five-place bar spec 010 set for the delivery connectors (FR-020)
- [x] The simulation's own convenient choices are named as choices, not presented as findings — the 75/25 reference-presence split and the one-aggregator-integrated model are both recorded in spec.md Assumptions and plan.md, with the honest admission that a 100%-reference mock would make the second matching tier decoration
- [x] The mock does **not** manufacture the ambiguity case so a flag has something to fire on; that path is proven by test, and whether it occurs against the simulated dataset is reported as measured (plan.md, "Ambiguity is not manufactured")
- [x] A limitation the simulation cannot exercise is recorded rather than omitted (the cross-midnight pair, spec.md Edge Cases)
- [x] Provenance identifies **both** sides of every dedup decision, so a removal is traceable to the ticket removed and the order it merged into (plan.md Constitution Check, Principle IV)

## Financial-integrity bar (the two-sided failure this feature exists to manage)

- [x] Double-counting is addressed by an FR, not an intention (FR-007, FR-013)
- [x] **Wrongly dropping a real order is treated as an equal failure**, with its own FR (FR-011), its own success criterion (SC-002, measured over the full dataset), and its own dedicated test (plan.md testing strategy)
- [x] The confidence bar for "same order" is stated explicitly and justified, not left implicit in code (FR-009, FR-010, plan.md "The matching rule")
- [x] The behaviour under ambiguity is specified as a refusal, and the asymmetry (disclose rather than merge) is argued rather than assumed (FR-012, spec.md Assumptions)
- [x] Which side of a resolved duplicate survives is a stated requirement with a stated reason, not an implementation accident (FR-013)
- [x] An amount disagreement between two sides of a confirmed match surfaces rather than being absorbed (FR-015)
- [x] Nothing is silently corrected: every outcome of the dedup pass, including the ones where it did nothing, is required to produce a visible flag (FR-014)

## Constitutional alignment

- [x] Deterministic/probabilistic split preserved: FR-008 forbids any model, scoring function, or fuzzy similarity in the matching decision; FR-019 forbids a model call anywhere in the path
- [x] "Refuse rather than estimate" has concrete FRs, not just a principle reference (FR-011, FR-012)
- [x] "Explicit cap on loop iterations" is a stated requirement for the new upstream (FR-021)
- [x] Provenance survives to every number shown and to every flag raised (FR-014, SC-007)
- [x] No new MCP tool — the model cannot trigger a sync or influence a match
- [x] Determinism-for-repeatability is required including flag content and ordering (FR-003, SC-004)

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows, including the two failure directions (US2, US3)
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification
- [x] The existing CSV path is protected by a requirement (FR-017) and a success criterion (SC-006), not by intent
- [x] Existing callers of the changed pipeline and reconcile functions have a stated compatibility path (plan.md: both old names stay as one-line delegates)
- [x] A delivery-only sync provably cannot clear POS revenue — expressed as a structural property (the `POSActive` boolean), not caller discipline (FR-006, US1.4, plan.md)

## Notes

Three items were resolved as Assumptions rather than blocking clarifications:

1. **Which platform the POS is integrated with.** Read as *one aggregator, not both* (iFood in, JET out). This is the common real configuration and it is also the more demanding one to build against, because it puts a control group — orders that must never be matched — inside every fetch.
2. **The matching window.** Read as ±15 minutes, recorded as a modelling choice rather than a derived constant, with the reasoning (injection lag, kitchen acceptance, clock skew) written down so it can be argued with.
3. **What to do under ambiguity.** Read as *disclose, do not merge*. The alternative — merge the best candidate — was rejected in writing because its errors are unrecoverable from the reconciliation output, whereas a missed merge leaves a flag pointing at exactly what to check.

One item is flagged for reviewer judgement rather than resolved: **whether the partner-order-reference field is a fair simulation of the problem or a shortcut around it.** The position taken is that it is fair — real POS/aggregator integrations genuinely carry the partner's order id, because that is the mechanism by which the order reached the POS — but that relying on it alone would be a shortcut, which is why 25% of echoed tickets deliberately omit it and must be matched without it. Stated in spec.md Assumptions and plan.md so a reviewer can disagree with it on the record.

Ready for implementation.
