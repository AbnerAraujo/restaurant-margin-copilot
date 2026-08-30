import * as React from 'react'
import {
  ArrowDown,
  Bookmark,
  Braces,
  CalendarRange,
  FileText,
  History,
  CircleHelp,
  Info,
  Loader2,
  PlugZap,
  RotateCw,
  Lightbulb,
  Send,
  Compass,
  SquarePen,
  Unplug,
  User,
  Wand2,
  Wrench,
  X,
  Zap,
} from 'lucide-react'

import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Chip } from '@/components/ui/page'
import Logo from '@/components/Logo/Logo'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Textarea } from '@/components/ui/textarea'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'
import ProvenanceTag, {
  type SourceRowRef,
} from '@/components/Provenance/ProvenanceTag'
import type { CostInteraction } from '@/components/CostPanel/CostPanel'
import {
  activeThread,
  addSavedPrompt,
  clearRequestInFlight,
  commitThreadMessages,
  isDocumentUnloading,
  isRequestInFlight,
  loadSavedPrompts,
  materialiseThreadStore,
  markRequestInFlight,
  mergeThreadStores,
  openThread,
  reconcileInterruptedMessages,
  recordSpend,
  removeSavedPrompt,
  replaceMessage,
  startNewThread,
  subscribeToThreadStore,
  type SavedPrompt,
  type ThreadStore,
} from '@/lib/chatStorage'
import AnswerText from '@/components/Chat/AnswerText'
import BusinessInsightChip, {
  type BusinessInsightTeaser,
  type ResolveBusinessInsight,
} from '@/components/Chat/BusinessInsightChip'
import { buildCompareToLastPeriodQuestion } from '@/components/Chat/comparePeriod'
import SuggestionChips from '@/components/Chat/SuggestionChips'
import {
  buildCapabilitySummary,
  EXAMPLE_QUESTIONS,
  type ExampleQuestion,
} from '@/components/Chat/exampleQuestions'
import AnswerVisualizationView from '@/components/Charts/AnswerVisualizationView'
import type { AnswerVisualization } from '@/components/Charts/answerVisualization'
import { useDataCoverage } from '@/lib/useDataCoverage'
import QuestionComposer from '@/components/Chat/QuestionComposer'

// ---------------------------------------------------------------------------
// Types — shaped to line up with QuestionInteraction in data-model.md and the
// answer/refusal contract in mcp-tools.md, so a real `resolveAnswer` can map
// a backend response onto these fields with no reshaping.
// ---------------------------------------------------------------------------

/**
 * Set when a response came from the backend's answer cache instead of a live
 * model call (`internal/answercache`). Carried on every assistant kind that
 * can be cached, because a cached refusal is exactly as much a cache hit as a
 * cached answer.
 */
export interface AnswerCacheInfo {
  hit: boolean
  cached_at?: string
  /** Model spend this request avoided — a saving, never added to the total. */
  cost_avoided_usd: number
  /** The backend's own statement of what this cache does and does not match. */
  note?: string
}

/**
 * Identifies the clarifying question a reply is answering. Derived
 * deterministically from the visible conversation — the last assistant
 * message and the user question immediately before it — never guessed and
 * never composed into prose here: the backend owns that composition so the
 * instrumentation log records what was actually typed.
 */
export interface PendingClarification {
  originalQuestion: string
  clarifyingQuestion: string
}

/**
 * The immediately preceding question and the real ANSWER it got — what a
 * follow-up to an answer needs in order to be classifiable at all ("and the
 * day before?", "why?", "what about the week after that?"). The answer-side
 * counterpart to {@link PendingClarification}: that type exists because a
 * bare reply to a clarifying question is meaningless without the question it
 * was answering, and this closes the identical gap for a follow-up to a real
 * answer, which previously had no equivalent mechanism at all — every such
 * question was classified against the gate in total isolation and would
 * almost certainly misfire.
 *
 * Deliberately exactly one hop, mirroring PendingClarification's own
 * discipline (see its doc comment) and the backend's
 * `ambiguity.PreviousExchange`: never a growing transcript, just the
 * immediately preceding question and the answer text the user actually
 * read.
 */
export interface PreviousExchange {
  question: string
  answerText: string
}

export interface UserChatMessage {
  id: string
  role: 'user'
  text: string
  askedAt: string
}

/**
 * The model calls that produced an assistant message, as measured by the
 * backend (`httpapi.AskResponse.interactions`).
 *
 * Carried on the message purely as transport: `ChatPanel` moves it into the
 * durable spend ledger (`chatStorage.recordSpend`) in the same commit that
 * persists the message, then strips it, so cost lives in exactly one place
 * and can never be double-counted by being both persisted and re-logged.
 *
 * Attaching it to the message at all — rather than reporting it up to the
 * shell as a side effect the way this used to work — is what makes the
 * running total durable: the same commit that survives an unmount also
 * records the spend, so an answer the owner was already billed for can
 * never arrive with its cost silently dropped.
 */
export interface CostAttributedMessage {
  interactions?: CostInteraction[]
}

export interface AnswerChatMessage extends CostAttributedMessage {
  id: string
  role: 'assistant'
  kind: 'answer'
  text: string
  /**
   * Points into the `DailyReconciliation` / `PromotionRoiRecord` rows this
   * answer was narrated from (data-model.md `QuestionInteraction.provenance_refs`) —
   * the same FR-005 shape `ProvenanceTag` renders everywhere else in the app,
   * so a citation looks and behaves identically whether it's attached to a
   * chat answer or a reconciliation summary figure.
   */
  provenance: SourceRowRef[]
  /**
   * Optional structured rendering of the very same deterministic tool result
   * this answer was narrated from — a grid, a bar chart, or a composition
   * donut. The FORM is chosen by the backend
   * (`internal/httpapi/visualization.go`) from which typed MCP tool ran, not
   * by this component and not by a second model call.
   */
  visualization?: AnswerVisualization
  cache?: AnswerCacheInfo
  /**
   * 0-3 natural-language next questions, generated deterministically by the
   * backend (`internal/httpapi/suggestions.go`) from the real tool call that
   * grounded THIS answer — never a second model call, never the model
   * describing its own capabilities. Rendered as one-tap chips so a
   * successful answer hands the reader somewhere to go next instead of
   * ending in a blank composer, the same follow-up-chip pattern
   * Perplexity/ChatGPT use. Omitted (or empty) is a normal, honest outcome
   * for a tool result with nothing sensible to suggest from.
   */
  followUps?: string[]
  /**
   * Spec 008 FR-003 ("show your work"): the exact MCP tool name(s) invoked
   * and the raw JSON result(s) already returned in this same response
   * (`AskResponse.tool_calls`) — pure transparency into data this answer
   * already carried internally, never a new tool call or a re-computation.
   * Omitted (or empty) whenever no tool ran.
   */
  toolCalls?: ChatToolCall[]
  /**
   * Spec 008 FR-004 ("Compare to last period"): the real [start, end] this
   * answer was grounded in (`AskResponse.resolved_period`), present only
   * for a period-totals/daily-summary question. Used to derive the prior
   * period client-side and offer a one-tap comparison — never re-parsed
   * from the original question text, per spec.md's own stated edge case.
   */
  resolvedPeriod?: ChatResolvedPeriod
  /**
   * Spec 009 (business-insight advisor): the zero-cost teaser derived
   * deterministically in Go (`AskResponse.business_insight`) — a kind tag
   * and a short title, nothing more. Present only when the answer's own
   * computed data matched one of the five documented insight patterns;
   * absent for most answers. The full advice text does NOT exist yet: it
   * is a separate, billed model call made only if the owner taps the chip
   * (see {@link BusinessInsightChip}), never auto-fetched.
   */
  businessInsight?: BusinessInsightTeaser
  askedAt: string
}

/** Mirrors `httpapi.ResolvedPeriodView`'s wire shape. */
export interface ChatResolvedPeriod {
  start: string
  end: string
}

/** One real MCP tool invocation, mirroring `httpapi.ToolCallView`'s wire shape. */
export interface ChatToolCall {
  name: string
  result_json: unknown
}

export interface ClarificationChatMessage extends CostAttributedMessage {
  id: string
  role: 'assistant'
  kind: 'clarification'
  text: string
  /** Quick-reply shortcuts so the owner can resolve the ambiguity in one tap. */
  options?: string[]
  cache?: AnswerCacheInfo
  askedAt: string
}

export interface RefusalChatMessage extends CostAttributedMessage {
  id: string
  role: 'assistant'
  kind: 'refusal'
  text: string
  /** What's missing that prevents a real answer — never a guessed figure. */
  missing: string[]
  cache?: AnswerCacheInfo
  askedAt: string
}

/**
 * A question that has been asked and persisted but has no verdict yet.
 *
 * This exists as a real, PERSISTED message rather than a transient `isPending`
 * boolean because of the defect it fixes: a question was written to storage
 * the moment it was asked, but the fact that an answer was on its way lived
 * only in React state. Reloading or navigating away therefore left a question
 * with no answer, no error, and no retry — silent, permanent limbo — while
 * the backend went right on completing the request and billing for it.
 *
 * Writing the pending record BEFORE the request starts means the interrupted
 * case is always recoverable: on the next mount, `chatStorage`'s
 * `reconcileInterruptedMessages` turns any pending message this document is
 * not actually waiting on into an honest, retryable {@link ErrorChatMessage}.
 */
export interface PendingChatMessage {
  id: string
  role: 'assistant'
  kind: 'pending'
  /** The question in flight, so an interruption can be retried verbatim. */
  question: string
  askedAt: string
}

/**
 * A request that never reached a verdict — the backend was unreachable, or
 * returned a non-2xx. Deliberately its own kind rather than reusing
 * {@link RefusalChatMessage}: a refusal is a product decision the system
 * stands behind ("I have the data path and I am declining to guess"), while
 * this is a defect ("I never got an answer at all"). Collapsing the two
 * would let an outage read as principled caution, which is exactly the kind
 * of flattering misreport this product's honesty discipline rules out.
 */
export interface ErrorChatMessage {
  id: string
  role: 'assistant'
  kind: 'error'
  text: string
  /**
   * Why there is no answer. The same distinction one level down from the
   * refusal/error split above, and for the same reason — the two failures
   * owe the reader different things:
   *
   * - `transport` (the default): the request itself failed. Nothing ran, so
   *   nothing was charged, and retrying is free.
   * - `interrupted`: the request was very likely COMPLETED by the backend,
   *   and therefore very likely billed, but the page that was waiting for it
   *   went away. Saying "I couldn't reach your data" here would be a lie in
   *   the flattering direction — it would hide spend the owner has already
   *   incurred — so this case says so plainly instead.
   */
  cause?: 'transport' | 'interrupted'
  /** The failed question, so the retry affordance can resend it verbatim. */
  question: string
  askedAt: string
}

export type AssistantChatMessage =
  | AnswerChatMessage
  | ClarificationChatMessage
  | RefusalChatMessage
  | ErrorChatMessage
  | PendingChatMessage

export type ChatMessage = UserChatMessage | AssistantChatMessage

export interface ChatPanelProps {
  /** Seed conversation shown on mount. Defaults to a realistic demo thread. */
  initialMessages?: ChatMessage[]
  /**
   * Resolves a new assistant message for a submitted question. Defaults to
   * an in-memory mock that demonstrates the answer / clarification / refusal
   * paths from spec.md User Stories 2–3. Pass a real implementation (e.g.
   * one that calls the backend's `/api/ask` endpoint and maps the response
   * onto {@link AssistantChatMessage}) to wire this up to the live system.
   */
  resolveAnswer?: (
    question: string,
    history: ChatMessage[],
    pendingClarification?: PendingClarification,
    previousExchange?: PreviousExchange,
  ) => Promise<AssistantChatMessage>
  /**
   * Fetches the full advice for a tapped business-insight teaser (spec
   * 009) — the same dependency-injection shape as {@link resolveAnswer}:
   * the page-level wiring owns the `POST /api/business-insight` call and
   * the cost-panel logging (see AskPage). When absent, teasers are not
   * rendered at all — a chip whose tap could never resolve would be a
   * dead affordance, and only the live surface (which passes this) ever
   * receives real teasers anyway.
   */
  resolveBusinessInsight?: ResolveBusinessInsight
  /**
   * Starter questions offered when the conversation is empty (Nielsen #6,
   * recognition rather than recall — an empty box gives a time-poor owner no
   * clue what this thing can actually answer). Only rendered when there are
   * no messages at all, so it never competes with a live conversation.
   */
  suggestions?: ExampleQuestion[]
  /**
   * Opt into localStorage-backed thread history and saved prompts. Left off
   * by default so the component stays a pure, self-contained view for tests
   * and for any embedding that shouldn't touch a shared browser key.
   */
  persistConversation?: boolean
  /**
   * Prefills the composer with this question exactly once, as soon as the
   * panel mounts — spec 008 FR-001's chart click-to-ask: `/close` and
   * `/promotions` are separate routes from `/ask` (no shared chat context
   * exists to call across pages), so a chart click navigates here with the
   * built question as router state (see AskPage) instead. Deliberately
   * does NOT submit on the owner's behalf — it only populates the draft, so
   * they can review or edit it before choosing to send. Re-fires only if
   * this prop changes to a NEW, different question while already mounted —
   * never twice for the same one (e.g. a duplicate navigation with
   * identical state), so it never stomps on text the owner has since typed.
   */
  prefillQuestion?: string
  className?: string
}

// ---------------------------------------------------------------------------
// Mocked conversation + resolver — realistic dataset-shaped data standing in
// for the backend per this task's brief; no live API exists yet.
// ---------------------------------------------------------------------------

let messageSequence = 0
function nextMessageId(prefix: string): string {
  messageSequence += 1
  return `${prefix}-${messageSequence}`
}

/**
 * Reads the cost transport field off whichever assistant kind carries it.
 * Only `AnswerChatMessage`/`ClarificationChatMessage`/`RefusalChatMessage`
 * declare it, so a narrowing-free accessor keeps the call sites from
 * re-discriminating the union just to find out whether a call was billed.
 */
function readInteractions(
  message: AssistantChatMessage,
): CostInteraction[] | undefined {
  return (message as { interactions?: CostInteraction[] }).interactions
}

/**
 * Strips the cost transport field before the message is persisted.
 *
 * Cost has exactly one home — the durable spend ledger — and persisting a
 * second copy on the message would be an invitation to double-count it
 * later. See {@link CostAttributedMessage}.
 */
function withoutInteractions(message: AssistantChatMessage): AssistantChatMessage {
  if (!readInteractions(message)) return message
  const copy: Record<string, unknown> = { ...message }
  delete copy.interactions
  return copy as unknown as AssistantChatMessage
}

/**
 * How close to the bottom still counts as "following the conversation".
 * A few pixels of slack absorbs sub-pixel rounding and the momentum tail of
 * a smooth scroll, which would otherwise unpin the view the instant an
 * auto-scroll finished.
 */
const BOTTOM_STICK_THRESHOLD_PX = 48

/**
 * Reads the pending clarification off the tail of the conversation: the last
 * message is a clarification, and the question it was asked about is the most
 * recent user message before it.
 *
 * Returns undefined when the last message is anything else, so a normal
 * question is never accidentally tagged as a reply to a clarification the
 * user has already moved past.
 */
export function derivePendingClarification(
  history: ChatMessage[],
): PendingClarification | undefined {
  const last = history[history.length - 1]
  if (!last || last.role !== 'assistant' || last.kind !== 'clarification') {
    return undefined
  }
  for (let i = history.length - 2; i >= 0; i--) {
    const candidate = history[i]
    if (candidate.role === 'user') {
      return {
        originalQuestion: candidate.text,
        clarifyingQuestion: last.text,
      }
    }
  }
  return undefined
}

/**
 * Reads the previous-answer context off the tail of the conversation: the
 * last message is a real ANSWER (never a clarification, refusal, or error),
 * and the question it answered is the most recent user message before it.
 *
 * Mutually exclusive with {@link derivePendingClarification} by
 * construction, not by any extra heuristic here: that one only returns a
 * value when the last message is a clarification, this one only when it's
 * an answer, so a submitted question can never carry both — and never needs
 * client-side guessing about which one applies. Gate/explain classification
 * server-side is what decides whether a follow-up is actually relevant to
 * the previous answer at all (see ambiguity.ComposeAnswerFollowUp) — this
 * function's only job is attaching the ONE hop of context that makes that
 * classification possible, exactly like derivePendingClarification already
 * does for the clarification case.
 *
 * Returns undefined when the last message is anything else (a clarification,
 * a refusal, an error, or an empty conversation), so a normal question never
 * accidentally carries a previous-answer context it doesn't need.
 */
export function derivePreviousExchange(
  history: ChatMessage[],
): PreviousExchange | undefined {
  const last = history[history.length - 1]
  if (!last || last.role !== 'assistant' || last.kind !== 'answer') {
    return undefined
  }
  for (let i = history.length - 2; i >= 0; i--) {
    const candidate = history[i]
    if (candidate.role === 'user') {
      return {
        question: candidate.text,
        answerText: last.text,
      }
    }
  }
  return undefined
}

/** Starter questions for an empty conversation. See exampleQuestions.ts. */
const DEFAULT_SUGGESTIONS = EXAMPLE_QUESTIONS

const SEED_MESSAGES: ChatMessage[] = [
  {
    id: 'seed-1',
    role: 'user',
    text: 'How did we do today?',
    askedAt: '2026-08-27T09:14:00-03:00',
  },
  {
    id: 'seed-2',
    role: 'assistant',
    kind: 'answer',
    text: "Today's reconciled margin was $612.40 on $2,180.00 in gross sales, commissions and refunds already netted out. That's roughly in line with your trailing 7-day average.",
    provenance: [
      {
        source_file: 'daily_reconciliation.csv',
        row_start: 27,
        row_end: 27,
        period_start: '2026-08-27',
        period_end: '2026-08-27',
      },
      {
        source_file: 'pos_export_2026-08-27.csv',
        row_start: 1,
        row_end: 42,
        period_start: '2026-08-27',
        period_end: '2026-08-27',
      },
    ],
    askedAt: '2026-08-27T09:14:04-03:00',
  },
  {
    id: 'seed-3',
    role: 'user',
    text: 'How was the weekend?',
    askedAt: '2026-08-27T09:15:10-03:00',
  },
  {
    id: 'seed-4',
    role: 'assistant',
    kind: 'clarification',
    text: '"Weekend" is ambiguous for your schedule — should that include Friday night, or just Saturday and Sunday?',
    options: ['Include Friday (Fri–Sun)', 'Saturday–Sunday only'],
    askedAt: '2026-08-27T09:15:12-03:00',
  },
  {
    id: 'seed-5',
    role: 'user',
    text: 'Saturday–Sunday only',
    askedAt: '2026-08-27T09:15:30-03:00',
  },
  {
    id: 'seed-6',
    role: 'assistant',
    kind: 'answer',
    text: 'Sat–Sun margin was $1,340.80, up 4.1% from last weekend, mostly from lower refunds rather than higher sales.',
    provenance: [
      {
        source_file: 'daily_reconciliation.csv',
        row_start: 22,
        row_end: 23,
        period_start: '2026-08-22',
        period_end: '2026-08-23',
      },
    ],
    askedAt: '2026-08-27T09:15:33-03:00',
  },
  {
    id: 'seed-7',
    role: 'user',
    text: 'What was our ROI on the Uber Eats banner campaign last week?',
    askedAt: '2026-08-27T09:16:40-03:00',
  },
  {
    id: 'seed-8',
    role: 'assistant',
    kind: 'refusal',
    text: "I can't compute an ROI for that — there's no Uber Eats promotion export on file for that period, so I won't estimate a figure.",
    missing: [
      'Uber Eats ad-spend export for Aug 18–24',
      'Attributed incremental orders for that campaign',
    ],
    askedAt: '2026-08-27T09:16:42-03:00',
  },
]

const AMBIGUOUS_PATTERN = /\bweekend\b/i
const UNKNOWN_SOURCE_PATTERN = /\b(uber eats|grubhub|sysco)\b/i

/**
 * Stands in for a real `/api/ask` call. Picks a response path from the
 * question text so the three flows in spec.md User Stories 2–3 (grounded
 * answer, clarifying question, refusal) are all reachable interactively —
 * never a model narrating an invented number, matching the deterministic
 * core the mock is standing in for.
 *
 * Exported so a page-level wiring component can reuse this exact routing
 * (rather than re-implementing the same demo patterns) when it needs to
 * observe which path fired — e.g. to log the matching cost interaction(s)
 * per FR-009 without duplicating this logic.
 */
export async function mockResolveAnswer(
  question: string,
): Promise<AssistantChatMessage> {
  await new Promise((resolve) => setTimeout(resolve, 450))

  if (UNKNOWN_SOURCE_PATTERN.test(question)) {
    return {
      id: nextMessageId('assistant'),
      role: 'assistant',
      kind: 'refusal',
      text: "I can't answer that from the data on file — there's no export for that source, so I won't guess.",
      missing: ['A matching platform export for the requested period'],
      askedAt: new Date().toISOString(),
    }
  }

  if (AMBIGUOUS_PATTERN.test(question)) {
    return {
      id: nextMessageId('assistant'),
      role: 'assistant',
      kind: 'clarification',
      text: '"Weekend" could mean Friday–Sunday or just Saturday–Sunday here — which did you mean?',
      options: ['Include Friday (Fri–Sun)', 'Saturday–Sunday only'],
      askedAt: new Date().toISOString(),
    }
  }

  return {
    id: nextMessageId('assistant'),
    role: 'assistant',
    kind: 'answer',
    text: 'Margin for that period was $1,842.60, up 6.4% from the week before, driven mainly by lower refunds rather than higher sales.',
    provenance: [
      {
        source_file: 'daily_reconciliation.csv',
        row_start: 18,
        row_end: 24,
        period_start: '2026-08-18',
        period_end: '2026-08-24',
      },
      {
        source_file: 'pos_export_2026-08-18.csv',
        row_start: 118,
        row_end: 166,
        period_start: '2026-08-18',
        period_end: '2026-08-24',
      },
    ],
    askedAt: new Date().toISOString(),
  }
}

// ---------------------------------------------------------------------------
// Presentation
// ---------------------------------------------------------------------------

function ChatAvatar({
  role,
  tone = 'neutral',
  pending = false,
}: {
  role: 'user' | 'assistant'
  tone?: 'neutral' | 'warning' | 'refusal'
  /**
   * True only for the bubble standing in for a real model call in flight
   * (PendingBubble). The mark loops the same door-swing the launch splash
   * plays once, for exactly as long as this instance stays mounted —
   * finishing the call unmounts the pending bubble and replaces it with a
   * real, fully static answer/clarification/refusal/error bubble, so
   * "static once finished" falls out of the message list swapping
   * components rather than needing its own stop condition here.
   */
  pending?: boolean
}) {
  const toneClasses =
    tone === 'warning'
      ? 'bg-warning/10 text-warning-text'
      : tone === 'refusal'
        ? 'bg-primary/10 text-primary'
        : role === 'user'
          ? 'bg-primary/10 text-primary'
          : 'bg-muted text-muted-foreground'

  return (
    <Avatar size="sm" className="mt-0.5 shrink-0">
      <AvatarFallback className={toneClasses}>
        {role === 'user' ? (
          <User className="size-3.5" />
        ) : (
          <Logo
            variant="icon"
            size={16}
            doorAnimation={pending ? 'loop' : undefined}
          />
        )}
      </AvatarFallback>
    </Avatar>
  )
}

/**
 * Marks an answer that cost nothing because no model call was made. Says
 * "$0 spent" and "saved $X" as two separate facts rather than one netted
 * number: the running cost panel must never treat an avoided cost as spend,
 * and the copy here mirrors that distinction so the two can't be conflated.
 *
 * `cache.note` — the backend's own statement of what this cache hit does and
 * does not match — used to ride on a bare `title=` attribute: an unstyled,
 * browser-default tooltip with no keyboard affordance and no visual cue that
 * anything was even hoverable. It's now the same Info-icon-plus-Tooltip
 * pattern `Stat`'s own info tooltips use, so a curious owner has something to
 * actually spot and Tab to.
 */
function CacheBadge({ cache }: { cache: AnswerCacheInfo }) {
  return (
    <p className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-micro text-muted-foreground">
      <Zap className="size-3 shrink-0" aria-hidden="true" />
      <span className="font-medium text-foreground">Served from cache</span>
      <span>— no model call, $0.000 spent</span>
      {cache.cost_avoided_usd > 0 ? (
        <span className="tabular-nums">
          (saved ${cache.cost_avoided_usd.toFixed(3)})
        </span>
      ) : null}
      {cache.note ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              type="button"
              className="inline-flex shrink-0 rounded-full text-muted-foreground/70 hover:text-foreground focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
              aria-label="Why this was served from cache"
            >
              <Info className="size-3" aria-hidden="true" />
            </button>
          </TooltipTrigger>
          <TooltipContent>{cache.note}</TooltipContent>
        </Tooltip>
      ) : null}
    </p>
  )
}

function UserBubble({ message }: { message: UserChatMessage }) {
  return (
    <li className="flex items-start justify-end gap-2">
      <div className="max-w-[85%] rounded-2xl rounded-tr-sm bg-primary px-4 py-2.5">
        <p className="text-sm leading-relaxed text-primary-foreground">
          {message.text}
        </p>
      </div>
      <ChatAvatar role="user" />
    </li>
  )
}

/**
 * An answered question.
 *
 * Reading order is deliberate and matches the architecture in CLAUDE.md: the
 * chips say which typed MCP tool produced this and what it cost, the
 * deterministic result is drawn next, and the model's narration comes last as
 * the thing that explains an already-visible figure. Previously the prose came
 * first at roughly 150 characters per line and the chart was buried under it,
 * so an answer read as an essay with a picture at the end.
 *
 * The narration text is passed through untouched. Nothing here summarises,
 * shortens, or re-states it, and no figure on screen is computed in this file.
 */
/**
 * Spec 008 FR-003's "show your work" — modeled directly on ProvenanceTag's
 * own toggle-button-plus-panel pattern (a real, expandable disclosure, not
 * an HTML `<details>` element, so its open/close state and focus behavior
 * match every other expandable affordance in this app). Renders the exact
 * tool name(s) and raw JSON already present in `AnswerChatMessage.toolCalls`
 * — no new fetch, no re-computation, pure transparency into data this
 * response already carried.
 */
function ShowYourWorkPanel({ toolCalls }: { toolCalls: ChatToolCall[] }) {
  const [open, setOpen] = React.useState(false)
  const panelId = React.useId()

  if (toolCalls.length === 0) {
    return null
  }

  return (
    <div className="relative">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={panelId}
        onClick={() => setOpen((wasOpen) => !wasOpen)}
        className="inline-flex items-center gap-1 text-xs text-muted-foreground underline decoration-dotted underline-offset-2 hover:text-foreground focus-visible:text-foreground focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:rounded-sm"
      >
        <Braces className="size-3" aria-hidden="true" />
        {open ? 'Hide' : 'Show'} your work
      </button>

      {open ? (
        <div
          id={panelId}
          role="group"
          aria-label="Tool calls behind this answer"
          className="mt-1.5 max-h-64 overflow-y-auto rounded-md border border-border bg-popover p-2.5"
        >
          <div className="mb-1.5 flex items-center justify-between gap-4">
            <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
              Tool call{toolCalls.length > 1 ? 's' : ''}
            </span>
            <button
              type="button"
              onClick={() => setOpen(false)}
              aria-label="Dismiss tool call detail"
              className="rounded-sm text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
            >
              <X className="size-3" aria-hidden="true" />
            </button>
          </div>
          <ul className="space-y-2.5">
            {toolCalls.map((call, index) => (
              <li key={`${call.name}-${index}`} className="text-xs">
                <p className="font-mono font-medium text-popover-foreground">
                  {call.name}
                </p>
                <pre className="mt-1 overflow-x-auto whitespace-pre-wrap break-all rounded bg-muted/60 p-2 font-mono text-[10.5px] leading-relaxed text-muted-foreground">
                  {JSON.stringify(call.result_json, null, 2)}
                </pre>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </div>
  )
}

function AnswerBubble({
  message,
  onSuggestionSelect,
  resolveBusinessInsight,
  onSpend,
}: {
  message: AnswerChatMessage
  onSuggestionSelect: (text: string) => void
  resolveBusinessInsight?: ResolveBusinessInsight
  /** Records what a tapped business-insight call cost, against this answer. */
  onSpend: (messageId: string, interactions: CostInteraction[]) => void
}) {
  // The advice call behind a business-insight chip is the app's only billed
  // request that isn't an /api/ask turn, and it used to report its cost to a
  // separate, purely in-memory channel — so it vanished on reload exactly
  // like the chat total did. Wrapping the resolver here routes it through
  // the same durable, message-attributed ledger every other model call now
  // uses, which is what lets the running total have ONE definition rather
  // than a persistent part and an ephemeral part.
  const resolveInsightAndRecordSpend = React.useMemo<
    ResolveBusinessInsight | undefined
  >(() => {
    if (!resolveBusinessInsight) return undefined
    return async (kind, toolCalls) => {
      const advice = await resolveBusinessInsight(kind, toolCalls)
      onSpend(message.id, [advice.interaction])
      return advice
    }
  }, [message.id, onSpend, resolveBusinessInsight])

  const sourceCount = message.provenance.length
  const followUps = message.followUps ?? []
  const toolCalls = message.toolCalls ?? []
  // The tool-name chip's source of truth is the real tool call(s) that ran
  // for this answer (`AskResponse.tool_calls` / `message.toolCalls`) — NOT
  // `message.visualization?.source_tool`. A visualization is a
  // presentational choice layered on top of a tool result (deriveVisualization
  // in visualization.go can and does decline to draw a chart for plenty of
  // real, tool-grounded answers, e.g. a prose-only day-of-month pattern
  // answer), so it must never be the thing that decides whether the
  // "which typed tool computed this" chip appears. Deduped and order-
  // preserving: today's backend only ever emits one tool call per answer,
  // but nothing here should silently drop a second, differently-named one if
  // that ever changes.
  const toolNames = Array.from(new Set(toolCalls.map((call) => call.name)))
  const resolvedPeriod = message.resolvedPeriod

  return (
    <li className="flex items-start gap-2">
      <ChatAvatar role="assistant" />
      <div className="w-full max-w-[52rem] space-y-3 rounded-xl rounded-tl-sm border border-border bg-card px-4 py-3.5">
        {toolNames.length > 0 || sourceCount > 0 || message.cache ? (
          <div className="flex flex-wrap items-center gap-1.5">
            {/* Both chip kinds below used to carry their explanation on a
                native `title=` attribute — invisible until the browser's
                own, unstyled delay fires, and unreachable by keyboard.
                `Chip` now forwards a ref (ui/page.tsx), so it can be the
                real trigger for the app's own styled Tooltip instead;
                `cursor-help` is the one visual addition, so a chip that has
                more to say reads as such before anyone hovers it. */}
            {toolNames.map((name) => (
              <Tooltip key={name}>
                <TooltipTrigger asChild>
                  <Chip icon={Wrench} tone="brand" tabIndex={0} className="cursor-help">
                    <span className="font-mono">{name}</span>
                  </Chip>
                </TooltipTrigger>
                <TooltipContent>
                  The typed MCP tool that computed this answer&apos;s figures
                </TooltipContent>
              </Tooltip>
            ))}
            {sourceCount > 0 ? (
              <Chip icon={FileText}>
                {sourceCount} source {sourceCount === 1 ? 'row' : 'rows'}
              </Chip>
            ) : null}
            {message.cache ? (
              message.cache.note ? (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Chip icon={Zap} tabIndex={0} className="cursor-help">
                      Cached, $0.000
                    </Chip>
                  </TooltipTrigger>
                  <TooltipContent>{message.cache.note}</TooltipContent>
                </Tooltip>
              ) : (
                <Chip icon={Zap}>Cached, $0.000</Chip>
              )
            ) : null}
          </div>
        ) : null}

        {message.visualization ? (
          <AnswerVisualizationView visualization={message.visualization} />
        ) : null}

        <AnswerText text={message.text} />

        {resolvedPeriod ? (
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="gap-1.5"
            onClick={() => onSuggestionSelect(buildCompareToLastPeriodQuestion(resolvedPeriod))}
          >
            <CalendarRange className="size-3.5" />
            Compare to last period
          </Button>
        ) : null}

        {message.provenance.length > 0 || message.cache ? (
          <div className="space-y-1.5 border-t border-border pt-2.5">
            {message.provenance.length > 0 ? (
              <ProvenanceTag refs={message.provenance} />
            ) : null}
            {message.cache ? <CacheBadge cache={message.cache} /> : null}
          </div>
        ) : null}

        {toolCalls.length > 0 ? (
          <div className="border-t border-border pt-2.5">
            <ShowYourWorkPanel toolCalls={toolCalls} />
          </div>
        ) : null}

        {/* The fix for the product's own "dead composer" defect: a
            successful answer used to end the turn with nothing to do next.
            These chips are deterministic Go, generated from the exact tool
            call and result this answer was narrated from
            (suggestions.go) — never the model guessing at its own
            capabilities. */}
        {followUps.length > 0 ? (
          <div className="space-y-1.5 border-t border-border pt-2.5">
            <p className="text-xs font-medium text-muted-foreground">
              Worth checking next
            </p>
            <SuggestionChips
              label="Worth checking next"
              questions={followUps.map((text) => ({ text }))}
              onSelect={onSuggestionSelect}
            />
          </div>
        ) : null}

        {/* Spec 009: the business-insight teaser, AFTER every part of the
            provenance-backed answer (follow-ups included) and visually
            distinct from all of it — probabilistic advice must never blend
            into computed facts. Rendered only when both the teaser and a
            resolver exist; the full advice is fetched exclusively on tap
            inside the chip, never here. */}
        {message.businessInsight && resolveInsightAndRecordSpend ? (
          <div className="border-t border-border pt-2.5">
            <BusinessInsightChip
              teaser={message.businessInsight}
              toolCalls={toolCalls}
              resolveBusinessInsight={resolveInsightAndRecordSpend}
            />
          </div>
        ) : null}
      </div>
    </li>
  )
}

function ClarificationBubble({
  message,
  onOptionSelect,
}: {
  message: ClarificationChatMessage
  onOptionSelect: (text: string) => void
}) {
  return (
    <li className="flex items-start gap-2">
      <ChatAvatar role="assistant" />
      <div className="max-w-[85%] space-y-2.5 rounded-2xl rounded-tl-sm border border-border bg-card px-4 py-3">
        <p className="flex items-center gap-1.5 text-sm font-medium text-foreground">
          <CircleHelp className="size-3.5 text-muted-foreground" aria-hidden="true" />
          Let me make sure I've got this right
        </p>
        <p className="text-sm leading-relaxed text-foreground">
          {message.text}
        </p>
        {message.options && message.options.length > 0 ? (
          <div className="flex flex-wrap gap-2 pt-0.5">
            {message.options.map((option) => (
              <button
                key={option}
                type="button"
                onClick={() => onOptionSelect(option)}
                className="rounded-full border border-warning/30 bg-background px-3 py-1 text-xs font-medium text-foreground transition-colors hover:bg-warning/10"
              >
                {option}
              </button>
            ))}
          </div>
        ) : null}
      </div>
    </li>
  )
}

function RefusalBubble({
  message,
  onSuggestionSelect,
}: {
  message: RefusalChatMessage
  onSuggestionSelect: (text: string) => void
}) {
  return (
    <li className="flex items-start gap-2">
      <ChatAvatar role="assistant" tone="refusal" />
      <div className="max-w-[85%] space-y-2 rounded-2xl rounded-tl-sm border border-primary/25 bg-primary/5 px-4 py-3">
        <p className="flex items-center gap-1.5 text-sm font-medium text-primary">
          <Compass className="size-3.5" aria-hidden="true" />
          I&apos;ll help you find what you need
        </p>
        <p className="text-sm leading-relaxed text-foreground">
          {message.text}
        </p>
        {message.missing.length > 0 ? (
          <ul className="list-inside list-disc space-y-0.5 text-xs text-muted-foreground">
            {message.missing.map((item) => (
              <li key={item}>{item}</li>
            ))}
          </ul>
        ) : null}

        {/* A correct refusal is still a dead end unless it hands the reader a
            way back in. These are the same tool-grounded examples the empty
            state offers — deterministic, never the model describing itself. */}
        <div className="space-y-1.5 border-t border-primary/15 pt-2">
          <p className="text-xs font-medium text-foreground">
            Here&apos;s what I can answer:
          </p>
          <SuggestionChips
            label="Questions this product can answer"
            questions={EXAMPLE_QUESTIONS.slice(0, 3)}
            onSelect={onSuggestionSelect}
          />
        </div>
        {message.cache ? <CacheBadge cache={message.cache} /> : null}
      </div>
    </li>
  )
}

function ErrorBubble({
  message,
  onRetry,
}: {
  message: ErrorChatMessage
  onRetry: (question: string) => void
}) {
  const interrupted = message.cause === 'interrupted'
  return (
    <li className="flex items-start gap-2">
      <ChatAvatar role="assistant" tone="warning" />
      <div className="max-w-[85%] space-y-2 rounded-2xl rounded-tl-sm border border-border bg-muted/50 px-4 py-3">
        <p className="flex items-center gap-1.5 text-sm font-medium text-foreground">
          {interrupted ? (
            <Unplug className="size-3.5 text-muted-foreground" aria-hidden="true" />
          ) : (
            <PlugZap className="size-3.5 text-muted-foreground" aria-hidden="true" />
          )}
          {interrupted
            ? 'This answer never made it back to you'
            : "I couldn't reach your data just now"}
        </p>
        <p className="text-sm leading-relaxed text-foreground">
          {message.text}
        </p>
        {/* The honest version of what happened, in both directions. A
            transport failure cost nothing; an interruption very likely DID
            run and therefore very likely was charged, and this product's
            instrumentation principle ("never hide or under-report spend")
            means saying so rather than letting it read as a free retry. */}
        <p className="text-xs text-muted-foreground">
          {interrupted
            ? 'Your question ran, so it may already be counted in the running model-spend total — asking again will cost again.'
            : 'This is a connection problem, not a refusal — your question was never answered either way.'}
        </p>
        <button
          type="button"
          onClick={() => onRetry(message.question)}
          className="inline-flex items-center gap-1.5 rounded-full border border-border bg-background px-3 py-1
            text-xs font-medium text-foreground transition-colors hover:bg-muted
            focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
        >
          <RotateCw className="size-3" aria-hidden="true" />
          Try again
        </button>
      </div>
    </li>
  )
}

/**
 * Empty conversation state. Nielsen #6 (recognition over recall) and #1
 * (visibility of system status): says what this surface can answer, before
 * the first question is asked.
 */
function EmptyState({
  suggestions,
  onSelect,
}: {
  suggestions: ExampleQuestion[]
  onSelect: (text: string) => void
}) {
  // Real, live coverage range (lib/useDataCoverage) — not a hardcoded date
  // string. Renders nothing coverage-specific until it resolves rather than
  // showing a stale or guessed range while loading.
  const coverage = useDataCoverage()
  return (
    <li className="flex flex-col items-start gap-5 px-1 py-6">
      <div className="space-y-3">
        <p className="text-sm font-medium text-foreground">
          Ask anything about your reconciled numbers.
        </p>
        {/* These chips used to run as one longer paragraph. As chips they
            are scannable, and each is a claim the reader can check against
            what the app then does. */}
        <div className="flex flex-wrap items-center gap-1.5">
          {coverage.label ? (
            <Chip icon={CalendarRange}>{coverage.label}</Chip>
          ) : null}
          <Chip icon={FileText}>Source rows on every figure</Chip>
        </div>
        {coverage.label ? (
          <p className="max-w-prose-measure text-sm leading-relaxed text-muted-foreground">
            {buildCapabilitySummary(coverage.label)}
          </p>
        ) : null}
      </div>
      <SuggestionChips
        label="Example questions"
        questions={suggestions}
        onSelect={onSelect}
        showTool
      />
    </li>
  )
}

function PendingBubble() {
  return (
    <li
      className="flex items-center gap-2 text-muted-foreground"
      aria-live="polite"
    >
      <ChatAvatar role="assistant" pending />
      <span className="inline-flex items-center gap-1.5 rounded-2xl rounded-tl-sm border border-border bg-card px-3 py-2 text-xs">
        <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
        Checking the reconciled numbers…
      </span>
    </li>
  )
}

/**
 * Conversation view for asking natural-language questions about reconciled
 * margin data (User Story 2) and seeing a clarifying question or refusal
 * instead of a guessed number when the data can't support one (User Story 3).
 * Self-contained: manages its own message state and, absent a real
 * `resolveAnswer`, demonstrates all three response paths against mocked data.
 */
export default function ChatPanel({
  initialMessages,
  resolveAnswer,
  resolveBusinessInsight,
  suggestions = DEFAULT_SUGGESTIONS,
  persistConversation = false,
  prefillQuestion,
  className,
}: ChatPanelProps) {
  // Real, live coverage range (lib/useDataCoverage) for the capability-ideas
  // popover's summary sentence — see EmptyState's own use of this same hook
  // for why it's fetched rather than hardcoded.
  const coverage = useDataCoverage()
  // Restored synchronously in the initializer, not in an effect: mounting
  // empty and then swapping in the saved thread a frame later would flash
  // the empty state on every reload.
  //
  // `reconcileInterruptedMessages` rather than a plain `loadThreadStore`: a
  // question left pending by a previous page load has to be resolved to an
  // honest, retryable state HERE, before the first paint, or the reader sees
  // their own question sitting under a spinner that will never stop.
  const [threadStore, setThreadStore] = React.useState<ThreadStore | null>(() =>
    persistConversation ? reconcileInterruptedMessages() : null,
  )
  const [savedPrompts, setSavedPrompts] = React.useState<SavedPrompt[]>(() =>
    persistConversation ? loadSavedPrompts() : [],
  )
  // Only used when this panel is NOT persisting (tests, embeddings). When it
  // is, storage is the source of truth and `messages` below is a view of it —
  // see chatStorage's doc comment for why that inversion is the whole fix.
  const [localMessages, setLocalMessages] = React.useState<ChatMessage[]>(() =>
    persistConversation ? [] : (initialMessages ?? SEED_MESSAGES),
  )
  const messages = React.useMemo<ChatMessage[]>(() => {
    if (!persistConversation) return localMessages
    return threadStore ? (activeThread(threadStore)?.messages ?? []) : []
  }, [localMessages, persistConversation, threadStore])
  const [draft, setDraft] = React.useState('')
  // Derived, never stored. A boolean "is something in flight" was itself part
  // of the bug: it lived only in React state, so unmounting erased the fact
  // that an answer was coming. The pending MESSAGE is the durable record; the
  // in-flight registry (document-scoped, not persisted) is what says whether
  // THIS document is the one still waiting on it. Another tab's pending
  // question therefore shows its bubble here without freezing this composer.
  const isPending = messages.some(
    (message) =>
      message.role === 'assistant' &&
      message.kind === 'pending' &&
      isRequestInFlight(message.id),
  )
  const viewportRef = React.useRef<HTMLDivElement>(null)
  const [isPinnedToBottom, setIsPinnedToBottom] = React.useState(true)
  const pinnedRef = React.useRef(true)
  const [ideasOpen, setIdeasOpen] = React.useState(false)
  const [composerOpen, setComposerOpen] = React.useState(false)
  const listRef = React.useRef<HTMLOListElement>(null)
  const composerHintId = React.useId()
  const [historyOpen, setHistoryOpen] = React.useState(false)
  const composerRef = React.useRef<HTMLDivElement>(null)
  // Matches the old static pb-28 (112px) as the pre-measurement default, so
  // there's no visible jump on first paint before the ResizeObserver below
  // fires once.
  const [composerHeight, setComposerHeight] = React.useState(112)

  // The panel's own view of the store, readable from a settled promise long
  // after the render that produced it. Passed to every commit as the base to
  // fold storage into, so a thread this panel created but has not yet
  // written (the very first question of a fresh browser) is still known to
  // the commit rather than being treated as a stranger.
  const threadStoreRef = React.useRef<ThreadStore | null>(threadStore)
  React.useEffect(() => {
    threadStoreRef.current = threadStore
  }, [threadStore])

  // Absorbs a store written by anything else — another tab through the
  // browser's `storage` event, or this panel's own commit from a settled
  // request — by MERGING it into what this panel already has rather than
  // replacing it.
  //
  // This is the direct fix for two tabs destroying each other's history. The
  // old code had no listener at all and re-persisted its whole mount-time
  // snapshot on every render, so the last tab to write silently won. Merging
  // is safe here precisely because messages are append-only and only move
  // forward through the resolution lattice (see chatStorage).
  //
  // `previous.activeId` is preserved on purpose: which thread this tab is
  // looking at is this reader's choice, and must not follow another tab's.
  React.useEffect(() => {
    if (!persistConversation) return
    return subscribeToThreadStore((incoming) => {
      setThreadStore((previous) =>
        previous ? mergeThreadStores(incoming, previous, previous.activeId) : incoming,
      )
    })
  }, [persistConversation])

  // Materialises a freshly minted thread into storage on mount, so a second
  // tab opening the same empty app joins THIS thread instead of silently
  // starting its own. Declared after the subscription above on purpose: the
  // write notifies subscribers, and this panel needs to already be one.
  React.useEffect(() => {
    if (!persistConversation) return
    const store = threadStoreRef.current
    if (store) materialiseThreadStore(store)
  }, [persistConversation])

  // The other half of the same story: a pending question this document is
  // not waiting on can only be resolved by whichever tab notices it. Mount
  // covers the reload case; coming back to a backgrounded tab covers "the
  // tab that owned the request was closed", which would otherwise leave a
  // spinner running forever in every OTHER tab. Safe to run eagerly because
  // the placeholder it writes ranks below a real verdict — if the owning tab
  // is in fact still working, its answer supersedes this automatically.
  React.useEffect(() => {
    if (!persistConversation) return
    function reconcileWhenVisible() {
      if (document.visibilityState !== 'visible') return
      setThreadStore((previous) => reconcileInterruptedMessages(previous ?? undefined))
    }
    document.addEventListener('visibilitychange', reconcileWhenVisible)
    return () => document.removeEventListener('visibilitychange', reconcileWhenVisible)
  }, [persistConversation])

  /**
   * The one write path for conversation changes.
   *
   * Deliberately NOT a `setMessages` call with a persist effect hanging off
   * it. `commitThreadMessages` is a plain module function that read-modify-
   * writes live storage, so it behaves identically whether this component is
   * still mounted or was unmounted the instant after the request was sent —
   * which is exactly the case that used to throw completed, already-billed
   * answers away. The `setThreadStore` that follows is only the local view
   * catching up, and is a no-op after unmount rather than the thing that
   * makes the write happen.
   */
  const commitMessages = React.useCallback(
    (threadId: string | null, mutate: (messages: ChatMessage[]) => ChatMessage[]) => {
      if (threadId === null) {
        setLocalMessages(mutate)
        return
      }
      const next = commitThreadMessages(
        threadId,
        mutate,
        threadStoreRef.current ?? undefined,
      )
      threadStoreRef.current = next
      setThreadStore((previous) =>
        previous ? mergeThreadStores(next, previous, previous.activeId) : next,
      )
    },
    [],
  )

  // Auto-scroll, fixed. Three defects were live here before this pass, all
  // confirmed by measuring the real page rather than reading the code:
  //
  //  1. `<ScrollArea className="flex-1">` had no `min-h-0`. A column flex
  //     item's automatic minimum size is its CONTENT height, so the Radix
  //     root grew to the full message list (measured: 761px inside a 574px
  //     panel) and its viewport never overflowed at all —
  //     scrollHeight === clientHeight === 761. There was nothing to scroll.
  //  2. Because the viewport couldn't scroll, `scrollIntoView` walked up to
  //     the next scrollable ancestor and scrolled the `overflow-hidden`
  //     <section> itself (measured: section.scrollTop = 245), dragging the
  //     panel header off the top.
  //  3. The composer, laid out after the overflowing list, was pushed past
  //     the section's clipped edge entirely (measured: form top 614 vs.
  //     section bottom 600) — invisible, not merely misplaced.
  //
  // The fix is `min-h-0` on the scroll area plus scrolling the viewport
  // directly. `scrollIntoView` is not used at all any more: it is defined to
  // scroll EVERY scrollable ancestor, so even once the viewport works it can
  // still move the page underneath the panel. Setting `scrollTop` moves
  // exactly one element and nothing else.
  // Instant, never smooth. A smooth scroll is an animation that takes many
  // frames, during which the scroll handler below sees intermediate
  // positions far from the bottom and un-pins the view — so the pin fought
  // its own animation. Snapping in one frame keeps "pinned" true throughout
  // and is what makes the ResizeObserver below safe to re-fire.
  const scrollToBottom = React.useCallback(() => {
    const viewport = viewportRef.current
    if (!viewport) return
    viewport.scrollTop = viewport.scrollHeight
  }, [])

  // `useLayoutEffect` rather than `useEffect`: the new message is measured
  // and the scroll applied in the same frame it is painted, so a smooth
  // scroll never starts from a stale scrollHeight.
  React.useLayoutEffect(() => {
    if (!isPinnedToBottom) return
    scrollToBottom()
  }, [messages.length, isPending, isPinnedToBottom, scrollToBottom])

  // A second, separate re-pin keyed on the composer's own measured height —
  // NOT covered by the content-resize observer below. That observer's
  // default box option is content-box, which by definition excludes
  // padding, so growing the list's padding-bottom (this component's own
  // fix for the composer overlapping the last message) never fires it: the
  // composer growing taller — the suggestions panel opening, on a short
  // viewport — moved its own top edge up over the last message with
  // nothing re-syncing scrollTop to the new, larger scrollHeight. Measured
  // live: padding-bottom was already correctly 376px, but a 231px overlap
  // persisted indefinitely without this. `useLayoutEffect` again, so the
  // resync happens in the same commit as the padding change, not a visible
  // frame later.
  React.useLayoutEffect(() => {
    if (!isPinnedToBottom) return
    scrollToBottom()
  }, [composerHeight, isPinnedToBottom, scrollToBottom])

  // Re-pin whenever the CONTENT grows, not just when a message is appended.
  // An answer carrying a chart is measurably taller after its SVG lays out
  // than at the moment the message was added, so the one-shot scroll above
  // ran against a stale scrollHeight and left the newest answer 435px below
  // the fold — verified at 1512x982, on the get_margin_delta bar-chart turn.
  // A late-loading font or an expanded table view has the same effect.
  React.useEffect(() => {
    const viewport = viewportRef.current
    const list = listRef.current
    if (!viewport || !list || typeof ResizeObserver === 'undefined') return

    const observer = new ResizeObserver(() => {
      if (!pinnedRef.current) return
      viewport.scrollTop = viewport.scrollHeight
    })
    observer.observe(list)
    return () => observer.disconnect()
  }, [])

  // The floating composer's real height, not a guess. It is an
  // absolutely-positioned overlay sitting ON TOP of the scrollable list, so
  // the list's bottom padding has to clear whatever the composer's ACTUAL
  // rendered height is — and that height is not fixed: the textarea below
  // uses `field-sizing: content` and genuinely grows with a multi-line
  // (Shift+Enter) question, and the "ideasOpen" suggestions panel adds a
  // whole extra block above the input row when open. A hardcoded pb-28
  // (112px) only happened to be enough for a single-line draft with the
  // panel closed — any taller composer state overlapped and hid the bottom
  // of the newest message, which is exactly the "message gets cut off"
  // report this fixes. Measuring the real node instead of estimating its
  // height is the same discipline as every other "trust but verify" fix in
  // this codebase, just applied to layout instead of data.
  React.useEffect(() => {
    const composer = composerRef.current
    if (!composer || typeof ResizeObserver === 'undefined') return

    const observer = new ResizeObserver((entries) => {
      const height = entries[0]?.borderBoxSize?.[0]?.blockSize ?? composer.offsetHeight
      // A little breathing room beyond the composer's exact edge, matching
      // the visual gap the old pb-28 left above the gradient fade.
      setComposerHeight(Math.ceil(height) + 24)
    })
    observer.observe(composer)
    return () => observer.disconnect()
  }, [])

  // Nielsen #3, user control and freedom: a chat that yanks you back down
  // while you are reading an earlier answer is the single most common chat
  // UX failure. Auto-scroll therefore only applies while the reader is
  // already at the bottom; scrolling up hands control back, and the
  // "Jump to latest" affordance below hands it back on request.
  //
  // Bound as a native listener on the viewport rather than an `onScroll` prop
  // on <ScrollArea>: that prop spreads onto Radix's Root, and `scroll` events
  // do not bubble, so a handler there never fires at all (verified — the
  // jump-to-latest control stayed hidden no matter how far the log was
  // scrolled).
  React.useEffect(() => {
    const viewport = viewportRef.current
    if (!viewport) return

    function handleScroll() {
      if (!viewport) return
      const { scrollTop, scrollHeight, clientHeight } = viewport
      const distanceFromBottom = scrollHeight - scrollTop - clientHeight
      const pinned = distanceFromBottom <= BOTTOM_STICK_THRESHOLD_PX
      // Mirrored into a ref as well as state: the ResizeObserver callback
      // above is created once and would otherwise close over the initial
      // value forever.
      pinnedRef.current = pinned
      setIsPinnedToBottom(pinned)
    }

    viewport.addEventListener('scroll', handleScroll, { passive: true })
    return () => viewport.removeEventListener('scroll', handleScroll)
  }, [])

  const activeThreadId = persistConversation ? (threadStore?.activeId ?? null) : null

  /**
   * Records spend for a model call that did not produce a new message —
   * today only the business-insight advice call. Skipped entirely when this
   * panel isn't persisting, because there is no ledger to write to and a
   * non-persisting panel is by definition not the live, billed surface.
   */
  const recordMessageSpend = React.useCallback(
    (messageId: string, interactions: CostInteraction[]) => {
      if (activeThreadId === null) return
      recordSpend(activeThreadId, `${messageId}:insight`, interactions)
    },
    [activeThreadId],
  )

  // A ref, not the derived `isPending` above, guards re-entry: `isPending`
  // is computed from a render's snapshot of `messages`, so two submissions
  // dispatched inside one tick would both see `false`. The ref flips
  // synchronously.
  const submitLockRef = React.useRef(false)

  const submitQuestion = React.useCallback(
    async (rawText: string) => {
      const text = rawText.trim()
      if (!text || submitLockRef.current) return
      submitLockRef.current = true

      // Captured now, not read later: an answer must land in the thread its
      // question was asked in even if the reader opens a different thread —
      // or a different tab does — while the request is in flight.
      const threadId = persistConversation ? (threadStore?.activeId ?? null) : null
      const history = messages

      // Derived from the history BEFORE this message is appended — the
      // clarification being answered (or the answer this might be a
      // follow-up to) is the one currently on screen. Mutually exclusive by
      // construction (see each function's doc comment): at most one of
      // these is ever defined for a given submission.
      const pendingClarification = derivePendingClarification(history)
      const previousExchange = derivePreviousExchange(history)

      const userMessage: UserChatMessage = {
        id: nextMessageId('user'),
        role: 'user',
        text,
        askedAt: new Date().toISOString(),
      }
      // The pending placeholder is written with the question, in the SAME
      // commit, before the request is made. Its id is then reused as the id
      // of whatever verdict comes back, so resolving is an in-place update of
      // one message rather than an append that a merge could duplicate.
      const pendingMessage: PendingChatMessage = {
        id: nextMessageId('assistant'),
        role: 'assistant',
        kind: 'pending',
        question: text,
        askedAt: new Date().toISOString(),
      }

      markRequestInFlight(pendingMessage.id)
      commitMessages(threadId, (previous) => [...previous, userMessage, pendingMessage])
      setDraft('')
      // Asking always re-pins: the reader just acted, so the newest message
      // is unambiguously what they want to see.
      pinnedRef.current = true
      setIsPinnedToBottom(true)

      /**
       * Writes the verdict, and the spend it cost, in one commit.
       *
       * Called from a settled promise, so it must not depend on this
       * component still being mounted — and it doesn't: `recordSpend` and
       * `commitThreadMessages` (inside `commitMessages`) both write storage
       * directly. That is the whole of the "an answer completed after the
       * reader navigated away is not thrown on the floor" fix.
       */
      function settle(verdict: AssistantChatMessage) {
        if (threadId !== null) {
          recordSpend(threadId, pendingMessage.id, readInteractions(verdict))
        }
        const stored = { ...withoutInteractions(verdict), id: pendingMessage.id }
        commitMessages(threadId, (previous) =>
          replaceMessage(previous, pendingMessage.id, stored),
        )
      }

      try {
        const answer = await (resolveAnswer ?? mockResolveAnswer)(
          text,
          [...history, userMessage],
          pendingClarification,
          previousExchange,
        )
        settle(answer)
      } catch (error) {
        // Found live: reloading mid-request makes the browser abort the
        // fetch, and that rejection lands HERE, before the page tears down.
        // Treating it as a transport failure was doubly wrong — it claimed
        // the data was unreachable when the request had already been sent
        // (and will be completed and billed), and it overwrote the pending
        // record the next page load needs in order to recognise the
        // interruption at all. Leaving the record alone is what makes the
        // reload case recoverable.
        if (isDocumentUnloading()) return
        // Nielsen #9, help users recognize and recover from errors. Before
        // this pass a failed `/api/ask` (backend down, non-2xx) rejected into
        // a bare `finally`: the spinner vanished and NOTHING appeared, so a
        // dead backend was indistinguishable from a question that silently
        // did nothing.
        settle({
          id: pendingMessage.id,
          role: 'assistant',
          kind: 'error',
          cause: 'transport',
          text:
            error instanceof Error
              ? error.message
              : 'The request failed before an answer could be computed.',
          question: text,
          askedAt: new Date().toISOString(),
        })
      } finally {
        clearRequestInFlight(pendingMessage.id)
        submitLockRef.current = false
      }
    },
    [commitMessages, messages, persistConversation, resolveAnswer, threadStore],
  )

  // Populates the draft only — never submits on the owner's behalf. Guarded
  // the same way the old auto-submit effect was: fires exactly once per
  // distinct `prefillQuestion` value, so a duplicate navigation with the
  // same question never re-stomps text the owner has since started editing.
  const lastPrefilledRef = React.useRef<string | null>(null)
  React.useEffect(() => {
    if (!prefillQuestion || lastPrefilledRef.current === prefillQuestion) {
      return
    }
    lastPrefilledRef.current = prefillQuestion
    setDraft(prefillQuestion)
  }, [prefillQuestion])

  function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    void submitQuestion(draft)
  }

  function handleKeyDown(event: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault()
      void submitQuestion(draft)
    }
  }

  return (
    <section
      aria-label="Ask about your margin"
      className={cn(
        // Fills whatever height the shell gives it instead of a fixed 36rem.
        // min-h-0 lets it shrink on a short viewport; the min-h-[20rem] floor
        // keeps the composer and at least one message visible if it ever
        // lands somewhere genuinely tiny.
        'mx-auto flex h-full min-h-[20rem] w-full max-w-content flex-col overflow-hidden rounded-xl border border-border bg-background',
        className,
      )}
    >
      <header className="flex flex-wrap items-start justify-between gap-3 border-b border-border px-4 py-3 sm:px-6">
        <div>
          <p className="text-micro font-medium uppercase tracking-wider text-muted-foreground">
            Reconciliation Q&amp;A
          </p>
          {/* h1, not h2: this panel is the whole content of the /ask route, and
              axe's page-has-heading-one fired on that route because the only
              heading here was a level 2 under no level 1. */}
          <h1 className="text-2xl font-semibold tracking-tight text-foreground">
            Ask about your margin
          </h1>
        </div>

        {threadStore ? (
          <div className="relative flex items-center gap-1.5">
            <button
              type="button"
              onClick={() => {
                setHistoryOpen((open) => !open)
              }}
              aria-expanded={historyOpen}
              className="flex items-center gap-1.5 rounded-md border border-border bg-card px-2.5 py-1.5
                text-xs font-medium text-muted-foreground transition-colors hover:text-foreground
                focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
            >
              <History className="size-3.5" aria-hidden="true" />
              Recent
            </button>
            <button
              type="button"
              onClick={() => {
                // `messages` is a view of the store now, so switching
                // threads is one state change, not two that could disagree.
                setThreadStore(startNewThread(threadStore))
                setHistoryOpen(false)
                pinnedRef.current = true
                setIsPinnedToBottom(true)
              }}
              className="flex items-center gap-1.5 rounded-md border border-border bg-card px-2.5 py-1.5
                text-xs font-medium text-foreground transition-colors hover:bg-muted
                focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
            >
              <SquarePen className="size-3.5" aria-hidden="true" />
              New chat
            </button>

            {historyOpen ? (
              <ul
                aria-label="Recent conversations"
                className="absolute right-0 top-full z-30 mt-1.5 w-72 overflow-hidden rounded-lg
                  border border-border bg-popover shadow-lg"
              >
                {threadStore.threads.map((thread) => (
                  <li key={thread.id}>
                    <button
                      type="button"
                      onClick={() => {
                        setThreadStore(openThread(threadStore, thread.id))
                        setHistoryOpen(false)
                        pinnedRef.current = true
                        setIsPinnedToBottom(true)
                      }}
                      className={cn(
                        'flex w-full flex-col items-start gap-0.5 px-3 py-2 text-left transition-colors hover:bg-muted',
                        thread.id === threadStore.activeId && 'bg-primary/5',
                      )}
                    >
                      <span className="line-clamp-1 text-xs font-medium text-foreground">
                        {thread.title}
                      </span>
                      <span className="text-[10px] text-muted-foreground">
                        {thread.messages.length === 0
                          ? 'Empty'
                          : thread.messages.length === 1
                            ? '1 message'
                            : `${thread.messages.length} messages`}
                        {thread.id === threadStore.activeId ? ' · current' : ''}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            ) : null}
          </div>
        ) : null}
      </header>

      {/* `relative` anchors the floating composer; `min-h-0` is the actual
          scroll-bug fix — without it this flex child's automatic minimum
          size is its content height and the viewport below never overflows. */}
      <div className="relative min-h-0 flex-1">
        <ScrollArea className="h-full" viewportRef={viewportRef}>
          {/* role="log" lives on a wrapper, not on the <ol> itself. Putting it
              on the list replaced the list role, which stranded every <li>
              inside a non-list parent — two real axe violations on this route
              (listitem + aria-allowed-role). The live region still announces
              new messages; the list still exposes its items. */}
          <div role="log" aria-live="polite" aria-label="Conversation">
          <ol
            ref={listRef}
            /* Bottom padding clears the floating composer so the newest
               message is never parked underneath it — measured from the
               composer's real height (see the ResizeObserver above), not a
               fixed guess that a multi-line question or the suggestions
               panel would outgrow. */
            className="space-y-4 px-4 pt-4 sm:px-6"
            style={{ paddingBottom: composerHeight }}
          >
            {messages.length === 0 ? (
              <EmptyState
                suggestions={suggestions}
                onSelect={submitQuestion}
              />
            ) : null}
            {messages.map((message) =>
              message.role === 'user' ? (
                <UserBubble key={message.id} message={message} />
              ) : message.kind === 'answer' ? (
                <AnswerBubble
                  key={message.id}
                  message={message}
                  onSuggestionSelect={submitQuestion}
                  resolveBusinessInsight={resolveBusinessInsight}
                  onSpend={recordMessageSpend}
                />
              ) : message.kind === 'clarification' ? (
                <ClarificationBubble
                  key={message.id}
                  message={message}
                  onOptionSelect={submitQuestion}
                />
              ) : message.kind === 'error' ? (
                <ErrorBubble
                  key={message.id}
                  message={message}
                  onRetry={submitQuestion}
                />
              ) : message.kind === 'pending' ? (
                /* Driven by the persisted pending message rather than by a
                   transient boolean appended after the list. That is what
                   makes the spinner survive an in-page navigation away and
                   back, and what makes an interrupted question resolvable
                   into something honest instead of vanishing. */
                <PendingBubble key={message.id} />
              ) : (
                <RefusalBubble
                  key={message.id}
                  message={message}
                  onSuggestionSelect={submitQuestion}
                />
              ),
            )}
          </ol>
          </div>
        </ScrollArea>

        {/* Jump-to-latest — only offered while the reader has scrolled away,
            so it never adds chrome to the common case. */}
        {!isPinnedToBottom ? (
          <button
            type="button"
            onClick={() => {
              pinnedRef.current = true
              setIsPinnedToBottom(true)
              scrollToBottom()
            }}
            className="absolute bottom-24 left-1/2 z-20 flex -translate-x-1/2 items-center gap-1.5
              rounded-full border border-border bg-popover px-3 py-1.5 text-xs font-medium
              text-foreground shadow-md transition-colors hover:bg-muted
              focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
          >
            <ArrowDown className="size-3.5" aria-hidden="true" />
            Jump to latest
          </button>
        ) : null}

        {/* Floating composer: elevated, rounded, anchored to the bottom of the
            panel, with the message list scrolling underneath it. The gradient
            strip behind it fades the list out rather than cutting it with a
            hard rule, which is what makes the bar read as sitting ABOVE the
            conversation instead of being a footer bolted below it. */}
        <div
          ref={composerRef}
          className="pointer-events-none absolute inset-x-0 bottom-0 z-10 px-3 pb-3 sm:px-5 sm:pb-4"
        >
          {/* Solid for its bottom half, then a short fade — a pure from/to
              gradient leaves the bar's own height half-transparent and
              message text shows through it, which reads as a rendering bug
              rather than depth. Fixed at 80px (not tied to the composer's
              own measured height, which can range from ~90px resting to
              350px+ with the suggestions panel open) specifically so this
              backdrop stays a small lip hugging the composer's bottom edge
              — a taller fixed value here previously extended 60-70px above
              a normal single-line composer's actual top edge, fading real
              conversation content that had nothing to do with the composer
              and reading as a shadow "overlapping" the chat log. */}
          <div
            aria-hidden="true"
            className="pointer-events-none absolute inset-x-0 bottom-0 -z-10 h-20
              bg-[linear-gradient(to_top,var(--background)_0%,var(--background)_55%,transparent_100%)]"
          />
          {/* Persistent capability shortcut. The empty state disappears after
              the first question and a refusal only appears when one fires —
              a returning user needs a way to be reminded what's worth asking
              at any point, not only on those two occasions. */}
          {ideasOpen ? (
            <div className="pointer-events-auto mb-2 max-h-[45vh] overflow-y-auto rounded-xl border border-border bg-card p-2.5 shadow-lg">
              {savedPrompts.length > 0 ? (
                <div className="mb-3">
                  <p className="mb-1.5 text-xs font-medium text-foreground">
                    Your saved questions
                  </p>
                  <ul className="flex flex-wrap gap-1.5">
                    {savedPrompts.map((prompt) => (
                      <li
                        key={prompt.id}
                        className="flex items-stretch overflow-hidden rounded-lg border border-border bg-background shadow-sm"
                      >
                        <button
                          type="button"
                          onClick={() => {
                            // Refills the composer rather than sending: a
                            // saved question is usually a starting point the
                            // owner tweaks (a different date), not a command.
                            setDraft(prompt.text)
                            setIdeasOpen(false)
                          }}
                          className="max-w-[18rem] px-2.5 py-1.5 text-left text-xs font-medium text-foreground
                            transition-colors hover:bg-primary/5 focus-visible:outline-none
                            focus-visible:ring-[3px] focus-visible:ring-ring/50"
                        >
                          <span className="line-clamp-2">{prompt.text}</span>
                        </button>
                        <button
                          type="button"
                          onClick={() =>
                            setSavedPrompts((prompts) =>
                              removeSavedPrompt(prompts, prompt.id),
                            )
                          }
                          aria-label={`Delete saved question: ${prompt.text}`}
                          className="border-l border-border px-1.5 text-muted-foreground transition-colors
                            hover:bg-destructive/10 hover:text-destructive-text focus-visible:outline-none
                            focus-visible:ring-[3px] focus-visible:ring-ring/50"
                        >
                          <X className="size-3" aria-hidden="true" />
                        </button>
                      </li>
                    ))}
                  </ul>
                </div>
              ) : null}

              {coverage.label ? (
                <p className="mb-1.5 text-xs text-muted-foreground">
                  {buildCapabilitySummary(coverage.label)}
                </p>
              ) : null}
              <SuggestionChips
                label="Example questions"
                questions={suggestions}
                onSelect={(text) => {
                  setIdeasOpen(false)
                  void submitQuestion(text)
                }}
                showTool
              />
            </div>
          ) : null}

          {/* Entry point for the guided question composer — walks an owner
              who doesn't know what to ask through the 8 real, answerable
              categories step by step, then hands the assembled question off
              to this exact same composer/submit path (onAsk below). It
              supplements the free-text input and the example-question
              chips above; it never replaces either. */}
          <div className="pointer-events-auto mb-1.5 flex items-center">
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="gap-1.5"
              onClick={() => setComposerOpen(true)}
            >
              <Wand2 className="size-3.5" aria-hidden="true" />
              Build a question
            </Button>
          </div>

          <form
            onSubmit={handleSubmit}
            className="pointer-events-auto flex items-end gap-2 rounded-2xl border border-border
              bg-card p-2 shadow-lg ring-1 ring-black/[0.03] transition-shadow
              focus-within:border-primary/40 focus-within:shadow-xl dark:ring-white/[0.04]"
          >
            <Textarea
              value={draft}
              onChange={(event) => setDraft(event.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="Ask about today, this week, or a promotion…"
              disabled={isPending}
              rows={1}
              aria-label="Ask a question about your margin"
              aria-describedby={composerHintId}
              /* max-h + overflow-y-auto, not just min-h. `field-sizing:
                 content` (ui/textarea.tsx) genuinely grows the box with its
                 value and has NO upper bound of its own, so pasting a long
                 question grew the composer until it filled — and then
                 outgrew — the panel, taking the Send button off screen with
                 it. 10rem is about six lines: enough to see a multi-line
                 question whole, after which the textarea scrolls internally
                 instead of the page. Set here as well as on the shared
                 primitive because this instance overrides the primitive's
                 own sizing classes. */
              className="min-h-10 max-h-40 resize-none overflow-y-auto border-0 bg-transparent
                shadow-none focus-visible:ring-0 dark:bg-transparent"
            />
            {/* These two composer buttons used to carry their hint on a
                native `title=` attribute — the browser's own unstyled
                tooltip, on its own delay, with no visual cue either button
                had one at all. `Button` now forwards a ref (ui/button.tsx),
                so it can sit inside `TooltipTrigger asChild` and get the
                app's own styled tooltip instead; `aria-label` (unchanged)
                stays the source of truth for assistive tech either way. */}
            {persistConversation ? (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    type="button"
                    size="icon"
                    variant="ghost"
                    disabled={draft.trim().length === 0}
                    onClick={() => {
                      setSavedPrompts((prompts) => addSavedPrompt(prompts, draft))
                      setIdeasOpen(true)
                    }}
                    aria-label="Save this question for reuse"
                    className="mb-0.5 shrink-0 rounded-xl text-muted-foreground hover:text-foreground"
                  >
                    <Bookmark className="size-4" aria-hidden="true" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>Save this question for reuse</TooltipContent>
              </Tooltip>
            ) : null}
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  type="button"
                  size="icon"
                  variant="ghost"
                  onClick={() => setIdeasOpen((open) => !open)}
                  aria-expanded={ideasOpen}
                  aria-label={ideasOpen ? 'Hide example questions' : 'Show example questions'}
                  className="mb-0.5 shrink-0 rounded-xl text-muted-foreground hover:text-foreground"
                >
                  <Lightbulb className="size-4" aria-hidden="true" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>What can I ask?</TooltipContent>
            </Tooltip>
            <Button
              type="submit"
              size="icon"
              className="mb-0.5 shrink-0 rounded-xl"
              disabled={isPending || draft.trim().length === 0}
              aria-label="Send question"
            >
              {isPending ? (
                <Loader2 className="size-4 animate-spin" aria-hidden="true" />
              ) : (
                <Send className="size-4" aria-hidden="true" />
              )}
            </Button>
          </form>
          {/* Nielsen #6 again: the Enter/Shift+Enter split is already
              implemented but was undiscoverable. */}
          <p
            id={composerHintId}
            className="pointer-events-none mt-1.5 px-1 text-center text-micro text-muted-foreground"
          >
            <kbd className="font-sans font-medium">Enter</kbd> to send ·{' '}
            <kbd className="font-sans font-medium">Shift</kbd>+
            <kbd className="font-sans font-medium">Enter</kbd> for a new line
          </p>
        </div>
      </div>

      <QuestionComposer
        open={composerOpen}
        onClose={() => setComposerOpen(false)}
        onAsk={(question) => {
          setComposerOpen(false)
          void submitQuestion(question)
        }}
        minDate={coverage.start}
        maxDate={coverage.end}
      />
    </section>
  )
}
