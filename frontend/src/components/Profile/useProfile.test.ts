import { renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { useProfile } from './useProfile'

function stubFetchOnce(status: number, body: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({
      ok: status >= 200 && status < 300,
      status,
      json: async () => body,
      text: async () => JSON.stringify(body),
    }),
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
})

// QA round 4 regression: useProfile's error field used to be set to
// `caught.message` straight from the thrown ApiError, bypassing
// lib/requestFailure.ts's blocklist entirely — unlike Points/usePoints.ts,
// the hook this one's own doc comment says it mirrors, which already
// routes through explainRequestFailure. GET /api/profile's query_failed
// path (internal/httpapi/profile.go) writes the raw pgx error string as
// `detail`, and that must never reach a consumer of this hook's `error`
// field, even though the current sole consumer (Sidebar) doesn't render it
// today — a future consumer that does must not inherit a live leak.
describe('useProfile', () => {
  it('never surfaces a raw internal error for an internal-only failure code', async () => {
    stubFetchOnce(500, {
      error: 'query_failed',
      detail: 'ERROR: relation "restaurant_profile" does not exist (SQLSTATE 42P01)',
    })

    const { result } = renderHook(() => useProfile())

    await waitFor(() => expect(result.current.error).not.toBeNull())

    expect(result.current.error).not.toMatch(/SQLSTATE/i)
    expect(result.current.error).not.toMatch(/relation "restaurant_profile"/i)
    expect(result.current.error).toMatch(/server ran into a problem/i)
  })

  it('still surfaces an authored, owner-facing detail unchanged', async () => {
    stubFetchOnce(400, {
      error: 'invalid_input',
      detail: "enter your restaurant's name — it's shown throughout the app and can't be blank",
    })

    const { result } = renderHook(() => useProfile())

    await waitFor(() => expect(result.current.error).not.toBeNull())

    expect(result.current.error).toBe(
      "enter your restaurant's name — it's shown throughout the app and can't be blank",
    )
  })
})
