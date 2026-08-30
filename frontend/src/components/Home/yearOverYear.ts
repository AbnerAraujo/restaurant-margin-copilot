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

const MONTH_ABBREVIATIONS = [
  'Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun',
  'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec',
]

/**
 * The day range this tile compares, in the app's own short-date form — the
 * one `guidedQuestion.formatDisplayDate`, `ProvenanceTag` and the chart axes
 * already use ("Aug 1–29").
 *
 * This label used to be built as `MM/DD`, which read "08/01–08/29" on screen.
 * Two things were wrong with it. It was the only date on Home not in the
 * app's format — every other one, including the Recent closes table directly
 * below this tile, is ISO or a month abbreviation. And numeric month/day with
 * no year is genuinely ambiguous: "08/01" is 1 August to a US reader and 8
 * January to most others, on a tile whose entire job is comparing one date
 * range against the same range a year earlier. Spelling the month removes
 * both problems, and matches what this file's own doc comment always claimed
 * the label looked like.
 *
 * No year appears here on purpose: the surrounding copy supplies it ("…, this
 * year" / "…, last year"), so a year in the label would contradict one of the
 * two stats it sits above.
 *
 * Built by hand from the numbers rather than via `Date`/`toLocaleDateString`
 * for the same reason `formatDisplayDate` is: those parse an ISO date as UTC
 * midnight and render it in the viewer's local zone, shifting the displayed
 * day by one for anyone west of UTC.
 */
function formatMonthDay(month: number, day: number): string {
  const abbreviation = MONTH_ABBREVIATIONS[month - 1]
  if (!abbreviation) return `${pad2(month)}-${pad2(day)}`
  return `${abbreviation} ${day}`
}

export interface YearOverYear {
  thisPeriodUsd: number
  priorYearUsd: number
  deltaUsd: number
  /** The real, identical day range compared in both years — e.g. "Aug 1–14". */
  label: string
}

/**
 * Month-to-date vs. the SAME calendar days one year earlier — never a full
 * month compared against a partial one, and never computed at all unless
 * every one of those exact calendar dates is present for BOTH years
 * (spec.md's acceptance scenario: "a full prior-year period of EQUAL
 * LENGTH", FR-013's degrade-to-omission discipline). A real gap in the
 * data (this project's dataset has one on purpose) simply shrinks the
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
      ? formatMonthDay(latestMonth, latestDay)
      : `${formatMonthDay(latestMonth, 1)}–${latestDay}`

  return { thisPeriodUsd, priorYearUsd, deltaUsd: thisPeriodUsd - priorYearUsd, label }
}
