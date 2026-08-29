/**
 * Mirrors `PointsCleanClose` / `PointsDiscrepancyCatcher` /
 * `PointsGrowth` / `PointsEngagement` / `PointsCampaignCreation` in
 * `backend/internal/badges/badges.go` — see that file's doc comment for the
 * reasoning behind each weight (spec 002-badge-expansion added the last
 * three: Growth 15, Engagement 5, Campaign Launcher 30).
 *
 * These are used ONLY for "what you could earn" copy — a forward-looking
 * statement the backend has no endpoint for, because nothing has been earned
 * yet. Every figure describing what HAS been earned comes from
 * `GET /api/badges` and is never computed from these constants, so a drift
 * between the two can never silently change a real balance; at worst it
 * makes an incentive line say the wrong number, which the value shown beside
 * it would immediately contradict.
 */
export const POINTS_PER_BADGE = {
  clean_close: 10,
  discrepancy_catcher: 25,
  growth: 15,
  engagement: 5,
  campaign_creation: 30,
} as const

/**
 * Mirrors `badges.CentsPerPoint` in `backend/internal/badges/badges.go` —
 * 1 point = $0.10. Used ONLY to preview how many points a spend amount would
 * need before the owner submits; the actual redemption is always re-checked
 * server-side against the real, live balance (POST /api/promotions), the
 * same "client previews, server verifies" discipline the FR-007 replaces
 * claim already uses. A drift here would at worst show a wrong preview
 * number, never let a redemption through the server didn't independently
 * confirm.
 */
export const CENTS_PER_POINT = 10
