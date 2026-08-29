# Feature Specification: Business Insight Advisor

**Feature Branch**: `009-business-insight-advisor`

**Created**: 2026-08-29

**Status**: Draft

**Input**: User description: "Add a new skill to the model as a business advisor that suggests what the restaurant can do based on the data, find information on the internet that can help with this, so if the user asks about the data, add suggestions about insights and what to do in this situation. Don't put it in the answer — put it in a bubble/balloon at the bottom of the message with the title of the suggestion, and only show and retrieve the full content from the LLM if the customer wants or asks related to it."

## Background

Every answer this product gives today is a computed fact with provenance: which tool ran, which rows grounded it, what it cost. What the product deliberately does NOT do is tell the owner what to *do* about a bad number — a negative-ROI promotion is flagged, but "what owners in this situation usually do next" is left entirely to the owner. That restraint is correct for the answer itself (Constitution Principle I: the model narrates computed facts, it never invents), but it leaves real, general, well-documented industry practice — commission-tier negotiation, chargeback dispute windows, ordering-cadence smoothing — permanently out of reach even when the data on screen is exactly the situation that practice addresses.

This feature adds a **business-advisor skill** with a hard architectural line through the middle of it:

- **Whether** an insight is worth offering, and **what it is about** (its title), is decided **deterministically in Go** from the same raw tool results already in the response — zero model calls, zero added cost, on every answered question. Most answers get nothing.
- **The advice text itself** is a real, billed, instrumented Claude Sonnet 5 call that runs **only when the owner explicitly asks for it** by tapping the teaser — never auto-fetched, never blended into the answer above it.

Advice is a fundamentally different epistemic category from everything else this product shows: it is probabilistic, general, and best-practice-shaped, not a computed fact about this restaurant. The spec therefore requires it to be **visually and structurally distinct** from the provenance-backed answer at every point — its own bubble, its own styling, its own disclosed "AI suggestion, not a computed figure" label, and its own real cost shown when fetched.

The trigger thresholds and the advice prompts are grounded in real published industry material researched for this spec (delivery-platform commission tiers and negotiation practice, payout-reconciliation dispute mechanics, restaurant cost-management practice); every claim used is tagged Sourced or Judgment in plan.md, per `docs/product-strategy.md`'s standing sourcing discipline.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A zero-cost, deterministic insight teaser on relevant answers (Priority: P1)

An owner asks a normal question. When — and only when — the computed result matches a known, actionable pattern (a discrepancy flag, a money-losing promotion, a high effective commission rate, a recurring day-of-month expense spike, a period-over-period margin decline), a small, clearly-labeled suggestion chip appears under the answer with a short title. Nothing else changes: no extra model call ran, the answer is byte-identical to what it would have been, and a question whose data matches no pattern gets no chip at all.

**Why this priority**: The teaser is the whole feature's gate: without a trustworthy, non-spammy, zero-cost trigger, the advisor is either always-on noise or another billed call on every question — both explicitly ruled out.

**Independent Test**: Ask a question whose grounding day carries a real discrepancy flag; confirm `business_insight` appears in the response with kind `discrepancy_pattern` and a human-readable title. Ask a question over clean data; confirm the field is absent. Confirm `interactions` is unchanged in both cases (the teaser cost nothing).

**Acceptance Scenarios**:

1. **Given** an answered question whose `get_daily_summary` grounding day(s) carry at least one real `discrepancy_flags` entry (or whose `list_discrepancies` result names at least one flagged day), **When** the answer renders, **Then** the response carries a `business_insight` teaser with kind `discrepancy_pattern` and a short title — and the answer text, provenance, and interactions are exactly what they would have been without this feature.
2. **Given** an answered promotions question whose result contains at least one `flagged_negative: true` promotion, **When** the answer renders, **Then** the teaser kind is `negative_promo_roi`.
3. **Given** an answered platform-comparison question where at least one platform's effective commission rate is at or above the documented threshold (see FR-004), **When** the answer renders, **Then** the teaser kind is `high_commission`.
4. **Given** an answered day-of-month-pattern question whose highest-expense day-of-month is a real outlier per FR-005's documented rule, **When** the answer renders, **Then** the teaser kind is `day_of_month_expense_spike`.
5. **Given** an answered margin-comparison question whose delta shows a material decline per FR-006's documented rule (or a period-totals question whose period margin is a real loss), **When** the answer renders, **Then** the teaser kind is `margin_decline`.
6. **Given** an answered question whose tool results match none of the five patterns, **When** the answer renders, **Then** no `business_insight` field is present at all — never an empty object, never a generic "here's a tip" filler.
7. **Given** a refusal or clarification, **When** the response renders, **Then** no `business_insight` field is present (the same scoping `suggested_followups` and `tool_calls` already use).

---

### User Story 2 - Full advice on demand, at a real, disclosed cost (Priority: P1)

The owner taps the suggestion chip. Only then does the product make one Claude Sonnet 5 call, grounded in the exact tool results that triggered the teaser, and expand the chip inline into a short piece of general, best-practice advice connected to the specific computed pattern — plus the real cost of that call, shown right there. The advice never claims specific facts about this restaurant beyond what the tool results state, and the call is logged to its own dedicated instrumentation table.

**Why this priority**: This is the half of the feature that spends money and produces probabilistic content — it must be opt-in, instrumented, grounded, and honest about what it is, or it undermines the trust architecture the rest of the product is built on.

**Independent Test**: Tap a teaser; confirm exactly one `POST /api/business-insight` request fires, carrying the kind and the same `tool_calls` data the answer already had; confirm the response carries real advice text plus real token/cost/latency figures; confirm one row landed in `business_insight_interaction` with matching figures; confirm nothing was fetched before the tap.

**Acceptance Scenarios**:

1. **Given** a rendered teaser, **When** the owner does nothing, **Then** no advice request is ever made — the full content is never auto-fetched on render.
2. **Given** the owner taps the teaser, **When** the request runs, **Then** a loading state is shown, and the expanded bubble then shows the advice text, an explicit "AI suggestion, not a computed figure" disclosure, and the call's real measured cost — never presented as free, never presented with the answer's provenance styling.
3. **Given** the advice call completes, **Then** exactly one row exists in `business_insight_interaction` recording the kind, the grounding tool results, the advice text, model, tokens, estimated USD cost, and latency.
4. **Given** the posted `tool_calls` data does not actually support the requested kind (a stale, tampered, or mismatched request), **When** the endpoint re-derives the teaser from the posted data, **Then** it refuses with a typed error rather than generating advice ungrounded in the data it was shown.
5. **Given** the model call fails or the model refuses, **When** the endpoint responds, **Then** the client shows a real error state (with the chip still tappable to retry), never fabricated advice.
6. **Given** the owner taps the already-expanded bubble closed and open again, **Then** the already-fetched advice is re-shown without a second billed call.

---

### User Story 3 - The advice reads as a suggestion, never as a computed fact (Priority: P1)

An owner glancing at the chat can tell, without reading a word of body text, which parts of the screen are provenance-backed computed facts and which part is an AI suggestion: the insight bubble uses a visibly different visual language from the answer card, carries a lightbulb-style icon and an explicit label, and sits after the answer's own content (follow-ups included), never inside it.

**Why this priority**: Constitution Principle I's deterministic/probabilistic split is this product's core demo claim. A probabilistic suggestion styled like a computed fact would be the exact class of lie the whole build exists to avoid.

**Independent Test**: Render an answer with a teaser; confirm the chip appears after the follow-up chips, styled distinctly from the `border-border bg-card` answer surface; confirm the expanded advice carries the disclosure text and the real cost line; confirm the answer text itself contains no advice.

**Acceptance Scenarios**:

1. **Given** an answer with a teaser, **When** it renders, **Then** the advice content appears nowhere in `answer_text` — the model's narration prompt is unchanged by this feature.
2. **Given** the teaser chip and the expanded advice bubble, **When** compared to the answer card, **Then** both use a distinct visual treatment (not the answer's neutral card treatment) and an explicit suggestion label.
3. **Given** the expanded advice, **Then** its real cost is displayed with it, in the same spirit as the existing cost panel — a model call never looks free.

### Edge Cases

- What happens when more than one pattern matches a single answer's tool results (e.g. a platform comparison over a period that also had discrepancy-flagged days pulled as supporting context)? → Exactly one teaser is offered, chosen by a fixed, documented priority order mirroring `deriveFollowUpSuggestions`' existing "narrowest subject wins" convention — never a stack of competing suggestion chips.
- What happens when the owner taps a teaser after a new ingestion changed the data? → Nothing stale is served: the request carries the tool results the client already has (the same data FR-003's "show your work" already exposes), the endpoint re-derives the trigger from exactly that payload, and the advice explicitly addresses the computed pattern it was shown — not the database's current state, which it never reads.
- What happens when a cached answer (exact or paraphrase hit) is served? → The cached response body already contains whatever `business_insight` teaser it was originally served with (the teaser is part of the response and costs nothing to store); tapping it still runs a fresh, real advice call — advice calls themselves are deliberately not cached in this pass (see Assumptions).
- What happens when the tool result parses but a money/percent field inside it doesn't? → That candidate trigger is skipped, exactly as `suggestions.go` skips an unparseable date — never a teaser grounded in a misread number.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST derive, in plain Go with zero model calls, an optional business-insight teaser (`kind` + human-readable `title`) for every answered question, from the same raw tool result JSON already carried in the response — and MUST omit it entirely when no documented pattern matches.
- **FR-002**: The teaser MUST be carried as a new optional `business_insight` field on the `POST /api/ask` response, populated only when `status` is `"answered"`, at the same construction site `suggested_followups`/`tool_calls` already use.
- **FR-003**: A discrepancy flag on any grounding day (`get_daily_summary` result carrying `discrepancy_flags`, or a `list_discrepancies` result with at least one flagged day) MUST produce kind `discrepancy_pattern`.
- **FR-004**: A platform whose effective commission rate (from `compare_platform_economics`) is at or above the documented threshold MUST produce kind `high_commission`. The threshold MUST be a named constant whose doc comment states what is sourced (published marketplace commission tiers run 15–30%, with entry tiers at ~15%) and what is judgment (the exact cut).
- **FR-005**: A day-of-month expense outlier (from `get_expense_pattern_by_day_of_month`) MUST produce kind `day_of_month_expense_spike`, per a documented deterministic rule (highest average at or above a named multiple of the median day-of-month average, with more than one occurrence), labeled as judgment.
- **FR-006**: A material margin decline (from `get_margin_delta`, delta at or below a documented negative materiality threshold relative to the prior period; or a `get_period_totals` result whose period margin is a real loss) MUST produce kind `margin_decline`.
- **FR-007**: A `flagged_negative: true` promotion in a `get_promotion_roi`/`list_negative_roi_promotions` result MUST produce kind `negative_promo_roi`.
- **FR-008**: At most ONE teaser is offered per answer, selected by a fixed, documented priority order; the derivation MUST return nothing for unrecognized tools, unparseable results, and clean data.
- **FR-009**: The system MUST expose `POST /api/business-insight`, taking the teaser `kind` and the same `tool_calls` data the client already received, and returning the full advice text plus the call's real measured cost — and MUST re-derive the teaser from the posted tool results, refusing with a typed error when the posted data does not support the requested kind.
- **FR-010**: The advice call MUST go through the shared `internal/llmclient` client (same timeout and instrumentation discipline as every other model call), under its own model constant (`ModelBusinessInsight`) with a documented model-choice rationale in `internal/llmclient/cost.go`.
- **FR-011**: The advice system prompt MUST instruct the model, per kind, to give general best-practice guidance connected to the specific computed pattern it is shown, to never state a fact about this restaurant that is not in the provided tool results, to never fabricate statistics or named sources, and to stay short and plain.
- **FR-012**: Every advice call MUST be logged to a NEW, dedicated `business_insight_interaction` table — never overloaded into `question_interaction` (this is not the gate or explain running), `answer_cache_hit`, or `paraphrase_match` — recording kind, grounding tool results, advice text, model, tokens, cost, and latency.
- **FR-013**: The frontend MUST render the teaser as a distinctly-styled, clearly-labeled suggestion chip after the answer's follow-up content — never inside or styled like the provenance-backed answer — showing only the title until tapped.
- **FR-014**: The full advice MUST be fetched only on explicit tap (never on render), with a visible loading state, and the expanded advice MUST display its real cost and an explicit "AI suggestion" disclosure. A repeat expand/collapse MUST NOT re-bill.
- **FR-015**: The advice text MUST never be merged into `answer_text`, and the `/api/ask` narration path MUST be entirely unchanged by this feature (same prompts, same calls, same cost).

### Key Entities

- **Business-insight teaser**: not persisted — a deterministic Go derivation (`deriveBusinessInsightTeaser`, alongside `deriveFollowUpSuggestions`) over raw tool result JSON already in the response; wire shape `{kind, title}`.
- **Business-insight advice**: the on-demand model output — returned to the client with its cost, persisted only in the interaction ledger below.
- **`business_insight_interaction`**: a new persisted ledger table, one row per advice call that actually ran — the fourth distinct interaction type, kept distinct for the same reason `question_interaction` / `answer_cache_hit` / `paraphrase_match` are three tables: every state stays distinguishable and no cost is ever netted or hidden.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of answered questions with no matching pattern carry no teaser, and the teaser derivation adds zero model calls and zero cost to `/api/ask` — verified by tests asserting `interactions` is unchanged and the field is absent.
- **SC-002**: Advice is never fetched without an explicit tap — verified by a component test asserting zero fetches on render.
- **SC-003**: Every advice call that runs is visible in three places with the same real numbers: the HTTP response, the `business_insight_interaction` row, and the on-screen cost line.
- **SC-004**: An owner can distinguish the suggestion from the computed answer at a glance — the chip/bubble never reuses the answer card's visual treatment, and always carries an explicit suggestion label.
- **SC-005**: A tampered or stale advice request (kind unsupported by the posted data) is refused with a typed error, 100% of the time — verified by handler tests.

## Assumptions

- Advice responses are deliberately NOT cached in this pass: unlike `/api/ask` answers, advice is only ever requested by explicit tap (low volume), and caching probabilistic advice would add a staleness surface for marginal savings. If tap volume ever makes this worth revisiting, the `answer_cache` discipline (own ledger, disclosed match rule) is the template.
- The five trigger kinds are a fixed, closed set in this pass, enforced by a DB CHECK constraint; adding a sixth is a migration plus a prompt, which is the right friction for a category of content this sensitive.
- The client re-posts `tool_calls` it already holds (FR-003 "show your work" data) rather than the server re-fetching or storing per-answer state — no server-side session state is introduced.
- Research grounding (full source list in plan.md): published delivery-marketplace commission tiers and ranges (OPA! Link 2025 commission-fee guide: DoorDash Basic/Plus/Premier at 15/25/30%, Uber Eats 15–30%, Grubhub 15–25% plus marketing; CloudKitchens 2025: 15–30% typical plus 2–4% processing), payout-reconciliation failure modes and dispute windows (Voosh.ai multi-unit reconciliation guide: platform-issued refunds/chargebacks deducted from payouts, marketing deductions, phantom fees, 14–30 day dispute windows; Restaurant365 automated dispute resolution material), and restaurant cost-management practice (Restaurant365 food-cost techniques and Toast inventory-management guidance on par levels and ordering cadence tied to delivery schedules). Where a specific numeric threshold is NOT prescribed by any source, the constant's doc comment says so explicitly and labels the cut as judgment.
- This feature makes no change to the ambiguity gate, the explanation step, the answer cache, or the paraphrase matcher; the only `/api/ask` change is the additive, deterministic `business_insight` field.
