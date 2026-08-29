import { CircleSlash, Info, type LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

/**
 * Stat / StatGroup — the KPI primitive.
 *
 * Why this exists: every screen in this app was burying already-computed
 * figures inside prose. `/close` said "2026-08-14 · margin on $402.50 in gross
 * sales, $30.11 commissions and $0.00 refunds already netted out" — four
 * deterministic numbers compressed into one 12px sentence above the margin.
 * Those numbers are the product. This component gives them a form.
 *
 * It renders values, it never produces them. Every `value` handed in is a
 * string the Go engine formatted or a `formatUsd` of a decimal the API sent;
 * nothing here adds, subtracts, or rounds. The deterministic/probabilistic
 * boundary in CLAUDE.md holds at this layer too: a presentation component that
 * did arithmetic would be a second implementation of the reconciliation math
 * living on the client, which is the same defect class as an LLM computing a
 * figure.
 *
 * Scope: this is a rule about *reconciliation* figures — sales, margin,
 * commissions, refunds, week-over-week deltas — numbers that came from
 * ledger data and where a client-side re-derivation would be a second,
 * possibly-divergent implementation of that math. It is not a blanket ban on
 * any arithmetic anywhere in the client. `CostPanel`'s running session-cost
 * total is the one documented exception: it sums per-call cost *estimates*
 * (already model-usage telemetry, not ledger money) across an ephemeral
 * browser session the backend has no matching concept of to sum server-side.
 * See `CostPanel.tsx`'s `sumCostUsd` for why that specific sum stays
 * client-side and how it avoids the float-summation drift that would
 * otherwise apply.
 *
 * Structure follows the linear-app density model: label as a micro overline,
 * value in tabular figures at a much larger step (extreme scale contrast, not
 * a timid one), cells separated by hairlines rather than each getting its own
 * card.
 */

/** How a figure should be read, which decides its colour AND its sign glyph. */
export type StatTone = 'neutral' | 'positive' | 'negative'

export interface StatProps {
  /** Micro overline. Sentence case, no trailing colon. */
  label: string
  /**
   * The already-formatted figure, exactly as it should appear. Pass `null`
   * for "the deterministic layer produced no figure for this" — which renders
   * as an explicit unavailable state, never as a zero. A zero and an absence
   * are different facts and this product's entire claim rests on not
   * conflating them.
   */
  value: string | null
  /** Shown in place of the value when `value` is null. */
  unavailableLabel?: string
  /** One short qualifier under the value. Not a sentence, not an explanation. */
  caption?: string
  /**
   * A short explanation of what this figure means, shown in a tooltip
   * behind an info affordance next to the label — for a stat whose label
   * alone doesn't make its meaning obvious (e.g. "Days with a flag" reads
   * as ambiguous without saying a flag means a reconciliation discrepancy
   * already caught, not an open problem needing action).
   */
  tooltip?: string
  tone?: StatTone
  size?: 'sm' | 'md' | 'lg'
  icon?: LucideIcon
  /** Provenance trigger or any trailing affordance, rendered under the value. */
  footer?: ReactNode
  className?: string
}

const VALUE_SIZE: Record<NonNullable<StatProps['size']>, string> = {
  sm: 'text-lg',
  md: 'text-2xl',
  lg: 'text-4xl',
}

const TONE_CLASS: Record<StatTone, string> = {
  neutral: 'text-foreground',
  positive: 'text-success-text',
  negative: 'text-destructive-text',
}

export function Stat({
  label,
  value,
  unavailableLabel = 'Not available',
  caption,
  tooltip,
  tone = 'neutral',
  size = 'md',
  icon: Icon,
  footer,
  className,
}: StatProps) {
  const unavailable = value === null

  return (
    <div className={cn('min-w-0', className)}>
      <dt className="flex items-start gap-1.5 text-micro font-medium uppercase tracking-wider text-muted-foreground">
        {Icon ? <Icon className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" /> : null}
        {/* Reported live at 390px: single-line `truncate` cut labels like
            "Days with a flag" off mid-word ("DAYS WITH A FL…"). This is a
            short overline, not prose that needs a hard one-line clamp — wrap
            up to two lines instead, so a narrow column shows the whole
            label rather than an unreadable fragment. */}
        <span className="line-clamp-2">{label}</span>
        {tooltip ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                type="button"
                className="inline-flex shrink-0 rounded-full text-muted-foreground/70 hover:text-foreground focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
                aria-label={`What does "${label}" mean?`}
              >
                <Info className="size-3" aria-hidden="true" />
              </button>
            </TooltipTrigger>
            <TooltipContent>{tooltip}</TooltipContent>
          </Tooltip>
        ) : null}
      </dt>
      <dd className="mt-1">
        {unavailable ? (
          // Shape + icon + words, never colour alone and never a $0 bar's
          // textual equivalent. Matches how the charts render a refused
          // campaign, so an absent figure looks the same everywhere.
          <span className="inline-flex items-center gap-1.5 rounded-md border border-dashed border-border px-2 py-0.5 text-sm font-medium text-muted-foreground">
            <CircleSlash className="size-3.5 shrink-0" aria-hidden="true" />
            {unavailableLabel}
          </span>
        ) : (
          <span
            className={cn(
              'block truncate font-semibold tabular-nums tracking-tight',
              VALUE_SIZE[size],
              TONE_CLASS[tone],
            )}
          >
            {value}
          </span>
        )}
        {caption ? (
          // Same fix as the label above: captions like a platform name or a
          // "08/01–08/14, this period" date range were the other reported
          // mid-word truncations at mobile widths.
          <span className="mt-0.5 line-clamp-2 text-xs text-muted-foreground">
            {caption}
          </span>
        ) : null}
        {footer ? <div className="mt-1.5">{footer}</div> : null}
      </dd>
    </div>
  )
}

/**
 * A row of stats on one hairline-separated rail. `auto-fit`, not `auto-fill`,
 * so three stats stretch across the row instead of clustering left against a
 * phantom fourth track.
 */
export function StatGroup({
  children,
  className,
  ...rest
}: {
  children: ReactNode
  className?: string
} & React.HTMLAttributes<HTMLElement>) {
  return (
    <dl
      {...rest}
      className={cn(
        'grid gap-x-6 gap-y-5 [grid-template-columns:repeat(auto-fit,minmax(min(9rem,100%),1fr))]',
        // Hairline rails between cells, drawn only where a cell actually has a
        // neighbour on that side, so a wrapped last row never trails a rule
        // into empty space.
        '[&>*]:relative [&>*+*]:sm:border-l [&>*+*]:sm:border-border [&>*+*]:sm:pl-6',
        className,
      )}
    >
      {children}
    </dl>
  )
}

/**
 * Loading placeholder with the same geometry as a real `Stat`, so the layout
 * does not shift when the fetch resolves (CLS) and the page never renders as
 * an empty card that reads as "finished and empty" rather than "loading".
 */
export function StatSkeleton({ size = 'md' }: { size?: StatProps['size'] }) {
  return (
    <div aria-hidden="true" className="min-w-0">
      <div className="h-3 w-20 rounded-sm bg-muted" />
      <div
        className={cn(
          'mt-2 rounded-sm bg-muted',
          size === 'lg' ? 'h-9 w-32' : size === 'sm' ? 'h-5 w-16' : 'h-7 w-24',
        )}
      />
    </div>
  )
}
