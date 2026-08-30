import { useCallback } from 'react'
import { useLocation } from 'react-router-dom'

import type { ChatMessage } from '@/components/Chat/ChatPanel'
import ChatPanel, {
  type AnswerCacheInfo,
  type AssistantChatMessage,
  type InlineAdvice,
  type PendingClarification,
  type PreviousExchange,
} from '@/components/Chat/ChatPanel'
import type { AnswerVisualization } from '@/components/Charts/answerVisualization'
import type {
  BusinessInsightAdvice,
  BusinessInsightTeaser,
  ResolveBusinessInsight,
} from '@/components/Chat/BusinessInsightChip'
import type { AskPageNavigationState } from '@/components/Charts/chartFollowUpQuestion'
import type { SourceRowRef } from '@/components/Provenance/ProvenanceTag'
import { postJson } from '@/lib/api'
import { createUniqueId } from '@/lib/id'

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
  /**
   * Spec 008 FR-004 ("Compare to last period"): the real [start, end] this
   * answer was grounded in (`httpapi.AskResponse.resolved_period`), present
   * only for a period-totals/daily-summary question. Passed straight
   * through to `AnswerChatMessage.resolvedPeriod` — this page never
   * re-derives it from the original question text.
   */
  resolved_period?: { start: string; end: string }
  /**
   * Spec 009 (business-insight advisor): the zero-cost teaser derived
   * deterministically in Go (`httpapi.deriveBusinessInsightTeaser`),
   * present only when the answer's computed data matched one of the five
   * documented insight patterns. Passed straight through to
   * `AnswerChatMessage.businessInsight` — the full advice is a separate,
   * billed call made only if the owner taps (see resolveBusinessInsight
   * below), never fetched here.
   */
  business_insight?: BusinessInsightTeaser
  /**
   * Spec 011 (inline grounded advice): present only when the owner's
   * question itself asked for a suggestion (the backend gate's typed
   * signal), the answer succeeded with real tool results to ground it,
   * and the one bounded advisor call succeeded. Its cost already appears
   * as its own `interactions` entry — this block is the content plus the
   * standing disclaimer, passed straight through to
   * `AnswerChatMessage.advice`.
   */
  advice?: InlineAdvice
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

/**
 * A prefix is kept purely for readability in dev tools (`assistant-…`) — the
 * uniqueness guarantee comes entirely from `createUniqueId()`. A previous
 * version of this function used a module-level incrementing counter, which
 * reset to 0 on every reload and made ids collide across reloads (see
 * `createUniqueId`'s doc comment for the incident this caused).
 */
function nextMessageId(prefix: string): string {
  return `${prefix}-${createUniqueId()}`
}

/**
 * `/ask` — "Ask about your margin", per redesign-spec.md §1. Hosts the
 * existing `ChatPanel` unchanged, full width, wired to the real backend.
 *
 * Unlike the old single-page `MarginCopilotApp` (which owned its own
 * `CostPanel` state and pre-seeded it with the cost of its already-visible
 * demo conversation), the shell mounts one `CostPanel` at the router root so
 * the running total survives navigation between routes.
 *
 * This page no longer reports cost to the shell at all. Each response's
 * measured `interactions` ride back on the assistant message, and `ChatPanel`
 * writes them to the durable, deduplicated spend ledger in the same commit
 * that persists the message. That change is what makes remounting safe by
 * construction rather than by care taken here: the ledger is keyed by message
 * id, so navigating away and back — or a component mounting twice — can no
 * more double-count spend than it can duplicate an answer.
 */
export default function AskPage() {
  // Spec 008 FR-001: a chart click on `/close` or `/promotions` navigates
  // here with the built follow-up question as router state (no shared chat
  // context exists across those separate routes). This is never persisted
  // by this page — it's read straight off `location.state` on each render —
  // so a fresh/PUSH navigation to `/ask` (typing the URL, a sidebar link)
  // never carries a stale question. React Router *does* restore
  // `location.state` on a POP navigation (Back/Forward), so a prefilled
  // question can legitimately reappear after Back — that's Back correctly
  // restoring what was on screen, not a leak of stale state.
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

      // Attached to the message rather than reported to the shell as a side
      // effect. `ChatPanel` moves it into the durable spend ledger in the
      // same commit that persists the message (see CostAttributedMessage),
      // so the running total survives a reload alongside the answers that
      // earned it — and, crucially, an answer that completes after this page
      // has unmounted records its cost too, instead of being billed
      // invisibly.
      const interactions = data.interactions

      const askedAt = new Date().toISOString()

      if (data.status === 'clarification_needed') {
        return {
          id: nextMessageId('assistant'),
          role: 'assistant',
          kind: 'clarification',
          text: data.clarifying_question ?? 'Could you clarify that question?',
          interactions,
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
          interactions,
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
        interactions,
        provenance: (data.provenance_refs ?? []).map(parseProvenanceRef),
        visualization: data.visualization,
        followUps: data.suggested_followups,
        toolCalls: data.tool_calls,
        resolvedPeriod: data.resolved_period,
        businessInsight: data.business_insight,
        advice: data.advice,
        cache: data.cache,
        askedAt,
      }
    },
    [],
  )

  // Spec 009: the on-demand advice call behind a tapped business-insight
  // teaser — the only model-backed request this app makes besides
  // /api/ask. The chip posts back the answer's own tool_calls (the exact
  // data the teaser was derived from; the backend re-derives and refuses
  // a mismatch), and the returned interaction is logged into the same
  // shared cost panel every /api/ask call already feeds, so an advice
  // call is never invisible spend.
  const resolveBusinessInsight = useCallback<ResolveBusinessInsight>(
    async (kind, toolCalls) => {
      return postJson<BusinessInsightAdvice>('/api/business-insight', {
        kind,
        tool_calls: toolCalls,
      })
    },
    [],
  )

  return (
    <ChatPanel
      resolveAnswer={resolveAnswer}
      resolveBusinessInsight={resolveBusinessInsight}
      // Deliberately empty rather than ChatPanel's demo seed. That seed is a
      // fabricated thread with invented figures ("$612.40", an Uber Eats
      // campaign that isn't in the dataset); rendering it on the LIVE surface
      // put unsourced numbers in front of the owner styled exactly like real,
      // provenance-backed answers. Same class of defect as this page calling a
      // mock instead of /api/ask, and ruled out by the same principle.
      initialMessages={[]}
      // The live surface remembers the thread across reloads and keeps a
      // short history of previous ones (localStorage — see lib/chatStorage).
      persistConversation
      // ChatPanel only prefills the composer from this — it no longer
      // submits on the owner's behalf (reported live: let them review/edit
      // before sending). The navigation-state key itself stays named
      // `autoSubmitQuestion` (chartFollowUpQuestion.ts's
      // AskPageNavigationState, and ClosePage.tsx/PromotionsPage.tsx's own
      // navigate() calls) — only this one prop name changed to match what
      // ChatPanel actually does with it now.
      prefillQuestion={autoSubmitQuestion}
      className="max-w-none"
    />
  )
}
