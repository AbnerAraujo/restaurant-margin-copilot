import { describe, expect, it } from 'vitest'

import {
  deriveBiggestWinCatch,
  deriveMarginTrend,
  type DaySummaryApi,
} from './homeInsights'

function day(date: string, margin: string): DaySummaryApi {
  return { date, margin, discrepancy_flags: [] }
}

describe('deriveMarginTrend', () => {
  it('returns null when fewer than 2 days exist (FR-013: omit, never a placeholder)', () => {
    expect(deriveMarginTrend([])).toBeNull()
    expect(deriveMarginTrend([day('2026-08-14', '100.00')])).toBeNull()
  })

  it('reports "up" and the real dollar delta when the latest day beat the previous one', () => {
    const trend = deriveMarginTrend([
      day('2026-08-13', '100.00'),
      day('2026-08-14', '150.00'),
    ])
    expect(trend).toEqual({
      direction: 'up',
      deltaUsd: 50,
      comparisonDate: '2026-08-13',
    })
  })

  it('reports "down" and a negative delta when the latest day is worse', () => {
    const trend = deriveMarginTrend([
      day('2026-08-13', '100.00'),
      day('2026-08-14', '60.00'),
    ])
    expect(trend).toEqual({
      direction: 'down',
      deltaUsd: -40,
      comparisonDate: '2026-08-13',
    })
  })

  it('reports "flat" when the latest day exactly matches the previous one', () => {
    const trend = deriveMarginTrend([
      day('2026-08-13', '100.00'),
      day('2026-08-14', '100.00'),
    ])
    expect(trend?.direction).toBe('flat')
    expect(trend?.deltaUsd).toBe(0)
  })

  it('compares against the immediately preceding reconciled day, not a fixed calendar offset, across a real gap', () => {
    // 2026-08-08 is missing (a constructed gap, mirroring the dataset's own
    // known missing-delivery day) — the
    // comparison point must still be the previous REAL entry (08-07), not
    // silently produce no result because "yesterday" (08-09) has no data.
    const trend = deriveMarginTrend([
      day('2026-08-07', '80.00'),
      day('2026-08-09', '120.00'),
    ])
    expect(trend).toEqual({
      direction: 'up',
      deltaUsd: 40,
      comparisonDate: '2026-08-07',
    })
  })
})

describe('deriveBiggestWinCatch', () => {
  it('returns null when fewer than 2 days exist (FR-013: omit, never a degenerate single-day card)', () => {
    expect(deriveBiggestWinCatch([])).toBeNull()
    expect(deriveBiggestWinCatch([day('2026-08-14', '100.00')])).toBeNull()
  })

  it('picks the real best and worst day within the trailing window', () => {
    const result = deriveBiggestWinCatch([
      day('2026-08-10', '50.00'),
      day('2026-08-11', '-30.00'),
      day('2026-08-12', '200.00'),
      day('2026-08-13', '10.00'),
    ])
    expect(result?.bestDay.date).toBe('2026-08-12')
    expect(result?.worstDay.date).toBe('2026-08-11')
    expect(result?.windowDays).toBe(4)
  })

  it('scopes honestly to fewer than 7 days rather than padding a missing day (spec.md acceptance scenario 2)', () => {
    const result = deriveBiggestWinCatch([
      day('2026-08-12', '50.00'),
      day('2026-08-13', '75.00'),
    ])
    expect(result?.windowDays).toBe(2)
    expect(result?.bestDay.date).toBe('2026-08-13')
    expect(result?.worstDay.date).toBe('2026-08-12')
  })

  it('ignores anything older than the trailing 7 reconciled days', () => {
    const days = [
      day('2026-08-01', '9999.00'), // would dominate if not excluded by the window
      day('2026-08-08', '10.00'),
      day('2026-08-09', '20.00'),
      day('2026-08-10', '30.00'),
      day('2026-08-11', '40.00'),
      day('2026-08-12', '50.00'),
      day('2026-08-13', '60.00'),
      day('2026-08-14', '70.00'),
    ]
    const result = deriveBiggestWinCatch(days)
    expect(result?.windowDays).toBe(7)
    expect(result?.bestDay.date).toBe('2026-08-14')
    expect(result?.worstDay.date).toBe('2026-08-08')
  })
})
