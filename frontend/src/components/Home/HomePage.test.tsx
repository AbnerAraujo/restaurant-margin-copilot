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

  it('names the specific flag(s) that fired on a given day, not just a generic explanation', async () => {
    stubFetchByUrl([
      {
        date: '2026-08-13',
        margin: '100.00',
        discrepancy_flags: [
          {
            type: 'duplicate_order_removed',
            detail: 'Duplicate order removed',
          },
        ],
      },
      {
        date: '2026-08-14',
        margin: '120.00',
        discrepancy_flags: [
          { type: 'refund', detail: 'Refund netted to sale date' },
          {
            type: 'anomaly_threshold_exceeded',
            detail: 'Margin outside the usual range',
          },
        ],
      },
    ])
    renderHomePageWithRoutes()

    await screen.findByText('Recent closes')

    const singleFlagHint = screen.getByRole('button', {
      name: 'What flagged 2026-08-13?',
    })
    await userEvent.hover(singleFlagHint)
    expect(
      await screen.findByText('1 duplicate order removed'),
    ).toBeInTheDocument()

    const multiFlagHint = screen.getByRole('button', {
      name: 'What flagged 2026-08-14?',
    })
    await userEvent.hover(multiFlagHint)
    expect(
      await screen.findByText(
        'An unusual change in revenue and 1 other item flagged',
      ),
    ).toBeInTheDocument()
  })

  it('summarizes many flags of the same type instead of joining each one\'s raw technical detail', async () => {
    // Reported live: a POS-heavy sync day can carry 40+ duplicate flags
    // alone, and this tooltip used to join every one's raw `detail`
    // sentence (internal type names, simulated:// provenance URIs, row
    // numbers) into one unreadable wall of text.
    const manyDuplicates = Array.from({ length: 32 }, (_, i) => ({
      type: 'cross_source_duplicate_removed',
      detail: `POS ticket POS-SIM-${i} carries iFood's own order reference...`,
    }))
    stubFetchByUrl([
      {
        date: '2026-08-30',
        margin: '4470.70',
        discrepancy_flags: manyDuplicates,
      },
    ])
    renderHomePageWithRoutes()

    await screen.findByText('Recent closes')
    await userEvent.hover(
      screen.getByRole('button', { name: 'What flagged 2026-08-30?' }),
    )
    expect(
      await screen.findByText('32 duplicates counted once'),
    ).toBeInTheDocument()
    expect(screen.queryByText(/POS-SIM-/)).not.toBeInTheDocument()
  })

  it('narrows the Recent closes table to flagged days only via the status chip', async () => {
    stubFetchByUrl([
      day('2026-08-10', '10.00'),
      flaggedDay('2026-08-11', '10.00'),
      day('2026-08-12', '10.00'),
    ])
    renderHomePageWithRoutes()

    const heading = await screen.findByText('Recent closes')
    const panel = heading.closest('section') as HTMLElement
    expect(within(panel).getByText('2026-08-10')).toBeInTheDocument()
    expect(within(panel).getByText('2026-08-11')).toBeInTheDocument()

    await userEvent.click(within(panel).getByRole('button', { name: 'Flagged' }))

    expect(within(panel).getByText('2026-08-11')).toBeInTheDocument()
    expect(within(panel).queryByText('2026-08-10')).not.toBeInTheDocument()
    expect(within(panel).queryByText('2026-08-12')).not.toBeInTheDocument()

    await userEvent.click(within(panel).getByRole('button', { name: /clear filters/i }))
    expect(within(panel).getByText('2026-08-10')).toBeInTheDocument()
  })

  it('narrows the Recent closes table by a Status column filter, composing with the page-level status chip rather than replacing it', async () => {
    stubFetchByUrl([
      day('2026-08-10', '10.00'),
      flaggedDay('2026-08-11', '10.00'),
      day('2026-08-12', '10.00'),
    ])
    renderHomePageWithRoutes()

    const heading = await screen.findByText('Recent closes')
    const panel = heading.closest('section') as HTMLElement
    expect(within(panel).getByText('2026-08-10')).toBeInTheDocument()
    expect(within(panel).getByText('2026-08-11')).toBeInTheDocument()

    // The filter popover renders in a portal outside `panel`'s DOM subtree
    // (same as `ColumnFilterButton`'s Radix `Popover.Portal`), so its
    // trigger is scoped to the panel but its content is queried at the
    // document level, matching CostSheetTab.test.tsx's own pattern.
    await userEvent.click(within(panel).getByRole('button', { name: /filter by status/i }))
    await userEvent.click(await screen.findByRole('checkbox', { name: 'Clean' }))

    // Only the two clean days remain — the flagged one is narrowed out by
    // the column filter, a SECOND surface on top of the (currently unset)
    // page-level status chips, not a replacement for them.
    expect(within(panel).getByText('2026-08-10')).toBeInTheDocument()
    expect(within(panel).getByText('2026-08-12')).toBeInTheDocument()
    expect(within(panel).queryByText('2026-08-11')).not.toBeInTheDocument()
    expect(within(panel).getByText('2 of 3 shown')).toBeInTheDocument()

    // Clearing via the shared "Clear filters" affordance resets the column
    // filter too, not just the page-level one.
    await userEvent.click(within(panel).getByRole('button', { name: /clear filters/i }))
    expect(within(panel).getByText('2026-08-11')).toBeInTheDocument()
  })

  it('narrows the Recent closes table by a Margin numeric column filter, composing with the page-level status chip', async () => {
    stubFetchByUrl([
      day('2026-08-10', '-25.00'),
      flaggedDay('2026-08-11', '50.00'),
      day('2026-08-12', '120.00'),
    ])
    renderHomePageWithRoutes()

    const heading = await screen.findByText('Recent closes')
    const panel = heading.closest('section') as HTMLElement

    // First narrow with the existing page-level status chip (Flagged), then
    // layer the column filter on top of THAT already-narrowed set.
    await userEvent.click(within(panel).getByRole('button', { name: 'Flagged' }))
    expect(within(panel).getByText('2026-08-11')).toBeInTheDocument()
    expect(within(panel).queryByText('2026-08-10')).not.toBeInTheDocument()
    expect(within(panel).queryByText('2026-08-12')).not.toBeInTheDocument()

    // Same portal caveat as the Status filter test above: the popover's
    // trigger is inside `panel`, its content is not.
    await userEvent.click(within(panel).getByRole('button', { name: /filter by margin/i }))
    const min = await screen.findByLabelText(/minimum, margin/i)
    await userEvent.type(min, '100')
    await userEvent.click(screen.getByRole('button', { name: 'Apply' }))

    // The one flagged day has a margin of 50, below the 100 minimum, so
    // composing both filters together narrows to nothing.
    expect(
      within(panel).getByText('No recent closes match these filters.'),
    ).toBeInTheDocument()
  })

  it('narrows the Recent closes table by date search, and shows a reassuring empty state for no match', async () => {
    stubFetchByUrl([day('2026-08-10', '10.00'), day('2026-08-11', '10.00')])
    renderHomePageWithRoutes()

    const heading = await screen.findByText('Recent closes')
    const panel = heading.closest('section') as HTMLElement

    await userEvent.type(
      within(panel).getByLabelText('Search recent closes by date'),
      '2026-08-99{Enter}',
    )

    expect(
      within(panel).getByText('No recent closes match these filters.'),
    ).toBeInTheDocument()
    expect(within(panel).queryByText('2026-08-10')).not.toBeInTheDocument()
  })
})
