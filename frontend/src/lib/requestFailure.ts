import { ApiError } from './api'

/**
 * One place that turns anything `lib/api`'s fetch helpers can reject with
 * into a sentence an owner can act on.
 *
 * Why this exists: every data page used to store `caught.message` directly
 * and render it in a monospace span. That put three different kinds of
 * developer string on screen —
 *
 *   - `getJson`'s own wrapper, e.g. "/api/platforms returned 500: {…}"
 *   - the backend's raw Go error for a `query_failed`, which for a Postgres
 *     fault is a bare `ERROR: … (SQLSTATE 42P01)`
 *   - `fetch()`'s browser-specific "Failed to fetch" / "NetworkError when
 *     attempting to fetch resource" / "Load failed"
 *
 * — none of which say what to do next, which is the whole job of an error
 * (ux-writing: what happened → why → how to fix; never a code or a dead
 * end). `ProfilePage` and `QuestionComposer` had each already solved this
 * locally, in slightly different words; this is that fix made shared, so
 * every surface that can fail to load says the same thing in the same voice.
 *
 * Callers supply the "what" ("We couldn't load your campaigns.") because
 * only the page knows what it was loading. This supplies the "why → how".
 * The two are composed as whole sentences, never as fragments spliced into
 * one sentence, so translation is free to reorder either independently.
 */

/**
 * The error codes whose `detail` is NOT fit to show an owner. Everything else
 * `internal/httpapi` emits is prose someone wrote for the person reading it
 * ("enter a valid email address, like name@restaurant.com", "row 4: amount
 * must be a number", "this profile was updated elsewhere since you loaded it
 * — reload to see the latest before saving your changes"), and passing those
 * through unchanged is the whole point: they are more specific and more
 * useful than any substitute this module could offer.
 *
 * A blocklist rather than an allowlist, deliberately. The handlers that pass
 * a bare `err.Error()` straight out are a short, known, and slow-growing set;
 * the handlers with authored copy are the large and growing one. Listing the
 * former means a new refusal message reaches the owner by default, instead of
 * silently degrading to `SERVER_FAULT` until someone remembers to allowlist
 * its code.
 *
 * Each entry, and what it would otherwise put on screen:
 *   query_failed, cache_clear_failed, live_data_not_ready, pipeline_failed
 *     — a raw Go/pgx error, e.g. `ERROR: … (SQLSTATE 42P01)`
 *   write_failed — `writing live_cost_sheet.csv: <raw>`
 *   method_not_allowed — "only GET is supported": true, but it describes a
 *     bug in this frontend, and there is nothing an owner can do with it
 *   unknown_error — a body that wasn't the handlers' {error, detail} JSON
 *     at all (a proxy's HTML 502, an empty 500)
 */
const INTERNAL_ONLY_CODES: ReadonlySet<string> = new Set([
  'query_failed',
  'cache_clear_failed',
  'live_data_not_ready',
  'pipeline_failed',
  'write_failed',
  'method_not_allowed',
  'unknown_error',
])

const UNREACHABLE =
  "The app couldn't reach the server. Check your connection, then reload this page."

const SERVER_FAULT =
  'The server ran into a problem completing the request. Reload this page to try again.'

/**
 * True when `caught` is the error `fetch()` itself throws for a request that
 * never reached a server at all — DNS failure, connection refused, or a
 * blocked CORS preflight. Browsers disagree on the wording and none of them
 * explain the cause, so this checks the type `fetch()` actually rejects with
 * (a `TypeError`, distinct from the `Error`/`ApiError` the helpers in
 * `lib/api` construct from a real HTTP response) rather than matching
 * message text, which would silently stop working in a browser that phrases
 * it differently.
 */
export function isNetworkFailure(caught: unknown): boolean {
  return caught instanceof TypeError
}

/**
 * The "why → how to fix" half of a load/save failure message.
 *
 * The original rejection is logged to the console rather than rendered: a
 * SQLSTATE or a stack is genuinely useful, but its reader is a developer
 * with DevTools open, not an owner looking at a margin figure.
 */
export function explainRequestFailure(caught: unknown): string {
  console.error('Request failed:', caught)

  if (isNetworkFailure(caught)) return UNREACHABLE

  if (
    caught instanceof ApiError &&
    !INTERNAL_ONLY_CODES.has(caught.code) &&
    caught.message.trim() !== ''
  ) {
    return caught.message
  }

  return SERVER_FAULT
}
