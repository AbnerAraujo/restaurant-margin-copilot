import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '@/lib/api'
import { explainRequestFailure, isNetworkFailure } from '@/lib/requestFailure'

describe('explainRequestFailure', () => {
  beforeEach(() => {
    // The describer logs the original rejection for a developer with DevTools
    // open. Silenced here so a passing suite stays readable.
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  it('tells the owner to check the connection when the request never reached a server', () => {
    // What `fetch()` itself rejects with: a TypeError, worded differently in
    // every browser and explaining nothing in any of them.
    expect(explainRequestFailure(new TypeError('Failed to fetch'))).toMatch(
      /check your connection/i,
    )
  })

  it('names no browser-specific wording in the unreachable-server message', () => {
    expect(explainRequestFailure(new TypeError('Load failed'))).not.toMatch(
      /load failed/i,
    )
  })

  it('replaces a raw Postgres error with copy that has a next step', () => {
    const message = explainRequestFailure(
      new ApiError(
        'query_failed',
        'ERROR: relation "reconciliations" does not exist (SQLSTATE 42P01)',
        500,
      ),
    )
    expect(message).toMatch(/reload this page/i)
  })

  it('never lets a SQLSTATE reach the sentence an owner reads', () => {
    const message = explainRequestFailure(
      new ApiError('query_failed', 'ERROR: something (SQLSTATE 42P01)', 500),
    )
    expect(message).not.toMatch(/SQLSTATE/i)
  })

  it('passes a handler-authored refusal through unchanged', () => {
    // `invalid_input` details are written for the person reading them, and
    // are more specific than anything this module could substitute.
    const detail = 'enter a valid email address, like name@restaurant.com'
    expect(explainRequestFailure(new ApiError('invalid_input', detail, 400))).toBe(
      detail,
    )
  })

  it('passes an unrecognised code through, so a new refusal message is not swallowed', () => {
    // The blocklist's whole point: a code nobody has added here yet is
    // assumed to carry owner-facing prose, because almost all of them do.
    const detail = 'you have already replaced that campaign'
    expect(explainRequestFailure(new ApiError('already_replaced', detail, 409))).toBe(
      detail,
    )
  })

  it('substitutes copy when a blocked code arrives with an empty detail', () => {
    expect(explainRequestFailure(new ApiError('unknown_error', '', 502))).toMatch(
      /reload this page/i,
    )
  })

  it('substitutes copy for a rejection that is not an ApiError at all', () => {
    expect(explainRequestFailure(new Error('boom'))).toMatch(/reload this page/i)
  })

  it('substitutes copy for a rejection that is not an Error at all', () => {
    expect(explainRequestFailure('boom')).toMatch(/reload this page/i)
  })

  it('does not speak in the first person', () => {
    // The interface is "we", never "I" — the voice rule two Points surfaces
    // used to break.
    expect(explainRequestFailure(new TypeError('Failed to fetch'))).not.toMatch(
      /\bI\b/,
    )
  })
})

describe('isNetworkFailure', () => {
  it('recognises the TypeError fetch throws for an unreachable server', () => {
    expect(isNetworkFailure(new TypeError('Failed to fetch'))).toBe(true)
  })

  it('does not treat a real HTTP response as a network failure', () => {
    expect(isNetworkFailure(new ApiError('query_failed', 'boom', 500))).toBe(false)
  })
})
