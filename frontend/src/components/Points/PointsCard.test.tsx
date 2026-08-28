import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import PointsCard from './PointsCard'

function stubBadgesResponse(body: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({ ok: true, json: async () => body }),
  )
}

const EARNED_RESPONSE = {
  badges: [
    { date: '2026-08-01', code: 'clean_close' },
    { date: '2026-08-02', code: 'clean_close' },
    { date: '2026-08-03', code: 'discrepancy_catcher' },
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
    render(<PointsCard />)

    expect(
      await screen.findByRole('status', {
        name: '45 Steward Points from 3 days already reconciled',
      }),
    ).toBeInTheDocument()
    expect(screen.getByText('2 × Clean Close')).toBeInTheDocument()
    expect(screen.getByText('1 × Discrepancy Catcher')).toBeInTheDocument()
    // The balance must be auditable: per-line subtotals are shown, not just
    // a total the reader has to take on faith.
    expect(screen.getByText(/\+20/)).toBeInTheDocument()
    expect(screen.getByText(/\+25/)).toBeInTheDocument()
  })

  it('states plainly that redemption is not built, and offers nothing to click', async () => {
    stubBadgesResponse(EARNED_RESPONSE)
    render(<PointsCard />)

    await screen.findByRole('status')
    expect(
      screen.getByText(/where this is going — not built yet/i),
    ).toBeInTheDocument()
    expect(
      screen.getByText(/no redemption flow in this prototype/i),
    ).toBeInTheDocument()
    // No fake affordance: a disabled "Redeem" button would still imply the
    // feature exists somewhere behind it.
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })

  it('shows a real zero rather than a placeholder when nothing has been reconciled', async () => {
    stubBadgesResponse({ badges: [], points: { total: 0, breakdown: [] } })
    render(<PointsCard />)

    expect(
      await screen.findByRole('status', {
        name: '0 Steward Points from 0 days already reconciled',
      }),
    ).toBeInTheDocument()
    expect(screen.getByText(/no closes on file yet/i)).toBeInTheDocument()
    expect(
      screen.getByText(/nothing here is awarded for signing up/i),
    ).toBeInTheDocument()
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
    render(<PointsCard />)

    expect(
      await screen.findByText(/couldn't reach the reconciliation engine/i),
    ).toBeInTheDocument()
    expect(screen.getByText(/query_failed/)).toBeInTheDocument()
    // A failed fetch must never fall back to a zero balance — that reads as
    // "you have earned nothing", a different and false statement.
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })
})
