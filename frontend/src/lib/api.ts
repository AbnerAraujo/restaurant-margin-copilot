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
 * A non-2xx response, carrying the server's own error code and detail (spec
 * 002-badge-expansion's POST /api/promotions and POST /api/usage both use
 * this). The typed `code` field lets a caller react to a specific refusal
 * (e.g. FR-007's `replaces_not_flagged_negative`) without parsing prose, and
 * lets `lib/requestFailure` tell an owner-facing refusal apart from a raw Go
 * error it must not put on screen.
 */
export class ApiError extends Error {
  code: string
  status: number
  constructor(code: string, detail: string, status: number) {
    super(detail)
    this.code = code
    this.status = status
  }
}

/**
 * Turns a non-2xx `Response` into the `ApiError` every caller in the app
 * handles, whatever the verb. Shared by all four helpers below so a GET
 * failure and a POST failure are the same kind of thing to a caller — before
 * this, only the POST/PUT/multipart paths produced an `ApiError` and `getJson`
 * threw a bare `Error` whose message was a developer string ("/api/x returned
 * 500: …"), which pages then rendered verbatim.
 *
 * A body that isn't the handlers' `{error, detail}` JSON (a proxy's HTML 502,
 * an empty 500) still yields an `ApiError`, coded `unknown_error` — which
 * `lib/requestFailure` treats as a server fault and replaces with copy,
 * rather than showing whatever bytes came back.
 */
async function toApiError(response: Response): Promise<ApiError> {
  const raw = await response.text()
  try {
    const parsed = JSON.parse(raw) as { error?: string; detail?: string }
    return new ApiError(
      parsed.error ?? 'unknown_error',
      parsed.detail ?? raw.trim(),
      response.status,
    )
  } catch {
    return new ApiError('unknown_error', raw.trim(), response.status)
  }
}

/**
 * GETs JSON, turning a non-2xx into a thrown `ApiError`. Callers pass that
 * through `lib/requestFailure` rather than substituting a friendlier fiction
 * — a page that can't reach the reconciliation engine has to say so, not
 * render an empty state that looks like "you have no data".
 */
export async function getJson<T>(path: string): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`)
  if (!response.ok) {
    throw await toApiError(response)
  }
  return (await response.json()) as T
}

export async function postJson<T>(path: string, body?: unknown): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    method: 'POST',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!response.ok) {
    throw await toApiError(response)
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
    throw await toApiError(response)
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
    throw await toApiError(response)
  }
  return (await response.json()) as T
}
