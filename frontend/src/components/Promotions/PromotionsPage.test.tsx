import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import PromotionsPage from './PromotionsPage'

// PromotionsPage now calls useNavigate() (spec 008 FR-001, chart click-to-ask
// navigates to /ask) — every render needs a Router ancestor, the same fix
// PointsCard.test.tsx already applied for its own <Link>.
function renderPage() {
  return render(
    <MemoryRouter>
      <PromotionsPage />
    </MemoryRouter>,
  )
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
        { file: 'fixtures/promotion_ad_spend_export.csv', row: 2 },
        { file: 'fixtures/delivery_platform_export.csv', row: 6 },
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
        { file: 'fixtures/promotion_ad_spend_export.csv', row: 4 },
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

  it('excludes a freshly-logged, not-yet-attributed campaign from the chart bars but keeps it in the count and table', async () => {
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
            { file: 'fixtures/promotion_ad_spend_export.csv', row: 9 },
          ],
        },
      ],
    })
    renderPage()

    await screen.findAllByText('IFOOD-CAMP-BOOST01')

    // Still counted in the header chip — a real campaign on file, just with
    // nothing plottable yet.
    expect(screen.getByText('3 campaigns')).toBeInTheDocument()

    // Never rendered as a bar target or refusal box — "hasn't happened yet"
    // is not the same fact as "refused", so it has nothing to plot at all.
    expect(
      screen.queryByRole('button', { name: /OWNER-CAMP-FRESH:/i }),
    ).not.toBeInTheDocument()

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
        text: async () => 'query_failed',
      }),
    )
    renderPage()

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/couldn't load campaigns/i)
    expect(alert).toHaveTextContent(/query_failed/)
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
            { file: 'fixtures/promotion_ad_spend_export.csv', row: 7 },
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
        { file: 'fixtures/promotion_ad_spend_export.csv', row: 7 },
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
          source_row_refs: [{ file: 'fixtures/promotion_ad_spend_export.csv', row: 10 }],
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
          source_row_refs: [{ file: 'fixtures/promotion_ad_spend_export.csv', row: 11 }],
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
          source_row_refs: [{ file: 'fixtures/promotion_ad_spend_export.csv', row: 12 }],
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
            { file: 'fixtures/promotion_ad_spend_export.csv', row: 7 },
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
            { file: 'fixtures/promotion_ad_spend_export.csv', row: 8 },
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
})
