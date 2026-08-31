import { useState } from 'react'
import { ShieldAlert, X } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { ColumnFilterButton } from '@/components/ui/column-filter'
import { FilterEmptyState } from '@/components/ui/filter-bar'
import { useColumnFilters, type ColumnFilterSpecs } from '@/lib/useColumnFilters'
import { cn } from '@/lib/utils'
import type { VisualizationPoint } from './answerVisualization'

export interface CategoryBarChartProps {
  title: string
  subtitle?: string
  valueLabel?: string
  points: VisualizationPoint[]
  sourceTool?: string
  className?: string
  /**
   * Opt-in Excel/Sheets-style header filter(s) for the "View as table"
   * fallback below the SVG bars — keyed the same way `DataGrid`'s own
   * `columnFilters` prop is (column 0 is Category, column 1 is the value
   * column). Omitted by every caller that doesn't pass it (chat's
   * `AnswerVisualizationView`, which renders a handful of rows scoped to one
   * answer — exactly the case `DataGrid`'s own doc comment says a filter
   * would be a control the reader has to understand before trusting the
   * number), so this stays exactly as plain as before for them. Wired
   * directly against `useColumnFilters` rather than through `DataGrid`,
   * since this table's cells (a red/green figure, an "unavailable" refusal
   * box) are this component's own rendering, not `DataGrid`'s plain strings.
   * The SVG bars above are never affected — only the fallback table narrows.
   */
  columnFilters?: ColumnFilterSpecs
}

// ---------------------------------------------------------------------------
// Geometry. Horizontal bars, not vertical: the categories here are named
// things (campaign ids, date ranges, ISO dates) whose labels do not fit under
// a vertical tick inside a chat bubble, and rotating axis labels to make them
// fit is an anti-pattern. Horizontal rows give every label a full line.
// ---------------------------------------------------------------------------

const CHART_WIDTH = 620
const LABEL_GUTTER = 178
const ROW_HEIGHT = 34
const BAR_HEIGHT = 16 // mark spec: thin marks
const BAR_RADIUS = 4
const MARGIN = { top: 8, right: 84, bottom: 26 }
const PLOT_WIDTH = CHART_WIDTH - LABEL_GUTTER - MARGIN.right
// Wide enough for the longest label these charts actually carry — a
// "2026-08-01 → 2026-08-07" period range at 23 characters — measured against
// the rendered output rather than guessed, since a truncated period label
// hides which period a bar belongs to.
const MAX_LABEL_CHARS = 24
// How much of the label's START survives truncation — see truncateLabel.
// 12 is enough to keep a real platform name ("Just Eat Takeaway") legible
// as an identifier without eating the whole budget, leaving the rest for
// whatever comes after it.
const LABEL_HEAD_CHARS = 12
const UNAVAILABLE_BOX_WIDTH = 104

// Reported live on /platforms: two adjacent rows for the SAME platform —
// "Just Eat Takeaway — commission only" and "Just Eat Takeaway — commission
// + promo" (toChartPoints in PlatformsPage.tsx) — both share an identical
// first 23 characters, so simple end-truncation rendered both as the
// indistinguishable "Just Eat Takeaway — com…" despite showing different
// values. Truncating from the middle instead — a fixed-length head (enough
// to identify the category, e.g. the platform name) plus whatever tail fits
// in the remaining budget (the part that actually varies between otherwise-
// identical labels, e.g. "only" vs "+ promo") — keeps two such labels
// visually distinct instead of colliding on their shared prefix. A label
// short enough to need no truncation (e.g. the 23-char period range this
// budget was originally sized for) is returned unchanged, so this is a
// strict improvement, never a regression, for every existing caller.
function truncateLabel(label: string): string {
  if (label.length <= MAX_LABEL_CHARS) return label
  const tailChars = MAX_LABEL_CHARS - LABEL_HEAD_CHARS - 1
  return `${label.slice(0, LABEL_HEAD_CHARS)}…${label.slice(label.length - tailChars)}`
}

/**
 * Domain always includes zero, so a bar's length is read against a true
 * baseline rather than against the smallest value present — an axis that
 * starts anywhere but zero makes a small difference look like a large one.
 */
function buildDomain(values: number[]): [number, number] {
  const min = Math.min(0, ...values)
  const max = Math.max(0, ...values)
  const span = max - min || 1
  const pad = span * 0.08
  return [min - (min < 0 ? pad : 0), max + (max > 0 ? pad : 0)]
}

/** A rect rounded only on its data end — the tip away from the baseline. */
function roundedBarPath(
  x: number,
  y: number,
  width: number,
  height: number,
  roundedEdge: 'right' | 'left',
): string {
  if (width <= 0) return ''
  const r = Math.min(BAR_RADIUS, width, height / 2)
  if (roundedEdge === 'right') {
    return `M${x},${y} L${x + width - r},${y} Q${x + width},${y} ${x + width},${y + r} L${x + width},${y + height - r} Q${x + width},${y + height} ${x + width - r},${y + height} L${x},${y + height} Z`
  }
  return `M${x + width},${y} L${x + r},${y} Q${x},${y} ${x},${y + r} L${x},${y + height - r} Q${x},${y + height} ${x + r},${y + height} L${x + width},${y + height} Z`
}

/**
 * Diverging horizontal bar chart for one answer's categories — margin per
 * period, margin per day, net ROI per campaign.
 *
 * Color job is POLARITY, not identity, so this uses the app's existing
 * diverging success/destructive pair rather than the categorical `--chart-*`
 * hues: the reader's question is "did this make or lose money", and the
 * categories are already separated by their own labelled rows. Position
 * relative to the zero baseline carries the same meaning independently of
 * colour, which is what makes that pair safe here (the same mitigation
 * `MarginTrendChart` documents).
 *
 * A point the backend marked `unavailable` renders as an explicit refusal
 * box, never a zero-length bar — a bar of length zero reads as "broke even"
 * when the truth is "we will not estimate this" (FR-013).
 */
export default function CategoryBarChart({
  title,
  subtitle,
  valueLabel,
  points,
  sourceTool,
  className,
  columnFilters,
}: CategoryBarChartProps) {
  const [hoveredIndex, setHoveredIndex] = useState<number | null>(null)
  const [tableOpen, setTableOpen] = useState(false)

  // Table-only filtering (see `columnFilters` doc comment above) — a cell
  // marked `unavailable` reads as '' here, which `parseNumericCell` refuses
  // to parse, so an "unavailable" row is correctly excluded from a numeric
  // filter's results rather than guessed at (never coerced to 0).
  const columnFilterSpecs = columnFilters ?? {}
  const hasColumnFilters = Object.keys(columnFilterSpecs).length > 0
  const columnFilterState = useColumnFilters<VisualizationPoint>({
    columns: ['Category', valueLabel ?? 'Value'],
    rows: points,
    specs: columnFilterSpecs,
    getCell: (point, columnIndex) =>
      columnIndex === 0 ? point.label : point.unavailable ? '' : String(point.value),
  })
  const visibleTableRows = hasColumnFilters ? columnFilterState.filteredRows : points

  const plotHeight = points.length * ROW_HEIGHT
  const chartHeight = plotHeight + MARGIN.top + MARGIN.bottom
  const drawable = points.filter((point) => !point.unavailable)
  const [min, max] = buildDomain(drawable.map((point) => point.value))
  const xToPixel = (value: number) =>
    LABEL_GUTTER + ((value - min) / (max - min || 1)) * PLOT_WIDTH
  const baselineX = xToPixel(0)

  const hovered = hoveredIndex === null ? null : points[hoveredIndex]
  const hasUnavailable = points.some((point) => point.unavailable)
  const hasNegative = drawable.some((point) => point.value < 0)
  const hasPositive = drawable.some((point) => point.value >= 0)

  return (
    <figure
      className={cn(
        'rounded-lg border border-border bg-background/60 p-3',
        className,
      )}
    >
      <figcaption className="mb-2">
        <p className="text-sm font-medium text-foreground">{title}</p>
        {subtitle ? (
          <p className="text-xs text-muted-foreground">{subtitle}</p>
        ) : null}
      </figcaption>

      <div className="relative w-full overflow-x-auto">
        <svg
          viewBox={`0 0 ${CHART_WIDTH} ${chartHeight}`}
          // role="group", not role="img": each bar below is a focusable
          // role="button" for keyboard readers, and role="img" forbids
          // focusable descendants (a real axe nested-interactive violation
          // that made those per-bar targets unreachable to assistive tech).
          // The aria-label still gives the whole chart a text alternative.
          role="group"
          aria-label={`${title}. ${points
            .map((point) =>
              point.unavailable
                ? `${point.label}: no figure, ${point.reason ?? 'unavailable'}`
                : `${point.label}: ${point.display}`,
            )
            .join('. ')}`}
          // See MarginTrendChart: capped at its design width so the viewBox
          // never scales the SVG's own text up inside a wide column.
          style={{ maxWidth: CHART_WIDTH }}
          className="h-auto w-full min-w-[340px]"
        >
          {/* Zero baseline — the primary above/below cue, colour-independent */}
          <line
            x1={baselineX}
            x2={baselineX}
            y1={MARGIN.top}
            y2={MARGIN.top + plotHeight}
            stroke="var(--border)"
            strokeWidth={1}
          />

          {points.map((point, index) => {
            const rowY = MARGIN.top + index * ROW_HEIGHT
            const barY = rowY + (ROW_HEIGHT - BAR_HEIGHT) / 2
            const isPositive = point.value >= 0
            const barX = point.unavailable
              ? baselineX
              : Math.min(baselineX, xToPixel(point.value))
            const barWidth = point.unavailable
              ? 0
              : Math.abs(xToPixel(point.value) - baselineX)

            return (
              <g
                key={point.label}
                tabIndex={0}
                role="button"
                aria-label={
                  point.unavailable
                    ? `${point.label}: no figure — ${point.reason ?? 'unavailable'}`
                    : `${point.label}: ${point.display}`
                }
                onMouseEnter={() => setHoveredIndex(index)}
                onMouseLeave={() => setHoveredIndex(null)}
                onFocus={() => setHoveredIndex(index)}
                onBlur={() => setHoveredIndex(null)}
                className="cursor-pointer [outline:none] [&:focus-visible]:[outline:2px_solid_var(--ring)] [&:focus-visible]:[outline-offset:2px]"
              >
                {/* Hit target spans the whole row, wider than the mark */}
                <rect
                  x={0}
                  y={rowY}
                  width={CHART_WIDTH}
                  height={ROW_HEIGHT}
                  fill="transparent"
                />

                <text
                  x={LABEL_GUTTER - 10}
                  y={rowY + ROW_HEIGHT / 2}
                  textAnchor="end"
                  dominantBaseline="middle"
                  className="fill-muted-foreground text-micro"
                >
                  {truncateLabel(point.label)}
                </text>

                {point.unavailable ? (
                  <>
                    <rect
                      x={baselineX + 4}
                      y={barY - 3}
                      width={UNAVAILABLE_BOX_WIDTH}
                      height={BAR_HEIGHT + 6}
                      rx={4}
                      fill="var(--destructive)"
                      fillOpacity={0.06}
                      stroke="var(--destructive)"
                      strokeOpacity={0.4}
                      strokeDasharray="4 3"
                    />
                    <text
                      x={baselineX + 4 + UNAVAILABLE_BOX_WIDTH / 2}
                      y={rowY + ROW_HEIGHT / 2}
                      textAnchor="middle"
                      dominantBaseline="middle"
                      className="fill-destructive-text text-[10px] font-medium"
                    >
                      {point.display}
                    </text>
                  </>
                ) : (
                  <>
                    <path
                      d={roundedBarPath(
                        barX,
                        barY,
                        barWidth,
                        BAR_HEIGHT,
                        isPositive ? 'right' : 'left',
                      )}
                      fill={
                        isPositive ? 'var(--success)' : 'var(--destructive)'
                      }
                      opacity={hoveredIndex === index ? 1 : 0.92}
                    />
                    {/* Direct label on every bar — the category count here is
                        always small, and a labelled bar never depends on the
                        reader tracing back to an axis. */}
                    <text
                      x={
                        isPositive
                          ? baselineX + barWidth + 8
                          : baselineX - barWidth - 8
                      }
                      y={rowY + ROW_HEIGHT / 2}
                      textAnchor={isPositive ? 'start' : 'end'}
                      dominantBaseline="middle"
                      className={cn(
                        'text-micro font-semibold tabular-nums',
                        isPositive
                          ? 'fill-success-text'
                          : 'fill-destructive-text',
                      )}
                    >
                      {point.display}
                    </text>
                  </>
                )}
              </g>
            )
          })}

          {valueLabel ? (
            <text
              x={CHART_WIDTH}
              y={chartHeight - 8}
              textAnchor="end"
              className="fill-muted-foreground text-[10px]"
            >
              {valueLabel}
            </text>
          ) : null}
        </svg>

        {hovered ? (
          <div
            role="status"
            className="pointer-events-none absolute left-1/2 top-0 z-10 -translate-x-1/2 rounded-md
              border border-border bg-popover px-2.5 py-1.5 text-xs shadow-md"
          >
            <p className="font-medium text-foreground">{hovered.label}</p>
            {hovered.unavailable ? (
              <p className="text-destructive-text">
                {hovered.reason ?? 'No figure available'}
              </p>
            ) : (
              <p
                className={cn(
                  'font-semibold tabular-nums',
                  hovered.value >= 0
                    ? 'text-success-text'
                    : 'text-destructive-text',
                )}
              >
                {hovered.display}
              </p>
            )}
          </div>
        ) : null}
      </div>

      {/* Legend — each meaning a swatch plus text, so identity never rests on
          colour alone. Rendered only once there are at least TWO meanings to
          tell apart: on a chart where every bar is above zero (the platform
          cost comparison, caught in the live rendering pass) a lone "Above
          zero" entry restates what the bars already show and costs a row of
          space, which is exactly the single-series legend the dataviz mark
          spec says to leave out. */}
      {[hasPositive, hasNegative, hasUnavailable].filter(Boolean).length < 2 ? null : (
      <ul
        aria-label="Chart legend"
        className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1"
      >
        {hasPositive ? (
          <li className="flex items-center gap-1.5 text-micro text-muted-foreground">
            <span className="size-2.5 rounded-sm bg-success" aria-hidden="true" />
            Above zero
          </li>
        ) : null}
        {hasNegative ? (
          <li className="flex items-center gap-1.5 text-micro text-muted-foreground">
            <span
              className="size-2.5 rounded-sm bg-destructive"
              aria-hidden="true"
            />
            Below zero
          </li>
        ) : null}
        {hasUnavailable ? (
          <li className="flex items-center gap-1.5 text-micro text-muted-foreground">
            <ShieldAlert
              className="size-3 text-destructive-text"
              aria-hidden="true"
            />
            No figure — refused, not zero
          </li>
        ) : null}
      </ul>
      )}

      <div className="mt-2 flex flex-wrap items-center justify-between gap-2 border-t border-border/60 pt-2">
        {sourceTool ? (
          <p className="text-micro text-muted-foreground">
            Computed by <code className="font-mono">{sourceTool}</code>
          </p>
        ) : (
          <span />
        )}
        <button
          type="button"
          onClick={() => setTableOpen((wasOpen) => !wasOpen)}
          aria-expanded={tableOpen}
          className="text-micro text-muted-foreground underline decoration-dotted underline-offset-2
            hover:text-foreground focus-visible:text-foreground focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:rounded-sm"
        >
          {tableOpen ? 'Hide table' : 'View as table'}
        </button>
      </div>

      {tableOpen ? (
        <div className="mt-2">
          {hasColumnFilters && columnFilterState.isFiltered ? (
            <div className="mb-1.5 flex items-center justify-end gap-2">
              <span className="text-xs text-muted-foreground" aria-live="polite">
                {visibleTableRows.length} of {points.length} shown
              </span>
              <Button type="button" variant="ghost" size="sm" onClick={columnFilterState.clearAll}>
                <X aria-hidden="true" />
                Clear filters
              </Button>
            </div>
          ) : null}

          {hasColumnFilters && points.length > 0 && visibleTableRows.length === 0 ? (
            <FilterEmptyState
              label="No rows match this filter."
              onClear={columnFilterState.clearAll}
            />
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-left text-xs">
                <caption className="sr-only">{title}, as a table</caption>
                <thead>
                  <tr className="border-b border-border text-muted-foreground">
                    <th scope="col" className="py-1.5 pr-4 font-medium">
                      Category
                    </th>
                    <th scope="col" className="py-1.5 font-medium">
                      <span className="inline-flex items-center gap-1">
                        {valueLabel ?? 'Value'}
                        {columnFilterSpecs[1] === 'numeric' ? (
                          <ColumnFilterButton
                            type="numeric"
                            columnLabel={valueLabel ?? 'Value'}
                            {...columnFilterState.getNumericRange(1)}
                            onApply={(min, max) => columnFilterState.setNumericRange(1, min, max)}
                            onClear={() => columnFilterState.clearColumn(1)}
                          />
                        ) : null}
                      </span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {visibleTableRows.map((point) => (
                    <tr key={point.label} className="border-b border-border/60">
                      <td className="py-1.5 pr-4 text-foreground">{point.label}</td>
                      <td
                        className={cn(
                          'py-1.5 font-medium tabular-nums',
                          point.unavailable
                            ? 'text-destructive-text'
                            : point.value >= 0
                              ? 'text-success-text'
                              : 'text-destructive-text',
                        )}
                      >
                        {point.unavailable
                          ? `${point.display} — ${point.reason ?? 'no figure'}`
                          : point.display}
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
