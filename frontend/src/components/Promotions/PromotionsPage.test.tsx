import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import PromotionsPage from './PromotionsPage'

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
    render(<PromotionsPage />)

    await screen.findByText('IFOOD-CAMP-BOOST01')
    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/promotions'),
    )
  })

  it('renders a campaign with no attributable revenue as refused, never as $0', async () => {
    stubFetch(PROMOTIONS_RESPONSE)
    render(<PromotionsPage />)

    await screen.findByText('IFOOD-CAMP-BOOST01')
    expect(
      screen.getByRole('button', {
        name: /IFOOD-CAMP-WEEKEND: unattributable/i,
      }),
    ).toBeInTheDocument()
    expect(screen.getByText('Unattributable')).toBeInTheDocument()
    expect(screen.queryByText('+$0.00')).not.toBeInTheDocument()
  })

  it('shows the backend net figure without recomputing it on the client', async () => {
    const user = userEvent.setup()
    stubFetch(PROMOTIONS_RESPONSE)
    render(<PromotionsPage />)

    await screen.findByText('IFOOD-CAMP-BOOST01')
    await user.click(screen.getByRole('button', { name: /view as table/i }))

    const table = screen.getByRole('table')
    const boostRow = within(table)
      .getByText('IFOOD-CAMP-BOOST01')
      .closest('tr') as HTMLElement
    // 214.00 - 180.00 = 34.00, computed in Go and served as `roi`.
    expect(within(boostRow).getByText('+$34.00')).toBeInTheDocument()
    expect(within(boostRow).getByText('$214.00')).toBeInTheDocument()
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
    render(<PromotionsPage />)

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/couldn't load campaigns/i)
    expect(alert).toHaveTextContent(/query_failed/)
  })

  it('says plainly when no promotions have been ingested yet', async () => {
    stubFetch({ promotions: [] })
    render(<PromotionsPage />)

    expect(
      await screen.findByText(/no promotion records on file yet/i),
    ).toBeInTheDocument()
  })
})
