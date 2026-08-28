import { render, screen } from '@testing-library/react'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// Stands in for a real chrome crash (the sidebar, the mobile nav bar —
// anything AppShell itself renders directly, as opposed to a routed page).
// Before AppShell was wrapped in its own boundary, a throw here took the
// whole app to React's default blank screen; this asserts the fix instead
// of trusting the wrapping by inspection.
vi.mock('@/components/Shell/Sidebar', () => ({
  default: () => {
    throw new Error('sidebar boom')
  },
  MobileNavBar: () => null,
}))

import { routes } from './router'

beforeEach(() => {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }),
  )
  vi.spyOn(console, 'error').mockImplementation(() => undefined)
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('root route', () => {
  it('catches a crash in the app shell itself rather than blanking the whole app', async () => {
    const router = createMemoryRouter(routes, { initialEntries: ['/'] })
    render(<RouterProvider router={router} />)

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Something broke in App shell.',
    )
  })
})
