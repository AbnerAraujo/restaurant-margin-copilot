import * as React from 'react'
import {
  ArrowDown,
  Bot,
  CircleHelp,
  Loader2,
  PlugZap,
  RotateCw,
  Send,
  ShieldAlert,
  User,
  Zap,
} from 'lucide-react'

import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'
import ProvenanceTag, {
  type SourceRowRef,
} from '@/components/Provenance/ProvenanceTag'
import AnswerVisualizationView from '@/components/Charts/AnswerVisualizationView'
import type { AnswerVisualization } from '@/components/Charts/answerVisualization'

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

export interface UserChatMessage {
  id: string
  role: 'user'
  text: string
  askedAt: string
}

export interface AnswerChatMessage {
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
  askedAt: string
}

export interface ClarificationChatMessage {
  id: string
  role: 'assistant'
  kind: 'clarification'
  text: string
  /** Quick-reply shortcuts so the owner can resolve the ambiguity in one tap. */
  options?: string[]
  cache?: AnswerCacheInfo
  askedAt: string
}

export interface RefusalChatMessage {
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
  /** The failed question, so the retry affordance can resend it verbatim. */
  question: string
  askedAt: string
}

export type AssistantChatMessage =
  | AnswerChatMessage
  | ClarificationChatMessage
  | RefusalChatMessage
  | ErrorChatMessage

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
  ) => Promise<AssistantChatMessage>
  /**
   * Starter questions offered when the conversation is empty (Nielsen #6,
   * recognition rather than recall — an empty box gives a time-poor owner no
   * clue what this thing can actually answer). Only rendered when there are
   * no messages at all, so it never competes with a live conversation.
   */
  suggestions?: string[]
  className?: string
}

// ---------------------------------------------------------------------------
// Mocked conversation + resolver — realistic fixture-shaped data standing in
// for the backend per this task's brief; no live API exists yet.
// ---------------------------------------------------------------------------

let messageSequence = 0
function nextMessageId(prefix: string): string {
  messageSequence += 1
  return `${prefix}-${messageSequence}`
}

/**
 * How close to the bottom still counts as "following the conversation".
 * A few pixels of slack absorbs sub-pixel rounding and the momentum tail of
 * a smooth scroll, which would otherwise unpin the view the instant an
 * auto-scroll finished.
 */
const BOTTOM_STICK_THRESHOLD_PX = 48

/**
 * Starter questions for an empty conversation. Every one of these is
 * answerable by a real MCP tool against the ingested fixture range — none
 * is aspirational, so a first click can never land on a refusal caused by
 * this list rather than by the data.
 */
const DEFAULT_SUGGESTIONS = [
  'How did we do on 2026-08-07?',
  'Which days had discrepancies this month?',
  'Compare margin for 2026-08-01 to 2026-08-07 against 2026-08-08 to 2026-08-14',
  'Which promotions lost money?',
]

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
}: {
  role: 'user' | 'assistant'
  tone?: 'neutral' | 'warning' | 'destructive'
}) {
  const toneClasses =
    tone === 'warning'
      ? 'bg-warning/10 text-warning-text'
      : tone === 'destructive'
        ? 'bg-destructive/10 text-destructive-text'
        : role === 'user'
          ? 'bg-primary/10 text-primary'
          : 'bg-muted text-muted-foreground'

  return (
    <Avatar size="sm" className="mt-0.5 shrink-0">
      <AvatarFallback className={toneClasses}>
        {role === 'user' ? (
          <User className="size-3.5" />
        ) : (
          <Bot className="size-3.5" />
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
 */
function CacheBadge({ cache }: { cache: AnswerCacheInfo }) {
  return (
    <p
      className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-[11px] text-muted-foreground"
      title={cache.note}
    >
      <Zap className="size-3 shrink-0" aria-hidden="true" />
      <span className="font-medium text-foreground">Served from cache</span>
      <span>— no model call, $0.000 spent</span>
      {cache.cost_avoided_usd > 0 ? (
        <span className="tabular-nums">
          (saved ${cache.cost_avoided_usd.toFixed(3)})
        </span>
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

function AnswerBubble({ message }: { message: AnswerChatMessage }) {
  return (
    <li className="flex items-start gap-2">
      <ChatAvatar role="assistant" />
      <div className="max-w-[85%] space-y-2 rounded-2xl rounded-tl-sm border border-border bg-card px-4 py-3">
        <p className="text-sm leading-relaxed text-foreground">
          {message.text}
        </p>
        {message.visualization ? (
          <AnswerVisualizationView visualization={message.visualization} />
        ) : null}
        {message.provenance.length > 0 || message.cache ? (
          <div className="space-y-1.5 border-t border-border/60 pt-2">
            {message.provenance.length > 0 ? (
              <ProvenanceTag refs={message.provenance} />
            ) : null}
            {message.cache ? <CacheBadge cache={message.cache} /> : null}
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
      <ChatAvatar role="assistant" tone="warning" />
      <div className="max-w-[85%] space-y-2.5 rounded-2xl rounded-tl-sm border border-warning/25 bg-warning/10 px-4 py-3">
        <p className="flex items-center gap-1.5 text-xs font-medium uppercase tracking-wide text-warning-text">
          <CircleHelp className="size-3.5" aria-hidden="true" />
          Needs a quick clarification
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

function RefusalBubble({ message }: { message: RefusalChatMessage }) {
  return (
    <li className="flex items-start gap-2">
      <ChatAvatar role="assistant" tone="destructive" />
      <div className="max-w-[85%] space-y-2 rounded-2xl rounded-tl-sm border border-destructive/25 bg-destructive/10 px-4 py-3">
        <p className="flex items-center gap-1.5 text-xs font-medium uppercase tracking-wide text-destructive-text">
          <ShieldAlert className="size-3.5" aria-hidden="true" />
          Can&apos;t answer this one
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
  return (
    <li className="flex items-start gap-2">
      <ChatAvatar role="assistant" tone="warning" />
      <div className="max-w-[85%] space-y-2 rounded-2xl rounded-tl-sm border border-border bg-muted/50 px-4 py-3">
        <p className="flex items-center gap-1.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">
          <PlugZap className="size-3.5" aria-hidden="true" />
          Couldn&apos;t reach the reconciliation engine
        </p>
        <p className="text-sm leading-relaxed text-foreground">
          {message.text}
        </p>
        <p className="text-xs text-muted-foreground">
          This is a connection problem, not a refusal — your question was never
          answered either way.
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
 * (visibility of system status): says what this surface can answer, and
 * states plainly that it will refuse rather than guess — the product's own
 * contract, surfaced before the first question rather than discovered on a
 * refusal.
 */
function EmptyState({
  suggestions,
  onSelect,
}: {
  suggestions: string[]
  onSelect: (text: string) => void
}) {
  return (
    <li className="flex flex-col items-start gap-4 px-1 py-6">
      <div className="space-y-1.5">
        <p className="text-sm font-medium text-foreground">
          Ask anything about your reconciled numbers.
        </p>
        <p className="max-w-md text-sm leading-relaxed text-muted-foreground">
          Every figure comes from the deterministic reconciliation engine and
          arrives with its source rows attached. If the data can&apos;t support
          an answer, you&apos;ll get a refusal — never a plausible guess.
        </p>
      </div>
      {suggestions.length > 0 ? (
        <div className="flex flex-wrap gap-2">
          {suggestions.map((suggestion) => (
            <button
              key={suggestion}
              type="button"
              onClick={() => onSelect(suggestion)}
              className="rounded-full border border-border bg-card px-3 py-1.5 text-xs font-medium
                text-foreground shadow-sm transition-colors hover:border-primary/40 hover:bg-primary/5
                focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
            >
              {suggestion}
            </button>
          ))}
        </div>
      ) : null}
    </li>
  )
}

function PendingBubble() {
  return (
    <li
      className="flex items-center gap-2 text-muted-foreground"
      aria-live="polite"
    >
      <ChatAvatar role="assistant" />
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
  suggestions = DEFAULT_SUGGESTIONS,
  className,
}: ChatPanelProps) {
  const [messages, setMessages] = React.useState<ChatMessage[]>(
    () => initialMessages ?? SEED_MESSAGES,
  )
  const [draft, setDraft] = React.useState('')
  const [isPending, setIsPending] = React.useState(false)
  const viewportRef = React.useRef<HTMLDivElement>(null)
  const [isPinnedToBottom, setIsPinnedToBottom] = React.useState(true)
  const composerHintId = React.useId()

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
  const scrollToBottom = React.useCallback((behavior: ScrollBehavior) => {
    const viewport = viewportRef.current
    if (!viewport) return
    viewport.scrollTo({ top: viewport.scrollHeight, behavior })
  }, [])

  // `useLayoutEffect` rather than `useEffect`: the new message is measured
  // and the scroll applied in the same frame it is painted, so a smooth
  // scroll never starts from a stale scrollHeight.
  React.useLayoutEffect(() => {
    if (!isPinnedToBottom) return
    scrollToBottom(messages.length <= 1 ? 'auto' : 'smooth')
  }, [messages.length, isPending, isPinnedToBottom, scrollToBottom])

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
      setIsPinnedToBottom(distanceFromBottom <= BOTTOM_STICK_THRESHOLD_PX)
    }

    viewport.addEventListener('scroll', handleScroll, { passive: true })
    return () => viewport.removeEventListener('scroll', handleScroll)
  }, [])

  const submitQuestion = React.useCallback(
    async (rawText: string) => {
      const text = rawText.trim()
      if (!text || isPending) return

      const userMessage: UserChatMessage = {
        id: nextMessageId('user'),
        role: 'user',
        text,
        askedAt: new Date().toISOString(),
      }
      setMessages((previous) => [...previous, userMessage])
      setDraft('')
      setIsPending(true)
      // Asking always re-pins: the reader just acted, so the newest message
      // is unambiguously what they want to see.
      setIsPinnedToBottom(true)

      try {
        const answer = await (resolveAnswer ?? mockResolveAnswer)(text, [
          ...messages,
          userMessage,
        ])
        setMessages((previous) => [...previous, answer])
      } catch (error) {
        // Nielsen #9, help users recognize and recover from errors. Before
        // this pass a failed `/api/ask` (backend down, non-2xx) rejected into
        // a bare `finally`: the spinner vanished and NOTHING appeared, so a
        // dead backend was indistinguishable from a question that silently
        // did nothing.
        setMessages((previous) => [
          ...previous,
          {
            id: nextMessageId('assistant'),
            role: 'assistant',
            kind: 'error',
            text:
              error instanceof Error
                ? error.message
                : 'The request failed before an answer could be computed.',
            question: text,
            askedAt: new Date().toISOString(),
          },
        ])
      } finally {
        setIsPending(false)
      }
    },
    [isPending, messages, resolveAnswer],
  )

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
        'mx-auto flex h-[36rem] max-h-[80vh] w-full max-w-3xl flex-col overflow-hidden rounded-lg border border-border bg-background',
        className,
      )}
    >
      <header className="border-b border-border px-4 py-3 sm:px-6">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Reconciliation Q&amp;A
        </p>
        <h2 className="text-2xl font-semibold tracking-tight text-foreground">
          Ask about your margin
        </h2>
      </header>

      {/* `relative` anchors the floating composer; `min-h-0` is the actual
          scroll-bug fix — without it this flex child's automatic minimum
          size is its content height and the viewport below never overflows. */}
      <div className="relative min-h-0 flex-1">
        <ScrollArea className="h-full" viewportRef={viewportRef}>
          <ol
            role="log"
            aria-live="polite"
            /* Bottom padding clears the floating composer so the newest
               message is never parked underneath it. */
            className="space-y-4 px-4 pb-28 pt-4 sm:px-6"
          >
            {messages.length === 0 && !isPending ? (
              <EmptyState
                suggestions={suggestions}
                onSelect={submitQuestion}
              />
            ) : null}
            {messages.map((message) =>
              message.role === 'user' ? (
                <UserBubble key={message.id} message={message} />
              ) : message.kind === 'answer' ? (
                <AnswerBubble key={message.id} message={message} />
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
              ) : (
                <RefusalBubble key={message.id} message={message} />
              ),
            )}
            {isPending ? <PendingBubble /> : null}
          </ol>
        </ScrollArea>

        {/* Jump-to-latest — only offered while the reader has scrolled away,
            so it never adds chrome to the common case. */}
        {!isPinnedToBottom ? (
          <button
            type="button"
            onClick={() => {
              setIsPinnedToBottom(true)
              scrollToBottom('smooth')
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
        <div className="pointer-events-none absolute inset-x-0 bottom-0 z-10 px-3 pb-3 sm:px-5 sm:pb-4">
          {/* Solid to ~70% of its height, then a short fade — a pure
              from/to gradient leaves the bar's own height half-transparent
              and message text shows through it, which reads as a rendering
              bug rather than depth. */}
          <div
            aria-hidden="true"
            className="pointer-events-none absolute inset-x-0 bottom-0 -z-10 h-40
              bg-[linear-gradient(to_top,var(--background)_0%,var(--background)_72%,transparent_100%)]"
          />
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
              className="min-h-10 resize-none border-0 bg-transparent shadow-none
                focus-visible:ring-0 dark:bg-transparent"
            />
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
            className="pointer-events-none mt-1.5 px-1 text-center text-[11px] text-muted-foreground"
          >
            <kbd className="font-sans font-medium">Enter</kbd> to send ·{' '}
            <kbd className="font-sans font-medium">Shift</kbd>+
            <kbd className="font-sans font-medium">Enter</kbd> for a new line
          </p>
        </div>
      </div>
    </section>
  )
}
