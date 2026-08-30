import { useEffect, useState } from 'react'
import { CalendarRange, Percent } from 'lucide-react'

import CategoryBarChart from '@/components/Charts/CategoryBarChart'
import DataGrid from '@/components/Charts/DataGrid'
import EffectiveRateTrendChart, {
  type EffectiveRateTrendPeriod,
} from '@/components/Charts/EffectiveRateTrendChart'
import { FilterBar, FilterEmptyState, FilterSearchInput } from '@/components/ui/filter-bar'
import { Chip, PageContainer, PageHeader, Panel } from '@/components/ui/page'
import { getJson } from '@/lib/api'
import { explainRequestFailure } from '@/lib/requestFailure'
import { useTableFilter } from '@/lib/useTableFilter'

// ---------------------------------------------------------------------------
// Live wiring to GET /api/platforms (backend internal/httpapi/data.go),
// which serves the exact mcptools.PlatformComparisonResult shape
// compare_platform_economics returns — so this page and a chat answer about
// the same period are reading one rendering of one computation, never two
// (spec 003-platform-comparator SC-003).
// ---------------------------------------------------------------------------

interface SourceRowRefApi {
  file: string
  row: number
}

interface PlatformEconomicsApi {
  source: string
  display_name: string
  gross_sales: string
  commission_paid: string
  /** Null exactly when gross_sales is zero for the period (FR-003) — a rate
   * over zero sales is undefined, never a fabricated "0.00%". */
  effective_rate: string | null
  promo_spend: string
  combined_cost: string
  combined_effective_rate: string | null
  source_row_refs: SourceRowRefApi[]
}

interface PlatformComparisonApi {
  period: { start: string; end: string }
  days_included: number
  platforms: PlatformEconomicsApi[]
}

interface PlatformsTrendPeriodApi {
  month: string
  result: PlatformComparisonApi
}

interface PlatformsTrendApi {
  periods: PlatformsTrendPeriodApi[]
}

/**
 * Maps GET /api/platforms/trend's period-wrapped shape into
 * EffectiveRateTrendChart's flatter props shape — the chart component
 * itself has no knowledge of this endpoint's wrapping, only of "a month and
 * the platforms in it" (spec 008 FR-007).
 */
function toTrendPeriods(periods: PlatformsTrendPeriodApi[]): EffectiveRateTrendPeriod[] {
  return periods.map((p) => ({
    month: p.month,
    platforms: p.result.platforms.map((platform) => ({
      source: platform.source,
      display_name: platform.display_name,
      effective_rate: platform.effective_rate,
    })),
  }))
}

function formatUsd(decimal: string): string {
  return Number(decimal).toLocaleString('en-US', {
    style: 'currency',
    currency: 'USD',
  })
}

/**
 * Builds the same "commission-only vs. commission + promo, per platform"
 * bar shape the chat surface's chart uses (backend
 * internal/httpapi/visualization.go's platformComparisonVisualization) —
 * mirrored here in TypeScript the same way PromotionsPage's toChartDatum
 * mirrors get_promotion_roi's rendering, rather than re-deriving any figure:
 * every value plotted is already a string the Go engine computed, only
 * parsed for bar geometry (`Number`), never recomputed.
 */
function toChartPoints(platforms: PlatformEconomicsApi[]) {
  return platforms.flatMap((platform) => [
    {
      label: `${platform.display_name} — commission only`,
      value: Number(platform.commission_paid),
      display: formatUsd(platform.commission_paid),
    },
    {
      label: `${platform.display_name} — commission + promo`,
      value: Number(platform.combined_cost),
      display: formatUsd(platform.combined_cost),
    },
  ])
}

function toTableRows(platforms: PlatformEconomicsApi[]): string[][] {
  return platforms.map((platform) => [
    platform.display_name,
    formatUsd(platform.gross_sales),
    formatUsd(platform.commission_paid),
    platform.effective_rate ?? 'No sales this period',
    formatUsd(platform.promo_spend),
    formatUsd(platform.combined_cost),
    platform.combined_effective_rate ?? 'No sales this period',
  ])
}

/**
 * `/platforms` — "Platform Economics", read live from Postgres via
 * compare_platform_economics' own computation. Answers spec
 * 003-platform-comparator's whole point at a glance: which of iFood/Just Eat
 * Takeaway costs more in commission, and how promotional spend changes that
 * picture — without the owner doing any arithmetic themselves (SC-002).
 *
 * A platform with zero activity in the period still renders its own row and
 * bar pair with real zeros (FR-003) — this page never filters a platform out
 * because its figures happen to be small or absent.
 */
export default function PlatformsPage() {
  const [data, setData] = useState<PlatformComparisonApi | null>(null)
  const [error, setError] = useState<string | null>(null)
  // A SEPARATE fetch and its own null state (spec 008 FR-007) — the trend
  // endpoint can legitimately return fewer than 2 real months (a fresh
  // dataset), which must omit the chart entirely rather than block or
  // degrade the page's main comparison above, which never depends on it.
  const [trendPeriods, setTrendPeriods] = useState<EffectiveRateTrendPeriod[] | null>(null)

  useEffect(() => {
    let cancelled = false
    getJson<PlatformComparisonApi>('/api/platforms')
      .then((response) => {
        if (!cancelled) setData(response)
      })
      .catch((caught: unknown) => {
        if (!cancelled) setError(explainRequestFailure(caught))
      })
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    getJson<PlatformsTrendApi>('/api/platforms/trend')
      .then((response) => {
        if (!cancelled) setTrendPeriods(toTrendPeriods(response.periods))
      })
      // A failed trend fetch never blocks the main comparison above —
      // the chart section simply stays omitted (FR-013).
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [])

  // The comparison grid's filter: a text search over the platform name —
  // no dropdown here, since the platform IS each row's identity (a
  // dropdown of platform names would just duplicate the search box). Feeds
  // BOTH the bar chart and the table below from one filtered list, per the
  // dataviz skill's "filters scope everything below them".
  const platformFilter = useTableFilter({
    rows: data?.platforms ?? [],
    getSearchableText: (platform) => [platform.display_name, platform.source],
  })

  return (
    <PageContainer className="flex flex-col gap-5">
      <PageHeader
        eyebrow="Cross-platform economics"
        title="Platform Economics"
        meta={
          data ? (
            <>
              <Chip icon={CalendarRange}>
                {data.period.start} → {data.period.end}
              </Chip>
              {/* Always every platform on file, never scoped by
                  platformFilter below (dataviz skill: filters narrow the
                  chart/table beneath them, not the header) — same
                  "overview totals" framing PromotionsPage's header chips
                  use, made explicit the same way once a filter is active. */}
              {platformFilter.isFiltered ? (
                <span className="text-micro font-medium uppercase tracking-wider text-muted-foreground">
                  Overall
                </span>
              ) : null}
              <Chip icon={Percent}>{data.platforms.length} platforms compared</Chip>
            </>
          ) : null
        }
      />

      {error ? (
        <Panel role="alert" className="p-4 text-sm text-muted-foreground">
          We couldn&apos;t load the platform comparison, so there are no
          figures to show. {error}
        </Panel>
      ) : null}

      {!error && !data ? (
        <Panel className="p-5 text-sm text-muted-foreground sm:p-6">
          Loading platform economics…
        </Panel>
      ) : null}

      {!error && data ? (
        <>
          {data.platforms.length > 1 ? (
            <Panel className="flex flex-wrap items-center gap-3 p-4">
              <FilterBar
                isFiltered={platformFilter.isFiltered}
                onClear={platformFilter.clearFilters}
                resultSummary={
                  platformFilter.isFiltered
                    ? `${platformFilter.visibleCount} of ${platformFilter.totalCount} platforms shown`
                    : undefined
                }
              >
                <FilterSearchInput
                  id="platforms-search"
                  label="Search platforms"
                  value={platformFilter.searchQuery}
                  onChange={platformFilter.setSearchQuery}
                  placeholder="Search by platform name"
                />
              </FilterBar>
            </Panel>
          ) : null}

          {platformFilter.isFiltered && platformFilter.filteredRows.length === 0 ? (
            <Panel>
              <FilterEmptyState
                label="No platforms match this search."
                onClear={platformFilter.clearFilters}
              />
            </Panel>
          ) : (
            <>
              <CategoryBarChart
                title="Commission vs. commission + promo spend"
                subtitle="Same period, both platforms — promo spend shown as a distinct, separately-sourced cost, never merged into commission"
                valueLabel="Cost (USD)"
                points={toChartPoints(platformFilter.filteredRows)}
                sourceTool="compare_platform_economics"
              />

              <DataGrid
                title="Platform economics, side by side"
                subtitle={`${data.days_included} days included`}
                columns={[
                  'Platform',
                  'Gross sales',
                  'Commission paid',
                  'Effective rate',
                  'Promo spend',
                  'Combined cost',
                  'Combined effective rate',
                ]}
                rows={toTableRows(platformFilter.filteredRows)}
                sourceTool="compare_platform_economics"
              />
            </>
          )}

          {/* Effective-rate trend (spec 008 FR-007) — a separate panel,
              omitted entirely (never a placeholder or a single-point chart)
              when fewer than 2 real trailing months exist, per
              EffectiveRateTrendChart's own FR-013 discipline. */}
          {trendPeriods && trendPeriods.length >= 2 ? (
            <Panel aria-label="Effective rate trend" className="p-5 sm:p-6">
              <h2 className="text-sm font-semibold tracking-tight text-foreground">
                Effective rate over time
              </h2>
              <p className="mt-1 text-xs text-muted-foreground">
                Has the nominal-vs-real commission gap moved across the trailing months?
              </p>
              <EffectiveRateTrendChart periods={trendPeriods} className="mt-4" />
            </Panel>
          ) : null}
        </>
      ) : null}
    </PageContainer>
  )
}
