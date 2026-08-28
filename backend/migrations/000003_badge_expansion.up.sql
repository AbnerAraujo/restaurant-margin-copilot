-- Schema additions for specs/002-badge-expansion: Growth, Engagement, and
-- Campaign-Creation badges.
--
-- Growth reads promotion_roi_record as it already exists (no schema change
-- needed beyond the two columns below). Campaign-Creation needs
-- promotion_roi_record to distinguish an owner-created record from one the
-- ingestion pipeline wrote, and to carry the optional "replaces" reference.
-- Engagement needs a genuinely new table: "days used" is not a fact any
-- existing table records (plan.md's Data model section).

-- origin distinguishes a record the ingestion pipeline wrote from one the
-- owner typed into POST /api/promotions (User Story 3). Defaulting to
-- 'ingested' keeps every row this migration finds already in the table
-- correct without a backfill statement.
ALTER TABLE promotion_roi_record
    ADD COLUMN origin TEXT NOT NULL DEFAULT 'ingested'
        CHECK (origin IN ('ingested', 'owner_created'));

-- replaces_campaign_id is a reference BY VALUE to another row's campaign_id
-- (campaigns are identified by string code in this schema, not a surrogate
-- key — see storage/promotion.go's PromotionPeriodRange comment for the same
-- convention elsewhere). Deliberately NOT a foreign key: the campaign it
-- names is free-text supplied by the owner and validated in application code
-- (FR-007 — re-verified server-side against list_negative_roi_promotions'
-- own query, never trusted from the client) before insert, not enforced by a
-- DB constraint that would also have to know about "currently flagged
-- negative", which is itself a computed, time-varying fact a FK cannot
-- express.
ALTER TABLE promotion_roi_record
    ADD COLUMN replaces_campaign_id TEXT;

COMMENT ON COLUMN promotion_roi_record.origin IS
    'ingested (file pipeline, the only origin before spec 002) or '
    'owner_created (POST /api/promotions, spec 002 User Story 3).';
COMMENT ON COLUMN promotion_roi_record.replaces_campaign_id IS
    'Set only on an owner_created row whose creation was framed as replacing '
    'a promotion already flagged negative-ROI (FR-006/FR-007). Backs the '
    'Campaign-Creation badge (internal/badges) — never itself re-validated '
    'after insert, since a re-ingestion cannot retroactively invalidate an '
    'action the owner already took (spec Edge Cases).';

-- One row per real, timestamped app-open (FR-003). occurred_on is a STORED
-- generated column, not computed in application code, so "one row per
-- calendar day" can be enforced by a single unique index at the database
-- layer — the actual mechanism behind FR-... /Acceptance Scenario 3's "the
-- server owns correctness, no manual dedup by the caller": two ping requests
-- on the same UTC day collide on this index and the second is a no-op
-- (ON CONFLICT DO NOTHING in the query, see queries/usage_event.sql).
--
-- UTC, not session/local time zone: spec 002's Assumptions call for the
-- server's own date convention rather than the owner's wall-clock date, the
-- same reasoning that already grounds "today" elsewhere in this product
-- (cmd/server/main.go's GetDataDateRange) — a single process, a single
-- definition of "what day is it", never a client-supplied one.
CREATE TABLE usage_event (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    occurred_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    occurred_on  DATE GENERATED ALWAYS AS ((occurred_at AT TIME ZONE 'UTC')::date) STORED NOT NULL
);

CREATE UNIQUE INDEX usage_event_occurred_on_idx ON usage_event (occurred_on);

COMMENT ON TABLE usage_event IS
    'One row per distinct UTC calendar day the app was opened (FR-003). '
    'No tenant/account column yet — single-owner prototype (spec 002 '
    'Assumptions); the shape to add tenant_id to if/when multi-tenant is '
    'ever built. Never pre-seeded: a fresh instance has zero rows, which is '
    'the correct, non-fabricated starting state (SC-002).';
