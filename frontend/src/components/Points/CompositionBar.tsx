import { BadgeCheck, ShieldCheck, type LucideIcon } from 'lucide-react'

import { Chip } from '@/components/ui/page'
import { cn } from '@/lib/utils'
import type { PointsLine } from './usePoints'

const LINE_ICON: Record<PointsLine['code'], LucideIcon> = {
  clean_close: BadgeCheck,
  discrepancy_catcher: ShieldCheck,
}

/** Fill used for that line's share of the composition bar. */
const LINE_FILL: Record<PointsLine['code'], string> = {
  clean_close: 'bg-success',
  discrepancy_catcher: 'bg-warning',
}

const LINE_SWATCH_TEXT: Record<PointsLine['code'], string> = {
  clean_close: 'text-success-text',
  discrepancy_catcher: 'text-warning-text',
}

const LINE_BLURB: Record<PointsLine['code'], string> = {
  clean_close: 'Days closed clean',
  discrepancy_catcher: 'Days something was caught',
}

/**
 * How the balance divides between the two earning rules, drawn from the same
 * `breakdown` figures printed beside it. The widths are percentages of the
 * total for geometry only — no money is computed here, and each segment's
 * real point value is printed as text next to it, so the bar is a second
 * reading of the numbers rather than the only one.
 *
 * Shared by `PointsCard` (the full `/points` page) and `HomePage`'s points
 * summary — one component so the two surfaces can never silently disagree
 * about how a balance is drawn.
 */
export default function CompositionBar({
  breakdown,
  total,
  showBlurb = true,
}: {
  breakdown: PointsLine[]
  total: number
  /** Home's summary omits the "Days closed clean" style blurb to stay compact. */
  showBlurb?: boolean
}) {
  if (total <= 0 || breakdown.length === 0) return null

  return (
    <div>
      <div
        className="flex h-2 w-full gap-0.5 overflow-hidden rounded-full bg-muted"
        role="img"
        aria-label={breakdown
          .map((line) => `${line.name}: ${line.points} of ${total} points`)
          .join('; ')}
      >
        {breakdown.map((line) => (
          <span
            key={line.code}
            className={cn('h-full', LINE_FILL[line.code])}
            style={{ width: `${(line.points / total) * 100}%` }}
          />
        ))}
      </div>

      <ul className="mt-4 space-y-0">
        {breakdown.map((line) => {
          const Icon = LINE_ICON[line.code]
          return (
            <li
              key={line.code}
              className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-border py-2.5 last:border-b-0 text-sm"
            >
              <Icon
                className={cn('size-4 shrink-0', LINE_SWATCH_TEXT[line.code])}
                aria-hidden="true"
              />
              <span className="font-medium text-foreground">{line.name}</span>
              {showBlurb ? (
                <span className="text-xs text-muted-foreground">
                  {LINE_BLURB[line.code]}
                </span>
              ) : null}
              <span className="ml-auto flex shrink-0 items-center gap-2">
                <Chip>
                  {line.count} × {line.points_each}
                </Chip>
                <span className="w-14 text-right font-semibold tabular-nums text-foreground">
                  +{line.points.toLocaleString('en-US')}
                </span>
              </span>
            </li>
          )
        })}
      </ul>
    </div>
  )
}
