import { fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import PromoRoiChart, { DEFAULT_PROMOTION_ROI } from './PromoRoiChart'

describe('PromoRoiChart', () => {
  it('renders one bar target per campaign', () => {
    render(<PromoRoiChart />)

    const bars = screen.getAllByRole('button', {
      name: /: (net |unattributable)/i,
    })
    expect(bars).toHaveLength(DEFAULT_PROMOTION_ROI.length)
  })

  it('fills a positive-net campaign with the success token and a negative one with destructive', () => {
    render(<PromoRoiChart />)

    const positiveBar = screen.getByRole('button', {
      name: /In-App Boost.*net \+\$34\.00/,
    })
    expect(positiveBar.querySelector('path')).toHaveAttribute(
      'fill',
      'var(--success)',
    )

    const negativeBar = screen.getByRole('button', {
      name: /Banner Ad.*net −\$165\.00/,
    })
    expect(negativeBar.querySelector('path')).toHaveAttribute(
      'fill',
      'var(--destructive)',
    )
  })

  it('renders the unattributable campaign as an explicit refusal state, never a bar', () => {
    render(<PromoRoiChart />)

    const refused = screen.getByRole('button', {
      name: /Featured Placement.*unattributable, ROI refused/i,
    })
    expect(refused.querySelector('path')).not.toBeInTheDocument()
    expect(screen.getAllByText('Unattributable').length).toBeGreaterThan(0)
  })

  it('labels every bar directly with a signed dollar amount — mandatory at 4 categories', () => {
    render(<PromoRoiChart />)

    expect(screen.getByText('+$34.00')).toBeInTheDocument()
    expect(screen.getByText('−$165.00')).toBeInTheDocument()
    expect(screen.getByText('+$19.50')).toBeInTheDocument()
  })

  it('shows a tooltip naming the FR-013 refusal explicitly on hover', () => {
    render(<PromoRoiChart />)

    const refused = screen.getByRole('button', {
      name: /Featured Placement.*unattributable/i,
    })
    fireEvent.mouseEnter(refused)

    const tooltip = screen.getByRole('status')
    expect(tooltip).toHaveTextContent('Unattributable — refusing to estimate (FR-013)')
    expect(tooltip).toHaveTextContent('promotion_ad_spend_export.csv')
  })

  it('cites the ad-spend export (not "no source") for the refused campaign, proving the refusal is about attribution', () => {
    render(<PromoRoiChart />)

    const provenanceList = screen.getByRole('list', {
      name: /per-campaign provenance/i,
    })
    const weekendItem = within(provenanceList)
      .getAllByRole('listitem')
      .find((item) => item.textContent?.includes('IFOOD-CAMP-WEEKEND'))

    expect(weekendItem).toBeDefined()
    expect(
      within(weekendItem as HTMLElement).getByRole('button', {
        name: /promotion_ad_spend_export\.csv/,
      }),
    ).toBeInTheDocument()
  })

  it('carries a three-state text-labeled legend', () => {
    render(<PromoRoiChart />)

    const legend = screen.getByRole('list', { name: /chart legend/i })
    expect(within(legend).getByText('Positive ROI')).toBeInTheDocument()
    expect(within(legend).getByText('Negative ROI')).toBeInTheDocument()
    expect(
      within(legend).getByText('Unattributable — refused'),
    ).toBeInTheDocument()
  })

  it('exposes a table view with the refusal spelled out, not a null or zero', async () => {
    const user = userEvent.setup()
    render(<PromoRoiChart />)

    await user.click(screen.getByRole('button', { name: /view as table/i }))

    const table = screen.getByRole('table')
    const rows = within(table).getAllByRole('row')
    expect(rows).toHaveLength(DEFAULT_PROMOTION_ROI.length + 1)
    expect(table).toHaveTextContent('Refused — cannot attribute (FR-013)')
    expect(table).not.toHaveTextContent('$0.00')
  })
})
