import { fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import MarginTrendChart, {
  DEFAULT_DAILY_MARGIN,
  MISSING_MARGIN_REASON,
  type DailyMarginDatum,
} from './MarginTrendChart'

/** A synthetic 2-year-scale period (744 days), the same order of magnitude
 *  as the real `daily_reconciliation` table this chart now has to survive. */
function buildLongPeriod(days: number): DailyMarginDatum[] {
  return Array.from({ length: days }, (_, i) => {
    const date = new Date(Date.UTC(2026, 7, 15))
    date.setUTCDate(date.getUTCDate() + i)
    return { date: date.toISOString().slice(0, 10), margin: 100 + i }
  })
}

describe('MarginTrendChart', () => {
  it('renders one bar target per day, including the missing day', () => {
    render(<MarginTrendChart />)

    const bars = screen.getAllByRole('button', { name: /^Aug \d+:/ })
    expect(bars).toHaveLength(DEFAULT_DAILY_MARGIN.length)
  })

  it('fills a profit day with the success token and a loss day with the destructive token', () => {
    render(<MarginTrendChart />)

    const profitBar = screen.getByRole('button', { name: /Aug 1:.*\+\$43\.26/ })
    expect(profitBar.querySelector('path')).toHaveAttribute(
      'fill',
      'var(--success)',
    )

    const lossBar = screen.getByRole('button', { name: /Aug 2:.*−\$227\.09/ })
    expect(lossBar.querySelector('path')).toHaveAttribute(
      'fill',
      'var(--destructive)',
    )
  })

  it('renders 2026-08-08 as an explicit missing-data placeholder, never a zero-value bar', () => {
    render(<MarginTrendChart />)

    const missingBar = screen.getByRole('button', { name: /Aug 8: no data/i })
    expect(missingBar.querySelector(':scope > path')).not.toBeInTheDocument()

    const hatchRect = missingBar.querySelector('rect[fill^="url("]')
    expect(hatchRect).toBeInTheDocument()
    expect(within(missingBar).getByText('No data')).toBeInTheDocument()
  })

  it('direct-labels only the two extreme days, with an explicit sign', () => {
    render(<MarginTrendChart />)

    expect(screen.getByText('+$375.82')).toBeInTheDocument()
    expect(screen.getByText('−$227.09')).toBeInTheDocument()
    // A mid-range day's exact value is not printed directly on the chart.
    expect(screen.queryByText('+$43.26')).not.toBeInTheDocument()
  })

  it('shows a tooltip with date, exact margin, and a provenance hint on hover', () => {
    render(<MarginTrendChart />)

    const profitBar = screen.getByRole('button', { name: /Aug 7:/ })
    fireEvent.mouseEnter(profitBar)

    const tooltip = screen.getByRole('status')
    expect(tooltip).toHaveTextContent('Aug 7')
    expect(tooltip).toHaveTextContent('+$375.82')
    expect(tooltip).toHaveTextContent('daily_reconciliation.csv')

    fireEvent.mouseLeave(profitBar)
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('shows the same tooltip detail on keyboard focus as on hover', () => {
    render(<MarginTrendChart />)

    const missingBar = screen.getByRole('button', { name: /Aug 8: no data/i })
    fireEvent.focus(missingBar)

    const tooltip = screen.getByRole('status')
    expect(tooltip).toHaveTextContent(MISSING_MARGIN_REASON.toLowerCase())

    fireEvent.blur(missingBar)
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('carries a text-labeled legend, not color-only swatches', () => {
    render(<MarginTrendChart />)

    const legend = screen.getByRole('list', { name: /chart legend/i })
    expect(within(legend).getByText('Profit day')).toBeInTheDocument()
    expect(within(legend).getByText('Loss day')).toBeInTheDocument()
    expect(within(legend).getByText('No data')).toBeInTheDocument()
  })

  it('renders a ProvenanceTag citation for the underlying source rows', () => {
    render(<MarginTrendChart />)

    expect(
      screen.getByRole('button', { name: /daily_reconciliation\.csv/ }),
    ).toBeInTheDocument()
  })

  it('buckets a 744-day period into readable groups instead of plotting 744 overlapping bars', () => {
    const longPeriod = buildLongPeriod(744)
    const { container } = render(<MarginTrendChart data={longPeriod} />)

    // Never one bar per day at this scale — that was the live "the chart
    // doesn't make sense" bug: 744 bars at a fixed 24px width overlap into
    // one unreadable block.
    const bars = screen.getAllByRole('button', { name: /: [+−]\$/ })
    expect(bars.length).toBeLessThan(744)
    expect(bars.length).toBeGreaterThan(1)

    // The canvas grows to give each remaining bar real room rather than
    // squeezing all 744 into the original 700px design width.
    const svg = container.querySelector('svg')
    const viewBoxWidth = Number(svg?.getAttribute('viewBox')?.split(' ')[2])
    expect(viewBoxWidth).toBeGreaterThan(700)

    // The heading still honestly reports the full 744-day span plotted.
    expect(screen.getByText('744-Day Margin Trend')).toBeInTheDocument()
    // ...and the table underneath still has one real row per day, so no
    // day's detail is lost to the aggregation.
    fireEvent.click(screen.getByRole('button', { name: /view as table/i }))
    const table = screen.getByRole('table')
    expect(within(table).getAllByRole('row')).toHaveLength(745)
  })

  it('renders exactly as before for a period under the bucketing threshold (no behavior change at normal scale)', () => {
    render(<MarginTrendChart data={buildLongPeriod(30)} />)

    // One bar per day, same as the 14-day default — 30 days is still well
    // under the threshold where aggregation kicks in.
    const bars = screen.getAllByRole('button', { name: /: [+−]\$/ })
    expect(bars).toHaveLength(30)
  })

  it('exposes a table view with every day, including the missing-data reason', async () => {
    const user = userEvent.setup()
    render(<MarginTrendChart />)

    expect(screen.queryByRole('table')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /view as table/i }))

    const table = screen.getByRole('table')
    const rows = within(table).getAllByRole('row')
    // header + 14 days
    expect(rows).toHaveLength(DEFAULT_DAILY_MARGIN.length + 1)
    expect(table).toHaveTextContent(MISSING_MARGIN_REASON)
    expect(table).toHaveTextContent('+$375.82')
    expect(table).toHaveTextContent('−$227.09')
  })

  // Spec 008 FR-001 — chart click-to-ask.

  it('calls onDataPointClick with the real, unbucketed date when a bar is clicked', async () => {
    const user = userEvent.setup()
    const onDataPointClick = vi.fn()
    render(<MarginTrendChart onDataPointClick={onDataPointClick} />)

    await user.click(screen.getByRole('button', { name: /Aug 7:.*\+\$375\.82/ }))

    expect(onDataPointClick).toHaveBeenCalledTimes(1)
    expect(onDataPointClick).toHaveBeenCalledWith({
      date: '2026-08-07',
      rangeEndDate: '2026-08-07',
    })
  })

  it('calls onDataPointClick with the bucketed date range when a bucketed bar is clicked', async () => {
    const user = userEvent.setup()
    const onDataPointClick = vi.fn()
    render(
      <MarginTrendChart
        data={buildLongPeriod(744)}
        onDataPointClick={onDataPointClick}
      />,
    )

    const bars = screen.getAllByRole('button', { name: /: [+−]\$/ })
    await user.click(bars[0])

    expect(onDataPointClick).toHaveBeenCalledTimes(1)
    const point = onDataPointClick.mock.calls[0][0]
    expect(point.date).not.toBe(point.rangeEndDate)
  })

  it('activates onDataPointClick via keyboard (Enter), matching the bar\'s own role="button"', async () => {
    const user = userEvent.setup()
    const onDataPointClick = vi.fn()
    render(<MarginTrendChart onDataPointClick={onDataPointClick} />)

    const bar = screen.getByRole('button', { name: /Aug 7:.*\+\$375\.82/ })
    bar.focus()
    await user.keyboard('{Enter}')

    expect(onDataPointClick).toHaveBeenCalledTimes(1)
  })

  it('renders no click affordance in the accessible name when onDataPointClick is not provided', () => {
    render(<MarginTrendChart />)

    const bar = screen.getByRole('button', { name: /Aug 7:.*\+\$375\.82/ })
    expect(bar).not.toHaveAccessibleName(/ask about this/i)
  })
})
