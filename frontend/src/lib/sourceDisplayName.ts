/**
 * Maps `internal/reconcile`'s normalized source keys to the names an owner
 * actually uses for those platforms — the frontend mirror of the backend's
 * `humanizeSource` (backend/internal/httpapi/visualization.go), which is
 * what a chat answer's "Where the day's revenue came from" pie legend
 * already renders (e.g. "In-house POS" rather than "pos"). Mirrored rather
 * than fetched for the same reason `KNOWN_PLATFORMS`
 * (components/Chat/guidedQuestion.ts) is hardcoded: it's a fixed switch on
 * the Go side, and "pos" in particular is never returned by
 * `GET /api/platforms` (that endpoint only covers commission-bearing
 * delivery platforms, not the in-house till).
 */
const SOURCE_DISPLAY_NAMES: Record<string, string> = {
  pos: 'In-house POS',
  ifood: 'iFood',
  just_eat_takeaway: 'Just Eat Takeaway',
}

export function humanizeSource(source: string): string {
  return SOURCE_DISPLAY_NAMES[source] ?? source
}
