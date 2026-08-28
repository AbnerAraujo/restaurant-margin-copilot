import { fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import MarginTrendChart, {
  DEFAULT_DAILY_MARGIN,
  MISSING_MARGIN_REASON,
} from './MarginTrendChart'

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
})
