import MarginTrendChart from '@/components/Charts/MarginTrendChart'
import PromoRoiChart from '@/components/Charts/PromoRoiChart'

/**
 * The chart-first replacement for what would otherwise be raw
 * `daily_reconciliation.csv` / `promotion_ad_spend_export.csv` tables: both
 * reconciliation reports as diverging bar charts, one per section, each
 * carrying its own legend, hover tooltip, `ProvenanceTag` citation, and
 * table-view toggle. Composes `MarginTrendChart` and `PromoRoiChart`
 * unchanged — this page owns layout and section framing only, per the
 * `dataviz` skill's chart-container contract.
 */
function ReportPage() {
  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-6">
      <header className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight text-foreground">
          Reports
        </h1>
        <p className="text-sm text-muted-foreground">
          Margin and promotion performance, reconciled from your delivery,
          POS, and ad-spend exports — every figure traces back to its source
          rows below the chart.
        </p>
      </header>

      <MarginTrendChart />
      <PromoRoiChart />
    </div>
  )
}

export default ReportPage
