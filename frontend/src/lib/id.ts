/**
 * A unique id, collision-resistant across page reloads and across browser
 * tabs.
 *
 * This is the one place that decision is made. A module-level incrementing
 * counter (`let seq = 0; seq += 1`) looks unique but isn't: it resets to 0
 * every time its module is re-evaluated, which includes every page reload.
 * Two different reloads of the same tab then mint the exact same sequence of
 * ids for genuinely different objects — the incident that motivated this
 * file was a chat message id colliding across a reload, which made the
 * durable spend ledger's `${messageId}#index` dedupe key treat a brand-new,
 * separately billed answer as a repeat of an old one and silently drop its
 * cost. Anything handed to `crypto.randomUUID()` instead is unique for
 * practical purposes regardless of how many times the page has reloaded or
 * how many tabs are open.
 *
 * Prefers `crypto.randomUUID()` — available in every runtime this app
 * targets. Falls back to a timestamp + random suffix only if some embedding
 * lacks it, so id generation can never throw.
 */
export function createUniqueId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
}
