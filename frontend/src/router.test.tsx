import type { ReactElement } from 'react'
import { render, screen } from '@testing-library/react'
import { createMemoryRouter, RouterProvider, type RouteObject } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import ProfilePage from '@/components/Profile/ProfilePage'

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

describe('profile route wiring', () => {
  it('registers /profile rendering ProfilePage inside its own named error boundary', () => {
    const shell = routes[0] as RouteObject
    const profileRoute = shell.children?.find((route) => route.path === 'profile')
    expect(profileRoute).toBeDefined()

    // withBoundary('Profile', <ProfilePage />) — the same
    // ErrorBoundary(component=name){children} shape every other route uses.
    const boundaryElement = profileRoute!.element as ReactElement<{
      component: string
      children: ReactElement
    }>
    expect(boundaryElement.props.component).toBe('Profile')
    expect(boundaryElement.props.children.type).toBe(ProfilePage)
  })
})
