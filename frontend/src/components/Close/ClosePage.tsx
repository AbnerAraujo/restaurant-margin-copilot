import { useEffect, useState } from 'react'

import BadgeDisplay, {
  type ReconciliationBadge,
} from '@/components/Badges/BadgeDisplay'
import MarginTrendChart, {
  type DailyMarginDatum,
} from '@/components/Charts/MarginTrendChart'
import ProvenanceTag, {
  type SourceRowRef,
} from '@/components/Provenance/ProvenanceTag'
import { getJson } from '@/lib/api'

// ---------------------------------------------------------------------------
// Live wiring to GET /api/reconciliation (backend internal/httpapi/data.go).
//
// Everything on this page previously came from a hardcoded literal — a
// $612.40 margin for a date that isn't in the fixtures, and a 14-point chart
// whose 2026-08-08 value said "no data" while Postgres actually holds a
// computed margin of $152.50 for that day. Both were plausible and both were
// wrong, which is exactly the failure mode this product exists to prevent.
// ---------------------------------------------------------------------------

interface SourceRowRefApi {
  file: string
  row: number
}

interface DiscrepancyFlagApi {
  type: string
  detail: string
}

/** Mirrors `mcptools.DailySummaryResult` — money as decimal strings. */
interface DailySummaryApi {
  date: string
  gross_sales_by_source: Record<string, string>
  total_delivery_gross_sales: string
  commissions: string
  refunds: string
  input_costs: string
  margin: string
  discrepancy_flags: DiscrepancyFlagApi[]
  source_row_refs: SourceRowRefApi[]
}

interface ReconciliationApiResponse {
  start: string
  end: string
  days: DailySummaryApi[]
}

/** Money arrives as "-227.09"-style decimal strings; parse once, here. */
function parseMoney(decimal: string): number {
  return Number(decimal)
}

function formatUsd(amount: number): string {
  return amount.toLocaleString('en-US', { style: 'currency', currency: 'USD' })
}

function grossSalesTotal(day: DailySummaryApi): number {
  return Object.values(day.gross_sales_by_source).reduce(
    (sum, amount) => sum + parseMoney(amount),
    0,
  )
}

function toProvenanceRefs(day: DailySummaryApi): SourceRowRef[] {
  // The API returns one {file,row} per source row; collapse consecutive rows
  // of the same file into a single citation range so a day backed by 40 POS
  // lines cites "pos_export.csv · rows 2–41" rather than forty separate tags.
  const byFile = new Map<string, number[]>()
  for (const ref of day.source_row_refs) {
    byFile.set(ref.file, [...(byFile.get(ref.file) ?? []), ref.row])
  }
  return [...byFile.entries()].map(([file, rows]) => ({
    source_file: file,
    row_start: Math.min(...rows),
    row_end: Math.max(...rows),
    period_start: day.date,
    period_end: day.date,
  }))
}

function toBadges(day: DailySummaryApi): ReconciliationBadge[] {
  // Same rule the backend's internal/badges applies, and the only rule:
  // empty discrepancy_flags is a Clean Close, anything else is a catch.
  const isClean = day.discrepancy_flags.length === 0
  return [
    {
      id: `${day.date}-${isClean ? 'clean_close' : 'discrepancy_catcher'}`,
      type: isClean ? 'clean_close' : 'discrepancy_catcher',
      date: day.date,
      detail: isClean
        ? undefined
        : day.discrepancy_flags.map((flag) => flag.detail).join(' · '),
    },
  ]
}

/**
 * Expands the served days into one entry per CALENDAR day across the period.
 * A date with no persisted reconciliation becomes `margin: null`, which the
 * chart draws as an explicit "No data" placeholder rather than a $0 bar. The
 * API omits such days entirely (a missing day and a zero day are different
 * facts), so reconstructing the calendar here is what keeps the gap visible
 * instead of silently closing it.
 */
function toChartData(
  days: DailySummaryApi[],
  start: string,
  end: string,
): DailyMarginDatum[] {
  if (days.length === 0) return []
  const byDate = new Map(days.map((day) => [day.date, day]))
  const out: DailyMarginDatum[] = []
  const cursor = new Date(`${start}T00:00:00Z`)
  const last = new Date(`${end}T00:00:00Z`)

  while (cursor <= last) {
    const iso = cursor.toISOString().slice(0, 10)
    const day = byDate.get(iso)
    out.push({ date: iso, margin: day ? parseMoney(day.margin) : null })
    cursor.setUTCDate(cursor.getUTCDate() + 1)
  }
  return out
}

/**
 * `/close` — "Today's Close": the most recent reconciled day's summary above
 * the margin trend, both read live from Postgres via GET /api/reconciliation.
 * "Today" here means the latest day the DATA covers, not the wall-clock date
 * — the same grounding rule the ambiguity gate and the explanation step use,
 * so the page and the chat never disagree about which day "today" is.
 */
export default function ClosePage() {
  const [data, setData] = useState<ReconciliationApiResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    getJson<ReconciliationApiResponse>('/api/reconciliation')
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

  const latest = data?.days[data.days.length - 1]

  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-4">
      <h1 className="text-2xl font-semibold tracking-tight text-foreground">
        Today&apos;s Close
      </h1>

      {error ? (
        <p
          role="alert"
          className="rounded-lg border border-border bg-card p-4 text-sm text-muted-foreground"
        >
          Couldn&apos;t load reconciled days from the backend, so there are no
          figures to show:{' '}
          <span className="font-mono text-xs">{error}</span>
        </p>
      ) : null}

      {!error && data && !latest ? (
        <p className="rounded-lg border border-border bg-card p-4 text-sm text-muted-foreground">
          No reconciled days on file yet. Run the ingestion pipeline
          (<code className="font-mono text-xs">-ingest</code>) and this page
          fills in from the real rows.
        </p>
      ) : null}

      {latest ? (
        <>
          <section
            aria-label="Latest reconciled day"
            className="rounded-lg border border-border bg-card p-4 sm:p-5"
          >
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div>
                <p className="text-xs text-muted-foreground">
                  {latest.date} · margin on{' '}
                  {formatUsd(grossSalesTotal(latest))} in gross sales,{' '}
                  {formatUsd(parseMoney(latest.commissions))} commissions and{' '}
                  {formatUsd(parseMoney(latest.refunds))} refunds already netted
                  out
                </p>
                <p className="text-3xl font-semibold tabular-nums tracking-tight text-foreground">
                  {formatUsd(parseMoney(latest.margin))}
                </p>
              </div>
              <BadgeDisplay badges={toBadges(latest)} className="pt-0.5" />
            </div>
            <div className="mt-2 border-t border-border/60 pt-2">
              <ProvenanceTag refs={toProvenanceRefs(latest)} />
            </div>
          </section>

          <MarginTrendChart
            data={toChartData(data.days, data.start, data.end)}
            sourceRefs={[
              {
                source_file: 'daily_reconciliation (Postgres)',
                row_start: 1,
                row_end: data.days.length,
                period_start: data.start,
                period_end: data.end,
              },
            ]}
          />
        </>
      ) : null}
    </div>
  )
}
