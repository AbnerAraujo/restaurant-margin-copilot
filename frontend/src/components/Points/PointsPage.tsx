import PointsCard from './PointsCard'
import { POINTS_PER_BADGE } from './pointValues'
import { usePoints } from './usePoints'

/**
 * `/points` — the full Steward Points surface, reachable from the sidebar so
 * the balance can be checked directly rather than only in passing on Home.
 *
 * Hosts the same `PointsCard` (one implementation, one set of copy, one
 * fetch path) and adds the earning rules underneath it. The rules table is
 * the honest counterpart to the balance: it says exactly how each point is
 * earned, which is what makes a derived score trustworthy rather than a
 * number the app asserts about you.
 */
export default function PointsPage() {
  const { data } = usePoints()
  const breakdown = data?.points.breakdown ?? []

  const earnedFor = (code: 'clean_close' | 'discrepancy_catcher') =>
    breakdown.find((line) => line.code === code)

  const RULES = [
    {
      code: 'clean_close' as const,
      name: 'Clean Close',
      each: POINTS_PER_BADGE.clean_close,
      when: 'A day reconciles with zero discrepancy flags.',
      why: 'The habit of closing daily is what earns it — not the outcome.',
    },
    {
      code: 'discrepancy_catcher' as const,
      name: 'Discrepancy Catcher',
      each: POINTS_PER_BADGE.discrepancy_catcher,
      when: 'A day reconciles with at least one flag: a duplicate order, a missing source, a commission mismatch, or an anomaly.',
      why: 'Worth more than a quiet day, because the money is found on the days something was wrong.',
    },
  ]

  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-4">
      <h1 className="text-2xl font-semibold tracking-tight text-foreground">
        Steward Points
      </h1>

      <PointsCard />

      <section
        aria-label="How points are earned"
        className="rounded-lg border border-border bg-card p-4 sm:p-5"
      >
        <h2 className="text-sm font-semibold text-foreground">
          How every point is earned
        </h2>
        <p className="mt-1 text-xs text-muted-foreground">
          There are exactly two ways. Both are computed from your reconciled
          days each time this page loads — nothing accrues in the background,
          and re-running an ingestion recomputes the balance from scratch.
        </p>

        <ul className="mt-4 space-y-4">
          {RULES.map((rule) => {
            const earned = earnedFor(rule.code)
            return (
              <li
                key={rule.code}
                className="flex flex-wrap items-start justify-between gap-x-4 gap-y-1 border-t border-border/60 pt-3 first:border-t-0 first:pt-0"
              >
                <div className="min-w-[16rem] flex-1">
                  <p className="text-sm font-medium text-foreground">
                    {rule.name}{' '}
                    <span className="font-normal text-muted-foreground">
                      — +{rule.each} each
                    </span>
                  </p>
                  <p className="mt-0.5 text-xs text-muted-foreground">
                    {rule.when}
                  </p>
                  <p className="mt-0.5 text-xs italic text-muted-foreground">
                    {rule.why}
                  </p>
                </div>
                <p className="shrink-0 text-sm tabular-nums text-foreground">
                  {earned ? (
                    <>
                      <span className="font-semibold">
                        +{earned.points.toLocaleString('en-US')}
                      </span>
                      <span className="ml-1.5 text-xs text-muted-foreground">
                        from {earned.count}{' '}
                        {earned.count === 1 ? 'day' : 'days'}
                      </span>
                    </>
                  ) : (
                    <span className="text-xs text-muted-foreground">
                      none yet
                    </span>
                  )}
                </p>
              </li>
            )
          })}
        </ul>
      </section>
    </div>
  )
}
