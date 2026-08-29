import { render, screen, within } from '@testing-library/react'
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
})
