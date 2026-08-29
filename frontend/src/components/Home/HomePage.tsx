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

import CompositionBar from '@/components/Points/CompositionBar'
import { POINTS_PER_BADGE } from '@/components/Points/pointValues'
import { usePoints, type BadgeCode, type PointsLine } from '@/components/Points/usePoints'
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
   * Which badge code(s) this area earns points from, summed for the tile's
   * own subtotal (never `data.points.total`, which is now the sum across
   * ALL FIVE codes since spec 002-badge-expansion — reading the grand total
   * here would overstate what closing/asking/promoting each specifically
   * earns).
   *
   * Every tile earns something real as of spec 002: Ask earns Engagement
   * (opening the app on real, distinct days) and Promotions earns Growth +
   * Campaign-Creation (a profitable campaign, or replacing a flagged one).
   * Before this spec, Ask/Promotions were the honest "roadmap, not earning
   * yet" case — that label would now itself be a fabrication in the other
   * direction (spec 002 FR-011), so the roadmap branch this component used
   * to have is gone, not merely relabelled.
   */
  earns: BadgeCode[]
  /** Forward-looking "what's next" copy shown under the earned subtotal. */
  nextHint: string
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
    earns: ['clean_close', 'discrepancy_catcher'],
    nextHint: `next close: +${POINTS_PER_BADGE.clean_close} clean, +${POINTS_PER_BADGE.discrepancy_catcher} if it catches something`,
  },
  {
    to: '/ask',
    icon: MessagesSquare,
    title: 'Ask about your margin',
    description: 'A grounded answer, or an honest refusal.',
    earns: ['engagement'],
    nextHint: `+${POINTS_PER_BADGE.engagement} pts for a new real day used — "Week One" at 7 distinct days`,
  },
  {
    to: '/promotions',
    icon: Megaphone,
    title: 'Promotion ROI',
    description: "Which campaigns paid for themselves, and which we won't guess at.",
    earns: ['growth', 'campaign_creation'],
    nextHint: `+${POINTS_PER_BADGE.growth} per profitable campaign, +${POINTS_PER_BADGE.campaign_creation} for replacing a flagged one`,
  },
]

/** Sums only the breakdown lines for this tile's own codes — never
 * `points.total`, which is the sum across all five codes as of spec 002. */
function subtotalFor(breakdown: PointsLine[], codes: BadgeCode[]): number {
  return breakdown
    .filter((line) => codes.includes(line.code))
    .reduce((sum, line) => sum + line.points, 0)
}

/**
 * Per-tile points line: what THIS area specifically has earned (its own
 * codes' subtotal, per the tile's `earns` list) and what's next there. Every
 * tile earns for real as of spec 002-badge-expansion — there is no more
 * "roadmap, not earning yet" case to render.
 */
function TilePoints({ tile }: { tile: CapabilityTile }) {
  const { data } = usePoints()

  const subtotal = data ? subtotalFor(data.points.breakdown, tile.earns) : null

  return (
    <p className="mt-4 flex flex-wrap items-baseline gap-x-2 border-t border-border pt-3 text-micro text-muted-foreground">
      <span className="font-medium text-foreground">
        {subtotal === null ? '—' : subtotal.toLocaleString('en-US')} pts earned
      </span>
      <span>{tile.nextHint}</span>
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
              tooltip="A flag means the reconciliation engine caught something worth a second look on that day — a duplicate order, a refund, or a number outside the usual range. It's already been caught and accounted for, not an open problem waiting on you."
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

      {/* Points summary. A dedicated section rather than folding this into
          the "At a glance" stat above — that stat answers "how many", this
          answers "from what". Same CompositionBar component as the full
          `/points` page (shared, not duplicated) so the two surfaces can
          never disagree about how a balance is drawn; the roadmap
          disclosure stays off this page and lives only on `/points`. */}
      <Panel aria-label="Points summary" className="overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border p-5 sm:px-6">
          <h2 className="text-sm font-semibold tracking-tight text-foreground">
            Points
          </h2>
          <Link
            to="/points"
            className="text-xs font-medium text-primary underline-offset-4 hover:underline focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
          >
            View full breakdown
          </Link>
        </div>
        <div className="p-5 sm:p-6">
          {pointsData ? (
            pointsData.points.total > 0 ? (
              <CompositionBar
                breakdown={pointsData.points.breakdown}
                total={pointsData.points.total}
                showBlurb={false}
              />
            ) : (
              <p className="text-sm text-muted-foreground">
                No closes on file yet. Run a day&apos;s reconciliation and the
                first points land immediately.
              </p>
            )
          ) : (
            <p className="text-sm text-muted-foreground">Loading points…</p>
          )}
        </div>
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

      <div
        role="region"
        aria-label="Capabilities"
        className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(min(17rem,100%),1fr))]"
      >
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
