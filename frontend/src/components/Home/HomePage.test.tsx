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
})
