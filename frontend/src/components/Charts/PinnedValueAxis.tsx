import { cn } from '@/lib/utils'

export interface PinnedValueAxisProps {
  /** Tick values, in the chart's own data units. */
  ticks: number[]
  /** The tick step, so the formatter can pick its decimal places. */
  step: number
  /** The chart's own value-to-pixel mapping, in viewBox units. */
  yToPixel: (value: number) => number
  /** Total viewBox height, so tick positions become percentages of it. */
  chartHeight: number
  /** Gutter width in CSS pixels — equal to the plot's own left inset. */
  width: number
  /** What the axis measures, e.g. "Margin (USD)". Sentence case, unit named. */
  title: string
  formatTick: (value: number, step: number) => string
  className?: string
}

/**
 * The value axis, frozen in place while the plot scrolls under it.
 *
 * Both bar charts here can grow far wider than their panel — a two-year
 * period is 109 weekly bars, a real promotion list is 29 campaigns — and live
 * inside an `overflow-x-auto` viewport. Before this, the axis lived inside the
 * same SVG as the bars, so scrolling right to reach recent history carried the
 * scale off the left edge: past the first screenful the reader had bars with
 * no numbers against them. Same problem a spreadsheet solves by freezing the
 * header row.
 *
 * Implemented as `position: sticky; left: 0` on a real flex sibling of the
 * plot SVG rather than a second SVG layer, for two reasons:
 *
 *  - Tick positions are percentages of the axis's own height, which the flex
 *    row stretches to exactly the plot's rendered height. That holds however
 *    the SVG is sized, so the labels cannot drift out of alignment with their
 *    own gridlines.
 *  - The labels are HTML text at a fixed 10px, so they stay legible instead of
 *    scaling with the viewBox (the "blown-up thumbnail" failure this codebase
 *    already hit once when the SVG was allowed to scale up).
 *
 * The gutter is painted in the card's own surface colour: without an opaque
 * background, bars scrolling underneath would read straight through the
 * numbers.
 */
export default function PinnedValueAxis({
  ticks,
  step,
  yToPixel,
  chartHeight,
  width,
  title,
  formatTick,
  className,
}: PinnedValueAxisProps) {
  return (
    <div
      // aria-hidden: every one of these values is already in the chart's own
      // aria-label and in its table view, so announcing the axis again would
      // read as a bare list of numbers with no context.
      aria-hidden="true"
      style={{ width, minWidth: width }}
      className={cn(
        'sticky left-0 z-10 shrink-0 self-stretch bg-card',
        className,
      )}
    >
      <span className="absolute left-0 top-1 text-[10px] leading-none text-muted-foreground">
        {title}
      </span>
      {ticks.map((tick) => (
        <span
          key={tick}
          style={{ top: `${(yToPixel(tick) / chartHeight) * 100}%` }}
          className="absolute right-2 -translate-y-1/2 whitespace-nowrap text-[10px] leading-none tabular-nums text-muted-foreground"
        >
          {formatTick(tick, step)}
        </span>
      ))}
      {/* The axis rule itself — a hairline one step off the surface, per the
          dataviz mark spec, and the visual seam between frozen and scrolling. */}
      <span className="absolute inset-y-0 right-0 w-px bg-border" />
    </div>
  )
}
