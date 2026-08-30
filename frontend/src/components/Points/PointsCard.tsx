import { Coins, Rocket } from 'lucide-react'
import { Link } from 'react-router-dom'

import { Chip, Panel } from '@/components/ui/page'
import { Stat, StatGroup } from '@/components/ui/stat'
import { cn } from '@/lib/utils'
import CompositionBar from './CompositionBar'
import { usePoints } from './usePoints'

/**
 * Steward Points: the real, derived balance and how it was made.
 *
 * The balance is a deterministic function of closes already run, and — as of
 * the points-payment feature — a real, working redemption path: an owner can
 * pay for a logged promotion's spend with points instead of cash on the
 * Promotions page (POST /api/promotions, payment_method: "points"), verified
 * server-side against this exact available figure. What's shown here is
 * "Available to spend" (earned minus already-redeemed), not a bare earned
 * total, since a spendable balance is the only honest number once spending
 * is real.
 */

export default function PointsCard({ className }: { className?: string }) {
  const { data, error } = usePoints()

  const total = data?.points.total ?? 0
  const available = data?.points.available ?? total
  const spent = data?.points.spent ?? 0
  const breakdown = data?.points.breakdown ?? []
  // Reconciliation badges only: spec 002 added three more badge categories
  // to this same response, and a Growth/Engagement/Campaign-Creation badge
  // is not a reconciled day — counting all of `badges` here would overstate
  // "days reconciled" the moment any of the new categories fires.
  const daysClosed =
    data?.badges.filter((badge) => badge.category === 'reconciliation')
      .length ?? 0

  return (
    <Panel
      aria-label="Steward Points"
      className={cn('overflow-hidden', className)}
    >
      {/* The failure branch carries role="alert", matching every other
          page's load-failure panel — this was the only one whose error was
          silent to assistive tech. */}
      <div className="p-5 sm:p-6">
        {error ? (
          <p role="alert" className="text-sm text-muted-foreground">
            We couldn&apos;t load your points, so there is no balance to show
            rather than a placeholder number. {error}
          </p>
        ) : (
          <>
            {/* The balance and the day count are one fact split across two
                stats. The group keeps a single role="status" with the whole
                sentence as its name so a screen reader hears "170 Steward
                Points from 14 days already reconciled" rather than a bare
                number followed by a fragment. This affordance predates the
                redesign and is deliberately carried through it. */}
            <div
              role="status"
              aria-label={`${available} Steward Points available to spend, out of ${total} earned from ${daysClosed} ${daysClosed === 1 ? 'day' : 'days'} already reconciled`}
            >
              <StatGroup aria-hidden="true">
                <Stat
                  label="Available to spend"
                  value={available.toLocaleString('en-US')}
                  size="lg"
                  icon={Coins}
                  caption={
                    spent > 0
                      ? `${total.toLocaleString('en-US')} earned − ${spent.toLocaleString('en-US')} redeemed`
                      : 'Earned, not awarded for signing up'
                  }
                />
                <Stat
                  label="Days reconciled"
                  value={String(daysClosed)}
                  caption="Reconciliation points trace to these"
                />
              </StatGroup>
            </div>

            <div className="mt-5">
              <CompositionBar breakdown={breakdown} total={total} />
            </div>

            {breakdown.length === 0 ? (
              <p className="mt-4 text-sm text-muted-foreground">
                No closes on file yet. Run a day&apos;s reconciliation and the
                first points land immediately.
              </p>
            ) : null}
          </>
        )}
      </div>

      {/* Shipped: a real, working redemption path, not a roadmap promise. */}
      <div className="border-t border-border bg-muted/40 p-5 sm:p-6">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <Chip icon={Rocket}>Live</Chip>
          {/* h2, not h3: this card sits directly under the route's h1 and
              carries no intermediate heading, so h3 skipped a level (axe
              heading-order on /points). */}
          <h2 className="text-sm font-semibold tracking-tight text-foreground">
            Points become campaign credit
          </h2>
        </div>
        <p className="mt-2 max-w-prose-measure text-sm leading-relaxed text-muted-foreground">
          This system already tells you which promotion lost money. Now you
          can fund its replacement with the closes you have already run:
          logging a promotion on the Promotions page lets you pay its spend
          in points instead of cash, at 10&cent; per point, verified against
          the real balance above before it's ever spent.
        </p>
        <Link
          to="/promotions"
          className="mt-3 inline-flex items-center gap-1.5 text-xs font-medium text-primary underline-offset-4 hover:underline focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
        >
          Log a promotion with points &rarr;
        </Link>
      </div>
    </Panel>
  )
}
