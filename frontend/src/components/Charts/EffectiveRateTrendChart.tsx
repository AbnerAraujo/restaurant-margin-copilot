// Spec 008 FR-007: a real line-chart trend of each platform's effective
// commission rate across the trailing calendar months GET /api/platforms/trend
// returns — net new, since no line-chart pattern existed in this codebase to
// extend (MarginTrendChart/PromoRoiChart/CategoryBarChart are all bar
// charts). Follows this project's dataviz-skill conventions: one hue per
// series in a fixed order, a thin 2px line, a legend since there are >=2
// series, direct point labels, and a real per-point hover affordance
// (native <title>, kept intentionally lightweight for this first version
// rather than a custom crosshair layer).

import { useState } from 'react'
import { X } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { ColumnFilterButton } from '@/components/ui/column-filter'
import { FilterEmptyState } from '@/components/ui/filter-bar'
import { buildLinearTickScale, formatAxisPercent } from '@/lib/chartScale'
import { useColumnFilters, type ColumnFilterSpecs } from '@/lib/useColumnFilters'
import { useHorizontalScrollFade } from '@/lib/useHorizontalScrollFade'

export interface EffectiveRateTrendPlatformPoint {
  source: string
  display_name: string
  /** Null exactly when gross sales were zero that month — the point is
   * skipped (a real gap in the line), never plotted as a fabricated 0%. */
  effective_rate: string | null
}

export interface EffectiveRateTrendPeriod {
  /** YYYY-MM */
  month: string
  platforms: EffectiveRateTrendPlatformPoint[]
}

export interface EffectiveRateTrendChartProps {
  periods: EffectiveRateTrendPeriod[]
  className?: string
}

const CHART_WIDTH = 640
const CHART_HEIGHT = 220
const MARGIN = { top: 16, right: 16, bottom: 28, left: 40 }
const PLOT_WIDTH = CHART_WIDTH - MARGIN.left - MARGIN.right
const PLOT_HEIGHT = CHART_HEIGHT - MARGIN.top - MARGIN.bottom

// Fixed categorical order, assigned by first appearance across periods —
// never cycled, never re-assigned by rank (dataviz skill: "color follows
// the entity, never its rank").
const SERIES_COLORS = ['var(--chart-1)', 'var(--chart-2)', 'var(--chart-3)', 'var(--chart-4)']

function parsePercent(rate: string | null): number | null {
  if (rate === null) return null
  const parsed = Number(rate.replace('%', ''))
  return Number.isFinite(parsed) ? parsed : null
}

function monthLabel(month: string): string {
  const [year, monthNum] = month.split('-')
  const date = new Date(Date.UTC(Number(year), Number(monthNum) - 1, 1))
  return date.toLocaleDateString('en-US', { month: 'short', year: '2-digit', timeZone: 'UTC' })
}

/**
 * Real, non-fabricated effective-rate trend. Renders nothing at all when
 * fewer than 2 periods exist — a single point has no trend to show, and
 * "trend" implies comparing across time (spec 008 FR-013: degrade to
 * omission, never a placeholder chart with one dot on it).
 */
export default function EffectiveRateTrendChart({
  periods,
  className,
}: EffectiveRateTrendChartProps) {
  const [tableOpen, setTableOpen] = useState(false)

  // Stable series order: every platform source seen across any period, in
  // first-appearance order. Computed (and the column-filter hook below
  // called) before the "fewer than 2 periods" early return so hook order
  // never varies across renders — React's Rules of Hooks — even though
  // both are only meaningful once the chart itself actually renders.
  const seriesOrder: string[] = []
  const displayNames = new Map<string, string>()
  for (const period of periods) {
    for (const platform of period.platforms) {
      if (!seriesOrder.includes(platform.source)) {
        seriesOrder.push(platform.source)
        displayNames.set(platform.source, platform.display_name)
      }
    }
  }

  // The "View as table" fallback's own column filters, always on for this
  // single-consumer chart (unlike CategoryBarChart, which is also rendered
  // in chat's answer view and so gates this behind an opt-in prop) — a
  // numeric filter per platform column, built dynamically to match however
  // many platform series this period range actually has. Month (column 0)
  // is never filterable: it's the row's own identity, same reasoning
  // Category gets no filter in CategoryBarChart's table.
  const columnFilterSpecs: ColumnFilterSpecs = Object.fromEntries(
    seriesOrder.map((_, seriesIndex) => [seriesIndex + 1, 'numeric']),
  )
  const columnFilterState = useColumnFilters<EffectiveRateTrendPeriod>({
    columns: ['Month', ...seriesOrder],
    rows: periods,
    specs: columnFilterSpecs,
    getCell: (period, columnIndex) => {
      if (columnIndex === 0) return period.month
      const source = seriesOrder[columnIndex - 1]
      const platform = period.platforms.find((p) => p.source === source)
      const rate = platform ? parsePercent(platform.effective_rate) : null
      // A "No sales" month reads as '' here, which `parseNumericCell`
      // refuses to parse — excluded from a numeric filter's results rather
      // than guessed at, never coerced to 0.
      return rate === null ? '' : String(rate)
    },
  })
  // Rules of Hooks: called unconditionally, before the early return below.
  const { ref: rateTableScrollRef, canScrollRight: rateTableCanScrollRight } =
    useHorizontalScrollFade<HTMLDivElement>()

  if (periods.length < 2) return null

  const allRates = periods
    .flatMap((p) => p.platforms.map((pl) => parsePercent(pl.effective_rate)))
    .filter((v): v is number => v !== null)
  const maxRate = allRates.length > 0 ? Math.max(...allRates) : 0
  // Domain always includes zero (dataviz skill: a true baseline) and is
  // rounded UP to a whole tick step, which both keeps the topmost point off
  // the very edge of the plot and makes every gridline a round number.
  //
  // This used to be `[0, maxRate/2, maxRate*1.15]` printed with `toFixed(0)`,
  // which meant the middle gridline of a 22% series was drawn at 12.65% and
  // labelled "13%" — an axis label that names a value the line it belongs to
  // is not at. Three ticks is also too few to read a rate off. Four is the
  // target here rather than the default five: the plot is only 176px tall.
  const { max: yMax, step: yStep, ticks: yTicks } = buildLinearTickScale(
    0,
    Math.max(maxRate, 5),
    4,
  )

  const xStep = periods.length > 1 ? PLOT_WIDTH / (periods.length - 1) : 0
  const xFor = (i: number) => MARGIN.left + i * xStep
  const yFor = (rate: number) => MARGIN.top + PLOT_HEIGHT - (rate / yMax) * PLOT_HEIGHT

  return (
    <div className={className}>
      <svg
        viewBox={`0 0 ${CHART_WIDTH} ${CHART_HEIGHT}`}
        // role="group", not role="img": each point below is a focusable
        // role="button" for keyboard/screen-reader users, and role="img"
        // forbids focusable descendants — it would have made the per-point
        // <title> values (the only place each month's rate lives) genuinely
        // unreachable to assistive tech, matching the same fix already
        // applied to CategoryBarChart/PromoRoiChart.
        role="group"
        aria-label={`Effective commission rate trend across ${periods.length} months, for ${seriesOrder
          .map((s) => displayNames.get(s))
          .join(' and ')}`}
        className="h-auto w-full"
      >
        {yTicks.map((tick) => (
          <g key={tick}>
            <line
              x1={MARGIN.left}
              x2={CHART_WIDTH - MARGIN.right}
              y1={yFor(tick)}
              y2={yFor(tick)}
              stroke="var(--border)"
              strokeWidth={1}
            />
            <text
              x={MARGIN.left - 8}
              y={yFor(tick)}
              textAnchor="end"
              dominantBaseline="middle"
              className="fill-muted-foreground text-[10px]"
            >
              {formatAxisPercent(tick, yStep)}
            </text>
          </g>
        ))}

        {periods.map((period, i) => (
          <text
            key={period.month}
            x={xFor(i)}
            y={CHART_HEIGHT - 8}
            textAnchor="middle"
            className="fill-muted-foreground text-[10px]"
          >
            {monthLabel(period.month)}
          </text>
        ))}

        {seriesOrder.map((source, seriesIndex) => {
          const color = SERIES_COLORS[seriesIndex % SERIES_COLORS.length]
          const points = periods
            .map((period, i) => {
              const platform = period.platforms.find((p) => p.source === source)
              const rate = platform ? parsePercent(platform.effective_rate) : null
              return rate === null ? null : { x: xFor(i), y: yFor(rate), rate, month: period.month }
            })
            .filter((p): p is { x: number; y: number; rate: number; month: string } => p !== null)

          // A gap (a null-rate month) breaks the line into separate
          // segments rather than connecting across it — connecting would
          // visually imply a real rate existed for a month with zero sales.
          const segments: (typeof points)[] = []
          let current: typeof points = []
          let lastIndex = -1
          for (const point of points) {
            const idx = periods.findIndex((p) => p.month === point.month)
            if (lastIndex !== -1 && idx !== lastIndex + 1) {
              segments.push(current)
              current = []
            }
            current.push(point)
            lastIndex = idx
          }
          if (current.length > 0) segments.push(current)

          return (
            <g key={source}>
              {segments.map((segment, segIndex) => (
                <polyline
                  key={segIndex}
                  points={segment.map((p) => `${p.x},${p.y}`).join(' ')}
                  fill="none"
                  stroke={color}
                  strokeWidth={2}
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              ))}
              {/* >=8px markers with a 2px ring in the surface colour, per the
                  dataviz mark spec — the ring is what keeps a dot legible
                  where two platforms' lines cross. Each point is its own
                  focusable role="button" (matching CategoryBarChart's
                  established fix for this exact problem): the <title>
                  tooltip below is invisible to a screen reader unless its
                  element is actually reachable, which role="img" on the
                  parent <svg> would otherwise forbid. */}
              {points.map((p) => (
                <g
                  key={p.month}
                  tabIndex={0}
                  role="button"
                  aria-label={`${displayNames.get(source)} — ${monthLabel(p.month)}: ${p.rate.toFixed(2)}%`}
                  className="cursor-pointer [outline:none] [&:focus-visible]:[outline:2px_solid_var(--ring)] [&:focus-visible]:[outline-offset:2px]"
                >
                  <circle cx={p.x} cy={p.y} r={4} fill={color} stroke="var(--card)" strokeWidth={2}>
                    <title>
                      {displayNames.get(source)} — {monthLabel(p.month)}: {p.rate.toFixed(2)}%
                    </title>
                  </circle>
                </g>
              ))}
            </g>
          )
        })}
      </svg>

      <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1">
        {seriesOrder.map((source, i) => (
          <div key={source} className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <span
              aria-hidden="true"
              className="inline-block size-2.5 rounded-full"
              style={{ backgroundColor: SERIES_COLORS[i % SERIES_COLORS.length] }}
            />
            {displayNames.get(source)}
          </div>
        ))}
      </div>

      {/* Text/table alternative to the SVG line chart above — every sibling
          chart in this folder (MarginTrendChart, CategoryBarChart,
          CompositionPieChart, PromoRoiChart) already offers this; this was
          the one that shipped without it (a QA-found accessibility gap: a
          screen-reader user got only the one static aria-label sentence and
          none of the underlying month-by-month percentages). */}
      <div className="mt-3 flex flex-wrap items-center justify-end border-t border-border/60 pt-2.5">
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
          {columnFilterState.isFiltered ? (
            <div className="mb-1.5 flex items-center justify-end gap-2">
              <span className="text-xs text-muted-foreground" aria-live="polite">
                {columnFilterState.filteredRows.length} of {periods.length} shown
              </span>
              <Button type="button" variant="ghost" size="sm" onClick={columnFilterState.clearAll}>
                <X aria-hidden="true" />
                Clear filters
              </Button>
            </div>
          ) : null}

          {columnFilterState.filteredRows.length === 0 ? (
            <FilterEmptyState
              label="No months match these filters."
              onClear={columnFilterState.clearAll}
            />
          ) : (
            <div className="relative">
            <div ref={rateTableScrollRef} className="overflow-x-auto">
              <table className="w-full min-w-[320px] text-left text-xs">
                <caption className="sr-only">
                  Effective commission rate trend across {periods.length} months, for{' '}
                  {seriesOrder.map((s) => displayNames.get(s)).join(' and ')}
                </caption>
                <thead>
                  <tr className="border-b border-border text-muted-foreground">
                    <th scope="col" className="py-1.5 pr-3 font-medium">
                      Month
                    </th>
                    {seriesOrder.map((source, seriesIndex) => {
                      const columnIndex = seriesIndex + 1
                      return (
                        <th key={source} scope="col" className="py-1.5 pr-3 font-medium">
                          <span className="inline-flex items-center gap-1">
                            {displayNames.get(source)}
                            <ColumnFilterButton
                              type="numeric"
                              columnLabel={displayNames.get(source) ?? source}
                              {...columnFilterState.getNumericRange(columnIndex)}
                              onApply={(min, max) =>
                                columnFilterState.setNumericRange(columnIndex, min, max)
                              }
                              onClear={() => columnFilterState.clearColumn(columnIndex)}
                            />
                          </span>
                        </th>
                      )
                    })}
                  </tr>
                </thead>
                <tbody>
                  {columnFilterState.filteredRows.map((period) => (
                    <tr key={period.month} className="border-b border-border/60">
                      <td className="py-1.5 pr-3 text-foreground">{monthLabel(period.month)}</td>
                      {seriesOrder.map((source) => {
                        const platform = period.platforms.find((p) => p.source === source)
                        const rate = platform ? parsePercent(platform.effective_rate) : null
                        return (
                          <td key={source} className="py-1.5 pr-3 tabular-nums text-foreground">
                            {rate === null ? 'No sales' : `${rate.toFixed(2)}%`}
                          </td>
                        )
                      })}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {rateTableCanScrollRight && (
              <div
                aria-hidden="true"
                className="pointer-events-none absolute inset-y-0 right-0 w-8 bg-gradient-to-r from-transparent to-card"
              />
            )}
            </div>
          )}
        </div>
      ) : null}
    </div>
  )
}
