import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { ChatMessage } from '@/components/Chat/ChatPanel'
import {
  MAX_THREADS,
  activeThread,
  addSavedPrompt,
  clearRequestInFlight,
  clearThreadStorage,
  commitThreadMessages,
  deriveThreadTitle,
  loadSavedPrompts,
  loadSpendLedger,
  loadThreadStore,
  markRequestInFlight,
  mergeThreadStores,
  openThread,
  persistActiveThread,
  reconcileInterruptedMessages,
  recordSpend,
  removeSavedPrompt,
  replaceMessage,
  startNewThread,
  subscribeToThreadStore,
} from './chatStorage'

function userMessage(text: string): ChatMessage {
  return { id: text, role: 'user', text, askedAt: '2026-08-27T10:00:00Z' }
}

function pendingMessage(id: string, question: string): ChatMessage {
  return {
    id,
    role: 'assistant',
    kind: 'pending',
    question,
    askedAt: '2026-08-27T10:00:01Z',
  }
}

function answerMessage(id: string, text: string): ChatMessage {
  return {
    id,
    role: 'assistant',
    kind: 'answer',
    text,
    provenance: [],
    askedAt: '2026-08-27T10:00:02Z',
  }
}

describe('chatStorage', () => {
  beforeEach(() => {
    window.localStorage.clear()
  })

  it('starts with a single empty thread when nothing is stored', () => {
    const store = loadThreadStore()
    expect(store.threads).toHaveLength(1)
    expect(activeThread(store)?.messages).toEqual([])
  })

  it('round-trips the active thread through storage', () => {
    const store = persistActiveThread(loadThreadStore(), [
      userMessage('How did we do on 2026-08-07?'),
    ])
    expect(activeThread(store)?.messages).toHaveLength(1)

    // A fresh load — what a page reload does.
    const reloaded = loadThreadStore()
    expect(activeThread(reloaded)?.messages).toEqual([
      userMessage('How did we do on 2026-08-07?'),
    ])
    expect(reloaded.activeId).toBe(store.activeId)
  })

  it('archives the current thread when a new one starts, and can reopen it', () => {
    let store = persistActiveThread(loadThreadStore(), [userMessage('first thread')])
    const firstId = store.activeId

    store = startNewThread(store)
    expect(store.activeId).not.toBe(firstId)
    expect(activeThread(store)?.messages).toEqual([])
    expect(store.threads).toHaveLength(2)

    store = openThread(store, firstId)
    expect(activeThread(store)?.messages).toEqual([userMessage('first thread')])
  })

  it('reuses an already-empty thread instead of stacking blank ones', () => {
    const store = loadThreadStore()
    const again = startNewThread(startNewThread(store))
    expect(again.threads).toHaveLength(1)
  })

  it('caps the thread history so storage cannot grow without bound', () => {
    let store = loadThreadStore()
    for (let i = 0; i < MAX_THREADS + 4; i++) {
      store = persistActiveThread(store, [userMessage(`thread ${i}`)])
      store = startNewThread(store)
    }
    expect(store.threads.length).toBeLessThanOrEqual(MAX_THREADS)
  })

  it('derives a thread title from the first question, never from a model', () => {
    expect(deriveThreadTitle([])).toBe('New chat')
    expect(deriveThreadTitle([userMessage('Which promotions lost money?')])).toBe(
      'Which promotions lost money?',
    )
    const long = deriveThreadTitle([
      userMessage(
        'Compare total margin for 2026-08-01 to 2026-08-07 against the following week',
      ),
    ])
    expect(long.length).toBeLessThanOrEqual(43)
    expect(long.endsWith('…')).toBe(true)
  })

  it('clearThreadStorage wipes thread history so the next load starts fresh, but leaves saved prompts alone', () => {
    persistActiveThread(loadThreadStore(), [userMessage('a question that later broke rendering')])
    addSavedPrompt(loadSavedPrompts(), 'a prompt worth keeping')

    clearThreadStorage()

    const reloaded = loadThreadStore()
    expect(reloaded.threads).toHaveLength(1)
    expect(activeThread(reloaded)?.messages).toEqual([])
    expect(loadSavedPrompts().map((p) => p.text)).toContain('a prompt worth keeping')
  })

  it('survives a corrupted or hand-edited storage key rather than crashing', () => {
    window.localStorage.setItem('mbs.chat.threads.v1', '{not json')
    expect(() => loadThreadStore()).not.toThrow()
    expect(loadThreadStore().threads).toHaveLength(1)

    window.localStorage.setItem('mbs.chat.threads.v1', '{"threads":[{"nope":1}]}')
    expect(loadThreadStore().threads).toHaveLength(1)
  })

  it('never throws when storage itself is unavailable (private mode, quota)', () => {
    const setItem = vi
      .spyOn(Storage.prototype, 'setItem')
      .mockImplementation(() => {
        throw new Error('QuotaExceededError')
      })
    expect(() =>
      persistActiveThread(loadThreadStore(), [userMessage('x')]),
    ).not.toThrow()
    setItem.mockRestore()
  })

  it('saves, reloads and deletes reusable prompts, ignoring blanks and duplicates', () => {
    let prompts = addSavedPrompt([], 'Which promotions lost money?')
    expect(prompts).toHaveLength(1)

    prompts = addSavedPrompt(prompts, '   ')
    prompts = addSavedPrompt(prompts, 'Which promotions lost money?')
    expect(prompts).toHaveLength(1)

    expect(loadSavedPrompts()).toHaveLength(1)

    prompts = removeSavedPrompt(prompts, prompts[0].id)
    expect(prompts).toHaveLength(0)
    expect(loadSavedPrompts()).toHaveLength(0)
  })
})

describe('chatStorage cross-tab reconciliation', () => {
  beforeEach(() => {
    window.localStorage.clear()
  })

  /**
   * The exact reported repro, at the layer where it happened. Two tabs load
   * the same thread; each then writes. The old code wrote each tab's whole
   * mount-time snapshot, so tab 2's write erased tab 1's question outright.
   */
  it('does not let a second tab with a stale snapshot destroy the first tab\'s history', () => {
    // Both tabs mount against the same, initially empty store.
    const tabOne = loadThreadStore()
    persistActiveThread(tabOne, [])
    const threadId = tabOne.activeId
    const tabTwo = loadThreadStore()
    expect(tabTwo.activeId).toBe(threadId)

    // Tab 1 asks. Tab 2 knows nothing about it — its `tabTwo` snapshot is now
    // stale, which is precisely the state that used to be fatal.
    commitThreadMessages(threadId, (messages) => [...messages, userMessage('from tab one')])

    // Tab 2 asks, committing against its OWN stale snapshot.
    commitThreadMessages(
      threadId,
      (messages) => [...messages, userMessage('from tab two')],
      tabTwo,
    )

    // What a reload of either tab now sees.
    const reloaded = activeThread(loadThreadStore())?.messages ?? []
    expect(reloaded.map((message) => message.id)).toEqual([
      'from tab one',
      'from tab two',
    ])
  })

  it('merges two divergent stores without dropping either side, keeping this tab on its own thread', () => {
    const shared = loadThreadStore()
    const threadId = shared.activeId

    const tabOne = {
      activeId: threadId,
      threads: [
        {
          id: threadId,
          title: 'x',
          updatedAt: '2026-08-27T10:00:00Z',
          messages: [userMessage('q1'), answerMessage('a1', 'answer one')],
        },
      ],
    }
    const tabTwo = {
      activeId: 'other-thread',
      threads: [
        {
          id: threadId,
          title: 'x',
          updatedAt: '2026-08-27T10:05:00Z',
          messages: [userMessage('q1'), userMessage('q2')],
        },
        {
          id: 'other-thread',
          title: 'y',
          updatedAt: '2026-08-27T10:06:00Z',
          messages: [userMessage('q3')],
        },
      ],
    }

    const merged = mergeThreadStores(tabTwo, tabOne, threadId)

    expect(merged.threads.map((thread) => thread.id)).toEqual([
      threadId,
      'other-thread',
    ])
    expect(merged.threads[0].messages.map((message) => message.id)).toEqual([
      'q1',
      'q2',
      'a1',
    ])
    // `activeId` is per-tab view state: a background tab must keep looking at
    // the thread its own reader chose.
    expect(merged.activeId).toBe(threadId)
  })

  it('notifies subscribers with what was actually written, not what was asked for', () => {
    const store = loadThreadStore()
    const seen: string[][] = []
    const unsubscribe = subscribeToThreadStore((next) => {
      seen.push((activeThread(next)?.messages ?? []).map((message) => message.id))
    })

    commitThreadMessages(store.activeId, () => [userMessage('q1')], store)
    commitThreadMessages(store.activeId, (messages) => [...messages, userMessage('q2')])

    unsubscribe()
    commitThreadMessages(store.activeId, (messages) => [...messages, userMessage('q3')])

    expect(seen).toEqual([['q1'], ['q1', 'q2']])
  })
})

describe('chatStorage interrupted-request recovery', () => {
  const inFlightIds: string[] = []

  beforeEach(() => {
    window.localStorage.clear()
  })

  afterEach(() => {
    for (const id of inFlightIds.splice(0)) clearRequestInFlight(id)
  })

  it('resolves a question left pending by a previous page load into a retryable state', () => {
    const store = loadThreadStore()
    commitThreadMessages(
      store.activeId,
      () => [
        userMessage('How did we do yesterday?'),
        pendingMessage('p1', 'How did we do yesterday?'),
      ],
      store,
    )

    // A fresh page load: nothing is in flight in THIS document.
    const reconciled = reconcileInterruptedMessages()
    const messages = activeThread(reconciled)?.messages ?? []

    expect(messages).toHaveLength(2)
    expect(messages[1]).toMatchObject({
      id: 'p1',
      kind: 'error',
      cause: 'interrupted',
      question: 'How did we do yesterday?',
    })
    // Persisted, not merely returned — the next load must not re-derive it.
    expect(activeThread(loadThreadStore())?.messages[1]).toMatchObject({
      kind: 'error',
      cause: 'interrupted',
    })
  })

  it('leaves a request this document is still waiting on alone', () => {
    const store = loadThreadStore()
    markRequestInFlight('p2')
    inFlightIds.push('p2')
    commitThreadMessages(store.activeId, () => [pendingMessage('p2', 'still running')], store)

    const messages = activeThread(reconcileInterruptedMessages())?.messages ?? []
    expect(messages[0]).toMatchObject({ kind: 'pending' })
  })

  /**
   * The self-healing property the eager reconciliation depends on: another
   * tab may mark a request "lost" while the tab that owns it is still
   * waiting, so a real verdict has to outrank that guess.
   */
  it('lets a real answer supersede an interrupted marker for the same question', () => {
    const interrupted: ChatMessage = {
      id: 'p3',
      role: 'assistant',
      kind: 'error',
      cause: 'interrupted',
      text: 'lost',
      question: 'q',
      askedAt: '2026-08-27T10:00:01Z',
    }
    const withAnswer = replaceMessage([interrupted], 'p3', answerMessage('p3', 'the real answer'))
    expect(withAnswer[0]).toMatchObject({ kind: 'answer', text: 'the real answer' })

    // And never the other way round.
    const stillAnswered = replaceMessage(withAnswer, 'p3', interrupted)
    expect(stillAnswered[0]).toMatchObject({ kind: 'answer' })
  })
})

describe('chatStorage spend ledger', () => {
  const gate = {
    model_used: 'claude-sonnet-5',
    input_tokens: 420,
    output_tokens: 18,
    estimated_cost_usd: 0.00051,
    latency_ms: 310,
  }
  const explain = {
    model_used: 'claude-sonnet-5',
    input_tokens: 1180,
    output_tokens: 240,
    estimated_cost_usd: 0.00476,
    latency_ms: 1420,
  }

  beforeEach(() => {
    window.localStorage.clear()
  })

  it('survives a reload, so the total matches the answers still on screen', () => {
    recordSpend('t1', 'a1', [gate, explain])

    // A fresh load — what a page reload does.
    const total = loadSpendLedger().reduce(
      (sum, entry) => sum + entry.estimated_cost_usd,
      0,
    )
    expect(total).toBeCloseTo(gate.estimated_cost_usd + explain.estimated_cost_usd, 8)
  })

  it('never double-counts a replayed commit', () => {
    recordSpend('t1', 'a1', [gate, explain])
    recordSpend('t1', 'a1', [gate, explain])
    expect(loadSpendLedger()).toHaveLength(2)
  })

  it('keeps spend when the thread that earned it is evicted, so the total never drops', () => {
    recordSpend('t1', 'a1', [explain])
    let store = loadThreadStore()
    for (let i = 0; i < MAX_THREADS + 4; i++) {
      store = persistActiveThread(store, [userMessage(`thread ${i}`)])
      store = startNewThread(store)
    }
    expect(store.threads.length).toBeLessThanOrEqual(MAX_THREADS)
    expect(loadSpendLedger()).toHaveLength(1)
  })

  it('is left alone by the chat-crash reset, which must never hide real spend', () => {
    recordSpend('t1', 'a1', [gate])
    clearThreadStorage()
    expect(loadSpendLedger()).toHaveLength(1)
  })

  /**
   * The regression this guards against, at the layer that actually lost
   * money: `ChatPanel`/`AskPage` used to mint message ids from a
   * module-level counter that reset to 0 on every reload, so the first
   * question asked after a reload got the exact same id
   * (`assistant-1`) as the first question asked before it. `recordSpend`'s
   * dedupe-by-id safeguard — a deliberate, documented anti-double-billing
   * feature — then had no way to tell "a retried commit for the same
   * answer" from "a genuinely new, separately billed answer that happens
   * to share an id", and silently dropped the second question's cost.
   *
   * This test documents that the dedupe itself is still correct and
   * intentional (colliding ids ARE treated as one entry) — the fix is that
   * production code must never hand it a colliding id for two different
   * questions again. See `createUniqueId` (`lib/id.ts`) and its test for
   * the other half of the guarantee: real message ids never collide like
   * this across a reload.
   */
  it('would silently drop a second question\'s cost if given the same id twice (documents why ids must never collide)', () => {
    recordSpend('t1', 'assistant-1', [gate]) // "before the reload"
    recordSpend('t1', 'assistant-1', [explain]) // "after the reload" — colliding id

    expect(loadSpendLedger()).toHaveLength(1)
    const total = loadSpendLedger().reduce(
      (sum, entry) => sum + entry.estimated_cost_usd,
      0,
    )
    // The bug, made concrete: only the gate's cost is on the books. The
    // explanation call that answered the second, real question was billed
    // by the backend but never shows up here.
    expect(total).toBeCloseTo(gate.estimated_cost_usd, 8)
  })

  /**
   * The outcome that actually matters to the reader: ask a question,
   * reload, ask a DIFFERENT question — the second question's spend must
   * show up too. `createUniqueId()` stands in for the real id each
   * component now generates via `nextMessageId`; unlike the old counter, it
   * is guaranteed not to repeat across a reload (see `lib/id.test.ts`), so
   * this is exercising the real fix end to end at the ledger layer.
   */
  it('records the second question\'s spend after a reload, because its id no longer collides with the first', async () => {
    const { createUniqueId } = await import('./id')

    const beforeReloadId = createUniqueId()
    recordSpend('t1', beforeReloadId, [gate])

    // Simulates the reload: a fresh id, exactly as `nextMessageId` would
    // hand a newly-asked question after the page comes back up.
    const afterReloadId = createUniqueId()
    recordSpend('t1', afterReloadId, [explain])

    const entries = loadSpendLedger()
    expect(entries).toHaveLength(2)
    const total = entries.reduce((sum, entry) => sum + entry.estimated_cost_usd, 0)
    expect(total).toBeCloseTo(gate.estimated_cost_usd + explain.estimated_cost_usd, 8)
  })
})
