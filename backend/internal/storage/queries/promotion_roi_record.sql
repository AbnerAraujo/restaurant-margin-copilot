-- name: UpsertPromotionRoiRecord :one
-- Writer: internal/reconcile/ only. roi/attributed_* are NULL together when
-- attribution is unavailable (FR-013) — this query never fills them in.
INSERT INTO promotion_roi_record (
    platform,
    campaign_id,
    period,
    spend,
    attributed_incremental_orders,
    attributed_incremental_revenue,
    roi,
    flagged_negative,
    source_row_refs
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (platform, campaign_id, period) DO UPDATE SET
    spend                          = EXCLUDED.spend,
    attributed_incremental_orders  = EXCLUDED.attributed_incremental_orders,
    attributed_incremental_revenue = EXCLUDED.attributed_incremental_revenue,
    roi                            = EXCLUDED.roi,
    flagged_negative               = EXCLUDED.flagged_negative,
    source_row_refs                = EXCLUDED.source_row_refs,
    updated_at                     = now()
RETURNING *;

-- name: GetPromotionRoiByCampaign :many
-- Backs the get_promotion_roi MCP tool contract (campaign_id input form).
SELECT * FROM promotion_roi_record
WHERE campaign_id = $1
ORDER BY period;

-- name: GetPromotionRoiByPlatformAndPeriod :many
-- Backs the get_promotion_roi MCP tool contract (platform+period input form).
SELECT * FROM promotion_roi_record
WHERE platform = $1 AND period && $2::daterange
ORDER BY period;

-- name: ListNegativeRoiPromotions :many
-- Backs the list_negative_roi_promotions MCP tool contract (spec User Story 4 / SC-006).
SELECT * FROM promotion_roi_record
WHERE flagged_negative = true AND period && $1::daterange
ORDER BY period;

-- name: ListDistinctCampaignIDs :many
-- Backs the campaign-lookup fuzzy-match fix (docs/plan.md mistakes log:
-- "campaign name/entity lookup defect"): the real, bounded set of
-- campaign_id values that actually exist, so a human-readable name or
-- shortened form (e.g. "LUNCHFIX", or the full display name "Banner Ad -
-- Lunch Fix Menu (JET-CAMP-LUNCHFIX)") can be matched against the known
-- set in Go code (internal/mcptools) rather than requiring an exact
-- string match or letting the model guess an id that doesn't exist
-- (Constitution Principle III: a typed, bounded match, never open-ended
-- fuzzy computation).
SELECT DISTINCT campaign_id FROM promotion_roi_record ORDER BY campaign_id;

-- name: CreateOwnerPromotion :one
-- Backs POST /api/promotions (User Story 3): the owner logging a new
-- promotion record directly in the app, per FR-005/FR-006. Deliberately a
-- plain INSERT, not the same ON CONFLICT upsert UpsertPromotionRoiRecord
-- uses for the ingestion pipeline — a second submission with the same
-- platform/campaign_id/period is a genuine new attempt from a human, not a
-- re-run of the same deterministic computation, so a unique-violation here
-- should surface as a real "that campaign already exists" error
-- (internal/httpapi), not silently overwrite what's there.
--
-- attributed_incremental_orders/revenue and roi are never supplied here: an
-- owner-created record has not been through attribution at all (no
-- delivery-platform data has been tagged to it yet), which is a DIFFERENT
-- fact from FR-013's "unattributable after trying" — both render the same
-- way (roi: null) because both really are "no ROI is known", never a
-- computed-looking zero.
INSERT INTO promotion_roi_record (
    platform,
    campaign_id,
    period,
    spend,
    flagged_negative,
    source_row_refs,
    origin,
    replaces_campaign_id,
    payment_method,
    points_spent
) VALUES (
    $1, $2, $3, $4, false, $5, 'owner_created', $6, $7, $8
)
RETURNING *;

-- name: ListAllPromotionRoiRecords :many
-- Backs GET /api/promotions (internal/httpapi/data.go): the whole promotion
-- set for the Promotions page, which is a deliberate full listing rather
-- than a period query — the page's job is "every campaign on file and
-- whether it paid for itself", and silently scoping it to a default window
-- would hide campaigns rather than answer that question.
SELECT * FROM promotion_roi_record
ORDER BY period, campaign_id;

-- name: SumPointsSpentOnPromotions :one
-- The other half of a points BALANCE (internal/badges.EvaluatePoints only
-- ever computes what's been EARNED): every point already committed to a
-- promotion's spend, regardless of that promotion's own period, since a
-- redeemed point stays spent forever, not just within one reporting window.
-- COALESCE keeps a fresh instance with zero points-paid rows at a real 0,
-- never NULL, so callers never special-case "no rows yet".
SELECT COALESCE(SUM(points_spent), 0)::bigint AS total_points_spent
FROM promotion_roi_record
WHERE payment_method = 'points';
