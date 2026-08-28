import { useId, useState } from 'react'
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
}

// ---------------------------------------------------------------------------
// Geometry — fixed viewBox units; the <svg> scales to its container width via
// CSS while keeping this aspect ratio, so percentage-based overlay
// positioning (tooltip, table toggle) stays aligned without measuring the DOM.
// ---------------------------------------------------------------------------

const CHART_WIDTH = 700
const CHART_HEIGHT = 300
const MARGIN = { top: 40, right: 12, bottom: 40, left: 44 }
const PLOT_WIDTH = CHART_WIDTH - MARGIN.left - MARGIN.right
const PLOT_HEIGHT = CHART_HEIGHT - MARGIN.top - MARGIN.bottom

const BAR_WIDTH = 24 // mark spec: bars <= 24px thick
const BAR_RADIUS = 4
const MISSING_CAPSULE_HEIGHT = 28

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
function buildScale(data: DailyMarginDatum[]) {
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
  datum: DailyMarginDatum
  index: number
  slotCenterX: number
  barX: number
  isMissing: boolean
  isPositive: boolean
  barY: number
  barHeight: number
}

function buildBars(
  data: DailyMarginDatum[],
  yToPixel: (value: number) => number,
): BarGeometry[] {
  const slotWidth = PLOT_WIDTH / data.length
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
}: MarginTrendChartProps) {
  const hatchPatternId = useId()
  const [hoveredIndex, setHoveredIndex] = useState<number | null>(null)
  const [tableOpen, setTableOpen] = useState(false)

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

  const { ticks, yToPixel, baselineY } = buildScale(data)
  const bars = buildBars(data, yToPixel)
  const values = data
    .map((d) => d.margin)
    .filter((v): v is number => v !== null)
  const maxValue = Math.max(...values)
  const minValue = Math.min(...values)
  const maxIndex = data.findIndex((d) => d.margin === maxValue)
  const minIndex = data.findIndex((d) => d.margin === minValue)

  const hovered = hoveredIndex === null ? null : bars[hoveredIndex]

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
      </figcaption>

      <div className="relative w-full overflow-x-auto">
        <svg
          viewBox={`0 0 ${CHART_WIDTH} ${CHART_HEIGHT}`}
          role="img"
          aria-label={`Bar chart of daily reconciled margin, ${rangeLabel}${
            missingDates.length > 0
              ? `, with ${missingDates.join(' and ')} flagged as missing data`
              : ''
          }`}
          className="h-auto w-full min-w-[420px]"
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
                x2={CHART_WIDTH - MARGIN.right}
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
            x2={CHART_WIDTH - MARGIN.right}
            y1={baselineY}
            y2={baselineY}
            stroke="var(--border)"
            strokeWidth={1}
          />

          {bars.map((bar) => {
            const { datum, index, slotCenterX, barX, isMissing, isPositive } =
              bar
            const isExtreme = index === maxIndex || index === minIndex
            const focusLabel = isMissing
              ? `${formatMonthDay(datum.date)}: no data, ${MISSING_MARGIN_REASON.toLowerCase()}`
              : `${formatMonthDay(datum.date)}: ${formatSignedUsd(datum.margin as number)}`

            return (
              <g
                key={datum.date}
                tabIndex={0}
                role="button"
                aria-label={focusLabel}
                onMouseEnter={() => setHoveredIndex(index)}
                onMouseLeave={() => setHoveredIndex(null)}
                onFocus={() => setHoveredIndex(index)}
                onBlur={() => setHoveredIndex(null)}
                className="cursor-pointer outline-none"
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
                      'text-[11px] font-semibold tabular-nums',
                      isPositive ? 'fill-success-text' : 'fill-destructive-text',
                    )}
                  >
                    {formatSignedUsd(datum.margin as number)}
                  </text>
                ) : null}

                {/* X-axis day-of-month tick */}
                <text
                  x={slotCenterX}
                  y={CHART_HEIGHT - MARGIN.bottom + 16}
                  textAnchor="middle"
                  className="fill-muted-foreground text-[10px] tabular-nums"
                >
                  {dayOfMonth(datum.date)}
                </text>
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

          {/* Single month/year label rather than repeating it per tick.
              Sits ABOVE the plot: on the tick row it overlapped the last
              day-of-month tick once the day count came from live data. */}
          <text
            x={CHART_WIDTH - MARGIN.right}
            y={MARGIN.top - 14}
            textAnchor="end"
            className="fill-muted-foreground text-[10px]"
          >
            {lastDate ? `${formatMonthDay(lastDate).split(' ')[0]} ${lastDate.slice(0, 4)}` : ''}
          </text>
        </svg>

        {hovered ? (
          <div
            role="status"
            className="pointer-events-none absolute z-10 -translate-x-1/2 -translate-y-full rounded-md border border-border bg-popover px-2.5 py-1.5 text-xs shadow-md"
            style={{
              left: `${(hovered.slotCenterX / CHART_WIDTH) * 100}%`,
              top: `${(Math.min(hovered.isMissing ? baselineY - MISSING_CAPSULE_HEIGHT / 2 : hovered.barY, baselineY) / CHART_HEIGHT) * 100 - 1}%`,
            }}
          >
            <p className="text-muted-foreground">
              {formatMonthDay(hovered.datum.date)}
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
          className="text-xs text-muted-foreground underline decoration-dotted underline-offset-2 hover:text-foreground focus-visible:text-foreground focus-visible:outline-none"
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
