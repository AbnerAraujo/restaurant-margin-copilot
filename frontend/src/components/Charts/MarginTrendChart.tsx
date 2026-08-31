import { useId, useLayoutEffect, useRef, useState } from 'react'
import { TriangleAlert, X } from 'lucide-react'

import { buildLinearTickScale, formatAxisCurrency } from '@/lib/chartScale'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { ColumnFilterButton } from '@/components/ui/column-filter'
import { FilterEmptyState } from '@/components/ui/filter-bar'
import { useColumnFilters, type ColumnFilterSpecs } from '@/lib/useColumnFilters'
import ProvenanceTag, {
  type SourceRowRef,
} from '@/components/Provenance/ProvenanceTag'
import PinnedValueAxis from './PinnedValueAxis'

// ---------------------------------------------------------------------------
// Data — the exact reconciled margins of the dataset's hand-authored opening
// window, 2024-08-01..14 (see backend/cmd/gendata/opening/README.md), used
// only when no `data` prop is passed; the live app always feeds this chart
// real `/api/reconciliation` rows. A calendar day with no reconciliation row
// at all renders as `margin: null` — an explicit "No data" placeholder,
// never a fabricated zero, which would read as "broke even" instead of
// "unknown" (see the missing-day rendering below).
// ---------------------------------------------------------------------------

export interface DailyMarginDatum {
  /** ISO date (yyyy-mm-dd). */
  date: string
  /** Reconciled margin in USD, or `null` when the day cannot be computed. */
  margin: number | null
}

export const MISSING_MARGIN_REASON =
  'No delivery-platform export on file for this date'

export const DEFAULT_DAILY_MARGIN: DailyMarginDatum[] = [
  { date: '2024-08-01', margin: 701.9 },
  { date: '2024-08-02', margin: 491.6 },
  { date: '2024-08-03', margin: 936.33 },
  { date: '2024-08-04', margin: 747.09 },
  { date: '2024-08-05', margin: 680.89 },
  { date: '2024-08-06', margin: 331.52 },
  { date: '2024-08-07', margin: 659.0 },
  { date: '2024-08-08', margin: 1019.45 },
  { date: '2024-08-09', margin: 831.65 },
  { date: '2024-08-10', margin: 868.5 },
  { date: '2024-08-11', margin: 466.86 },
  { date: '2024-08-12', margin: 683.06 },
  { date: '2024-08-13', margin: 596.16 },
  { date: '2024-08-14', margin: 493.58 },
]

/**
 * Default provenance: `DailyReconciliation` rows 1–14 of
 * `daily_reconciliation.csv` cover the full 2024-08-01..14 period this chart
 * plots (see specs/001-margin-reconciliation-qa/data-model.md).
 */
const DEFAULT_SOURCE_REFS: SourceRowRef[] = [
  {
    source_file: 'daily_reconciliation.csv',
    row_start: 1,
    row_end: 14,
    period_start: '2024-08-01',
    period_end: '2024-08-14',
  },
]

// ---------------------------------------------------------------------------
// "View as table" column filter — the hand-rolled 2-column fallback table
// below (Date, Margin) is the one table in this app spec 015's column-header
// filter never reached, because it isn't `DataGrid`: it keeps its own row
// type (`DailyMarginDatum`) so the Margin cell can carry conditional
// red/green/muted text and the "No data — {reason}" string rather than being
// flattened to a plain string. `useColumnFilters`'s generic `getCell` exists
// for exactly this — the hook narrows `DailyMarginDatum[]` directly, and the
// rich per-row rendering below is untouched.
//
// Only the Margin column gets a filter; Date has no categorical or numeric
// dimension worth narrowing by in a 2-column table.
// ---------------------------------------------------------------------------

const MARGIN_TABLE_COLUMNS = ['Date', 'Margin']
const MARGIN_TABLE_FILTER_SPECS: ColumnFilterSpecs = { 1: 'numeric' }

/** Column 1 (Margin) must return a numeric-parseable string, or one that
 *  `parseNumericCell` (see `useColumnFilters.ts`) fails to parse for a `null`
 *  margin — an empty string does that, so a "no data" day is excluded from a
 *  numeric-filtered result rather than guessed at, matching FR-004's
 *  refuse-over-estimate discipline. Column 0 (Date) has no filter spec, so
 *  its returned string is never read by the hook, but it's supplied anyway
 *  for a total, honest `getCell`. */
function getMarginTableCell(datum: DailyMarginDatum, columnIndex: number): string {
  if (columnIndex === 1) return datum.margin === null ? '' : String(datum.margin)
  return datum.date
}

export interface MarginTrendChartProps {
  data?: DailyMarginDatum[]
  sourceRefs?: SourceRowRef[]
  className?: string
  /**
   * Spec 008 FR-001: called with the real date (or bucketed date range) of
   * whichever bar the owner clicked or activated via keyboard, so the caller
   * can turn it into a real follow-up question. Omitted entirely (no click
   * affordance beyond the existing hover/focus tooltip) when not provided —
   * this chart also renders inside a chat answer bubble, where "click a bar
   * to ask about it" would be a confusing action mid-conversation.
   */
  onDataPointClick?: (point: { date: string; rangeEndDate: string }) => void
}

// ---------------------------------------------------------------------------
// Geometry — fixed viewBox units; the <svg> scales to its container width via
// CSS while keeping this aspect ratio, so percentage-based overlay
// positioning (tooltip, table toggle) stays aligned without measuring the DOM.
// ---------------------------------------------------------------------------

const CHART_WIDTH = 700
const CHART_HEIGHT = 300
// `left` is both the plot's own left inset AND the pixel width of the pinned
// value-axis gutter beside it — the two must stay equal so the first gridline
// begins exactly where the gutter ends. Widened from 44 to 56 for the live
// dataset's compacted labels ("−$12.5K" does not fit 44px at 10px type).
const MARGIN = { top: 40, right: 12, bottom: 40, left: 56 }
const PLOT_HEIGHT = CHART_HEIGHT - MARGIN.top - MARGIN.bottom
/** Roughly the rendered height of the hover tooltip, used to decide whether
 *  it has room to sit above a bar's tip or must flip below it. */
const TOOLTIP_HEIGHT = 58

const BAR_WIDTH = 24 // mark spec: bars <= 24px thick
const BAR_RADIUS = 4
const MISSING_CAPSULE_HEIGHT = 28

// ---------------------------------------------------------------------------
// Scale — the full multi-year dataset (hundreds of days) exposed a real bug
// a 14-day window never could: `PLOT_WIDTH / data.length` with a FIXED 24px
// `BAR_WIDTH` means bars start overlapping the moment a period holds more
// than ~27 days, and at multi-year scale every bar overlaps every neighbor into one
// solid, unreadable block — a diverging bar chart simply is not the right
// mark past a few dozen categories (dataviz skill: bars <= 24px thick and
// unclamped is a per-bar promise this chart cannot keep at unbounded N).
//
// Two changes, both scoped to fire only once a period is actually too wide
// to read, so the default 14-day sample and any period under
// MAX_DISPLAY_BARS renders pixel-identical to before:
//
//  1. `aggregateForDisplay` buckets consecutive days once there would be more
//     than MAX_DISPLAY_BARS of them, so the chart is never asked to plot more
//     bars than it can given a real BAR_WIDTH. A multi-year period buckets into
//     7-day (weekly) totals; a 90-day period stays daily. Buckets are dated
//     to their FIRST day and sum the days actually present — a bucket with no
//     reconciled day at all is `margin: null` (still an explicit gap, never a
//     fabricated $0), the same honesty rule the unbucketed chart already
//     applied per day.
//  2. The SVG's own width now grows with however many bars are actually
//     rendered (MIN_SLOT_WIDTH per bar) instead of staying pinned to 700px —
//     the existing `overflow-x-auto` wrapper turns that into a horizontal
//     scroll for a wide chart rather than a squeeze, matching how this app
//     already treats overflow everywhere else (the badge/table panels).
// ---------------------------------------------------------------------------

const MAX_DISPLAY_BARS = 120
const MIN_SLOT_WIDTH = 28 // BAR_WIDTH + a visible gutter between neighbors

interface DisplayDatum {
  /** ISO date of the bucket's first day (single day when unbucketed). */
  date: string
  /** ISO date of the bucket's last day — equal to `date` when unbucketed. */
  rangeEndDate: string
  /** Sum of the days actually present in the bucket, or null if none are. */
  margin: number | null
  daysPresent: number
  daysInBucket: number
}

/**
 * Groups `data` into buckets of `bucketDays` consecutive entries so the
 * chart never plots more than MAX_DISPLAY_BARS bars. Returns `bucketDays: 1`
 * (one bucket per day, unchanged from before this fix) whenever `data`
 * already fits, which is every case this app shipped and tested against
 * until the 2-year dataset.
 */
function aggregateForDisplay(data: DailyMarginDatum[]): {
  display: DisplayDatum[]
  bucketDays: number
} {
  const bucketDays = Math.max(1, Math.ceil(data.length / MAX_DISPLAY_BARS))
  if (bucketDays === 1) {
    return {
      display: data.map((datum) => ({
        date: datum.date,
        rangeEndDate: datum.date,
        margin: datum.margin,
        daysPresent: datum.margin === null ? 0 : 1,
        daysInBucket: 1,
      })),
      bucketDays,
    }
  }

  const display: DisplayDatum[] = []
  for (let start = 0; start < data.length; start += bucketDays) {
    const bucket = data.slice(start, start + bucketDays)
    const present = bucket.filter(
      (datum): datum is DailyMarginDatum & { margin: number } =>
        datum.margin !== null,
    )
    display.push({
      date: bucket[0].date,
      rangeEndDate: bucket[bucket.length - 1].date,
      margin: present.length === 0 ? null : present.reduce((sum, d) => sum + d.margin, 0),
      daysPresent: present.length,
      daysInBucket: bucket.length,
    })
  }
  return { display, bucketDays }
}

/**
 * Y scale derived from the data rather than hard-coded. The previous fixed
 * [-250, 400] domain was tuned to one specific 14-day sample; against the real
 * `/api/reconciliation` rows a day outside that window would be silently
 * CLAMPED to the axis edge — a bar drawn shorter than the loss it represents,
 * which is the one failure mode a margin chart must not have.
 *
 * The step comes from `buildLinearTickScale`'s nice-number algorithm rather
 * than the 100/200/500 ladder this used to carry. That ladder was tuned to a
 * few-hundred-dollar span and had no tier above $500, so the live dataset's
 * $36,000 weekly-bucket span drew ~75 gridlines 3px apart — the reported
 * "precision of the lines on the Y axis is bad". See `lib/chartScale.ts`,
 * where the maths is unit-tested directly across the full range of spans.
 *
 * Exported so the geometry (not just the arithmetic beneath it) is testable
 * without rendering an SVG.
 */
export function buildScale(data: DisplayDatum[]) {
  const values = data
    .map((datum) => datum.margin)
    .filter((value): value is number => value !== null)
  // Always zero-baselined: a margin bar's length is only meaningful against a
  // true zero, and profit/loss is read as which side of zero the bar sits on.
  const { min, max, step, ticks } = buildLinearTickScale(
    Math.min(0, ...values),
    Math.max(0, ...values),
  )

  const yToPixel = (value: number) => {
    const clamped = Math.min(Math.max(value, min), max)
    return MARGIN.top + ((max - clamped) / (max - min || 1)) * PLOT_HEIGHT
  }
  return { ticks, step, yToPixel, baselineY: yToPixel(0) }
}

/** A rect rounded only on the "data end" — the tip away from the baseline. */
function roundedBarPath(
  x: number,
  y: number,
  width: number,
  height: number,
  roundedEdge: 'top' | 'bottom',
): string {
  if (height <= 0) return ''
  const r = Math.min(BAR_RADIUS, height, width / 2)
  if (roundedEdge === 'top') {
    return `M${x},${y + height} L${x},${y + r} Q${x},${y} ${x + r},${y} L${x + width - r},${y} Q${x + width},${y} ${x + width},${y + r} L${x + width},${y + height} Z`
  }
  return `M${x},${y} L${x + width},${y} L${x + width},${y + height - r} Q${x + width},${y + height} ${x + width - r},${y + height} L${x + r},${y + height} Q${x},${y + height} ${x},${y + height - r} Z`
}

function dayOfMonth(iso: string): string {
  return String(Number(iso.slice(8, 10)))
}

function formatMonthDay(iso: string): string {
  const date = new Date(`${iso}T00:00:00Z`)
  return date.toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    timeZone: 'UTC',
  })
}

function formatMonthYear(iso: string): string {
  const date = new Date(`${iso}T00:00:00Z`)
  return date.toLocaleDateString('en-US', {
    month: 'short',
    year: 'numeric',
    timeZone: 'UTC',
  })
}

/**
 * The month/year context shown above the plot, honest about however many
 * months the chart actually spans — this used to always show just the LAST
 * date's month ("Aug 2026"), which was correct for the original single-month
 * single-month window but became a real, reported bug once this same label kept
 * appearing on a chart spanning a full year or more: a lone "Aug 2026" above
 * a chart running from 2024 to 2026 reads as if the whole chart were August
 * 2026, when the per-tick day-of-month labels (bucketDays === 1) carry no
 * month of their own to disambiguate. Collapses to one month when the range
 * genuinely doesn't leave it, so the common (still single-month) case reads
 * exactly as it always did.
 */
function formatChartMonthContext(firstIso: string, lastIso: string): string {
  if (!firstIso || !lastIso) return ''
  const first = formatMonthYear(firstIso)
  const last = formatMonthYear(lastIso)
  return first === last ? last : `${first} – ${last}`
}

/** A single day reads as "Aug 7"; a bucketed range reads as "Aug 7 – 13" so a
 *  weekly total is never mistaken for one day's figure. */
function formatBarDateLabel(datum: DisplayDatum): string {
  if (datum.date === datum.rangeEndDate) return formatMonthDay(datum.date)
  return `${formatMonthDay(datum.date)} – ${formatMonthDay(datum.rangeEndDate)}`
}

function formatSignedUsd(value: number): string {
  const magnitude = Math.abs(value).toLocaleString('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
  return value < 0 ? `−${magnitude}` : `+${magnitude}`
}

interface BarGeometry {
  datum: DisplayDatum
  index: number
  slotCenterX: number
  barX: number
  isMissing: boolean
  isPositive: boolean
  barY: number
  barHeight: number
}

function buildBars(
  data: DisplayDatum[],
  yToPixel: (value: number) => number,
  plotWidth: number,
): BarGeometry[] {
  const slotWidth = plotWidth / data.length
  return data.map((datum, index) => {
    const slotCenterX = MARGIN.left + slotWidth * (index + 0.5)
    const barX = slotCenterX - BAR_WIDTH / 2
    const isMissing = datum.margin === null
    const isPositive = (datum.margin ?? 0) >= 0
    const barTopY = isMissing ? 0 : yToPixel(Math.max(datum.margin as number, 0))
    const barBottomY = isMissing
      ? 0
      : yToPixel(Math.min(datum.margin as number, 0))
    return {
      datum,
      index,
      slotCenterX,
      barX,
      isMissing,
      isPositive,
      barY: barTopY,
      barHeight: barBottomY - barTopY,
    }
  })
}

/**
 * Diverging bar chart of daily margin, zero-baselined so a loss day and a
 * profit day are distinguished by position first and color second (the
 * `dataviz` skill's mandatory mitigation for this app's success/destructive
 * pair — see the palette validator output reported alongside this component).
 * The 2026-08-08 gap renders as an explicit hatched placeholder, never a
 * fabricated $0 bar.
 */
function MarginTrendChart({
  data = DEFAULT_DAILY_MARGIN,
  sourceRefs = DEFAULT_SOURCE_REFS,
  className,
  onDataPointClick,
}: MarginTrendChartProps) {
  const hatchPatternId = useId()
  const [hoveredIndex, setHoveredIndex] = useState<number | null>(null)
  const [tableOpen, setTableOpen] = useState(false)
  const scrollContainerRef = useRef<HTMLDivElement>(null)

  // "View as table" column filter — see the module-level comment above
  // MARGIN_TABLE_COLUMNS. Kept unconditional (not gated on `tableOpen`) since
  // hooks can't be called conditionally; it's cheap when the table is hidden.
  const marginTableFilters = useColumnFilters<DailyMarginDatum>({
    columns: MARGIN_TABLE_COLUMNS,
    rows: data,
    specs: MARGIN_TABLE_FILTER_SPECS,
    getCell: getMarginTableCell,
  })
  const marginFilterActive = marginTableFilters.isColumnActive(1)
  const filteredTableRows = marginTableFilters.filteredRows

  // Every caption below is derived from the data actually plotted. Hard-coded
  // "14-Day" / "August 1–14" strings survived the switch to live
  // /api/reconciliation data as captions that no longer described the chart —
  // a mislabelled axis is a wrong number.
  const firstDate = data[0]?.date ?? ''
  const lastDate = data[data.length - 1]?.date ?? ''
  const rangeLabel =
    firstDate && lastDate
      ? `${formatMonthDay(firstDate)} – ${formatMonthDay(lastDate)}, ${lastDate.slice(0, 4)}`
      : 'no reconciled days on file'
  const missingDates = data
    .filter((datum) => datum.margin === null)
    .map((datum) => formatMonthDay(datum.date))
  // Enumerating every missing date in the aria-label reads fine at 1-2 gaps;
  // a real 2-year period can have dozens, which would turn one sentence into
  // an unreadable wall of dates — so it collapses to a count past a handful.
  const missingDatesSummary =
    missingDates.length === 0
      ? ''
      : missingDates.length <= 3
        ? `, with ${missingDates.join(' and ')} flagged as missing data`
        : `, with ${missingDates.length} days flagged as missing data`

  const { display, bucketDays } = aggregateForDisplay(data)
  const chartWidth = Math.max(
    CHART_WIDTH,
    MARGIN.left + MARGIN.right + display.length * MIN_SLOT_WIDTH,
  )
  const plotWidth = chartWidth - MARGIN.left - MARGIN.right
  // Never label every bar past ~14 of them — a 14-day chart
  // keeps its exact previous look (one tick per day), while a bucketed
  // multi-year chart shows at most ~14 evenly-spaced ticks instead of an
  // illegible smear of overlapping text.
  const tickLabelStep =
    display.length <= 14 ? 1 : Math.ceil(display.length / 14)
  // A bare day-of-month tick ("10", "17", "24") is only unambiguous while the
  // chart stays inside one month. Caught in the live rendering pass: the
  // default 90-day period spans three, so the axis read "10 17 24 1 8 15 …"
  // with nothing to say which month any of them belonged to.
  const spansMultipleMonths = firstDate.slice(0, 7) !== lastDate.slice(0, 7)

  const { ticks, step, yToPixel, baselineY } = buildScale(display)
  const bars = buildBars(display, yToPixel, plotWidth)
  const values = display
    .map((d) => d.margin)
    .filter((v): v is number => v !== null)
  const maxValue = Math.max(...values)
  const minValue = Math.min(...values)
  const maxIndex = display.findIndex((d) => d.margin === maxValue)
  const minIndex = display.findIndex((d) => d.margin === minValue)

  const hovered = hoveredIndex === null ? null : bars[hoveredIndex]
  // The tip of the hovered mark, in viewBox units: where the tooltip points.
  const tooltipAnchorX = hovered?.slotCenterX ?? 0
  const tooltipAnchorY = hovered
    ? Math.min(
        hovered.isMissing
          ? baselineY - MISSING_CAPSULE_HEIGHT / 2
          : hovered.barY,
        baselineY,
      ) - 4
    : 0
  const tooltipBelow = tooltipAnchorY < TOOLTIP_HEIGHT

  // Mount (and every genuinely new period load) scrolled to the RIGHT edge —
  // today / the most recent data — rather than the oldest history a plain
  // overflow-x-auto container defaults to. "Today's Close" is about now
  // first, with history a deliberate scroll away, not the other way round.
  // Keyed on the actual plotted range (not the `data` array's own identity,
  // which is a fresh array reference on every parent render even for the
  // SAME period) so re-scrolling only fires when the period genuinely
  // changes, never on an unrelated re-render that would otherwise yank a
  // reader back to the right after they scrolled left on purpose.
  useLayoutEffect(() => {
    const container = scrollContainerRef.current
    if (!container) return
    container.scrollLeft = container.scrollWidth
  }, [firstDate, lastDate, display.length, chartWidth])

  return (
    <figure
      aria-label="Daily margin trend"
      // min-w-0: this figure is routinely a grid/flex item, and the plot
      // beside the frozen axis now carries a definite pixel width. Without
      // it, an `auto` grid track sizes itself to that full width and the
      // whole PAGE scrolls sideways instead of the chart scrolling inside
      // its own panel — caught in the live rendering pass, not by any test.
      className={cn(
        'min-w-0 rounded-lg border border-border bg-card p-4 sm:p-5',
        className,
      )}
    >
      <figcaption className="mb-4">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Daily Close
        </p>
        <h2 className="text-lg font-semibold tracking-tight text-foreground">
          {data.length}-Day Margin Trend
        </h2>
        {/* The months the chart actually covers. This used to be drawn inside
            the SVG at its far right edge, which put it thousands of pixels
            off-screen on any chart wide enough to scroll — a caption that
            described the chart only if you happened to be scrolled to the
            end. It belongs with the title, which never scrolls.
            formatChartMonthContext spans the full range honestly ("Aug 2024 –
            Aug 2026") and collapses to one month when the data genuinely
            doesn't leave it. */}
        <p className="mt-0.5 text-xs text-muted-foreground">
          {formatChartMonthContext(firstDate, lastDate)}
        </p>
        {bucketDays > 1 ? (
          <p className="mt-0.5 text-xs text-muted-foreground">
            Grouped into {display.length} {bucketDays}-day totals so{' '}
            {data.length} days stay readable — every day is still in the
            table below.
          </p>
        ) : null}
      </figcaption>

      {/* Scroll viewport (always the panel's own width) wrapping a flex row of
          two children: the frozen value axis, then the plot. The plot renders
          at a fixed 1:1 pixel width rather than scaling to fit, which is what
          keeps the axis labels aligned with their own gridlines however narrow
          the panel gets — and keeps 10px type at 10px instead of shrinking it
          to an illegible 5px on a phone. Overflow becomes a scroll, which is
          exactly the interaction the frozen axis exists to support. */}
      <div
        ref={scrollContainerRef}
        className="flex w-full overflow-x-auto overscroll-x-contain"
      >
        <PinnedValueAxis
          ticks={ticks}
          step={step}
          yToPixel={yToPixel}
          chartHeight={CHART_HEIGHT}
          width={MARGIN.left}
          title="Margin (USD)"
          formatTick={formatAxisCurrency}
        />
        <div className="relative shrink-0" style={{ width: plotWidth + MARGIN.right }}>
        <svg
          // Cropped to start where the frozen axis gutter ends, so every
          // coordinate below stays in the original whole-chart space.
          viewBox={`${MARGIN.left} 0 ${plotWidth + MARGIN.right} ${CHART_HEIGHT}`}
          // See CategoryBarChart: role="img" cannot contain the focusable
          // per-day targets below (axe nested-interactive).
          role="group"
          aria-label={`Bar chart of daily reconciled margin, ${rangeLabel}${
            bucketDays > 1
              ? `, grouped into ${display.length} ${bucketDays}-day totals`
              : ''
          }${missingDatesSummary}`}
          width={plotWidth + MARGIN.right}
          height={CHART_HEIGHT}
          className="block"
        >
          <defs>
            <pattern
              id={hatchPatternId}
              width="6"
              height="6"
              patternUnits="userSpaceOnUse"
              patternTransform="rotate(45)"
            >
              <rect width="6" height="6" fill="var(--muted)" fillOpacity="0.5" />
              <line
                x1="0"
                y1="0"
                x2="0"
                y2="6"
                stroke="var(--muted-foreground)"
                strokeWidth="1.5"
                strokeOpacity="0.55"
              />
            </pattern>
          </defs>

          {/* Y gridlines — recessive solid hairlines, one step off the
              surface. Their labels live in the frozen axis beside this SVG. */}
          {ticks.map((tick) => (
            <line
              key={tick}
              x1={MARGIN.left}
              x2={chartWidth - MARGIN.right}
              y1={yToPixel(tick)}
              y2={yToPixel(tick)}
              stroke="var(--border)"
              strokeWidth={1}
              opacity={tick === 0 ? 0 : 0.6}
            />
          ))}

          {/* Baseline — the primary above/below cue, independent of color */}
          <line
            x1={MARGIN.left}
            x2={chartWidth - MARGIN.right}
            y1={baselineY}
            y2={baselineY}
            stroke="var(--border)"
            strokeWidth={1}
          />

          {bars.map((bar) => {
            const { datum, index, slotCenterX, barX, isMissing, isPositive } =
              bar
            const isExtreme = index === maxIndex || index === minIndex
            const barDateLabel = formatBarDateLabel(datum)
            const focusLabel = isMissing
              ? `${barDateLabel}: no data, ${MISSING_MARGIN_REASON.toLowerCase()}`
              : `${barDateLabel}: ${formatSignedUsd(datum.margin as number)}${
                  bucketDays > 1
                    ? ` (${datum.daysPresent} of ${datum.daysInBucket} days reconciled)`
                    : ''
                }`

            const handleActivate = onDataPointClick
              ? () =>
                  onDataPointClick({
                    date: datum.date,
                    rangeEndDate: datum.rangeEndDate,
                  })
              : undefined

            return (
              <g
                key={datum.date}
                tabIndex={0}
                role="button"
                aria-label={
                  handleActivate ? `${focusLabel}. Ask about this.` : focusLabel
                }
                onMouseEnter={() => setHoveredIndex(index)}
                onMouseLeave={() => setHoveredIndex(null)}
                onFocus={() => setHoveredIndex(index)}
                onBlur={() => setHoveredIndex(null)}
                onClick={handleActivate}
                onKeyDown={
                  handleActivate
                    ? (event) => {
                        if (event.key === 'Enter' || event.key === ' ') {
                          event.preventDefault()
                          handleActivate()
                        }
                      }
                    : undefined
                }
                className="cursor-pointer [outline:none] [&:focus-visible]:[outline:2px_solid_var(--ring)] [&:focus-visible]:[outline-offset:2px]"
              >
                {/* Larger, invisible hit target than the painted bar */}
                <rect
                  x={barX - 6}
                  y={MARGIN.top}
                  width={BAR_WIDTH + 12}
                  height={PLOT_HEIGHT}
                  fill="transparent"
                />

                {isMissing ? (
                  <>
                    <rect
                      x={barX}
                      y={baselineY - MISSING_CAPSULE_HEIGHT / 2}
                      width={BAR_WIDTH}
                      height={MISSING_CAPSULE_HEIGHT}
                      rx={4}
                      fill={`url(#${hatchPatternId})`}
                      stroke="var(--muted-foreground)"
                      strokeOpacity={0.35}
                      opacity={hoveredIndex === index ? 1 : 0.9}
                    />
                    <foreignObject
                      x={slotCenterX - 8}
                      y={baselineY - MISSING_CAPSULE_HEIGHT / 2 - 20}
                      width={16}
                      height={16}
                    >
                      <TriangleAlert
                        className="size-4 text-muted-foreground"
                        aria-hidden="true"
                      />
                    </foreignObject>
                  </>
                ) : (
                  <path
                    d={roundedBarPath(
                      barX,
                      bar.barY,
                      BAR_WIDTH,
                      bar.barHeight,
                      isPositive ? 'top' : 'bottom',
                    )}
                    fill={isPositive ? 'var(--success)' : 'var(--destructive)'}
                    opacity={hoveredIndex === index ? 1 : 0.92}
                  />
                )}

                {/* Direct label on the two extremes only (marks-and-anatomy: label the extreme, not every point) */}
                {isExtreme && !isMissing ? (
                  <text
                    x={slotCenterX}
                    y={
                      isPositive
                        ? bar.barY - 8
                        : bar.barY + bar.barHeight + 14
                    }
                    textAnchor="middle"
                    className={cn(
                      'text-micro font-semibold tabular-nums',
                      isPositive ? 'fill-success-text' : 'fill-destructive-text',
                    )}
                  >
                    {formatSignedUsd(datum.margin as number)}
                  </text>
                ) : null}

                {/* X-axis tick — day-of-month when unbucketed (unchanged from
                    before this fix), the bucket's short start date once
                    grouped (a bare day-of-month digit is ambiguous once a
                    bar spans several days across a month boundary). Sparse
                    past 14 bars so a wide chart shows ~14 readable ticks
                    instead of one illegible label per bar. */}
                {index % tickLabelStep === 0 ? (
                  <text
                    x={slotCenterX}
                    y={CHART_HEIGHT - MARGIN.bottom + 16}
                    textAnchor="middle"
                    className="fill-muted-foreground text-[10px] tabular-nums"
                  >
                    {bucketDays > 1 || spansMultipleMonths
                      ? formatMonthDay(datum.date)
                      : dayOfMonth(datum.date)}
                  </text>
                ) : null}
                {isMissing ? (
                  <text
                    x={slotCenterX}
                    y={CHART_HEIGHT - MARGIN.bottom + 28}
                    textAnchor="middle"
                    className="fill-muted-foreground text-[9px] font-medium"
                  >
                    No data
                  </text>
                ) : null}
              </g>
            )
          })}

        </svg>

        {hovered ? (
          <div
            role="status"
            // Positioned in real pixels against the plot layer, which renders
            // 1:1 — the previous percentages resolved against the scroll
            // VIEWPORT rather than the scrolled content, so on any chart wide
            // enough to scroll the tooltip drifted away from its own bar.
            // Flips below the tip when the bar reaches too near the top for a
            // tooltip to fit above it, rather than being clipped by the scroll
            // container's own edge.
            className={cn(
              'pointer-events-none absolute z-20 w-max -translate-x-1/2 rounded-md border border-border bg-popover px-2.5 py-1.5 text-xs shadow-md',
              tooltipBelow ? 'translate-y-2' : '-translate-y-full',
            )}
            style={{ left: tooltipAnchorX - MARGIN.left, top: tooltipAnchorY }}
          >
            <p className="text-muted-foreground">
              {formatBarDateLabel(hovered.datum)}
            </p>
            {hovered.isMissing ? (
              <p className="font-semibold text-muted-foreground">
                No data — {MISSING_MARGIN_REASON.toLowerCase()}
              </p>
            ) : (
              <p
                className={cn(
                  'text-sm font-semibold tabular-nums',
                  hovered.isPositive ? 'text-success-text' : 'text-destructive-text',
                )}
              >
                {formatSignedUsd(hovered.datum.margin as number)}
                {bucketDays > 1 ? (
                  <span className="ml-1 font-normal text-muted-foreground">
                    ({hovered.datum.daysPresent} of {hovered.datum.daysInBucket} days)
                  </span>
                ) : null}
              </p>
            )}
            <p className="text-muted-foreground">daily_reconciliation.csv</p>
          </div>
        ) : null}
        </div>
      </div>

      {/* Legend — mandatory at 2+ series; text labels, not bare swatches, since
          the success/destructive pair fails CVD separation (see the palette
          validator report) and position + text carry the meaning instead. */}
      <ul
        aria-label="Chart legend"
        className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1.5"
      >
        <li className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <span className="size-2.5 rounded-sm bg-success" aria-hidden="true" />
          Profit day
        </li>
        <li className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <span className="size-2.5 rounded-sm bg-destructive" aria-hidden="true" />
          Loss day
        </li>
        {/* Only advertised when a gap actually exists — a legend entry for a
            state not present in the chart invites the reader to hunt for it. */}
        {missingDates.length > 0 ? (
          <li className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <span
              className="size-2.5 rounded-sm border border-muted-foreground/40 bg-muted"
              aria-hidden="true"
            />
            No data
          </li>
        ) : null}
      </ul>

      <div className="mt-3 flex flex-wrap items-center justify-between gap-2 border-t border-border/60 pt-2.5">
        <ProvenanceTag refs={sourceRefs} />
        <button
          type="button"
          onClick={() => setTableOpen((wasOpen) => !wasOpen)}
          aria-expanded={tableOpen}
          className="text-xs text-muted-foreground underline decoration-dotted underline-offset-2 hover:text-foreground focus-visible:text-foreground focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:rounded-sm"
        >
          {tableOpen ? 'Hide table' : 'View as table'}
        </button>
      </div>

      {tableOpen ? (
        <div className="mt-2">
          {marginFilterActive ? (
            <div className="mb-1.5 flex items-center justify-end gap-2">
              <span className="text-xs text-muted-foreground" aria-live="polite">
                {filteredTableRows.length} of {data.length} shown
              </span>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => marginTableFilters.clearColumn(1)}
              >
                <X aria-hidden="true" />
                Clear filter
              </Button>
            </div>
          ) : null}
          {data.length > 0 && marginFilterActive && filteredTableRows.length === 0 ? (
            <FilterEmptyState
              label="No days match this margin filter."
              onClear={() => marginTableFilters.clearColumn(1)}
            />
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[320px] text-left text-xs">
                <caption className="sr-only">
                  Daily reconciled margin, {rangeLabel}
                </caption>
                <thead>
                  <tr className="border-b border-border text-muted-foreground">
                    <th scope="col" className="py-1.5 pr-3 font-medium">
                      Date
                    </th>
                    <th scope="col" className="py-1.5 pr-3 font-medium">
                      <span className="inline-flex items-center gap-1">
                        Margin
                        <ColumnFilterButton
                          type="numeric"
                          columnLabel="Margin"
                          {...marginTableFilters.getNumericRange(1)}
                          onApply={(min, max) =>
                            marginTableFilters.setNumericRange(1, min, max)
                          }
                          onClear={() => marginTableFilters.clearColumn(1)}
                        />
                      </span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {filteredTableRows.map((datum) => (
                    <tr key={datum.date} className="border-b border-border/60">
                      <td className="py-1.5 pr-3 text-foreground">
                        {formatMonthDay(datum.date)}
                      </td>
                      <td
                        className={cn(
                          'py-1.5 pr-3 font-medium tabular-nums',
                          datum.margin === null
                            ? 'text-muted-foreground'
                            : datum.margin >= 0
                              ? 'text-success-text'
                              : 'text-destructive-text',
                        )}
                      >
                        {datum.margin === null
                          ? `No data — ${MISSING_MARGIN_REASON}`
                          : formatSignedUsd(datum.margin)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      ) : null}
    </figure>
  )
}

export default MarginTrendChart
