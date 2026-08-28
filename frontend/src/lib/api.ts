/**
 * One place that knows where the Go backend lives, and one way to call it.
 *
 * `VITE_API_BASE_URL` overrides the local default so the same build can point
 * at a non-localhost backend; the default matches the `-serve :8080` address
 * `backend/cmd/server` documents and the origin `withDevCORS` allows.
 */
export const API_BASE =
  import.meta.env.VITE_API_BASE_URL ?? 'http://localhost:8080'

/**
 * GETs JSON, turning a non-2xx into a thrown Error carrying the server's own
 * message. Callers surface that message rather than substituting a friendlier
 * fiction — a page that can't reach the reconciliation engine has to say so,
 * not render an empty state that looks like "you have no data".
 */
export async function getJson<T>(path: string): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`)
  if (!response.ok) {
    const detail = await response.text()
    throw new Error(`${path} returned ${response.status}: ${detail.trim()}`)
  }
  return (await response.json()) as T
}
