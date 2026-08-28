import { useId, useState } from 'react'
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

/**
 * The running-cost stat required by FR-009: a small, always-visible corner
 * pill (never a hero element, per design-tokens.md §4) showing session cost
 * at a glance, with tokens/latency detail available on demand without
 * competing for attention with the day's margin figure.
 */
function CostPanel({ interactions, className }: CostPanelProps) {
  const [open, setOpen] = useState(false)
  const detailId = useId()

  const totalCostUsd = sum(interactions.map((i) => i.estimated_cost_usd))
  const totalTokens = sum(
    interactions.map((i) => i.input_tokens + i.output_tokens),
  )
  const averageLatencyMs =
    interactions.length === 0
      ? null
      : Math.round(sum(interactions.map((i) => i.latency_ms)) / interactions.length)

  return (
    <div
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
}

export default CostPanel
