import {
  CalendarCheck,
  Coins,
  HelpCircle,
  LayoutGrid,
  Megaphone,
  MessagesSquare,
  Scale,
  Settings,
  UploadCloud,
  UserCircle,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { NavLink } from 'react-router-dom'

import Logo from '@/components/Logo/Logo'
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

/**
 * Small-viewport (`< lg`) replacement for the sidebar: the same four nav
 * items as horizontal icon pills, so the IA stays fully reachable without a
 * hidden drawer (§2.3 — no Sheet/Drawer primitive exists in `components/ui`
 * yet, and adding one is out of scope here).
 */
export function MobileNavBar() {
  return (
    <nav
      aria-label="Primary navigation"
      className="flex items-center gap-1 overflow-x-auto border-b border-border bg-card/50 py-2 pl-3 pr-16 lg:hidden"
    >
      <Logo variant="icon" size={28} doorAnimation="once" />
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
  )
}
