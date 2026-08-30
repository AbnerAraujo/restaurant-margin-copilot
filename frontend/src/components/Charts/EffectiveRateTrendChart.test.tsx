import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
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
    // role="group", not role="img": role="img" forbids focusable
    // descendants, which would make the per-point role="button" markers
    // below unreachable to assistive tech (the same fix CategoryBarChart
    // and PromoRoiChart already apply for this exact reason).
    expect(screen.getByRole('group', { name: /effective commission rate trend/i })).toBeInTheDocument()
    expect(screen.getByText('iFood')).toBeInTheDocument()
    expect(screen.getByText('Just Eat Takeaway')).toBeInTheDocument()
  })

  it('exposes every point as a focusable, labeled control reachable without a mouse', () => {
    render(
      <EffectiveRateTrendChart
        periods={[period('2026-07', '21.00%', '19.00%'), period('2026-08', '22.00%', '20.00%')]}
      />,
    )
    // 2 platforms x 2 months = 4 focusable points, each carrying the exact
    // value a sighted user gets from hovering the dot.
    expect(screen.getByRole('button', { name: 'iFood — Jul 26: 21.00%' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'iFood — Aug 26: 22.00%' })).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Just Eat Takeaway — Jul 26: 19.00%' }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Just Eat Takeaway — Aug 26: 20.00%' }),
    ).toBeInTheDocument()
  })

  it('offers a real table alternative with every month and rate a screen-reader user cannot get from the SVG alone', async () => {
    const user = userEvent.setup()
    render(
      <EffectiveRateTrendChart
        periods={[
          period('2026-07', '21.00%', '19.00%'),
          period('2026-08', null, '20.00%'), // iFood had zero sales this month
        ]}
      />,
    )

    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'View as table' }))

    const table = screen.getByRole('table')
    const rows = table.querySelectorAll('tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows[0]).toHaveTextContent('Jul 26')
    expect(rows[0]).toHaveTextContent('21.00%')
    expect(rows[0]).toHaveTextContent('19.00%')
    // A real, disclosed absence for the null-rate month — never a
    // fabricated "0.00%" standing in for "no sales that month."
    expect(rows[1]).toHaveTextContent('No sales')
    expect(rows[1]).toHaveTextContent('20.00%')

    await user.click(screen.getByRole('button', { name: 'Hide table' }))
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
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
