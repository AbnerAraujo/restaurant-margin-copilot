import { useEffect, useRef, useState } from 'react'
import { Outlet, useLocation, useOutletContext } from 'react-router-dom'

import CostPanel, { type CostInteraction } from '@/components/CostPanel/CostPanel'
import FullscreenToggle from '@/components/Shell/FullscreenToggle'
import Sidebar, { MobileNavBar } from '@/components/Shell/Sidebar'
import SplashScreen from '@/components/Shell/SplashScreen'
import { postJson } from '@/lib/api'

/**
 * What a routed page can read/do with the shell-level running-cost total.
 * `CostPanel` is mounted once here (outside the router outlet, per
 * redesign-spec.md §1/§6) rather than per-page, so a page that logs new
 * interactions — today only `/ask`'s chat panel — reports them upward
 * through `useOutletContext` instead of each route owning its own cost
 * state and duplicating the pill.
 */
export interface ShellOutletContext {
  interactions: CostInteraction[]
  logInteractions: (newInteractions: CostInteraction[]) => void
}

/** Convenience hook for a routed page to read/report shell-level cost state. */
export function useShellOutletContext() {
  return useOutletContext<ShellOutletContext>()
}

/**
 * App shell: fixed left sidebar (desktop) / top icon bar (mobile) beside a
 * routed content area, with the session cost pill pinned at the shell root
 * so it stays visible across every route. Per redesign-spec.md §2.1 — the
 * root stays a row at `lg`+ (aside beside content) and stacks to a column
 * below it (mobile nav bar above `<main>`, not beside it).
 */
export default function AppShell() {
  const [interactions, setInteractions] = useState<CostInteraction[]>([])
  const mainRef = useRef<HTMLElement>(null)
  const { pathname } = useLocation()

  // Reported live: opening a new page kept whatever scroll position was
  // left on the PREVIOUS page instead of starting at the top. React
  // Router's own <ScrollRestoration> doesn't fit here — it manages
  // `window.scrollTo`, but this shell's `<html>`/`<body>`/`window` never
  // scroll at all (see the h-screen/overflow-hidden comment below); `<main>`
  // is the one real scroll container, so this resets ITS scrollTop instead,
  // once per route change. A plain assignment, not smooth-scroll: a fresh
  // page should just start at the top, not visibly animate there.
  useEffect(() => {
    if (mainRef.current) {
      mainRef.current.scrollTop = 0
    }
  }, [pathname])

  const logInteractions = (newInteractions: CostInteraction[]) => {
    setInteractions((previous) => [...previous, ...newInteractions])
  }

  // The real usage-event ping backing Engagement badges (spec
  // 002-badge-expansion, FR-003). Fired once per mount of the SHELL, not per
  // routed page: `<Outlet>` swaps children as the owner navigates between
  // `/close`, `/ask`, `/promotions`, etc., but this component itself does
  // not remount on those transitions, so this effect runs once per real
  // app load/session — never once per page view, which would let ordinary
  // in-app navigation inflate "distinct days used" (plan.md's Frontend
  // changes: "not per page navigation"). The actual "never double-count a
  // calendar day" guarantee still lives entirely server-side (a unique
  // index on a generated column, migrations/000003) — this effect firing
  // more than once (a hot reload, a second tab) is harmless by
  // construction, not by care taken here.
  useEffect(() => {
    postJson('/api/usage').catch(() => undefined)
  }, [])

  return (
    // h-screen + overflow-hidden, not min-h-screen: the shell owns exactly
    // the viewport and never grows a second, page-level scrollbar. <main> is
    // the one scroll container for tall pages (Home, Close, Promotions),
    // while a page that wants to fill the viewport instead — the chat — asks
    // for h-full and gets a definite height to resolve against. Before this,
    // the chat was a fixed 36rem letterbox floating in a 982px viewport with
    // ~382px of dead space beneath it, so only about one and a half messages
    // were ever visible at once.
    //
    // contain-layout: reported live as "the whole page scrolls wrongly, on
    // top of main's own scroll" on a data-heavy page (29 real promotions).
    // Root cause: this div's fixed-position children (FullscreenToggle,
    // CostPanel, SplashScreen) have no containing block of their own, so
    // they resolve against the true viewport/initial containing block —
    // and a real Chromium quirk lets a deeply nested, dynamically-sized
    // subtree (a long scrollable page with many rows) leak into how the
    // browser computes THAT viewport-relative scrollable area, growing
    // document.documentElement's own scrollHeight past main's intended
    // single scroll region. `contain: layout` makes this div itself the
    // containing block for those fixed descendants instead — a no-op
    // visually, since this div already exactly IS the viewport box
    // (h-screen, full width) — but stops the leak at its source rather
    // than chasing it page by page.
    <div className="contain-layout flex h-screen flex-col overflow-hidden bg-background lg:flex-row">
      <SplashScreen />
      <FullscreenToggle />
      <Sidebar />
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        <MobileNavBar />
        <main
          ref={mainRef}
          className="min-h-0 flex-1 overflow-y-auto px-4 py-6 sm:px-6 lg:px-8"
        >
          <Outlet context={{ interactions, logInteractions } satisfies ShellOutletContext} />
        </main>
      </div>
      <CostPanel interactions={interactions} />
    </div>
  )
}
