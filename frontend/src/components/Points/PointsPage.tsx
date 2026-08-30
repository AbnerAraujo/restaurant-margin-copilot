import { useEffect, useMemo, useState } from 'react'
import {
  BadgeCheck,
  CalendarCheck,
  Coins,
  Rocket,
  ShieldCheck,
  TrendingUp,
  type LucideIcon,
} from 'lucide-react'

import {
  FilterBar,
  FilterEmptyState,
  FilterSearchInput,
  FilterSelect,
} from '@/components/ui/filter-bar'
import { Chip, PageContainer, PageHeader, Panel } from '@/components/ui/page'
import { getJson } from '@/lib/api'
import { explainRequestFailure } from '@/lib/requestFailure'
import { useTableFilter } from '@/lib/useTableFilter'
import PointsCard from './PointsCard'
import { POINTS_PER_BADGE } from './pointValues'
import { usePoints, type BadgeCode } from './usePoints'

// ---------------------------------------------------------------------------
// Spec 008 FR-014: a real redemption history, reading GET /api/promotions —
// the same endpoint PromotionsPage already reads, filtered here to
// payment_method === 'points'. Fetched from THIS page, not PointsCard: that
// component is reused on Home, Settings, and LogReplacementForm, and none of
// those surfaces need a promotions fetch on every mount.
// ---------------------------------------------------------------------------

interface RedeemedPromotionApi {
  campaign_id: string
  platform: string
  period: { start: string }
  payment_method?: string
  points_spent?: number
}

interface PromotionsApiResponse {
  promotions: RedeemedPromotionApi[]
}

/** Newest first. `period.start` is the closest real date this API carries to
 * "when the redemption happened" — there is no separate redeemed_at
 * timestamp — so it doubles as both the display date and the sort key. */
function sortRedemptionsNewestFirst(
  promotions: RedeemedPromotionApi[],
): RedeemedPromotionApi[] {
  return [...promotions].sort((a, b) =>
    b.period.start.localeCompare(a.period.start),
  )
}

function useRedemptionHistory() {
  const [promotions, setPromotions] = useState<RedeemedPromotionApi[] | null>(
    null,
  )
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    getJson<PromotionsApiResponse>('/api/promotions')
      .then((response) => {
        if (!cancelled) setPromotions(response.promotions)
      })
      .catch((caught: unknown) => {
        if (!cancelled) {
          setError(explainRequestFailure(caught))
        }
      })
    return () => {
      cancelled = true
    }
  }, [])

  const redemptions = promotions
    ? sortRedemptionsNewestFirst(
        promotions.filter((promotion) => promotion.payment_method === 'points'),
      )
    : null

  return { redemptions, error }
}

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
  const { redemptions, error: redemptionsError } = useRedemptionHistory()
  const breakdown = data?.points.breakdown ?? []

  // The redemption-history grid filter (ux-writing + dataviz skills): a
  // text search over the same fields the list already shows, plus a
  // platform dropdown — the obvious categorical dimension here, since
  // campaign ids in this history are already unique (a "campaign" filter
  // would just duplicate the search box).
  const redemptionFilter = useTableFilter({
    rows: redemptions ?? [],
    getSearchableText: (redemption) => [
      redemption.campaign_id,
      redemption.platform,
    ],
    dimensions: useMemo(
      () => [
        {
          key: 'platform',
          getValue: (redemption: RedeemedPromotionApi) => redemption.platform,
        },
      ],
      [],
    ),
  })

  const earnedFor = (code: BadgeCode) =>
    breakdown.find((line) => line.code === code)

  const RULES: {
    code: BadgeCode
    name: string
    each: number
    icon: LucideIcon
    when: string
    // What `earned.count` actually counts for THIS rule — bug fix: the
    // "Earned" column used to hardcode "day(s)" for every rule, which is
    // only true for the two Reconciliation badges (Clean Close/Discrepancy
    // Catcher, one per calendar day). Growth and Campaign Launcher each
    // count distinct CAMPAIGNS, and Week One counts distinct real usage
    // days but fires as a single milestone, not once per day — "2 days" for
    // two logged replacements on the same calendar day was actively wrong.
    unit: string
  }[] = [
    {
      code: 'clean_close',
      name: 'Clean Close',
      each: POINTS_PER_BADGE.clean_close,
      icon: BadgeCheck,
      when: 'A day reconciles with zero discrepancy flags.',
      unit: 'day',
    },
    {
      code: 'discrepancy_catcher',
      name: 'Discrepancy Catcher',
      each: POINTS_PER_BADGE.discrepancy_catcher,
      icon: ShieldCheck,
      when: 'A day reconciles with at least one flag: a duplicate order, a missing source, a commission mismatch, or an anomaly.',
      unit: 'day',
    },
    {
      code: 'growth',
      name: 'Growth',
      each: POINTS_PER_BADGE.growth,
      icon: TrendingUp,
      when: 'A promotion closes with a positive, attributable ROI — spend paid for itself.',
      unit: 'campaign',
    },
    {
      code: 'engagement',
      name: 'Week One',
      each: POINTS_PER_BADGE.engagement,
      icon: CalendarCheck,
      when: 'You open the app on 7 distinct real calendar days — never simulated, never pre-seeded.',
      unit: 'milestone',
    },
    {
      code: 'campaign_creation',
      name: 'Campaign Launcher',
      each: POINTS_PER_BADGE.campaign_creation,
      icon: Rocket,
      when: 'You log a new promotion marked as replacing one flagged negative-ROI — acting on the insight, not just logging a campaign.',
      unit: 'campaign',
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
            Recomputed from your real activity — reconciled days, promotion
            ROI, app usage, and logged campaigns — on every page load.
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
                            {earned.count} {rule.unit}
                            {earned.count === 1 ? '' : 's'}
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

      {/* Spec 008 FR-014: every points-paid promotion, traceable from this
          page alone — no need to cross-reference Promotions (SC-005). */}
      <Panel aria-label="Points redemption history" className="overflow-hidden">
        <div className="border-b border-border p-5 sm:px-6">
          <h2 className="text-sm font-semibold tracking-tight text-foreground">
            Redemption history
          </h2>
          <p className="mt-1 text-xs text-muted-foreground">
            Every promotion paid for with points instead of cash.
          </p>
        </div>

        {redemptions && redemptions.length > 0 ? (
          <div className="border-b border-border p-5 sm:px-6">
            <FilterBar
              isFiltered={redemptionFilter.isFiltered}
              onClear={redemptionFilter.clearFilters}
              resultSummary={
                redemptionFilter.isFiltered
                  ? `${redemptionFilter.visibleCount} of ${redemptionFilter.totalCount} shown`
                  : undefined
              }
            >
              <FilterSearchInput
                id="points-redemption-search"
                label="Search redemption history"
                value={redemptionFilter.searchQuery}
                onChange={redemptionFilter.setSearchQuery}
                placeholder="Search by campaign ID"
              />
              <FilterSelect
                id="points-redemption-platform-filter"
                label="Filter by platform"
                value={redemptionFilter.filterValues.platform ?? null}
                onChange={(value) => redemptionFilter.setFilterValue('platform', value)}
                options={redemptionFilter.dimensionOptions.platform ?? []}
                allLabel="All platforms"
              />
            </FilterBar>
          </div>
        ) : null}

        {redemptionsError ? (
          <p role="alert" className="p-5 text-sm text-muted-foreground sm:px-6">
            We couldn&apos;t load your redemption history, so there is nothing
            to show here. {redemptionsError}
          </p>
        ) : redemptions === null ? (
          <div className="flex flex-col gap-2 p-5 sm:px-6" aria-hidden="true">
            <div className="h-4 w-3/4 rounded-sm bg-muted" />
            <div className="h-4 w-1/2 rounded-sm bg-muted" />
          </div>
        ) : redemptions.length === 0 ? (
          <p className="p-5 text-sm text-muted-foreground sm:px-6">
            No points redemptions yet. Pay for a promotion&apos;s spend with
            points on the Promotions page and it will appear here.
          </p>
        ) : redemptionFilter.filteredRows.length === 0 ? (
          <FilterEmptyState
            label="No redemptions match these filters."
            onClear={redemptionFilter.clearFilters}
          />
        ) : (
          <ul className="divide-y divide-border">
            {redemptionFilter.filteredRows.map((redemption) => (
              <li
                key={`${redemption.campaign_id}-${redemption.period.start}`}
                className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 px-5 py-3 sm:px-6"
              >
                <div className="flex items-center gap-2">
                  <Coins
                    className="size-4 shrink-0 text-muted-foreground"
                    aria-hidden="true"
                  />
                  <span className="text-sm font-medium text-foreground">
                    {redemption.campaign_id}
                  </span>
                  <span className="text-xs text-muted-foreground">
                    {redemption.platform} · {redemption.period.start}
                  </span>
                </div>
                <span className="text-sm font-semibold tabular-nums text-foreground">
                  −{(redemption.points_spent ?? 0).toLocaleString('en-US')} pts
                </span>
              </li>
            ))}
          </ul>
        )}
      </Panel>
    </PageContainer>
  )
}
