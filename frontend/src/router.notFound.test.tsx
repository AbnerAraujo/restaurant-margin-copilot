import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { routes } from './router'

// A separate file from router.test.tsx on purpose: that file mocks
// `Shell/Sidebar` to throw (it exercises the app-shell crash boundary), which
// would make every render here look like a crash too. These tests need the
// REAL sidebar, since "the nav is still usable" is exactly what the 404 fix
// is supposed to prove.
beforeEach(() => {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }),
  )
  vi.spyOn(console, 'error').mockImplementation(() => undefined)
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('catch-all 404 route', () => {
  // Reported live: an unmatched path (a typo, a stale bookmark) had no
  // catch-all route, so it fell through to the root `errorElement` — the
  // WHOLE app shell (sidebar included) got replaced by the crash screen,
  // with no nav left to click back to anywhere real. This proves the fix:
  // a real page rendered INSIDE the shell, with working navigation intact.
  it('renders a 404 page inside the app shell, with the nav still usable', async () => {
    const router = createMemoryRouter(routes, {
      initialEntries: ['/nope-does-not-exist'],
    })
    render(<RouterProvider router={router} />)

    expect(
      await screen.findByRole('heading', { name: 'Page not found' }),
    ).toBeInTheDocument()

    // The sidebar/mobile nav bar is still rendered and usable, not replaced
    // by the crash screen — the actual bug this route exists to fix.
    expect(
      screen.getAllByRole('navigation', { name: 'Primary navigation' }).length,
    ).toBeGreaterThan(0)

    const homeLink = screen.getAllByRole('link', { name: 'Go to Home' })[0]
    expect(homeLink).toHaveAttribute('href', '/')
  })

  // The other half of the reported bug: a 404 fired two POSTs to
  // `/api/client-errors`, polluting real error telemetry with what is just
  // a typo'd URL. A 404 is expected user behavior, not an application
  // fault, and must never report there.
  it('does not report to /api/client-errors — a 404 is expected behavior, not a crash', async () => {
    const router = createMemoryRouter(routes, {
      initialEntries: ['/nope-does-not-exist'],
    })
    render(<RouterProvider router={router} />)

    await screen.findByRole('heading', { name: 'Page not found' })

    expect(fetch).not.toHaveBeenCalledWith(
      expect.stringContaining('/api/client-errors'),
      expect.anything(),
    )
  })

  it('lets the owner navigate to a real page from the 404 (Home link works)', async () => {
    const router = createMemoryRouter(routes, {
      initialEntries: ['/nope-does-not-exist'],
    })
    render(<RouterProvider router={router} />)

    await screen.findByRole('heading', { name: 'Page not found' })

    const user = userEvent.setup()
    await user.click(screen.getAllByRole('link', { name: 'Go to Home' })[0])

    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/')
    })
    expect(screen.queryByRole('heading', { name: 'Page not found' })).not.toBeInTheDocument()
  })
})
