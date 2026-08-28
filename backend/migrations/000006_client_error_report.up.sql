-- Client-side error feed. A real bug (a stale, pre-schema-change chat
-- message shape in localStorage crashing the renderer) was diagnosed
-- tonight by reasoning about git history and reproducing manually — there
-- was no record of it actually happening in a real browser. This table is
-- the fix for THAT gap: any frontend crash the app can catch about itself
-- gets a real, timestamped, queryable record, instead of only ever being
-- known if a person happens to notice and describe it.
--
-- Deliberately its own table, not squeezed into question_interaction: this
-- has nothing to do with a model call, gate, or explain step — it is a
-- frontend defect report, a different kind of fact entirely.

CREATE TABLE client_error_report (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    message      TEXT NOT NULL,
    component    TEXT,
    stack        TEXT,
    url          TEXT,
    user_agent   TEXT,
    occurred_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX client_error_report_occurred_at_idx ON client_error_report (occurred_at);

COMMENT ON TABLE client_error_report IS
    'One row per frontend crash caught by the ErrorBoundary (frontend/src/components/ErrorBoundary.tsx). '
    'Read-only feed for retrospectives, not a support ticket queue — no auth, no PII collected on purpose '
    '(single-owner prototype, per the same scope constraint as everywhere else in this build).';
