-- Answer cache for POST /api/ask.
--
-- Two tables, deliberately, rather than one cache table plus a flag on
-- question_interaction:
--
--   answer_cache      the cached response itself, keyed by the NORMALIZED
--                     question text (trim + collapse whitespace + lowercase).
--   answer_cache_hit  one row per cache hit served.
--
-- Constitution Principle VI requires every model interaction to be
-- instrumented. A cache hit is the ABSENCE of a model interaction, so writing
-- a question_interaction row for it would fabricate an API call that never
-- happened and double-count its cost in SumEstimatedCostUSD. Leaving it
-- unrecorded would be the opposite failure — real product activity, invisible.
-- A separate table is the only shape that is both complete and honest:
-- question_interaction stays exactly "model calls that really ran", and the
-- cache's own value is measured in its own ledger.
--
-- estimated_cost_avoided_usd is the cost of the ORIGINAL calls that produced
-- the cached answer — what this hit did not spend. It is a saving, never
-- added to the spend total.

CREATE TABLE answer_cache (
    normalized_question  TEXT PRIMARY KEY,
    -- The question as actually typed the first time, kept for auditing what
    -- a normalized key came from.
    original_question    TEXT NOT NULL,
    -- The full httpapi.AskResponse served for this question, as JSON.
    response             JSONB NOT NULL,
    -- Sum of the real model spend that produced this entry, carried so a hit
    -- can report what it avoided without re-parsing the response body.
    origin_cost_usd      NUMERIC(12, 6) NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE answer_cache IS
    'Exact-match answer cache keyed on normalized question text. Paraphrases '
    'do NOT hit THIS lookup — matching them here would need a model or an '
    'embedding, reintroducing the cost this exact-match table exists to '
    'avoid. specs/004-semantic-cache later added a second-tier check in '
    'front of this table (see internal/paraphrase) that DOES let a '
    'paraphrase reach a cached answer: one bounded Claude Haiku 4.5 call on '
    'an exact-match miss, verified against this table''s real, current '
    'contents before ever being served. That is a disclosed, bounded, '
    're-verified cost, not the unbounded one this comment originally ruled '
    'out — see internal/paraphrase''s package doc for the full reasoning. '
    'This table''s own behavior (key, normalization, zero-cost hit) is '
    'unchanged by that addition.';

CREATE TABLE answer_cache_hit (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    normalized_question         TEXT NOT NULL,
    question_text               TEXT NOT NULL,
    estimated_cost_avoided_usd  NUMERIC(12, 6) NOT NULL,
    served_at                   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX answer_cache_hit_served_at_idx ON answer_cache_hit (served_at);

COMMENT ON TABLE answer_cache_hit IS
    'One row per answer served from cache — a NON-interaction. Kept out of '
    'question_interaction so that table stays exactly "model calls that '
    'really ran" (Constitution Principle VI), and so cost totals are never '
    'inflated by spend that did not happen.';
