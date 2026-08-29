import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import AppShell from '@/components/Shell/AppShell'

// Reported live: opening a new page kept whatever scroll position was left
// on the previous one. AppShell's <main> is the one real scroll container
// in this app (see its own doc comment — <html>/<body>/window never scroll)
// — this proves route changes reset ITS scrollTop, isolated from any real
// page's own data fetching (two trivial stub routes, not the real route
// table) so the test only exercises the scroll-reset behavior itself.
function renderShellAt(initialPath: string) {
  const router = createMemoryRouter(
    [
      {
        path: '/',
        element: <AppShell />,
        children: [
          { index: true, element: <h1>Page One</h1> },
          { path: 'two', element: <h1>Page Two</h1> },
        ],
      },
    ],
    { initialEntries: [initialPath] },
  )
  return { ...render(<RouterProvider router={router} />), router }
}

describe('AppShell scroll-to-top on navigation', () => {
  it("resets <main>'s scrollTop to 0 when the route changes", async () => {
    const { router } = renderShellAt('/')
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Page One' })).toBeInTheDocument()
    })

    const main = document.querySelector('main')
    expect(main).not.toBeNull()

    // Simulate the owner having scrolled down before navigating away.
    Object.defineProperty(main, 'scrollTop', { value: 400, writable: true })
    expect(main?.scrollTop).toBe(400)

    await router.navigate('/two')
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Page Two' })).toBeInTheDocument()
    })

    expect(main?.scrollTop).toBe(0)
  })
})

// WCAG 2.4.1 (Bypass Blocks): every route puts the sidebar's ~10 nav links
// ahead of the actual page content in tab order. This proves the skip link
// is real (reachable in one Tab, targets a genuine element) rather than
// just visually present — the thing an owner tabbing through the app would
// actually rely on.
describe('AppShell skip-to-content link', () => {
  it('is the very first focusable element, reachable in a single Tab', async () => {
    const user = userEvent.setup()
    renderShellAt('/')
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Page One' })).toBeInTheDocument()
    })

    await user.tab()

    const skipLink = screen.getByRole('link', { name: /skip to main content/i })
    expect(document.activeElement).toBe(skipLink)
  })

  it('points at the id of <main>, the routed content region', async () => {
    renderShellAt('/')
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Page One' })).toBeInTheDocument()
    })

    const main = document.querySelector('main')
    expect(main).toHaveAttribute('id', 'main-content')
    expect(screen.getByRole('link', { name: /skip to main content/i })).toHaveAttribute(
      'href',
      '#main-content',
    )
  })
})
