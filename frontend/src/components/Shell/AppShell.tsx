import { useEffect, useRef, useState } from 'react'
import { Outlet, useLocation, useOutletContext } from 'react-router-dom'

import CostPanel, { type CostInteraction } from '@/components/CostPanel/CostPanel'
import FullscreenToggle from '@/components/Shell/FullscreenToggle'
import Sidebar, { MobileNavBar } from '@/components/Shell/Sidebar'
import SplashScreen from '@/components/Shell/SplashScreen'
import { postJson } from '@/lib/api'
import { useSpendLedger } from '@/lib/useSpendLedger'

/**
 * What a routed page can read of the shell-level running-cost total.
 *
 * `CostPanel` is mounted once here (outside the router outlet, per
 * redesign-spec.md §1/§6) rather than per-page, so no route owns cost state
 * or duplicates the pill.
 *
 * There is deliberately no `logInteractions` any more. It used to be how
 * `/ask` reported spend upward, and it was the mechanism behind the QA
 * finding that the total resets to $0.000 on reload while the answers that
 * cost the money are still on screen: the report was a side effect into
 * component state, so it lived and died with a mount. Spend is now written
 * durably by the same commit that writes the answer it paid for
 * (`chatStorage.recordSpend`) and read back here, which gives the total one
 * definition instead of a persistent half and an ephemeral half.
 */
export interface ShellOutletContext {
  interactions: CostInteraction[]
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
/** The routed content region's id — the skip link's target and the one
 * landmark a keyboard user should be able to reach in a single tab/Enter,
 * per WCAG 2.4.1 (Bypass Blocks). */
const MAIN_CONTENT_ID = 'main-content'

/**
 * `bottom-4` (16px) — the same inset `CostPanel`'s own fixed positioning
 * uses — so the reserved clearance's edge lines up with the pill's actual
 * edge instead of an arbitrary extra gap.
 */
const COST_PANEL_INSET_PX = 16

/**
 * Pre-measurement default for `costPanelHeight` below: the panel's real
 * collapsed height (a `text-xs`/`text-sm` row inside `px-3 py-1.5`), so
 * there's no visible jump in `<main>`'s bottom padding on first paint —
 * the same "match the pre-measurement steady state" discipline
 * `ChatPanel`'s own `composerHeight` default comment documents.
 */
const DEFAULT_COST_PANEL_HEIGHT_PX = 32

export default function AppShell() {
  // Durable and cross-tab consistent, not per-mount: see useSpendLedger.
  const interactions = useSpendLedger()
  const mainRef = useRef<HTMLElement>(null)
  const costPanelRef = useRef<HTMLDivElement>(null)
  const { pathname } = useLocation()

  // Reported live (QA pass): the fixed `CostPanel` sits in the bottom-right
  // corner of the viewport on every route, and grows a LOT taller the
  // instant its detail box opens (~40px collapsed to ~180px+ expanded). On
  // `/ask` that taller footprint sat directly on top of the composer's Send
  // button — `elementFromPoint` at the button's own center returned the
  // cost panel, not the button, so it was genuinely unclickable while
  // expanded. On `/promotions` at a narrow viewport, ordinary scrolled
  // content (a filter chip) landed under the same fixed footprint.
  //
  // The fix reserves real clearance at the bottom of the routed content
  // region, sized to the panel's ACTUAL measured height (not a guessed
  // constant, matching this codebase's own `ChatPanel` composerHeight
  // pattern) — so nothing this shell renders can ever scroll or lay out
  // underneath the pill, collapsed or expanded. `<main>`'s own `flex-1
  // min-h-0` means shrinking its content-box height via this padding also
  // pushes up any `h-full` page (the chat composer included) rather than
  // just adding dead space to a page that scrolls, so this one fix covers
  // both reported cases instead of needing a page-specific patch.
  const [costPanelHeight, setCostPanelHeight] = useState(DEFAULT_COST_PANEL_HEIGHT_PX)
  useEffect(() => {
    const node = costPanelRef.current
    if (!node || typeof ResizeObserver === 'undefined') return

    const observer = new ResizeObserver((entries) => {
      const height = entries[0]?.borderBoxSize?.[0]?.blockSize ?? node.offsetHeight
      setCostPanelHeight(Math.ceil(height))
    })
    observer.observe(node)
    return () => observer.disconnect()
  }, [])

  // The remaining edge case the padding reservation above doesn't cover on
  // its own: a page taller than the viewport, scrolled all the way down,
  // where the owner THEN expands the pill. `<main>`'s `scrollHeight` grows
  // the instant the reserved padding grows, but a browser never re-clamps
  // an already-set `scrollTop` to a newly bigger max — so without this, the
  // page stays exactly as scrolled as it was, the fixed panel grows upward
  // from the viewport's bottom edge exactly as before, and the two can
  // still meet in a corner. Mirrors `ChatPanel`'s own "stay pinned to the
  // bottom" tracking (`isPinnedToBottom`/`BOTTOM_STICK_THRESHOLD_PX`) so the
  // same discipline applies here: only an owner who was ALREADY at the
  // bottom gets re-pinned when the panel resizes — someone reading content
  // higher up the page never has their scroll position yanked.
  //
  // Measured live: this relies on `<main>` reading a FRESH `scrollHeight`
  // the instant its padding-bottom changes, which requires `<main>` to have
  // no transition on that property. Nothing here authors one — but this
  // app's own `prefers-reduced-motion` rule (index.css) sets
  // `transition-duration: 0.01ms !important` on literally every element to
  // collapse EXISTING transitions, and every element also carries the
  // CSS-initial `transition-property: all` that nothing ever overrides to
  // `none`. That combination quietly hands `<main>` a genuine transition on
  // ITS `padding-bottom` for exactly the reduced-motion users this app's
  // own accessibility rule exists to serve — `scrollHeight` read even a
  // full animation frame later was observed still returning the
  // PRE-transition box size. `transition-none` on `<main>` below (a real
  // layout reservation, never a decorative value) opts it out of that
  // default so this reads correctly regardless of the visitor's motion
  // preference.
  const [isMainPinnedToBottom, setIsMainPinnedToBottom] = useState(false)
  useEffect(() => {
    const main = mainRef.current
    if (!main) return

    function handleScroll() {
      if (!main) return
      const distanceFromBottom = main.scrollHeight - main.scrollTop - main.clientHeight
      setIsMainPinnedToBottom(distanceFromBottom <= 1)
    }

    handleScroll()
    main.addEventListener('scroll', handleScroll, { passive: true })
    return () => main.removeEventListener('scroll', handleScroll)
  }, [])

  useEffect(() => {
    if (!isMainPinnedToBottom) return
    const main = mainRef.current
    if (!main) return
    main.scrollTop = main.scrollHeight
  }, [costPanelHeight, isMainPinnedToBottom])

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
      {/* WCAG 2.4.1 (Bypass Blocks): every route puts ~10 nav links ahead of
          the actual page content in tab order. Must be the very first
          focusable element in the DOM — visually hidden until it receives
          focus (Tailwind's sr-only / focus:not-sr-only pair), so it costs a
          sighted mouse user nothing and gives a keyboard user a one-tab exit
          straight to `<main>`. */}
      <a
        href={`#${MAIN_CONTENT_ID}`}
        className="sr-only focus:not-sr-only focus:fixed focus:top-4 focus:left-4 focus:z-50
          focus:rounded-md focus:border focus:border-border focus:bg-background focus:px-4
          focus:py-2 focus:text-sm focus:font-medium focus:text-foreground focus:shadow-lg
          focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
      >
        Skip to main content
      </a>
      <SplashScreen />
      <FullscreenToggle />
      <Sidebar />
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        <MobileNavBar />
        <main
          ref={mainRef}
          id={MAIN_CONTENT_ID}
          // -1, not omitted: this only needs to be a valid target for the
          // skip link's programmatic focus() (so a screen reader/keyboard
          // user's next Tab starts from here, not back at the top of the
          // nav) — never a stop in ordinary Tab order, which -1 guarantees.
          tabIndex={-1}
          className="min-h-0 flex-1 overflow-y-auto px-4 py-6 outline-none transition-none sm:px-6 lg:px-8"
          // Reserves real clearance under every route for the fixed
          // CostPanel's actual measured height (see the effect above) —
          // never a guess — so a page's own scrolled content or an h-full
          // page's own bottom-anchored controls (the chat composer's Send
          // button) can never end up underneath it, collapsed or expanded.
          style={{ paddingBottom: costPanelHeight + COST_PANEL_INSET_PX * 2 }}
        >
          <Outlet context={{ interactions } satisfies ShellOutletContext} />
        </main>
      </div>
      <CostPanel ref={costPanelRef} interactions={interactions} />
    </div>
  )
}
