/**
 * Mirrors `PointsCleanClose` / `PointsDiscrepancyCatcher` in
 * `backend/internal/badges/badges.go`.
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
} as const
