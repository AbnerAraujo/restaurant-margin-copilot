import { describe, expect, it } from 'vitest'

import { buildCompareToLastPeriodQuestion, derivePriorPeriod } from './comparePeriod'

describe('derivePriorPeriod', () => {
  it('derives the prior CALENDAR month for a full calendar month, not a fixed-day shift', () => {
    expect(derivePriorPeriod({ start: '2026-07-01', end: '2026-07-31' })).toEqual({
      start: '2026-06-01',
      end: '2026-06-30',
    })
  })

  it('handles February (non-leap) correctly as a full-month period', () => {
    expect(derivePriorPeriod({ start: '2025-03-01', end: '2025-03-31' })).toEqual({
      start: '2025-02-01',
      end: '2025-02-28',
    })
  })

  it('handles February (leap year) correctly as a full-month period', () => {
    expect(derivePriorPeriod({ start: '2024-03-01', end: '2024-03-31' })).toEqual({
      start: '2024-02-01',
      end: '2024-02-29',
    })
  })

  it('crosses a year boundary for January', () => {
    expect(derivePriorPeriod({ start: '2026-01-01', end: '2026-01-31' })).toEqual({
      start: '2025-12-01',
      end: '2025-12-31',
    })
  })

  it('derives the prior CALENDAR year for a full calendar year', () => {
    expect(derivePriorPeriod({ start: '2026-01-01', end: '2026-12-31' })).toEqual({
      start: '2025-01-01',
      end: '2025-12-31',
    })
  })

  it('falls back to an immediately-preceding fixed-length shift for an arbitrary custom range', () => {
    expect(derivePriorPeriod({ start: '2026-08-01', end: '2026-08-14' })).toEqual({
      start: '2026-07-18',
      end: '2026-07-31',
    })
  })

  it('handles a single day', () => {
    expect(derivePriorPeriod({ start: '2026-08-07', end: '2026-08-07' })).toEqual({
      start: '2026-08-06',
      end: '2026-08-06',
    })
  })
})

describe('buildCompareToLastPeriodQuestion', () => {
  it('states both periods as absolute, fully-dated ranges', () => {
    expect(buildCompareToLastPeriodQuestion({ start: '2026-08-01', end: '2026-08-31' })).toBe(
      'What was our margin for 2026-08-01 through 2026-08-31, compared to 2026-07-01 through 2026-07-31?',
    )
  })

  it('states a single-day period without a redundant "through" range', () => {
    expect(buildCompareToLastPeriodQuestion({ start: '2026-08-07', end: '2026-08-07' })).toBe(
      'What was our margin for 2026-08-07, compared to 2026-08-06?',
    )
  })
})
