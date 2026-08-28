import BadgeDisplay, {
  type ReconciliationBadge,
} from '@/components/Badges/BadgeDisplay'
import MarginTrendChart from '@/components/Charts/MarginTrendChart'
import ProvenanceTag, {
  type SourceRowRef,
} from '@/components/Provenance/ProvenanceTag'

// ---------------------------------------------------------------------------
// Mocked "today's close" — a `DailyReconciliation` row shaped per
// data-model.md (date, margin, discrepancy_flags, source_row_refs). No live
// backend exists yet; this is the same fixture-shaped demo data the prior
// single-page layout used, now on its own route (redesign-spec.md §1/§4.1).
// ---------------------------------------------------------------------------

const TODAY_ISO = '2026-08-27'

const TODAY_SOURCE_REFS: SourceRowRef[] = [
  {
    source_file: 'daily_reconciliation.csv',
    row_start: 27,
    row_end: 27,
    period_start: TODAY_ISO,
    period_end: TODAY_ISO,
  },
  {
    source_file: 'pos_export_2026-08-27.csv',
    row_start: 1,
    row_end: 42,
    period_start: TODAY_ISO,
    period_end: TODAY_ISO,
  },
]

// discrepancy_flags = [] for today's row -> a Clean Close badge fires.
const TODAY_BADGES: ReconciliationBadge[] = [
  { id: `${TODAY_ISO}-clean_close`, type: 'clean_close', date: TODAY_ISO },
]

const TODAY_MARGIN_USD = 612.4
const TODAY_GROSS_SALES_USD = 2180.0

function formatUsd(amount: number): string {
  return amount.toLocaleString('en-US', { style: 'currency', currency: 'USD' })
}

/**
 * `/close` — "Today's Close": today's reconciliation summary card above the
 * 14-day margin trend chart, per redesign-spec.md §1/§4.1. The chart replaces
 * what would otherwise be a raw `daily_reconciliation.csv` table as the
 * default view; the underlying rows stay reachable through each
 * `ProvenanceTag` and the chart's own table-view toggle.
 */
export default function ClosePage() {
  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-4">
      <h1 className="text-2xl font-semibold tracking-tight text-foreground">
        Today&apos;s Close
      </h1>

      <section
        aria-label="Today's reconciliation summary"
        className="rounded-lg border border-border bg-card p-4 sm:p-5"
      >
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <p className="text-xs text-muted-foreground">
              Margin on {formatUsd(TODAY_GROSS_SALES_USD)} in gross sales,
              commissions and refunds already netted out
            </p>
            <p className="text-3xl font-semibold tabular-nums tracking-tight text-foreground">
              {formatUsd(TODAY_MARGIN_USD)}
            </p>
          </div>
          <BadgeDisplay badges={TODAY_BADGES} className="pt-0.5" />
        </div>
        <div className="mt-2 border-t border-border/60 pt-2">
          <ProvenanceTag refs={TODAY_SOURCE_REFS} />
        </div>
      </section>

      <MarginTrendChart />
    </div>
  )
}
