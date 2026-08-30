import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import ProfilePage from './ProfilePage'

const EMPTY_PROFILE = {
  name: '',
  address: '',
  phone: '',
  email: '',
  description: '',
  photo: null,
  updated_at: '',
}

function stubFetch(responses: Array<{ ok: boolean; status?: number; body: unknown }>) {
  const mockFetch = vi.fn()
  for (const { ok, status, body } of responses) {
    mockFetch.mockResolvedValueOnce({
      ok,
      status: status ?? (ok ? 200 : 400),
      json: async () => body,
      text: async () => JSON.stringify(body),
    })
  }
  vi.stubGlobal('fetch', mockFetch)
  return mockFetch
}

/** A 1x1 transparent PNG, small enough to stay well under the 5MB cap. */
function tinyPngFile(name = 'storefront.png') {
  return new File([new Uint8Array([0x89, 0x50, 0x4e, 0x47, 1, 2, 3, 4])], name, {
    type: 'image/png',
  })
}

describe('ProfilePage', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('loads the current profile on mount and renders it into the form', async () => {
    stubFetch([
      {
        ok: true,
        body: {
          name: 'Trattoria Bellavista',
          address: '123 Main St',
          phone: '+1 555 123 4567',
          email: 'owner@bellavista.example',
          description: 'Family-run Italian kitchen since 1998.',
          photo: null,
          updated_at: '2026-08-20T12:00:00Z',
        },
      },
    ])

    render(<ProfilePage />)

    expect(await screen.findByDisplayValue('Trattoria Bellavista')).toBeInTheDocument()
    expect(screen.getByDisplayValue('123 Main St')).toBeInTheDocument()
    expect(screen.getByDisplayValue('owner@bellavista.example')).toBeInTheDocument()
  })

  it('renders an empty, ready-to-fill form on first run (no profile saved yet)', async () => {
    stubFetch([{ ok: true, body: EMPTY_PROFILE }])

    render(<ProfilePage />)

    expect(await screen.findByLabelText(/restaurant name/i)).toHaveValue('')
    // A first-run empty profile is a normal state, never an error.
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows the backend load failure plainly rather than a blank form', async () => {
    stubFetch([{ ok: false, status: 500, body: { error: 'query_failed', detail: 'db down' } }])

    render(<ProfilePage />)

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/couldn't load your profile/i)
  })

  it('marks the restaurant name as required, matching the form copy', async () => {
    stubFetch([{ ok: true, body: EMPTY_PROFILE }])
    render(<ProfilePage />)

    const nameInput = await screen.findByLabelText(/restaurant name/i)
    expect(nameInput).toBeRequired()
    expect(nameInput).toHaveAttribute('aria-required', 'true')
  })

  it('blocks a blank restaurant name natively and never calls the save endpoint', async () => {
    const fetchMock = stubFetch([{ ok: true, body: EMPTY_PROFILE }])
    const user = userEvent.setup()
    render(<ProfilePage />)

    const nameInput = await screen.findByLabelText(/restaurant name/i)
    const saveButton = screen.getByRole('button', { name: /save profile/i })
    await user.click(saveButton)

    // The native `required` attribute stops the browser from ever
    // dispatching the form's submit event, so the field is left flagged
    // invalid and the save endpoint is never reached.
    expect(nameInput).toBeInvalid()
    expect(fetchMock.mock.calls.filter(([, init]) => init?.method === 'PUT')).toHaveLength(0)
  })

  it('refuses to submit a whitespace-only restaurant name (passes native required, fails the trim check)', async () => {
    stubFetch([{ ok: true, body: EMPTY_PROFILE }])
    const user = userEvent.setup()
    render(<ProfilePage />)

    await user.type(await screen.findByLabelText(/restaurant name/i), '   ')
    await user.click(screen.getByRole('button', { name: /save profile/i }))

    expect(await screen.findByRole('alert')).toHaveTextContent(/enter your restaurant's name/i)
  })

  it('shows a clear, actionable message when the save request never reaches the server', async () => {
    stubFetch([{ ok: true, body: EMPTY_PROFILE }])
    const user = userEvent.setup()
    render(<ProfilePage />)

    await user.type(await screen.findByLabelText(/restaurant name/i), 'Cafe Luz')

    // The exact failure QA found: a blocked CORS preflight surfaces to
    // fetch() as a raw TypeError, never an HTTP response.
    vi.mocked(fetch).mockRejectedValueOnce(new TypeError('Failed to fetch'))
    await user.click(screen.getByRole('button', { name: /save profile/i }))

    const alert = await screen.findByRole('alert')
    expect(alert).not.toHaveTextContent(/failed to fetch/i)
    expect(alert).toHaveTextContent(/couldn't reach the server/i)
  })

  it('previews a chosen photo before saving, using a client-side data URI', async () => {
    stubFetch([{ ok: true, body: EMPTY_PROFILE }])
    const user = userEvent.setup()
    render(<ProfilePage />)

    await screen.findByLabelText(/restaurant name/i)
    expect(screen.queryByAltText(/restaurant photo preview/i)).not.toBeInTheDocument()

    const fileInput = screen.getByLabelText(/choose a restaurant photo/i)
    await user.upload(fileInput, tinyPngFile())

    const preview = await screen.findByAltText(/restaurant photo preview/i)
    expect(preview).toHaveAttribute('src', expect.stringMatching(/^data:image\/png;base64,/))
    expect(screen.getByRole('button', { name: /remove/i })).toBeInTheDocument()
  })

  it('rejects an oversized photo client-side without ever touching the file reader result', async () => {
    stubFetch([{ ok: true, body: EMPTY_PROFILE }])
    const user = userEvent.setup()
    render(<ProfilePage />)

    await screen.findByLabelText(/restaurant name/i)
    const oversized = new File([new Uint8Array(5 * 1024 * 1024 + 1)], 'huge.png', {
      type: 'image/png',
    })
    const fileInput = screen.getByLabelText(/choose a restaurant photo/i)
    await user.upload(fileInput, oversized)

    expect(await screen.findByText(/over the 5MB limit/i)).toBeInTheDocument()
    expect(screen.queryByAltText(/restaurant photo preview/i)).not.toBeInTheDocument()
  })

  it('rejects an unsupported file type client-side', async () => {
    stubFetch([{ ok: true, body: EMPTY_PROFILE }])
    // The input's `accept` attribute is only a picker hint — a real browser
    // still lets a user choose "all files" and pick anything, so the
    // runtime check must reject it too. `applyAccept: false` simulates that
    // rather than relying on user-event's own accept-attribute filtering.
    const user = userEvent.setup({ applyAccept: false })
    render(<ProfilePage />)

    await screen.findByLabelText(/restaurant name/i)
    const svg = new File(['<svg></svg>'], 'logo.svg', { type: 'image/svg+xml' })
    const fileInput = screen.getByLabelText(/choose a restaurant photo/i)
    await user.upload(fileInput, svg)

    expect(await screen.findByText(/must be png, jpeg, or webp/i)).toBeInTheDocument()
  })

  it('submits the form as a full replace and shows the saved confirmation', async () => {
    const fetchMock = stubFetch([
      { ok: true, body: EMPTY_PROFILE },
      {
        ok: true,
        body: {
          name: 'Cafe Luz',
          address: '9 Ocean Ave',
          phone: '',
          email: '',
          description: '',
          photo: null,
          updated_at: '2026-08-27T09:00:00Z',
        },
      },
    ])
    const user = userEvent.setup()
    render(<ProfilePage />)

    await user.type(await screen.findByLabelText(/restaurant name/i), 'Cafe Luz')
    await user.type(screen.getByLabelText(/^address$/i), '9 Ocean Ave')
    await user.click(screen.getByRole('button', { name: /save profile/i }))

    expect(await screen.findByText(/profile saved/i)).toBeInTheDocument()

    const putCall = fetchMock.mock.calls.find(([, init]) => init?.method === 'PUT')
    expect(putCall).toBeDefined()
    const [, init] = putCall!
    const body = JSON.parse(init!.body as string) as { name: string; address: string; photo: null }
    expect(body.name).toBe('Cafe Luz')
    expect(body.address).toBe('9 Ocean Ave')
    expect(body.photo).toBeNull()
  })

  it('surfaces the server-side refusal (e.g. size cap) rather than a generic error', async () => {
    stubFetch([
      { ok: true, body: EMPTY_PROFILE },
      {
        ok: false,
        status: 413,
        body: { error: 'photo_too_large', detail: 'that photo is 6.0MB, which is over the 5MB limit' },
      },
    ])
    const user = userEvent.setup()
    render(<ProfilePage />)

    await user.type(await screen.findByLabelText(/restaurant name/i), 'Cafe Luz')
    await user.click(screen.getByRole('button', { name: /save profile/i }))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/6\.0MB/i)
    })
  })

  it('echoes back the loaded updated_at on save, for optimistic-concurrency checking', async () => {
    const fetchMock = stubFetch([
      {
        ok: true,
        body: { ...EMPTY_PROFILE, name: 'Cafe Luz', updated_at: '2026-08-20T12:00:00.123456Z' },
      },
      {
        ok: true,
        body: { ...EMPTY_PROFILE, name: 'Cafe Luz', updated_at: '2026-08-27T09:00:00Z' },
      },
    ])
    const user = userEvent.setup()
    render(<ProfilePage />)

    await screen.findByDisplayValue('Cafe Luz')
    await user.click(screen.getByRole('button', { name: /save profile/i }))
    await screen.findByText(/profile saved/i)

    const putCall = fetchMock.mock.calls.find(([, init]) => init?.method === 'PUT')
    const body = JSON.parse(putCall![1]!.body as string) as { updated_at: string }
    expect(body.updated_at).toBe('2026-08-20T12:00:00.123456Z')
  })

  it('shows a clear, actionable message when a stale tab tries to save over a newer save (409 conflict) rather than a raw error or a silent success', async () => {
    stubFetch([
      { ok: true, body: { ...EMPTY_PROFILE, name: 'Cafe Luz', updated_at: '2026-08-20T12:00:00Z' } },
      {
        ok: false,
        status: 409,
        body: {
          error: 'profile_conflict',
          detail:
            'this profile was updated elsewhere since you loaded it — reload to see the latest before saving your changes',
        },
      },
    ])
    const user = userEvent.setup()
    render(<ProfilePage />)

    await user.type(await screen.findByLabelText(/^address$/i), '9 Ocean Ave')
    await user.click(screen.getByRole('button', { name: /save profile/i }))

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/updated elsewhere/i)
    expect(alert).toHaveTextContent(/reload/i)
    // Never a silent success for what is actually a lost-update refusal.
    expect(screen.queryByText(/profile saved/i)).not.toBeInTheDocument()
  })

  it('clears a stale field error from a previous submit once a new submit attempt begins, even one blocked by native validation', async () => {
    stubFetch([
      { ok: true, body: EMPTY_PROFILE },
      {
        ok: false,
        status: 400,
        body: {
          error: 'invalid_input',
          detail: 'enter a valid phone number, using only digits, spaces, and + - ( )',
        },
      },
    ])
    const user = userEvent.setup()
    render(<ProfilePage />)

    await user.type(await screen.findByLabelText(/restaurant name/i), 'Cafe Luz')
    await user.type(screen.getByLabelText(/^phone$/i), 'call-us-maybe')
    await user.click(screen.getByRole('button', { name: /save profile/i }))

    expect(await screen.findByRole('alert')).toHaveTextContent(/valid phone number/i)

    // Fix the phone, then introduce an email the browser's own native
    // `type="email"` validation will block — the exact QA repro.
    const phoneInput = screen.getByLabelText(/^phone$/i)
    await user.clear(phoneInput)
    await user.type(phoneInput, '+1 555 123 4567')
    const emailInput = screen.getByLabelText(/^email$/i)
    await user.type(emailInput, 'not-an-email')
    expect(emailInput).toBeInvalid()

    await user.click(screen.getByRole('button', { name: /save profile/i }))

    // The stale phone error must not survive this second, blocked attempt
    // — it now describes a field that's already fixed, and would
    // otherwise leave the owner with no visible link between the blocked
    // submit and the actual (email) problem.
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('rejects a photo exactly 1 byte over the limit without a self-contradictory size in the message', async () => {
    stubFetch([{ ok: true, body: EMPTY_PROFILE }])
    const user = userEvent.setup()
    render(<ProfilePage />)

    await screen.findByLabelText(/restaurant name/i)
    // 1 byte over 5MB: naive one-decimal rounding renders this as "5.0MB",
    // which reads as satisfying "...over the 5MB limit" rather than
    // violating it.
    const boundary = new File([new Uint8Array(5 * 1024 * 1024 + 1)], 'boundary.png', {
      type: 'image/png',
    })
    const fileInput = screen.getByLabelText(/choose a restaurant photo/i)
    await user.upload(fileInput, boundary)

    const message = await screen.findByText(/over the 5MB limit/i)
    expect(message).not.toHaveTextContent(/5\.0MB/i)
  })

  it('caps the About textarea height and scrolls internally rather than growing without bound', async () => {
    stubFetch([{ ok: true, body: EMPTY_PROFILE }])
    render(<ProfilePage />)

    const textarea = await screen.findByLabelText(/about/i)
    expect(textarea.className).toMatch(/max-h-/)
    expect(textarea.className).toMatch(/overflow-y-auto/)
  })

  it('sets maxLength on every profile field, matching the backend limits', async () => {
    stubFetch([{ ok: true, body: EMPTY_PROFILE }])
    render(<ProfilePage />)

    expect(await screen.findByLabelText(/restaurant name/i)).toHaveAttribute('maxLength', '200')
    expect(screen.getByLabelText(/^address$/i)).toHaveAttribute('maxLength', '300')
    expect(screen.getByLabelText(/^phone$/i)).toHaveAttribute('maxLength', '40')
    expect(screen.getByLabelText(/^email$/i)).toHaveAttribute('maxLength', '254')
    expect(screen.getByLabelText(/about/i)).toHaveAttribute('maxLength', '1000')
  })
})
