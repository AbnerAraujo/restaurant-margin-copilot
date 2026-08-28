import { Coins, Lock } from 'lucide-react'

import { Chip, Panel } from '@/components/ui/page'
import { Stat, StatGroup } from '@/components/ui/stat'
import { cn } from '@/lib/utils'
import CompositionBar from './CompositionBar'
import { usePoints } from './usePoints'

/**
 * Steward Points: the real, derived balance and how it was made.
 *
 * The honesty discipline this card was built around is unchanged — the
 * balance is a deterministic function of closes already run, redemption is
 * not built, and the roadmap block is a destination rather than a disabled
 * button, because a button that does nothing is a fabricated capability.
 *
 * What changed is the form, not the claims. Three paragraphs of explanation
 * (roughly 130 words) became a composition bar showing how the balance splits
 * between the two earning rules, a two-cell stat rail, and a labelled
 * "Not built" panel. The weights are still auditable on screen: they are
 * printed on each legend row rather than argued for in a sentence.
 */

export default function PointsCard({ className }: { className?: string }) {
  const { data, error } = usePoints()

  const total = data?.points.total ?? 0
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
      <div className="p-5 sm:p-6">
        {error ? (
          <p className="text-sm text-muted-foreground">
            Couldn&apos;t reach the reconciliation engine, so there is no
            balance to show. Rather than a placeholder number:{' '}
            <span className="font-mono text-xs">{error}</span>
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
              aria-label={`${total} Steward Points from ${daysClosed} ${daysClosed === 1 ? 'day' : 'days'} already reconciled`}
            >
              <StatGroup aria-hidden="true">
                <Stat
                  label="Steward points"
                  value={total.toLocaleString('en-US')}
                  size="lg"
                  icon={Coins}
                  caption="Earned, not awarded for signing up"
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

      {/* Roadmap. A recessed surface and a Lock chip carry "not live" before
          the sentence does, so nothing here can be mistaken for a feature. */}
      <div className="border-t border-border bg-muted/40 p-5 sm:p-6">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <Chip icon={Lock}>Not built yet</Chip>
          {/* h2, not h3: this card sits directly under the route's h1 and
              carries no intermediate heading, so h3 skipped a level (axe
              heading-order on /points). */}
          <h2 className="text-sm font-semibold tracking-tight text-foreground">
            Points become campaign credit
          </h2>
        </div>
        <p className="mt-2 max-w-prose-measure text-sm leading-relaxed text-muted-foreground">
          This system already tells you which promotion lost money. The next
          step funds its replacement with the closes you have already run.
          There is no redemption flow in this prototype and nothing to click
          here, because spending points needs an integration with promotional
          tooling this build has no API access to. The balance above is the
          part that is real today.
        </p>
      </div>
    </Panel>
  )
}
