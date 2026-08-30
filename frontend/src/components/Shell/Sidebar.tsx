import {
  CalendarCheck,
  Coins,
  HelpCircle,
  LayoutGrid,
  Megaphone,
  MessagesSquare,
  Scale,
  Settings,
  Store,
  UploadCloud,
  UserCircle,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { NavLink } from 'react-router-dom'

import Logo from '@/components/Logo/Logo'
import { useProfile } from '@/components/Profile/useProfile'
import { cn } from '@/lib/utils'

/**
 * One entry per route, in the same order the home tile grid uses (§2.2/§3.3
 * of redesign-spec.md) so the sidebar and the home tiles teach the same
 * mental map of the app's capabilities.
 */
interface NavItem {
  to: string
  label: string
  icon: LucideIcon
  /** Only `/` should match exactly — every other route also matches nested paths. */
  end?: boolean
}

const NAV_ITEMS: NavItem[] = [
  { to: '/', label: 'Home', icon: LayoutGrid, end: true },
  { to: '/close', label: "Today's Close", icon: CalendarCheck },
  { to: '/upload', label: 'Upload costs', icon: UploadCloud },
  { to: '/ask', label: 'Ask', icon: MessagesSquare },
  { to: '/promotions', label: 'Promotions', icon: Megaphone },
  { to: '/platforms', label: 'Platforms', icon: Scale },
  { to: '/points', label: 'Points', icon: Coins },
  { to: '/profile', label: 'Profile', icon: UserCircle },
  { to: '/settings', label: 'Settings', icon: Settings },
  { to: '/help', label: 'Help', icon: HelpCircle },
]

const INACTIVE_LINK_CLASSES =
  'text-muted-foreground hover:bg-accent hover:text-foreground'

// Brand accent only — never a semantic-status token (--success/--warning/
// --destructive) here. Those are reserved for reconciliation state inside a
// page; the active nav indicator must never be read as "this page has fired
// a status." See redesign-spec.md §2.2.
const ACTIVE_LINK_CLASSES = 'bg-primary/10 text-primary'

const LINK_BASE_CLASSES =
  'flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors ' +
  'focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50'

const MOBILE_LINK_BASE_CLASSES =
  'flex shrink-0 items-center gap-1.5 rounded-full px-3 py-1.5 text-xs font-medium ' +
  'text-muted-foreground transition-colors ' +
  'focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50'

/**
 * The saved restaurant's own name/photo (`GET /api/profile`, saved via
 * `/profile`) — distinct from the `Logo` mark above it, which is this
 * PRODUCT's own brand ("My Business Steward") and never changes per
 * restaurant. This is what makes the Profile page's "shown throughout the
 * app" copy actually true (previously it was not: nothing outside the
 * Profile page itself read `GET /api/profile` at all). Renders nothing
 * until the owner has saved a name — a fresh install's sidebar looks
 * exactly as it did before this existed, never a placeholder inviting
 * "add your restaurant" nag here (that ask already lives on `/profile`
 * itself).
 */
function RestaurantIdentity({ compact = false }: { compact?: boolean }) {
  const { data } = useProfile()
  const name = data?.name?.trim()
  if (!name) return null

  const avatar = (
    <div
      className={cn(
        'flex shrink-0 items-center justify-center overflow-hidden rounded-full border border-border bg-muted/40',
        compact ? 'size-6' : 'size-7',
      )}
    >
      {data?.photo ? (
        <img src={data.photo} alt="" className="size-full object-cover" />
      ) : (
        <Store className={compact ? 'size-3' : 'size-3.5'} aria-hidden="true" />
      )}
    </div>
  )

  if (compact) {
    // Mobile icon bar: avatar only, no room for a name beside the nav pills
    // without crowding them — the full name still shows in the desktop
    // sidebar and on /profile itself.
    return (
      <span title={name} aria-label={`Restaurant: ${name}`}>
        {avatar}
      </span>
    )
  }

  return (
    <div className="flex items-center gap-2 border-b border-border px-5 py-3">
      {avatar}
      <span className="truncate text-xs font-medium text-foreground" title={name}>
        {name}
      </span>
    </div>
  )
}

/**
 * Fixed-width (w-60/240px) left sidebar, Toqan-inspired: logo/workspace
 * lockup at top, four nav items below, active-route highlighted with the
 * brand accent. Hidden below the `lg` breakpoint — see `MobileNavBar` for
 * the small-viewport replacement (§2.3), rendered separately by `AppShell`
 * so it lands in the correct spot in the shell's flex layout (above `<main>`,
 * not beside the aside).
 */
export default function Sidebar() {
  return (
    <aside
      className="hidden lg:sticky lg:top-0 lg:flex lg:h-screen lg:w-60 lg:shrink-0
        lg:flex-col lg:border-r lg:border-border lg:bg-card/50"
    >
      <div className="flex items-center border-b border-border px-5 py-5">
        <Logo doorAnimation="once" />
      </div>

      <RestaurantIdentity />

      <nav
        aria-label="Primary navigation"
        className="flex flex-1 flex-col gap-1 overflow-y-auto px-3 py-4"
      >
        {NAV_ITEMS.map(({ to, label, icon: Icon, end }) => (
          <NavLink
            key={to}
            to={to}
            end={end}
            className={({ isActive }) =>
              cn(LINK_BASE_CLASSES, isActive ? ACTIVE_LINK_CLASSES : INACTIVE_LINK_CLASSES)
            }
          >
            <Icon className="size-4 shrink-0" aria-hidden="true" />
            <span>{label}</span>
          </NavLink>
        ))}
      </nav>
    </aside>
  )
}

// SCROLL_EDGE_SLACK_PX tolerates the sub-pixel rounding a browser's own
// scroll math can leave behind (scrollWidth/clientWidth are already rounded
// integers, but scrollLeft can land at e.g. 639.6 on a display with a
// fractional device pixel ratio) — without it, "has this been scrolled all
// the way to the end" could stay permanently false-negative on some
// displays, leaving the end-of-scroll fade stuck on even once nothing more
// is scrollable.
const SCROLL_EDGE_SLACK_PX = 1

/**
 * Small-viewport (`< lg`) replacement for the sidebar: the same nav items
 * as horizontal icon pills, so the IA stays fully reachable without a
 * hidden drawer (§2.3 — no Sheet/Drawer primitive exists in `components/ui`
 * yet, and adding one is out of scope here).
 *
 * Found live in a QA pass, at exactly the 375px/768px widths this ships to:
 * ten items (NAV_ITEMS above, plus the logo/restaurant-identity pills)
 * never fit in one screen's width, `overflow-x-auto` handles the overflow
 * without visually breaking the layout, but nothing about a plain
 * `overflow-x-auto` row tells a first-time visitor there IS more content to
 * the right — the row simply looks like it ends after "Upload costs," with
 * `Ask` (this product's own core Q&A feature), `Promotions`, `Platforms`,
 * `Points`, `Profile`, `Settings`, and `Help` invisible and undiscoverable
 * unless a visitor happens to swipe a bar that gives no visual reason to.
 * A right-edge fade (shown only while there is real unscrolled content
 * past the edge, per canScrollRight below — never a permanent decoration
 * that would misleadingly persist after the row is fully scrolled) is the
 * same affordance pattern browsers themselves use for overflowing tab
 * strips, applied here since this codebase has no shared scroll-fade
 * primitive yet to reuse.
 */
export function MobileNavBar() {
  const scrollerRef = useRef<HTMLElement>(null)
  const [canScrollRight, setCanScrollRight] = useState(false)

  useEffect(() => {
    const el = scrollerRef.current
    if (!el) return

    const updateFadeVisibility = () => {
      setCanScrollRight(el.scrollLeft + el.clientWidth < el.scrollWidth - SCROLL_EDGE_SLACK_PX)
    }

    updateFadeVisibility()
    el.addEventListener('scroll', updateFadeVisibility, { passive: true })
    // ResizeObserver, not just a mount-time check: rotating the device,
    // resizing a desktop window down into this breakpoint, or the profile
    // name (RestaurantIdentity) loading in after this first paints can all
    // change whether the row overflows at all, without ever firing a
    // 'scroll' event.
    const observer = new ResizeObserver(updateFadeVisibility)
    observer.observe(el)

    return () => {
      el.removeEventListener('scroll', updateFadeVisibility)
      observer.disconnect()
    }
  }, [])

  return (
    <div className="relative lg:hidden">
      <nav
        ref={scrollerRef}
        aria-label="Primary navigation"
        className="flex items-center gap-1 overflow-x-auto border-b border-border bg-card/50 py-2 pl-3 pr-16"
      >
        <Logo variant="icon" size={28} doorAnimation="once" />
        <RestaurantIdentity compact />
        {NAV_ITEMS.map(({ to, label, icon: Icon, end }) => (
          <NavLink
            key={to}
            to={to}
            end={end}
            className={({ isActive }) =>
              cn(MOBILE_LINK_BASE_CLASSES, isActive && ACTIVE_LINK_CLASSES)
            }
          >
            <Icon className="size-3.5" aria-hidden="true" />
            {label}
          </NavLink>
        ))}
      </nav>
      {canScrollRight && (
        <div
          aria-hidden="true"
          data-testid="mobile-nav-scroll-fade"
          className="pointer-events-none absolute inset-y-0 right-16 w-8 bg-gradient-to-r from-transparent to-card/50"
        />
      )}
    </div>
  )
}
