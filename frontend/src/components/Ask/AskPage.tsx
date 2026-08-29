import { useCallback } from 'react'
import { useLocation } from 'react-router-dom'

import type { ChatMessage } from '@/components/Chat/ChatPanel'
import ChatPanel, {
  type AnswerCacheInfo,
  type AssistantChatMessage,
  type PendingClarification,
  type PreviousExchange,
} from '@/components/Chat/ChatPanel'
import type { AnswerVisualization } from '@/components/Charts/answerVisualization'
import type { AskPageNavigationState } from '@/components/Charts/chartFollowUpQuestion'
import type { SourceRowRef } from '@/components/Provenance/ProvenanceTag'
import { postJson } from '@/lib/api'
import { useShellOutletContext } from '@/components/Shell/AppShell'

// ---------------------------------------------------------------------------
// Live wiring to POST /api/ask (httpapi.HandleAsk) — the real two-step
// architecture in CLAUDE.md: every question first runs the Claude Sonnet 5
// ambiguity gate (moved off Haiku 4.5 on 2026-08-29, see
// internal/llmclient/cost.go); only a question that clears it goes on to
// the Claude Sonnet 5 explanation step. The backend returns one
// CostInteraction per model call that actually ran, so the cost panel
// reflects this session's real measured spend, never a placeholder figure.
//
// The request goes through `postJson` from `lib/api` — the same helper every
// other page uses — rather than a page-local `fetch`, so this page honors
// `VITE_API_BASE_URL` like the rest of the app instead of always hitting
// localhost.
// ---------------------------------------------------------------------------

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
   * 0-3 deterministically generated next questions
   * (`internal/httpapi/suggestions.go`), present only when `status` is
   * "answered". Passed straight through to `AnswerChatMessage.followUps` —
   * this page never invents, reorders, or filters them further.
   */
  suggested_followups?: string[]
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
  /**
   * Spec 008 FR-003 ("show your work"): the exact MCP tool name(s) and raw
   * JSON result(s) already computed for this answer
   * (`httpapi.AskResponse.tool_calls`), present only when `status` is
   * "answered". Passed straight through to `AnswerChatMessage.toolCalls` —
   * this page never re-fetches or reformats the underlying data.
   */
  tool_calls?: { name: string; result_json: unknown }[]
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
  // Spec 008 FR-001: a chart click on `/close` or `/promotions` navigates
  // here with the built follow-up question as router state (no shared chat
  // context exists across those separate routes) — read once per navigation,
  // never persisted, so a plain visit to `/ask` never carries a stale
  // question from browser history.
  const location = useLocation()
  const autoSubmitQuestion = (location.state as AskPageNavigationState | null)
    ?.autoSubmitQuestion

  const resolveAnswer = useCallback(
    async (
      question: string,
      _history: ChatMessage[],
      pendingClarification?: PendingClarification,
      previousExchange?: PreviousExchange,
    ): Promise<AssistantChatMessage> => {
      // Both contexts travel as structured fields, not merged into
      // `question`: the backend composes the resolved question itself
      // (ambiguity.ComposeFollowUp / ambiguity.ComposeAnswerFollowUp) so
      // question_interaction.question_text stays exactly what the owner
      // typed. ChatPanel derives at most one of these per submission (see
      // derivePendingClarification/derivePreviousExchange) — never both.
      const data = await postJson<AskApiResponse>('/api/ask', {
        question,
        pending_clarification: pendingClarification
          ? {
              original_question: pendingClarification.originalQuestion,
              clarifying_question: pendingClarification.clarifyingQuestion,
            }
          : undefined,
        previous_exchange: previousExchange
          ? {
              question: previousExchange.question,
              answer_text: previousExchange.answerText,
            }
          : undefined,
      })

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
        followUps: data.suggested_followups,
        toolCalls: data.tool_calls,
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
      autoSubmitQuestion={autoSubmitQuestion}
      className="max-w-none"
    />
  )
}
