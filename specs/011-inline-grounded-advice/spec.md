# Feature Specification: Inline Grounded Advice (widening the Business Insight Advisor)

**Feature Branch**: `feature/011-inline-advice`

**Created**: 2026-08-30

**Status**: Draft

**Input**: Product owner description: "the advisor should advise whatever the customer asks and use the data in context for it — not bringing wrong data or hallucination, but using an advisor that gets all the rich data we have and brings suggestions is something of value to the product strategy and vision."

> **Process note** (same situation specs/010-platform-connector-proxy documented): this spec was authored directly against the repo's own spec/plan/checklist conventions rather than through the `speckit-specify` script, whose branch/worktree bootstrap collides with an agent-created worktree. Content and structure follow the template exactly.

## Background

specs/009-business-insight-advisor shipped a deliberately narrow advisor: exactly five insight kinds, each detected deterministically in Go from a real computed tool result, surfaced as an opt-in teaser chip, re-verified server-side before the one bounded advice call runs. That narrowness bought trust: the model never picks the topic, never sees data the deterministic layer didn't compute, and never advises unprompted.

It also left a real gap, exposed by the mixed data-plus-advice fix that landed just before this spec (CHANGELOG 2026-08-30): when the owner **explicitly asks** for a suggestion — "how can I improve my margin overall?", "should I push delivery or dine-in?" — the product answers the data core and then plainly declines the advice part, even though (a) an advisor skill with exactly the right grounding discipline already exists, and (b) the tool results needed to ground a real suggestion were just computed for that very answer. The owner asked; the product has the data; the advisor says nothing.

This feature is an **evolution of spec 009, not a rewrite**. It adds a second avenue INTO the same advisor:

- **Path 1 (unchanged, spec 009)**: the owner didn't ask, but Go detected one of the five documented patterns in the computed data → zero-cost teaser chip → opt-in tap → advice call. Every mechanism, threshold, prompt, and test from 009 stays byte-identical.
- **Path 2 (new, this spec)**: the owner explicitly asked for a suggestion/recommendation/how-to, and the question has a data-groundable core → the normal tool-calling narration runs first, answering the data part with full provenance → then ONE advisor call runs inline, in the same turn, grounded exclusively in the tool results that interaction actually computed. No tap needed — the explicit ask IS the opt-in.

The line that does not move: **advice must always trace to real, tool-computed data**. A question asking for advice that no typed tool can ground ("what should I pay my staff?", "how do I motivate my team?") is still refused plainly — the refusal discipline is the product; this spec widens what the advisor can reach, never what it may invent.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Ask for a suggestion, get one grounded in your own numbers (Priority: P1)

The owner asks an open-ended, advice-shaped question the five fixed kinds never covered — "how can I improve my margin overall?", "should I focus more on delivery or dine-in?". The product answers the data part exactly as today (real tools, real provenance, real cost), and then, in the same reply, presents a clearly-labeled AI suggestion generated from those same tool results — general industry practice connected to the figures on screen, never a fact the tools didn't compute.

**Why this priority**: This is the feature — the owner's explicit request finally reaches the advisor that was built for exactly this discipline.

**Independent Test**: Ask "how can I improve my margin overall?" against a live instance. Confirm the response carries (a) a narrated data answer with tool calls and provenance, (b) an `advice` block with text, the standing disclaimer, and its own real cost entry in `interactions`, and (c) a new `business_insight_interaction` ledger row with kind `question_advice`.

**Acceptance Scenarios**:

1. **Given** an advice-shaped question with a data-groundable core, **When** it is asked, **Then** the gate classifies it answerable AND flags it as an advice request; the narration step runs its normal tool-calling loop; and one advisor call runs after it, grounded in exactly the tool results that loop produced.
2. **Given** the advisor call succeeds, **Then** the response's `advice` field carries the suggestion text and the same disclaimer wording spec 009 established, structurally separate from `answer_text`, and `interactions` gains one entry for the advisor call's real measured cost.
3. **Given** the advisor call fails (transport error, refusal, empty reply), **Then** the computed data answer is served unchanged and un-failed, the advice field is simply absent, and the failure is logged — a broken suggestion must never cost the owner the answer they already paid for.
4. **Given** the narration produced zero successful tool results, **Then** no advisor call is made at all — no grounding means no advice, exactly as spec 009's `ErrNoToolResults` already rules.

---

### User Story 2 - Ungroundable advice requests are still refused plainly (Priority: P1)

The owner asks for advice nothing in this product's tool set can ground — staffing pay, team motivation, opening a second location. The product declines plainly, exactly as it does today, and never emits a generic ungrounded suggestion just to avoid saying no.

**Why this priority**: Equal to User Story 1. "Refuse rather than guess" is the product's constitution; widening the advisor without preserving this boundary would convert the product's core differentiator into a liability.

**Independent Test**: Ask "what should I pay my staff?" against a live instance. Confirm status `refused` with a specific reason, zero tool calls, zero advisor calls, zero `business_insight_interaction` rows.

**Acceptance Scenarios**:

1. **Given** an advice question with NO data-groundable core, **When** it is asked, **Then** the gate classifies it unanswerable and the existing refusal path runs unchanged — no narration, no advisor call.
2. **Given** a mixed question whose advice part names a lever the tools have no data for but whose data core is real, **When** it is asked, **Then** the data core is answered in full and the advisor's suggestion stays within what the gathered tool results can ground (the advisor's own prompt forbids the rest).

---

### User Story 3 - The proactive teaser path is untouched (Priority: P2)

An owner who did NOT ask for advice keeps getting exactly the spec 009 experience: a question whose computed data matches one of the five documented patterns gets a zero-cost teaser chip; tapping it runs the existing `POST /api/business-insight` flow; clean data gets nothing.

**Why this priority**: This spec is additive. Any behavioral drift in the 009 path is a regression, not a side effect.

**Independent Test**: Ask a promotions question whose result contains a `flagged_negative` promotion, without advice-shaped phrasing. Confirm the teaser appears with kind `negative_promo_roi` and no inline `advice` field; tap it and confirm the 009 endpoint behaves exactly as before.

**Acceptance Scenarios**:

1. **Given** a plain data question whose result matches one of the five patterns, **Then** the teaser appears exactly as before and no inline advice is generated.
2. **Given** `POST /api/business-insight` is called with kind `question_advice`, **Then** it is rejected as an unknown kind — the inline path's ledger kind is not a tappable teaser kind, and the five-kind closed set on that endpoint stays closed.

## Functional Requirements

- **FR-001**: The ambiguity gate MUST emit, alongside its existing three-way classification, an explicit boolean signal that the question is itself asking for a suggestion/recommendation/how-to ("advice requested"). The signal MUST NOT alter the three-way classification's semantics, and the gate's second-pass prose writer MUST remain structurally unable to set it.
- **FR-002**: An advice-requesting question with a data-groundable core MUST be classified answerable and MUST flow through the existing narration tool-calling loop first — the advisor is invoked only after, and only if, a narrated answer with at least one successful tool result exists.
- **FR-003**: The inline advisor call MUST be grounded exclusively in the tool results the SAME interaction actually computed — never in data fetched specially for the advice, never in client-posted data, never in nothing.
- **FR-004**: The advisor's system prompt for this path MUST be assembled per-question in plain Go: a fixed safety base carrying spec 009's non-fabrication rules (no restaurant-specific fact not literally present in the shown JSON; no invented statistics, percentages, dollar amounts, study names, or source names), plus researched-practice guidance sections selected deterministically from the NAMES of the tools that actually ran. The model never selects, requests, or authors guidance content.
- **FR-005**: Every guidance section MUST embed only researched, checkable industry practice (sourced in plan.md, tagged Sourced/Judgment per docs/product-strategy.md's discipline) — a claim that cannot be verified is stated as general practice without a citation or omitted, exactly as spec 009 already handles the unverifiable "~52% take-home" figure.
- **FR-006**: An advice question with NO data-groundable core MUST still be refused plainly by the existing gate unanswerable path — no narration, no advisor call, no generic fallback advice.
- **FR-007**: The inline advice MUST be structurally separate from the computed answer on the wire (its own response field) and MUST carry, verbatim, the standing disclaimer spec 009 established ("AI suggestion — general industry practice connected to your computed numbers, not a computed fact about your business...").
- **FR-008**: Every inline advisor call MUST write one `business_insight_interaction` ledger row (kind `question_advice`, grounding tool-call JSON, advice text, real token/cost/latency) and MUST surface its cost as its own entry in the response's `interactions` — an advice call must never look free, on either path.
- **FR-009**: An advisor-call failure of any sort MUST degrade to serving the computed answer unchanged (advice absent, failure logged) — never to failing the request, and never to substitute ungrounded text.
- **FR-010**: The spec 009 path MUST be behaviorally unchanged: the five-kind teaser derivation, `POST /api/business-insight`'s closed five-kind validation and re-derivation gate, the five kinds' guidance prompts, and the teaser UI. `question_advice` MUST NOT be accepted by that endpoint.
- **FR-011**: When an inline advisor call will follow, the narration step MUST present the data without adding its own recommendations and without stating that advice cannot be given (the advisor owns the advice part of the reply). When no advisor is configured, the pre-existing plain-decline behavior for mixed questions stands.
- **FR-012**: The model MUST NOT compute any number anywhere in this feature — the advisor synthesizes prose from already-computed JSON; every figure it may restate must appear literally in that JSON (Constitution Principle I, unchanged).

## Success Criteria

- **SC-001**: An open-ended, data-groundable advice question not covered by any of the five 009 kinds returns a grounded suggestion in the same turn, with its real cost disclosed, verified live.
- **SC-002**: A genuinely ungroundable advice question returns status `refused` with zero tool calls and zero advisor calls, verified live.
- **SC-003**: A five-kind teaser scenario behaves identically before and after this feature (same teaser, same tap flow), verified live.
- **SC-004**: Unit tests prove the dynamic prompt builder includes exactly the guidance sections for tools present in the grounding set, never a 009 kind-template for a topic outside the five, and never any content when the grounding set is empty (the call is refused instead).
- **SC-005**: `business_insight_interaction` carries one `question_advice` row per inline call; the response's `interactions` and the ledger agree on tokens and cost.

## Assumptions

1. **"Whatever the customer asks" is bounded by groundability, not topic count.** The owner's stated widening is read as "any question the tools can ground", NOT "any question". The explicit out-of-scope list (staffing pay, hiring, motivation, expansion, legal/tax) stays refused. This is the smallest reading consistent with the owner's own "not bringing wrong data or hallucination" clause.
2. **The explicit ask replaces the tap as the opt-in.** Spec 009 required a tap because the owner hadn't asked. Here the owner's own question is the consent and the cost trigger; requiring a second tap would be ceremony. The cost still lands visibly in `interactions`.
3. **Cached answers cache their advice.** An advice-carrying response enters the existing answer cache like any other; an identical question re-served from cache re-shows the same suggestion at zero cost, with the cache's existing disclosure. The cache clears on ingestion, exactly when the grounding could change.
