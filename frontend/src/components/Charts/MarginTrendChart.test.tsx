import { fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import MarginTrendChart, {
  DEFAULT_DAILY_MARGIN,
  MISSING_MARGIN_REASON,
  type DailyMarginDatum,
} from './MarginTrendChart'

/** A synthetic multi-year period (759 days matches the real dataset's own
 *  scale), the same order of magnitude as the real `daily_reconciliation`
 *  table this chart has to survive. */
function buildLongPeriod(days: number): DailyMarginDatum[] {
  return Array.from({ length: days }, (_, i) => {
    const date = new Date(Date.UTC(2024, 7, 15))
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
    // Explicit data: the default sample (the dataset's own opening window)
    // has no loss day, so the destructive-token path needs its own datum.
    render(
      <MarginTrendChart
        data={[
          { date: '2024-08-01', margin: 500 },
          { date: '2024-08-02', margin: -250 },
        ]}
      />,
    )

    const profitBar = screen.getByRole('button', { name: /Aug 1:.*\+\$500\.00/ })
    expect(profitBar.querySelector('path')).toHaveAttribute(
      'fill',
      'var(--success)',
    )

    const lossBar = screen.getByRole('button', { name: /Aug 2:.*−\$250\.00/ })
    expect(lossBar.querySelector('path')).toHaveAttribute(
      'fill',
      'var(--destructive)',
    )
  })

  it('renders a day with no reconciliation row as an explicit missing-data placeholder, never a zero-value bar', () => {
    // Explicit data: every calendar day of the real dataset has a
    // reconciliation row, so the no-row case is constructed here (it still
    // happens live for a gap inside a requested period, e.g. after a
    // partial upload).
    render(
      <MarginTrendChart
        data={[
          { date: '2024-08-07', margin: 659 },
          { date: '2024-08-08', margin: null },
          { date: '2024-08-09', margin: 831.65 },
        ]}
      />,
    )

    const missingBar = screen.getByRole('button', { name: /Aug 8: no data/i })
    expect(missingBar.querySelector(':scope > path')).not.toBeInTheDocument()

    const hatchRect = missingBar.querySelector('rect[fill^="url("]')
    expect(hatchRect).toBeInTheDocument()
    expect(within(missingBar).getByText('No data')).toBeInTheDocument()
  })

  it('direct-labels only the two extreme days, with an explicit sign', () => {
    render(<MarginTrendChart />)

    expect(screen.getByText('+$1,019.45')).toBeInTheDocument()
    expect(screen.getByText('+$331.52')).toBeInTheDocument()
    // A mid-range day's exact value is not printed directly on the chart.
    expect(screen.queryByText('+$701.90')).not.toBeInTheDocument()
  })

  it('shows a tooltip with date, exact margin, and a provenance hint on hover', () => {
    render(<MarginTrendChart />)

    const profitBar = screen.getByRole('button', { name: /Aug 7:/ })
    fireEvent.mouseEnter(profitBar)

    const tooltip = screen.getByRole('status')
    expect(tooltip).toHaveTextContent('Aug 7')
    expect(tooltip).toHaveTextContent('+$659.00')
    expect(tooltip).toHaveTextContent('daily_reconciliation.csv')

    fireEvent.mouseLeave(profitBar)
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('shows the same tooltip detail on keyboard focus as on hover', () => {
    render(
      <MarginTrendChart
        data={[
          { date: '2024-08-07', margin: 659 },
          { date: '2024-08-08', margin: null },
        ]}
      />,
    )

    const missingBar = screen.getByRole('button', { name: /Aug 8: no data/i })
    fireEvent.focus(missingBar)

    const tooltip = screen.getByRole('status')
    expect(tooltip).toHaveTextContent(MISSING_MARGIN_REASON.toLowerCase())

    fireEvent.blur(missingBar)
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('carries a text-labeled legend, not color-only swatches', () => {
    // Explicit data with a missing day so the conditional "No data" legend
    // entry renders too (the default sample has no missing day).
    render(
      <MarginTrendChart
        data={[
          { date: '2024-08-07', margin: 659 },
          { date: '2024-08-08', margin: null },
        ]}
      />,
    )

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

  it('buckets a 759-day period into readable groups instead of plotting 759 overlapping bars', () => {
    const longPeriod = buildLongPeriod(759)
    const { container } = render(<MarginTrendChart data={longPeriod} />)

    // Never one bar per day at this scale — that was the live "the chart
    // doesn't make sense" bug: 759 bars at a fixed 24px width overlap into
    // one unreadable block.
    const bars = screen.getAllByRole('button', { name: /: [+−]\$/ })
    expect(bars.length).toBeLessThan(759)
    expect(bars.length).toBeGreaterThan(1)

    // The canvas grows to give each remaining bar real room rather than
    // squeezing all 759 into the original 700px design width.
    const svg = container.querySelector('svg')
    const viewBoxWidth = Number(svg?.getAttribute('viewBox')?.split(' ')[2])
    expect(viewBoxWidth).toBeGreaterThan(700)

    // The heading still honestly reports the full 759-day span plotted.
    expect(screen.getByText('759-Day Margin Trend')).toBeInTheDocument()
    // ...and the table underneath still has one real row per day, so no
    // day's detail is lost to the aggregation.
    fireEvent.click(screen.getByRole('button', { name: /view as table/i }))
    const table = screen.getByRole('table')
    expect(within(table).getAllByRole('row')).toHaveLength(760)
  })

  // Reported live: the month/year label above the plot used to show only
  // the LAST date's month ("Aug 2026") even when the chart spanned a full
  // year or more — reading as if the whole multi-year chart were that one
  // month, since the per-tick day-of-month labels carry no month of their
  // own to disambiguate at this scale.
  it('shows an honest month/year range above the plot when the period spans more than one month, not just the last date\'s month', () => {
    const longPeriod = buildLongPeriod(759) // 2024-08-15 -> spans ~2 years
    const { container } = render(<MarginTrendChart data={longPeriod} />)

    const monthLabel = [...container.querySelectorAll('text')].find((el) =>
      /^[A-Z][a-z]{2} \d{4}/.test(el.textContent ?? ''),
    )
    expect(monthLabel).toBeDefined()
    // A real range, not a single trailing month — proves the fix reports
    // the full span rather than just where the data happens to end.
    expect(monthLabel?.textContent).toContain('Aug 2024')
    expect(monthLabel?.textContent).toMatch(/–/)
    expect(monthLabel?.textContent).not.toBe('Aug 2024')
  })

  it('collapses the month/year label to a single month when the period genuinely stays within one (unchanged from before)', () => {
    const { container } = render(<MarginTrendChart />) // DEFAULT_DAILY_MARGIN: a single-month 14-day sample

    const monthLabel = [...container.querySelectorAll('text')].find((el) =>
      /^[A-Z][a-z]{2} \d{4}$/.test(el.textContent ?? ''),
    )
    expect(monthLabel?.textContent).toBe('Aug 2024')
  })

  it('renders exactly as before for a period under the bucketing threshold (no behavior change at normal scale)', () => {
    render(<MarginTrendChart data={buildLongPeriod(30)} />)

    // One bar per day, same as the 14-day default — 30 days is still well
    // under the threshold where aggregation kicks in.
    const bars = screen.getAllByRole('button', { name: /: [+−]\$/ })
    expect(bars).toHaveLength(30)
  })

  it('exposes a table view with every day of the default sample', async () => {
    const user = userEvent.setup()
    render(<MarginTrendChart />)

    expect(screen.queryByRole('table')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /view as table/i }))

    const table = screen.getByRole('table')
    const rows = within(table).getAllByRole('row')
    // header + 14 days
    expect(rows).toHaveLength(DEFAULT_DAILY_MARGIN.length + 1)
    expect(table).toHaveTextContent('+$1,019.45')
    expect(table).toHaveTextContent('+$331.52')
  })

  it('prints the missing-data reason in the table for a day with no reconciliation row', async () => {
    const user = userEvent.setup()
    render(
      <MarginTrendChart
        data={[
          { date: '2024-08-07', margin: 659 },
          { date: '2024-08-08', margin: null },
        ]}
      />,
    )

    await user.click(screen.getByRole('button', { name: /view as table/i }))
    expect(screen.getByRole('table')).toHaveTextContent(MISSING_MARGIN_REASON)
  })

  // Spec 008 FR-001 — chart click-to-ask.

  it('calls onDataPointClick with the real, unbucketed date when a bar is clicked', async () => {
    const user = userEvent.setup()
    const onDataPointClick = vi.fn()
    render(<MarginTrendChart onDataPointClick={onDataPointClick} />)

    await user.click(screen.getByRole('button', { name: /Aug 7:.*\+\$659\.00/ }))

    expect(onDataPointClick).toHaveBeenCalledTimes(1)
    expect(onDataPointClick).toHaveBeenCalledWith({
      date: '2024-08-07',
      rangeEndDate: '2024-08-07',
    })
  })

  it('calls onDataPointClick with the bucketed date range when a bucketed bar is clicked', async () => {
    const user = userEvent.setup()
    const onDataPointClick = vi.fn()
    render(
      <MarginTrendChart
        data={buildLongPeriod(759)}
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

    const bar = screen.getByRole('button', { name: /Aug 7:.*\+\$659\.00/ })
    bar.focus()
    await user.keyboard('{Enter}')

    expect(onDataPointClick).toHaveBeenCalledTimes(1)
  })

  it('renders no click affordance in the accessible name when onDataPointClick is not provided', () => {
    render(<MarginTrendChart />)

    const bar = screen.getByRole('button', { name: /Aug 7:.*\+\$659\.00/ })
    expect(bar).not.toHaveAccessibleName(/ask about this/i)
  })

  // Reported live: the chart's overflow-x-auto wrapper defaulted to showing
  // its LEFT edge (the oldest history) on mount — the opposite of the
  // natural mental model for "Today's Close" (care about now first, dig
  // into history on purpose).
  describe('mounts scrolled to the right edge (today), not the oldest history', () => {
    it('sets the scroll container\'s scrollLeft to its full scrollWidth after render', () => {
      const scrollWidthSpy = vi
        .spyOn(HTMLElement.prototype, 'scrollWidth', 'get')
        .mockReturnValue(2000)

      const { container } = render(
        <MarginTrendChart data={buildLongPeriod(759)} />,
      )

      const scrollContainer = container.querySelector(
        '.overflow-x-auto',
      ) as HTMLDivElement
      expect(scrollContainer.scrollLeft).toBe(2000)

      scrollWidthSpy.mockRestore()
    })

    it('does not fight a reader\'s manual scroll on a re-render of the SAME period (only a new array reference)', () => {
      const scrollWidthSpy = vi
        .spyOn(HTMLElement.prototype, 'scrollWidth', 'get')
        .mockReturnValue(2000)

      const period = buildLongPeriod(759)
      const { container, rerender } = render(<MarginTrendChart data={period} />)
      const scrollContainer = container.querySelector(
        '.overflow-x-auto',
      ) as HTMLDivElement
      expect(scrollContainer.scrollLeft).toBe(2000)

      // The reader scrolls back to look at older history.
      scrollContainer.scrollLeft = 0

      // A fresh array with the exact same dates/values — e.g. a parent
      // re-rendering for an unrelated reason and recomputing the same
      // period. This must NOT yank the reader back to the right.
      rerender(<MarginTrendChart data={[...period]} />)
      expect(scrollContainer.scrollLeft).toBe(0)

      scrollWidthSpy.mockRestore()
    })

    it('DOES re-scroll to the right when a genuinely new period loads', () => {
      const scrollWidthSpy = vi
        .spyOn(HTMLElement.prototype, 'scrollWidth', 'get')
        .mockReturnValue(2000)

      const { container, rerender } = render(
        <MarginTrendChart data={buildLongPeriod(759)} />,
      )
      const scrollContainer = container.querySelector(
        '.overflow-x-auto',
      ) as HTMLDivElement
      scrollContainer.scrollLeft = 0

      rerender(<MarginTrendChart data={DEFAULT_DAILY_MARGIN} />)
      expect(scrollContainer.scrollLeft).toBe(2000)

      scrollWidthSpy.mockRestore()
    })
  })
})
