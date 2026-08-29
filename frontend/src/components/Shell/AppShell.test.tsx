import { render, screen, waitFor } from '@testing-library/react'
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
