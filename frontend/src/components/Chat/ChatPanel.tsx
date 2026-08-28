import * as React from 'react'
import { Bot, CircleHelp, Loader2, Send, ShieldAlert, User } from 'lucide-react'

import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'
import ProvenanceTag, {
  type SourceRowRef,
} from '@/components/Provenance/ProvenanceTag'

// ---------------------------------------------------------------------------
// Types — shaped to line up with QuestionInteraction in data-model.md and the
// answer/refusal contract in mcp-tools.md, so a real `resolveAnswer` can map
// a backend response onto these fields with no reshaping.
// ---------------------------------------------------------------------------

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
  askedAt: string
}

export interface ClarificationChatMessage {
  id: string
  role: 'assistant'
  kind: 'clarification'
  text: string
  /** Quick-reply shortcuts so the owner can resolve the ambiguity in one tap. */
  options?: string[]
  askedAt: string
}

export interface RefusalChatMessage {
  id: string
  role: 'assistant'
  kind: 'refusal'
  text: string
  /** What's missing that prevents a real answer — never a guessed figure. */
  missing: string[]
  askedAt: string
}

export type AssistantChatMessage =
  AnswerChatMessage | ClarificationChatMessage | RefusalChatMessage

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
        {message.provenance.length > 0 ? (
          <div className="border-t border-border/60 pt-2">
            <ProvenanceTag refs={message.provenance} />
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
      </div>
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
  className,
}: ChatPanelProps) {
  const [messages, setMessages] = React.useState<ChatMessage[]>(
    () => initialMessages ?? SEED_MESSAGES,
  )
  const [draft, setDraft] = React.useState('')
  const [isPending, setIsPending] = React.useState(false)
  const bottomRef = React.useRef<HTMLLIElement>(null)

  React.useEffect(() => {
    bottomRef.current?.scrollIntoView?.({ behavior: 'smooth', block: 'end' })
  }, [messages.length, isPending])

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

      try {
        const answer = await (resolveAnswer ?? mockResolveAnswer)(text, [
          ...messages,
          userMessage,
        ])
        setMessages((previous) => [...previous, answer])
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

      <ScrollArea className="flex-1">
        <ol
          role="log"
          aria-live="polite"
          className="space-y-4 px-4 py-4 sm:px-6"
        >
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
            ) : (
              <RefusalBubble key={message.id} message={message} />
            ),
          )}
          {isPending ? <PendingBubble /> : null}
          <li ref={bottomRef} aria-hidden="true" className="h-0" />
        </ol>
      </ScrollArea>

      <form
        onSubmit={handleSubmit}
        className="flex items-end gap-2 border-t border-border p-4 sm:p-5"
      >
        <Textarea
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Ask about today, this week, or a promotion…"
          disabled={isPending}
          rows={1}
          aria-label="Ask a question about your margin"
          className="min-h-10 resize-none"
        />
        <Button
          type="submit"
          size="icon"
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
    </section>
  )
}
