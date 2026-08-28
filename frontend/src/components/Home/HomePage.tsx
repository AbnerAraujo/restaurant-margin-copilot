import {
  ArrowRight,
  CalendarCheck,
  Megaphone,
  MessagesSquare,
  type LucideIcon,
} from 'lucide-react'
import { Link } from 'react-router-dom'

import PointsCard from '@/components/Points/PointsCard'
import { POINTS_PER_BADGE } from '@/components/Points/pointValues'
import { usePoints } from '@/components/Points/usePoints'

interface CapabilityTile {
  /** Route this tile navigates to. */
  to: string
  icon: LucideIcon
  title: string
  description: string
  /**
   * Which badge code's points this area actually earns, or null when the
   * area earns none today.
   *
   * Null is the honest answer for Ask and Promotions: docs/product-strategy.md
   * scopes the Engagement and Campaign-Creation badge categories as roadmap,
   * and only the Reconciliation category is built. Showing a live-looking
   * "points earned" figure on an area that earns nothing would be inventing
   * a reward, which is the same class of fabrication as inventing a number.
   */
  earns: 'clean_close' | 'discrepancy_catcher' | null
  /** What the roadmap category would be called, when earns is null. */
  roadmapCategory?: string
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
    earns: 'clean_close',
  },
  {
    to: '/ask',
    icon: MessagesSquare,
    title: 'Ask about your margin',
    description:
      'Natural-language questions about your numbers — a grounded answer, or an honest refusal.',
    earns: null,
    roadmapCategory: 'Engagement points',
  },
  {
    to: '/promotions',
    icon: Megaphone,
    title: 'Promotion ROI',
    description:
      "Which campaigns paid for themselves, which didn't, and which we won't guess at.",
    earns: null,
    roadmapCategory: 'Campaign points',
  },
]

/**
 * The entire home route (`/`): a grid of capability tiles built on
 * `BadgeDisplay`'s pill/banner visual DNA, scaled up into real navigation
 * targets — gamification doubling as navigation, not achievement decoration.
 * No chat box, no chart, no card of numbers here; each tile is a `Link` to
 * the route that renders that capability's real content.
 */
/**
 * Per-tile points line: what this area has actually earned, and what the next
 * close there is worth. Reconciliation is the only category that earns today,
 * so the other two tiles say so plainly instead of showing a zero that would
 * read as "you have failed to earn any".
 */
function TilePoints({ tile }: { tile: CapabilityTile }) {
  const { data } = usePoints()

  if (tile.earns === null) {
    return (
      <p className="mt-3 border-t border-border/60 pt-2 text-[11px] text-muted-foreground">
        <span className="font-medium">{tile.roadmapCategory}</span> — roadmap,
        not earning yet
      </p>
    )
  }

  // The Close tile is the whole Reconciliation category: both badge types
  // are earned by closing a day, so its balance is the full total.
  const total = data?.points.total ?? null

  return (
    <p className="mt-3 flex flex-wrap items-baseline gap-x-2 border-t border-border/60 pt-2 text-[11px] text-muted-foreground">
      <span className="font-medium text-foreground">
        {total === null ? '—' : total.toLocaleString('en-US')} pts earned
      </span>
      <span>
        · next close: +{POINTS_PER_BADGE.clean_close} clean, +
        {POINTS_PER_BADGE.discrepancy_catcher} if it catches something
      </span>
    </p>
  )
}

export default function HomePage() {
  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-4">
      <PointsCard />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        {CAPABILITY_TILES.map((tile) => {
          const { to, icon: Icon, title, description } = tile
          return (
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
                <TilePoints tile={tile} />
              </div>
            </Link>
          )
        })}
      </div>
    </div>
  )
}
