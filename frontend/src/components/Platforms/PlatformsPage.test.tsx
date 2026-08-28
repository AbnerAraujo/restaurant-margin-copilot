import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import PlatformsPage from './PlatformsPage'

const PLATFORM_COMPARISON_RESPONSE = {
  period: { start: '2026-08-01', end: '2026-08-14' },
  days_included: 14,
  platforms: [
    {
      source: 'ifood',
      display_name: 'iFood',
      gross_sales: '838.00',
      commission_paid: '184.85',
      effective_rate: '22.06%',
      promo_spend: '275.00',
      combined_cost: '459.85',
      combined_effective_rate: '54.87%',
      source_row_refs: [
        { file: 'fixtures/delivery_platform_export.csv', row: 2 },
      ],
    },
    {
      source: 'just_eat_takeaway',
      display_name: 'Just Eat Takeaway',
      gross_sales: '908.00',
      commission_paid: '181.60',
      effective_rate: '20.00%',
      promo_spend: '280.00',
      combined_cost: '461.60',
      combined_effective_rate: '50.84%',
      source_row_refs: [
        { file: 'fixtures/delivery_platform_export.csv', row: 4 },
      ],
    },
  ],
}

const ZERO_SALES_RESPONSE = {
  period: { start: '1999-04-01', end: '1999-04-01' },
  days_included: 1,
  platforms: [
    {
      source: 'ifood',
      display_name: 'iFood',
      gross_sales: '0.00',
      commission_paid: '0.00',
      effective_rate: null,
      promo_spend: '0.00',
      combined_cost: '0.00',
      combined_effective_rate: null,
      source_row_refs: [],
    },
    {
      source: 'just_eat_takeaway',
      display_name: 'Just Eat Takeaway',
      gross_sales: '100.00',
      commission_paid: '20.00',
      effective_rate: '20.00%',
      promo_spend: '0.00',
      combined_cost: '20.00',
      combined_effective_rate: '20.00%',
      source_row_refs: [],
    },
  ],
}

function stubFetch(body: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({ ok: true, json: async () => body }),
  )
}

describe('PlatformsPage', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('fetches the real platforms endpoint and shows both platforms side by side', async () => {
    stubFetch(PLATFORM_COMPARISON_RESPONSE)
    render(<PlatformsPage />)

    await screen.findAllByText('iFood')
    expect(fetch).toHaveBeenCalledWith(expect.stringContaining('/api/platforms'))
    expect(screen.getAllByText('Just Eat Takeaway').length).toBeGreaterThan(0)
  })

  it("shows iFood's real 22.06% effective rate, not the nominal flat 23% rate", async () => {
    stubFetch(PLATFORM_COMPARISON_RESPONSE)
    render(<PlatformsPage />)

    await screen.findAllByText('iFood')
    expect(screen.getAllByText('22.06%').length).toBeGreaterThan(0)
    expect(screen.queryByText('23.00%')).not.toBeInTheDocument()
  })

  it('renders commission-only and commission+promo as two distinct bars, never merged', async () => {
    stubFetch(PLATFORM_COMPARISON_RESPONSE)
    render(<PlatformsPage />)

    await screen.findAllByText('iFood')
    expect(
      screen.getByRole('button', {
        name: /iFood — commission only: \$184\.85/i,
      }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', {
        name: /iFood — commission \+ promo: \$459\.85/i,
      }),
    ).toBeInTheDocument()
  })

  it('shows a platform with zero sales this period as a real zero, never omitted (FR-003)', async () => {
    stubFetch(ZERO_SALES_RESPONSE)
    render(<PlatformsPage />)

    await screen.findAllByText('iFood')
    // The table renders "No sales this period" for the null rate rather than
    // a fabricated "0.00%".
    expect(screen.getAllByText('No sales this period').length).toBeGreaterThan(0)
    expect(screen.queryByText('0.00%')).not.toBeInTheDocument()
  })

  it('surfaces a fetch failure rather than rendering an empty comparison', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({ ok: false, status: 500, text: async () => 'boom' }),
    )
    render(<PlatformsPage />)

    expect(await screen.findByRole('alert')).toHaveTextContent(/boom/i)
  })
})
