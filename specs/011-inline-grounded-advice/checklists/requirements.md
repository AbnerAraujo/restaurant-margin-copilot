# Specification Quality Checklist: Inline Grounded Advice

**Purpose**: Definition of Ready — validate specification completeness and quality before implementation begins
**Created**: 2026-08-30
**Feature**: [spec.md](../spec.md) · [plan.md](../plan.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs) in spec.md — package/type decisions live in plan.md
- [x] Focused on user value and business needs (the owner's own stated rationale is quoted as the Input)
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain — every gap resolved as a documented Assumption instead
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable (SC-001..SC-005 each name a concrete observable)
- [x] Success criteria are technology-agnostic
- [x] All acceptance scenarios are defined
- [x] Edge cases are identified (advisor failure, zero tool results, nil configuration, cache interaction)
- [x] Scope is clearly bounded (spec Assumptions §1 + plan.md Non-goals name what stays refused)
- [x] Dependencies and assumptions identified

## Honesty (feature-specific — fabricated advice is this feature's worst possible failure)

- [x] The non-fabrication rules are required VERBATIM from spec 009's advisor, not paraphrased (FR-004) — no restaurant fact outside the shown JSON, no invented statistics or sources
- [x] Every new guidance section requires a real, checkable, dated source or explicit general-practice framing (FR-005); unverifiable vendor marketing figures are named and excluded in plan.md ("Not used")
- [x] Grounding is structurally limited to the SAME interaction's real tool results — never client-posted, never fetched specially, never absent (FR-003)
- [x] Advice remains structurally separate from computed facts on the wire and on screen, with the standing disclaimer verbatim (FR-007)
- [x] The advisor call can never look free: ledger row + its own interactions entry on every call (FR-008)
- [x] Degradation is specified: a failed advice call serves the data answer unchanged, never substitute text (FR-009)

## Constitutional alignment

- [x] Deterministic/probabilistic split preserved: whether advice runs, and what guidance enters its prompt, are plain Go decisions over typed signals and tool names; the model computes and selects nothing (FR-002, FR-004, FR-012)
- [x] "Refuse rather than guess" survives the widening as a first-class requirement with a live test (FR-006, SC-002, User Story 2)
- [x] No new tool, no new loop — grounding reuses the existing budgeted, timeout-guarded MCP loop (plan.md Design §2)
- [x] Provenance: the advice's grounding JSON is persisted; the data answer's provenance is untouched (FR-008, plan.md Principle IV)
- [x] Instrumentation extends the existing dedicated ledger via a reviewed migration, exactly as that ledger's own comment prescribes for a new kind (FR-008, plan.md Design §5)
- [x] Spec 009's closed five-kind teaser endpoint stays closed: `question_advice` is a ledger kind, never a tappable/postable kind (FR-010)

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover both the new path and the preserved refusal and teaser paths
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] The 009 path's invariance is a stated, live-verified success criterion (SC-003), not an intention
- [x] The gate's writer pass provably cannot set the new signal (structural, tested) — the same guarantee the classification field already has
- [x] Backward compatibility: nil `QuestionAdviser`/`InsightStore` deps reproduce today's behavior exactly (plan.md Design §4)

## Notes

Three items were resolved as documented Assumptions rather than blocking clarifications:

1. **"Whatever the customer asks"** was read as bounded by groundability (the owner's own "not bringing wrong data or hallucination" clause), not as unbounded topic scope. Recorded in spec.md Assumptions §1.
2. **Opt-in semantics**: the explicit ask replaces the tap as consent for the billed call; cost stays visible. Recorded in spec.md Assumptions §2.
3. **Cache interaction**: advice-carrying responses cache like any other; ingestion clears them. Recorded in spec.md Assumptions §3.

Ready for implementation.
