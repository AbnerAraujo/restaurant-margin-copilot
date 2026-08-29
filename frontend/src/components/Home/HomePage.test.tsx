import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import HomePage from './HomePage'

/**
 * Renders `HomePage` inside a real router with stub destination routes, so
 * "clicking a tile navigates" is proven by an actual route change rather
 * than asserted from markup alone.
 */
function renderHomePageWithRoutes() {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <Routes>
        <Route path="/" element={<HomePage />} />
        <Route path="/close" element={<div>Today's Close page</div>} />
        <Route path="/ask" element={<div>Ask page</div>} />
        <Route path="/promotions" element={<div>Promotions page</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

const EMPTY_BADGES_RESPONSE = { badges: [], points: { total: 0, breakdown: [] } }

/**
 * Routes by URL so a test can supply real `/api/reconciliation` days
 * (for the trend-arrow/biggest-win-catch assertions) alongside the
 * `/api/badges` response every render of `HomePage` needs regardless —
 * a single flat mock would silently hand the badges body to both calls.
 */
function stubFetchByUrl(reconciliationDays: unknown[]) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string) => {
      if (String(url).includes('/api/reconciliation')) {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            start: reconciliationDays[0]
              ? (reconciliationDays[0] as { date: string }).date
              : '',
            end: reconciliationDays[reconciliationDays.length - 1]
              ? (reconciliationDays[reconciliationDays.length - 1] as {
                  date: string
                }).date
              : '',
            days: reconciliationDays,
          }),
        })
      }
      return Promise.resolve({ ok: true, json: async () => EMPTY_BADGES_RESPONSE })
    }),
  )
}

function day(date: string, margin: string) {
  return { date, margin, discrepancy_flags: [] }
}

function flaggedDay(date: string, margin: string) {
  return { date, margin, discrepancy_flags: [{ type: 'refund', detail: 'test' }] }
}

/** N consecutive days ending on 2026-08-14, oldest first — for exercising
 * the "At a glance" strip's 90-day window against a history much longer
 * than 90 real days, the way the live 744-day dataset now does. */
function consecutiveDays(count: number, flaggedIndexes: number[] = []) {
  const end = new Date('2026-08-14T00:00:00Z')
  return Array.from({ length: count }, (_, i) => {
    const d = new Date(end)
    d.setUTCDate(d.getUTCDate() - (count - 1 - i))
    const date = d.toISOString().slice(0, 10)
    return flaggedIndexes.includes(i)
      ? flaggedDay(date, '10.00')
      : day(date, '10.00')
  })
}

describe('HomePage', () => {
  // HomePage now mounts PointsCard, which fetches GET /api/badges. Stubbed
  // so these navigation assertions don't depend on a live backend; the card's
  // own behaviour is covered in PointsCard.test.tsx.
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ badges: [], points: { total: 0, breakdown: [] } }),
      }),
    )
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders exactly the three capability tiles as real links, not decorative divs', () => {
    renderHomePageWithRoutes()

    // Scoped to the capability grid specifically — the page also carries a
    // "View full breakdown" link to /points (the points-summary section)
    // that is not one of the three capability tiles this test is about.
    const capabilities = screen.getByRole('region', { name: 'Capabilities' })
    const links = within(capabilities).getAllByRole('link')
    expect(links).toHaveLength(3)
  })

  it.each([
    ["Today's Close", '/close'],
    ['Ask about your margin', '/ask'],
    ['Promotion ROI', '/promotions'],
  ])('tile "%s" is an <a> with href="%s"', (name, expectedHref) => {
    renderHomePageWithRoutes()

    const link = screen.getByRole('link', { name: new RegExp(name) })
    expect(link.tagName).toBe('A')
    expect(link).toHaveAttribute('href', expectedHref)
  })

  it('renders each tile description as visible text (not link-name-only)', () => {
    renderHomePageWithRoutes()

    // Descriptions were shortened from full sentences to one line each; the
    // guarantee under test is that they are real visible text rather than
    // only the link's accessible name, which is unchanged.
    expect(
      screen.getByText(/the rows behind every figure/i),
    ).toBeInTheDocument()
    expect(
      screen.getByText(/a grounded answer, or an honest refusal/i),
    ).toBeInTheDocument()
    expect(
      screen.getByText(/which we won't guess at/i),
    ).toBeInTheDocument()
  })

  it('clicking the "Today\'s Close" tile actually navigates to /close', async () => {
    const user = userEvent.setup()
    renderHomePageWithRoutes()

    await user.click(screen.getByRole('link', { name: /Today's Close/ }))

    expect(screen.getByText("Today's Close page")).toBeInTheDocument()
  })

  it('clicking the "Promotion ROI" tile actually navigates to /promotions', async () => {
    const user = userEvent.setup()
    renderHomePageWithRoutes()

    await user.click(screen.getByRole('link', { name: /Promotion ROI/ }))

    expect(screen.getByText('Promotions page')).toBeInTheDocument()
  })

  it('each tile link is keyboard-focusable (a real interactive element)', () => {
    renderHomePageWithRoutes()

    for (const link of screen.getAllByRole('link')) {
      link.focus()
      expect(link).toHaveFocus()
    }
  })

  it('never shows "roadmap, not earning yet" — every tile earns for real as of spec 002', async () => {
    renderHomePageWithRoutes()

    await screen.findAllByText(/pts earned/i)
    expect(screen.queryByText(/roadmap, not earning yet/i)).not.toBeInTheDocument()
  })

  it('shows each tile\'s OWN earned subtotal, not the grand total across all badge categories', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          badges: [],
          points: {
            total: 55,
            breakdown: [
              { code: 'clean_close', name: 'Clean Close', count: 1, points_each: 10, points: 10 },
              { code: 'engagement', name: 'Week One', count: 1, points_each: 5, points: 5 },
              { code: 'growth', name: 'Growth', count: 1, points_each: 15, points: 15 },
              {
                code: 'campaign_creation',
                name: 'Campaign Launcher',
                count: 1,
                points_each: 30,
                points: 30,
              },
            ],
          },
        }),
      }),
    )
    renderHomePageWithRoutes()

    const closeTile = (await screen.findByRole('link', { name: /Today's Close/ }))
    const askTile = screen.getByRole('link', { name: /Ask about your margin/ })
    const promoTile = screen.getByRole('link', { name: /Promotion ROI/ })

    // Close earns only clean_close+discrepancy_catcher here (10), not the
    // grand total (55).
    expect(within(closeTile).getByText(/^10 pts earned$/)).toBeInTheDocument()
    // Ask earns only engagement (5).
    expect(within(askTile).getByText(/^5 pts earned$/)).toBeInTheDocument()
    // Promotions earns growth + campaign_creation (15 + 30 = 45).
    expect(within(promoTile).getByText(/^45 pts earned$/)).toBeInTheDocument()
  })

  it('shows a real trend indicator against the previous reconciled day (spec 008 FR-008)', async () => {
    stubFetchByUrl([day('2026-08-13', '100.00'), day('2026-08-14', '150.00')])
    renderHomePageWithRoutes()

    expect(await screen.findByText(/\+\$50\.00 vs 2026-08-13/)).toBeInTheDocument()
  })

  it('omits the trend indicator with fewer than 2 reconciled days, never a placeholder (FR-013)', async () => {
    stubFetchByUrl([day('2026-08-14', '100.00')])
    renderHomePageWithRoutes()

    await screen.findAllByText('2026-08-14')
    expect(screen.queryByText(/vs 2026-08-13/)).not.toBeInTheDocument()
  })

  it('shows a "Biggest win / biggest catch" card scoped to the trailing 7 reconciled days (spec 008 FR-009)', async () => {
    stubFetchByUrl([
      day('2026-08-10', '50.00'),
      day('2026-08-11', '-30.00'),
      day('2026-08-12', '200.00'),
      day('2026-08-13', '10.00'),
      day('2026-08-14', '75.00'),
    ])
    renderHomePageWithRoutes()

    const panel = await screen.findByRole('region', {
      name: 'Biggest win and catch this week',
    })
    expect(within(panel).getByText('$200.00')).toBeInTheDocument()
    expect(within(panel).getByText('-$30.00')).toBeInTheDocument()
    // Fewer than 7 real days behind it — scoped honestly, not padded.
    expect(within(panel).getByText(/5 days reconciled/)).toBeInTheDocument()
  })

  it('omits the "Biggest win / biggest catch" card with fewer than 2 reconciled days (FR-013)', async () => {
    stubFetchByUrl([day('2026-08-14', '100.00')])
    renderHomePageWithRoutes()

    await screen.findAllByText('2026-08-14')
    expect(
      screen.queryByRole('region', { name: 'Biggest win and catch this week' }),
    ).not.toBeInTheDocument()
  })

  it('shows a real year-over-year tile when the exact same calendar days one year earlier exist (spec 008 FR-006)', async () => {
    stubFetchByUrl([
      day('2025-08-01', '100.00'),
      day('2025-08-02', '120.00'),
      day('2026-08-01', '150.00'),
      day('2026-08-02', '130.00'),
    ])
    renderHomePageWithRoutes()

    const panel = await screen.findByRole('region', { name: 'Year over year' })
    expect(within(panel).getByText('$280.00')).toBeInTheDocument() // this year: 150+130
    expect(within(panel).getByText('$220.00')).toBeInTheDocument() // last year: 100+120
    expect(within(panel).getByText('+$60.00')).toBeInTheDocument()
  })

  it('omits the year-over-year tile with no matching prior-year data, never a partial or fabricated figure (FR-013)', async () => {
    stubFetchByUrl([day('2026-08-01', '100.00'), day('2026-08-02', '130.00')])
    renderHomePageWithRoutes()

    await screen.findAllByText('2026-08-02')
    expect(screen.queryByRole('region', { name: 'Year over year' })).not.toBeInTheDocument()
  })

  it('scopes "Days reconciled" and "Days with a flag" to the trailing 90 days, not the full all-time history', async () => {
    // 100 real days, 2 flagged outside the trailing-90 window (days 0 and 5)
    // and 1 flagged inside it (day 95) — the stats must reflect only the 90
    // most recent days: 90 reconciled, 1 flagged, never 100 and 3.
    stubFetchByUrl(consecutiveDays(100, [0, 5, 95]))
    renderHomePageWithRoutes()

    const glance = await screen.findByRole('region', { name: 'At a glance' })
    expect(within(glance).getByText('90')).toBeInTheDocument()
    expect(within(glance).getByText('1')).toBeInTheDocument()
    expect(within(glance).getByText('last 90 days')).toBeInTheDocument()
  })

  it('shows the true, smaller count and an honest window label when fewer than 90 days of history exist', async () => {
    stubFetchByUrl(consecutiveDays(12, [3]))
    renderHomePageWithRoutes()

    const glance = await screen.findByRole('region', { name: 'At a glance' })
    expect(within(glance).getByText('12')).toBeInTheDocument()
    expect(within(glance).getByText('1')).toBeInTheDocument()
    expect(within(glance).getByText('last 12 days')).toBeInTheDocument()
    expect(within(glance).queryByText(/last 90 days/)).not.toBeInTheDocument()
  })

  it('offers a discoverable explanation of "Status" on the Recent closes table, not a bare unexplained label', async () => {
    stubFetchByUrl(consecutiveDays(5))
    renderHomePageWithRoutes()

    await screen.findByText('Recent closes')
    expect(
      screen.getByRole('button', { name: 'What does "Status" mean?' }),
    ).toBeInTheDocument()
  })
})
