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
