import { describe, expect, it } from 'vitest'

import {
  GUIDED_CATEGORIES,
  KNOWN_PLATFORMS,
  composeGuidedQuestion,
  isChronologicalRange,
  toGuidedParams,
  type GuidedDraft,
} from './guidedQuestion'

describe('GUIDED_CATEGORIES', () => {
  it('covers exactly the 8 real MCP tools, one category each, with no raw tool name in the label', () => {
    expect(GUIDED_CATEGORIES).toHaveLength(8)
    const tools = GUIDED_CATEGORIES.map((c) => c.tool)
    expect(new Set(tools).size).toBe(8)
    expect(tools).toEqual(
      expect.arrayContaining([
        'get_daily_summary',
        'get_margin_delta',
        'list_discrepancies',
        'get_promotion_roi',
        'list_negative_roi_promotions',
        'compare_platform_economics',
        'get_period_totals',
        'get_expense_pattern_by_day_of_month',
      ]),
    )
    for (const category of GUIDED_CATEGORIES) {
      expect(category.label).not.toContain('_')
      expect(category.label.toLowerCase()).not.toContain('get_')
    }
  })
})

describe('isChronologicalRange', () => {
  it('accepts a start on or before the end', () => {
    expect(isChronologicalRange({ start: '2026-08-01', end: '2026-08-07' })).toBe(true)
    expect(isChronologicalRange({ start: '2026-08-01', end: '2026-08-01' })).toBe(true)
  })

  it('rejects a missing field or an end before the start', () => {
    expect(isChronologicalRange({ start: '2026-08-07', end: '2026-08-01' })).toBe(false)
    expect(isChronologicalRange({ start: '2026-08-01' })).toBe(false)
    expect(isChronologicalRange({ end: '2026-08-01' })).toBe(false)
    expect(isChronologicalRange({})).toBe(false)
  })
})

describe('toGuidedParams', () => {
  it('returns null while a single-date category is incomplete, then the params once filled', () => {
    expect(toGuidedParams('daily_summary', {})).toBeNull()
    expect(toGuidedParams('daily_summary', { date: '2026-08-07' })).toEqual({
      category: 'daily_summary',
      date: '2026-08-07',
    })
  })

  it('requires both periods, in order, for margin_delta', () => {
    const partial: GuidedDraft = { periodA: { start: '2026-08-01', end: '2026-08-07' } }
    expect(toGuidedParams('margin_delta', partial)).toBeNull()

    const complete: GuidedDraft = {
      periodA: { start: '2026-08-01', end: '2026-08-07' },
      periodB: { start: '2026-08-08', end: '2026-08-14' },
    }
    expect(toGuidedParams('margin_delta', complete)).toEqual({
      category: 'margin_delta',
      periodA: { start: '2026-08-01', end: '2026-08-07' },
      periodB: { start: '2026-08-08', end: '2026-08-14' },
    })

    const backwardsPeriod: GuidedDraft = {
      periodA: { start: '2026-08-01', end: '2026-08-07' },
      periodB: { start: '2026-08-14', end: '2026-08-08' },
    }
    expect(toGuidedParams('margin_delta', backwardsPeriod)).toBeNull()
  })

  it('switches which fields it needs for discrepancies based on scope', () => {
    expect(toGuidedParams('discrepancies', { scope: 'single_date' })).toBeNull()
    expect(
      toGuidedParams('discrepancies', { scope: 'single_date', date: '2026-08-07' }),
    ).toEqual({ category: 'discrepancies', scope: 'single_date', date: '2026-08-07' })

    expect(toGuidedParams('discrepancies', { scope: 'period' })).toBeNull()
    expect(
      toGuidedParams('discrepancies', {
        scope: 'period',
        period: { start: '2026-08-01', end: '2026-08-14' },
      }),
    ).toEqual({
      category: 'discrepancies',
      scope: 'period',
      period: { start: '2026-08-01', end: '2026-08-14' },
    })
  })

  it('switches which fields it needs for promotion_roi based on mode', () => {
    expect(toGuidedParams('promotion_roi', { mode: 'campaign' })).toBeNull()
    expect(
      toGuidedParams('promotion_roi', { mode: 'campaign', campaignId: 'IFOOD-CAMP-1' }),
    ).toEqual({ category: 'promotion_roi', mode: 'campaign', campaignId: 'IFOOD-CAMP-1' })

    expect(
      toGuidedParams('promotion_roi', { mode: 'platform_period', platform: 'ifood' }),
    ).toBeNull()
    expect(
      toGuidedParams('promotion_roi', {
        mode: 'platform_period',
        platform: 'ifood',
        period: { start: '2026-08-01', end: '2026-08-14' },
      }),
    ).toEqual({
      category: 'promotion_roi',
      mode: 'platform_period',
      platform: 'ifood',
      period: { start: '2026-08-01', end: '2026-08-14' },
    })
  })

  it('requires a valid period for every remaining period-only category', () => {
    for (const category of [
      'negative_roi_promotions',
      'platform_economics',
      'period_totals',
      'expense_pattern_by_day',
    ] as const) {
      expect(toGuidedParams(category, {})).toBeNull()
      expect(
        toGuidedParams(category, { period: { start: '2026-08-01', end: '2026-08-14' } }),
      ).toEqual({ category, period: { start: '2026-08-01', end: '2026-08-14' } })
    }
  })
})

describe('composeGuidedQuestion', () => {
  it('composes a well-formed single-date question (daily_summary)', () => {
    expect(composeGuidedQuestion({ category: 'daily_summary', date: '2026-08-07' })).toBe(
      'How did we do on 2026-08-07?',
    )
  })

  it('composes a well-formed two-period comparison question (margin_delta)', () => {
    expect(
      composeGuidedQuestion({
        category: 'margin_delta',
        periodA: { start: '2026-08-01', end: '2026-08-07' },
        periodB: { start: '2026-08-08', end: '2026-08-14' },
      }),
    ).toBe(
      'Compare total margin for 2026-08-01 to 2026-08-07 against 2026-08-08 to 2026-08-14',
    )
  })

  it('composes different well-formed questions for the two discrepancies scopes', () => {
    expect(
      composeGuidedQuestion({ category: 'discrepancies', scope: 'single_date', date: '2026-08-07' }),
    ).toBe('Were there any discrepancies on 2026-08-07?')

    expect(
      composeGuidedQuestion({
        category: 'discrepancies',
        scope: 'period',
        period: { start: '2026-08-01', end: '2026-08-14' },
      }),
    ).toBe('Which days had discrepancies between 2026-08-01 and 2026-08-14?')
  })

  it('composes different well-formed questions for the two promotion_roi modes, naming the real platform label', () => {
    expect(
      composeGuidedQuestion({ category: 'promotion_roi', mode: 'campaign', campaignId: 'IFOOD-CAMP-1' }),
    ).toBe('What was the ROI for campaign IFOOD-CAMP-1?')

    expect(
      composeGuidedQuestion({
        category: 'promotion_roi',
        mode: 'platform_period',
        platform: 'ifood',
        period: { start: '2026-08-01', end: '2026-08-14' },
      }),
    ).toBe('Show me the ROI for every iFood campaign between 2026-08-01 and 2026-08-14')
  })

  it('composes a well-formed money-losing-promotions question', () => {
    expect(
      composeGuidedQuestion({
        category: 'negative_roi_promotions',
        period: { start: '2026-08-01', end: '2026-08-14' },
      }),
    ).toBe('Which promotions lost money between 2026-08-01 and 2026-08-14?')
  })

  it('composes a well-formed platform-comparison question naming both known platforms', () => {
    const question = composeGuidedQuestion({
      category: 'platform_economics',
      period: { start: '2026-08-01', end: '2026-08-14' },
    })
    for (const platform of KNOWN_PLATFORMS) {
      expect(question).toContain(platform.label)
    }
    expect(question).toBe(
      'Which platform costs me more in commission — iFood or Just Eat Takeaway — between 2026-08-01 and 2026-08-14?',
    )
  })

  it('composes a well-formed period-totals question', () => {
    expect(
      composeGuidedQuestion({
        category: 'period_totals',
        period: { start: '2026-08-01', end: '2026-08-14' },
      }),
    ).toBe(
      'What was our best day between 2026-08-01 and 2026-08-14, and what was the total margin for the period?',
    )
  })

  it('composes a well-formed expense-pattern-by-day-of-month question', () => {
    expect(
      composeGuidedQuestion({
        category: 'expense_pattern_by_day',
        period: { start: '2026-08-01', end: '2026-08-14' },
      }),
    ).toBe('Which day of the month costs the most, on average, between 2026-08-01 and 2026-08-14?')
  })
})
