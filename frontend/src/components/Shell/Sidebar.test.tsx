import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createMemoryRouter, Outlet, RouterProvider } from 'react-router-dom'

import Sidebar from './Sidebar'

const EMPTY_PROFILE = {
  name: '',
  address: '',
  phone: '',
  email: '',
  description: '',
  photo: null,
  updated_at: '',
}

/** Every test renders `Sidebar`, which now reads `GET /api/profile` (the
 * fix for the "shown throughout the app" copy bug) — stub it the same way
 * `Profile/ProfilePage.test.tsx` and `Points/PointsCard.test.tsx` stub their
 * own data fetch, defaulting to "no profile saved yet" so pre-existing
 * nav-only assertions keep seeing the sidebar's original, unchanged shape.
 */
function stubProfile(body: unknown = EMPTY_PROFILE) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({ ok: true, json: async () => body }),
  )
}

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
          { path: 'profile', element: <p>Profile content</p> },
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
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders all four nav items with their labels', () => {
    stubProfile()
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
    stubProfile()
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
    stubProfile()
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
    stubProfile()
    const user = userEvent.setup()
    renderSidebarAt('/')

    await user.click(screen.getByRole('link', { name: /today's close/i }))

    expect(screen.getByText('Close content')).toBeInTheDocument()
    expect(screen.queryByText('Home content')).not.toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: /today's close/i }),
    ).toHaveAttribute('aria-current', 'page')
  })

  it('renders a Profile nav item linking to /profile', async () => {
    stubProfile()
    const user = userEvent.setup()
    renderSidebarAt('/')
    const nav = getNav()

    const profileLink = within(nav).getByRole('link', { name: /profile/i })
    expect(profileLink).toBeInTheDocument()
    expect(profileLink).toHaveAttribute('href', '/profile')

    await user.click(profileLink)

    expect(screen.getByText('Profile content')).toBeInTheDocument()
    expect(profileLink).toHaveAttribute('aria-current', 'page')
  })

  // Bug fix: the Profile page told the owner their profile is "shown
  // throughout the app" while nothing outside Profile itself ever read
  // GET /api/profile. The sidebar is now the surface that makes that copy
  // true — these two tests are the sidebar half of the fix.
  it('shows the saved restaurant name once GET /api/profile resolves', async () => {
    stubProfile({ ...EMPTY_PROFILE, name: 'Trattoria Bellavista' })
    renderSidebarAt('/')

    expect(await screen.findByText('Trattoria Bellavista')).toBeInTheDocument()
  })

  it('renders nothing extra when no restaurant name has been saved yet', async () => {
    stubProfile(EMPTY_PROFILE)
    renderSidebarAt('/')

    // Give the profile fetch a turn to resolve, then confirm it produced no
    // name text anywhere in the sidebar — a fresh install must look exactly
    // as it did before this feature existed, never a blank placeholder row.
    await screen.findByRole('navigation', { name: /primary navigation/i })
    expect(screen.queryByLabelText(/^restaurant:/i)).not.toBeInTheDocument()
  })
})
