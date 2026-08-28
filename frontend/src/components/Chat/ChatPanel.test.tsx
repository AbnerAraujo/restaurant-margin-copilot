import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeAll, describe, expect, it, vi } from 'vitest'

import ChatPanel, {
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

describe('ChatPanel', () => {
  it('renders the seeded conversation with a grounded answer and its provenance citations', () => {
    render(<ChatPanel />)

    expect(
      screen.getByText(/today's reconciled margin was \$612\.40/i),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: /daily reconciliation · aug 27/i }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: /pos export · rows 1–42/i }),
    ).toBeInTheDocument()
  })

  it('renders a clarification response with a visibly distinct banner and quick-reply options', () => {
    render(<ChatPanel />)

    const clarificationLabel = screen.getByText(/needs a quick clarification/i)
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

    const refusalLabel = screen.getByText(/can't answer this one/i)
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
    // a refusal never carries a provenance citation (data-model.md: refusal
    // implies provenance_refs = [])
    expect(withinRefusal.queryAllByRole('button')).toHaveLength(0)
  })

  it('renders the refusal and clarification banners with different visual treatments', () => {
    render(<ChatPanel />)

    const refusalBanner = screen
      .getByText(/can't answer this one/i)
      .closest('div')
    const clarificationBanner = screen
      .getByText(/needs a quick clarification/i)
      .closest('div')

    expect(refusalBanner?.className).toContain('border-destructive/25')
    expect(clarificationBanner?.className).toContain('border-warning/25')
    expect(refusalBanner?.className).not.toBe(clarificationBanner?.className)
  })

  it('expands a provenance citation to reveal its detail on click, and hides it again', async () => {
    const user = userEvent.setup()
    render(<ChatPanel />)

    const citation = screen.getByRole('button', {
      name: /daily reconciliation · aug 27/i,
    })
    expect(
      screen.queryByText(/computed from 3 source files, 0 discrepancy/i),
    ).not.toBeInTheDocument()

    await user.click(citation)
    expect(
      screen.getByText(/computed from 3 source files, 0 discrepancy/i),
    ).toBeInTheDocument()

    await user.click(citation)
    expect(
      screen.queryByText(/computed from 3 source files, 0 discrepancy/i),
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
      provenance: [{ label: 'Daily reconciliation · Aug 26' }],
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
    expect(resolveAnswer).toHaveBeenCalledWith('Option A', expect.any(Array))
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
