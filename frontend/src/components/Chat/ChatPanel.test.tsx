import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '@/lib/api'

import {
  activeThread,
  loadSpendLedger,
  loadThreadStore,
  type ThreadStore,
} from '@/lib/chatStorage'
import ChatPanel, {
  derivePendingClarification,
  derivePreviousExchange,
  type AssistantChatMessage,
  type ChatMessage,
} from './ChatPanel'

const THREADS_KEY = 'mbs.chat.threads.v2'

/** What `loadThreadStore()` would return after a reload, right now. */
function persistedMessages(): ChatMessage[] {
  return activeThread(loadThreadStore())?.messages ?? []
}

/**
 * Simulates ANOTHER TAB writing the shared key. Writing storage directly and
 * dispatching the browser's own `storage` event is the only faithful
 * simulation available in jsdom: a same-document commit would go through the
 * in-process notifier and prove nothing about the cross-tab path.
 */
function simulateOtherTabWrite(mutate: (store: ThreadStore) => ThreadStore): void {
  const next = mutate(loadThreadStore())
  const newValue = JSON.stringify(next)
  window.localStorage.setItem(THREADS_KEY, newValue)
  window.dispatchEvent(
    new StorageEvent('storage', { key: THREADS_KEY, newValue }),
  )
}

/** A promise plus its resolver, for asserting on an in-flight pending state. */
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => {
    resolve = res
  })
  return { promise, resolve }
}

describe('derivePendingClarification', () => {
  const question: ChatMessage = {
    id: 'u1',
    role: 'user',
    text: 'Which days had discrepancies this month?',
    askedAt: '2026-08-27T10:00:00Z',
  }
  const clarification: ChatMessage = {
    id: 'a1',
    role: 'assistant',
    kind: 'clarification',
    text: 'Do you mean August 2026?',
    askedAt: '2026-08-27T10:00:02Z',
  }

  it('pairs a pending clarification with the question it was asked about', () => {
    expect(derivePendingClarification([question, clarification])).toEqual({
      originalQuestion: 'Which days had discrepancies this month?',
      clarifyingQuestion: 'Do you mean August 2026?',
    })
  })

  it('returns nothing when the last message is not a clarification', () => {
    const answer: ChatMessage = {
      id: 'a2',
      role: 'assistant',
      kind: 'answer',
      text: 'Two days had flags.',
      provenance: [],
      askedAt: '2026-08-27T10:00:05Z',
    }
    expect(derivePendingClarification([question, clarification, answer])).toBeUndefined()
    expect(derivePendingClarification([])).toBeUndefined()
    expect(derivePendingClarification([question])).toBeUndefined()
  })

  it('skips back past intervening assistant turns to find the real question', () => {
    const refusal: ChatMessage = {
      id: 'a0',
      role: 'assistant',
      kind: 'refusal',
      text: 'no',
      missing: [],
      askedAt: '2026-08-27T09:59:00Z',
    }
    expect(
      derivePendingClarification([refusal, question, clarification]),
    ).toEqual({
      originalQuestion: 'Which days had discrepancies this month?',
      clarifyingQuestion: 'Do you mean August 2026?',
    })
  })
})

describe('derivePreviousExchange', () => {
  const question: ChatMessage = {
    id: 'u1',
    role: 'user',
    text: 'What was our margin on 2026-08-05?',
    askedAt: '2026-08-27T10:00:00Z',
  }
  const answer: ChatMessage = {
    id: 'a1',
    role: 'assistant',
    kind: 'answer',
    text: 'Margin on 2026-08-05 was $612.40.',
    provenance: [],
    askedAt: '2026-08-27T10:00:02Z',
  }

  it('pairs the previous answer with the question it answered', () => {
    expect(derivePreviousExchange([question, answer])).toEqual({
      question: 'What was our margin on 2026-08-05?',
      answerText: 'Margin on 2026-08-05 was $612.40.',
    })
  })

  it('returns nothing when the last message is not a real answer', () => {
    const clarification: ChatMessage = {
      id: 'a2',
      role: 'assistant',
      kind: 'clarification',
      text: 'Which period did you mean?',
      askedAt: '2026-08-27T10:00:05Z',
    }
    const refusal: ChatMessage = {
      id: 'a3',
      role: 'assistant',
      kind: 'refusal',
      text: 'no data',
      missing: [],
      askedAt: '2026-08-27T10:00:06Z',
    }
    const errorMsg: ChatMessage = {
      id: 'a4',
      role: 'assistant',
      kind: 'error',
      text: 'connection failed',
      question: 'x',
      askedAt: '2026-08-27T10:00:07Z',
    }
    expect(derivePreviousExchange([question, clarification])).toBeUndefined()
    expect(derivePreviousExchange([question, refusal])).toBeUndefined()
    expect(derivePreviousExchange([question, errorMsg])).toBeUndefined()
    expect(derivePreviousExchange([])).toBeUndefined()
    expect(derivePreviousExchange([question])).toBeUndefined()
  })

  it('skips back past intervening assistant turns to find the real question', () => {
    const priorRefusal: ChatMessage = {
      id: 'a0',
      role: 'assistant',
      kind: 'refusal',
      text: 'no',
      missing: [],
      askedAt: '2026-08-27T09:59:00Z',
    }
    expect(derivePreviousExchange([priorRefusal, question, answer])).toEqual({
      question: 'What was our margin on 2026-08-05?',
      answerText: 'Margin on 2026-08-05 was $612.40.',
    })
  })

  // Mutual exclusivity with derivePendingClarification is by construction
  // (each only fires on a different last-message kind), asserted directly
  // here rather than trusted: the same tail can never satisfy both.
  it('never returns a value at the same time as derivePendingClarification', () => {
    expect(derivePreviousExchange([question, answer])).toBeDefined()
    expect(derivePendingClarification([question, answer])).toBeUndefined()

    const clarification: ChatMessage = {
      id: 'c1',
      role: 'assistant',
      kind: 'clarification',
      text: 'Which period?',
      askedAt: '2026-08-27T10:00:05Z',
    }
    expect(derivePendingClarification([question, clarification])).toBeDefined()
    expect(derivePreviousExchange([question, clarification])).toBeUndefined()
  })
})

describe('ChatPanel', () => {
  it('renders the seeded conversation with a grounded answer and its provenance citation', () => {
    render(<ChatPanel />)

    expect(
      screen.getByText(/today's reconciled margin was \$612\.40/i),
    ).toBeInTheDocument()
    // Two source rows back this answer, so the shared ProvenanceTag trigger
    // (FR-005 — the same component used everywhere else in the app) renders
    // its "N source files" form rather than a single inline citation.
    expect(
      screen.getByRole('button', { name: '2 source files' }),
    ).toBeInTheDocument()
  })

  it('renders a clarification response with a visibly distinct banner and quick-reply options', () => {
    render(<ChatPanel />)

    const clarificationLabel = screen.getByText(/let me make sure/i)
    expect(clarificationLabel).toBeInTheDocument()

    // distinct from a normal answer bubble: it carries its own eyebrow label
    // and quick-reply chips that an answer bubble never renders
    expect(
      screen.getByRole('button', { name: /include friday \(fri–sun\)/i }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: /saturday–sunday only/i }),
    ).toBeInTheDocument()
  })

  it('renders a refusal with a distinct banner and its missing-data list, never a citation', () => {
    render(<ChatPanel />)

    const refusalLabel = screen.getByText(/find what you need/i)
    expect(refusalLabel).toBeInTheDocument()

    const refusalItem = screen.getByText(
      /no uber eats promotion export on file/i,
    )
    const refusalBubble = refusalItem.closest('li')
    expect(refusalBubble).not.toBeNull()

    const withinRefusal = within(refusalBubble as HTMLElement)
    expect(
      withinRefusal.getByText(/uber eats ad-spend export for aug 18–24/i),
    ).toBeInTheDocument()
    // A refusal never carries a provenance citation (data-model.md: refusal
    // implies provenance_refs = []). Asserted precisely rather than as "no
    // buttons at all" — a refusal now also offers capability-guidance chips,
    // which are navigation, not a citation of a number it declined to give.
    expect(
      withinRefusal.queryByRole('button', { name: /source/i }),
    ).not.toBeInTheDocument()
    expect(
      withinRefusal.queryByRole('group', { name: /source citations/i }),
    ).not.toBeInTheDocument()

    // ...and it must hand the reader a way forward rather than dead-ending.
    expect(
      withinRefusal.getByRole('list', {
        name: /questions this product can answer/i,
      }),
    ).toBeInTheDocument()
  })

  it('renders the refusal and clarification banners with different visual treatments', () => {
    render(<ChatPanel />)

    const refusalBanner = screen
      .getByText(/find what you need/i)
      .closest('div')
    const clarificationBanner = screen
      .getByText(/let me make sure/i)
      .closest('div')

    // Clarification moved off an amber "warning" treatment (a routine
    // question, not a caution) onto the same calm, neutral card an
    // ordinary answer uses — refusal keeps its distinct primary-brand
    // tint, so the two still read as visually different from each other.
    expect(refusalBanner?.className).toContain('border-primary/25')
    expect(clarificationBanner?.className).toContain('border-border')
    expect(refusalBanner?.className).not.toBe(clarificationBanner?.className)
  })

  it('expands a provenance citation to reveal its individual source rows, and hides it again', async () => {
    const user = userEvent.setup()
    render(<ChatPanel />)

    const citation = screen.getByRole('button', { name: '2 source files' })
    expect(
      screen.queryByRole('group', { name: /source citations/i }),
    ).not.toBeInTheDocument()

    await user.click(citation)
    const panel = screen.getByRole('group', { name: /source citations/i })
    expect(panel).toHaveTextContent('pos_export_2026-08-27.csv')
    expect(panel).toHaveTextContent('daily_reconciliation.csv')

    await user.click(citation)
    expect(
      screen.queryByRole('group', { name: /source citations/i }),
    ).not.toBeInTheDocument()
  })

  it('sends a typed question, disables the composer while pending, and renders the resolved answer', async () => {
    const user = userEvent.setup()
    const { promise, resolve } = deferred<AssistantChatMessage>()
    const resolveAnswer = vi
      .fn<
        (
          question: string,
          history: ChatMessage[],
        ) => Promise<AssistantChatMessage>
      >()
      .mockReturnValue(promise)

    render(<ChatPanel initialMessages={[]} resolveAnswer={resolveAnswer} />)

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'How did we do yesterday?')
    await user.click(screen.getByRole('button', { name: /send question/i }))

    expect(screen.getByText('How did we do yesterday?')).toBeInTheDocument()
    expect(input).toHaveValue('')
    expect(input).toBeDisabled()
    expect(
      screen.getByText(/checking the reconciled numbers/i),
    ).toBeInTheDocument()

    resolve({
      id: 'test-answer-1',
      role: 'assistant',
      kind: 'answer',
      text: 'Test-resolved margin answer.',
      provenance: [
        {
          source_file: 'daily_reconciliation.csv',
          row_start: 26,
          row_end: 26,
          period_start: '2026-08-26',
          period_end: '2026-08-26',
        },
      ],
      askedAt: '2026-08-27T10:00:00Z',
    })

    expect(
      await screen.findByText('Test-resolved margin answer.'),
    ).toBeInTheDocument()
    expect(resolveAnswer).toHaveBeenCalledWith(
      'How did we do yesterday?',
      expect.arrayContaining([
        expect.objectContaining({ text: 'How did we do yesterday?' }),
      ]),
      // No clarification was pending and no prior answer exists yet (this is
      // the first message), so neither context is attached.
      undefined,
      undefined,
    )
    expect(input).not.toBeDisabled()
  })

  it('submits a clarification quick-reply as a new question and resolves its answer', async () => {
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<
        (
          question: string,
          history: ChatMessage[],
        ) => Promise<AssistantChatMessage>
      >()
      .mockResolvedValue({
        id: 'test-answer-2',
        role: 'assistant',
        kind: 'answer',
        text: 'Resolved after clarification.',
        provenance: [],
        askedAt: '2026-08-27T10:05:00Z',
      })

    const clarificationOnly: ChatMessage[] = [
      {
        id: 'u1',
        role: 'user',
        text: 'Which period did you mean?',
        askedAt: '2026-08-27T10:03:00Z',
      },
      {
        id: 'c1',
        role: 'assistant',
        kind: 'clarification',
        text: 'Which range did you mean?',
        options: ['Option A'],
        askedAt: '2026-08-27T10:04:00Z',
      },
    ]

    render(
      <ChatPanel
        initialMessages={clarificationOnly}
        resolveAnswer={resolveAnswer}
      />,
    )

    await user.click(screen.getByRole('button', { name: 'Option A' }))

    expect(screen.getByText('Option A', { selector: 'p' })).toBeInTheDocument()
    expect(
      await screen.findByText('Resolved after clarification.'),
    ).toBeInTheDocument()
    // The quick-reply chip is a reply to the clarification above it, so it
    // must carry that clarification's context — without it the backend sees
    // an orphaned fragment and refuses.
    expect(resolveAnswer).toHaveBeenCalledWith(
      'Option A',
      expect.any(Array),
      { originalQuestion: 'Which period did you mean?', clarifyingQuestion: 'Which range did you mean?' },
      // The last message was a clarification, not an answer, so no
      // previous-exchange context is attached — the two are mutually
      // exclusive by construction.
      undefined,
    )
  })

  it('does not submit an empty or whitespace-only question', async () => {
    const user = userEvent.setup()
    const resolveAnswer =
      vi.fn<
        (
          question: string,
          history: ChatMessage[],
        ) => Promise<AssistantChatMessage>
      >()

    render(<ChatPanel initialMessages={[]} resolveAnswer={resolveAnswer} />)

    const sendButton = screen.getByRole('button', { name: /send question/i })
    expect(sendButton).toBeDisabled()

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, '   ')
    expect(sendButton).toBeDisabled()
    expect(resolveAnswer).not.toHaveBeenCalled()
  })

  it('renders an empty conversation as a starter-question state, and submits a suggestion', async () => {
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<
        (
          question: string,
          history: ChatMessage[],
        ) => Promise<AssistantChatMessage>
      >()
      .mockResolvedValue({
        id: 'test-answer-suggestion',
        role: 'assistant',
        kind: 'answer',
        text: 'Answer from a suggestion.',
        provenance: [],
        askedAt: '2026-08-27T10:20:00Z',
      })

    render(
      <ChatPanel
        initialMessages={[]}
        resolveAnswer={resolveAnswer}
        suggestions={[
          {
            text: 'Which promotions lost money?',
            tool: 'list_negative_roi_promotions',
            topic: 'Promotions',
          },
        ]}
      />,
    )

    const suggestion = screen.getByRole('button', {
      name: /Which promotions lost money\?/,
    })
    await user.click(suggestion)

    expect(resolveAnswer).toHaveBeenCalledWith(
      'Which promotions lost money?',
      expect.any(Array),
      undefined,
      undefined,
    )
    expect(
      await screen.findByText('Answer from a suggestion.'),
    ).toBeInTheDocument()
    // The starter state is for an empty thread only — it must not linger
    // alongside a real conversation.
    expect(suggestion).not.toBeInTheDocument()
  })

  it('surfaces a failed request as a connection error distinct from a refusal, and retries it', async () => {
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<
        (
          question: string,
          history: ChatMessage[],
        ) => Promise<AssistantChatMessage>
      >()
      .mockRejectedValueOnce(new Error('/api/ask returned 502: upstream down'))
      .mockResolvedValueOnce({
        id: 'test-answer-retry',
        role: 'assistant',
        kind: 'answer',
        text: 'Answer after retry.',
        provenance: [],
        askedAt: '2026-08-27T10:30:00Z',
      })

    render(<ChatPanel initialMessages={[]} resolveAnswer={resolveAnswer} />)

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'How did we do on 2026-08-07?{Enter}')

    expect(
      await screen.findByText(/couldn't reach your data/i),
    ).toBeInTheDocument()
    // The owner gets a next step, not the request path and status code that
    // this bubble used to print verbatim.
    expect(screen.getByText(/reload this page/i)).toBeInTheDocument()
    expect(
      screen.queryByText('/api/ask returned 502: upstream down'),
    ).not.toBeInTheDocument()
    // A transport failure must never be dressed up as the product's own
    // principled refusal.
    expect(
      screen.queryByText(/find what you need/i),
    ).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /try again/i }))
    expect(await screen.findByText('Answer after retry.')).toBeInTheDocument()
    expect(resolveAnswer).toHaveBeenNthCalledWith(
      2,
      'How did we do on 2026-08-07?',
      expect.any(Array),
      undefined,
      // The prior message was a connection ERROR, not a real answer, so no
      // previous-exchange context is attached to the retry.
      undefined,
    )
  })

  it('renders an answer’s backend-chosen visualization inline in its bubble', async () => {
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<
        (
          question: string,
          history: ChatMessage[],
        ) => Promise<AssistantChatMessage>
      >()
      .mockResolvedValue({
        id: 'test-answer-viz',
        role: 'assistant',
        kind: 'answer',
        text: 'Two days had discrepancy flags.',
        provenance: [],
        visualization: {
          kind: 'table',
          title: 'Flagged days',
          source_tool: 'list_discrepancies',
          columns: ['Date', 'Discrepancy'],
          rows: [['2026-08-03', 'Duplicate order removed']],
        },
        askedAt: '2026-08-27T10:40:00Z',
      })

    render(<ChatPanel initialMessages={[]} resolveAnswer={resolveAnswer} />)

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'Which days had discrepancies?{Enter}')

    const answer = await screen.findByText('Two days had discrepancy flags.')
    const bubble = answer.closest('li')
    expect(bubble).not.toBeNull()
    // The chart belongs to the answer, not to a separate panel beside it.
    expect(within(bubble as HTMLElement).getByRole('table')).toBeInTheDocument()
    expect(
      within(bubble as HTMLElement).getByText('Duplicate order removed'),
    ).toBeInTheDocument()
  })

  it('renders the tool-name chip from the real tool call even when the answer draws no chart', async () => {
    // Regression test for a real QA repro: a day-of-month expense-pattern
    // question answers in prose (with a source-row provenance note) but the
    // backend's deriveVisualization never draws a chart for it. The tool
    // chip must be keyed off `toolCalls`, not `visualization`, so it still
    // shows which typed MCP tool actually ran.
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<
        (
          question: string,
          history: ChatMessage[],
        ) => Promise<AssistantChatMessage>
      >()
      .mockResolvedValueOnce({
        id: 'test-answer-no-viz-tool-chip',
        role: 'assistant',
        kind: 'answer',
        text: 'Expenses tend to run higher in the first week of the month.',
        // Six distinct source-row citations, matching the exact repro that
        // surfaced this bug: a prose-only answer showing "6 source rows"
        // and nothing naming which tool computed it.
        provenance: Array.from({ length: 6 }, (_, index) => ({
          source_file: 'daily_reconciliation.csv',
          row_start: index + 1,
          row_end: index + 1,
          period_start: '2026-08-01',
          period_end: '2026-08-06',
        })),
        toolCalls: [
          {
            name: 'get_expense_pattern',
            result_json: { pattern: 'front_loaded' },
          },
        ],
        askedAt: '2026-08-27T11:35:00Z',
      })

    render(<ChatPanel initialMessages={[]} resolveAnswer={resolveAnswer} />)

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(
      input,
      'Do expenses follow a pattern by day of month?{Enter}',
    )

    const answer = await screen.findByText(
      'Expenses tend to run higher in the first week of the month.',
    )
    const bubble = answer.closest('li') as HTMLElement

    // No chart was drawn — the old bug's exact repro — yet the tool chip
    // must still appear, naming the real tool.
    expect(
      within(bubble).queryByRole('table'),
    ).not.toBeInTheDocument()
    expect(
      within(bubble).getByText('get_expense_pattern', { selector: 'span.font-mono' }),
    ).toBeInTheDocument()
    expect(within(bubble).getByText('6 source rows')).toBeInTheDocument()
  })

  it('still shows the correct tool-name chip when an answer has both a tool call and a chart', async () => {
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<
        (
          question: string,
          history: ChatMessage[],
        ) => Promise<AssistantChatMessage>
      >()
      .mockResolvedValueOnce({
        id: 'test-answer-viz-and-tool-chip',
        role: 'assistant',
        kind: 'answer',
        text: 'Two days had discrepancy flags.',
        provenance: [],
        toolCalls: [
          {
            name: 'list_discrepancies',
            result_json: { flagged_days: 2 },
          },
        ],
        visualization: {
          kind: 'table',
          title: 'Flagged days',
          source_tool: 'list_discrepancies',
          columns: ['Date', 'Discrepancy'],
          rows: [['2026-08-03', 'Duplicate order removed']],
        },
        askedAt: '2026-08-27T11:36:00Z',
      })

    render(<ChatPanel initialMessages={[]} resolveAnswer={resolveAnswer} />)

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'Which days had discrepancies?{Enter}')

    const answer = await screen.findByText('Two days had discrepancy flags.')
    const bubble = answer.closest('li') as HTMLElement

    expect(within(bubble).getByRole('table')).toBeInTheDocument()
    expect(
      within(bubble).getByText('list_discrepancies', { selector: 'span.font-mono' }),
    ).toBeInTheDocument()
  })

  it('renders deterministic follow-up chips under a successful answer, and submits one as a new question', async () => {
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<
        (
          question: string,
          history: ChatMessage[],
        ) => Promise<AssistantChatMessage>
      >()
      .mockResolvedValueOnce({
        id: 'test-answer-followups',
        role: 'assistant',
        kind: 'answer',
        text: 'Margin on 2026-08-07 was $375.82.',
        provenance: [],
        followUps: [
          'Were there any discrepancies on 2026-08-07?',
          'How did 2026-08-01 to 2026-08-07 compare to 2026-08-08 to 2026-08-14?',
        ],
        askedAt: '2026-08-27T11:00:00Z',
      })
      .mockResolvedValueOnce({
        id: 'test-answer-followup-reply',
        role: 'assistant',
        kind: 'answer',
        text: 'No discrepancies were flagged on 2026-08-07.',
        provenance: [],
        askedAt: '2026-08-27T11:01:00Z',
      })

    render(<ChatPanel initialMessages={[]} resolveAnswer={resolveAnswer} />)

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'How did we do on 2026-08-07?{Enter}')

    const answer = await screen.findByText(
      'Margin on 2026-08-07 was $375.82.',
    )
    const bubble = answer.closest('li') as HTMLElement
    expect(
      within(bubble).getByText('Worth checking next'),
    ).toBeInTheDocument()
    const followUpChip = within(bubble).getByRole('button', {
      name: /were there any discrepancies on 2026-08-07\?/i,
    })
    expect(followUpChip).toBeInTheDocument()

    await user.click(followUpChip)

    expect(resolveAnswer).toHaveBeenNthCalledWith(
      2,
      'Were there any discrepancies on 2026-08-07?',
      expect.any(Array),
      undefined,
      // The previous message WAS a real answer, so tapping a follow-up chip
      // carries that answer's context — the one-hop mechanism this feature
      // adds — even though this particular chip's text is self-contained.
      // Deciding whether it's actually a follow-up is the gate's job, not
      // this component's.
      {
        question: 'How did we do on 2026-08-07?',
        answerText: 'Margin on 2026-08-07 was $375.82.',
      },
    )
    expect(
      await screen.findByText('No discrepancies were flagged on 2026-08-07.'),
    ).toBeInTheDocument()
  })

  it('renders no follow-up section when an answer carries no follow-ups', async () => {
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<
        (
          question: string,
          history: ChatMessage[],
        ) => Promise<AssistantChatMessage>
      >()
      .mockResolvedValue({
        id: 'test-answer-no-followups',
        role: 'assistant',
        kind: 'answer',
        text: 'Margin on 2026-08-08 was $152.50.',
        provenance: [],
        askedAt: '2026-08-27T11:05:00Z',
      })

    render(<ChatPanel initialMessages={[]} resolveAnswer={resolveAnswer} />)

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'How did we do on 2026-08-08?{Enter}')

    const answer = await screen.findByText('Margin on 2026-08-08 was $152.50.')
    const bubble = answer.closest('li') as HTMLElement
    expect(within(bubble).queryByText('Worth checking next')).not.toBeInTheDocument()
  })

  it('marks a cached answer as costing nothing, and states the saving separately', async () => {
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<
        (
          question: string,
          history: ChatMessage[],
        ) => Promise<AssistantChatMessage>
      >()
      .mockResolvedValue({
        id: 'test-answer-cached',
        role: 'assistant',
        kind: 'answer',
        text: 'Margin on 2026-08-07 was $375.82.',
        provenance: [],
        cache: {
          hit: true,
          cost_avoided_usd: 0.00527,
          note: 'Exact question match.',
        },
        askedAt: '2026-08-27T10:50:00Z',
      })

    render(<ChatPanel initialMessages={[]} resolveAnswer={resolveAnswer} />)

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'How did we do on 2026-08-07?{Enter}')

    await screen.findByText('Margin on 2026-08-07 was $375.82.')
    expect(screen.getByText('Served from cache')).toBeInTheDocument()
    // Spend and saving are two separate statements — an avoided cost must
    // never read as money spent.
    expect(screen.getByText(/no model call, \$0\.000 spent/i)).toBeInTheDocument()
    expect(screen.getByText(/saved \$0\.005/)).toBeInTheDocument()
  })

  // --- Guided question composer entry point -------------------------------

  it('opens the guided composer from its entry point and hands off the composed question to the normal ask flow', async () => {
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<
        (
          question: string,
          history: ChatMessage[],
        ) => Promise<AssistantChatMessage>
      >()
      .mockResolvedValue({
        id: 'test-answer-guided',
        role: 'assistant',
        kind: 'answer',
        text: 'Margin on 2026-08-07 was $375.82.',
        provenance: [],
        askedAt: '2026-08-27T12:00:00Z',
      })

    render(<ChatPanel initialMessages={[]} resolveAnswer={resolveAnswer} />)

    await user.click(screen.getByRole('button', { name: /build a question/i }))
    const dialog = screen.getByRole('dialog', { name: /build a question/i })

    await user.click(within(dialog).getByText('Check a single day'))
    await user.type(within(dialog).getByLabelText('Date'), '2026-08-07')
    await user.click(within(dialog).getByRole('button', { name: /continue/i }))

    expect(within(dialog).getByLabelText('Your question')).toHaveValue(
      'How did we do on 2026-08-07?',
    )
    await user.click(within(dialog).getByRole('button', { name: /ask this question/i }))

    // The dialog closes and the question goes through the exact same
    // resolveAnswer path as a typed question — no parallel submission path.
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.getByText('How did we do on 2026-08-07?')).toBeInTheDocument()
    expect(resolveAnswer).toHaveBeenCalledWith(
      'How did we do on 2026-08-07?',
      expect.any(Array),
      undefined,
      undefined,
    )
    expect(
      await screen.findByText('Margin on 2026-08-07 was $375.82.'),
    ).toBeInTheDocument()
  })

  it('drives the composer’s advice path through the same ask flow and surfaces the chip', async () => {
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<
        (
          question: string,
          history: ChatMessage[],
        ) => Promise<AssistantChatMessage>
      >()
      .mockResolvedValue({
        id: 'test-answer-advice',
        role: 'assistant',
        kind: 'answer',
        text: 'iFood’s effective commission rate was 23.00%.',
        provenance: [],
        toolCalls: [{ name: 'compare_platform_economics', result_json: {} }],
        businessInsight: {
          kind: 'high_commission',
          title: 'That commission rate is in the platforms’ premium band',
        },
        askedAt: '2026-08-27T12:00:00Z',
      })

    render(
      <ChatPanel
        initialMessages={[]}
        resolveAnswer={resolveAnswer}
        resolveBusinessInsight={vi.fn()}
      />,
    )

    await user.click(screen.getByRole('button', { name: /build a question/i }))
    const dialog = screen.getByRole('dialog', { name: /build a question/i })
    await user.click(within(dialog).getByRole('button', { name: /get business advice/i }))
    await user.click(within(dialog).getByText('Advice on a high commission rate'))
    await user.type(within(dialog).getByLabelText('Start date'), '2026-08-01')
    await user.type(within(dialog).getByLabelText('End date'), '2026-08-14')
    await user.click(within(dialog).getByRole('button', { name: /continue/i }))
    await user.click(
      within(dialog).getByRole('button', { name: /compute this and offer advice/i }),
    )

    // The pattern is computed through the ordinary, guaranteed-answerable ask
    // flow — there is no second submission path, and no advice call has been
    // billed yet.
    expect(resolveAnswer).toHaveBeenCalledWith(
      'Which platform costs me more in commission — iFood or Just Eat Takeaway — between 2026-08-01 and 2026-08-14?',
      expect.any(Array),
      undefined,
      undefined,
    )
    expect(
      await screen.findByText('iFood’s effective commission rate was 23.00%.'),
    ).toBeInTheDocument()
    // The advice itself is still one explicit tap away (spec FR-014).
    expect(
      screen.getByText('That commission rate is in the platforms’ premium band'),
    ).toBeInTheDocument()
    expect(screen.queryByText(/no advice needed/i)).not.toBeInTheDocument()
  })

  it('says so honestly when the requested pattern simply is not in the data', async () => {
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<
        (
          question: string,
          history: ChatMessage[],
        ) => Promise<AssistantChatMessage>
      >()
      .mockResolvedValue({
        id: 'test-answer-no-advice',
        role: 'assistant',
        kind: 'answer',
        text: 'No discrepancies were flagged between 2026-08-01 and 2026-08-14.',
        provenance: [],
        toolCalls: [{ name: 'list_discrepancies', result_json: { days: [] } }],
        // No businessInsight: a clean period genuinely has no pattern to
        // advise on, and Go returns no teaser for one.
        askedAt: '2026-08-27T12:00:00Z',
      })

    render(
      <ChatPanel
        initialMessages={[]}
        resolveAnswer={resolveAnswer}
        resolveBusinessInsight={vi.fn()}
      />,
    )

    await user.click(screen.getByRole('button', { name: /build a question/i }))
    const dialog = screen.getByRole('dialog', { name: /build a question/i })
    await user.click(within(dialog).getByRole('button', { name: /get business advice/i }))
    await user.click(within(dialog).getByText('Advice on recurring discrepancies'))
    await user.type(within(dialog).getByLabelText('Start date'), '2026-08-01')
    await user.type(within(dialog).getByLabelText('End date'), '2026-08-14')
    await user.click(within(dialog).getByRole('button', { name: /continue/i }))
    await user.click(
      within(dialog).getByRole('button', { name: /compute this and offer advice/i }),
    )

    expect(
      await screen.findByText(
        'No discrepancies were flagged between 2026-08-01 and 2026-08-14.',
      ),
    ).toBeInTheDocument()
    // Without this, the owner cannot tell "clean data" from "the advice
    // request quietly went nowhere".
    expect(screen.getByText(/no advice needed/i)).toBeInTheDocument()
    expect(
      screen.getByText(/nothing was flagged in this period/i),
    ).toBeInTheDocument()
  })

  it('never claims a clean result for an ordinary question that got no teaser', async () => {
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<
        (
          question: string,
          history: ChatMessage[],
        ) => Promise<AssistantChatMessage>
      >()
      .mockResolvedValue({
        id: 'test-answer-plain',
        role: 'assistant',
        kind: 'answer',
        text: 'Margin on 2026-08-07 was $375.82.',
        provenance: [],
        askedAt: '2026-08-27T12:00:00Z',
      })

    render(<ChatPanel initialMessages={[]} resolveAnswer={resolveAnswer} />)
    await user.type(
      screen.getByRole('textbox', { name: /ask a question about your margin/i }),
      'How did we do on 2026-08-07?',
    )
    await user.click(screen.getByRole('button', { name: /send question/i }))

    expect(
      await screen.findByText('Margin on 2026-08-07 was $375.82.'),
    ).toBeInTheDocument()
    // Most answers carry no teaser; that is the norm, not an advisory outcome.
    expect(screen.queryByText(/no advice needed/i)).not.toBeInTheDocument()
  })

  it('keeps the scroll area able to shrink below its content height', () => {
    // Regression guard for the measured layout defect: a `flex-1` column
    // child's automatic minimum size is its CONTENT height, so without
    // `min-h-0` the Radix viewport never overflows, nothing is scrollable,
    // and the composer is pushed past the panel's clipped edge.
    const { container } = render(<ChatPanel />)
    const scrollArea = container.querySelector('[data-slot="scroll-area"]')
    expect(scrollArea).not.toBeNull()
    expect(scrollArea?.parentElement?.className).toContain('min-h-0')
  })

  it('submits on Enter and inserts a newline on Shift+Enter instead of submitting', async () => {
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<
        (
          question: string,
          history: ChatMessage[],
        ) => Promise<AssistantChatMessage>
      >()
      .mockResolvedValue({
        id: 'test-answer-3',
        role: 'assistant',
        kind: 'answer',
        text: 'Answer after enter.',
        provenance: [],
        askedAt: '2026-08-27T10:10:00Z',
      })

    render(<ChatPanel initialMessages={[]} resolveAnswer={resolveAnswer} />)
    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })

    await user.type(input, 'line one{Shift>}{Enter}{/Shift}line two')
    expect(resolveAnswer).not.toHaveBeenCalled()
    expect(input).toHaveValue('line one\nline two')

    await user.type(input, '{Enter}')
    expect(resolveAnswer).toHaveBeenCalledTimes(1)
    expect(await screen.findByText('Answer after enter.')).toBeInTheDocument()
  })

  // Spec 008 US1 — flag-based follow-up chips, show-your-work, and chart
  // click-to-ask (via prefillQuestion).

  it('renders a flag-based "why is this different?" follow-up exactly like any other suggestion', async () => {
    // ChatPanel itself never special-cases a flag-based suggestion — the
    // backend (suggestions.go's flagBasedFollowUp) is the only place that
    // decides one belongs in the list. This test proves the panel renders
    // that exact real-world wording correctly as a normal chip, never
    // dropping or garbling it.
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<
        (
          question: string,
          history: ChatMessage[],
        ) => Promise<AssistantChatMessage>
      >()
      .mockResolvedValueOnce({
        id: 'test-answer-flag-followup',
        role: 'assistant',
        kind: 'answer',
        text: 'Margin on 2026-08-08 was $152.50.',
        provenance: [],
        followUps: ['Why is 2026-08-08 different from usual?'],
        askedAt: '2026-08-27T11:10:00Z',
      })

    render(<ChatPanel initialMessages={[]} resolveAnswer={resolveAnswer} />)

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'How did we do on 2026-08-08?{Enter}')

    const answer = await screen.findByText('Margin on 2026-08-08 was $152.50.')
    const bubble = answer.closest('li') as HTMLElement
    expect(
      within(bubble).getByRole('button', {
        name: /why is 2026-08-08 different from usual\?/i,
      }),
    ).toBeInTheDocument()
  })

  it('shows "show your work" collapsed by default, then reveals the real tool name and JSON on click', async () => {
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<
        (
          question: string,
          history: ChatMessage[],
        ) => Promise<AssistantChatMessage>
      >()
      .mockResolvedValueOnce({
        id: 'test-answer-show-your-work',
        role: 'assistant',
        kind: 'answer',
        text: 'Margin on 2026-08-07 was $375.82.',
        provenance: [],
        toolCalls: [
          {
            name: 'get_daily_summary',
            result_json: { date: '2026-08-07', margin: '375.82' },
          },
        ],
        askedAt: '2026-08-27T11:15:00Z',
      })

    render(<ChatPanel initialMessages={[]} resolveAnswer={resolveAnswer} />)

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'How did we do on 2026-08-07?{Enter}')

    const answer = await screen.findByText('Margin on 2026-08-07 was $375.82.')
    const bubble = answer.closest('li') as HTMLElement

    // The tool-name chip (derived from the real tool call, independent of
    // any visualization) is visible immediately — it is not gated behind
    // the disclosure. Only the raw JSON is collapsed by default.
    expect(
      within(bubble).getByText('get_daily_summary', { selector: 'span.font-mono' }),
    ).toBeInTheDocument()
    expect(
      within(bubble).queryByRole('group', { name: /tool calls behind this answer/i }),
    ).not.toBeInTheDocument()
    expect(within(bubble).queryByText(/"margin": "375\.82"/)).not.toBeInTheDocument()

    const toggle = within(bubble).getByRole('button', { name: /show your work/i })
    await user.click(toggle)

    const panel = within(bubble).getByRole('group', {
      name: /tool calls behind this answer/i,
    })
    expect(within(panel).getByText('get_daily_summary')).toBeInTheDocument()
    expect(within(panel).getByText(/"margin": "375\.82"/)).toBeInTheDocument()
  })

  it('renders no "show your work" affordance when an answer carries no tool calls', async () => {
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<
        (
          question: string,
          history: ChatMessage[],
        ) => Promise<AssistantChatMessage>
      >()
      .mockResolvedValue({
        id: 'test-answer-no-tool-calls',
        role: 'assistant',
        kind: 'answer',
        text: 'Margin on 2026-08-08 was $152.50.',
        provenance: [],
        askedAt: '2026-08-27T11:20:00Z',
      })

    render(<ChatPanel initialMessages={[]} resolveAnswer={resolveAnswer} />)

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'How did we do on 2026-08-08?{Enter}')

    const answer = await screen.findByText('Margin on 2026-08-08 was $152.50.')
    const bubble = answer.closest('li') as HTMLElement
    expect(
      within(bubble).queryByRole('button', { name: /show your work/i }),
    ).not.toBeInTheDocument()
  })

  it('offers "Compare to last period" on an answer with a resolved period, and submits the real derived comparison question', async () => {
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<
        (
          question: string,
          history: ChatMessage[],
        ) => Promise<AssistantChatMessage>
      >()
      .mockResolvedValueOnce({
        id: 'test-answer-resolved-period',
        role: 'assistant',
        kind: 'answer',
        text: 'Your margin for August 2026 was $8,214.50.',
        provenance: [],
        resolvedPeriod: { start: '2026-08-01', end: '2026-08-31' },
        askedAt: '2026-08-27T11:25:00Z',
      })
      .mockResolvedValueOnce({
        id: 'test-answer-comparison',
        role: 'assistant',
        kind: 'answer',
        text: 'July 2026 was $7,900.00 — August was up $314.50.',
        provenance: [],
        askedAt: '2026-08-27T11:25:05Z',
      })

    render(<ChatPanel initialMessages={[]} resolveAnswer={resolveAnswer} />)

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'What was our margin for August 2026?{Enter}')

    const answer = await screen.findByText('Your margin for August 2026 was $8,214.50.')
    const bubble = answer.closest('li') as HTMLElement

    const compareButton = within(bubble).getByRole('button', { name: /compare to last period/i })
    await user.click(compareButton)

    await screen.findByText('July 2026 was $7,900.00 — August was up $314.50.')

    // The real, calendar-aware derived comparison question — the immediately
    // preceding calendar month, not a fixed 30-day shift — submitted through
    // the SAME resolveAnswer path as any typed question (no bypass).
    expect(resolveAnswer).toHaveBeenCalledTimes(2)
    expect(resolveAnswer.mock.calls[1][0]).toBe(
      'What was our margin for 2026-08-01 through 2026-08-31, compared to 2026-07-01 through 2026-07-31?',
    )
  })

  it('renders no "Compare to last period" button when an answer carries no resolved period', async () => {
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<
        (
          question: string,
          history: ChatMessage[],
        ) => Promise<AssistantChatMessage>
      >()
      .mockResolvedValue({
        id: 'test-answer-no-resolved-period',
        role: 'assistant',
        kind: 'answer',
        text: 'Margin on 2026-08-08 was $152.50.',
        provenance: [],
        askedAt: '2026-08-27T11:30:00Z',
      })

    render(<ChatPanel initialMessages={[]} resolveAnswer={resolveAnswer} />)

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'How did we do on 2026-08-08?{Enter}')

    const answer = await screen.findByText('Margin on 2026-08-08 was $152.50.')
    const bubble = answer.closest('li') as HTMLElement
    expect(
      within(bubble).queryByRole('button', { name: /compare to last period/i }),
    ).not.toBeInTheDocument()
  })

  it('prefills a chart click-to-ask question into the composer without submitting it', async () => {
    const resolveAnswer = vi
      .fn<
        (
          question: string,
          history: ChatMessage[],
        ) => Promise<AssistantChatMessage>
      >()
      .mockResolvedValue({
        id: 'test-answer-prefill',
        role: 'assistant',
        kind: 'answer',
        text: 'What happened on 2026-08-07 was a strong lunch rush.',
        provenance: [],
        askedAt: '2026-08-27T11:25:00Z',
      })

    render(
      <ChatPanel
        initialMessages={[]}
        resolveAnswer={resolveAnswer}
        prefillQuestion="What happened on 2026-08-07?"
      />,
    )

    // Populates the composer...
    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    expect(input).toHaveValue('What happened on 2026-08-07?')
    // ...but never submits on the owner's behalf — no answer, no user
    // message, no call to resolveAnswer, until they choose to send it.
    expect(resolveAnswer).not.toHaveBeenCalled()
    expect(
      screen.queryByText('What happened on 2026-08-07 was a strong lunch rush.'),
    ).not.toBeInTheDocument()
  })

  it('lets the owner edit and submit a prefilled chart click-to-ask question themselves', async () => {
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<
        (
          question: string,
          history: ChatMessage[],
        ) => Promise<AssistantChatMessage>
      >()
      .mockResolvedValue({
        id: 'test-answer-prefill-submitted',
        role: 'assistant',
        kind: 'answer',
        text: 'What happened on 2026-08-07 was a strong lunch rush.',
        provenance: [],
        askedAt: '2026-08-27T11:25:00Z',
      })

    render(
      <ChatPanel
        initialMessages={[]}
        resolveAnswer={resolveAnswer}
        prefillQuestion="What happened on 2026-08-07?"
      />,
    )

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, '{Enter}')

    expect(resolveAnswer).toHaveBeenCalledTimes(1)
    expect(resolveAnswer).toHaveBeenCalledWith(
      'What happened on 2026-08-07?',
      expect.any(Array),
      undefined,
      undefined,
    )
    expect(
      await screen.findByText(
        'What happened on 2026-08-07 was a strong lunch rush.',
      ),
    ).toBeInTheDocument()
  })

  // --- Spec 009: business-insight advisor ---------------------------------

  /** An answered message carrying a real teaser + the tool calls that ground it. */
  function insightAnswer(): AssistantChatMessage {
    return {
      id: 'test-answer-business-insight',
      role: 'assistant',
      kind: 'answer',
      text: 'Margin on 2026-08-03 was -$120.26.',
      provenance: [],
      toolCalls: [
        {
          name: 'get_daily_summary',
          result_json: {
            date: '2026-08-03',
            discrepancy_flags: [{ type: 'duplicate_order_removed', detail: 'dup' }],
          },
        },
      ],
      businessInsight: {
        kind: 'discrepancy_pattern',
        title: 'Recurring discrepancies may be preventable — see how',
      },
      askedAt: '2026-08-27T11:30:00Z',
    }
  }

  const adviceResponse = {
    kind: 'discrepancy_pattern',
    advice_text:
      'Restaurants in this situation typically reconcile daily and dispute invalid deductions within the platform window.',
    disclaimer:
      'AI suggestion — general industry practice connected to your computed numbers, not a computed fact about your business.',
    interaction: {
      model_used: 'claude-sonnet-5',
      input_tokens: 1420,
      output_tokens: 190,
      estimated_cost_usd: 0.00474,
      latency_ms: 2100,
    },
  }

  it('renders the insight teaser title as a labeled AI suggestion, without fetching anything on render', async () => {
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<(question: string, history: ChatMessage[]) => Promise<AssistantChatMessage>>()
      .mockResolvedValueOnce(insightAnswer())
    const resolveBusinessInsight = vi.fn()

    render(
      <ChatPanel
        initialMessages={[]}
        resolveAnswer={resolveAnswer}
        resolveBusinessInsight={resolveBusinessInsight}
      />,
    )

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'How did we do on 2026-08-03?{Enter}')

    const answer = await screen.findByText('Margin on 2026-08-03 was -$120.26.')
    const bubble = answer.closest('li') as HTMLElement

    // The teaser shows ONLY the title, explicitly labeled as a suggestion.
    expect(
      within(bubble).getByRole('button', {
        name: /recurring discrepancies may be preventable/i,
      }),
    ).toBeInTheDocument()
    expect(within(bubble).getByText('AI suggestion')).toBeInTheDocument()

    // SC-002: the full advice is NEVER auto-fetched on render.
    expect(resolveBusinessInsight).not.toHaveBeenCalled()
    expect(within(bubble).queryByText(/reconcile daily/i)).not.toBeInTheDocument()
  })

  it('renders no insight chip when the answer carries no teaser', async () => {
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<(question: string, history: ChatMessage[]) => Promise<AssistantChatMessage>>()
      .mockResolvedValueOnce({
        id: 'test-answer-no-insight',
        role: 'assistant',
        kind: 'answer',
        text: 'Margin on 2026-08-04 was $375.82.',
        provenance: [],
        askedAt: '2026-08-27T11:31:00Z',
      })

    render(
      <ChatPanel
        initialMessages={[]}
        resolveAnswer={resolveAnswer}
        resolveBusinessInsight={vi.fn()}
      />,
    )

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'How did we do on 2026-08-04?{Enter}')

    const answer = await screen.findByText('Margin on 2026-08-04 was $375.82.')
    const bubble = answer.closest('li') as HTMLElement
    expect(within(bubble).queryByText('AI suggestion')).not.toBeInTheDocument()
  })

  it('fetches the advice on tap with a visible loading state, then shows the text, disclosure, and real cost', async () => {
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<(question: string, history: ChatMessage[]) => Promise<AssistantChatMessage>>()
      .mockResolvedValueOnce(insightAnswer())
    const pending = deferred<typeof adviceResponse>()
    const resolveBusinessInsight = vi.fn().mockReturnValueOnce(pending.promise)

    render(
      <ChatPanel
        initialMessages={[]}
        resolveAnswer={resolveAnswer}
        resolveBusinessInsight={resolveBusinessInsight}
      />,
    )

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'How did we do on 2026-08-03?{Enter}')
    const answer = await screen.findByText('Margin on 2026-08-03 was -$120.26.')
    const bubble = answer.closest('li') as HTMLElement

    await user.click(
      within(bubble).getByRole('button', {
        name: /recurring discrepancies may be preventable/i,
      }),
    )

    // In-flight: a real loading state, exactly one call, carrying the
    // teaser's kind and the answer's own tool_calls back to the backend.
    expect(within(bubble).getByText(/generating the suggestion/i)).toBeInTheDocument()
    expect(resolveBusinessInsight).toHaveBeenCalledTimes(1)
    expect(resolveBusinessInsight).toHaveBeenCalledWith('discrepancy_pattern', [
      expect.objectContaining({ name: 'get_daily_summary' }),
    ])

    pending.resolve(adviceResponse)

    // Loaded: the advice text, the disclosure, and the call's REAL cost —
    // an advice call must never look free.
    expect(await within(bubble).findByText(/reconcile daily/i)).toBeInTheDocument()
    expect(within(bubble).getByText(/not a computed fact/i)).toBeInTheDocument()
    expect(within(bubble).getByText(/\$0\.005 · claude-sonnet-5/)).toBeInTheDocument()
  })

  it('re-expands already-fetched advice without a second billed call', async () => {
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<(question: string, history: ChatMessage[]) => Promise<AssistantChatMessage>>()
      .mockResolvedValueOnce(insightAnswer())
    const resolveBusinessInsight = vi.fn().mockResolvedValue(adviceResponse)

    render(
      <ChatPanel
        initialMessages={[]}
        resolveAnswer={resolveAnswer}
        resolveBusinessInsight={resolveBusinessInsight}
      />,
    )

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'How did we do on 2026-08-03?{Enter}')
    const answer = await screen.findByText('Margin on 2026-08-03 was -$120.26.')
    const bubble = answer.closest('li') as HTMLElement
    const chip = within(bubble).getByRole('button', {
      name: /recurring discrepancies may be preventable/i,
    })

    await user.click(chip)
    expect(await within(bubble).findByText(/reconcile daily/i)).toBeInTheDocument()

    // Collapse, then re-expand: the advice comes back from memory.
    await user.click(chip)
    expect(within(bubble).queryByText(/reconcile daily/i)).not.toBeInTheDocument()
    await user.click(chip)
    expect(within(bubble).getByText(/reconcile daily/i)).toBeInTheDocument()
    expect(resolveBusinessInsight).toHaveBeenCalledTimes(1)
  })

  it('shows a real error state when the advice call fails, and stays tappable to retry', async () => {
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<(question: string, history: ChatMessage[]) => Promise<AssistantChatMessage>>()
      .mockResolvedValueOnce(insightAnswer())
    const resolveBusinessInsight = vi
      .fn()
      // The real rejection `postJson` produces for this backend refusal
      // (httpapi's 502 `advice_failed`), not a bare Error — `advice_failed`
      // carries prose written for the owner, so lib/requestFailure passes it
      // through unchanged rather than replacing it with generic copy.
      .mockRejectedValueOnce(
        new ApiError('advice_failed', 'the advice call failed; please try again', 502),
      )
      .mockResolvedValueOnce(adviceResponse)

    render(
      <ChatPanel
        initialMessages={[]}
        resolveAnswer={resolveAnswer}
        resolveBusinessInsight={resolveBusinessInsight}
      />,
    )

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'How did we do on 2026-08-03?{Enter}')
    const answer = await screen.findByText('Margin on 2026-08-03 was -$120.26.')
    const bubble = answer.closest('li') as HTMLElement
    const chip = within(bubble).getByRole('button', {
      name: /recurring discrepancies may be preventable/i,
    })

    await user.click(chip)
    expect(
      await within(bubble).findByText(/the advice call failed/i),
    ).toBeInTheDocument()

    // The failure is recoverable: tapping again retries for real.
    await user.click(chip)
    expect(await within(bubble).findByText(/reconcile daily/i)).toBeInTheDocument()
    expect(resolveBusinessInsight).toHaveBeenCalledTimes(2)
  })
})

// Bug fix: the "Recent conversations" history panel's per-thread message
// count had no singular branch (`${thread.messages.length} messages`), so a
// one-message thread read "1 messages".
describe('ChatPanel — thread history message-count pluralization (bug fix)', () => {
  const THREADS_KEY = 'mbs.chat.threads.v2'

  beforeEach(() => {
    window.localStorage.clear()
  })

  function userMessage(id: string, text: string): ChatMessage {
    return { id, role: 'user', text, askedAt: '2026-08-27T10:00:00Z' }
  }

  it('reads "1 message" (not "1 messages") for a one-message thread, and "N messages" otherwise', async () => {
    const user = userEvent.setup()
    window.localStorage.setItem(
      THREADS_KEY,
      JSON.stringify({
        activeId: 'thread-active',
        threads: [
          {
            id: 'thread-active',
            title: 'Active thread',
            updatedAt: '2026-08-27T10:00:00Z',
            messages: [userMessage('m1', 'How did today close?')],
          },
          {
            id: 'thread-one',
            title: 'One-message thread',
            updatedAt: '2026-08-26T10:00:00Z',
            messages: [userMessage('m2', 'What changed this week?')],
          },
          {
            id: 'thread-three',
            title: 'Three-message thread',
            updatedAt: '2026-08-25T10:00:00Z',
            messages: [
              userMessage('m3', 'a'),
              userMessage('m4', 'b'),
              userMessage('m5', 'c'),
            ],
          },
        ],
      }),
    )

    render(<ChatPanel persistConversation />)

    await user.click(screen.getByRole('button', { name: /recent/i }))
    const list = screen.getByRole('list', { name: /recent conversations/i })

    expect(within(list).getByText('One-message thread')).toBeInTheDocument()
    expect(within(list).getByText('1 message')).toBeInTheDocument()
    expect(within(list).queryByText('1 messages')).not.toBeInTheDocument()

    expect(within(list).getByText('Three-message thread')).toBeInTheDocument()
    expect(within(list).getByText('3 messages')).toBeInTheDocument()
  })
})

/**
 * The three defects the state-persistence QA pass found were one defect:
 * React state was treated as the source of truth and storage as a mirror of
 * it. These exercise each reported repro against the inverted model.
 */
describe('ChatPanel durable conversation state', () => {
  beforeEach(() => {
    window.localStorage.clear()
  })

  it('keeps an answer that resolves after the panel has unmounted', async () => {
    const user = userEvent.setup()
    const { promise, resolve } = deferred<AssistantChatMessage>()
    const resolveAnswer = vi
      .fn<(question: string, history: ChatMessage[]) => Promise<AssistantChatMessage>>()
      .mockReturnValue(promise)

    const view = render(<ChatPanel persistConversation resolveAnswer={resolveAnswer} />)

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'How did we do yesterday?{Enter}')
    expect(screen.getByText(/checking the reconciled numbers/i)).toBeInTheDocument()

    // The reported repro: navigate away (or reload) while the request is in
    // flight. The backend completes it regardless, and bills for it.
    view.unmount()

    await act(async () => {
      resolve({
        id: 'ignored-resolver-id',
        role: 'assistant',
        kind: 'answer',
        text: 'Margin for that period was $1,842.60.',
        provenance: [],
        interactions: [
          {
            model_used: 'claude-sonnet-5',
            input_tokens: 1180,
            output_tokens: 240,
            estimated_cost_usd: 0.00476,
            latency_ms: 1420,
          },
        ],
        askedAt: '2026-08-27T10:00:02Z',
      })
      await promise
    })

    // The answer must be in storage even though nothing was mounted to
    // receive it, and it must have replaced the pending record rather than
    // being appended beside it.
    await waitFor(() => {
      const messages = persistedMessages()
      expect(messages).toHaveLength(2)
      expect(messages[1]).toMatchObject({
        kind: 'answer',
        text: 'Margin for that period was $1,842.60.',
      })
    })

    // And the spend it incurred was recorded, not billed invisibly.
    expect(loadSpendLedger()).toHaveLength(1)

    // Coming back to the page shows the real answer, not a "lost" placeholder.
    render(<ChatPanel persistConversation resolveAnswer={resolveAnswer} />)
    expect(
      await screen.findByText('Margin for that period was $1,842.60.'),
    ).toBeInTheDocument()
  })

  it('turns a question orphaned by a reload into an honest, retryable state instead of silent limbo', async () => {
    // What storage looks like after a reload interrupted a live request: the
    // question is there, the pending record is there, no answer ever came.
    window.localStorage.setItem(
      THREADS_KEY,
      JSON.stringify({
        activeId: 't-reload',
        threads: [
          {
            id: 't-reload',
            title: 'How did we do yesterday?',
            updatedAt: '2026-08-27T10:00:01Z',
            messages: [
              {
                id: 'u-1',
                role: 'user',
                text: 'How did we do yesterday?',
                askedAt: '2026-08-27T10:00:00Z',
              },
              {
                id: 'a-1',
                role: 'assistant',
                kind: 'pending',
                question: 'How did we do yesterday?',
                askedAt: '2026-08-27T10:00:01Z',
              },
            ],
          },
        ],
      }),
    )

    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<(question: string, history: ChatMessage[]) => Promise<AssistantChatMessage>>()
      .mockResolvedValue({
        id: 'retry-answer',
        role: 'assistant',
        kind: 'answer',
        text: 'Answer after retry.',
        provenance: [],
        askedAt: '2026-08-27T10:05:00Z',
      })

    render(<ChatPanel persistConversation resolveAnswer={resolveAnswer} />)

    // Not a spinner that never stops, and not nothing at all.
    expect(screen.queryByText(/checking the reconciled numbers/i)).not.toBeInTheDocument()
    expect(screen.getByText(/this answer never made it back to you/i)).toBeInTheDocument()
    // Honest about the money, per this project's instrumentation principle:
    // the request very likely ran, so it very likely cost something.
    expect(
      screen.getByText(/may already be counted in the running model-spend total/i),
    ).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /try again/i }))

    expect(await screen.findByText('Answer after retry.')).toBeInTheDocument()
    expect(resolveAnswer).toHaveBeenCalledWith(
      'How did we do yesterday?',
      expect.anything(),
      undefined,
      undefined,
    )
  })

  it('merges another tab\'s write instead of overwriting it, in both directions', async () => {
    const user = userEvent.setup()
    const resolveAnswer = vi
      .fn<(question: string, history: ChatMessage[]) => Promise<AssistantChatMessage>>()
      .mockResolvedValue({
        id: 'tab-one-answer',
        role: 'assistant',
        kind: 'answer',
        text: 'Tab one answer.',
        provenance: [],
        askedAt: '2026-08-27T10:00:02Z',
      })

    render(<ChatPanel persistConversation resolveAnswer={resolveAnswer} />)

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'Tab one question{Enter}')
    expect(await screen.findByText('Tab one answer.')).toBeInTheDocument()

    // Another tab asks its own question against the same thread.
    act(() => {
      simulateOtherTabWrite((store) => ({
        ...store,
        threads: store.threads.map((thread) =>
          thread.id === store.activeId
            ? {
                ...thread,
                messages: [
                  ...thread.messages,
                  {
                    id: 'other-tab-question',
                    role: 'user',
                    text: 'Tab two question',
                    askedAt: '2026-08-27T10:01:00Z',
                  },
                ],
              }
            : thread,
        ),
      }))
    })

    // This tab absorbs it rather than ignoring it...
    expect(await screen.findByText('Tab two question')).toBeInTheDocument()
    // ...and its own history is untouched.
    expect(screen.getByText('Tab one question')).toBeInTheDocument()

    // ...and this tab's NEXT write preserves the other tab's message rather
    // than writing back its own mount-time snapshot over it.
    await user.type(input, 'Tab one follow-up{Enter}')
    await waitFor(() => {
      expect(persistedMessages().map((message) => message.id)).toContain(
        'other-tab-question',
      )
    })
    const texts = persistedMessages().map((message) =>
      message.role === 'user' ? message.text : '',
    )
    expect(texts).toContain('Tab one question')
    expect(texts).toContain('Tab two question')
    expect(texts).toContain('Tab one follow-up')
  })

  /**
   * Found by driving a real browser, not by reading the code: reloading
   * mid-request makes the browser abort the fetch, and that rejection is
   * delivered to the catch block BEFORE the page tears down. Reported as a
   * transport failure it said something false ("I couldn't reach your data"
   * — the request had already been sent, and the backend goes on to complete
   * and bill it) and, worse, it overwrote the pending record that the next
   * page load needs in order to recognise the interruption at all.
   */
  it('does not report a fetch aborted by page teardown as a transport failure', async () => {
    const user = userEvent.setup()
    let rejectRequest!: (reason: Error) => void
    const request = new Promise<AssistantChatMessage>((_, reject) => {
      rejectRequest = reject
    })

    const resolveAnswer = vi
      .fn<(question: string, history: ChatMessage[]) => Promise<AssistantChatMessage>>()
      .mockReturnValue(request)

    const view = render(<ChatPanel persistConversation resolveAnswer={resolveAnswer} />)

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'How did we do last week?{Enter}')
    expect(screen.getByText(/checking the reconciled numbers/i)).toBeInTheDocument()

    // The browser announces the teardown, then cancels the request.
    window.dispatchEvent(new Event('beforeunload'))
    await act(async () => {
      rejectRequest(new TypeError('Failed to fetch'))
      await request.catch(() => undefined)
    })

    // The pending record must survive: it is the only evidence the next load
    // has that a question was asked and never answered.
    const stored = persistedMessages()
    expect(stored[1]).toMatchObject({ kind: 'pending' })

    view.unmount()
    // The document is alive again (cancelled navigation / bfcache restore),
    // so later failures are once more real transport failures.
    window.dispatchEvent(new Event('pageshow'))

    // What the reader sees after the reload actually completes.
    render(<ChatPanel persistConversation resolveAnswer={resolveAnswer} />)
    expect(
      screen.getByText(/this answer never made it back to you/i),
    ).toBeInTheDocument()
    expect(
      screen.queryByText(/couldn't reach your data just now/i),
    ).not.toBeInTheDocument()
  })
})
