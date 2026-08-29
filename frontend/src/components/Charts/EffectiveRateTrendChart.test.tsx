import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import EffectiveRateTrendChart, { type EffectiveRateTrendPeriod } from './EffectiveRateTrendChart'

function period(
  month: string,
  ifoodRate: string | null,
  jetRate: string | null,
): EffectiveRateTrendPeriod {
  return {
    month,
    platforms: [
      { source: 'ifood', display_name: 'iFood', effective_rate: ifoodRate },
      { source: 'just_eat_takeaway', display_name: 'Just Eat Takeaway', effective_rate: jetRate },
    ],
  }
}

describe('EffectiveRateTrendChart', () => {
  it('renders nothing with fewer than 2 periods — a single point has no trend to show (FR-013)', () => {
    const { container } = render(
      <EffectiveRateTrendChart periods={[period('2026-08', '22.00%', '20.00%')]} />,
    )
    expect(container).toBeEmptyDOMElement()
  })

  it('renders nothing with zero periods', () => {
    const { container } = render(<EffectiveRateTrendChart periods={[]} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('renders a real chart and legend with 2 or more periods', () => {
    render(
      <EffectiveRateTrendChart
        periods={[period('2026-07', '21.00%', '19.00%'), period('2026-08', '22.00%', '20.00%')]}
      />,
    )
    expect(screen.getByRole('img', { name: /effective commission rate trend/i })).toBeInTheDocument()
    expect(screen.getByText('iFood')).toBeInTheDocument()
    expect(screen.getByText('Just Eat Takeaway')).toBeInTheDocument()
  })

  it('shows the real per-point rate as an accessible title, never a fabricated value for a null-rate month', () => {
    const { container } = render(
      <EffectiveRateTrendChart
        periods={[
          period('2026-07', '21.00%', '19.00%'),
          period('2026-08', null, '20.00%'), // iFood had zero sales this month
        ]}
      />,
    )
    const titles = Array.from(container.querySelectorAll('title')).map((t) => t.textContent)
    // Two real iFood points (July only, since August is null and skipped)
    // plus two real Just Eat Takeaway points — never a fabricated "0.00%"
    // for iFood's August gap.
    expect(titles.some((t) => t?.includes('iFood — Jul 26: 21.00%'))).toBe(true)
    expect(titles.some((t) => t?.startsWith('iFood') && t.includes('Aug'))).toBe(false)
    expect(titles.filter((t) => t?.startsWith('iFood'))).toHaveLength(1)
    expect(titles.filter((t) => t?.startsWith('Just Eat Takeaway'))).toHaveLength(2)
  })
})
