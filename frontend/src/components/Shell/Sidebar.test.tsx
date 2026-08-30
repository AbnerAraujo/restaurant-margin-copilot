import { fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createMemoryRouter, MemoryRouter, Outlet, RouterProvider } from 'react-router-dom'

import Sidebar, { MobileNavBar } from './Sidebar'

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

/**
 * QA-round-5 finding: ten nav items (the logo/restaurant-identity pills
 * plus every NAV_ITEMS entry) never fit at 375px/768px, `overflow-x-auto`
 * hides the excess without breaking layout, but a plain overflow row gives
 * a first-time mobile visitor no visual reason to believe `Ask` (this
 * product's own core feature),`Promotions`, `Platforms`, `Points`,
 * `Profile`, `Settings`, and `Help` exist just past the edge. These tests
 * cover the fix: a right-edge fade that appears only while there is real
 * unscrolled content, and disappears once scrolled to the end — never a
 * permanent decoration that would misleadingly persist either way.
 */
describe('MobileNavBar scroll-fade affordance', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  function renderMobileNavBar() {
    stubProfile()
    return render(
      <MemoryRouter>
        <MobileNavBar />
      </MemoryRouter>,
    )
  }

  function stubOverflow({ scrollWidth, clientWidth, scrollLeft = 0 }: { scrollWidth: number; clientWidth: number; scrollLeft?: number }) {
    vi.spyOn(HTMLElement.prototype, 'scrollWidth', 'get').mockReturnValue(scrollWidth)
    vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(clientWidth)
    vi.spyOn(HTMLElement.prototype, 'scrollLeft', 'get').mockReturnValue(scrollLeft)
  }

  it('renders every nav item, even though most are scrolled out of view', () => {
    renderMobileNavBar()
    const nav = getNav()

    for (const name of ['home', "today's close", '^ask$', 'promotions', 'platforms', 'points', 'profile', 'settings', 'help']) {
      expect(within(nav).getByRole('link', { name: new RegExp(name, 'i') })).toBeInTheDocument()
    }
  })

  it('shows no fade when every item already fits within the viewport', () => {
    stubOverflow({ scrollWidth: 300, clientWidth: 375 })
    renderMobileNavBar()
    getNav()

    expect(screen.queryByTestId('mobile-nav-scroll-fade')).not.toBeInTheDocument()
  })

  it('shows the fade when scrolled content extends past the right edge', () => {
    stubOverflow({ scrollWidth: 900, clientWidth: 375, scrollLeft: 0 })
    renderMobileNavBar()
    getNav()

    expect(screen.getByTestId('mobile-nav-scroll-fade')).toBeInTheDocument()
  })

  it('hides the fade once scrolled all the way to the end', () => {
    stubOverflow({ scrollWidth: 900, clientWidth: 375, scrollLeft: 0 })
    renderMobileNavBar()
    const nav = getNav()

    expect(screen.getByTestId('mobile-nav-scroll-fade')).toBeInTheDocument()

    // Re-stub scrollLeft as if the user scrolled all the way to the end,
    // then fire the same 'scroll' event the browser would — fireEvent
    // (unlike a raw dispatchEvent) wraps this in act() so the resulting
    // state update is flushed before the assertion below runs.
    vi.spyOn(HTMLElement.prototype, 'scrollLeft', 'get').mockReturnValue(525)
    fireEvent.scroll(nav)

    expect(screen.queryByTestId('mobile-nav-scroll-fade')).not.toBeInTheDocument()
  })
})
