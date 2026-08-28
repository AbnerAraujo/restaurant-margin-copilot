import type { ChatMessage } from '@/components/Chat/ChatPanel'

/**
 * Client-side persistence for the chat: the current thread, a short history
 * of previous ones, and reusable saved prompts.
 *
 * `localStorage`, deliberately, not Postgres. `question_interaction` is an
 * instrumentation/audit log — one row per model call, written for cost and
 * refusal accounting — not a resumable conversation store, and reshaping it
 * into one would mean inventing a session/ownership model this build has no
 * auth to hang off (the PRD scopes multi-tenant out). For a single-user
 * prototype the browser is the correct owner of "what I was looking at".
 *
 * Every access is wrapped: Safari private mode throws on setItem, storage
 * can be full, and a user can have hand-edited the key. None of that is
 * worth failing a chat over, so a storage failure degrades to "no history"
 * rather than a broken page.
 */

// Independently versioned: a thread stores whole `ChatMessage` objects, whose
// shape has changed more than once since persistence was first added
// (ErrorChatMessage, AnswerCacheInfo, and other fields landed without a
// version bump) — a browser that kept an old-shape thread across those
// changes could hand the current renderer an object missing fields it now
// assumes exist. Saved prompts are plain strings and never changed shape, so
// they don't need to be invalidated by a threads-only bump.
const THREADS_VERSION = 2
const PROMPTS_VERSION = 1
const THREADS_KEY = `mbs.chat.threads.v${THREADS_VERSION}`
const PROMPTS_KEY = `mbs.chat.prompts.v${PROMPTS_VERSION}`

/** How many threads are kept in total, including the active one. */
export const MAX_THREADS = 6
/** How many saved prompts are kept. */
export const MAX_SAVED_PROMPTS = 12

export interface StoredThread {
  id: string
  /** Derived from the first user message; never model-generated. */
  title: string
  updatedAt: string
  messages: ChatMessage[]
}

export interface ThreadStore {
  activeId: string
  threads: StoredThread[]
}

export interface SavedPrompt {
  id: string
  text: string
  savedAt: string
}

function readJSON<T>(key: string): T | null {
  try {
    const raw = window.localStorage.getItem(key)
    return raw ? (JSON.parse(raw) as T) : null
  } catch {
    return null
  }
}

function writeJSON(key: string, value: unknown): void {
  try {
    window.localStorage.setItem(key, JSON.stringify(value))
  } catch {
    // Quota exceeded, private mode, or storage disabled. History is a
    // convenience; losing it must never interrupt the conversation.
  }
}

/**
 * Wipes persisted thread history so the next `loadThreadStore()` starts
 * completely fresh, per `ErrorBoundary`'s `onReset` — the real incident this
 * module's doc comment describes (a stale, pre-schema-change message
 * crashing the renderer) means clicking "Reset" on the chat page must clear
 * THIS key, not just the component's own error state, or the boundary
 * re-reads the same poisoned thread and crashes again immediately.
 *
 * Deliberately leaves `PROMPTS_KEY` alone: saved prompts are plain strings
 * that never change shape (see `THREADS_VERSION`'s comment above), so they
 * are never the cause of this class of crash, and wiping a user's saved
 * prompts on an unrelated chat-render failure would be its own new defect.
 */
export function clearThreadStorage(): void {
  try {
    window.localStorage.removeItem(THREADS_KEY)
  } catch {
    // Same "never fail harder than the thing we're recovering from" rule as
    // writeJSON above.
  }
}

/**
 * Per-message shape check, not just per-thread. This is the specific gap
 * that let a real bug through once already: `ChatMessage`'s shape changed
 * more than once (ErrorChatMessage added, AnswerCacheInfo added) without a
 * `THREADS_VERSION` bump at the time, so a browser that kept using the app
 * across those changes could have an old-shape message sitting in storage
 * that the current renderer assumes has fields it doesn't. `THREADS_VERSION`
 * is now bumped for that specific incident, but this function is the
 * general-purpose defense for the next time a shape changes and a version
 * bump is missed — belt and suspenders, not a substitute for versioning.
 */
function isWellFormedMessage(message: unknown): message is ChatMessage {
  if (!message || typeof message !== 'object') return false
  const m = message as Record<string, unknown>
  if (typeof m.id !== 'string' || typeof m.askedAt !== 'string') return false
  if (m.role === 'user') return typeof m.text === 'string'
  if (m.role !== 'assistant') return false
  switch (m.kind) {
    case 'answer':
      return typeof m.text === 'string' && Array.isArray(m.provenance)
    case 'clarification':
      return typeof m.text === 'string'
    case 'refusal':
      return typeof m.text === 'string' && Array.isArray(m.missing)
    case 'error':
      return typeof m.text === 'string' && typeof m.question === 'string'
    default:
      return false
  }
}

function newId(): string {
  return `t-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`
}

/**
 * Titles a thread from its first user message. Truncated at a word boundary
 * where possible so a title reads as a phrase rather than a cut-off token.
 * Deliberately not asked of the model: a title is not worth an API call, and
 * a summarized title can misdescribe what the thread actually contains.
 */
export function deriveThreadTitle(messages: ChatMessage[]): string {
  const firstUser = messages.find((message) => message.role === 'user')
  if (!firstUser) return 'New chat'
  const text = firstUser.text.trim().replace(/\s+/g, ' ')
  if (text.length <= 42) return text
  const cut = text.slice(0, 42)
  const lastSpace = cut.lastIndexOf(' ')
  return `${lastSpace > 20 ? cut.slice(0, lastSpace) : cut}…`
}

export function loadThreadStore(): ThreadStore {
  const stored = readJSON<ThreadStore>(THREADS_KEY)
  if (!stored || !Array.isArray(stored.threads) || stored.threads.length === 0) {
    const id = newId()
    return {
      activeId: id,
      threads: [{ id, title: 'New chat', updatedAt: new Date().toISOString(), messages: [] }],
    }
  }
  // A hand-edited or partially-written key must not crash the page: anything
  // that isn't a well-formed thread is dropped rather than trusted. This
  // checks the thread shell (id, messages array) but NOT each message's own
  // shape — see isWellFormedMessage below for why that second layer matters
  // just as much: a thread can be well-formed while one message inside it
  // is a stale, pre-schema-change object that would still crash the renderer.
  const threads = stored.threads
    .filter(
      (thread): thread is StoredThread =>
        Boolean(thread) &&
        typeof thread.id === 'string' &&
        Array.isArray(thread.messages),
    )
    .map((thread) => ({
      ...thread,
      messages: thread.messages.filter(isWellFormedMessage),
    }))
  if (threads.length === 0) return loadThreadStoreFresh()
  const activeId = threads.some((t) => t.id === stored.activeId)
    ? stored.activeId
    : threads[0].id
  return { activeId, threads }
}

function loadThreadStoreFresh(): ThreadStore {
  const id = newId()
  return {
    activeId: id,
    threads: [{ id, title: 'New chat', updatedAt: new Date().toISOString(), messages: [] }],
  }
}

/** Replaces the active thread's messages and persists the whole store. */
export function persistActiveThread(
  store: ThreadStore,
  messages: ChatMessage[],
): ThreadStore {
  const next: ThreadStore = {
    activeId: store.activeId,
    threads: store.threads.map((thread) =>
      thread.id === store.activeId
        ? {
            ...thread,
            title: deriveThreadTitle(messages),
            updatedAt: new Date().toISOString(),
            messages,
          }
        : thread,
    ),
  }
  writeJSON(THREADS_KEY, next)
  return next
}

/**
 * Archives the current thread and opens a fresh one.
 *
 * An empty active thread is reused rather than archived — otherwise pressing
 * "New chat" twice would leave a trail of blank threads in the history list.
 */
export function startNewThread(store: ThreadStore): ThreadStore {
  const active = store.threads.find((thread) => thread.id === store.activeId)
  if (active && active.messages.length === 0) return store

  const id = newId()
  const next: ThreadStore = {
    activeId: id,
    threads: [
      { id, title: 'New chat', updatedAt: new Date().toISOString(), messages: [] },
      ...store.threads,
    ].slice(0, MAX_THREADS),
  }
  writeJSON(THREADS_KEY, next)
  return next
}

export function openThread(store: ThreadStore, id: string): ThreadStore {
  if (!store.threads.some((thread) => thread.id === id)) return store
  const next: ThreadStore = { ...store, activeId: id }
  writeJSON(THREADS_KEY, next)
  return next
}

export function activeThread(store: ThreadStore): StoredThread | undefined {
  return store.threads.find((thread) => thread.id === store.activeId)
}

// --- saved prompts -----------------------------------------------------

export function loadSavedPrompts(): SavedPrompt[] {
  const stored = readJSON<SavedPrompt[]>(PROMPTS_KEY)
  if (!Array.isArray(stored)) return []
  return stored.filter(
    (prompt): prompt is SavedPrompt =>
      Boolean(prompt) && typeof prompt.text === 'string' && typeof prompt.id === 'string',
  )
}

/** Saves text, ignoring a blank or exact-duplicate entry. */
export function addSavedPrompt(
  prompts: SavedPrompt[],
  text: string,
): SavedPrompt[] {
  const trimmed = text.trim()
  if (!trimmed) return prompts
  if (prompts.some((prompt) => prompt.text === trimmed)) return prompts

  const next = [
    { id: newId(), text: trimmed, savedAt: new Date().toISOString() },
    ...prompts,
  ].slice(0, MAX_SAVED_PROMPTS)
  writeJSON(PROMPTS_KEY, next)
  return next
}

export function removeSavedPrompt(
  prompts: SavedPrompt[],
  id: string,
): SavedPrompt[] {
  const next = prompts.filter((prompt) => prompt.id !== id)
  writeJSON(PROMPTS_KEY, next)
  return next
}
