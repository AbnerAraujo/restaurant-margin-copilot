import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, Outlet, RouterProvider } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import Sidebar from '@/components/Shell/Sidebar'
import ProfilePage from './ProfilePage'

interface ProfileRecord {
  name: string
  address: string
  phone: string
  email: string
  description: string
  photo: string | null
  updated_at: string
}

const EMPTY_PROFILE: ProfileRecord = {
  name: '',
  address: '',
  phone: '',
  email: '',
  description: '',
  photo: null,
  updated_at: 'V0',
}

/**
 * A minimal in-memory stand-in for the real `GET`/`PUT /api/profile`
 * backend, including its optimistic-concurrency 409: a `PUT` whose
 * `updated_at` no longer matches the record's current value is refused,
 * exactly like `backend/internal/httpapi/profile.go`. `setCurrent` lets a
 * test simulate a save landing from OUTSIDE this rendered tree — i.e. what
 * a second browser tab would have done — without ever rendering a second
 * copy of the app (this app has no real cross-tab channel; the two tabs'
 * only shared state is what they can both fetch from the server).
 */
function createProfileBackend(initial: ProfileRecord) {
  let current: ProfileRecord = { ...initial }
  let version = Number(initial.updated_at.replace('V', '')) || 0

  const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
    if (!String(url).includes('/api/profile')) {
      throw new Error(`unexpected fetch: ${String(url)}`)
    }
    const method = (init?.method ?? 'GET').toUpperCase()

    if (method === 'GET') {
      const body = { ...current }
      return { ok: true, status: 200, json: async () => body, text: async () => JSON.stringify(body) }
    }

    if (method === 'PUT') {
      const payload = JSON.parse(init!.body as string) as ProfileRecord
      if (payload.updated_at !== current.updated_at) {
        const body = {
          error: 'profile_conflict',
          detail:
            'this profile was updated elsewhere since you loaded it — reload to see the latest before saving your changes',
        }
        return {
          ok: false,
          status: 409,
          json: async () => body,
          text: async () => JSON.stringify(body),
        }
      }
      version += 1
      current = { ...payload, updated_at: `V${version}` }
      const body = { ...current }
      return { ok: true, status: 200, json: async () => body, text: async () => JSON.stringify(body) }
    }

    throw new Error(`unexpected method: ${method}`)
  })

  vi.stubGlobal('fetch', fetchMock)
  return { getCurrent: () => current, setCurrent: (next: Partial<ProfileRecord>) => {
    version += 1
    current = { ...current, ...next, updated_at: `V${version}` }
  } }
}

/**
 * Stands in for `AppShell`: `Sidebar` mounted once, outside the routed
 * `<Outlet/>`, exactly the nesting that makes the real sidebar never
 * remount as the owner navigates (the whole reason bug #1 existed).
 */
function TestShell() {
  return (
    <>
      <Sidebar />
      <Outlet />
    </>
  )
}

function renderShellAt(initialPath: string) {
  const router = createMemoryRouter(
    [
      {
        path: '/',
        element: <TestShell />,
        children: [
          { index: true, element: <p>Home content</p> },
          { path: 'profile', element: <ProfilePage /> },
        ],
      },
    ],
    { initialEntries: [initialPath] },
  )
  return render(<RouterProvider router={router} />)
}

describe('Sidebar/Profile freshness', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('shows a newly saved name in the sidebar on the same page load, with no reload, and it survives navigating to another page', async () => {
    createProfileBackend(EMPTY_PROFILE)
    const user = userEvent.setup()
    renderShellAt('/profile')

    await screen.findByLabelText(/restaurant name/i)
    // Fresh install: nothing saved yet, sidebar shows no name.
    expect(screen.queryByText('Trattoria Bellavista')).not.toBeInTheDocument()

    await user.type(screen.getByLabelText(/restaurant name/i), 'Trattoria Bellavista')
    await user.click(screen.getByRole('button', { name: /save profile/i }))
    await screen.findByText(/profile saved/i)

    // The sidebar picks up the new name without any full reload.
    expect(await screen.findByText('Trattoria Bellavista')).toBeInTheDocument()

    // It's still correct after navigating away — the sidebar never remounts.
    await user.click(screen.getByRole('link', { name: /^home$/i }))
    expect(await screen.findByText('Home content')).toBeInTheDocument()
    expect(screen.getByText('Trattoria Bellavista')).toBeInTheDocument()
  })

  it('after a two-tab 409 conflict, refreshes the sidebar to the real value immediately, and a reload brings the form, sidebar, and server into full agreement', async () => {
    const backend = createProfileBackend({ ...EMPTY_PROFILE, name: 'Old Name', updated_at: 'V0' })
    const user = userEvent.setup()
    const { unmount } = renderShellAt('/profile')

    await screen.findByDisplayValue('Old Name')
    expect(await screen.findByText('Old Name')).toBeInTheDocument()

    // Simulate a second tab's successful save landing on the server, invisible
    // to this tab until it next talks to the server.
    backend.setCurrent({ name: 'New Name From Tab A' })

    // This tab is still holding the stale updated_at from its own initial
    // load, and now tries to save its own edit.
    const nameInput = screen.getByLabelText(/restaurant name/i)
    await user.clear(nameInput)
    await user.type(nameInput, 'Tab B Attempted Name')
    await user.click(screen.getByRole('button', { name: /save profile/i }))

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/updated elsewhere/i)
    expect(alert).toHaveTextContent(/reload/i)
    expect(screen.queryByText(/profile saved/i)).not.toBeInTheDocument()

    // Bug #3 fix: the 409 itself refreshes the sidebar to the real current
    // value right away — it must not sit frozen on what this tab loaded.
    expect(await screen.findByText('New Name From Tab A')).toBeInTheDocument()
    // The owner's own attempted edit is left alone in the form (not silently
    // discarded) — the error message is what tells them to reload.
    expect(screen.getByDisplayValue('Tab B Attempted Name')).toBeInTheDocument()

    // Follow the error message's own advice and reload.
    unmount()
    renderShellAt('/profile')

    await screen.findByDisplayValue('New Name From Tab A')
    expect(screen.getByText('New Name From Tab A')).toBeInTheDocument()
    expect(backend.getCurrent().name).toBe('New Name From Tab A')
  })
})
