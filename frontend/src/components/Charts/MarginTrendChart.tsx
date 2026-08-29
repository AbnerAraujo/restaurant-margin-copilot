import { useId, useLayoutEffect, useRef, useState } from 'react'
import { TriangleAlert } from 'lucide-react'

import { cn } from '@/lib/utils'
import ProvenanceTag, {
  type SourceRowRef,
} from '@/components/Provenance/ProvenanceTag'

// ---------------------------------------------------------------------------
// Data — the exact 2026-08-01..14 `daily_reconciliation.csv` fixture values
// (see backend/fixtures/README.md). 2026-08-08 is `margin: null`, never a
// fabricated zero: the delivery-platform source has zero rows that day
// (irregularity #3), so the reconciliation engine cannot compute a figure
// and the chart must say so rather than draw a bar at $0, which would read
// as "broke even" instead of "unknown."
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
  { date: '2026-08-01', margin: 43.26 },
  { date: '2026-08-02', margin: -227.09 },
  { date: '2026-08-03', margin: -120.26 },
  { date: '2026-08-04', margin: 34.27 },
  { date: '2026-08-05', margin: 182.91 },
  { date: '2026-08-06', margin: -183.9 },
  { date: '2026-08-07', margin: 375.82 },
  { date: '2026-08-08', margin: null },
  { date: '2026-08-09', margin: -70.58 },
  { date: '2026-08-10', margin: 328.82 },
  { date: '2026-08-11', margin: 25.77 },
  { date: '2026-08-12', margin: -214.55 },
  { date: '2026-08-13', margin: 184.94 },
  { date: '2026-08-14', margin: -29.86 },
]

/**
 * Default provenance: `DailyReconciliation` rows 1–14 of
 * `daily_reconciliation.csv` cover the full 2026-08-01..14 period this chart
 * plots (see specs/001-margin-reconciliation-qa/data-model.md).
 */
const DEFAULT_SOURCE_REFS: SourceRowRef[] = [
  {
    source_file: 'daily_reconciliation.csv',
    row_start: 1,
    row_end: 14,
    period_start: '2026-08-01',
    period_end: '2026-08-14',
  },
]

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
const MARGIN = { top: 40, right: 12, bottom: 40, left: 44 }
const PLOT_HEIGHT = CHART_HEIGHT - MARGIN.top - MARGIN.bottom

const BAR_WIDTH = 24 // mark spec: bars <= 24px thick
const BAR_RADIUS = 4
const MISSING_CAPSULE_HEIGHT = 28

// ---------------------------------------------------------------------------
// Scale — the live 2-year dataset (744 days) exposed a real bug the 14-day
// fixture never could: `PLOT_WIDTH / data.length` with a FIXED 24px
// `BAR_WIDTH` means bars start overlapping the moment a period holds more
// than ~27 days, and by 744 days every bar overlaps every neighbor into one
// solid, unreadable block — a diverging bar chart simply is not the right
// mark past a few dozen categories (dataviz skill: bars <= 24px thick and
// unclamped is a per-bar promise this chart cannot keep at unbounded N).
//
// Two changes, both scoped to fire only once a period is actually too wide
// to read, so the default 14-day fixture and any period under
// MAX_DISPLAY_BARS renders pixel-identical to before:
//
//  1. `aggregateForDisplay` buckets consecutive days once there would be more
//     than MAX_DISPLAY_BARS of them, so the chart is never asked to plot more
//     bars than it can given a real BAR_WIDTH. A 744-day period buckets into
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

// ---------------------------------------------------------------------------
// Dataset boundary — the two datasets this chart may be asked to plot span
// very different dollar magnitudes. backend/fixtures/ (see its README) is a
// hand-authored, deliberately small-dollar 14-day period, 2026-08-01..14,
// NEVER modified — and it is chronologically the MOST RECENT slice of the
// whole timeline the backend serves. Everything before it is a 730-day
// synthetic dataset (backend/cmd/gendata) generated at realistic restaurant
// scale, averaging roughly $40,000/month net margin with individual days
// often in the $1,000-4,000+ range.
//
// Reported live: any chart window wide enough to include days on both sides
// of this boundary renders a real, jarring cliff right where they meet — the
// fixture's own $10s-$100s bars flatten to a near-invisible line beside
// towering four-figure live bars, exactly at the "today" edge of the chart a
// reader looks at first. That is two honestly different datasets sharing one
// timeline, not broken data, but nothing on the chart said so. This constant
// lets the chart detect the crossing and label it (see showFixtureBoundary
// below) rather than let the reader guess "why did the bars disappear".
const FIXTURE_WINDOW_START = '2026-08-01'

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
 * [-250, 400] domain was tuned to one specific fixture; against the real
 * `/api/reconciliation` rows a day outside that window would be silently
 * CLAMPED to the axis edge — a bar drawn shorter than the loss it represents,
 * which is the one failure mode a margin chart must not have.
 *
 * Ticks are stepped on a round 100/200/500 so the labels stay readable
 * whatever the range turns out to be.
 */
function buildScale(data: DisplayDatum[]) {
  const values = data
    .map((datum) => datum.margin)
    .filter((value): value is number => value !== null)
  const rawMin = Math.min(0, ...values)
  const rawMax = Math.max(0, ...values)
  const span = rawMax - rawMin || 1
  const step = span > 2000 ? 500 : span > 800 ? 200 : 100
  const min = Math.floor(rawMin / step) * step
  const max = Math.ceil(rawMax / step) * step

  const ticks: number[] = []
  for (let tick = min; tick <= max; tick += step) ticks.push(tick)

  const yToPixel = (value: number) => {
    const clamped = Math.min(Math.max(value, min), max)
    return MARGIN.top + ((max - clamped) / (max - min || 1)) * PLOT_HEIGHT
  }
  return { ticks, yToPixel, baselineY: yToPixel(0) }
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
 * fixture but became a real, reported bug once this same label kept
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
  // Never label every bar past ~14 of them — the fixture's 14-day chart
  // keeps its exact previous look (one tick per day), while a bucketed
  // multi-year chart shows at most ~14 evenly-spaced ticks instead of an
  // illegible smear of overlapping text.
  const tickLabelStep =
    display.length <= 14 ? 1 : Math.ceil(display.length / 14)

  const { ticks, yToPixel, baselineY } = buildScale(display)
  const bars = buildBars(display, yToPixel, plotWidth)
  const values = display
    .map((d) => d.margin)
    .filter((v): v is number => v !== null)
  const maxValue = Math.max(...values)
  const minValue = Math.min(...values)
  const maxIndex = display.findIndex((d) => d.margin === maxValue)
  const minIndex = display.findIndex((d) => d.margin === minValue)

  const hovered = hoveredIndex === null ? null : bars[hoveredIndex]

  // The dataset boundary (see FIXTURE_WINDOW_START) only needs marking when
  // the plotted window actually straddles it — a window entirely inside the
  // fixture (fixtureBoundaryIndex === 0, e.g. the default 14-day fixture) or
  // entirely inside the live history (findIndex returns -1) crosses nothing,
  // so `> 0` is deliberately the whole condition rather than `!== -1`.
  const fixtureBoundaryIndex = display.findIndex(
    (datum) => datum.date >= FIXTURE_WINDOW_START,
  )
  const showFixtureBoundary = fixtureBoundaryIndex > 0
  const fixtureBoundaryX =
    MARGIN.left + (plotWidth / display.length) * fixtureBoundaryIndex
  const datasetBoundarySummary = showFixtureBoundary
    ? `, crossing from the multi-year synthetic history into the small-dollar eval fixture window at ${formatMonthDay(FIXTURE_WINDOW_START)}`
    : ''

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
      className={cn('rounded-lg border border-border bg-card p-4 sm:p-5', className)}
    >
      <figcaption className="mb-4">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Daily Close
        </p>
        <h2 className="text-lg font-semibold tracking-tight text-foreground">
          {data.length}-Day Margin Trend
        </h2>
        {bucketDays > 1 ? (
          <p className="mt-0.5 text-xs text-muted-foreground">
            Grouped into {display.length} {bucketDays}-day totals so{' '}
            {data.length} days stay readable — every day is still in the
            table below.
          </p>
        ) : null}
      </figcaption>

      <div ref={scrollContainerRef} className="relative w-full overflow-x-auto">
        <svg
          viewBox={`0 0 ${chartWidth} ${CHART_HEIGHT}`}
          // See CategoryBarChart: role="img" cannot contain the focusable
          // per-day targets below (axe nested-interactive).
          role="group"
          aria-label={`Bar chart of daily reconciled margin, ${rangeLabel}${
            bucketDays > 1
              ? `, grouped into ${display.length} ${bucketDays}-day totals`
              : ''
          }${missingDatesSummary}${datasetBoundarySummary}`}
          // Capped at its own design width (`w-full` alone let the viewBox
          // scale up inside the widened 1200px content column, which
          // enlarges the SVG's text with it — axis ticks rendered at roughly
          // 20px and the whole chart read as a blown-up thumbnail). Once
          // bucketing has widened the design width past the base 700px,
          // switching from a responsive `w-full` to a FIXED pixel width is
          // what actually makes the wrapper's `overflow-x-auto` scroll: a
          // percentage width still resolves to the (narrower) panel's own
          // width, which would silently rescale every bar back down instead
          // of giving each one its real, readable BAR_WIDTH.
          style={
            chartWidth > CHART_WIDTH ? { width: chartWidth } : { maxWidth: chartWidth }
          }
          className={cn(
            'h-auto min-w-[420px]',
            chartWidth > CHART_WIDTH ? '' : 'w-full',
          )}
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

          {/* Y gridlines + tick labels — recessive hairlines, one step off the surface */}
          {ticks.map((tick) => (
            <g key={tick}>
              <line
                x1={MARGIN.left}
                x2={chartWidth - MARGIN.right}
                y1={yToPixel(tick)}
                y2={yToPixel(tick)}
                stroke="var(--border)"
                strokeWidth={1}
                opacity={tick === 0 ? 0 : 0.6}
              />
              <text
                x={MARGIN.left - 8}
                y={yToPixel(tick)}
                textAnchor="end"
                dominantBaseline="middle"
                className="fill-muted-foreground text-[10px] tabular-nums"
              >
                {tick}
              </text>
            </g>
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
                    {bucketDays > 1 ? formatMonthDay(datum.date) : dayOfMonth(datum.date)}
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

          {/* Month/year context rather than repeating it per tick. Sits
              ABOVE the plot: on the tick row it overlapped the last
              day-of-month tick once the day count came from live data.
              Reported live as a real bug: this used to show only the LAST
              date's month ("Aug 2026") even when the chart spanned a full
              year or more, which reads as if the entire chart were that one
              month — formatChartMonthContext spans the full range honestly
              ("Aug 2024 – Aug 2026") and only collapses to one month when
              the data genuinely doesn't leave it. */}
          <text
            x={chartWidth - MARGIN.right}
            y={MARGIN.top - 14}
            textAnchor="end"
            className="fill-muted-foreground text-[10px]"
          >
            {formatChartMonthContext(firstDate, lastDate)}
          </text>

          {/* Dataset boundary — see FIXTURE_WINDOW_START's doc comment. Drawn
              LAST (on top of the bars) so the dashed rule and its labels stay
              legible even where a tall live-scale bar sits right beside the
              crossing. Labels are only rendered when there is genuinely room
              for them (a real gap on that side of the line) — this project's
              own dashed-boundary-plus-label visual language for "two honest
              things sharing one view" (see the architecture diagrams in
              docs/presentation.html), translated to a vertical rule since
              this is a timeline, not a stacked layout. */}
          {showFixtureBoundary ? (
            <g aria-hidden="true">
              <line
                x1={fixtureBoundaryX}
                x2={fixtureBoundaryX}
                y1={MARGIN.top}
                y2={CHART_HEIGHT - MARGIN.bottom}
                stroke="var(--muted-foreground)"
                strokeWidth={1}
                strokeDasharray="4 3"
                opacity={0.6}
              />
              {fixtureBoundaryX - MARGIN.left > 70 ? (
                <text
                  x={fixtureBoundaryX - 6}
                  y={MARGIN.top - 2}
                  textAnchor="end"
                  className="fill-muted-foreground text-[9px] uppercase tracking-wide"
                >
                  2-yr synthetic history
                </text>
              ) : null}
              {chartWidth - MARGIN.right - fixtureBoundaryX > 60 ? (
                <text
                  x={fixtureBoundaryX + 6}
                  y={MARGIN.top - 2}
                  textAnchor="start"
                  className="fill-muted-foreground text-[9px] uppercase tracking-wide"
                >
                  eval fixture window
                </text>
              ) : null}
            </g>
          ) : null}
        </svg>

        {hovered ? (
          <div
            role="status"
            className="pointer-events-none absolute z-10 -translate-x-1/2 -translate-y-full rounded-md border border-border bg-popover px-2.5 py-1.5 text-xs shadow-md"
            style={{
              left: `${(hovered.slotCenterX / chartWidth) * 100}%`,
              top: `${(Math.min(hovered.isMissing ? baselineY - MISSING_CAPSULE_HEIGHT / 2 : hovered.barY, baselineY) / CHART_HEIGHT) * 100 - 1}%`,
            }}
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
        <div className="mt-2 overflow-x-auto">
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
                  Margin
                </th>
              </tr>
            </thead>
            <tbody>
              {data.map((datum) => (
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
      ) : null}
    </figure>
  )
}

export default MarginTrendChart
