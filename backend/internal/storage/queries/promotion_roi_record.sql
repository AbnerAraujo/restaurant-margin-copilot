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

-- name: ListAllPromotionRoiRecords :many
-- Backs GET /api/promotions (internal/httpapi/data.go): the whole promotion
-- set for the Promotions page, which is a deliberate full listing rather
-- than a period query — the page's job is "every campaign on file and
-- whether it paid for itself", and silently scoping it to a default window
-- would hide campaigns rather than answer that question.
SELECT * FROM promotion_roi_record
ORDER BY period, campaign_id;
