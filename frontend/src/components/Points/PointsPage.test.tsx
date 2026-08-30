import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import PointsPage from './PointsPage'

// PointsPage renders PointsCard (which links to /promotions) — needs a
// Router in scope, same as PointsCard.test.tsx and PromotionsPage.test.tsx.
function renderPage() {
  return render(
    <MemoryRouter>
      <PointsPage />
    </MemoryRouter>,
  )
}

const BADGES_RESPONSE = {
  badges: [],
  points: { total: 0, breakdown: [], spent: 0, available: 0 },
}

/**
 * PointsPage fetches TWO endpoints on mount: GET /api/badges (usePoints,
 * via PointsCard and the rules table) and GET /api/promotions (spec 008
 * FR-014's new redemption history). Dispatch by URL rather than assuming a
 * single shared response body, the same discipline
 * LogReplacementForm.test.tsx's findPromotionsPostCall comment establishes
 * for this codebase once fetch mocks need to distinguish real endpoints.
 */
function stubFetch(promotionsBody: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string) => {
      if (String(url).includes('/api/promotions')) {
        return Promise.resolve({
          ok: true,
          json: async () => promotionsBody,
        })
      }
      return Promise.resolve({ ok: true, json: async () => BADGES_RESPONSE })
    }),
  )
}

describe('PointsPage redemption history (spec 008 FR-014)', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('lists every points-paid promotion, newest first, with campaign/date/points', async () => {
    stubFetch({
      promotions: [
        {
          campaign_id: 'IFOOD-CAMP-EARLY',
          platform: 'iFood',
          period: { start: '2026-08-01', end: '2026-08-07' },
          payment_method: 'points',
          points_spent: 500,
        },
        {
          campaign_id: 'JET-CAMP-LATE',
          platform: 'Just Eat Takeaway',
          period: { start: '2026-08-10', end: '2026-08-14' },
          payment_method: 'points',
          points_spent: 1200,
        },
        // Paid with money — must never appear in the redemption history.
        {
          campaign_id: 'IFOOD-CAMP-CASH',
          platform: 'iFood',
          period: { start: '2026-08-12', end: '2026-08-13' },
          payment_method: 'money',
        },
      ],
    })
    renderPage()

    const list = await screen.findByText('JET-CAMP-LATE')
    const section = list.closest('ul') as HTMLElement

    expect(within(section).getByText('IFOOD-CAMP-EARLY')).toBeInTheDocument()
    expect(within(section).queryByText('IFOOD-CAMP-CASH')).not.toBeInTheDocument()

    // Newest (2026-08-10) before oldest (2026-08-01).
    const rows = within(section).getAllByRole('listitem')
    expect(rows[0]).toHaveTextContent('JET-CAMP-LATE')
    expect(rows[1]).toHaveTextContent('IFOOD-CAMP-EARLY')

    expect(within(rows[0]).getByText('−1,200 pts')).toBeInTheDocument()
    expect(within(rows[1]).getByText('−500 pts')).toBeInTheDocument()
  })

  it('states plainly when no points redemptions exist yet, never a bare empty table', async () => {
    stubFetch({ promotions: [] })
    renderPage()

    expect(
      await screen.findByText(/no points redemptions yet/i),
    ).toBeInTheDocument()
    expect(screen.queryByRole('list')).not.toBeInTheDocument()
  })

  it('excludes a money-paid promotion entirely when it is the only promotion on file', async () => {
    stubFetch({
      promotions: [
        {
          campaign_id: 'IFOOD-CAMP-CASH',
          platform: 'iFood',
          period: { start: '2026-08-12', end: '2026-08-13' },
          payment_method: 'money',
        },
      ],
    })
    renderPage()

    expect(
      await screen.findByText(/no points redemptions yet/i),
    ).toBeInTheDocument()
  })

  it('narrows the redemption list to matches, and clears back to the full list', async () => {
    stubFetch({
      promotions: [
        {
          campaign_id: 'IFOOD-CAMP-EARLY',
          platform: 'iFood',
          period: { start: '2026-08-01', end: '2026-08-07' },
          payment_method: 'points',
          points_spent: 500,
        },
        {
          campaign_id: 'JET-CAMP-LATE',
          platform: 'Just Eat Takeaway',
          period: { start: '2026-08-10', end: '2026-08-14' },
          payment_method: 'points',
          points_spent: 1200,
        },
      ],
    })
    renderPage()

    const list = await screen.findByText('JET-CAMP-LATE')
    const section = list.closest('ul') as HTMLElement
    expect(within(section).getByText('IFOOD-CAMP-EARLY')).toBeInTheDocument()

    await userEvent.selectOptions(
      screen.getByLabelText('Filter by platform'),
      'Just Eat Takeaway',
    )

    expect(screen.getByText('JET-CAMP-LATE')).toBeInTheDocument()
    expect(screen.queryByText('IFOOD-CAMP-EARLY')).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: /clear filters/i }))
    expect(screen.getByText('IFOOD-CAMP-EARLY')).toBeInTheDocument()
  })

  it('shows a reassuring empty state — never a dead end — when the filter matches nothing', async () => {
    stubFetch({
      promotions: [
        {
          campaign_id: 'IFOOD-CAMP-EARLY',
          platform: 'iFood',
          period: { start: '2026-08-01', end: '2026-08-07' },
          payment_method: 'points',
          points_spent: 500,
        },
      ],
    })
    renderPage()

    await screen.findByText('IFOOD-CAMP-EARLY')
    await userEvent.type(
      screen.getByLabelText('Search redemption history'),
      'no-such-campaign',
    )

    expect(
      screen.getByText('No redemptions match these filters.'),
    ).toBeInTheDocument()
    expect(screen.queryByText('IFOOD-CAMP-EARLY')).not.toBeInTheDocument()
  })
})

// Bug fix: the "Earned" column of the rules table hardcoded "day(s)" for
// every rule, so two replacement campaigns logged on the same calendar day
// rendered as "Campaign Launcher ... 2 days" — Campaign Launcher and Growth
// are counted per CAMPAIGN, not per day. Only Clean Close/Discrepancy
// Catcher are genuinely day-counted.
describe('PointsPage rules table — earned-count units (bug fix)', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  function stubBadgesOnly(breakdown: unknown[]) {
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string) => {
        if (String(url).includes('/api/promotions')) {
          return Promise.resolve({ ok: true, json: async () => ({ promotions: [] }) })
        }
        return Promise.resolve({
          ok: true,
          json: async () => ({
            badges: [],
            points: { total: 0, breakdown, spent: 0, available: 0 },
          }),
        })
      }),
    )
  }

  it('labels a day-counted rule (Clean Close) with "day(s)"', async () => {
    stubBadgesOnly([
      { code: 'clean_close', name: 'Clean Close', count: 1, points_each: 10, points: 10 },
    ])
    renderPage()

    expect(await screen.findByText('1 day')).toBeInTheDocument()
  })

  it('labels Campaign Launcher with "campaign(s)", never "day(s)", even for two same-day replacements', async () => {
    stubBadgesOnly([
      {
        code: 'campaign_creation',
        name: 'Campaign Launcher',
        count: 2,
        points_each: 30,
        points: 60,
      },
    ])
    renderPage()

    expect(await screen.findByText('2 campaigns')).toBeInTheDocument()
    expect(screen.queryByText('2 days')).not.toBeInTheDocument()
  })

  it('labels Growth with "campaign(s)", never "day(s)"', async () => {
    stubBadgesOnly([
      { code: 'growth', name: 'Growth', count: 3, points_each: 15, points: 45 },
    ])
    renderPage()

    expect(await screen.findByText('3 campaigns')).toBeInTheDocument()
  })

  it('labels Week One with "milestone", never "day"', async () => {
    stubBadgesOnly([
      { code: 'engagement', name: 'Week One', count: 1, points_each: 5, points: 5 },
    ])
    renderPage()

    expect(await screen.findByText('1 milestone')).toBeInTheDocument()
  })
})
