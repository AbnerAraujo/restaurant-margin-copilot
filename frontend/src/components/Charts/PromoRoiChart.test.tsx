import { act, fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import PromoRoiChart, {
  DEFAULT_PROMOTION_ROI,
  type PromotionRoiDatum,
} from './PromoRoiChart'

// jsdom has no ResizeObserver at all (see PromoRoiChart.tsx's own guard for
// why that's survivable, not just a test-env inconvenience). This mock is
// controllable rather than inert, unlike the plain do-nothing stubs
// HomePage.test.tsx/ChatPanel.test.tsx use elsewhere: the container-width
// fill behavior below can only be proven by actually firing a resize.
let resizeCallbacks: ResizeObserverCallback[] = []

class MockResizeObserver {
  #callback: ResizeObserverCallback
  constructor(callback: ResizeObserverCallback) {
    this.#callback = callback
    resizeCallbacks.push(this.#callback)
  }
  observe() {}
  unobserve() {}
  disconnect() {}
}

/**
 * The plot SVG's viewBox is cropped to start where the pinned value-axis
 * gutter ends (`MARGIN.left 0 plotWidth+right height`), so its own width is
 * the plot's, not the whole chart's. These two read the geometry back out in
 * the terms the assertions below are actually about.
 */
function plotLeftEdge(container: HTMLElement): number {
  const viewBox = container.querySelector('svg')?.getAttribute('viewBox') ?? ''
  return Number(viewBox.split(' ')[0])
}

function renderedChartWidth(container: HTMLElement): number {
  const viewBox = container.querySelector('svg')?.getAttribute('viewBox') ?? ''
  const [x, , width] = viewBox.split(' ').map(Number)
  return x + width
}

function triggerResize(width: number) {
  const entries = [
    { contentRect: { width } } as ResizeObserverEntry,
  ]
  act(() => {
    resizeCallbacks.forEach((callback) =>
      callback(entries, {} as ResizeObserver),
    )
  })
}

beforeEach(() => {
  resizeCallbacks = []
  globalThis.ResizeObserver =
    MockResizeObserver as unknown as typeof ResizeObserver
})

afterEach(() => {
  vi.unstubAllGlobals()
})

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

  it('gives a not_yet_attributed campaign the same no-bar marker as attribution_unavailable, so the chart never undercounts the table', async () => {
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

    // Plotted as its own no-bar marker — same treatment as the genuine
    // FR-013 refusal — worded as "not yet attributed", never as an active
    // refusal it never went through.
    const freshBar = screen.getByRole('button', {
      name: /OWNER-CAMP-FRESH: not yet attributed/i,
    })
    expect(freshBar).toBeInTheDocument()
    fireEvent.mouseEnter(freshBar)
    expect(screen.getByRole('status')).toHaveTextContent(
      'Not yet attributed — awaiting incremental-order data',
    )

    // The genuine FR-013 refusal is untouched and still worded as a refusal.
    expect(
      screen.getByRole('button', {
        name: /Featured Placement.*unattributable, ROI refused/i,
      }),
    ).toBeInTheDocument()

    // Bar count includes the fifth, deferred campaign — the chart's own
    // count/aria description must never undercount the table below it.
    const bars = screen.getAllByRole('button', {
      name: /: (net |unattributable|not yet attributed)/i,
    })
    expect(bars).toHaveLength(DEFAULT_PROMOTION_ROI.length + 1)
    expect(
      screen.getByRole('group', { name: new RegExp(`across ${DEFAULT_PROMOTION_ROI.length + 1} promotion campaigns`) }),
    ).toBeInTheDocument()

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
    // staying pinned to the 4-campaign sample's 560px, which is what left
    // a fixed-width chart flush against the left edge with dead space
    // beside it once there was real data to fill that space with.
    expect(renderedChartWidth(container)).toBeGreaterThan(560)
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

  it('never lets adjacent bars\' hover hit-targets overlap at real scale, so hovering one never reports its neighbor\'s data', () => {
    // Reported live: at 29 campaigns, each bar's hit-rect (24px bar +
    // 14px padding on each side = 52px) was wider than the 28px slot it
    // sat in, so it overlapped both neighbors and hovering near a slot
    // boundary could trigger the WRONG bar's tooltip.
    const manyCampaigns: PromotionRoiDatum[] = Array.from(
      { length: 29 },
      (_, i) => ({
        campaignId: `CAMP-${i}`,
        campaignName: `CAMP-${i}`,
        platform: 'iFood',
        spend: 100,
        attributedIncrementalRevenue: 100 + i,
        net: i - 14,
        sourceRefs: [],
      }),
    )

    render(<PromoRoiChart data={manyCampaigns} />)

    // The first <rect> inside each bar's <g role="button"> is the
    // invisible hit-target (the visible bar is drawn after it).
    const bars = screen.getAllByRole('button', { name: /: net /i })
    const hitRects = bars.map((bar) => bar.querySelector('rect'))
    expect(hitRects.every((rect) => rect !== null)).toBe(true)

    for (let i = 0; i < hitRects.length - 1; i++) {
      const current = hitRects[i] as SVGRectElement
      const next = hitRects[i + 1] as SVGRectElement
      const currentRight =
        Number(current.getAttribute('x')) + Number(current.getAttribute('width'))
      const nextLeft = Number(next.getAttribute('x'))
      // Adjacent hit-rects may touch (share an edge) but must never overlap.
      expect(currentRight).toBeLessThanOrEqual(nextLeft + 0.01)
    }
  })

  it('grows to fill the real container width when the data needs less room than is available, instead of leaving dead space', () => {
    // Reported live: with few enough campaigns that the data-driven width
    // (well under CHART_WIDTH for the 4-campaign sample) stayed under the
    // panel's real available width, the chart rendered at its minimum
    // computed size and left visible dead space to the right of it.
    const { container } = render(<PromoRoiChart />)

    act(() => triggerResize(1200))

    expect(renderedChartWidth(container)).toBe(1200)
  })

  it('keeps the existing scroll-on-overflow behavior when the data needs MORE room than the container has', () => {
    // The container-fill fix must never shrink a chart that genuinely needs
    // to be wider than its container — that case still scrolls, unchanged.
    const manyCampaigns: PromotionRoiDatum[] = Array.from(
      { length: 29 },
      (_, i) => ({
        campaignId: `CAMP-${i}`,
        campaignName: `CAMP-${i}`,
        platform: 'iFood',
        spend: 100,
        attributedIncrementalRevenue: 100 + i,
        net: i - 14,
        sourceRefs: [],
      }),
    )

    const { container } = render(<PromoRoiChart data={manyCampaigns} />)
    const dataWidth = renderedChartWidth(container)

    // A container narrower than what 29 campaigns need must not shrink it.
    act(() => triggerResize(600))

    expect(renderedChartWidth(container)).toBe(dataWidth)
    expect(dataWidth).toBeGreaterThan(600)
  })

  it('anchors the first and last axis tick label inward from the plot edge instead of centering a long campaign id past it', () => {
    // Reported live at real scale: the LAST tick's label sat only
    // MARGIN.right (16px) from the chart's own right edge — nowhere near
    // half of a real id's rendered width ("JET-CAMP-NEWMENU") — and the
    // SVG's default overflow:hidden clipped it mid-word ("JET-CAMP-NEWM").
    // The large-N tests above never hit this: their synthetic
    // "CAMP-0".."CAMP-28" ids are short enough to never reach an edge.
    // Only the two edge campaigns get a real-length id here — every other
    // candidate tick stays short so this test isolates edge-anchoring from
    // the separate overlap/collision behavior covered by the next test.
    const longIdCampaigns: PromotionRoiDatum[] = Array.from(
      { length: 29 },
      (_, i) => ({
        campaignId:
          i === 0
            ? 'IFOOD-CAMP-BOOST01'
            : i === 28
              ? 'JET-CAMP-NEWMENU'
              : `C${i}`,
        campaignName: 'x',
        platform: 'iFood',
        spend: 100,
        attributedIncrementalRevenue: 100 + i,
        net: i - 14,
        sourceRefs: [],
      }),
    )

    const { container } = render(<PromoRoiChart data={longIdCampaigns} />)
    const leftEdge = plotLeftEdge(container) // MARGIN.left
    const rightEdge = renderedChartWidth(container) - 16 // MARGIN.right

    const firstTick = screen.getByText('IFOOD-CAMP-BOOST01')
    expect(firstTick).toHaveAttribute('text-anchor', 'start')
    expect(Number(firstTick.getAttribute('x'))).toBeCloseTo(leftEdge)

    const lastTick = screen.getByText('JET-CAMP-NEWMENU')
    expect(lastTick).toHaveAttribute('text-anchor', 'end')
    expect(Number(lastTick.getAttribute('x'))).toBeCloseTo(rightEdge)
  })

  it('drops an axis tick label rather than let it overlap its neighbor, when real-length campaign ids are too wide for the tickLabelStep gap', () => {
    // Reported live immediately after the edge-anchor fix above: pinning
    // the last tick fully on-canvas then visually overlapped the tick
    // immediately before it — tickLabelStep spaces candidate ticks assuming
    // short labels fit the gap between them, which a real ~20-25 char
    // campaign id does not at MIN_SLOT_WIDTH.
    const longIdCampaigns: PromotionRoiDatum[] = Array.from(
      { length: 29 },
      (_, i) => ({
        campaignId:
          i % 4 === 0 ? `PLATFORM-CAMP-LONGNAME-${i}` : `CAMP-${i}`,
        campaignName: 'x',
        platform: 'iFood',
        spend: 100,
        attributedIncrementalRevenue: 100 + i,
        net: i - 14,
        sourceRefs: [],
      }),
    )

    render(<PromoRoiChart data={longIdCampaigns} />)

    // tickLabelStep = ceil(29/8) = 4, so the naive candidate set is indices
    // 0,4,8,...,28, every one given a deliberately long id here. If all 8
    // still rendered full-length, the uniform 4-slot gap between them
    // (112px at MIN_SLOT_WIDTH) would have to hold a ~140px label — it
    // can't, so at least one candidate must be dropped for overlap...
    const candidateIds = [0, 4, 8, 12, 16, 20, 24, 28].map(
      (i) => `PLATFORM-CAMP-LONGNAME-${i}`,
    )
    const renderedCount = candidateIds.filter((id) =>
      screen.queryByText(id),
    ).length
    expect(renderedCount).toBeLessThan(candidateIds.length)

    // ...but the LAST one — the newest campaign, and the one this bug was
    // specifically reported against, not an arbitrary middle one — always
    // survives the drop.
    expect(screen.getByText('PLATFORM-CAMP-LONGNAME-28')).toBeInTheDocument()

    // Every campaign is still a real, focusable, hoverable bar regardless
    // of whether its id happens to be one of the dropped axis ticks —
    // dropping a LABEL is never dropping the campaign itself.
    expect(screen.getAllByRole('button', { name: /: net /i })).toHaveLength(
      29,
    )
  })

  it('names the x-axis explicitly ("Campaigns"), the same way the y-axis names itself ("Net (USD)")', () => {
    render(<PromoRoiChart />)

    expect(screen.getByText('Campaigns')).toBeInTheDocument()
    expect(screen.getByText('Net (USD)')).toBeInTheDocument()
  })

  // Spec 015 extension — column filters on the "view as table" fallback.
  // Scoped to Platform (categorical), Spend (numeric), and Net (numeric)
  // only; Campaign, Incremental revenue, and Sources stay unfiltered (see
  // PROMO_TABLE_COLUMN_FILTERS's doc comment in PromoRoiChart.tsx).
  describe('column filters on the embedded table', () => {
    it('renders a filter button only for Platform, Spend, and Net', async () => {
      const user = userEvent.setup()
      render(<PromoRoiChart />)
      await user.click(screen.getByRole('button', { name: /view as table/i }))

      expect(screen.getByRole('button', { name: /filter by platform/i })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /filter by spend/i })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /filter by net/i })).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /filter by campaign/i })).not.toBeInTheDocument()
      expect(
        screen.queryByRole('button', { name: /filter by incremental revenue/i }),
      ).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /filter by sources/i })).not.toBeInTheDocument()
    })

    it('narrows the table to the checked platform, and "Clear filters" restores every row', async () => {
      const user = userEvent.setup()
      render(<PromoRoiChart />)
      await user.click(screen.getByRole('button', { name: /view as table/i }))

      await user.click(screen.getByRole('button', { name: /filter by platform/i }))
      await user.click(await screen.findByRole('checkbox', { name: 'iFood' }))

      const table = screen.getByRole('table')
      expect(within(table).getByText('IFOOD-CAMP-BOOST01')).toBeInTheDocument()
      expect(within(table).getByText('IFOOD-CAMP-WEEKEND')).toBeInTheDocument()
      expect(within(table).queryByText('JET-CAMP-LUNCHFIX')).not.toBeInTheDocument()
      expect(within(table).queryByText('JET-CAMP-NEWMENU')).not.toBeInTheDocument()
      expect(screen.getByText('2 of 4 shown')).toBeInTheDocument()

      await user.click(screen.getByRole('button', { name: /clear filters/i }))
      expect(within(screen.getByRole('table')).getByText('JET-CAMP-LUNCHFIX')).toBeInTheDocument()
    })

    it('narrows the table to a Spend dollar range', async () => {
      const user = userEvent.setup()
      render(<PromoRoiChart />)
      await user.click(screen.getByRole('button', { name: /view as table/i }))

      await user.click(screen.getByRole('button', { name: /filter by spend/i }))
      await user.type(await screen.findByLabelText(/minimum, spend/i), '100')
      await user.type(screen.getByLabelText(/maximum, spend/i), '200')
      await user.click(screen.getByRole('button', { name: /^apply$/i }))

      const table = screen.getByRole('table')
      // Only IFOOD-CAMP-BOOST01 ($180.00) falls inside [100, 200]; JET-CAMP-
      // LUNCHFIX ($220.00) is above it, the other two ($95.00, $60.00) below.
      expect(within(table).getByText('IFOOD-CAMP-BOOST01')).toBeInTheDocument()
      expect(within(table).queryByText('JET-CAMP-LUNCHFIX')).not.toBeInTheDocument()
      expect(within(table).queryByText('IFOOD-CAMP-WEEKEND')).not.toBeInTheDocument()
      expect(within(table).queryByText('JET-CAMP-NEWMENU')).not.toBeInTheDocument()
    })

    it('excludes the FR-013 refused (null net) row from a Net range filter rather than guessing at it', async () => {
      const user = userEvent.setup()
      render(<PromoRoiChart />)
      await user.click(screen.getByRole('button', { name: /view as table/i }))

      await user.click(screen.getByRole('button', { name: /filter by net/i }))
      // A range wide enough to cover every real net in the sample
      // (-165..34), so a row only drops out here because its net is null,
      // not because it's out of range.
      await user.type(await screen.findByLabelText(/minimum, net/i), '-1000')
      await user.type(screen.getByLabelText(/maximum, net/i), '1000')
      await user.click(screen.getByRole('button', { name: /^apply$/i }))

      const table = screen.getByRole('table')
      expect(within(table).getByText('IFOOD-CAMP-BOOST01')).toBeInTheDocument()
      expect(within(table).getByText('JET-CAMP-LUNCHFIX')).toBeInTheDocument()
      expect(within(table).getByText('JET-CAMP-NEWMENU')).toBeInTheDocument()
      // IFOOD-CAMP-WEEKEND's net is null (refused, FR-013) — excluded, not
      // guessed at as $0 or coerced into either bound.
      expect(within(table).queryByText('IFOOD-CAMP-WEEKEND')).not.toBeInTheDocument()
      expect(screen.getByText('3 of 4 shown')).toBeInTheDocument()
    })

    it('shows the empty state, not a blank table, when a column filter matches nothing', async () => {
      const user = userEvent.setup()
      render(<PromoRoiChart />)
      await user.click(screen.getByRole('button', { name: /view as table/i }))

      await user.click(screen.getByRole('button', { name: /filter by spend/i }))
      await user.type(await screen.findByLabelText(/minimum, spend/i), '10000')
      await user.click(screen.getByRole('button', { name: /^apply$/i }))

      expect(screen.getByText('No campaigns match these filters.')).toBeInTheDocument()
      expect(screen.queryByRole('table')).not.toBeInTheDocument()
    })
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

  // Reported live against the real dataset (30 campaigns on file): the owner
  // read the chart as missing campaigns. It wasn't — every campaign has
  // always reached this component as a real bar target (see the 29-campaign
  // scale tests above, which predate this report) — but confirms it again at
  // the EXACT real count, matching the header chip/table's own "30
  // campaigns" so the chart and the rest of the page can never silently
  // disagree about how many campaigns exist.
  it('renders every campaign as a real, focusable bar at the real 30-campaign dataset scale', () => {
    const realScaleCampaigns: PromotionRoiDatum[] = Array.from(
      { length: 30 },
      (_, i) => ({
        campaignId: `${i % 2 === 0 ? 'IFOOD' : 'JET'}-CAMP-${String(i + 1).padStart(3, '0')}`,
        campaignName: `${i % 2 === 0 ? 'IFOOD' : 'JET'}-CAMP-${String(i + 1).padStart(3, '0')}`,
        platform: i % 2 === 0 ? 'iFood' : 'Just Eat Takeaway',
        spend: 100,
        attributedIncrementalRevenue: 100 + i,
        net: i - 15,
        sourceRefs: [],
      }),
    )

    render(<PromoRoiChart data={realScaleCampaigns} />)

    const bars = screen.getAllByRole('button', { name: /: net /i })
    expect(bars).toHaveLength(30)
    expect(
      screen.getByRole('group', {
        name: /across 30 promotion campaigns/,
      }),
    ).toBeInTheDocument()
  })

  // Reported live: the chart's own natural order is chronological (oldest
  // first — see PromotionsPage's toChartDatum/API-order comment), and a
  // plain `overflow-x-auto` wrapper defaults to showing that LEFT edge on
  // mount — the oldest campaigns, not the newest, most-actionable ones. The
  // owner read the newest campaigns being scrolled out of view as "not all
  // campaigns are in the chart." Exactly MarginTrendChart's own fix for the
  // identical problem (see that chart's matching describe block).
  describe('mounts scrolled to the right edge (the newest campaigns), not the oldest history', () => {
    const chronological: PromotionRoiDatum[] = Array.from(
      { length: 12 },
      (_, i) => ({
        campaignId: `CAMP-${i}`,
        campaignName: `CAMP-${i}`,
        platform: 'iFood',
        spend: 100,
        attributedIncrementalRevenue: 120,
        net: 20,
        sourceRefs: [],
      }),
    )

    it('sets the scroll container\'s scrollLeft to its full scrollWidth after render, by default', () => {
      const scrollWidthSpy = vi
        .spyOn(HTMLElement.prototype, 'scrollWidth', 'get')
        .mockReturnValue(2000)

      const { container } = render(<PromoRoiChart data={chronological} />)

      const scrollContainer = container.querySelector(
        '.overflow-x-auto',
      ) as HTMLDivElement
      expect(scrollContainer.scrollLeft).toBe(2000)

      scrollWidthSpy.mockRestore()
    })

    it('does NOT auto-scroll when initialScrollToEnd is false — the ROI-sorted case, where the campaign the owner asked to see first is already at the left', () => {
      const scrollWidthSpy = vi
        .spyOn(HTMLElement.prototype, 'scrollWidth', 'get')
        .mockReturnValue(2000)

      const { container } = render(
        <PromoRoiChart data={chronological} initialScrollToEnd={false} />,
      )

      const scrollContainer = container.querySelector(
        '.overflow-x-auto',
      ) as HTMLDivElement
      expect(scrollContainer.scrollLeft).toBe(0)

      scrollWidthSpy.mockRestore()
    })

    it('does not fight a reader\'s manual scroll on a re-render of the SAME order (only a new array reference)', () => {
      const scrollWidthSpy = vi
        .spyOn(HTMLElement.prototype, 'scrollWidth', 'get')
        .mockReturnValue(2000)

      const { container, rerender } = render(
        <PromoRoiChart data={chronological} />,
      )
      const scrollContainer = container.querySelector(
        '.overflow-x-auto',
      ) as HTMLDivElement
      expect(scrollContainer.scrollLeft).toBe(2000)

      // The reader scrolls back to look at older history.
      scrollContainer.scrollLeft = 0

      // A fresh array with the exact same campaigns in the exact same
      // order — e.g. a parent re-rendering for an unrelated reason. This
      // must NOT yank the reader back to the right.
      rerender(<PromoRoiChart data={[...chronological]} />)
      expect(scrollContainer.scrollLeft).toBe(0)

      scrollWidthSpy.mockRestore()
    })

    it('DOES re-scroll to the right when the plotted order genuinely changes', () => {
      const scrollWidthSpy = vi
        .spyOn(HTMLElement.prototype, 'scrollWidth', 'get')
        .mockReturnValue(2000)

      const { container, rerender } = render(
        <PromoRoiChart data={chronological} />,
      )
      const scrollContainer = container.querySelector(
        '.overflow-x-auto',
      ) as HTMLDivElement
      scrollContainer.scrollLeft = 0

      // A different order entirely (e.g. an ROI sort toggling back off) —
      // first/last campaign id both change.
      rerender(<PromoRoiChart data={[...chronological].reverse()} />)
      expect(scrollContainer.scrollLeft).toBe(2000)

      scrollWidthSpy.mockRestore()
    })
  })

  // Reported live: even once every campaign renders as a real bar and the
  // chart mounts scrolled to the newest ones, a plain overflow-x-auto row
  // gives no visual reason to suspect there's MORE off-screen in either
  // direction — the same discoverability gap Shell/Sidebar.tsx's
  // MobileNavBar already hit and fixed with an edge fade. Same pattern,
  // applied here.
  describe('scroll-fade affordance', () => {
    const manyCampaigns: PromotionRoiDatum[] = Array.from(
      { length: 29 },
      (_, i) => ({
        campaignId: `CAMP-${i}`,
        campaignName: `CAMP-${i}`,
        platform: 'iFood',
        spend: 100,
        attributedIncrementalRevenue: 100 + i,
        net: i - 14,
        sourceRefs: [],
      }),
    )

    it('shows a left fade — never a right one — once mounted scrolled to the default right edge, when real history sits off-screen', () => {
      const scrollWidthSpy = vi
        .spyOn(HTMLElement.prototype, 'scrollWidth', 'get')
        .mockReturnValue(2000)
      const clientWidthSpy = vi
        .spyOn(HTMLElement.prototype, 'clientWidth', 'get')
        .mockReturnValue(500)

      render(<PromoRoiChart data={manyCampaigns} />)

      expect(
        screen.getByTestId('promo-roi-chart-scroll-fade-left'),
      ).toBeInTheDocument()
      expect(
        screen.queryByTestId('promo-roi-chart-scroll-fade-right'),
      ).not.toBeInTheDocument()

      scrollWidthSpy.mockRestore()
      clientWidthSpy.mockRestore()
    })

    it('shows a right fade — never a left one — in the ROI-sorted view, where the chart stays scrolled to its start', () => {
      const scrollWidthSpy = vi
        .spyOn(HTMLElement.prototype, 'scrollWidth', 'get')
        .mockReturnValue(2000)
      const clientWidthSpy = vi
        .spyOn(HTMLElement.prototype, 'clientWidth', 'get')
        .mockReturnValue(500)

      render(
        <PromoRoiChart data={manyCampaigns} initialScrollToEnd={false} />,
      )

      expect(
        screen.getByTestId('promo-roi-chart-scroll-fade-right'),
      ).toBeInTheDocument()
      expect(
        screen.queryByTestId('promo-roi-chart-scroll-fade-left'),
      ).not.toBeInTheDocument()

      scrollWidthSpy.mockRestore()
      clientWidthSpy.mockRestore()
    })

    it('shows no fade in either direction once everything already fits — never a permanent decoration', () => {
      const scrollWidthSpy = vi
        .spyOn(HTMLElement.prototype, 'scrollWidth', 'get')
        .mockReturnValue(500)
      const clientWidthSpy = vi
        .spyOn(HTMLElement.prototype, 'clientWidth', 'get')
        .mockReturnValue(500)
      // jsdom has no real scroll clamping — a real browser refuses to let
      // scrollLeft exceed scrollWidth - clientWidth (0 here, no overflow),
      // so the component's own `scrollLeft = scrollWidth` write is a no-op
      // in practice. Modeling that clamp explicitly, rather than leaving
      // jsdom free to "accept" a scrollLeft no real browser would.
      const scrollLeftSpy = vi
        .spyOn(HTMLElement.prototype, 'scrollLeft', 'get')
        .mockReturnValue(0)

      render(<PromoRoiChart />)

      expect(
        screen.queryByTestId('promo-roi-chart-scroll-fade-left'),
      ).not.toBeInTheDocument()
      expect(
        screen.queryByTestId('promo-roi-chart-scroll-fade-right'),
      ).not.toBeInTheDocument()

      scrollWidthSpy.mockRestore()
      clientWidthSpy.mockRestore()
      scrollLeftSpy.mockRestore()
    })

    it('hides the left fade once scrolled all the way back to the start', () => {
      const scrollWidthSpy = vi
        .spyOn(HTMLElement.prototype, 'scrollWidth', 'get')
        .mockReturnValue(2000)
      const clientWidthSpy = vi
        .spyOn(HTMLElement.prototype, 'clientWidth', 'get')
        .mockReturnValue(500)

      const { container } = render(<PromoRoiChart data={manyCampaigns} />)
      const scrollContainer = container.querySelector(
        '.overflow-x-auto',
      ) as HTMLDivElement

      expect(
        screen.getByTestId('promo-roi-chart-scroll-fade-left'),
      ).toBeInTheDocument()

      act(() => {
        scrollContainer.scrollLeft = 0
        fireEvent.scroll(scrollContainer)
      })

      expect(
        screen.queryByTestId('promo-roi-chart-scroll-fade-left'),
      ).not.toBeInTheDocument()
      expect(
        screen.getByTestId('promo-roi-chart-scroll-fade-right'),
      ).toBeInTheDocument()

      scrollWidthSpy.mockRestore()
      clientWidthSpy.mockRestore()
    })
  })
})
