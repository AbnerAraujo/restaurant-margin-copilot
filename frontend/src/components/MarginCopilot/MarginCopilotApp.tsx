import { useCallback, useState } from 'react'

import ChatPanel, {
  mockResolveAnswer,
  type AssistantChatMessage,
} from '@/components/Chat/ChatPanel'
import ProvenanceTag, {
  type SourceRowRef,
} from '@/components/Provenance/ProvenanceTag'
import BadgeDisplay, {
  type ReconciliationBadge,
} from '@/components/Badges/BadgeDisplay'
import CostPanel, { type CostInteraction } from '@/components/CostPanel/CostPanel'

// ---------------------------------------------------------------------------
// Mocked "today's close" — a DailyReconciliation row shaped exactly per
// data-model.md (date, margin, discrepancy_flags, source_row_refs). No live
// backend exists yet; this stands in for a `GET /api/reconciliation/:date`
// response so the summary card and its provenance/badge are real,
// independently-testable UI against a realistic fixture shape.
// ---------------------------------------------------------------------------

const TODAY_ISO = '2026-08-27'

const TODAY_SOURCE_REFS: SourceRowRef[] = [
  {
    source_file: 'daily_reconciliation.csv',
    row_start: 27,
    row_end: 27,
    period_start: TODAY_ISO,
    period_end: TODAY_ISO,
  },
  {
    source_file: 'pos_export_2026-08-27.csv',
    row_start: 1,
    row_end: 42,
    period_start: TODAY_ISO,
    period_end: TODAY_ISO,
  },
]

// discrepancy_flags = [] for today's row -> a Clean Close badge fires. A
// Discrepancy Catcher only fires on a row that actually caught something
// (see the Aug 22–23 weekend answer in ChatPanel's seed conversation).
const TODAY_BADGES: ReconciliationBadge[] = [
  { id: `${TODAY_ISO}-clean_close`, type: 'clean_close', date: TODAY_ISO },
]

const TODAY_MARGIN_USD = 612.4
const TODAY_GROSS_SALES_USD = 2180.0

function formatUsd(amount: number): string {
  return amount.toLocaleString('en-US', {
    style: 'currency',
    currency: 'USD',
  })
}

// ---------------------------------------------------------------------------
// Cost instrumentation (FR-008/FR-009) — mirrors the two-step architecture
// documented in CLAUDE.md: every question first runs the cheap Claude Haiku
// 4.5 ambiguity gate; only a question that clears the gate (kind ===
// 'answer') goes on to the Claude Sonnet 5 explanation step. A clarification
// or refusal short-circuits after the gate alone, so it only ever costs one
// model call. `mockResolveAnswer`'s returned `kind` tells us which path
// fired — no need to duplicate its classification logic here.
// ---------------------------------------------------------------------------

const HAIKU_GATE_CALL: CostInteraction = {
  model_used: 'claude-haiku-4-5',
  input_tokens: 420,
  output_tokens: 18,
  estimated_cost_usd: 0.00051,
  latency_ms: 310,
}

const SONNET_EXPLAIN_CALL: CostInteraction = {
  model_used: 'claude-sonnet-5',
  input_tokens: 1180,
  output_tokens: 240,
  estimated_cost_usd: 0.00476,
  latency_ms: 1420,
}

/** One (gate, [explain]) pair per seeded Q&A turn in ChatPanel's demo thread. */
const SEED_INTERACTIONS: CostInteraction[] = [
  HAIKU_GATE_CALL, // "How did we do today?" -> answer
  SONNET_EXPLAIN_CALL,
  HAIKU_GATE_CALL, // "How was the weekend?" -> clarification (gate only)
  HAIKU_GATE_CALL, // "Saturday–Sunday only" -> answer
  SONNET_EXPLAIN_CALL,
  HAIKU_GATE_CALL, // Uber Eats ROI question -> refusal (gate only)
]

/**
 * Top-level page composition: the "Today's Close" reconciliation summary
 * (margin + its provenance + any badge that fired for the day) above the
 * Q&A chat panel, with the running session-cost stat pinned to a corner
 * throughout — the one place all three parallel builds (chat, provenance/
 * cost, badges) come together into a single glanceable screen for a
 * time-poor owner, per docs/product-strategy.md and prd.md §4.
 */
export default function MarginCopilotApp() {
  const [interactions, setInteractions] =
    useState<CostInteraction[]>(SEED_INTERACTIONS)

  // Wraps the chat demo's own mock resolver so every new question also logs
  // the cost interaction(s) its path would have incurred against the real
  // Haiku-gate/Sonnet-explain pipeline — without re-implementing which
  // questions are ambiguous/unanswerable, which `mockResolveAnswer` already
  // decides.
  const resolveAnswerWithCostTracking = useCallback(
    async (question: string): Promise<AssistantChatMessage> => {
      const message = await mockResolveAnswer(question)
      setInteractions((previous) =>
        message.kind === 'answer'
          ? [...previous, HAIKU_GATE_CALL, SONNET_EXPLAIN_CALL]
          : [...previous, HAIKU_GATE_CALL],
      )
      return message
    },
    [],
  )

  return (
    <div className="min-h-screen bg-background px-4 py-6 sm:px-6 lg:px-8">
      <div className="mx-auto flex max-w-3xl flex-col gap-4">
        <header className="flex flex-col gap-0.5">
          <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
            Margin Copilot
          </p>
          <h1 className="text-2xl font-semibold tracking-tight text-foreground">
            Today&apos;s Close
          </h1>
        </header>

        <section
          aria-label="Today's reconciliation summary"
          className="rounded-lg border border-border bg-card p-4 sm:p-5"
        >
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div>
              <p className="text-xs text-muted-foreground">
                Margin on {formatUsd(TODAY_GROSS_SALES_USD)} in gross sales,
                commissions and refunds already netted out
              </p>
              <p className="text-3xl font-semibold tabular-nums tracking-tight text-foreground">
                {formatUsd(TODAY_MARGIN_USD)}
              </p>
            </div>
            <BadgeDisplay badges={TODAY_BADGES} className="pt-0.5" />
          </div>
          <div className="mt-2 border-t border-border/60 pt-2">
            <ProvenanceTag refs={TODAY_SOURCE_REFS} />
          </div>
        </section>

        <ChatPanel
          resolveAnswer={resolveAnswerWithCostTracking}
          className="max-w-none"
        />
      </div>

      <CostPanel interactions={interactions} />
    </div>
  )
}
