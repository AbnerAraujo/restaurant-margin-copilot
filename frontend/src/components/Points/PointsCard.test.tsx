import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import PointsCard from './PointsCard'

// PointsCard now links to /promotions (the real, shipped points-payment
// redemption path) — it needs a Router in scope to render at all.
function renderCard() {
  return render(
    <MemoryRouter>
      <PointsCard />
    </MemoryRouter>,
  )
}

function stubBadgesResponse(body: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({ ok: true, json: async () => body }),
  )
}

const EARNED_RESPONSE = {
  badges: [
    { date: '2026-08-01', code: 'clean_close', name: 'Clean Close', category: 'reconciliation' },
    { date: '2026-08-02', code: 'clean_close', name: 'Clean Close', category: 'reconciliation' },
    {
      date: '2026-08-03',
      code: 'discrepancy_catcher',
      name: 'Discrepancy Catcher',
      category: 'reconciliation',
    },
  ],
  points: {
    total: 45,
    breakdown: [
      {
        code: 'clean_close',
        name: 'Clean Close',
        count: 2,
        points_each: 10,
        points: 20,
      },
      {
        code: 'discrepancy_catcher',
        name: 'Discrepancy Catcher',
        count: 1,
        points_each: 25,
        points: 25,
      },
    ],
  },
}

describe('PointsCard', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders the backend’s real derived total and the arithmetic behind it', async () => {
    stubBadgesResponse(EARNED_RESPONSE)
    renderCard()

    expect(
      await screen.findByRole('status', {
        name: '45 Steward Points available to spend, out of 45 earned from 3 days already reconciled',
      }),
    ).toBeInTheDocument()
    // The breakdown moved from a run-on sentence per line ("2 × Clean Close —
    // days closed with nothing out of place") to a labelled row with the rate
    // as a chip. The guarantee under test is unchanged and still asserted
    // here: every rule, its count, its rate, and its subtotal are all on
    // screen, so the balance is auditable rather than taken on faith.
    expect(screen.getByText('Clean Close')).toBeInTheDocument()
    expect(screen.getByText('Discrepancy Catcher')).toBeInTheDocument()
    expect(screen.getByText('2 × 10')).toBeInTheDocument()
    expect(screen.getByText('1 × 25')).toBeInTheDocument()
    expect(screen.getByText(/\+20/)).toBeInTheDocument()
    expect(screen.getByText(/\+25/)).toBeInTheDocument()
  })

  it('states plainly that redemption is live, and links to where it actually happens', async () => {
    stubBadgesResponse(EARNED_RESPONSE)
    renderCard()

    await screen.findByRole('status')
    // Redemption shipped (POST /api/promotions, payment_method: "points") —
    // the roadmap block now says so and links to the real place it happens,
    // rather than disclaiming it as not built.
    expect(screen.getByText(/^live$/i)).toBeInTheDocument()
    expect(
      screen.getByText(/pay its spend in points instead of cash/i),
    ).toBeInTheDocument()
    const link = screen.getByRole('link', { name: /log a promotion with points/i })
    expect(link).toHaveAttribute('href', '/promotions')
  })

  it('shows a real zero rather than a placeholder when nothing has been reconciled', async () => {
    stubBadgesResponse({ badges: [], points: { total: 0, breakdown: [] } })
    renderCard()

    expect(
      await screen.findByRole('status', {
        name: '0 Steward Points available to spend, out of 0 earned from 0 days already reconciled',
      }),
    ).toBeInTheDocument()
    expect(screen.getByText(/no closes on file yet/i)).toBeInTheDocument()
    // "Nothing is awarded for signing up" moved from the empty-state sentence
    // to the balance's own caption, where it now qualifies every reading of
    // the figure rather than only the zero case.
    expect(
      screen.getByText(/not awarded for signing up/i),
    ).toBeInTheDocument()
  })

  it('counts "days reconciled" from Reconciliation badges only, never a Growth/Engagement/Campaign-Creation badge', async () => {
    stubBadgesResponse({
      badges: [
        { date: '2026-08-01', code: 'clean_close', name: 'Clean Close', category: 'reconciliation' },
        { date: '2026-08-07', code: 'growth', name: 'Growth', category: 'growth', campaign_id: 'X' },
        { date: '2026-08-14', code: 'engagement', name: 'Week One', category: 'engagement', usage_days: 7 },
      ],
      points: {
        total: 25,
        breakdown: [
          { code: 'clean_close', name: 'Clean Close', count: 1, points_each: 10, points: 10 },
          { code: 'growth', name: 'Growth', count: 1, points_each: 15, points: 15 },
        ],
      },
    })
    renderCard()

    // 1 Reconciliation badge among the 3 total — "days reconciled" must read
    // 1, not 3, even though the total point balance (25) reflects all of them.
    expect(
      await screen.findByRole('status', {
        name: '25 Steward Points available to spend, out of 25 earned from 1 day already reconciled',
      }),
    ).toBeInTheDocument()
  })

  it('shows the available balance after redemption, not the bare earned total', async () => {
    stubBadgesResponse({
      badges: EARNED_RESPONSE.badges,
      points: { total: 45, breakdown: EARNED_RESPONSE.points.breakdown, spent: 10, available: 35 },
    })
    renderCard()

    expect(
      await screen.findByRole('status', {
        name: '35 Steward Points available to spend, out of 45 earned from 3 days already reconciled',
      }),
    ).toBeInTheDocument()
    expect(screen.getByText(/45 earned . 10 redeemed/i)).toBeInTheDocument()
  })

  it('surfaces a backend failure instead of rendering a balance it does not have', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        text: async () => 'query_failed',
      }),
    )
    renderCard()

    expect(
      await screen.findByText(/couldn't reach your data/i),
    ).toBeInTheDocument()
    expect(screen.getByText(/query_failed/)).toBeInTheDocument()
    // A failed fetch must never fall back to a zero balance — that reads as
    // "you have earned nothing", a different and false statement.
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })
})
