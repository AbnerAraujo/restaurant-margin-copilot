import { act, render, screen, waitFor } from '@testing-library/react'
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

    // Root cause of the reported flake: a bare `await router.navigate(...)`
    // outside `act()` races AppShell's own scroll-reset effect (see
    // AppShell.tsx around line 152 — a useEffect keyed on `pathname`, which
    // React flushes on a separate tick from the route change itself).
    // Wrapping the navigation in `act()` ensures React flushes every effect
    // the route change triggers — the scroll reset included — before this
    // proceeds, rather than only waiting for the new heading to appear and
    // hoping the effect already ran by then.
    await act(async () => {
      await router.navigate('/two')
    })
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Page Two' })).toBeInTheDocument()
    })

    // Asserted inside waitFor too, not just after it: belt-and-suspenders
    // against the same class of race even with the act() wrap above — a
    // flush ordering this test doesn't fully control should never turn into
    // a flaky failure here.
    await waitFor(() => {
      expect(main?.scrollTop).toBe(0)
    })
  })
})

// Reported live: the fix for the test above (always reset scrollTop on
// pathname change) broke the OPPOSITE case — pressing the browser's real
// Back/Forward button also always landed at the top, even though a POP
// navigation is exactly the case where a user expects their previous
// position back. These prove `useNavigationType()` tells PUSH and POP apart.
describe('AppShell scroll restore on POP navigation (browser Back/Forward)', () => {
  it('restores the scroll position a page had before the owner navigated away, on a real Back press', async () => {
    const { router } = renderShellAt('/')
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Page One' })).toBeInTheDocument()
    })

    const main = document.querySelector('main') as HTMLElement
    Object.defineProperty(main, 'scrollTop', { value: 0, writable: true })

    // Owner scrolls down on Page One, then navigates away (a real scroll
    // event, not just the property write, since AppShell's own recording
    // effect listens for `scroll`, matching how a real mouse-wheel scroll
    // behaves).
    main.scrollTop = 300
    main.dispatchEvent(new Event('scroll'))

    // A genuine new-page navigation (PUSH) — Page Two correctly starts at
    // the top, same as the test above.
    await act(async () => {
      await router.navigate('/two')
    })
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Page Two' })).toBeInTheDocument()
    })
    expect(main.scrollTop).toBe(0)

    // The browser's real Back button is a POP navigation. Page One's
    // previously recorded position (300) should come back — not 0, which is
    // what forcing the reset unconditionally used to produce.
    await act(async () => {
      await router.navigate(-1)
    })
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Page One' })).toBeInTheDocument()
    })
    await waitFor(() => {
      expect(main.scrollTop).toBe(300)
    })
  })

  it('leaves the scroll position alone on a POP navigation with nothing recorded for that page, rather than forcing it to 0', async () => {
    const { router } = renderShellAt('/')
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Page One' })).toBeInTheDocument()
    })

    const main = document.querySelector('main') as HTMLElement
    Object.defineProperty(main, 'scrollTop', { value: 0, writable: true })

    await act(async () => {
      await router.navigate('/two')
    })
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Page Two' })).toBeInTheDocument()
    })
    expect(main.scrollTop).toBe(0)

    // Simulate whatever the DOM update itself left `<main>` at — never
    // recorded via a real `scroll` event, so there is nothing cached for
    // '/' to restore.
    main.scrollTop = 75

    await act(async () => {
      await router.navigate(-1)
    })
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Page One' })).toBeInTheDocument()
    })

    // No recorded position for '/' — the fix's fallback is to leave this
    // untouched, not to force it back to 0.
    expect(main.scrollTop).toBe(75)
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

// QA-round-5 finding: <main> is overflow-y-auto only, no overflow-x set —
// per the CSS spec, that makes its COMPUTED overflow-x "auto" too (an
// element can't have overflow-y anything-but-visible and overflow-x
// visible at the same time: https://www.w3.org/TR/css-overflow-3/
// #overflow-properties), so any page whose content is even slightly wider
// than the viewport (found live: Close's Period date-range row not
// wrapping at 375px) quietly made <main> itself horizontally scrollable.
// That combination is worse than a plain visible overflow: focusing an
// off-canvas descendant (e.g. clicking/tabbing to a button pushed past the
// right edge) triggers the browser's native focus-follows-scroll behavior,
// shifting <main>'s scrollLeft and clipping the START of every other line
// on the page, with no scrollbar and no way back except undoing whatever
// focused the off-canvas element. jsdom has no layout engine, so this
// can't reproduce the overflow itself — it asserts the actual fix
// (overflow-x-hidden forecloses the CSS quirk regardless of what any given
// page renders), verified live with Playwright screenshots at 375px/768px.
describe('AppShell main content region', () => {
  it('sets overflow-x-hidden on <main>, so no page can make it horizontally scrollable', async () => {
    renderShellAt('/')
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Page One' })).toBeInTheDocument()
    })

    const main = document.querySelector('main')
    expect(main).toHaveClass('overflow-x-hidden')
    expect(main).toHaveClass('overflow-y-auto')
  })
})
