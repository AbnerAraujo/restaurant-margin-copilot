// Spec 008 FR-004 ("Compare to last period"): a pure, calendar-aware
// TypeScript port of the backend's `httpapi.derivePriorPeriod` — kept as a
// deliberate duplication (not a shared package) since this is plain date
// arithmetic on strings the client already has (`AnswerChatMessage.resolvedPeriod`,
// itself sourced from `AskResponse.resolved_period`), not a new tool call
// or a re-computation of anything the backend already decided. The button
// this feeds re-asks the derived comparison through the real `/api/ask`
// path (ChatPanel's existing `submitQuestion`) — the backend's own gate is
// still the one and only authority on whether the derived period is
// actually answerable (FR-005), this file only builds the question text.
//
// Dates are plain "YYYY-MM-DD" strings throughout, parsed/formatted with
// UTC semantics (`Date.UTC`) so a day never shifts under a browser's local
// timezone — the same "date, not an instant" discipline this codebase
// applies to every date field on the wire.

/** Mirrors `ChatResolvedPeriod` — kept structurally compatible on purpose. */
export interface PriorPeriodInput {
  start: string
  end: string
}

function parseISODate(s: string): Date {
  const [year, month, day] = s.split('-').map(Number)
  return new Date(Date.UTC(year, month - 1, day))
}

function formatISODate(d: Date): string {
  return d.toISOString().slice(0, 10)
}

function addDaysUTC(d: Date, days: number): Date {
  return new Date(d.getTime() + days * 86_400_000)
}

function isFullCalendarMonth(start: Date, end: Date): boolean {
  if (start.getUTCDate() !== 1) return false
  if (start.getUTCFullYear() !== end.getUTCFullYear() || start.getUTCMonth() !== end.getUTCMonth()) {
    return false
  }
  const lastDay = new Date(Date.UTC(start.getUTCFullYear(), start.getUTCMonth() + 1, 0))
  return (
    end.getUTCFullYear() === lastDay.getUTCFullYear() &&
    end.getUTCMonth() === lastDay.getUTCMonth() &&
    end.getUTCDate() === lastDay.getUTCDate()
  )
}

function isFullCalendarYear(start: Date, end: Date): boolean {
  return (
    start.getUTCMonth() === 0 &&
    start.getUTCDate() === 1 &&
    end.getUTCMonth() === 11 &&
    end.getUTCDate() === 31 &&
    start.getUTCFullYear() === end.getUTCFullYear()
  )
}

/**
 * Computes the immediately preceding period of the same real-world length —
 * calendar-aware for a full month or a full year (mirroring the backend's
 * `derivePriorPeriod` exactly), and a fixed-length immediately-preceding
 * shift for anything else (a week, a custom range).
 */
export function derivePriorPeriod(period: PriorPeriodInput): PriorPeriodInput {
  const start = parseISODate(period.start)
  const end = parseISODate(period.end)

  if (isFullCalendarMonth(start, end)) {
    const priorMonthEnd = addDaysUTC(start, -1)
    const priorMonthStart = new Date(Date.UTC(priorMonthEnd.getUTCFullYear(), priorMonthEnd.getUTCMonth(), 1))
    return { start: formatISODate(priorMonthStart), end: formatISODate(priorMonthEnd) }
  }
  if (isFullCalendarYear(start, end)) {
    const priorYear = start.getUTCFullYear() - 1
    return {
      start: formatISODate(new Date(Date.UTC(priorYear, 0, 1))),
      end: formatISODate(new Date(Date.UTC(priorYear, 11, 31))),
    }
  }

  const days = Math.round((end.getTime() - start.getTime()) / 86_400_000) + 1
  const priorEnd = addDaysUTC(start, -1)
  const priorStart = addDaysUTC(priorEnd, -(days - 1))
  return { start: formatISODate(priorStart), end: formatISODate(priorEnd) }
}

/**
 * Builds the real, self-contained comparison question the "Compare to last
 * period" button submits — both periods stated as absolute, fully-dated
 * ranges (never "last month"/"the period before"), since the ambiguity
 * gate resolves an explicit, fully-dated range far more reliably than a
 * relative phrase (verified live: an out-of-range fully-dated comparison
 * question comes back with a refusal that plainly names the out-of-range
 * dates — see this task's own verification note).
 */
export function buildCompareToLastPeriodQuestion(resolvedPeriod: PriorPeriodInput): string {
  const prior = derivePriorPeriod(resolvedPeriod)
  const label = (p: PriorPeriodInput) => (p.start === p.end ? p.start : `${p.start} through ${p.end}`)
  return `What was our margin for ${label(resolvedPeriod)}, compared to ${label(prior)}?`
}
