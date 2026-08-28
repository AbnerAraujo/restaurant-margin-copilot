import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeAll, describe, expect, it, vi } from 'vitest'

import ChatPanel, {
  derivePendingClarification,
  derivePreviousExchange,
  type AssistantChatMessage,
  type ChatMessage,
} from './ChatPanel'

// jsdom has no ResizeObserver; Radix's ScrollArea needs one to mount. This
// stub is local to this file rather than the shared test setup so it stays
// scoped to the one component that pulls in ScrollArea.
beforeAll(() => {
  if (!('ResizeObserver' in globalThis)) {
    class ResizeObserverStub {
      observe() {}
      unobserve() {}
      disconnect() {}
    }
    globalThis.ResizeObserver = ResizeObserverStub
  }
})

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
    // its "N sources" form rather than a single inline citation.
    expect(
      screen.getByRole('button', { name: '2 sources' }),
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

    expect(refusalBanner?.className).toContain('border-destructive/25')
    expect(clarificationBanner?.className).toContain('border-warning/25')
    expect(refusalBanner?.className).not.toBe(clarificationBanner?.className)
  })

  it('expands a provenance citation to reveal its individual source rows, and hides it again', async () => {
    const user = userEvent.setup()
    render(<ChatPanel />)

    const citation = screen.getByRole('button', { name: '2 sources' })
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
    expect(
      screen.getByText('/api/ask returned 502: upstream down'),
    ).toBeInTheDocument()
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
})
