import { useCallback } from 'react'

import ChatPanel, {
  mockResolveAnswer,
  type AssistantChatMessage,
} from '@/components/Chat/ChatPanel'
import type { CostInteraction } from '@/components/CostPanel/CostPanel'
import { useShellOutletContext } from '@/components/Shell/AppShell'

// ---------------------------------------------------------------------------
// Cost instrumentation (FR-008/FR-009) — mirrors the two-step architecture in
// CLAUDE.md: every question first runs the cheap Claude Haiku 4.5 ambiguity
// gate; only a question that clears the gate (kind === 'answer') goes on to
// the Claude Sonnet 5 explanation step. A clarification or refusal
// short-circuits after the gate alone. `mockResolveAnswer`'s returned `kind`
// tells us which path fired — no need to duplicate its classification logic
// here.
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

/**
 * `/ask` — "Ask about your margin", per redesign-spec.md §1. Hosts the
 * existing `ChatPanel` unchanged, full width.
 *
 * Unlike the old single-page `MarginCopilotApp` (which owned its own
 * `CostPanel` state and pre-seeded it with the cost of its already-visible
 * demo conversation), the shell now mounts one `CostPanel` at the router
 * root so the running total survives navigation between routes. This page
 * only reports the cost of questions actually asked *in this visit* through
 * `logInteractions` — it deliberately does not replay the seed
 * conversation's cost on mount, since this page (unlike the old app root)
 * can mount and unmount many times as the owner navigates away and back,
 * and re-seeding on every remount would silently inflate the session total.
 */
export default function AskPage() {
  const { logInteractions } = useShellOutletContext()

  const resolveAnswerWithCostTracking = useCallback(
    async (question: string): Promise<AssistantChatMessage> => {
      const message = await mockResolveAnswer(question)
      logInteractions(
        message.kind === 'answer'
          ? [HAIKU_GATE_CALL, SONNET_EXPLAIN_CALL]
          : [HAIKU_GATE_CALL],
      )
      return message
    },
    [logInteractions],
  )

  return (
    <ChatPanel
      resolveAnswer={resolveAnswerWithCostTracking}
      className="max-w-none"
    />
  )
}
