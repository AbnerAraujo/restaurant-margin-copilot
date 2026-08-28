import { useCallback } from 'react'

import type { ChatMessage } from '@/components/Chat/ChatPanel'
import ChatPanel, {
  type AnswerCacheInfo,
  type AssistantChatMessage,
  type PendingClarification,
} from '@/components/Chat/ChatPanel'
import type { AnswerVisualization } from '@/components/Charts/answerVisualization'
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
  clarifying_options?: string[]
  assumption_stated?: string
  refusal_reason?: string
  /**
   * Chart/table shape chosen by the backend from which typed MCP tool ran
   * (`internal/httpapi/visualization.go`). Passed straight through — this
   * page never decides, or overrides, the form.
   */
  visualization?: AnswerVisualization
  /**
   * Present only when the backend served this answer from its cache without
   * any model call. When it is set, `interactions` is empty because NOTHING
   * RAN — not because measurement failed — so the running cost total is
   * correctly left untouched.
   */
  cache?: AnswerCacheInfo
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
    async (
      question: string,
      _history: ChatMessage[],
      pendingClarification?: PendingClarification,
    ): Promise<AssistantChatMessage> => {
      const response = await fetch(`${API_BASE}/api/ask`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        // The clarification context travels as structured fields, not merged
        // into `question`: the backend composes the resolved question itself
        // (ambiguity.ComposeFollowUp) so question_interaction.question_text
        // stays exactly what the owner typed.
        body: JSON.stringify({
          question,
          pending_clarification: pendingClarification
            ? {
                original_question: pendingClarification.originalQuestion,
                clarifying_question: pendingClarification.clarifyingQuestion,
              }
            : undefined,
        }),
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
          // Rendered as one-tap chips. Picking one posts it back through the
          // gate with the clarification context attached, exactly as typing
          // it would — an option is a shortcut, never an accepted answer.
          options: data.clarifying_options,
          cache: data.cache,
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
          cache: data.cache,
          askedAt,
        }
      }

      return {
        id: nextMessageId('assistant'),
        role: 'assistant',
        kind: 'answer',
        text: data.answer_text ?? '',
        provenance: (data.provenance_refs ?? []).map(parseProvenanceRef),
        visualization: data.visualization,
        cache: data.cache,
        askedAt,
      }
    },
    [logInteractions],
  )

  return (
    <ChatPanel
      resolveAnswer={resolveAnswer}
      // Deliberately empty rather than ChatPanel's demo seed. That seed is a
      // fabricated thread with invented figures ("$612.40", an Uber Eats
      // campaign that isn't in the fixtures); rendering it on the LIVE surface
      // put unsourced numbers in front of the owner styled exactly like real,
      // provenance-backed answers. Same class of defect as this page calling a
      // mock instead of /api/ask, and ruled out by the same principle.
      initialMessages={[]}
      // The live surface remembers the thread across reloads and keeps a
      // short history of previous ones (localStorage — see lib/chatStorage).
      persistConversation
      className="max-w-none"
    />
  )
}
