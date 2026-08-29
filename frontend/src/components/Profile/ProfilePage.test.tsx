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

  it('refuses to submit with a blank restaurant name and never calls the save endpoint', async () => {
    stubFetch([{ ok: true, body: EMPTY_PROFILE }])
    const user = userEvent.setup()
    render(<ProfilePage />)

    const saveButton = await screen.findByRole('button', { name: /save profile/i })
    await user.click(saveButton)

    expect(await screen.findByRole('alert')).toHaveTextContent(/enter your restaurant's name/i)
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
})
