import { forwardRef, useId, useState } from 'react'
import { ChevronUp } from 'lucide-react'

import { cn } from '@/lib/utils'

/**
 * The subset of `QuestionInteraction` (data-model.md) this panel aggregates
 * into a running total, per FR-008/FR-009: one entry per answered question.
 */
export interface CostInteraction {
  model_used: string
  input_tokens: number
  output_tokens: number
  estimated_cost_usd: number
  latency_ms: number
}

export interface CostPanelProps {
  /** All interactions logged so far in the session, in any order. */
  interactions: CostInteraction[]
  className?: string
}

function formatUsd(amountUsd: number): string {
  return `$${amountUsd.toFixed(3)}`
}

function sum(values: number[]): number {
  return values.reduce((total, value) => total + value, 0)
}

// One millionth of a dollar. `estimated_cost_usd` (llmclient.EstimateCostUSD)
// prices Haiku/Sonnet tokens at $1-$10 per million tokens, so a single call
// is routinely a few hundredths of a cent (e.g. $0.00051) — an order of
// magnitude too small for integer *cents* to represent without rounding
// every call to $0.00. Micro-dollars is the smallest minor unit that still
// holds this project's own display precision (three decimal places) exactly.
const MICRO_USD_PER_USD = 1_000_000

function toMicroUsd(amountUsd: number): number {
  return Math.round(amountUsd * MICRO_USD_PER_USD)
}

/**
 * Sums cost figures as integer micro-dollars rather than summing the raw
 * floats directly. This total is a genuine exception to `stat.tsx`'s "a
 * presentation component never does arithmetic" rule — see that file's doc
 * comment for the boundary this draws — because there is no server-side
 * figure to fetch instead: `estimated_cost_usd` per interaction is real
 * reconciliation-adjacent math (`llmclient.EstimateCostUSD`), but *this*
 * total is a running sum across an ephemeral, browser-local session
 * (`AppShell`'s in-memory `interactions` state) that the backend has no
 * concept of — `SumEstimatedCostUSD` sums the entire, unscoped
 * `question_interaction` table and isn't reachable from any HTTP endpoint
 * this page calls. Inventing a session-scoped backend endpoint just to move
 * this one sum server-side would be new product surface for a cost-telemetry
 * display, not a fix for a reconciliation defect.
 *
 * What IS worth fixing, and what this function actually does: summing many
 * small IEEE-754 floats (as low as $0.0001) directly can accumulate binary
 * rounding drift exactly the way `0.1 + 0.2 !== 0.3` does. Converting each
 * figure to an integer number of micro-dollars, summing those as integers,
 * then converting back, makes the summation itself exact — the same
 * technique `internal/money` uses in cents, sized for this domain's smaller
 * unit.
 */
function sumCostUsd(costsUsd: number[]): number {
  const totalMicroUsd = sum(costsUsd.map(toMicroUsd))
  return totalMicroUsd / MICRO_USD_PER_USD
}

/**
 * The running-cost stat required by FR-009: a small, always-visible corner
 * pill (never a hero element, per design-tokens.md §4) showing session cost
 * at a glance, with tokens/latency detail available on demand without
 * competing for attention with the day's margin figure.
 *
 * Forwards a ref to its own root (rather than a plain function component)
 * so `AppShell` can measure this element's REAL rendered height — which
 * changes a lot between collapsed (~40px pill) and expanded (~180px pill +
 * detail box) — and reserve exactly that much clearance at the bottom of
 * the routed content instead of guessing a fixed number. See AppShell's own
 * doc comment on `costPanelHeight` for why this is a genuine layout fix (QA
 * found the expanded detail box sitting on top of the `/ask` composer's
 * Send button) and not just cosmetic.
 */
const CostPanel = forwardRef<HTMLDivElement, CostPanelProps>(function CostPanel(
  { interactions, className },
  ref,
) {
  const [open, setOpen] = useState(false)
  const detailId = useId()

  const totalCostUsd = sumCostUsd(interactions.map((i) => i.estimated_cost_usd))
  const totalTokens = sum(
    interactions.map((i) => i.input_tokens + i.output_tokens),
  )
  const averageLatencyMs =
    interactions.length === 0
      ? null
      : Math.round(sum(interactions.map((i) => i.latency_ms)) / interactions.length)

  return (
    <div
      ref={ref}
      className={cn(
        'fixed bottom-4 right-4 z-20 flex flex-col items-end gap-1.5',
        className,
      )}
    >
      {open && (
        <div
          id={detailId}
          role="group"
          aria-label="Session cost detail"
          className="w-52 rounded-md border border-border bg-popover p-3 shadow-sm"
        >
          <dl className="space-y-1.5">
            <div className="flex items-center justify-between gap-3">
              <dt className="text-xs text-muted-foreground">Interactions</dt>
              <dd className="text-xs font-medium tabular-nums text-popover-foreground">
                {interactions.length}
              </dd>
            </div>
            <div className="flex items-center justify-between gap-3">
              <dt className="text-xs text-muted-foreground">Tokens</dt>
              <dd className="text-xs font-medium tabular-nums text-popover-foreground">
                {totalTokens.toLocaleString('en-US')}
              </dd>
            </div>
            <div className="flex items-center justify-between gap-3">
              <dt className="text-xs text-muted-foreground">Avg latency</dt>
              <dd className="text-xs font-medium tabular-nums text-popover-foreground">
                {averageLatencyMs === null ? '—' : `${averageLatencyMs}ms`}
              </dd>
            </div>
          </dl>
        </div>
      )}

      <button
        type="button"
        aria-expanded={open}
        aria-controls={detailId}
        onClick={() => setOpen((wasOpen) => !wasOpen)}
        className="flex items-center gap-1.5 rounded-full border border-border bg-card/95 px-3 py-1.5 shadow-sm backdrop-blur-sm hover:bg-card focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
      >
        <span className="text-xs text-muted-foreground">Session cost</span>
        <span className="text-sm font-semibold tabular-nums text-foreground">
          {formatUsd(totalCostUsd)}
        </span>
        <ChevronUp
          className={cn(
            'size-3 text-muted-foreground transition-transform',
            !open && 'rotate-180',
          )}
          aria-hidden="true"
        />
      </button>
    </div>
  )
})

export default CostPanel
