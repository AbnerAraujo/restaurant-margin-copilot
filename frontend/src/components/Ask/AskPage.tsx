import { useCallback } from 'react'

import ChatPanel, { type AssistantChatMessage } from '@/components/Chat/ChatPanel'
import type { SourceRowRef } from '@/components/Provenance/ProvenanceTag'
import { useShellOutletContext } from '@/components/Shell/AppShell'

// ---------------------------------------------------------------------------
// Live wiring to POST /api/ask (httpapi.HandleAsk) — the real two-step
// architecture in CLAUDE.md: every question first runs the Claude Haiku 4.5
// ambiguity gate; only a question that clears it goes on to the Claude
// Sonnet 5 explanation step. The backend returns one CostInteraction per
// model call that actually ran, so the cost panel reflects this session's
// real measured spend, never a placeholder figure.
// ---------------------------------------------------------------------------

const API_BASE = 'http://localhost:8080'

interface AskApiResponse {
  status: 'answered' | 'clarification_needed' | 'refused'
  answer_text?: string
  provenance_refs?: string[]
  clarifying_question?: string
  assumption_stated?: string
  refusal_reason?: string
  interactions?: {
    model_used: string
    input_tokens: number
    output_tokens: number
    estimated_cost_usd: number
    latency_ms: number
  }[]
}

/**
 * The live backend's provenance_refs are flat "path/to/file.csv:12" strings
 * (httpapi.AskResponse — no period info travels over that contract), so
 * these refs render with no date range rather than a fabricated one. See
 * ProvenanceTag's period_start/period_end going optional for this exact case.
 */
function parseProvenanceRef(ref: string): SourceRowRef {
  const separatorIndex = ref.lastIndexOf(':')
  const rowNumber = Number(ref.slice(separatorIndex + 1))
  if (separatorIndex === -1 || Number.isNaN(rowNumber)) {
    return { source_file: ref, row_start: 0, row_end: 0 }
  }
  return {
    source_file: ref.slice(0, separatorIndex),
    row_start: rowNumber,
    row_end: rowNumber,
  }
}

let messageSequence = 0
function nextMessageId(prefix: string): string {
  messageSequence += 1
  return `${prefix}-${messageSequence}`
}

/**
 * `/ask` — "Ask about your margin", per redesign-spec.md §1. Hosts the
 * existing `ChatPanel` unchanged, full width, wired to the real backend.
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

  const resolveAnswer = useCallback(
    async (question: string): Promise<AssistantChatMessage> => {
      const response = await fetch(`${API_BASE}/api/ask`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ question }),
      })

      if (!response.ok) {
        const detail = await response.text()
        throw new Error(`/api/ask returned ${response.status}: ${detail}`)
      }

      const data = (await response.json()) as AskApiResponse
      if (data.interactions && data.interactions.length > 0) {
        logInteractions(data.interactions)
      }

      const askedAt = new Date().toISOString()

      if (data.status === 'clarification_needed') {
        return {
          id: nextMessageId('assistant'),
          role: 'assistant',
          kind: 'clarification',
          text: data.clarifying_question ?? 'Could you clarify that question?',
          askedAt,
        }
      }

      if (data.status === 'refused') {
        return {
          id: nextMessageId('assistant'),
          role: 'assistant',
          kind: 'refusal',
          text: data.refusal_reason ?? "I can't answer that from the data on file.",
          missing: [],
          askedAt,
        }
      }

      return {
        id: nextMessageId('assistant'),
        role: 'assistant',
        kind: 'answer',
        text: data.answer_text ?? '',
        provenance: (data.provenance_refs ?? []).map(parseProvenanceRef),
        askedAt,
      }
    },
    [logInteractions],
  )

  return <ChatPanel resolveAnswer={resolveAnswer} className="max-w-none" />
}
