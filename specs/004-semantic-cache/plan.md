# Implementation Plan: Paraphrase-Aware Answer Cache

**Spec**: [spec.md](./spec.md) · **Status**: Ready for tasks/implementation

## Technical Context

The real technical decision this plan makes, that the spec deliberately deferred: **how** to recognize a paraphrase.

## Decision: cheap Haiku classification against a bounded candidate set — not embeddings

**The constraint that drives this**: the constitution restricts this project to the Anthropic API. Anthropic has no first-party embeddings endpoint (their own documentation points to Voyage AI as a partner for embeddings) — so "add semantic search via embeddings" would mean adding a second LLM vendor to a project whose constitution was explicitly amended once already to *consolidate* onto one vendor (the OpenAI→Anthropic switch recorded in the constitution's own Sync Impact Report). Reversing that consolidation for a cost-optimization feature is a worse trade than the feature is worth.

**Chosen mechanism**: on an exact-match cache miss, before running the full ambiguity-gate-plus-explain cycle, make one classification call to Claude Haiku 4.5 (the model already used for the cheap ambiguity gate) with the new question plus a bounded list of existing distinct cached questions (capped at the most recent 20 — see Alternatives below for why a cap is necessary), asking it to return the exact text of a matching cached question, or `NONE`. If it returns a match, verify that exact string still exists in the cache (defensive against the model inventing a plausible-but-wrong answer — never trust an LLM's claim about the cache's own contents without checking), and serve that entry. If `NONE` or an unverifiable answer, proceed to the normal full cycle exactly as today.

This keeps the entire mechanism inside the existing vendor boundary, reuses a model already paid-for and rate-limited in this codebase, and — critically — makes the match-or-miss decision an LLM classification the same way the ambiguity gate already is, rather than a new, less-inspectable subsystem (a vector index, a similarity threshold to tune) this project's evaluation harness has no established way to measure.

## Constitution Check

- **Principle I**: This is the one place in the plan worth flagging directly against Principle I's letter — the paraphrase-match decision IS a model call, not deterministic Go. It is scoped narrowly (classify equivalence of two short strings against a bounded list, never touching real financial data or computing a number) and is closer in kind to the existing ambiguity gate (itself a Principle-I-compliant "classify, don't compute" model use) than to a computation the model has no business doing. Documented here rather than silently justified.
- **Principle II (refuse rather than guess)**: FR-003's "default to treating as new when uncertain" is this principle applied to cache-matching specifically — an uncertain paraphrase match is treated as "refuse to reuse," not "guess it's the same."
- **Principle VI (instrument every model interaction)**: The classification call itself is a real model interaction and MUST be logged with its own real cost (FR-005) — extending `internal/instrumentation`, not bypassing it because it's "just a cache check."

## Data model additions

- `answer_cache` gains no schema change to its match logic (still keyed by normalized exact text) — the paraphrase layer sits *in front of* the existing exact-match lookup, checked only on a miss, never replacing it.
- New `paraphrase_match` table (or a `match_kind` column added to the existing `answer_cache_hit` table — implementation's call between a new table vs. an added column, spec's FR-004 only requires the three states be distinguishable, not a specific schema shape): records `original_question`, `matched_cache_key`, the classification call's own real cost, and timestamp.

## Request flow

1. `POST /api/ask` receives a question.
2. Existing exact-match cache lookup runs first (unchanged, zero cost, per FR-006 "must not weaken the existing exact-match cache").
3. On a miss, if the cache is non-empty, one Haiku classification call checks the new question against up to 20 candidate distinct cached questions.
4. A verified match → serve that cache entry, log a `paraphrase_hit` instrumentation record carrying both the real classification cost incurred AND the full gate+explain cost avoided, kept as two distinct numbers (FR-005 — never netted into one).
5. No match (or an empty cache) → proceed to the existing gate → explain flow unchanged.

## Candidate-set cap: why 20, and why a cap at all

An unbounded "check against every cached question ever" does not scale — both in prompt size/cost (defeating the point, since the check itself would eventually cost more than the savings) and in classification accuracy (a model asked to pick the right match out of hundreds of candidates degrades). Twenty is a starting, adjustable constant (documented as such in code, not a magic number), chosen so the classification prompt stays small and cheap relative to a full gate+explain cycle. Candidate selection is most-recently-cached-first, so the questions most likely to still be relevant (the current data period, current common questions) are the ones checked.

## Testing strategy

- Unit tests for the classification-call boundary (mocked Haiku response) covering: verified match → cache served; hallucinated/unverifiable match → treated as miss (never trust the model's claim without checking the cache actually contains it); `NONE` → normal flow.
- A real, live-API test (following this project's existing pattern of a small number of real, cost-bounded live tests, e.g. `TestExplain_LiveSmokeTest`) using the harness's own 5×3 consistency-question set as genuine paraphrase pairs, measuring the real hit rate improvement (SC-001) and confirming zero false positives (SC-002) against the known-correct answers already recorded for that set.
- Cost measurement: run the harness before/after, report the real classification-call cost incurred versus the real full-cycle cost avoided, exactly as this project has reported every other cost claim this build (real numbers, not estimates).

## Alternatives considered

- **True embeddings via Voyage AI** (Anthropic's own recommended partner): rejected for the vendor-consolidation reason above — worth revisiting explicitly if this project's constitution is ever amended to permit a second vendor for a specific, narrow purpose, but not assumed here.
- **Heuristic text similarity (edit distance, token overlap) with no model call at all**: rejected as the primary mechanism — it would catch superficial rewording but not genuine paraphrases with different vocabulary (the exact class of miss the spec's User Story 1 example describes: "yesterday" vs. "the day before today" share almost no tokens). Worth keeping as a **cheap pre-filter** in front of the Haiku call (only ask Haiku if a fast heuristic first suggests at least one candidate is plausible), as a later optimization if the classification-call cost proves higher than expected in practice — not required for the first implementation.
