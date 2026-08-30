import { describe, expect, it } from 'vitest'

import { summarizeFlags } from './discrepancyFlags'

describe('summarizeFlags', () => {
  it('summarizes a single known flag type with a count', () => {
    expect(
      summarizeFlags([{ type: 'duplicate_order_removed', detail: 'raw text' }]),
    ).toBe('1 duplicate order removed')
  })

  it('pluralizes correctly for counts above one', () => {
    expect(
      summarizeFlags([
        { type: 'duplicate_order_removed', detail: 'a' },
        { type: 'duplicate_order_removed', detail: 'b' },
      ]),
    ).toBe('2 duplicate orders removed')
  })

  it('never falls back to the raw per-flag detail text', () => {
    const manyDuplicates = Array.from({ length: 32 }, (_, i) => ({
      type: 'cross_source_duplicate_removed',
      detail: `POS ticket POS-SIM-${i} carries simulated://provenance/row/${i}`,
    }))
    const summary = summarizeFlags(manyDuplicates)
    expect(summary).toBe('32 duplicates counted once')
    expect(summary).not.toMatch(/POS-SIM-|simulated:\/\//)
  })

  it('orders by owner-actionability and caps at two phrases with a fold-in count', () => {
    const summary = summarizeFlags([
      { type: 'cross_source_duplicate_removed', detail: '' },
      { type: 'anomaly_threshold_exceeded', detail: '' },
      { type: 'cross_source_amount_mismatch', detail: '' },
      { type: 'pos_non_completed_row_excluded', detail: '' },
    ])
    // amount_mismatch and anomaly outrank duplicate_removed and
    // pos_non_completed_row_excluded per the priority list.
    expect(summary).toBe(
      '1 order with a promotion-driven amount difference, an unusual change in revenue, and 2 more things flagged',
    )
  })

  it('never drops an unrecognized flag type, folding it into a generic count', () => {
    expect(summarizeFlags([{ type: 'some_future_flag', detail: 'x' }])).toBe(
      '1 other item flagged',
    )
  })
})
