import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

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
  it('renders exactly the three capability tiles as real links, not decorative divs', () => {
    renderHomePageWithRoutes()

    const links = screen.getAllByRole('link')
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

    expect(
      screen.getByText(/reconciliation badges, and the provenance/i),
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
