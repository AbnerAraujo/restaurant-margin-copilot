import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { ChatMessage } from '@/components/Chat/ChatPanel'
import {
  MAX_THREADS,
  activeThread,
  addSavedPrompt,
  deriveThreadTitle,
  loadSavedPrompts,
  loadThreadStore,
  openThread,
  persistActiveThread,
  removeSavedPrompt,
  startNewThread,
} from './chatStorage'

function userMessage(text: string): ChatMessage {
  return { id: text, role: 'user', text, askedAt: '2026-08-27T10:00:00Z' }
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
