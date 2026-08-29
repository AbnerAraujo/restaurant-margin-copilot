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

/**
 * POSTs a JSON body, turning a non-2xx into a thrown `ApiError` carrying the
 * server's own error code/detail (spec 002-badge-expansion's POST
 * /api/promotions and POST /api/usage both use this) — same "surface the
 * real message" discipline as `getJson`, plus a typed `code` field so a
 * caller can react to a specific refusal (e.g. FR-007's
 * `replaces_not_flagged_negative`) without parsing prose.
 */
export class ApiError extends Error {
  code: string
  constructor(code: string, detail: string) {
    super(detail)
    this.code = code
  }
}

export async function postJson<T>(path: string, body?: unknown): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    method: 'POST',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!response.ok) {
    const raw = await response.text()
    try {
      const parsed = JSON.parse(raw) as { error?: string; detail?: string }
      throw new ApiError(parsed.error ?? 'unknown_error', parsed.detail ?? raw)
    } catch (caught) {
      if (caught instanceof ApiError) throw caught
      throw new Error(`${path} returned ${response.status}: ${raw.trim()}`)
    }
  }
  return (await response.json()) as T
}

/**
 * PUTs a JSON body, turning a non-2xx into a thrown `ApiError` — the same
 * shape `postJson` already gives every POST endpoint. Backs
 * `PUT /api/profile` (the Profile page's full-replace save), and any future
 * "replace this whole resource" endpoint that isn't a good fit for POST's
 * create-a-new-thing connotation.
 */
export async function putJson<T>(path: string, body: unknown): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!response.ok) {
    const raw = await response.text()
    try {
      const parsed = JSON.parse(raw) as { error?: string; detail?: string }
      throw new ApiError(parsed.error ?? 'unknown_error', parsed.detail ?? raw)
    } catch (caught) {
      if (caught instanceof ApiError) throw caught
      throw new Error(`${path} returned ${response.status}: ${raw.trim()}`)
    }
  }
  return (await response.json()) as T
}

/**
 * POSTs a `multipart/form-data` body carrying a single file under field name
 * `file` (specs/007-cost-sheet-upload's preview/commit endpoints) — no
 * `Content-Type` header is set explicitly, since the browser must generate
 * the multipart boundary itself; setting one manually is a classic way to
 * silently corrupt a multipart body. Error handling matches `postJson`
 * exactly: the server's real `{error, detail}` body surfaces as a typed
 * `ApiError`, never a generic re-wording.
 */
export async function postMultipart<T>(path: string, file: File): Promise<T> {
  const formData = new FormData()
  formData.append('file', file)

  const response = await fetch(`${API_BASE}${path}`, {
    method: 'POST',
    body: formData,
  })
  if (!response.ok) {
    const raw = await response.text()
    try {
      const parsed = JSON.parse(raw) as { error?: string; detail?: string }
      throw new ApiError(parsed.error ?? 'unknown_error', parsed.detail ?? raw)
    } catch (caught) {
      if (caught instanceof ApiError) throw caught
      throw new Error(`${path} returned ${response.status}: ${raw.trim()}`)
    }
  }
  return (await response.json()) as T
}
