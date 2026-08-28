import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { createMemoryRouter, Outlet, RouterProvider } from 'react-router-dom'

import Sidebar from './Sidebar'

/**
 * `Sidebar` renders only the `<aside>` (no `<Outlet/>` of its own — see
 * `AppShell`, which places the mobile nav bar between the sidebar and the
 * outlet). Pairing it with a bare `<Outlet/>` here is the smallest layout
 * that still gives `<NavLink>` a real router location/outlet to navigate
 * against, without pulling in `AppShell` or the real page components.
 */
function TestShell() {
  return (
    <>
      <Sidebar />
      <Outlet />
    </>
  )
}

/**
 * A minimal route tree exercising just the sidebar plus one piece of routed
 * content per route, so highlighting and navigation can be asserted without
 * pulling in the real page components (built separately, per
 * redesign-spec.md's parallel-agent split).
 */
function renderSidebarAt(initialPath: string) {
  const router = createMemoryRouter(
    [
      {
        path: '/',
        element: <TestShell />,
        children: [
          { index: true, element: <p>Home content</p> },
          { path: 'close', element: <p>Close content</p> },
          { path: 'ask', element: <p>Ask content</p> },
          { path: 'promotions', element: <p>Promotions content</p> },
        ],
      },
    ],
    { initialEntries: [initialPath] },
  )
  // Sidebar itself has no <Outlet/> (AppShell owns that) — render it beside
  // a router that also mounts the matched route so `<NavLink>` still has a
  // real location to compute isActive against.
  return render(<RouterProvider router={router} />)
}

function getNav() {
  return screen.getByRole('navigation', { name: /primary navigation/i })
}

describe('Sidebar', () => {
  it('renders all four nav items with their labels', () => {
    renderSidebarAt('/')
    const nav = getNav()

    expect(within(nav).getByRole('link', { name: /home/i })).toBeInTheDocument()
    expect(
      within(nav).getByRole('link', { name: /today's close/i }),
    ).toBeInTheDocument()
    expect(within(nav).getByRole('link', { name: /^ask$/i })).toBeInTheDocument()
    expect(
      within(nav).getByRole('link', { name: /promotions/i }),
    ).toBeInTheDocument()
  })

  it('highlights the Home link as active at the root route, and no other link', () => {
    renderSidebarAt('/')
    const nav = getNav()

    expect(within(nav).getByRole('link', { name: /home/i })).toHaveAttribute(
      'aria-current',
      'page',
    )
    expect(
      within(nav).getByRole('link', { name: /today's close/i }),
    ).not.toHaveAttribute('aria-current')
    expect(within(nav).getByRole('link', { name: /^ask$/i })).not.toHaveAttribute(
      'aria-current',
    )
    expect(
      within(nav).getByRole('link', { name: /promotions/i }),
    ).not.toHaveAttribute('aria-current')
  })

  it('highlights the matching link (not Home) when on a nested route', () => {
    renderSidebarAt('/promotions')
    const nav = getNav()

    expect(
      within(nav).getByRole('link', { name: /promotions/i }),
    ).toHaveAttribute('aria-current', 'page')
    expect(within(nav).getByRole('link', { name: /home/i })).not.toHaveAttribute(
      'aria-current',
    )
  })

  it('navigates to the matching content when a nav item is clicked', async () => {
    const user = userEvent.setup()
    renderSidebarAt('/')

    await user.click(screen.getByRole('link', { name: /today's close/i }))

    expect(screen.getByText('Close content')).toBeInTheDocument()
    expect(screen.queryByText('Home content')).not.toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: /today's close/i }),
    ).toHaveAttribute('aria-current', 'page')
  })
})
