import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { Stat } from './stat'

/**
 * Regression coverage for a reported mobile defect: at 390px, `Stat` labels
 * and captions used a single-line `truncate` (overflow: hidden;
 * text-overflow: ellipsis; white-space: nowrap), which cut long text off
 * mid-word — e.g. "Days with a flag" rendered as "DAYS WITH A FL…". jsdom
 * has no real layout engine, so this can't assert the rendered pixel
 * result, but it can assert the component no longer opts into single-line
 * truncation for these two fields, and instead uses a bounded multi-line
 * clamp that wraps whole words.
 */
describe('Stat', () => {
  it('does not single-line-truncate a long label', () => {
    render(<Stat label="Just Eat Takeaway effective commission rate" value="20.00%" />)

    const label = screen.getByText('Just Eat Takeaway effective commission rate')
    expect(label.className).not.toContain('truncate')
    expect(label.className).toContain('line-clamp-2')
  })

  it('does not single-line-truncate a long caption', () => {
    render(<Stat label="Period" value="$1,200.00" caption="08/01–08/14, this period" />)

    const caption = screen.getByText('08/01–08/14, this period')
    expect(caption.className).not.toContain('truncate')
    expect(caption.className).toContain('line-clamp-2')
  })

  it('still renders the value and an unavailable state correctly', () => {
    const { rerender } = render(<Stat label="Latest margin" value="$402.50" />)
    expect(screen.getByText('$402.50')).toBeInTheDocument()

    rerender(<Stat label="Latest margin" value={null} unavailableLabel="No data yet" />)
    expect(screen.getByText('No data yet')).toBeInTheDocument()
  })
})
