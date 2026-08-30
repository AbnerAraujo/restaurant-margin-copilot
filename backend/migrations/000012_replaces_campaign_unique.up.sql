-- Overnight QA (fresh round): a real, live-reproducible race in POST
-- /api/promotions' FR-007 "replaces" path. internal/storage/promotion.go's
-- IsCampaignAlreadyReplaced (the application-level defense already added for
-- the client-dropdown version of this exact bug — see its own doc comment)
-- is a plain check-then-insert: two requests both naming the same flagged
-- campaign in "replaces", submitted close together, can both read
-- "not yet replaced" before either has committed, and both insert — double
-- awarding a Campaign Launcher badge (and its points) for one real
-- replacement. A partial unique index closes the gap the application check
-- alone cannot: only the database sees every concurrent transaction, not
-- just the one currently running.
--
-- Partial (WHERE replaces_campaign_id IS NOT NULL) because NULL is the
-- overwhelming common case (most promotions don't replace anything) and
-- NULL <> NULL in a unique index anyway, so a plain unique index would
-- already have to special-case it — being explicit says what this
-- constraint is actually for. Mirrors the existing
-- promotion_roi_record_platform_campaign_period_idx pattern in
-- 000001_init_schema.up.sql.
CREATE UNIQUE INDEX promotion_roi_record_replaces_campaign_id_idx
    ON promotion_roi_record (replaces_campaign_id)
    WHERE replaces_campaign_id IS NOT NULL;

COMMENT ON INDEX promotion_roi_record_replaces_campaign_id_idx IS
    'Enforces "a flagged campaign can only be replaced once" at the database '
    'layer — internal/storage.IsCampaignAlreadyReplaced''s own check-then-'
    'insert cannot close this alone under concurrent requests. See '
    'internal/httpapi/promotions_create.go''s isUniqueViolation handling for '
    'how a violation here is surfaced as the same 409 already_replaced error.';
