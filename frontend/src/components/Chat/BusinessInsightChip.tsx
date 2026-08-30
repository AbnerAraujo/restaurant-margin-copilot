import * as React from 'react'
import { Lightbulb, Loader2, Sparkles } from 'lucide-react'

import type { ChatToolCall } from '@/components/Chat/ChatPanel'
import { explainRequestFailure } from '@/lib/requestFailure'

/**
 * Business Insight Advisor (specs/009-business-insight-advisor), frontend
 * half. The teaser below is a DETERMINISTIC Go derivation carried on the
 * answer (`AskResponse.business_insight`) — zero model calls, just a kind
 * tag and a title. The full advice text does not exist until the owner
 * taps: tapping runs exactly one real, billed Claude Sonnet 5 call
 * (`POST /api/business-insight`), whose real cost is then shown right
 * next to the advice — this project never lets a model call look free.
 *
 * Visual language is deliberately NOT the answer card's calm
 * `border-border bg-card` treatment: advice is probabilistic content, a
 * fundamentally different epistemic category from every provenance-backed
 * number above it, so it gets a dashed, warning-tinted surface and an
 * explicit "AI suggestion" label — distinguishable at a glance, before
 * reading a word (spec User Story 3 / SC-004).
 */

/** Mirrors `httpapi.BusinessInsightTeaser`'s wire shape. */
export interface BusinessInsightTeaser {
  kind: string
  title: string
}

/** Mirrors `httpapi.BusinessInsightResponse`'s wire shape. */
export interface BusinessInsightAdvice {
  kind: string
  advice_text: string
  /** The backend's own statement of what this content is and is not. */
  disclaimer: string
  interaction: {
    model_used: string
    input_tokens: number
    output_tokens: number
    estimated_cost_usd: number
    latency_ms: number
  }
}

/**
 * Fetches the full advice for a tapped teaser. Injected (never a direct
 * fetch here) for the same reason `ChatPanelProps.resolveAnswer` is: the
 * page-level wiring owns the HTTP call and the cost-panel logging, and
 * tests can count/control calls without a network.
 */
export type ResolveBusinessInsight = (
  kind: string,
  toolCalls: ChatToolCall[],
) => Promise<BusinessInsightAdvice>

type FetchState =
  | { phase: 'idle' }
  | { phase: 'loading' }
  | { phase: 'error'; message: string }
  | { phase: 'loaded'; advice: BusinessInsightAdvice }

export default function BusinessInsightChip({
  teaser,
  toolCalls,
  resolveBusinessInsight,
}: {
  teaser: BusinessInsightTeaser
  /**
   * The SAME tool_calls data this answer already carries (FR-003's "show
   * your work" payload) — posted back verbatim so the backend can
   * re-derive the trigger and ground the advice in exactly the computed
   * results on screen. No server-side per-answer state exists.
   */
  toolCalls: ChatToolCall[]
  resolveBusinessInsight: ResolveBusinessInsight
}) {
  // Never auto-fetched: the advice is generated only on explicit tap
  // (spec FR-014 / SC-002), and once fetched it is kept so collapse/
  // re-expand never bills a second call.
  const [state, setState] = React.useState<FetchState>({ phase: 'idle' })
  const [open, setOpen] = React.useState(false)
  const panelId = React.useId()

  async function handleTap() {
    if (state.phase === 'loading') return
    if (state.phase === 'loaded') {
      setOpen((wasOpen) => !wasOpen)
      return
    }
    setState({ phase: 'loading' })
    try {
      const advice = await resolveBusinessInsight(teaser.kind, toolCalls)
      setState({ phase: 'loaded', advice })
      setOpen(true)
    } catch (error) {
      setState({ phase: 'error', message: explainRequestFailure(error) })
    }
  }

  return (
    <div className="space-y-2">
      <button
        type="button"
        onClick={() => void handleTap()}
        aria-expanded={state.phase === 'loaded' && open}
        aria-controls={panelId}
        disabled={state.phase === 'loading'}
        className="inline-flex max-w-full items-center gap-1.5 rounded-full border border-dashed
          border-warning/50 bg-warning/5 px-3 py-1.5 text-left text-xs font-medium text-foreground
          transition-colors hover:bg-warning/10 focus-visible:outline-none
          focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:opacity-70"
      >
        {state.phase === 'loading' ? (
          <Loader2 className="size-3.5 shrink-0 animate-spin text-warning-text" aria-hidden="true" />
        ) : (
          <Lightbulb className="size-3.5 shrink-0 text-warning-text" aria-hidden="true" />
        )}
        <span className="shrink-0 font-semibold uppercase tracking-wide text-warning-text">
          AI suggestion
        </span>
        <span className="min-w-0">
          {state.phase === 'loading' ? 'Generating the suggestion…' : teaser.title}
        </span>
      </button>

      {state.phase === 'error' ? (
        <p className="text-xs text-destructive-text" role="alert">
          Couldn&apos;t generate the suggestion: {state.message} — tap again to retry.
        </p>
      ) : null}

      {state.phase === 'loaded' && open ? (
        <div
          id={panelId}
          role="group"
          aria-label="AI business suggestion"
          className="space-y-2 rounded-lg border border-dashed border-warning/40 bg-warning/5 px-3.5 py-3"
        >
          <p className="whitespace-pre-line text-sm leading-relaxed text-foreground">
            {state.advice.advice_text}
          </p>
          <div className="space-y-1 border-t border-dashed border-warning/30 pt-2">
            <p className="flex items-start gap-1.5 text-micro text-muted-foreground">
              <Sparkles className="mt-0.5 size-3 shrink-0" aria-hidden="true" />
              <span>{state.advice.disclaimer}</span>
            </p>
            {/* The call's real, just-measured cost — same transparency
                discipline as the cost panel: a model call never looks
                free. */}
            <p className="text-micro tabular-nums text-muted-foreground">
              This suggestion cost ${state.advice.interaction.estimated_cost_usd.toFixed(3)} ·{' '}
              {state.advice.interaction.model_used} ·{' '}
              {state.advice.interaction.input_tokens.toLocaleString()} in /{' '}
              {state.advice.interaction.output_tokens.toLocaleString()} out tokens
            </p>
          </div>
        </div>
      ) : null}
    </div>
  )
}
