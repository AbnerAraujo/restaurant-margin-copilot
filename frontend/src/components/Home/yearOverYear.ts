import type { DaySummaryApi } from '@/components/Home/homeInsights'

// Spec 008 FR-006 (Home year-over-year tile): a pure derivation over the
// SAME `days` array `GET /api/reconciliation` already returns to this page
// (no new fetch, no new backend endpoint — this dataset already covers the
// full persisted range, not just a recent window, per
// httpapi.HandleReconciliation's own wide-open default). Kept in its own
// file rather than added to homeInsights.ts to avoid touching that
// already-landed User Story 3 file at all.

function pad2(n: number): string {
  return n < 10 ? `0${n}` : `${n}`
}

export interface YearOverYear {
  thisPeriodUsd: number
  priorYearUsd: number
  deltaUsd: number
  /** The real, identical day range compared in both years — e.g. "Aug 1-14". */
  label: string
}

/**
 * Month-to-date vs. the SAME calendar days one year earlier — never a full
 * month compared against a partial one, and never computed at all unless
 * every one of those exact calendar dates is present for BOTH years
 * (spec.md's acceptance scenario: "a full prior-year period of EQUAL
 * LENGTH", FR-013's degrade-to-omission discipline). A real gap in the
 * data (this project's fixture has one on purpose) simply shrinks the
 * month-to-date window being compared rather than being padded over.
 */
export function deriveYearOverYear(days: DaySummaryApi[]): YearOverYear | null {
  if (days.length === 0) return null

  const byDate = new Map(days.map((d) => [d.date, d]))
  const latest = days[days.length - 1]
  const [latestYearStr, latestMonthStr, latestDayStr] = latest.date.split('-')
  const latestYear = Number(latestYearStr)
  const latestMonth = Number(latestMonthStr)
  const latestDay = Number(latestDayStr)

  const thisPeriodDates: string[] = []
  for (let day = 1; day <= latestDay; day++) {
    const dateStr = `${latestYearStr}-${latestMonthStr}-${pad2(day)}`
    if (byDate.has(dateStr)) thisPeriodDates.push(dateStr)
  }
  if (thisPeriodDates.length === 0) return null

  const priorYear = latestYear - 1
  const priorPeriodDates = thisPeriodDates.map((d) => `${priorYear}-${d.slice(5)}`)
  if (!priorPeriodDates.every((d) => byDate.has(d))) return null

  const sum = (dates: string[]) =>
    dates.reduce((total, d) => total + Number(byDate.get(d)!.margin), 0)

  const thisPeriodUsd = sum(thisPeriodDates)
  const priorYearUsd = sum(priorPeriodDates)

  const label =
    thisPeriodDates.length === 1
      ? `${pad2(latestMonth)}/${pad2(latestDay)}`
      : `${pad2(latestMonth)}/01–${pad2(latestMonth)}/${pad2(latestDay)}`

  return { thisPeriodUsd, priorYearUsd, deltaUsd: thisPeriodUsd - priorYearUsd, label }
}
