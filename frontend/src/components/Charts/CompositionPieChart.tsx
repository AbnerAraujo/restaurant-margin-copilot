import { useState } from 'react'

import { cn } from '@/lib/utils'
import {
  CATEGORICAL_FILLS,
  MAX_CATEGORICAL_SERIES,
  type VisualizationPoint,
} from './answerVisualization'

export interface CompositionPieChartProps {
  title: string
  subtitle?: string
  valueLabel?: string
  points: VisualizationPoint[]
  sourceTool?: string
  className?: string
}

// ---------------------------------------------------------------------------
// Geometry. A donut rather than a filled pie: the hole carries the total, so
// the reader gets the whole AND the split from one mark instead of having to
// add the slices back up.
// ---------------------------------------------------------------------------

const SIZE = 220
const CENTER = SIZE / 2
const OUTER_RADIUS = 92
const INNER_RADIUS = 58
const SEGMENT_GAP_DEGREES = 1.6 // ~2px of surface between fills at this radius

function polar(radius: number, degrees: number): [number, number] {
  const radians = ((degrees - 90) * Math.PI) / 180
  return [CENTER + radius * Math.cos(radians), CENTER + radius * Math.sin(radians)]
}

function donutSegmentPath(startDeg: number, endDeg: number): string {
  const sweep = endDeg - startDeg
  if (sweep <= 0) return ''
  const largeArc = sweep > 180 ? 1 : 0
  const [outerStartX, outerStartY] = polar(OUTER_RADIUS, startDeg)
  const [outerEndX, outerEndY] = polar(OUTER_RADIUS, endDeg)
  const [innerEndX, innerEndY] = polar(INNER_RADIUS, endDeg)
  const [innerStartX, innerStartY] = polar(INNER_RADIUS, startDeg)
  return [
    `M${outerStartX},${outerStartY}`,
    `A${OUTER_RADIUS},${OUTER_RADIUS} 0 ${largeArc} 1 ${outerEndX},${outerEndY}`,
    `L${innerEndX},${innerEndY}`,
    `A${INNER_RADIUS},${INNER_RADIUS} 0 ${largeArc} 0 ${innerStartX},${innerStartY}`,
    'Z',
  ].join(' ')
}

function formatUsd(value: number): string {
  return value.toLocaleString('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
}

interface Segment {
  point: VisualizationPoint
  index: number
  startDeg: number
  endDeg: number
  share: number
  fill: string
}

/**
 * Part-to-whole donut for one answer's composition — today only "where the
 * day's revenue came from", the single genuine part-to-whole in this data
 * model.
 *
 * The backend gates this form at 3–6 slices (`MinPieSlices`/`MaxPieSlices` in
 * `visualization.go`), so the two anti-patterns this form is prone to — a
 * two-slice pie, and a pie with more segments than a reader can separate —
 * cannot reach this component. Color job here is IDENTITY (which platform),
 * so it uses the validated categorical `--chart-*` hues in fixed order, never
 * the status palette. Every slice is also direct-labelled with its share and
 * listed in the legend, so identity never rests on colour alone.
 */
export default function CompositionPieChart({
  title,
  subtitle,
  valueLabel,
  points,
  sourceTool,
  className,
}: CompositionPieChartProps) {
  const [hoveredIndex, setHoveredIndex] = useState<number | null>(null)
  const [tableOpen, setTableOpen] = useState(false)

  // A negative or unavailable value has no meaning as a share of a whole, so
  // it is excluded from the geometry rather than drawn as a nonsense arc.
  const slices = points.filter((point) => !point.unavailable && point.value > 0)
  const total = slices.reduce((sum, point) => sum + point.value, 0)

  let cursorDeg = 0
  const segments: Segment[] = slices.map((point, index) => {
    const sweep = total > 0 ? (point.value / total) * 360 : 0
    const startDeg = cursorDeg
    cursorDeg += sweep
    return {
      point,
      index,
      startDeg: startDeg + SEGMENT_GAP_DEGREES / 2,
      endDeg: cursorDeg - SEGMENT_GAP_DEGREES / 2,
      share: total > 0 ? point.value / total : 0,
      fill:
        index < MAX_CATEGORICAL_SERIES
          ? CATEGORICAL_FILLS[index]
          : 'var(--muted-foreground)',
    }
  })

  const hovered = hoveredIndex === null ? null : segments[hoveredIndex]

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

      <div className="flex flex-wrap items-center gap-4">
        <div className="relative shrink-0">
          <svg
            viewBox={`0 0 ${SIZE} ${SIZE}`}
            // See CategoryBarChart: role="img" cannot contain the focusable
            // per-slice targets below (axe nested-interactive).
            role="group"
            aria-label={`${title}. ${segments
              .map(
                (segment) =>
                  `${segment.point.label}: ${segment.point.display}, ${Math.round(segment.share * 100)} percent`,
              )
              .join('. ')}`}
            className="h-[180px] w-[180px]"
          >
            {segments.map((segment) => (
              <path
                key={segment.point.label}
                d={donutSegmentPath(segment.startDeg, segment.endDeg)}
                fill={segment.fill}
                opacity={
                  hoveredIndex === null || hoveredIndex === segment.index
                    ? 1
                    : 0.45
                }
                tabIndex={0}
                role="button"
                aria-label={`${segment.point.label}: ${segment.point.display}, ${Math.round(segment.share * 100)} percent of the total`}
                onMouseEnter={() => setHoveredIndex(segment.index)}
                onMouseLeave={() => setHoveredIndex(null)}
                onFocus={() => setHoveredIndex(segment.index)}
                onBlur={() => setHoveredIndex(null)}
                className="cursor-pointer [outline:none] [&:focus-visible]:[outline:2px_solid_var(--ring)] [&:focus-visible]:[outline-offset:2px]"
              />
            ))}

            {/* The hole carries the total, so the whole is stated rather than
                left for the reader to reconstruct from the slices. */}
            <text
              x={CENTER}
              y={CENTER - 6}
              textAnchor="middle"
              className="fill-foreground text-[15px] font-semibold tabular-nums"
            >
              {formatUsd(total)}
            </text>
            <text
              x={CENTER}
              y={CENTER + 12}
              textAnchor="middle"
              className="fill-muted-foreground text-[9px] uppercase tracking-wide"
            >
              Total
            </text>
          </svg>

          {hovered ? (
            <div
              role="status"
              className="pointer-events-none absolute left-1/2 top-0 z-10 -translate-x-1/2 -translate-y-1/2
                rounded-md border border-border bg-popover px-2.5 py-1.5 text-xs shadow-md"
            >
              <p className="font-medium text-foreground">
                {hovered.point.label}
              </p>
              <p className="tabular-nums text-muted-foreground">
                {hovered.point.display} · {Math.round(hovered.share * 100)}%
              </p>
            </div>
          ) : null}
        </div>

        {/* Legend doubles as the direct labelling: swatch, name, amount and
            share on one line each, which is more readable at this size than
            leader lines into thin arcs. */}
        <ul aria-label="Chart legend" className="min-w-[9rem] flex-1 space-y-1.5">
          {segments.map((segment) => (
            <li
              key={segment.point.label}
              className="flex items-baseline justify-between gap-3 text-xs"
            >
              <span className="flex items-center gap-1.5 text-muted-foreground">
                <span
                  className="size-2.5 shrink-0 rounded-sm"
                  style={{ backgroundColor: segment.fill }}
                  aria-hidden="true"
                />
                {segment.point.label}
              </span>
              <span className="shrink-0 tabular-nums text-foreground">
                {segment.point.display}
                <span className="ml-1.5 text-muted-foreground">
                  {Math.round(segment.share * 100)}%
                </span>
              </span>
            </li>
          ))}
        </ul>
      </div>

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
        <div className="mt-2 overflow-x-auto">
          <table className="w-full text-left text-xs">
            <caption className="sr-only">{title}, as a table</caption>
            <thead>
              <tr className="border-b border-border text-muted-foreground">
                <th scope="col" className="py-1.5 pr-4 font-medium">
                  Source
                </th>
                <th scope="col" className="py-1.5 pr-4 font-medium">
                  {valueLabel ?? 'Value'}
                </th>
                <th scope="col" className="py-1.5 font-medium">
                  Share
                </th>
              </tr>
            </thead>
            <tbody>
              {segments.map((segment) => (
                <tr
                  key={segment.point.label}
                  className="border-b border-border/60"
                >
                  <td className="py-1.5 pr-4 text-foreground">
                    {segment.point.label}
                  </td>
                  <td className="py-1.5 pr-4 tabular-nums text-foreground">
                    {segment.point.display}
                  </td>
                  <td className="py-1.5 tabular-nums text-muted-foreground">
                    {Math.round(segment.share * 100)}%
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
