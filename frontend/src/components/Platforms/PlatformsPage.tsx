import { useEffect, useState } from 'react'
import { CalendarRange, Percent } from 'lucide-react'

import CategoryBarChart from '@/components/Charts/CategoryBarChart'
import DataGrid from '@/components/Charts/DataGrid'
import { Chip, PageContainer, PageHeader, Panel } from '@/components/ui/page'
import { getJson } from '@/lib/api'

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

  useEffect(() => {
    let cancelled = false
    getJson<PlatformComparisonApi>('/api/platforms')
      .then((response) => {
        if (!cancelled) setData(response)
      })
      .catch((caught: unknown) => {
        if (!cancelled) {
          setError(caught instanceof Error ? caught.message : String(caught))
        }
      })
    return () => {
      cancelled = true
    }
  }, [])

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
              <Chip icon={Percent}>{data.platforms.length} platforms compared</Chip>
            </>
          ) : null
        }
      />

      {error ? (
        <Panel role="alert" className="p-4 text-sm text-muted-foreground">
          Couldn&apos;t load the platform comparison from the backend:{' '}
          <span className="font-mono text-xs">{error}</span>
        </Panel>
      ) : null}

      {!error && !data ? (
        <Panel className="p-5 text-sm text-muted-foreground sm:p-6">
          Loading platform economics…
        </Panel>
      ) : null}

      {!error && data ? (
        <>
          <CategoryBarChart
            title="Commission vs. commission + promo spend"
            subtitle="Same period, both platforms — promo spend shown as a distinct, separately-sourced cost, never merged into commission"
            valueLabel="Cost (USD)"
            points={toChartPoints(data.platforms)}
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
            rows={toTableRows(data.platforms)}
            sourceTool="compare_platform_economics"
          />
        </>
      ) : null}
    </PageContainer>
  )
}
