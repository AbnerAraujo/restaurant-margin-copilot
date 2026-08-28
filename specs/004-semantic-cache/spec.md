# Feature Specification: Paraphrase-Aware Answer Cache

**Feature Branch**: `004-semantic-cache`

**Created**: 2026-08-28

**Status**: Draft

**Input**: User description: "The 'semantic-memory/LLMOps harness' roadmap idea, concretized: extend the existing exact-match answer cache (built earlier this build) to also match questions that mean the same thing as an already-cached question, even when worded differently — closing the documented gap where a paraphrase currently misses the cache and costs full price."

## Background

The existing answer cache (`backend/internal/answercache`) is deliberately exact-match only: it normalizes whitespace and case, but two differently-worded questions with the same meaning ("What was our margin yesterday?" vs. "How did we do the day before today?") are treated as entirely different cache keys, so the second one always pays full model cost even though the first already answered the same underlying question. This was a disclosed, intentional limitation at build time (`docs/plan.md`), not an oversight — a naive fuzzy match risked serving a wrong answer to a subtly different question, and the exact-match design traded some missed cache hits for zero risk of a false hit.

This feature narrows that gap without reintroducing that risk: it should catch genuine paraphrases (same question, different words) while continuing to refuse a "near enough" match between two questions that could resolve differently — the same discipline the rest of this product applies everywhere else (refuse rather than guess).

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A paraphrased repeat question still avoids full cost (Priority: P1)

An owner who already asked "What was our margin on 2026-08-07?" later asks "How did we do on August 7th?" — a different wording of the identical question — and gets the same answer without the system re-running the full ambiguity-gate-plus-explanation cycle a second time.

**Why this priority**: This is the entire point of the feature and the direct answer to the documented gap.

**Independent Test**: Ask a question, then ask a clearly-equivalent paraphrase of it, and confirm the second one is served from cache (real-money-cost of $0, `cache: hit` in the response) rather than triggering a new model round-trip.

**Acceptance Scenarios**:

1. **Given** a question already answered and cached, **When** an owner asks a genuine paraphrase of it (same date, same metric, same intent, different wording), **Then** the cached answer is served with no new model call.
2. **Given** two questions that are superficially similar but meaningfully different (different date, different metric, or a different scope like "this week" vs. "today"), **When** the second is asked, **Then** it is treated as a new question and answered fresh — never served a cached answer to a question it doesn't actually match.
3. **Given** the system cannot confidently determine whether a new question means the same thing as a cached one, **When** that ambiguity exists, **Then** it defaults to treating the question as new (a full, fresh answer) rather than risking an incorrect cache hit — consistent with this product's existing "refuse/re-verify rather than guess" discipline.

---

### User Story 2 - The owner and the operator can both tell a paraphrase hit from an exact hit (Priority: P2)

Since a paraphrase match carries more risk of error than an exact-text match, both the served answer and the underlying interaction record should be able to distinguish "exact repeat" from "recognized paraphrase" — not because the owner needs to see this every time, but because it must be auditable.

**Why this priority**: A support/debugging need, not a core user-facing outcome, but necessary for anyone (including the person running this product) to verify the feature is behaving safely rather than silently.

**Independent Test**: Trigger one exact-match hit and one paraphrase hit, and confirm the two are distinguishable in whatever surface records cache activity.

**Acceptance Scenarios**:

1. **Given** a paraphrase-matched cache hit, **When** the interaction is recorded, **Then** it is distinguishable from an exact-text cache hit in the record, and both remain distinguishable from a real, fresh model call — three distinct states, never collapsed into two.

### Edge Cases

- What happens when a question is a paraphrase of MORE THAN ONE previously-cached question (e.g., two prior questions happen to have overlapping possible meanings)? The system must not guess which one is meant — this is treated the same as User Story 1's Acceptance Scenario 3 (ambiguous match → treat as new).
- What happens when the underlying data changes (a new day is ingested) between when the original question was cached and when a paraphrase arrives? The existing cache-invalidation-on-ingestion behavior already clears the whole cache in this case, so a paraphrase after new data simply finds nothing to match — same as today's exact-match behavior, not a new edge case this feature introduces.
- What happens to cost accounting when a paraphrase hit still requires some real computation (e.g., a classification step to confirm the match) versus zero computation for an exact-text hit? Both the real cost incurred (if any) and the cost avoided must be honestly and separately recorded — a paraphrase-match mechanism that costs a small amount to operate is not free, and this product does not hide small real costs to make a savings number look bigger than it is.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST serve a cached answer for a new question that is a genuine paraphrase of a previously-answered, still-valid cached question, without repeating the full ambiguity-gate-plus-explanation cycle.
- **FR-002**: System MUST NOT serve a cached answer when the new question could plausibly resolve to a different answer than the cached one (different date, different metric, different scope, or any other meaningful difference) — a false cache hit is a worse outcome than a missed one.
- **FR-003**: When the system cannot confidently classify a new question as matching or not matching a cached one, it MUST default to treating it as a new question.
- **FR-004**: The interaction record for any answer MUST distinguish three states: a fresh model-computed answer, an exact-text cache hit, and a paraphrase-matched cache hit.
- **FR-005**: Any real cost incurred while checking for a paraphrase match (if the chosen mechanism has one) MUST be recorded honestly as real spend, separately from the cost the match then avoided — never netted together into a single number that overstates the saving.
- **FR-006**: This feature MUST NOT weaken the existing exact-match cache's behavior or its cache-invalidation-on-ingestion guarantee — it extends the cache's reach, it does not replace or bypass the existing safeguards.

### Key Entities

- **ParaphraseMatch**: A record of a new question being recognized as equivalent to a specific existing cache entry, distinct from the exact-match cache-hit record already in place.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A representative set of genuine paraphrases of already-cached questions are served from cache at a materially higher rate than today's 0% (exact-match-only baseline), measured on the existing evaluation harness's consistency-question set (the same 5-questions-×-3-phrasings set already used to measure consistency).
- **SC-002**: Zero false-positive cache hits occur across the same evaluation set — no paraphrase-matched answer is ever wrong for the actual question asked.
- **SC-003**: The real cost of operating the paraphrase-matching mechanism itself, measured over a representative session, remains a small fraction of the cost it avoids — the feature is a net, honestly-measured saving, not merely a cost shifted and hidden.

## Assumptions

- This feature operates on the existing single-restaurant, single-dataset prototype scope — no cross-tenant cache sharing or isolation concerns apply yet (that would be revisited alongside any future multi-tenant expansion).
- "Genuine paraphrase" is scoped to same-intent, same-parameters questions (date, metric, scope all equivalent) — this feature does not attempt to answer a broader class of "similar but not identical" questions (e.g., inferring an answer to a question that was never actually asked before in any form). That remains the existing ambiguity-gate-plus-explanation path's job.
- The specific technical mechanism for recognizing a paraphrase (and any vendor/technology it requires) is a planning-level decision, made and documented in this feature's plan/RFC — this specification defines only the required behavior and safeguards.
