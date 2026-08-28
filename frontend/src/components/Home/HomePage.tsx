import { ArrowRight, CalendarCheck, Megaphone, MessagesSquare, type LucideIcon } from 'lucide-react'
import { Link } from 'react-router-dom'

interface CapabilityTile {
  /** Route this tile navigates to. */
  to: string
  icon: LucideIcon
  title: string
  description: string
}

// Order matches the sidebar's nav order (redesign-spec.md §2.2/§3.3) so the
// home grid and the sidebar teach the same left-to-right/top-to-bottom mental
// map. `/` itself is not a tile — this grid is what `/` renders.
const CAPABILITY_TILES: CapabilityTile[] = [
  {
    to: '/close',
    icon: CalendarCheck,
    title: "Today's Close",
    description:
      "Today's margin, reconciliation badges, and the provenance behind the number.",
  },
  {
    to: '/ask',
    icon: MessagesSquare,
    title: 'Ask about your margin',
    description:
      'Natural-language questions about your numbers — a grounded answer, or an honest refusal.',
  },
  {
    to: '/promotions',
    icon: Megaphone,
    title: 'Promotion ROI',
    description:
      "Which campaigns paid for themselves, which didn't, and which we won't guess at.",
  },
]

/**
 * The entire home route (`/`): a grid of capability tiles built on
 * `BadgeDisplay`'s pill/banner visual DNA, scaled up into real navigation
 * targets — gamification doubling as navigation, not achievement decoration.
 * No chat box, no chart, no card of numbers here; each tile is a `Link` to
 * the route that renders that capability's real content.
 */
export default function HomePage() {
  return (
    <div className="mx-auto grid max-w-3xl grid-cols-1 gap-4 sm:grid-cols-2">
      {CAPABILITY_TILES.map(({ to, icon: Icon, title, description }) => (
        <Link
          key={to}
          to={to}
          className="group flex flex-col gap-3 rounded-lg border border-border bg-card
            p-5 text-left shadow-sm transition-all
            hover:-translate-y-0.5 hover:border-primary/30 hover:shadow-md
            focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
        >
          <div className="flex items-center justify-between">
            <span className="flex size-10 items-center justify-center rounded-full bg-primary/10 text-primary">
              <Icon className="size-5" aria-hidden="true" />
            </span>
            <ArrowRight
              className="size-4 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-primary"
              aria-hidden="true"
            />
          </div>
          <div>
            <h2 className="text-base font-semibold tracking-tight text-foreground">
              {title}
            </h2>
            <p className="mt-1 text-sm leading-relaxed text-muted-foreground">
              {description}
            </p>
          </div>
        </Link>
      ))}
    </div>
  )
}
