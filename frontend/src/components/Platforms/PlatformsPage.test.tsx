import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
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

/**
 * Routes by URL so a test can supply a real /api/platforms/trend body
 * alongside the main /api/platforms response every render needs regardless
 * — a single flat mock would silently hand the comparison body to both
 * fetches, which is exactly what stubFetch above does for the tests that
 * don't care about the trend section at all.
 */
function stubFetchByUrl(comparisonBody: unknown, trendBody: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string) => {
      if (String(url).includes('/api/platforms/trend')) {
        return Promise.resolve({ ok: true, json: async () => trendBody })
      }
      return Promise.resolve({ ok: true, json: async () => comparisonBody })
    }),
  )
}

function trendPeriod(month: string, ifoodRate: string | null, jetRate: string | null) {
  return {
    month,
    result: {
      period: { start: `${month}-01`, end: `${month}-28` },
      days_included: 28,
      platforms: [
        {
          source: 'ifood',
          display_name: 'iFood',
          gross_sales: '0',
          commission_paid: '0',
          effective_rate: ifoodRate,
          promo_spend: '0',
          combined_cost: '0',
          combined_effective_rate: ifoodRate,
          source_row_refs: [],
        },
        {
          source: 'just_eat_takeaway',
          display_name: 'Just Eat Takeaway',
          gross_sales: '0',
          commission_paid: '0',
          effective_rate: jetRate,
          promo_spend: '0',
          combined_cost: '0',
          combined_effective_rate: jetRate,
          source_row_refs: [],
        },
      ],
    },
  }
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

  it('shows the effective-rate trend chart with at least 2 real trailing months (spec 008 FR-007)', async () => {
    stubFetchByUrl(PLATFORM_COMPARISON_RESPONSE, {
      periods: [
        trendPeriod('2026-06', '21.00%', '19.50%'),
        trendPeriod('2026-07', '21.50%', '19.80%'),
        trendPeriod('2026-08', '22.06%', '20.00%'),
      ],
    })
    render(<PlatformsPage />)

    const panel = await screen.findByRole('region', { name: 'Effective rate trend' })
    expect(panel).toBeInTheDocument()
    expect(screen.getByRole('img', { name: /effective commission rate trend/i })).toBeInTheDocument()
  })

  it('omits the effective-rate trend chart with fewer than 2 real trailing months, never a single-point chart (FR-013)', async () => {
    stubFetchByUrl(PLATFORM_COMPARISON_RESPONSE, {
      periods: [trendPeriod('2026-08', '22.06%', '20.00%')],
    })
    render(<PlatformsPage />)

    await screen.findAllByText('iFood')
    expect(screen.queryByRole('region', { name: 'Effective rate trend' })).not.toBeInTheDocument()
  })

  it('omits the effective-rate trend chart when the trend fetch fails, without blocking the main comparison', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation((url: string) => {
        if (String(url).includes('/api/platforms/trend')) {
          return Promise.resolve({ ok: false, status: 500, text: async () => 'boom' })
        }
        return Promise.resolve({ ok: true, json: async () => PLATFORM_COMPARISON_RESPONSE })
      }),
    )
    render(<PlatformsPage />)

    await screen.findAllByText('iFood')
    expect(screen.queryByRole('region', { name: 'Effective rate trend' })).not.toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('narrows the chart and table to platforms matching the search, and clears back', async () => {
    stubFetch(PLATFORM_COMPARISON_RESPONSE)
    render(<PlatformsPage />)

    await screen.findAllByText('iFood')
    expect(screen.getAllByText('Just Eat Takeaway').length).toBeGreaterThan(0)

    await userEvent.type(screen.getByLabelText('Search platforms'), 'ifood')

    expect(screen.getAllByText('iFood').length).toBeGreaterThan(0)
    expect(screen.queryByText('Just Eat Takeaway')).not.toBeInTheDocument()
    expect(screen.getByText('1 of 2 platforms shown')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: /clear filters/i }))
    expect(screen.getAllByText('Just Eat Takeaway').length).toBeGreaterThan(0)
  })

  it('shows a reassuring empty state when the platform search matches nothing', async () => {
    stubFetch(PLATFORM_COMPARISON_RESPONSE)
    render(<PlatformsPage />)

    await screen.findAllByText('iFood')
    await userEvent.type(
      screen.getByLabelText('Search platforms'),
      'no-such-platform',
    )

    expect(
      screen.getByText('No platforms match this search.'),
    ).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })
})
