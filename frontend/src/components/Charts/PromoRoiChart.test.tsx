import { fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import PromoRoiChart, {
  DEFAULT_PROMOTION_ROI,
  type PromotionRoiDatum,
} from './PromoRoiChart'

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

  it('cites the ad-spend export (not "no source") for the refused campaign in the sources table column, proving the refusal is about attribution', async () => {
    render(<PromoRoiChart />)

    await userEvent.click(
      screen.getByRole('button', { name: /view as table/i }),
    )

    const table = screen.getByRole('table')
    const weekendRow = within(table)
      .getByText('IFOOD-CAMP-WEEKEND')
      .closest('tr')
    expect(weekendRow).not.toBeNull()
    expect(
      within(weekendRow as HTMLElement).getByRole('button', {
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

  it('excludes a not_yet_attributed campaign from the bars, unlike attribution_unavailable', async () => {
    const freshlyLogged: PromotionRoiDatum = {
      campaignId: 'OWNER-CAMP-FRESH',
      campaignName: 'OWNER-CAMP-FRESH',
      platform: 'iFood',
      spend: 40.0,
      attributedIncrementalRevenue: null,
      net: null,
      reason: 'not_yet_attributed',
      sourceRefs: [
        {
          source_file: 'promotion_ad_spend_export.csv',
          row_start: 9,
          row_end: 9,
          period_start: '2026-08-20',
          period_end: '2026-08-20',
        },
      ],
    }
    const user = userEvent.setup()
    render(<PromoRoiChart data={[...DEFAULT_PROMOTION_ROI, freshlyLogged]} />)

    // Not plotted at all — no bar target, no refusal box, nothing to hover.
    expect(
      screen.queryByRole('button', { name: /OWNER-CAMP-FRESH/i }),
    ).not.toBeInTheDocument()
    // The genuine FR-013 refusal is untouched by the filter.
    expect(
      screen.getByRole('button', {
        name: /Featured Placement.*unattributable, ROI refused/i,
      }),
    ).toBeInTheDocument()
    // Bar count stays at the original four — the fifth, deferred campaign
    // never reaches buildBars.
    const bars = screen.getAllByRole('button', {
      name: /: (net |unattributable)/i,
    })
    expect(bars).toHaveLength(DEFAULT_PROMOTION_ROI.length)

    // Still present in the table underneath — every logged campaign belongs
    // there, including one with nothing plottable yet.
    await user.click(screen.getByRole('button', { name: /view as table/i }))
    const table = screen.getByRole('table')
    expect(within(table).getByText('OWNER-CAMP-FRESH')).toBeInTheDocument()
    const rows = within(table).getAllByRole('row')
    expect(rows).toHaveLength(DEFAULT_PROMOTION_ROI.length + 1 + 1)
  })

  it('stops labeling every bar past the small-count threshold, and grows the canvas instead of squeezing bars', () => {
    const manyCampaigns: PromotionRoiDatum[] = Array.from(
      { length: 29 },
      (_, i) => ({
        campaignId: `CAMP-${i}`,
        campaignName: `CAMP-${i}`,
        platform: 'iFood',
        spend: 100,
        attributedIncrementalRevenue: 100 + i,
        net: i - 14, // spans negative to positive, so both extremes exist
        sourceRefs: [],
      }),
    )

    const { container } = render(<PromoRoiChart data={manyCampaigns} />)

    // Every campaign still gets a focusable bar target...
    const bars = screen.getAllByRole('button', { name: /: net /i })
    expect(bars).toHaveLength(29)

    // ...but the chart no longer prints a campaign id under every one of
    // them (the "chart is on the left"/illegible-smear bug at this count) —
    // only the two extreme bars get a direct VALUE label...
    expect(screen.getByText('+$14.00')).toBeInTheDocument() // net = 28-14
    expect(screen.getByText('−$14.00')).toBeInTheDocument() // net = 0-14

    // ...while the x-AXIS itself still reads as an axis: evenly-spaced
    // campaign-id ticks (tickLabelStep, same discipline as
    // MarginTrendChart's own x-axis at scale), not just the 2 ROI extremes.
    // 29 campaigns, MAX_AXIS_TICKS=8 -> step ceil(29/8)=4 -> indices
    // 0,4,8,...,28 are labeled; the ones in between are not.
    expect(screen.getByText('CAMP-0')).toBeInTheDocument()
    expect(screen.getByText('CAMP-4')).toBeInTheDocument()
    expect(screen.getByText('CAMP-28')).toBeInTheDocument()
    expect(screen.queryByText('CAMP-1')).not.toBeInTheDocument()
    expect(screen.queryByText('CAMP-2')).not.toBeInTheDocument()
    expect(screen.queryByText('CAMP-3')).not.toBeInTheDocument()

    // The SVG's own design width grows with campaign count rather than
    // staying pinned to the 4-campaign fixture's 560px, which is what left
    // a fixed-width chart flush against the left edge with dead space
    // beside it once there was real data to fill that space with.
    const svg = container.querySelector('svg')
    const viewBoxWidth = Number(svg?.getAttribute('viewBox')?.split(' ')[2])
    expect(viewBoxWidth).toBeGreaterThan(560)
  })

  it('shrinks the refusal marker to fit its own slot at real scale, dropping the text label rather than spilling into neighboring bars', () => {
    // Reported live: at 29 campaigns (MIN_SLOT_WIDTH=28px), the refusal
    // marker's original fixed 64px box was wider than 2 slots and visually
    // overlapped whichever campaigns sat on either side of a refused one.
    const manyCampaigns: PromotionRoiDatum[] = Array.from(
      { length: 29 },
      (_, i) => ({
        campaignId: `CAMP-${i}`,
        campaignName: `CAMP-${i}`,
        platform: 'iFood',
        spend: 100,
        attributedIncrementalRevenue: i === 15 ? null : 100 + i,
        net: i === 15 ? null : i - 14,
        reason: i === 15 ? 'attribution_unavailable' : undefined,
        sourceRefs: [],
      }),
    )

    render(<PromoRoiChart data={manyCampaigns} />)

    // The refused bar is still a real, focusable target with its full
    // refusal state named in its accessible name...
    expect(
      screen.getByRole('button', { name: /CAMP-15.*unattributable, ROI refused/i }),
    ).toBeInTheDocument()
    // ...but past LABEL_ALL_MAX the box drops its visible text label (kept
    // only in the aria-label and hover tooltip) so it can shrink to fit its
    // own slot instead of spilling into CAMP-14's or CAMP-16's.
    expect(screen.queryByText('Unattributable')).not.toBeInTheDocument()
  })

  it('names the x-axis explicitly ("Campaigns"), the same way the y-axis names itself ("Net (USD)")', () => {
    render(<PromoRoiChart />)

    expect(screen.getByText('Campaigns')).toBeInTheDocument()
    expect(screen.getByText('Net (USD)')).toBeInTheDocument()
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

  // Spec 008 FR-001 — chart click-to-ask.

  it('calls onDataPointClick with the real campaign id and name when a bar is clicked', async () => {
    const user = userEvent.setup()
    const onDataPointClick = vi.fn()
    render(<PromoRoiChart onDataPointClick={onDataPointClick} />)

    await user.click(
      screen.getByRole('button', { name: /In-App Boost.*net \+\$34\.00/ }),
    )

    expect(onDataPointClick).toHaveBeenCalledTimes(1)
    expect(onDataPointClick).toHaveBeenCalledWith({
      campaignId: 'IFOOD-CAMP-BOOST01',
      campaignName: 'In-App Boost — Weekday Lunch',
    })
  })

  it('activates onDataPointClick via keyboard (Space), matching the bar\'s own role="button"', async () => {
    const user = userEvent.setup()
    const onDataPointClick = vi.fn()
    render(<PromoRoiChart onDataPointClick={onDataPointClick} />)

    const bar = screen.getByRole('button', {
      name: /In-App Boost.*net \+\$34\.00/,
    })
    bar.focus()
    await user.keyboard(' ')

    expect(onDataPointClick).toHaveBeenCalledTimes(1)
  })

  it('renders no click affordance in the accessible name when onDataPointClick is not provided', () => {
    render(<PromoRoiChart />)

    const bar = screen.getByRole('button', {
      name: /In-App Boost.*net \+\$34\.00/,
    })
    expect(bar).not.toHaveAccessibleName(/ask about this/i)
  })
})
