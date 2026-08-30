import { useEffect, useRef, useState } from 'react'
import { CalendarDays, CalendarRange } from 'lucide-react'
import { useNavigate } from 'react-router-dom'

import BadgeDisplay, {
  type ReconciliationBadge,
} from '@/components/Badges/BadgeDisplay'
import {
  ASK_PAGE_PATH,
  buildMarginTrendFollowUpQuestion,
  type AskPageNavigationState,
  type MarginTrendDataPointClick,
} from '@/components/Charts/chartFollowUpQuestion'
import MarginTrendChart, {
  type DailyMarginDatum,
} from '@/components/Charts/MarginTrendChart'
import ProvenanceTag, {
  type SourceRowRef,
} from '@/components/Provenance/ProvenanceTag'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Chip, PageContainer, PageHeader, Panel } from '@/components/ui/page'
import { Stat, StatGroup, StatSkeleton } from '@/components/ui/stat'
import { getJson } from '@/lib/api'
import { explainRequestFailure } from '@/lib/requestFailure'
import { humanizeSource } from '@/lib/sourceDisplayName'

// ---------------------------------------------------------------------------
// Live wiring to GET /api/reconciliation (backend internal/httpapi/data.go).
//
// Everything on this page previously came from a hardcoded literal — a
// $612.40 margin for a date that isn't in the dataset, and a 14-point chart
// whose 2026-08-08 value said "no data" while Postgres actually holds a
// computed margin of $152.50 for that day. Both were plausible and both were
// wrong, which is exactly the failure mode this product exists to prevent.
//
// This page used to show ONLY the latest reconciled day, with no way to look
// at anything else. The backend's ?start=&end= query parameters
// (YYYY-MM-DD, both required together or neither — data.go's
// parseOptionalPeriod) already supported an arbitrary window; the picker
// below is the only thing that was missing.
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

/** The real ingested data's [min, max] date, learned from the unfiltered
 *  ("latest") fetch's own echoed start/end — the same bound the backend
 *  grounds its own defaults against, never a client-side guess. */
interface DataBounds {
  start: string
  end: string
}

type ViewMode = 'latest' | 'day' | 'period'

// Reported live: "Latest" fetches the full, unfiltered history (so the
// single most-recent day can be read off its own echoed start/end — see the
// effect below), but the chart was then handed that SAME full multi-year
// `days` array unconditionally, rendering a "744-Day Margin Trend" bucketed
// into weekly totals by default. Two real problems followed from that: the
// chart never actually showed "the latest" at readable, day-level
// granularity, and weekly bucketing silently netted individual loss days
// against a mostly-profitable week, making every bar render green even
// though a genuine fraction of real days were in the red. Scoping "Latest"
// to a trailing window — same RECENT_WINDOW_SIZE convention HomePage.tsx
// already established for its own "last 90 days" stats — fixes both at
// once: at <=120 days MarginTrendChart's own MAX_DISPLAY_BARS threshold
// never bucket-aggregates, so every real day, including a loss day, renders
// as its own bar again.
const RECENT_WINDOW_SIZE = 90

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

/** Same citation shape the trend chart already uses for a whole period. */
function toPeriodProvenanceRefs(
  days: DailySummaryApi[],
  start: string,
  end: string,
): SourceRowRef[] {
  return [
    {
      source_file: 'daily_reconciliation (Postgres)',
      row_start: 1,
      row_end: days.length,
      period_start: start,
      period_end: end,
    },
  ]
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
 * The period view's badge list — NOT a plain days.flatMap(toBadges). A
 * 14-day clean period would otherwise stack 12 identical, unlabeled
 * "Clean Close" pills (each one only distinguishable by an aria-label a
 * sighted user never sees), which reads as broken rather than as 12 good
 * days — so every Clean Close day collapses into one quiet count pill.
 *
 * A real 2-year period carries the same problem for Discrepancy Catcher:
 * the 730-day synthetic dataset genuinely earns 30+ of them (one per
 * flagged day, never a duplicate), and stacking 30+ visually identical rows
 * reads as "duplicated" to an owner scanning the page even though every row
 * is a distinct, real catch. Once there is more than one in the period they
 * collapse into a single "Discrepancy Catcher ×N" pill too — but, unlike
 * Clean Close, each one carries a distinct, actionable detail worth an
 * owner's attention, so nothing is discarded: `children` carries every
 * individual day's badge, and BadgeDisplay's summary row expands to reveal
 * them on demand instead of burying them. A period with exactly one flagged
 * day still renders it directly — collapsing a single row into a "×1"
 * summary would only add a click for no benefit.
 */
function toPeriodBadges(days: DailySummaryApi[]): ReconciliationBadge[] {
  const cleanDays = days.filter((day) => day.discrepancy_flags.length === 0)
  const discrepancyDays = days.filter((day) => day.discrepancy_flags.length > 0)
  const discrepancyBadges = discrepancyDays.flatMap(toBadges)

  const collapsedDiscrepancy: ReconciliationBadge[] =
    discrepancyBadges.length <= 1
      ? discrepancyBadges
      : [
          {
            id: `discrepancy_catcher-summary-${discrepancyDays[0].date}-${discrepancyDays[discrepancyDays.length - 1].date}`,
            type: 'discrepancy_catcher',
            date: discrepancyDays[discrepancyDays.length - 1].date,
            count: discrepancyBadges.length,
            children: discrepancyBadges,
          },
        ]

  if (cleanDays.length === 0) return collapsedDiscrepancy

  const lastCleanDay = cleanDays[cleanDays.length - 1]
  return [
    ...collapsedDiscrepancy,
    {
      id: `clean_close-summary-${cleanDays[0].date}-${lastCleanDay.date}`,
      type: 'clean_close',
      date: lastCleanDay.date,
      count: cleanDays.length,
    },
  ]
}

/**
 * Sums an already-computed decimal field across days. This is the same class
 * of arithmetic `grossSalesTotal` above already does (adding figures the Go
 * engine produced) — not a second implementation of reconciliation math, just
 * a client-side total of numbers the API already computed per day.
 */
function sumField(
  days: DailySummaryApi[],
  field: 'commissions' | 'refunds' | 'input_costs' | 'margin',
): number {
  return days.reduce((sum, day) => sum + parseMoney(day[field]), 0)
}

function sumGrossSales(days: DailySummaryApi[]): number {
  return days.reduce((sum, day) => sum + grossSalesTotal(day), 0)
}

/** Per-source totals across every day in the period, same rule as above. */
function aggregateGrossSalesBySource(days: DailySummaryApi[]): [string, number][] {
  const totals = new Map<string, number>()
  for (const day of days) {
    for (const [source, amount] of Object.entries(day.gross_sales_by_source)) {
      totals.set(source, (totals.get(source) ?? 0) + parseMoney(amount))
    }
  }
  return [...totals.entries()]
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

/** `days` back from `iso`, never going earlier than `minIso`. Used only to
 *  seed a sensible default period window ("last week of real data") — the
 *  user can always widen or narrow it with the date inputs afterward. */
function shiftDateClamped(iso: string, days: number, minIso: string): string {
  const date = new Date(`${iso}T00:00:00Z`)
  date.setUTCDate(date.getUTCDate() + days)
  const min = new Date(`${minIso}T00:00:00Z`)
  return (date < min ? min : date).toISOString().slice(0, 10)
}

/** Builds the query suffix for the current mode, or `null` when the mode's
 *  inputs aren't complete enough to fetch yet (e.g. a period missing one
 *  bound) — the caller skips the request rather than hitting the backend
 *  with a half-picked range. */
function buildQuery(
  mode: ViewMode,
  day: string,
  rangeStart: string,
  rangeEnd: string,
): string | null {
  if (mode === 'latest') return ''
  if (mode === 'day') return day ? `?start=${day}&end=${day}` : null
  return rangeStart && rangeEnd ? `?start=${rangeStart}&end=${rangeEnd}` : null
}

/**
 * `/close` — "Today's Close": a reconciled day's summary above the margin
 * trend, read live from Postgres via GET /api/reconciliation. Defaults to
 * "today" meaning the latest day the DATA covers, not the wall-clock date —
 * the same grounding rule the ambiguity gate and the explanation step use, so
 * the page and the chat never disagree about which day "today" is — but a
 * picker lets the owner look at any other day, or any period, on demand.
 */
export default function ClosePage() {
  const navigate = useNavigate()
  // Spec 008 FR-001: `/close` and `/ask` are separate routes with no shared
  // chat context, so a chart click navigates to `/ask` carrying the built
  // question as router state (see chartFollowUpQuestion.ts's doc comment).
  function handleChartDataPointClick(point: MarginTrendDataPointClick) {
    navigate(ASK_PAGE_PATH, {
      state: {
        autoSubmitQuestion: buildMarginTrendFollowUpQuestion(point),
      } satisfies AskPageNavigationState,
    })
  }

  const [viewMode, setViewMode] = useState<ViewMode>('latest')
  const [selectedDate, setSelectedDate] = useState('')
  const [rangeStart, setRangeStart] = useState('')
  const [rangeEnd, setRangeEnd] = useState('')

  const [bounds, setBounds] = useState<DataBounds | null>(null)
  // `data === null` IS the loading state — the same convention the original
  // single-fetch version of this page used (and PromotionsPage still uses):
  // no separate boolean to keep in sync, just "no response for the current
  // query yet". Every handler that changes what query is active below resets
  // `data` to null in the same synchronous state update, so a newly-picked
  // day/period shows the loading skeleton immediately rather than the
  // previous selection's stale figures.
  const [data, setData] = useState<ReconciliationApiResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  // Reported live: Period mode has two independent date fields (rangeStart,
  // rangeEnd), each its own piece of state with its own onChange handler —
  // auto-fetching on every change fired an immediate request for a range the
  // owner hadn't finished choosing yet, then a second request a moment later
  // once the other field changed too, even with the debounce this used to
  // have. The fix is not a longer debounce — it's not auto-fetching Period at
  // all: `periodLoading` tracks only an explicitly-triggered Period fetch
  // (see handleApplyPeriod), so a null `data` in Period mode with this false
  // reads as "waiting on the owner to pick dates," not "loading."
  const [periodLoading, setPeriodLoading] = useState(false)

  // Guards a fetch's own state updates against a newer request finishing
  // first (Period's explicit Apply button can now fire an out-of-order
  // second request if clicked again before the first resolves) and against
  // updating state after unmount — the same two failure modes the previous
  // per-effect `cancelled` flag guarded, generalized so both the automatic
  // effect below and the explicit handleApplyPeriod button can share one
  // fetch path.
  const latestRequestId = useRef(0)
  const isMountedRef = useRef(true)
  useEffect(() => {
    isMountedRef.current = true
    return () => {
      isMountedRef.current = false
    }
  }, [])

  function fetchReconciliation(query: string, mode: ViewMode) {
    const requestId = ++latestRequestId.current
    getJson<ReconciliationApiResponse>(`/api/reconciliation${query}`)
      .then((response) => {
        if (!isMountedRef.current || latestRequestId.current !== requestId) {
          return
        }
        setData(response)
        // Only the unfiltered "latest" fetch's echoed start/end reflects the
        // real ingested data's own range — a filtered fetch echoes back the
        // requested window instead (data.go's servedBound), which would be
        // the wrong thing to treat as the picker's min/max.
        if (mode === 'latest') {
          setBounds({ start: response.start, end: response.end })
        }
        setPeriodLoading(false)
      })
      .catch((caught: unknown) => {
        if (!isMountedRef.current || latestRequestId.current !== requestId) {
          return
        }
        setError(explainRequestFailure(caught))
        setPeriodLoading(false)
      })
  }

  // "Latest" and "Day" still fetch immediately on their own single-field
  // change, same as before. Period is excluded here — it fetches only via
  // the explicit "Show results" button (handleApplyPeriod) below, never as
  // a side effect of typing into either date field.
  useEffect(() => {
    if (viewMode === 'period') return
    const query = buildQuery(viewMode, selectedDate, rangeStart, rangeEnd)
    if (query === null) return
    fetchReconciliation(query, viewMode)
    // rangeStart/rangeEnd are intentionally not deps: this effect never
    // reads them for 'latest' or 'day', and Period's own fields are read
    // only by handleApplyPeriod.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [viewMode, selectedDate])

  /** Period's explicit confirm action — the only thing that fetches a
   *  period, now that editing rangeStart/rangeEnd no longer does. */
  function handleApplyPeriod() {
    const query = buildQuery('period', selectedDate, rangeStart, rangeEnd)
    if (query === null) return
    setError(null)
    setData(null)
    setPeriodLoading(true)
    fetchReconciliation(query, 'period')
  }

  const hasAnyData = bounds !== null && bounds.start !== ''

  function handleModeChange(mode: ViewMode) {
    if (mode === 'day' && !selectedDate) {
      setSelectedDate(bounds?.end ?? '')
    }
    if (mode === 'period' && (!rangeStart || !rangeEnd)) {
      const end = bounds?.end ?? ''
      setRangeStart(bounds ? shiftDateClamped(end, -6, bounds.start) : end)
      setRangeEnd(end)
    }
    setViewMode(mode)
    setData(null)
    setError(null)
    setPeriodLoading(false)
  }

  function handleSelectedDateChange(value: string) {
    setSelectedDate(value)
    setData(null)
    setError(null)
  }

  // Editing either Period field only updates the field's own state — no
  // fetch, per the button below. Also clears any stale `data`/`error` from
  // a previously APPLIED range and drops `periodLoading` back to false: an
  // in-flight fetch for the range being edited away from is left running
  // (fetchReconciliation's requestId guard drops its result when it lands),
  // but the UI should read as "waiting on you to pick dates," not "loading,"
  // the moment the owner starts changing what they asked for.
  function handleRangeStartChange(value: string) {
    setRangeStart(value)
    setData(null)
    setError(null)
    setPeriodLoading(false)
  }

  function handleRangeEndChange(value: string) {
    setRangeEnd(value)
    setData(null)
    setError(null)
    setPeriodLoading(false)
  }

  const days = data?.days ?? []
  const latest = days[days.length - 1]
  const isPeriodView = viewMode === 'period'
  const margin = latest ? parseMoney(latest.margin) : 0

  // The chart's own window, trailing RECENT_WINDOW_SIZE real days — see the
  // constant's doc comment. A no-op for 'day' mode (already a single
  // fetched day) and for 'period' mode (which builds its own chart range
  // below from the explicitly picked start/end, never this one); the fix
  // is specifically for 'latest' mode's previously-unscoped full history.
  // `.slice(-N)` on a shorter array just returns however many days really
  // exist — the same honest-degrade behavior HomePage.tsx's own 90-day
  // stats already rely on.
  const chartDays = days.slice(-RECENT_WINDOW_SIZE)
  const chartRangeStart = chartDays[0]?.date ?? data?.start ?? ''
  const chartRangeEnd = chartDays[chartDays.length - 1]?.date ?? data?.end ?? ''
  const periodMargin = sumField(days, 'margin')

  return (
    <PageContainer className="flex flex-col gap-5">
      <PageHeader
        eyebrow="Daily reconciliation"
        title="Today's Close"
        meta={
          data && latest ? (
            <>
              {isPeriodView ? (
                <Chip icon={CalendarRange}>
                  {data.start} → {data.end}
                </Chip>
              ) : (
                <Chip icon={CalendarDays}>{latest.date}</Chip>
              )}
              <Chip>
                {days.length} {days.length === 1 ? 'day' : 'days'} reconciled
              </Chip>
              {!isPeriodView ? (
                <Chip>
                  {data.start} to {data.end}
                </Chip>
              ) : null}
            </>
          ) : null
        }
        actions={
          hasAnyData ? (
            <div
              role="group"
              aria-label="View mode"
              className="inline-flex items-center gap-1 rounded-md border border-border p-0.5"
            >
              <Button
                type="button"
                size="sm"
                variant={viewMode === 'latest' ? 'default' : 'ghost'}
                aria-pressed={viewMode === 'latest'}
                onClick={() => handleModeChange('latest')}
              >
                Latest
              </Button>
              <Button
                type="button"
                size="sm"
                variant={viewMode === 'day' ? 'default' : 'ghost'}
                aria-pressed={viewMode === 'day'}
                onClick={() => handleModeChange('day')}
              >
                Day
              </Button>
              <Button
                type="button"
                size="sm"
                variant={viewMode === 'period' ? 'default' : 'ghost'}
                aria-pressed={viewMode === 'period'}
                onClick={() => handleModeChange('period')}
              >
                Period
              </Button>
            </div>
          ) : null
        }
      />

      {hasAnyData && bounds && viewMode !== 'latest' ? (
        <div className="flex flex-wrap items-center gap-3">
          {viewMode === 'day' ? (
            <div className="flex items-center gap-1.5">
              <label htmlFor="close-day-picker" className="text-xs font-medium text-muted-foreground">
                Day
              </label>
              <Input
                id="close-day-picker"
                type="date"
                className="h-8 w-auto"
                value={selectedDate}
                min={bounds.start}
                max={bounds.end}
                onChange={(event) => handleSelectedDateChange(event.target.value)}
              />
            </div>
          ) : (
            <div className="flex items-center gap-1.5">
              <label htmlFor="close-period-start" className="text-xs font-medium text-muted-foreground">
                From
              </label>
              <Input
                id="close-period-start"
                type="date"
                className="h-8 w-auto"
                value={rangeStart}
                min={bounds.start}
                max={rangeEnd || bounds.end}
                onChange={(event) => handleRangeStartChange(event.target.value)}
              />
              <label htmlFor="close-period-end" className="text-xs font-medium text-muted-foreground">
                To
              </label>
              <Input
                id="close-period-end"
                type="date"
                className="h-8 w-auto"
                value={rangeEnd}
                min={rangeStart || bounds.start}
                max={bounds.end}
                onChange={(event) => handleRangeEndChange(event.target.value)}
              />
              {/* Explicit confirm, not auto-fetch: reported live, editing
                  From/To used to each trigger their own debounced request,
                  firing once for a range the owner hadn't finished picking
                  yet and again once the second field changed. Both fields
                  now free-edit with no request until this is clicked. */}
              <Button
                type="button"
                size="sm"
                onClick={handleApplyPeriod}
                disabled={!rangeStart || !rangeEnd || periodLoading}
              >
                Show results
              </Button>
            </div>
          )}
          <span className="text-xs text-muted-foreground">
            Data on file covers {bounds.start} to {bounds.end}
          </span>
        </div>
      ) : null}

      {error ? (
        <Panel role="alert" className="p-4 text-sm text-muted-foreground">
          We couldn&apos;t load reconciled days, so there are no figures to
          show. {error}
        </Panel>
      ) : null}

      {/* Period with no APPLIED range yet reads as "waiting on you," not
          "loading" — there is no request in flight until Show results is
          clicked, so a loading skeleton here would be a lie about what the
          page is doing. */}
      {!error && !data && isPeriodView && !periodLoading ? (
        <Panel className="p-4 text-sm text-muted-foreground">
          Choose a start and end date, then Show results to load that period.
        </Panel>
      ) : null}

      {/* Loading is a real state, not a blank page. Skeletons hold the exact
          geometry the resolved stats will occupy, so nothing jumps. This
          fires on the initial load AND on every user-triggered re-fetch when
          a different day or period is picked (including Period's own
          explicit Show results click, tracked by periodLoading since Period
          no longer fetches as a side effect of the date fields changing). */}
      {!error && !data && (!isPeriodView || periodLoading) ? (
        <Panel className="p-5 sm:p-6">
          <StatGroup>
            <StatSkeleton size="lg" />
            <StatSkeleton />
            <StatSkeleton />
            <StatSkeleton />
          </StatGroup>
        </Panel>
      ) : null}

      {!error && data && !hasAnyData ? (
        <Panel className="p-4 text-sm text-muted-foreground">
          No reconciled days on file yet. Run the ingestion pipeline (
          <code className="font-mono text-xs">-ingest</code>) and this page
          fills in from the real rows.
        </Panel>
      ) : null}

      {/* Honest empty state for a date/period that IS within a system that
          has real data, but has no reconciliation of its own — never a
          zeroed-out chart that reads as "broke even that day". */}
      {!error && data && hasAnyData && days.length === 0 ? (
        <Panel className="p-4 text-sm text-muted-foreground">
          No reconciled data for{' '}
          {viewMode === 'day' ? (
            <>the selected day ({data.start})</>
          ) : (
            <>
              the selected period ({data.start} to {data.end})
            </>
          )}
          . This restaurant&apos;s data covers{' '}
          <span className="font-medium text-foreground">
            {bounds?.start} to {bounds?.end}
          </span>{' '}
          — pick a date in that range.
        </Panel>
      ) : null}

      {!error && data && latest && !isPeriodView ? (
        <>
          <Panel aria-label="Latest reconciled day" className="p-5 sm:p-6">
            <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
              <h2 className="text-sm font-semibold tracking-tight text-foreground">
                Where the day landed
              </h2>
              <BadgeDisplay badges={toBadges(latest)} />
            </div>

            {/* The four figures that used to live inside one 12px sentence.
                Every value is either a decimal string the API sent or the
                pre-existing grossSalesTotal of the per-source values it sent
                — no new arithmetic was introduced to build this row. */}
            {/* The margin captions below read "Closed at a loss"/"at a
                profit". They used to say "in the red"/"in the green" — an
                English finance idiom that means nothing translated literally,
                and one that leans on a colour name to say "loss": the two
                things the ux-writing rules ask a caption not to do. The plain
                wording still pairs with the Stat's own negative/positive
                tone, which is what carries the colour. */}
            <StatGroup>
              <Stat
                label="Margin"
                value={formatUsd(margin)}
                size="lg"
                tone={margin < 0 ? 'negative' : 'positive'}
                caption={margin < 0 ? 'Closed at a loss' : 'Closed at a profit'}
                footer={<ProvenanceTag refs={toProvenanceRefs(latest)} />}
              />
              <Stat
                label="Gross sales"
                value={formatUsd(grossSalesTotal(latest))}
                // "channels", not "sources" — this Stat sits right beside
                // Margin's own ProvenanceTag footer, which ALSO says "N
                // sources" but counts a completely different thing (distinct
                // provenance FILES, not distinct SALES CHANNELS). Same word,
                // same row, two different denominators reads as if a source
                // were missing from one of them when neither is wrong — a
                // QA pass found exactly that confusion. Naming this one
                // "channels" (iFood, Just Eat Takeaway, POS, ...) makes the
                // two counts unambiguous next to each other.
                caption={`${Object.keys(latest.gross_sales_by_source).length} ${Object.keys(latest.gross_sales_by_source).length === 1 ? 'channel' : 'channels'}`}
              />
              <Stat
                label="Commissions"
                value={formatUsd(parseMoney(latest.commissions))}
                caption="Netted out"
              />
              <Stat
                label="Refunds"
                value={formatUsd(parseMoney(latest.refunds))}
                caption="Netted out"
              />
              <Stat
                label="Input costs"
                value={formatUsd(parseMoney(latest.input_costs))}
                caption="Netted out"
              />
            </StatGroup>
          </Panel>

          {/* Two columns rather than a stack: the chart renders at its own
              700px design width, so a full-width row would leave a third of
              the line empty beside it. The by-source breakdown fills that
              column with content the chart does not carry. */}
          <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_18rem]">
            <MarginTrendChart
              data={toChartData(chartDays, chartRangeStart, chartRangeEnd)}
              sourceRefs={toPeriodProvenanceRefs(
                chartDays,
                chartRangeStart,
                chartRangeEnd,
              )}
              onDataPointClick={handleChartDataPointClick}
            />

            {/* Gross sales by source: one stat per channel, so "3 channels"
                above is checkable rather than an assertion. Amounts are
                printed exactly as the API sent them; the source label is
                humanized via the same ifood/just_eat_takeaway/pos ->
                display-name mapping the chat's own pie-chart legend uses
                (backend's humanizeSource), so "pos" never leaks to the UI
                as a raw key. */}
            <Panel className="p-5">
              <h2 className="text-sm font-semibold tracking-tight text-foreground">
                Gross sales by source
              </h2>
              <p className="mt-1 text-xs text-muted-foreground">
                {latest.date}
              </p>
              <dl className="mt-4 flex flex-col">
                {Object.entries(latest.gross_sales_by_source).map(
                  ([source, amount]) => (
                    <div
                      key={source}
                      className="flex items-baseline justify-between gap-3 border-b border-border py-2.5 last:border-b-0"
                    >
                      <dt className="min-w-0 truncate text-xs text-muted-foreground">
                        {humanizeSource(source)}
                      </dt>
                      <dd className="shrink-0 text-sm font-semibold tabular-nums text-foreground">
                        {formatUsd(parseMoney(amount))}
                      </dd>
                    </div>
                  ),
                )}
              </dl>
            </Panel>
          </div>
        </>
      ) : null}

      {/* Period view: the same figures, summed across every reconciled day
          in the window — an aggregate, not a second copy of the day view.
          Every number here is a client-side sum of decimal strings the Go
          engine already computed per day (sumField / aggregateGrossSalesBySource
          above), the same class of arithmetic `grossSalesTotal` already does;
          nothing here re-derives reconciliation math. */}
      {!error && data && isPeriodView && days.length > 0 ? (
        <>
          <Panel aria-label="Period reconciliation" className="p-5 sm:p-6">
            <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
              <h2 className="text-sm font-semibold tracking-tight text-foreground">
                Where the period landed
              </h2>
              <BadgeDisplay badges={toPeriodBadges(days)} />
            </div>

            <StatGroup>
              <Stat
                label="Total margin"
                value={formatUsd(periodMargin)}
                size="lg"
                tone={periodMargin < 0 ? 'negative' : 'positive'}
                caption={
                  periodMargin < 0
                    ? `Closed at a loss over ${days.length} ${days.length === 1 ? 'day' : 'days'}`
                    : `Closed at a profit over ${days.length} ${days.length === 1 ? 'day' : 'days'}`
                }
                footer={
                  <ProvenanceTag
                    refs={toPeriodProvenanceRefs(days, data.start, data.end)}
                  />
                }
              />
              <Stat
                label="Gross sales"
                value={formatUsd(sumGrossSales(days))}
                caption="Summed across the period"
              />
              <Stat
                label="Commissions"
                value={formatUsd(sumField(days, 'commissions'))}
                caption="Netted out"
              />
              <Stat
                label="Refunds"
                value={formatUsd(sumField(days, 'refunds'))}
                caption="Netted out"
              />
              <Stat
                label="Input costs"
                value={formatUsd(sumField(days, 'input_costs'))}
                caption="Netted out"
              />
            </StatGroup>
          </Panel>

          <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_18rem]">
            <MarginTrendChart
              data={toChartData(days, data.start, data.end)}
              sourceRefs={toPeriodProvenanceRefs(days, data.start, data.end)}
              onDataPointClick={handleChartDataPointClick}
            />

            <Panel className="p-5">
              <h2 className="text-sm font-semibold tracking-tight text-foreground">
                Gross sales by source
              </h2>
              <p className="mt-1 text-xs text-muted-foreground">
                {data.start} to {data.end}
              </p>
              <dl className="mt-4 flex flex-col">
                {aggregateGrossSalesBySource(days).map(([source, amount]) => (
                  <div
                    key={source}
                    className="flex items-baseline justify-between gap-3 border-b border-border py-2.5 last:border-b-0"
                  >
                    <dt className="min-w-0 truncate text-xs text-muted-foreground">
                      {source}
                    </dt>
                    <dd className="shrink-0 text-sm font-semibold tabular-nums text-foreground">
                      {formatUsd(amount)}
                    </dd>
                  </div>
                ))}
              </dl>
            </Panel>
          </div>
        </>
      ) : null}
    </PageContainer>
  )
}
