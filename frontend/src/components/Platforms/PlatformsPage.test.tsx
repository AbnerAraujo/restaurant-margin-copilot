import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import PlatformsPage from './PlatformsPage'

// useTableFilter now syncs to the URL via useSearchParams (spec 008 FR-001
// fix), which needs a Router ancestor — the same fix PromotionsPage.test.tsx
// already applies for its own router usage.
function renderPage() {
  return render(
    <MemoryRouter>
      <PlatformsPage />
    </MemoryRouter>,
  )
}

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
        { file: 'data/live/delivery_platform_export.csv', row: 2 },
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
        { file: 'data/live/delivery_platform_export.csv', row: 4 },
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
    renderPage()

    await screen.findAllByText('iFood')
    expect(fetch).toHaveBeenCalledWith(expect.stringContaining('/api/platforms'))
    expect(screen.getAllByText('Just Eat Takeaway').length).toBeGreaterThan(0)
  })

  it("shows iFood's real 22.06% effective rate, not the nominal flat 23% rate", async () => {
    stubFetch(PLATFORM_COMPARISON_RESPONSE)
    renderPage()

    await screen.findAllByText('iFood')
    expect(screen.getAllByText('22.06%').length).toBeGreaterThan(0)
    expect(screen.queryByText('23.00%')).not.toBeInTheDocument()
  })

  it('renders commission-only and commission+promo as two distinct bars, never merged', async () => {
    stubFetch(PLATFORM_COMPARISON_RESPONSE)
    renderPage()

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

  it('never truncates two same-platform bar labels down to identical visible text (reported live: both read "Just Eat Takeaway — com…")', async () => {
    stubFetch(PLATFORM_COMPARISON_RESPONSE)
    renderPage()

    await screen.findAllByText('iFood')

    // Full labels ("Just Eat Takeaway — commission only" / "... + promo")
    // both run past the chart's label budget and share an identical first
    // 23 characters — the exact case that used to collapse both rows to
    // "Just Eat Takeaway — com…". The fixed truncation keeps a stable head
    // (enough to identify the platform) plus whatever tail fits, so the
    // two visible strings stay distinguishable.
    const commissionOnlyLabel = screen.getByText('Just Eat Tak…ission only')
    const commissionPlusPromoLabel = screen.getByText('Just Eat Tak…ion + promo')
    expect(commissionOnlyLabel).toBeInTheDocument()
    expect(commissionPlusPromoLabel).toBeInTheDocument()
    expect(commissionOnlyLabel.textContent).not.toEqual(
      commissionPlusPromoLabel.textContent,
    )
  })

  it('shows a platform with zero sales this period as a real zero, never omitted (FR-003)', async () => {
    stubFetch(ZERO_SALES_RESPONSE)
    renderPage()

    await screen.findAllByText('iFood')
    // The table renders "No sales this period" for the null rate rather than
    // a fabricated "0.00%".
    expect(screen.getAllByText('No sales this period').length).toBeGreaterThan(0)
    expect(screen.queryByText('0.00%')).not.toBeInTheDocument()
  })

  it('surfaces a fetch failure rather than rendering an empty comparison', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        text: async () =>
          JSON.stringify({
            error: 'query_failed',
            detail: 'ERROR: relation "reconciliations" does not exist (SQLSTATE 42P01)',
          }),
      }),
    )
    renderPage()

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/couldn't load the platform comparison/i)
  })

  it('tells the owner how to recover from a failed load, without putting the raw Go error on screen', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        text: async () =>
          JSON.stringify({
            error: 'query_failed',
            detail: 'ERROR: relation "reconciliations" does not exist (SQLSTATE 42P01)',
          }),
      }),
    )
    renderPage()

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/reload this page/i)
    expect(alert).not.toHaveTextContent(/SQLSTATE/i)
  })

  it('shows the effective-rate trend chart with at least 2 real trailing months (spec 008 FR-007)', async () => {
    stubFetchByUrl(PLATFORM_COMPARISON_RESPONSE, {
      periods: [
        trendPeriod('2026-06', '21.00%', '19.50%'),
        trendPeriod('2026-07', '21.50%', '19.80%'),
        trendPeriod('2026-08', '22.06%', '20.00%'),
      ],
    })
    renderPage()

    const panel = await screen.findByRole('region', { name: 'Effective rate trend' })
    expect(panel).toBeInTheDocument()
    // role="group", not role="img": role="img" forbids focusable
    // descendants, which would make EffectiveRateTrendChart's per-point
    // role="button" markers unreachable to assistive tech.
    expect(screen.getByRole('group', { name: /effective commission rate trend/i })).toBeInTheDocument()
  })

  it('omits the effective-rate trend chart with fewer than 2 real trailing months, never a single-point chart (FR-013)', async () => {
    stubFetchByUrl(PLATFORM_COMPARISON_RESPONSE, {
      periods: [trendPeriod('2026-08', '22.06%', '20.00%')],
    })
    renderPage()

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
    renderPage()

    await screen.findAllByText('iFood')
    expect(screen.queryByRole('region', { name: 'Effective rate trend' })).not.toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('narrows the chart and table to platforms matching the search, and clears back', async () => {
    stubFetch(PLATFORM_COMPARISON_RESPONSE)
    renderPage()

    await screen.findAllByText('iFood')
    expect(screen.getAllByText('Just Eat Takeaway').length).toBeGreaterThan(0)

    await userEvent.type(screen.getByLabelText('Search platforms'), 'ifood{Enter}')

    expect(screen.getAllByText('iFood').length).toBeGreaterThan(0)
    expect(screen.queryByText('Just Eat Takeaway')).not.toBeInTheDocument()
    const summary = screen.getByText('1 of 2 platforms shown')
    expect(summary).toBeInTheDocument()
    // Matching Home/Promotions/Points (FilterBar's shared resultSummary):
    // the count must live in an aria-live region, not just be visible text,
    // so a screen-reader user hears it change as they filter.
    expect(summary).toHaveAttribute('aria-live', 'polite')

    await userEvent.click(screen.getByRole('button', { name: /clear filters/i }))
    expect(screen.getAllByText('Just Eat Takeaway').length).toBeGreaterThan(0)
  })

  it('labels the header chip "Overall" once the search narrows the chart/table, matching PromotionsPage\'s convention', async () => {
    stubFetch(PLATFORM_COMPARISON_RESPONSE)
    renderPage()

    await screen.findAllByText('iFood')
    expect(screen.queryByText('Overall')).not.toBeInTheDocument()

    await userEvent.type(screen.getByLabelText('Search platforms'), 'ifood{Enter}')

    // "2 platforms compared" stays the honest total of every platform on
    // file even though the table/chart now show just 1 — the label makes
    // that an intentional overview, not a stale or disagreeing count.
    expect(screen.getByText('Overall')).toBeInTheDocument()
    expect(screen.getByText('2 platforms compared')).toBeInTheDocument()
  })

  it('shows a reassuring empty state when the platform search matches nothing', async () => {
    stubFetch(PLATFORM_COMPARISON_RESPONSE)
    renderPage()

    await screen.findAllByText('iFood')
    await userEvent.type(
      screen.getByLabelText('Search platforms'),
      'no-such-platform{Enter}',
    )

    expect(
      screen.getByText('No platforms match this search.'),
    ).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })
})
