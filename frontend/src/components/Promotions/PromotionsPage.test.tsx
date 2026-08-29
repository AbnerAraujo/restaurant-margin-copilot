import { render, screen, within } from '@testing-library/react'
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
