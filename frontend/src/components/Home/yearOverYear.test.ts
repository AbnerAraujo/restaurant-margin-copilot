import { describe, expect, it } from 'vitest'

import type { DaySummaryApi } from './homeInsights'
import { deriveYearOverYear } from './yearOverYear'

function day(date: string, margin: string): DaySummaryApi {
  return { date, margin, discrepancy_flags: [] }
}

describe('deriveYearOverYear', () => {
  it('returns null with no data', () => {
    expect(deriveYearOverYear([])).toBeNull()
  })

  it('returns null when the same calendar days one year earlier are not all present (FR-013: omit, never a partial figure)', () => {
    const days = [day('2025-08-01', '10.00'), day('2026-08-01', '20.00')]
    // 2025-08-01 exists, but the comparison needs it for THIS latest day's
    // exact month-to-date window — with only a single day of history this
    // year, that window is just 2026-08-01, and 2025-08-01 IS present, so
    // this actually succeeds; the real gap case is tested below.
    expect(deriveYearOverYear(days)).not.toBeNull()

    const withGap = [day('2026-08-01', '10.00'), day('2026-08-02', '15.00')]
    // No 2025 data at all — the prior-year window is entirely absent.
    expect(deriveYearOverYear(withGap)).toBeNull()
  })

  it('compares month-to-date against the exact same calendar days one year earlier when all are present', () => {
    const days = [
      day('2025-08-01', '100.00'),
      day('2025-08-02', '120.00'),
      day('2025-08-03', '90.00'),
      day('2026-08-01', '150.00'),
      day('2026-08-02', '130.00'),
      day('2026-08-03', '200.00'),
    ]
    const result = deriveYearOverYear(days)
    expect(result).toEqual({
      thisPeriodUsd: 480, // 150 + 130 + 200
      priorYearUsd: 310, // 100 + 120 + 90
      deltaUsd: 170,
      label: '08/01–08/03',
    })
  })

  it('never pads over a real gap — a missing day this year shrinks the compared window rather than being treated as zero', () => {
    const days = [
      day('2025-08-01', '100.00'),
      day('2025-08-02', '120.00'),
      day('2025-08-03', '90.00'),
      day('2026-08-01', '150.00'),
      // 2026-08-02 is missing (a real, known ingestion gap) —
      day('2026-08-03', '200.00'),
    ]
    // The month-to-date window this year is {08-01, 08-03} (08-02 skipped),
    // so the prior-year comparison must use exactly {2025-08-01, 2025-08-03}
    // too, never the full 3-day 2025 window.
    const result = deriveYearOverYear(days)
    expect(result).toEqual({
      thisPeriodUsd: 350, // 150 + 200
      priorYearUsd: 190, // 100 + 90
      deltaUsd: 160,
      label: '08/01–08/03',
    })
  })

  it('labels a single-day window without a redundant range', () => {
    const days = [day('2025-08-01', '100.00'), day('2026-08-01', '150.00')]
    const result = deriveYearOverYear(days)
    expect(result?.label).toBe('08/01')
  })
})
