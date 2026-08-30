import type { ChatMessage, ErrorChatMessage } from '@/components/Chat/ChatPanel'
import type { CostInteraction } from '@/components/CostPanel/CostPanel'

/**
 * Client-side persistence for the chat: the current thread, a short history
 * of previous ones, reusable saved prompts, and the durable record of what
 * the model calls behind those threads actually cost.
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
 *
 * ## The persistence model (rewritten after the state-persistence QA pass)
 *
 * Three defects found in that pass shared one root cause: React state was
 * treated as the source of truth and `localStorage` as a dumb mirror written
 * from whatever snapshot the component happened to be holding. That model
 * loses data three different ways — a component that unmounts mid-request
 * takes the answer with it, a second tab writes its own stale snapshot over
 * the first tab's newer one, and anything not held in that snapshot (the
 * running cost total) simply doesn't survive a reload at all.
 *
 * The model is now inverted, and the three fixes are one design:
 *
 *  1. **Storage is the source of truth; React state is a view of it.** Every
 *     mutation goes through `commitThreadMessages`, a read-modify-write
 *     transaction against the CURRENT contents of the key — never against a
 *     snapshot captured at mount. Components subscribe
 *     (`subscribeToThreadStore`) and re-render from what was actually
 *     written.
 *  2. **Commits are plain module functions, not state updaters.** A commit
 *     made from a settled promise lands in storage whether or not the
 *     component that started the request is still mounted. That is what
 *     stops a completed, already-billed answer from being discarded because
 *     the reader navigated away.
 *  3. **Merges never overwrite; they reconcile.** Two divergent stores union
 *     by message id, and where the same id exists on both sides the more
 *     RESOLVED version wins (see `resolutionRank`). Messages are append-only
 *     and only ever move forward through that lattice, so a union is always
 *     the correct reconciliation — no tab can destroy another tab's history
 *     and no late writer can un-resolve a resolved answer.
 *  4. **Spend is durable because the conversation is durable.** The cost
 *     ledger is written by the same commit that writes the answer it paid
 *     for, so a reload that still shows the answers also still shows what
 *     they cost.
 */

// Independently versioned: a thread stores whole `ChatMessage` objects, whose
// shape has changed more than once since persistence was first added
// (ErrorChatMessage, AnswerCacheInfo, and other fields landed without a
// version bump) — a browser that kept an old-shape thread across those
// changes could hand the current renderer an object missing fields it now
// assumes exist. Saved prompts are plain strings and never changed shape, so
// they don't need to be invalidated by a threads-only bump.
//
// NOT bumped for the `pending` message kind or `ErrorChatMessage.cause` added
// in this pass: both are purely additive, and older stored data (which simply
// contains neither) stays valid under the current renderer. A bump here costs
// a real user their thread history, so it is reserved for shape changes that
// would actually break rendering.
const THREADS_VERSION = 2
const PROMPTS_VERSION = 1
const SPEND_VERSION = 1
const THREADS_KEY = `mbs.chat.threads.v${THREADS_VERSION}`
const PROMPTS_KEY = `mbs.chat.prompts.v${PROMPTS_VERSION}`
const SPEND_KEY = `mbs.chat.spend.v${SPEND_VERSION}`

/** How many threads are kept in total, including the active one. */
export const MAX_THREADS = 6
/** How many saved prompts are kept. */
export const MAX_SAVED_PROMPTS = 12
/**
 * How many model calls the spend ledger keeps. Far larger than MAX_THREADS'
 * worth of conversation on purpose: an evicted thread must not take its cost
 * off the running total (that would make the total silently DROP, which is
 * the under-reporting this project's instrumentation principle forbids), so
 * the ledger deliberately outlives the threads it describes.
 */
export const MAX_SPEND_ENTRIES = 500

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

/**
 * One real model call, kept durably and attributed to the message it paid
 * for. `CostPanel` renders the sum of these.
 *
 * Attribution (`threadId`/`messageId`) is what makes the total honest rather
 * than merely persistent: the figure on the pill can always be traced to
 * specific answers, and the ledger is deduplicated by `id` so replaying a
 * commit — a retry, a cross-tab merge, a double-mount in React strict mode —
 * can never double-count spend.
 */
export interface SpendEntry extends CostInteraction {
  /** `${messageId}#${index}` — deterministic, so a replayed commit dedupes. */
  id: string
  threadId: string
  messageId: string
  recordedAt: string
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

// --- change notification ------------------------------------------------

type ThreadListener = (store: ThreadStore) => void
type SpendListener = (entries: SpendEntry[]) => void

const threadListeners = new Set<ThreadListener>()
const spendListeners = new Set<SpendListener>()
let storageListenerAttached = false

/**
 * A `storage` event fires in every OTHER tab of the same origin, never in
 * the tab that wrote. That asymmetry is exactly the shape needed here: a
 * local commit notifies subscribers directly (below), and this listener
 * covers the remote case, so both paths converge on "re-read what is
 * actually stored and merge it in" rather than either one trusting a
 * snapshot.
 *
 * `event.key === null` is a whole-storage `clear()`, which must invalidate
 * everything rather than being ignored for not matching a key name.
 */
function ensureStorageListener(): void {
  if (storageListenerAttached || typeof window === 'undefined') return
  storageListenerAttached = true
  window.addEventListener('storage', (event: StorageEvent) => {
    if (event.key === null || event.key === THREADS_KEY) {
      const store = loadThreadStore()
      for (const listener of threadListeners) listener(store)
    }
    if (event.key === null || event.key === SPEND_KEY) {
      const entries = loadSpendLedger()
      for (const listener of spendListeners) listener(entries)
    }
  })
}

export function subscribeToThreadStore(listener: ThreadListener): () => void {
  ensureStorageListener()
  threadListeners.add(listener)
  return () => {
    threadListeners.delete(listener)
  }
}

export function subscribeToSpendLedger(listener: SpendListener): () => void {
  ensureStorageListener()
  spendListeners.add(listener)
  return () => {
    spendListeners.delete(listener)
  }
}

function notifyThreads(store: ThreadStore): void {
  for (const listener of threadListeners) listener(store)
}

function notifySpend(entries: SpendEntry[]): void {
  for (const listener of spendListeners) listener(entries)
}

// --- in-flight request registry -----------------------------------------

/**
 * Message ids for requests THIS document currently has in flight.
 *
 * Module-level, not component state, and deliberately not persisted: it must
 * survive `ChatPanel` unmounting (the owner navigating to `/help` and back
 * while a question is still resolving) and must be empty after a real reload
 * (a reload genuinely orphans any request that was in flight).
 *
 * That single distinction is what lets `reconcileInterruptedMessages` tell
 * "this request is still coming" from "this request died with the previous
 * page load" without a timeout, a heartbeat, or any guessing.
 */
const inFlightMessageIds = new Set<string>()

/**
 * True once this document has started going away.
 *
 * Found live rather than by reading the code: reloading with a request in
 * flight makes the browser abort that request, and the abort rejects the
 * fetch BEFORE the page tears down — so the caller's catch block ran and
 * wrote a "couldn't reach your data" transport error over the pending
 * record. Two things were wrong with that. The copy was false (nothing was
 * unreachable; the request had already been sent and the backend goes on to
 * complete and bill it), and it destroyed the pending record that the next
 * load needs in order to recognise the interruption at all.
 *
 * Both `beforeunload` and `pagehide` are needed, and the reason is the whole
 * point of the flag. Measured in a real browser: on a reload, Chromium
 * aborts the in-flight fetch and delivers that rejection BEFORE `pagehide`
 * fires, so a `pagehide`-only flag was still false when the catch block ran
 * and the wrong error was written anyway. `beforeunload` fires at the START
 * of the unload sequence and wins that race; `pagehide` stays as the backstop
 * for the teardowns `beforeunload` does not cover (bfcache eviction, and
 * browsers that skip it). Neither listener calls `preventDefault`, so this
 * never produces a "leave site?" prompt.
 */
let documentUnloading = false
let unloadListenerAttached = false

function ensureUnloadListener(): void {
  if (unloadListenerAttached || typeof window === 'undefined') return
  unloadListenerAttached = true
  const markUnloading = () => {
    documentUnloading = true
  }
  window.addEventListener('beforeunload', markUnloading)
  window.addEventListener('pagehide', markUnloading)
  // A teardown that was announced and then didn't happen: the reader
  // cancelled the navigation, or the page came back out of the bfcache. The
  // document is alive again, so a request that fails from here really is a
  // transport failure and must be reported as one.
  window.addEventListener('pageshow', () => {
    documentUnloading = false
  })
}

/**
 * Whether a rejected request is this document being torn down rather than a
 * real transport failure. The two owe the reader completely different
 * things, and only one of them is free to retry.
 */
export function isDocumentUnloading(): boolean {
  return documentUnloading
}

export function markRequestInFlight(messageId: string): void {
  ensureUnloadListener()
  inFlightMessageIds.add(messageId)
}

export function clearRequestInFlight(messageId: string): void {
  inFlightMessageIds.delete(messageId)
}

export function isRequestInFlight(messageId: string): boolean {
  return inFlightMessageIds.has(messageId)
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
 *
 * Deliberately leaves `SPEND_KEY` alone too, for a stronger reason: this
 * button exists to recover from a render crash, and money that was already
 * spent does not stop having been spent because the page that displayed it
 * broke. Zeroing the running total as a side effect of crash recovery would
 * be precisely the "hidden or under-reported spend" this project rules out.
 */
export function clearThreadStorage(): void {
  try {
    window.localStorage.removeItem(THREADS_KEY)
  } catch {
    // Same "never fail harder than the thing we're recovering from" rule as
    // writeJSON above.
  }
  notifyThreads(loadThreadStore())
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
    // A question whose request had not resolved yet when it was persisted.
    // Kept, not dropped: dropping it here would silently recreate the exact
    // defect the pending record exists to fix.
    case 'pending':
      return typeof m.question === 'string'
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
  return readStoredThreadStore() ?? loadThreadStoreFresh()
}

/**
 * What is actually in the key, or `null` when there is nothing usable
 * there — as distinct from {@link loadThreadStore}, which substitutes a
 * brand-new empty thread.
 *
 * The distinction is load-bearing for the write path. `loadThreadStoreFresh`
 * mints a new random thread id every time it is called, so a `commitStore`
 * that read through `loadThreadStore` on an empty key would invent a phantom
 * empty thread on the way to writing the caller's real one, and the "Recent"
 * list would collect one blank entry per first write.
 */
function readStoredThreadStore(): ThreadStore | null {
  const stored = readJSON<ThreadStore>(THREADS_KEY)
  if (!stored || !Array.isArray(stored.threads) || stored.threads.length === 0) {
    return null
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
  if (threads.length === 0) return null
  const activeId = threads.some((t) => t.id === stored.activeId)
    ? stored.activeId
    : threads[0].id
  return { activeId, threads }
}

/**
 * Writes a brand-new store so other tabs can find the thread this one just
 * minted; a no-op once anything is stored.
 *
 * Without this, two tabs opening the app with an empty key each mint their
 * own random thread id and the reader who reloads the first tab lands on the
 * second tab's thread — nothing destroyed, but a confusing echo of the very
 * bug this pass fixes. Materialising the first tab's thread means the second
 * one simply joins it.
 *
 * Two tabs opening in the same instant can still both see an empty key and
 * both write. That race is deliberately left alone: the outcome is two real
 * threads that merge and are both reachable from "Recent", which is a
 * cosmetic surprise rather than data loss, and closing it properly would
 * mean a cross-tab lock this prototype has no business carrying.
 */
export function materialiseThreadStore(store: ThreadStore): void {
  if (readStoredThreadStore()) return
  writeJSON(THREADS_KEY, store)
  notifyThreads(store)
}

function loadThreadStoreFresh(): ThreadStore {
  const id = newId()
  return {
    activeId: id,
    threads: [{ id, title: 'New chat', updatedAt: new Date().toISOString(), messages: [] }],
  }
}

// --- reconciliation ------------------------------------------------------

/**
 * Where a message sits on the resolution lattice. A message only ever moves
 * FORWARD through these ranks, which is what makes a union of two divergent
 * stores a correct merge rather than a coin flip:
 *
 *   0 `pending`          — asked, no verdict yet.
 *   1 `error` (interrupted) — the local guess that a pending request died.
 *   2 anything else      — a real verdict from the backend, or a user message.
 *
 * Rank 1 sits BELOW rank 2 on purpose, and that placement is load-bearing.
 * It means "we assumed this request was lost" is always superseded by a real
 * answer for the same id, which is what makes the interruption recovery
 * safe to run eagerly on mount: if a second tab still had the request in
 * flight and it lands afterwards, the real answer replaces the placeholder
 * automatically instead of the two fighting. No timeout, no heartbeat, no
 * cross-tab lock needed.
 */
function resolutionRank(message: ChatMessage): number {
  if (message.role !== 'assistant') return 2
  if (message.kind === 'pending') return 0
  if (message.kind === 'error' && message.cause === 'interrupted') return 1
  return 2
}

/**
 * Unions two message lists by id, keeping the more resolved version of any
 * id present in both.
 *
 * Order is positional (primary's order, then primary-absent ids in
 * secondary's order) rather than sorted by `askedAt`. Sorting by timestamp
 * looked tempting and is wrong: an assistant message's `askedAt` is stamped
 * when the ANSWER arrives, so a slow answer sorts after a question the owner
 * typed while waiting, and a resolved message would visibly jump position
 * the moment it stopped being pending. Positional merge never reorders
 * anything already on screen — the dominant single-tab case is a strict
 * no-op — and in the genuinely ambiguous two-tab case it appends rather than
 * interleaves, which is at worst an odd-looking transcript and never a lost
 * one.
 */
export function mergeMessages(
  primary: ChatMessage[],
  secondary: ChatMessage[],
): ChatMessage[] {
  const secondaryById = new Map(secondary.map((message) => [message.id, message]))
  const merged = primary.map((message) => {
    const other = secondaryById.get(message.id)
    if (!other) return message
    return resolutionRank(other) > resolutionRank(message) ? other : message
  })
  const primaryIds = new Set(primary.map((message) => message.id))
  for (const message of secondary) {
    if (!primaryIds.has(message.id)) merged.push(message)
  }
  return merged
}

/**
 * Enforces MAX_THREADS without ever evicting the thread being written to.
 * Order is preserved untouched while under the cap so the "Recent" list
 * keeps its newest-first arrangement; only an over-cap store is reordered,
 * and then by real recency rather than insertion accident.
 */
function capThreads(threads: StoredThread[], activeId: string): StoredThread[] {
  if (threads.length <= MAX_THREADS) return threads
  const active = threads.filter((thread) => thread.id === activeId)
  const rest = threads
    .filter((thread) => thread.id !== activeId)
    .sort((a, b) => (b.updatedAt ?? '').localeCompare(a.updatedAt ?? ''))
  return [...active, ...rest].slice(0, MAX_THREADS)
}

/**
 * Reconciles two thread stores without either one destroying the other.
 *
 * `primary` wins ties; `secondary`'s threads and messages are folded in
 * rather than discarded. `preferredActiveId` is how a tab keeps looking at
 * the thread ITS reader chose: `activeId` is really per-tab view state that
 * happens to be persisted, so a background tab must never be yanked onto
 * whatever thread another tab opened.
 */
export function mergeThreadStores(
  primary: ThreadStore,
  secondary: ThreadStore,
  preferredActiveId?: string,
): ThreadStore {
  const secondaryById = new Map(secondary.threads.map((thread) => [thread.id, thread]))
  const merged: StoredThread[] = primary.threads.map((thread) => {
    const other = secondaryById.get(thread.id)
    if (!other) return thread
    const messages = mergeMessages(thread.messages, other.messages)
    return {
      id: thread.id,
      title: deriveThreadTitle(messages),
      updatedAt:
        (thread.updatedAt ?? '') >= (other.updatedAt ?? '')
          ? thread.updatedAt
          : other.updatedAt,
      messages,
    }
  })
  const primaryIds = new Set(primary.threads.map((thread) => thread.id))
  for (const thread of secondary.threads) {
    if (!primaryIds.has(thread.id)) merged.push(thread)
  }

  const candidates = [preferredActiveId, primary.activeId, secondary.activeId]
  const activeId =
    candidates.find(
      (id): id is string => Boolean(id) && merged.some((thread) => thread.id === id),
    ) ??
    merged[0]?.id ??
    primary.activeId

  return { activeId, threads: capThreads(merged, activeId) }
}

/**
 * The authoritative "what does the world look like right now" read: whatever
 * is in storage, with the caller's own view folded in on top of it.
 */
function currentStore(base?: ThreadStore): ThreadStore {
  const stored = readStoredThreadStore()
  if (!stored) return base ?? loadThreadStoreFresh()
  return base ? mergeThreadStores(stored, base, base.activeId) : stored
}

/**
 * The single write path. Reads what is CURRENTLY stored, folds in the
 * caller's own view of the world (`base`), applies the mutation, writes, and
 * tells every subscriber what actually landed.
 *
 * Read-modify-write against live storage — not against a snapshot taken when
 * a component mounted — is what stops a second tab from clobbering a first
 * one. It is a plain module function, not a state updater, so it also works
 * from a promise that settles after the component that started it has
 * unmounted.
 */
function commitStore(
  mutate: (store: ThreadStore) => ThreadStore,
  base?: ThreadStore,
): ThreadStore {
  const next = mutate(currentStore(base))
  writeJSON(THREADS_KEY, next)
  notifyThreads(next)
  return next
}

/**
 * Applies a change to one specific thread's messages and persists it.
 *
 * The thread is addressed by id rather than by "whichever is active", so an
 * answer always lands in the thread its question was asked in — even if the
 * owner opened a different thread, or a different tab did, while the request
 * was in flight.
 */
export function commitThreadMessages(
  threadId: string,
  mutate: (messages: ChatMessage[]) => ChatMessage[],
  base?: ThreadStore,
): ThreadStore {
  return commitStore((store) => {
    const exists = store.threads.some((thread) => thread.id === threadId)
    const threads = exists
      ? store.threads
      : [
          {
            id: threadId,
            title: 'New chat',
            updatedAt: new Date().toISOString(),
            messages: [],
          },
          ...store.threads,
        ]
    return {
      activeId: store.activeId,
      threads: capThreads(
        threads.map((thread) => {
          if (thread.id !== threadId) return thread
          const messages = mutate(thread.messages)
          return {
            ...thread,
            title: deriveThreadTitle(messages),
            updatedAt: new Date().toISOString(),
            messages,
          }
        }),
        store.activeId,
      ),
    }
  }, base)
}

/** Replaces the message with `id`, appending it if the id is gone. */
export function replaceMessage(
  messages: ChatMessage[],
  id: string,
  next: ChatMessage,
): ChatMessage[] {
  if (!messages.some((message) => message.id === id)) return [...messages, next]
  return messages.map((message) => {
    if (message.id !== id) return message
    // Never move a message backwards down the lattice: a late-arriving
    // "interrupted" placeholder must not overwrite a real answer that
    // another tab already recorded for the same question.
    return resolutionRank(next) >= resolutionRank(message) ? next : message
  })
}

/**
 * Resolves every pending message this document is NOT currently waiting on
 * into an honest "this never came back" state with a retry affordance.
 *
 * Called on mount. A pending message that survives into a fresh mount is,
 * by construction, one whose request died with a previous page load (a
 * reload, a closed tab) — `inFlightMessageIds` is empty after a reload but
 * intact across an in-page navigation, so a request that is genuinely still
 * running is left alone.
 *
 * The one case this cannot distinguish is a pending message another TAB is
 * still waiting on. That is handled by the lattice rather than by detection:
 * the placeholder written here ranks below a real verdict, so when the other
 * tab's answer lands it supersedes this placeholder automatically. Guessing
 * "lost" early and self-correcting is strictly better than the alternative
 * the QA pass found, which was leaving the question in silent limbo forever.
 */
export function reconcileInterruptedMessages(base?: ThreadStore): ThreadStore {
  const current = currentStore(base)

  let changed = false
  const threads = current.threads.map((thread) => {
    let threadChanged = false
    const messages = thread.messages.map((message) => {
      if (
        message.role !== 'assistant' ||
        message.kind !== 'pending' ||
        isRequestInFlight(message.id)
      ) {
        return message
      }
      threadChanged = true
      changed = true
      const interrupted: ErrorChatMessage = {
        id: message.id,
        role: 'assistant',
        kind: 'error',
        cause: 'interrupted',
        text:
          'The page was reloaded or closed before this answer came back, so it never reached you.',
        question: message.question,
        askedAt: message.askedAt,
      }
      return interrupted
    })
    return threadChanged ? { ...thread, messages } : thread
  })

  if (!changed) return current
  const next: ThreadStore = { ...current, threads }
  writeJSON(THREADS_KEY, next)
  notifyThreads(next)
  return next
}

/** Replaces the active thread's messages and persists the whole store. */
export function persistActiveThread(
  store: ThreadStore,
  messages: ChatMessage[],
): ThreadStore {
  return commitThreadMessages(
    store.activeId,
    (existing) => mergeMessages(messages, existing),
    store,
  )
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
  return commitStore(
    (current) => ({
      activeId: id,
      threads: capThreads(
        [
          { id, title: 'New chat', updatedAt: new Date().toISOString(), messages: [] },
          ...current.threads,
        ],
        id,
      ),
    }),
    store,
  )
}

export function openThread(store: ThreadStore, id: string): ThreadStore {
  if (!store.threads.some((thread) => thread.id === id)) return store
  return commitStore((current) => ({ ...current, activeId: id }), store)
}

export function activeThread(store: ThreadStore): StoredThread | undefined {
  return store.threads.find((thread) => thread.id === store.activeId)
}

// --- spend ledger --------------------------------------------------------

export function loadSpendLedger(): SpendEntry[] {
  const stored = readJSON<SpendEntry[]>(SPEND_KEY)
  if (!Array.isArray(stored)) return []
  return stored.filter(
    (entry): entry is SpendEntry =>
      Boolean(entry) &&
      typeof entry.id === 'string' &&
      typeof entry.estimated_cost_usd === 'number' &&
      Number.isFinite(entry.estimated_cost_usd),
  )
}

/**
 * Records what one message's model calls cost, durably and idempotently.
 *
 * Written by the same commit that writes the message itself, which is what
 * makes the running total consistent with the conversation on screen after a
 * reload — the defect this replaces showed $0.000 above answers that had
 * demonstrably cost money.
 *
 * Deduplicated by a deterministic `${messageId}#${index}` id, so a retry, a
 * cross-tab merge, or a double-invoked effect can never inflate the total
 * either.
 */
export function recordSpend(
  threadId: string,
  messageId: string,
  interactions: readonly CostInteraction[] | undefined,
): SpendEntry[] {
  if (!interactions || interactions.length === 0) return loadSpendLedger()

  const existing = loadSpendLedger()
  const knownIds = new Set(existing.map((entry) => entry.id))
  const recordedAt = new Date().toISOString()
  const additions = interactions
    .map((interaction, index) => ({
      ...interaction,
      id: `${messageId}#${index}`,
      threadId,
      messageId,
      recordedAt,
    }))
    .filter((entry) => !knownIds.has(entry.id))

  if (additions.length === 0) return existing

  const next = [...existing, ...additions].slice(-MAX_SPEND_ENTRIES)
  writeJSON(SPEND_KEY, next)
  notifySpend(next)
  return next
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
