import {
  BadgeCheck,
  CalendarCheck,
  Rocket,
  ShieldCheck,
  TrendingUp,
  type LucideIcon,
} from 'lucide-react'

import { Chip, PageContainer, PageHeader, Panel } from '@/components/ui/page'
import PointsCard from './PointsCard'
import { POINTS_PER_BADGE } from './pointValues'
import { usePoints, type BadgeCode } from './usePoints'

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

  const earnedFor = (code: BadgeCode) =>
    breakdown.find((line) => line.code === code)

  const RULES: {
    code: BadgeCode
    name: string
    each: number
    icon: LucideIcon
    when: string
  }[] = [
    {
      code: 'clean_close',
      name: 'Clean Close',
      each: POINTS_PER_BADGE.clean_close,
      icon: BadgeCheck,
      when: 'A day reconciles with zero discrepancy flags.',
    },
    {
      code: 'discrepancy_catcher',
      name: 'Discrepancy Catcher',
      each: POINTS_PER_BADGE.discrepancy_catcher,
      icon: ShieldCheck,
      when: 'A day reconciles with at least one flag: a duplicate order, a missing source, a commission mismatch, or an anomaly.',
    },
    {
      code: 'growth',
      name: 'Growth',
      each: POINTS_PER_BADGE.growth,
      icon: TrendingUp,
      when: 'A promotion closes with a positive, attributable ROI — spend paid for itself.',
    },
    {
      code: 'engagement',
      name: 'Week One',
      each: POINTS_PER_BADGE.engagement,
      icon: CalendarCheck,
      when: 'You open the app on 7 distinct real calendar days — never simulated, never pre-seeded.',
    },
    {
      code: 'campaign_creation',
      name: 'Campaign Launcher',
      each: POINTS_PER_BADGE.campaign_creation,
      icon: Rocket,
      when: 'You log a new promotion marked as replacing one flagged negative-ROI — acting on the insight, not just logging a campaign.',
    },
  ]

  return (
    <PageContainer className="flex flex-col gap-5">
      <PageHeader
        eyebrow="Reconciliation rewards"
        title="Steward Points"
        meta={
          <>
            <Chip>Derived at read time</Chip>
            <Chip>No fabricated streaks</Chip>
            <Chip>Every point traces to a real action</Chip>
          </>
        }
      />

      <PointsCard />

      {/* The rules table, restructured from stacked prose into a real table:
          rule, trigger, rate, earned. The "why" sentences that used to sit in
          italics under each rule are gone as sentences and present as the
          rate column, which is what they were arguing about. */}
      <Panel aria-label="How points are earned" className="overflow-hidden">
        <div className="border-b border-border p-5 sm:px-6">
          <h2 className="text-sm font-semibold tracking-tight text-foreground">
            How every point is earned
          </h2>
          <p className="mt-1 text-xs text-muted-foreground">
            Exactly two ways, both recomputed from your reconciled days on every
            page load.
          </p>
        </div>

        <div className="overflow-x-auto">
          <table className="w-full min-w-[34rem] border-collapse text-left">
            <thead>
              <tr className="border-b border-border">
                <th
                  scope="col"
                  className="px-5 py-2.5 text-micro font-medium uppercase tracking-wider text-muted-foreground sm:px-6"
                >
                  Rule
                </th>
                <th
                  scope="col"
                  className="px-5 py-2.5 text-micro font-medium uppercase tracking-wider text-muted-foreground"
                >
                  Fires when
                </th>
                <th
                  scope="col"
                  className="px-5 py-2.5 text-right text-micro font-medium uppercase tracking-wider text-muted-foreground"
                >
                  Rate
                </th>
                <th
                  scope="col"
                  className="px-5 py-2.5 text-right text-micro font-medium uppercase tracking-wider text-muted-foreground sm:px-6"
                >
                  Earned
                </th>
              </tr>
            </thead>
            <tbody>
              {RULES.map((rule) => {
                const earned = earnedFor(rule.code)
                const Icon = rule.icon
                return (
                  <tr
                    key={rule.code}
                    className="border-b border-border last:border-b-0"
                  >
                    <th
                      scope="row"
                      className="px-5 py-4 align-top text-sm font-medium text-foreground sm:px-6"
                    >
                      <span className="flex items-center gap-2">
                        <Icon
                          className="size-4 shrink-0 text-muted-foreground"
                          aria-hidden="true"
                        />
                        {rule.name}
                      </span>
                    </th>
                    <td className="px-5 py-4 align-top text-xs leading-relaxed text-muted-foreground">
                      {rule.when}
                    </td>
                    <td className="px-5 py-4 text-right align-top text-sm tabular-nums text-foreground">
                      +{rule.each}
                    </td>
                    <td className="px-5 py-4 text-right align-top text-sm tabular-nums sm:px-6">
                      {earned ? (
                        <>
                          <span className="font-semibold text-foreground">
                            +{earned.points.toLocaleString('en-US')}
                          </span>
                          <span className="mt-0.5 block text-micro font-normal text-muted-foreground">
                            {earned.count} {earned.count === 1 ? 'day' : 'days'}
                          </span>
                        </>
                      ) : (
                        <span className="text-xs text-muted-foreground">
                          None yet
                        </span>
                      )}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      </Panel>
    </PageContainer>
  )
}
