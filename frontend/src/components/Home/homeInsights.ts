/**
 * Pure, deterministic derivations over the `days` array `GET /api/reconciliation`
 * already returns — no new fetch, no new backend endpoint (spec 008 User
 * Story 3, tasks T026/T027). Both functions follow FR-013's discipline:
 * when there isn't enough real data to compute an honest figure, they
 * return `null` rather than a zero or a placeholder, and the caller must
 * render nothing for that case.
 */

export interface DaySummaryApi {
  date: string
  margin: string
  discrepancy_flags: { type: string; detail: string }[]
}

export type TrendDirection = 'up' | 'down' | 'flat'

export interface MarginTrend {
  direction: TrendDirection
  /** Latest day's margin minus the comparison day's margin, in dollars. */
  deltaUsd: number
  /** The date compared against — see the comparison-point note below. */
  comparisonDate: string
}

/**
 * Comparison point, chosen and documented rather than left ambiguous
 * (plan.md's own open question): the immediately PRECEDING entry in `days`,
 * not calendar-yesterday. `days` can have real gaps (a missing ingestion
 * day is a known, deliberate case in this project's fixture set), so "the
 * previous reconciled day" is the honest, always-available comparison
 * point — comparing against a fixed calendar offset would silently produce
 * no result across a gap even though a perfectly good prior data point
 * exists one slot back.
 */
export function deriveMarginTrend(days: DaySummaryApi[]): MarginTrend | null {
  if (days.length < 2) return null

  const latest = days[days.length - 1]
  const previous = days[days.length - 2]
  const deltaUsd = Number(latest.margin) - Number(previous.margin)
  const direction: TrendDirection =
    deltaUsd > 0 ? 'up' : deltaUsd < 0 ? 'down' : 'flat'

  return { direction, deltaUsd, comparisonDate: previous.date }
}

export interface BiggestWinCatch {
  bestDay: DaySummaryApi
  worstDay: DaySummaryApi
  /** How many of the trailing 7 days actually had data — shown so the card
   * never implies a full week when fewer days are really behind it. */
  windowDays: number
}

/**
 * Trailing-7 best/worst day by margin, scoped to however many of those
 * days actually exist (FR-009) — never padded with a day that has no data.
 * Requires at least 2 real days, matching the same floor `deriveMarginTrend`
 * uses: with only 1 day, "biggest win" and "biggest catch" would be the
 * same day shown twice, which reads as a broken card rather than a useful
 * one, so this degrades to omission (FR-013) below that floor too.
 */
export function deriveBiggestWinCatch(
  days: DaySummaryApi[],
): BiggestWinCatch | null {
  if (days.length < 2) return null

  const window = days.slice(-7)
  let bestDay = window[0]
  let worstDay = window[0]
  for (const day of window) {
    if (Number(day.margin) > Number(bestDay.margin)) bestDay = day
    if (Number(day.margin) < Number(worstDay.margin)) worstDay = day
  }

  return { bestDay, worstDay, windowDays: window.length }
}
