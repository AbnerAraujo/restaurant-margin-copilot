import { useEffect, useState } from 'react'
import {
  ArrowRight,
  CalendarCheck,
  Coins,
  Megaphone,
  MessagesSquare,
  ShieldCheck,
  type LucideIcon,
} from 'lucide-react'
import { Link } from 'react-router-dom'

import { POINTS_PER_BADGE } from '@/components/Points/pointValues'
import { usePoints } from '@/components/Points/usePoints'
import { Chip, PageContainer, PageHeader, Panel } from '@/components/ui/page'
import { Stat, StatGroup, StatSkeleton } from '@/components/ui/stat'
import { getJson } from '@/lib/api'
import { cn } from '@/lib/utils'

/**
 * How many days the "Recent closes" table lists. Seven is a week, which is
 * the window an owner actually reasons about; the full period stays one click
 * away on `/close` rather than being duplicated here.
 */
const RECENT_CLOSE_ROWS = 7

interface DaySummaryApi {
  date: string
  margin: string
  discrepancy_flags: { type: string; detail: string }[]
}

interface ReconciliationApiResponse {
  start: string
  end: string
  days: DaySummaryApi[]
}

function formatUsd(decimal: string): string {
  return Number(decimal).toLocaleString('en-US', {
    style: 'currency',
    currency: 'USD',
  })
}

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
    description: 'Margin, badges, and the rows behind every figure.',
    earns: 'clean_close',
  },
  {
    to: '/ask',
    icon: MessagesSquare,
    title: 'Ask about your margin',
    description: 'A grounded answer, or an honest refusal.',
    earns: null,
    roadmapCategory: 'Engagement points',
  },
  {
    to: '/promotions',
    icon: Megaphone,
    title: 'Promotion ROI',
    description: "Which campaigns paid for themselves, and which we won't guess at.",
    earns: null,
    roadmapCategory: 'Campaign points',
  },
]

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
      <p className="mt-4 border-t border-border pt-3 text-micro text-muted-foreground">
        <span className="font-medium">{tile.roadmapCategory}</span>: roadmap,
        not earning yet
      </p>
    )
  }

  // The Close tile is the whole Reconciliation category: both badge types
  // are earned by closing a day, so its balance is the full total.
  const total = data?.points.total ?? null

  return (
    <p className="mt-4 flex flex-wrap items-baseline gap-x-2 border-t border-border pt-3 text-micro text-muted-foreground">
      <span className="font-medium text-foreground">
        {total === null ? '—' : total.toLocaleString('en-US')} pts earned
      </span>
      <span>
        next close: +{POINTS_PER_BADGE.clean_close} clean, +
        {POINTS_PER_BADGE.discrepancy_catcher} if it catches something
      </span>
    </p>
  )
}

/**
 * `/` — the owner's entry point.
 *
 * What changed and why: this route used to open with `PointsCard`, a roughly
 * 250-word explainer that filled the first viewport and pushed all three
 * actual entry points below the fold. A launcher whose first screen is an
 * essay about a loyalty scheme is the "too much text, too little application"
 * complaint in one component. The full points pitch now lives on `/points`,
 * which is the surface whose job it is, and `/` leads with the numbers the
 * owner opened the app to see.
 *
 * Every figure in the strip is read live: margin and flag counts from
 * GET /api/reconciliation, the balance from GET /api/badges through
 * `usePoints`. Nothing is a placeholder, and nothing is summed here — the
 * margin shown is the latest day's own value as the engine computed it, and
 * the counts are counts of records, not arithmetic on money.
 */
export default function HomePage() {
  const { data: pointsData } = usePoints()
  const [reconciliation, setReconciliation] =
    useState<ReconciliationApiResponse | null>(null)

  useEffect(() => {
    let cancelled = false
    getJson<ReconciliationApiResponse>('/api/reconciliation')
      .then((response) => {
        if (!cancelled) setReconciliation(response)
      })
      // A failed fetch leaves the strip in its skeleton state rather than
      // rendering zeroes, which would read as real reconciled figures.
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [])

  const days = reconciliation?.days ?? []
  const latest = days[days.length - 1]
  const flaggedDays = days.filter((day) => day.discrepancy_flags.length > 0)
  const margin = latest ? Number(latest.margin) : 0

  return (
    <PageContainer className="flex flex-col gap-5">
      <PageHeader
        eyebrow="My Business Steward"
        title="Daily margin, reconciled"
        meta={
          reconciliation ? (
            <Chip icon={CalendarCheck}>
              {reconciliation.start} to {reconciliation.end}
            </Chip>
          ) : null
        }
      />

      <Panel aria-label="At a glance" className="p-5 sm:p-6">
        {latest ? (
          <StatGroup>
            <Stat
              label="Latest margin"
              value={formatUsd(latest.margin)}
              size="lg"
              tone={margin < 0 ? 'negative' : 'positive'}
              caption={latest.date}
            />
            <Stat
              label="Days reconciled"
              value={String(days.length)}
              icon={CalendarCheck}
            />
            <Stat
              label="Days with a flag"
              value={String(flaggedDays.length)}
              icon={ShieldCheck}
              caption={
                flaggedDays.length === 0
                  ? 'Nothing caught'
                  : 'Caught before you paid'
              }
            />
            <Stat
              label="Steward points"
              value={
                pointsData
                  ? pointsData.points.total.toLocaleString('en-US')
                  : null
              }
              unavailableLabel="Loading"
              icon={Coins}
              footer={
                <Link
                  to="/points"
                  className="text-xs font-medium text-primary underline-offset-4 hover:underline focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
                >
                  How points work
                </Link>
              }
            />
          </StatGroup>
        ) : (
          <StatGroup>
            <StatSkeleton size="lg" />
            <StatSkeleton />
            <StatSkeleton />
            <StatSkeleton />
          </StatGroup>
        )}
      </Panel>

      {/* Recent closes. The same GET /api/reconciliation payload the strip
          above reads, listed newest first: date, the day's own margin exactly
          as the engine computed it, and whether anything was flagged. Nothing
          is aggregated and nothing is a placeholder — a page whose lower two
          thirds were empty read as unfinished, and the honest way to fill it
          is with data the app already holds. */}
      {days.length > 0 ? (
        <Panel className="overflow-hidden">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border p-5 sm:px-6">
            <h2 className="text-sm font-semibold tracking-tight text-foreground">
              Recent closes
            </h2>
            <Link
              to="/close"
              className="text-xs font-medium text-primary underline-offset-4 hover:underline focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
            >
              Open Today&apos;s Close
            </Link>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full min-w-[30rem] border-collapse text-left">
              <thead>
                <tr className="border-b border-border">
                  <th
                    scope="col"
                    className="px-5 py-2.5 text-micro font-medium uppercase tracking-wider text-muted-foreground sm:px-6"
                  >
                    Date
                  </th>
                  <th
                    scope="col"
                    className="px-5 py-2.5 text-micro font-medium uppercase tracking-wider text-muted-foreground"
                  >
                    Status
                  </th>
                  <th
                    scope="col"
                    className="px-5 py-2.5 text-right text-micro font-medium uppercase tracking-wider text-muted-foreground sm:px-6"
                  >
                    Margin
                  </th>
                </tr>
              </thead>
              <tbody>
                {[...days]
                  .reverse()
                  .slice(0, RECENT_CLOSE_ROWS)
                  .map((day) => {
                    const dayMargin = Number(day.margin)
                    const flagged = day.discrepancy_flags.length > 0
                    return (
                      <tr
                        key={day.date}
                        className="border-b border-border last:border-b-0"
                      >
                        <th
                          scope="row"
                          className="px-5 py-3 text-sm font-medium tabular-nums text-foreground sm:px-6"
                        >
                          {day.date}
                        </th>
                        <td className="px-5 py-3">
                          {flagged ? (
                            <Chip icon={ShieldCheck} tone="warning">
                              {day.discrepancy_flags.length}{' '}
                              {day.discrepancy_flags.length === 1
                                ? 'flag'
                                : 'flags'}
                            </Chip>
                          ) : (
                            <Chip tone="success">Clean</Chip>
                          )}
                        </td>
                        <td
                          className={cn(
                            'px-5 py-3 text-right text-sm font-semibold tabular-nums sm:px-6',
                            dayMargin < 0
                              ? 'text-destructive-text'
                              : 'text-success-text',
                          )}
                        >
                          {formatUsd(day.margin)}
                        </td>
                      </tr>
                    )
                  })}
              </tbody>
            </table>
          </div>
        </Panel>
      ) : null}

      <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(min(17rem,100%),1fr))]">
        {CAPABILITY_TILES.map((tile) => {
          const { to, icon: Icon, title, description } = tile
          return (
            <Link
              key={to}
              to={to}
              // Hover is a surface and border step, not a lift. A card that
              // translates and grows a shadow on hover is the generic-SaaS
              // tell the taste doctrine bans; the linear-app interaction model
              // moves luminance, not geometry.
              className="group flex flex-col rounded-xl border border-border bg-card p-5
                transition-colors hover:border-primary/40 hover:bg-accent
                focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
            >
              <div className="flex items-center justify-between">
                <span className="flex size-9 items-center justify-center rounded-md bg-primary/10 text-primary">
                  <Icon className="size-4.5" aria-hidden="true" />
                </span>
                <ArrowRight
                  className="size-4 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-primary motion-reduce:transition-none motion-reduce:group-hover:translate-x-0"
                  aria-hidden="true"
                />
              </div>
              <h2 className="mt-4 text-sm font-semibold tracking-tight text-foreground">
                {title}
              </h2>
              <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
                {description}
              </p>
              <div className="mt-auto">
                <TilePoints tile={tile} />
              </div>
            </Link>
          )
        })}
      </div>
    </PageContainer>
  )
}
