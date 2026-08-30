import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import PromotionsPage from './PromotionsPage'

// PromotionsPage now calls useNavigate() (spec 008 FR-001, chart click-to-ask
// navigates to /ask) — every render needs a Router ancestor, the same fix
// PointsCard.test.tsx already applied for its own <Link>. A real DATA
// router (createMemoryRouter/RouterProvider), not the plain <MemoryRouter>
// this used to be: PromotionsPage renders LogReplacementForm, which now
// calls useBlocker (the unsaved-changes discard guard) — that hook throws
// outside a data router, matching router.test.tsx's own pattern. Still
// takes `initialEntries` so the URL-filter-restoration tests can render on
// a specific path+search string, same as the old MemoryRouter version did.
function renderPage(initialEntries: string[] = ['/']) {
  const router = createMemoryRouter(
    [{ path: '/promotions', element: <PromotionsPage /> }, { path: '/', element: <PromotionsPage /> }],
    { initialEntries },
  )
  return render(<RouterProvider router={router} />)
}

const PROMOTIONS_RESPONSE = {
  promotions: [
    {
      platform: 'iFood',
      campaign_id: 'IFOOD-CAMP-BOOST01',
      period: { start: '2026-08-01', end: '2026-08-07' },
      spend: '180.00',
      attributed_incremental_orders: 6,
      attributed_incremental_revenue: '214.00',
      roi: '34.00',
      flagged_negative: false,
      source_row_refs: [
        { file: 'data/live/promotion_ad_spend_export.csv', row: 2 },
        { file: 'data/live/delivery_platform_export.csv', row: 6 },
      ],
    },
    {
      platform: 'iFood',
      campaign_id: 'IFOOD-CAMP-WEEKEND',
      period: { start: '2026-08-08', end: '2026-08-09' },
      spend: '95.00',
      attributed_incremental_orders: null,
      attributed_incremental_revenue: null,
      roi: null,
      reason: 'attribution_unavailable',
      flagged_negative: false,
      source_row_refs: [
        { file: 'data/live/promotion_ad_spend_export.csv', row: 4 },
      ],
    },
  ],
}

function stubFetch(body: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({ ok: true, json: async () => body }),
  )
}

describe('PromotionsPage', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('fetches the real promotions endpoint rather than rendering hardcoded campaigns', async () => {
    stubFetch(PROMOTIONS_RESPONSE)
    renderPage()

    // The chart's x-axis label and its underlying table both name the
    // campaign, and the table now opens by default on this route, so this
    // scopes to the axis label rather than asserting a single match.
    await screen.findAllByText('IFOOD-CAMP-BOOST01')
    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/promotions'),
    )
  })

  it('renders a campaign with no attributable revenue as refused, never as $0', async () => {
    stubFetch(PROMOTIONS_RESPONSE)
    renderPage()

    await screen.findAllByText('IFOOD-CAMP-BOOST01')
    expect(
      screen.getByRole('button', {
        name: /IFOOD-CAMP-WEEKEND: unattributable/i,
      }),
    ).toBeInTheDocument()
    // Present in the chart's refusal marker and again in the table's Net
    // cell. Both must say "unattributable"; neither may say a dollar figure.
    expect(screen.getAllByText(/unattributable/i).length).toBeGreaterThan(0)
    expect(screen.queryByText('+$0.00')).not.toBeInTheDocument()
    expect(screen.queryByText('$0.00')).not.toBeInTheDocument()
  })

  it('shows the backend net figure without recomputing it on the client', async () => {
    stubFetch(PROMOTIONS_RESPONSE)
    renderPage()

    await screen.findAllByText('IFOOD-CAMP-BOOST01')
    // The table opens by default on this route now, so there is nothing to
    // click first. Assert that directly rather than quietly dropping it.
    expect(
      screen.queryByRole('button', { name: /view as table/i }),
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: /hide table/i }),
    ).toBeInTheDocument()

    const table = screen.getByRole('table')
    const boostRow = within(table)
      .getByText('IFOOD-CAMP-BOOST01')
      .closest('tr') as HTMLElement
    // 214.00 - 180.00 = 34.00, computed in Go and served as `roi`.
    expect(within(boostRow).getByText('+$34.00')).toBeInTheDocument()
    expect(within(boostRow).getByText('$214.00')).toBeInTheDocument()
  })

  it('gives a freshly-logged, not-yet-attributed campaign the same no-bar marker as a refused one, so the chart never undercounts the table', async () => {
    stubFetch({
      promotions: [
        ...PROMOTIONS_RESPONSE.promotions,
        {
          platform: 'iFood',
          campaign_id: 'OWNER-CAMP-FRESH',
          period: { start: '2026-08-20', end: '2026-08-20' },
          spend: '40.00',
          attributed_incremental_orders: null,
          attributed_incremental_revenue: null,
          roi: null,
          reason: 'not_yet_attributed',
          flagged_negative: false,
          origin: 'owner_created',
          source_row_refs: [
            { file: 'data/live/promotion_ad_spend_export.csv', row: 9 },
          ],
        },
      ],
    })
    renderPage()

    await screen.findAllByText('IFOOD-CAMP-BOOST01')

    // Still counted in the header chip — a real campaign on file, just with
    // nothing plottable yet.
    expect(screen.getByText('3 campaigns')).toBeInTheDocument()

    // Rendered as its own no-bar marker, worded as "not yet attributed"
    // (never as an active refusal it never went through) — the chart's own
    // bar count and accessible description must never undercount the table
    // below it, which is exactly the bug this test used to codify.
    expect(
      screen.getByRole('button', { name: /OWNER-CAMP-FRESH: not yet attributed/i }),
    ).toBeInTheDocument()

    // The genuine FR-013 refusal keeps its refused bar treatment.
    expect(
      screen.getByRole('button', {
        name: /IFOOD-CAMP-WEEKEND: unattributable/i,
      }),
    ).toBeInTheDocument()

    // Still present in the table underneath — every logged campaign belongs
    // there, including one with nothing attributed yet.
    const table = screen.getByRole('table')
    expect(within(table).getByText('OWNER-CAMP-FRESH')).toBeInTheDocument()
  })

  it('reports a backend failure instead of an empty page that looks like "no campaigns"', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        text: async () =>
          JSON.stringify({
            error: 'query_failed',
            detail: 'ERROR: relation "promotions" does not exist (SQLSTATE 42P01)',
          }),
      }),
    )
    renderPage()

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/couldn't load your campaigns/i)
  })

  it('hands a failed campaign load a next step rather than the raw Go error', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        text: async () =>
          JSON.stringify({
            error: 'query_failed',
            detail: 'ERROR: relation "promotions" does not exist (SQLSTATE 42P01)',
          }),
      }),
    )
    renderPage()

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/reload this page/i)
    expect(alert).not.toHaveTextContent(/SQLSTATE/i)
  })

  it('says plainly when no promotions have been ingested yet', async () => {
    stubFetch({ promotions: [] })
    renderPage()

    expect(
      await screen.findByText(/no promotion records on file yet/i),
    ).toBeInTheDocument()
  })

  it('marks a flagged campaign with no replacement as needing action (spec 008 FR-010)', async () => {
    stubFetch({
      promotions: [
        ...PROMOTIONS_RESPONSE.promotions,
        {
          platform: 'Just Eat Takeaway',
          campaign_id: 'JET-CAMP-LOSER',
          period: { start: '2026-08-01', end: '2026-08-07' },
          spend: '100.00',
          attributed_incremental_orders: 1,
          attributed_incremental_revenue: '20.00',
          roi: '-80.00',
          flagged_negative: true,
          source_row_refs: [
            { file: 'data/live/promotion_ad_spend_export.csv', row: 7 },
          ],
        },
      ],
    })
    renderPage()

    await screen.findAllByText('IFOOD-CAMP-BOOST01')

    expect(screen.getByText('1 campaign needs a decision')).toBeInTheDocument()
    expect(
      within(screen.getByRole('status')).getByText('JET-CAMP-LOSER'),
    ).toBeInTheDocument()
  })

  it('collapses a large needs-action list to 3 rows with a "Show N more" toggle, never one row per campaign unconditionally', async () => {
    const flaggedCampaigns = Array.from({ length: 5 }, (_, i) => ({
      platform: 'Just Eat Takeaway',
      campaign_id: `JET-CAMP-LOSER-${i}`,
      period: { start: '2026-08-01', end: '2026-08-07' },
      spend: '100.00',
      attributed_incremental_orders: 1,
      attributed_incremental_revenue: '20.00',
      roi: '-80.00',
      flagged_negative: true,
      source_row_refs: [
        { file: 'data/live/promotion_ad_spend_export.csv', row: 7 },
      ],
    }))
    stubFetch({
      promotions: [...PROMOTIONS_RESPONSE.promotions, ...flaggedCampaigns],
    })
    renderPage()

    await screen.findAllByText('IFOOD-CAMP-BOOST01')

    const panel = screen.getByRole('status')
    expect(within(panel).getByText('5 campaigns need a decision')).toBeInTheDocument()

    // Capped to 3 visible rows, not all 5.
    expect(within(panel).getByText('JET-CAMP-LOSER-0')).toBeInTheDocument()
    expect(within(panel).getByText('JET-CAMP-LOSER-1')).toBeInTheDocument()
    expect(within(panel).getByText('JET-CAMP-LOSER-2')).toBeInTheDocument()
    expect(within(panel).queryByText('JET-CAMP-LOSER-3')).not.toBeInTheDocument()
    expect(within(panel).queryByText('JET-CAMP-LOSER-4')).not.toBeInTheDocument()

    const toggle = within(panel).getByRole('button', { name: /show 2 more/i })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')

    await userEvent.click(toggle)

    expect(within(panel).getByText('JET-CAMP-LOSER-3')).toBeInTheDocument()
    expect(within(panel).getByText('JET-CAMP-LOSER-4')).toBeInTheDocument()
    expect(
      within(panel).getByRole('button', { name: /show fewer/i }),
    ).toHaveAttribute('aria-expanded', 'true')
  })

  it('shows the real sum of each platform\'s attributed ROI, excluding unattributable campaigns (spec 008 FR-011)', async () => {
    stubFetch({
      promotions: [
        // iFood: 34.00 (attributed) + unattributable (excluded) = 34.00, not 34.00-averaged-with-zero.
        ...PROMOTIONS_RESPONSE.promotions,
        {
          platform: 'Just Eat Takeaway',
          campaign_id: 'JET-CAMP-A',
          period: { start: '2026-08-01', end: '2026-08-07' },
          spend: '100.00',
          attributed_incremental_orders: 1,
          attributed_incremental_revenue: '150.00',
          roi: '50.00',
          flagged_negative: false,
          source_row_refs: [{ file: 'data/live/promotion_ad_spend_export.csv', row: 10 }],
        },
        {
          platform: 'Just Eat Takeaway',
          campaign_id: 'JET-CAMP-B',
          period: { start: '2026-08-08', end: '2026-08-14' },
          spend: '100.00',
          attributed_incremental_orders: 1,
          attributed_incremental_revenue: '20.00',
          roi: '-80.00',
          flagged_negative: true,
          source_row_refs: [{ file: 'data/live/promotion_ad_spend_export.csv', row: 11 }],
        },
      ],
    })
    renderPage()

    await screen.findAllByText('IFOOD-CAMP-BOOST01')

    // Both the aggregate stat AND the chart/table render "+$34.00" — scope
    // to the aggregate Stat's own container (its dt/dd share one parent) so
    // this asserts the aggregate specifically, not a coincidental match
    // against a per-campaign figure elsewhere on the page.
    const ifoodStat = screen.getByText('iFood ROI').closest('div') as HTMLElement
    expect(within(ifoodStat).getByText('+$34.00')).toBeInTheDocument()

    // Just Eat Takeaway: 50.00 + (-80.00) = -30.00.
    const jetStat = screen
      .getByText('Just Eat Takeaway ROI')
      .closest('div') as HTMLElement
    expect(within(jetStat).getByText('−$30.00')).toBeInTheDocument()
  })

  it('sorts the campaign list by ROI, keeping unattributable campaigns at the same end in either direction (spec 008 FR-012)', async () => {
    stubFetch({
      promotions: [
        ...PROMOTIONS_RESPONSE.promotions, // BOOST01: roi 34.00; WEEKEND: roi null
        {
          platform: 'Just Eat Takeaway',
          campaign_id: 'JET-CAMP-WORST',
          period: { start: '2026-08-01', end: '2026-08-07' },
          spend: '100.00',
          attributed_incremental_orders: 1,
          attributed_incremental_revenue: '20.00',
          roi: '-80.00',
          flagged_negative: true,
          source_row_refs: [{ file: 'data/live/promotion_ad_spend_export.csv', row: 12 }],
        },
      ],
    })
    renderPage()

    await screen.findAllByText('IFOOD-CAMP-BOOST01')
    const table = screen.getByRole('table')
    const campaignOrder = () =>
      within(table)
        .getAllByRole('row')
        .slice(1) // drop the header row
        .map((row) => row.textContent ?? '')

    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: /highest first/i }))
    const highestFirst = campaignOrder()
    expect(highestFirst[0]).toContain('IFOOD-CAMP-BOOST01') // 34.00
    expect(highestFirst[1]).toContain('JET-CAMP-WORST') // -80.00
    expect(highestFirst[2]).toContain('IFOOD-CAMP-WEEKEND') // null, still last

    await user.click(screen.getByRole('button', { name: /lowest first/i }))
    const lowestFirst = campaignOrder()
    expect(lowestFirst[0]).toContain('JET-CAMP-WORST') // -80.00
    expect(lowestFirst[1]).toContain('IFOOD-CAMP-BOOST01') // 34.00
    // Unattributable stays LAST in both directions — never jumps to the
    // top just because the direction flipped.
    expect(lowestFirst[2]).toContain('IFOOD-CAMP-WEEKEND')
  })

  it('passes every fetched campaign through to the chart with no silent cap, at the real 2-year dataset\'s scale (30+ campaigns, realistic-length ids)', async () => {
    // Reported live against backend/data/live/ (2024-08-01–2026-07-31, a
    // realistic promo cadence): 30 campaigns on file. Guards the whole
    // fetch -> displayedPromotions -> toChartDatum -> PromoRoiChart pipeline
    // against a silent truncation ANYWHERE along it — this test intentionally
    // uses real-length campaign ids (not the short "CAMP-N" synthetic ones
    // PromoRoiChart.test.tsx's own large-N tests use), since a truncation OR
    // a rendering/clipping bug at this scale specifically involves id length,
    // not just count.
    const manyCampaigns = Array.from({ length: 30 }, (_, i) => ({
      platform: i % 2 === 0 ? 'iFood' : 'Just Eat Takeaway',
      campaign_id: `${i % 2 === 0 ? 'IFOOD' : 'JET'}-CAMP-${String(i + 1).padStart(3, '0')}`,
      period: { start: '2024-08-01', end: '2024-08-08' },
      spend: '100.00',
      attributed_incremental_orders: 5,
      attributed_incremental_revenue: '120.00',
      roi: String(i - 15),
      flagged_negative: i - 15 < 0,
      source_row_refs: [
        { file: 'delivery_platform_export.csv', row: i + 2 },
      ],
    }))
    stubFetch({ promotions: manyCampaigns })
    renderPage()

    await screen.findAllByText('IFOOD-CAMP-001')

    // The header chip counts every fetched record, not a capped subset.
    expect(screen.getByText('30 campaigns')).toBeInTheDocument()

    // Every campaign reaches the chart as a real, focusable bar target —
    // nothing dropped between the fetch and the chart.
    const bars = screen.getAllByRole('button', { name: /: net /i })
    expect(bars).toHaveLength(30)
    for (const campaign of manyCampaigns) {
      expect(
        screen.getByRole('button', {
          name: new RegExp(`^${campaign.campaign_id}:`),
        }),
      ).toBeInTheDocument()
    }

    // And every campaign is in the underlying table too (table opens by
    // default on this route) — the chart and the table read one list, not
    // two independently-capped ones.
    const table = screen.getByRole('table')
    for (const campaign of manyCampaigns) {
      expect(within(table).getByText(campaign.campaign_id)).toBeInTheDocument()
    }
  })

  it('does not mark a flagged campaign as needing action once another campaign replaces it', async () => {
    stubFetch({
      promotions: [
        ...PROMOTIONS_RESPONSE.promotions,
        {
          platform: 'Just Eat Takeaway',
          campaign_id: 'JET-CAMP-LOSER',
          period: { start: '2026-08-01', end: '2026-08-07' },
          spend: '100.00',
          attributed_incremental_orders: 1,
          attributed_incremental_revenue: '20.00',
          roi: '-80.00',
          flagged_negative: true,
          source_row_refs: [
            { file: 'data/live/promotion_ad_spend_export.csv', row: 7 },
          ],
        },
        {
          platform: 'Just Eat Takeaway',
          campaign_id: 'JET-CAMP-REPLACEMENT',
          period: { start: '2026-08-08', end: '2026-08-14' },
          spend: '100.00',
          attributed_incremental_orders: null,
          attributed_incremental_revenue: null,
          roi: null,
          reason: 'not_yet_attributed',
          flagged_negative: false,
          origin: 'owner_created',
          replaces_campaign_id: 'JET-CAMP-LOSER',
          source_row_refs: [
            { file: 'data/live/promotion_ad_spend_export.csv', row: 8 },
          ],
        },
      ],
    })
    renderPage()

    await screen.findAllByText('IFOOD-CAMP-BOOST01')

    expect(
      screen.queryByText(/needs a decision/i),
    ).not.toBeInTheDocument()
  })

  it('narrows the table to campaigns matching the search box, by campaign ID', async () => {
    stubFetch(PROMOTIONS_RESPONSE)
    renderPage()

    await screen.findAllByText('IFOOD-CAMP-BOOST01')
    const table = screen.getByRole('table')
    expect(within(table).getByText('IFOOD-CAMP-WEEKEND')).toBeInTheDocument()

    await userEvent.type(
      screen.getByLabelText('Search campaigns'),
      'BOOST01{Enter}',
    )

    expect(within(table).getByText('IFOOD-CAMP-BOOST01')).toBeInTheDocument()
    expect(
      within(table).queryByText('IFOOD-CAMP-WEEKEND'),
    ).not.toBeInTheDocument()
    // The header count and platform aggregates are unaffected by the grid
    // filter — they stay honest totals of every campaign on file.
    expect(screen.getByText('2 campaigns')).toBeInTheDocument()
  })

  it('labels the header chips "Overall" once a filter narrows the table, so the two counts read as deliberate rather than disagreeing', async () => {
    stubFetch(PROMOTIONS_RESPONSE)
    renderPage()

    await screen.findAllByText('IFOOD-CAMP-BOOST01')
    // Unfiltered: the chips are the only summary on screen, so the label
    // would be pure clutter — it stays hidden.
    expect(screen.queryByText('Overall')).not.toBeInTheDocument()

    await userEvent.type(
      screen.getByLabelText('Search campaigns'),
      'BOOST01{Enter}',
    )

    // Filtered: the header chip ("2 campaigns") and the now-1-row table
    // disagree unless it's clear the chip is a total, not a live readout —
    // this label makes that explicit.
    expect(screen.getByText('Overall')).toBeInTheDocument()
    expect(screen.getByText('2 campaigns')).toBeInTheDocument()
  })

  it('narrows the table by platform, and shows a way back to the full list', async () => {
    stubFetch({
      promotions: [
        ...PROMOTIONS_RESPONSE.promotions,
        {
          platform: 'Just Eat Takeaway',
          campaign_id: 'JET-CAMP-A',
          period: { start: '2026-08-01', end: '2026-08-07' },
          spend: '100.00',
          attributed_incremental_orders: 1,
          attributed_incremental_revenue: '150.00',
          roi: '50.00',
          flagged_negative: false,
          source_row_refs: [{ file: 'data/live/promotion_ad_spend_export.csv', row: 10 }],
        },
      ],
    })
    renderPage()

    await screen.findAllByText('IFOOD-CAMP-BOOST01')
    const table = screen.getByRole('table')

    await userEvent.selectOptions(
      screen.getByLabelText('Filter by platform'),
      'Just Eat Takeaway',
    )

    expect(within(table).getByText('JET-CAMP-A')).toBeInTheDocument()
    expect(
      within(table).queryByText('IFOOD-CAMP-BOOST01'),
    ).not.toBeInTheDocument()
    expect(
      screen.getByText('1 of 3 campaigns shown'),
    ).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: /clear filters/i }))

    expect(within(table).getByText('IFOOD-CAMP-BOOST01')).toBeInTheDocument()
    expect(within(table).getByText('JET-CAMP-A')).toBeInTheDocument()
  })

  it('shows a reassuring, actionable empty state when filters match nothing', async () => {
    stubFetch(PROMOTIONS_RESPONSE)
    renderPage()

    await screen.findAllByText('IFOOD-CAMP-BOOST01')
    await userEvent.type(
      screen.getByLabelText('Search campaigns'),
      'no-such-campaign{Enter}',
    )

    expect(
      screen.getByText('No campaigns match these filters.'),
    ).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()

    await userEvent.click(
      screen.getAllByRole('button', { name: /clear filters/i })[0],
    )
    expect(await screen.findByRole('table')).toBeInTheDocument()
  })

  it('restores search and platform filters from the URL — the browser-Back-to-a-filtered-view case (spec 008 FR-001)', async () => {
    // Stands in for the real, designed flow this bug broke: narrow the
    // table, click a chart point through to `/ask`, then press the
    // browser's real Back button. That POP navigation remounts this page
    // against whatever URL was already in history for `/promotions` — with
    // `useTableFilter` now sourcing state from `useSearchParams` (and every
    // filter write using `{ replace: true }`), that's the SAME entry the
    // filters were written into, not a fresh unfiltered one.
    stubFetch({
      promotions: [
        ...PROMOTIONS_RESPONSE.promotions,
        {
          platform: 'Just Eat Takeaway',
          campaign_id: 'JET-CAMP-A',
          period: { start: '2026-08-01', end: '2026-08-07' },
          spend: '100.00',
          attributed_incremental_orders: 1,
          attributed_incremental_revenue: '150.00',
          roi: '50.00',
          flagged_negative: false,
          source_row_refs: [{ file: 'data/live/promotion_ad_spend_export.csv', row: 10 }],
        },
      ],
    })
    renderPage(['/promotions?tf-search=boost&platform=iFood'])

    await screen.findAllByText('IFOOD-CAMP-BOOST01')

    expect(screen.getByLabelText('Search campaigns')).toHaveValue('boost')
    expect(screen.getByLabelText('Filter by platform')).toHaveValue('iFood')
    expect(screen.getByText('1 of 3 campaigns shown')).toBeInTheDocument()

    const table = screen.getByRole('table')
    expect(within(table).getByText('IFOOD-CAMP-BOOST01')).toBeInTheDocument()
    expect(within(table).queryByText('IFOOD-CAMP-WEEKEND')).not.toBeInTheDocument()
    expect(within(table).queryByText('JET-CAMP-A')).not.toBeInTheDocument()
  })

  it('starts unfiltered on a plain /promotions URL — an ordinary in-app navigation (e.g. the sidebar link), not a restored one', async () => {
    stubFetch(PROMOTIONS_RESPONSE)
    renderPage(['/promotions'])

    await screen.findAllByText('IFOOD-CAMP-BOOST01')

    expect(screen.getByLabelText('Search campaigns')).toHaveValue('')
    expect(screen.getByLabelText('Filter by platform')).toHaveValue('')
    const table = screen.getByRole('table')
    expect(within(table).getByText('IFOOD-CAMP-WEEKEND')).toBeInTheDocument()
  })

  it('groups the two ROI sort toggles under their visible label, the way the Home status filter already does', async () => {
    // The pair used to sit loose beside a <span> that nothing associated them
    // with, so "Highest first" reached assistive tech with no hint of what it
    // sorted. Labelled by that same span rather than a duplicated aria-label,
    // so the visible and accessible names can never drift apart.
    stubFetch(PROMOTIONS_RESPONSE)
    renderPage()

    const group = await screen.findByRole('group', { name: 'Sort by ROI' })
    expect(
      within(group).getByRole('button', { name: /highest first/i }),
    ).toBeInTheDocument()
    expect(
      within(group).getByRole('button', { name: /lowest first/i }),
    ).toBeInTheDocument()
  })
})
