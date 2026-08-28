-- Paraphrase-match ledger for POST /api/ask (specs/004-semantic-cache).
--
-- Sits alongside answer_cache / answer_cache_hit as a THIRD table, not a
-- column bolted onto either of them, for the same reason those two are
-- already split: a paraphrase hit is neither "a model call that really ran"
-- (question_interaction — it never runs the ambiguity gate or explain) nor
-- "free" the way an exact-text hit is (answer_cache_hit — that ledger's
-- whole point is estimated_cost_avoided_usd with nothing else spent to net
-- against it). A paraphrase hit is a THIRD, distinct state: it spends a
-- small real amount (one bounded Claude Haiku 4.5 classification call) to
-- avoid a much larger one (the full gate+explain cycle). Cramming that into
-- either existing table would force a choice between hiding the real cost
-- (if squeezed into answer_cache_hit's shape) or fabricating a fake
-- ambiguity_gate_result for a call that was never the ambiguity gate (if
-- squeezed into question_interaction, whose CHECK constraint requires one).
-- A dedicated table keeps both numbers honest and un-netted (spec FR-005).
--
-- classification_cost_usd is the REAL money this row's classification call
-- cost — always > 0 in practice, since reaching this table means a Haiku
-- call actually ran and returned a verified match.
--
-- cost_avoided_usd is what the ORIGINAL gate+explain cycle that produced the
-- matched cache entry cost — i.e. answer_cache.origin_cost_usd for the
-- matched row, carried here so a paraphrase hit's net saving can be reported
-- without re-joining back to answer_cache (which may since have been
-- cleared by a later ingestion — this ledger is a historical record, not a
-- live pointer).
--
-- matched_normalized_question is NOT a foreign key into answer_cache: that
-- row can legitimately be gone by the time anyone reads this ledger (a later
-- ingestion clears the whole cache), and this is a record of what happened,
-- not a live reference that must still resolve.

CREATE TABLE paraphrase_match (
    id                             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    new_question                   TEXT NOT NULL,
    matched_normalized_question    TEXT NOT NULL,
    classification_input_tokens    INTEGER NOT NULL,
    classification_output_tokens   INTEGER NOT NULL,
    classification_cost_usd        NUMERIC(12, 6) NOT NULL,
    classification_latency_ms      INTEGER NOT NULL,
    cost_avoided_usd               NUMERIC(12, 6) NOT NULL,
    matched_at                     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX paraphrase_match_matched_at_idx ON paraphrase_match (matched_at);

COMMENT ON TABLE paraphrase_match IS
    'One row per answer served via paraphrase recognition (specs/004-semantic-cache) — '
    'a real, bounded Claude Haiku 4.5 classification call (classification_cost_usd, '
    'never zero) that avoided a full gate+explain cycle (cost_avoided_usd). Kept out '
    'of both question_interaction (this is not the ambiguity gate or explain running) '
    'and answer_cache_hit (this is not free) so all three states — fresh call, exact-text '
    'hit, paraphrase hit — stay distinguishable and no cost is ever netted or hidden.';
