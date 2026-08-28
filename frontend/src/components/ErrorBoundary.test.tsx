import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import ErrorBoundary, { RouteErrorBoundary } from './ErrorBoundary'

// componentDidCatch (and RouteErrorBoundary's effect) fire a real
// `postJson('/api/client-errors', ...)` — stub `fetch` so that reporting
// call resolves instead of hitting a real network in tests, matching the
// stubbing pattern other tests in this repo use for `lib/api`.
beforeEach(() => {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }),
  )
  // React logs the caught error to the console on every render pass; this
  // is expected noise for these tests, not a signal to act on.
  vi.spyOn(console, 'error').mockImplementation(() => undefined)
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('ErrorBoundary', () => {
  it('renders children normally when nothing has thrown', () => {
    render(
      <ErrorBoundary component="Test surface">
        <div>All good</div>
      </ErrorBoundary>,
    )

    expect(screen.getByText('All good')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('catches a render error and names the broken surface', () => {
    function AlwaysThrows(): never {
      throw new Error('boom')
    }

    render(
      <ErrorBoundary component="Test surface">
        <AlwaysThrows />
      </ErrorBoundary>,
    )

    expect(screen.getByRole('alert')).toHaveTextContent(
      'Something broke in Test surface.',
    )
  })

  it('reports the crash to /api/client-errors', () => {
    function AlwaysThrows(): never {
      throw new Error('boom')
    }

    render(
      <ErrorBoundary component="Test surface">
        <AlwaysThrows />
      </ErrorBoundary>,
    )

    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/client-errors'),
      expect.objectContaining({ method: 'POST' }),
    )
  })

  it('runs onReset before clearing its own error state, so a fix applied there prevents an immediate re-crash', async () => {
    // Stands in for the real incident: a still-poisoned data source (here,
    // a module-level flag instead of localStorage) makes the child throw
    // again on every render until something clears it. `onReset` is that
    // "something" — the whole reason this prop exists.
    let poisoned = true
    function MaybeThrows() {
      if (poisoned) throw new Error('boom')
      return <div>Recovered</div>
    }
    const onReset = vi.fn(() => {
      poisoned = false
    })
    const user = userEvent.setup()

    render(
      <ErrorBoundary component="Ask" onReset={onReset}>
        <MaybeThrows />
      </ErrorBoundary>,
    )

    expect(screen.getByRole('alert')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /reset/i }))

    expect(onReset).toHaveBeenCalledTimes(1)
    expect(await screen.findByText('Recovered')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('without an onReset, clicking Reset re-renders the same still-broken child and crashes again (the bug onReset exists to fix)', async () => {
    function AlwaysThrows(): never {
      throw new Error('boom')
    }
    const user = userEvent.setup()

    render(
      <ErrorBoundary component="Test surface">
        <AlwaysThrows />
      </ErrorBoundary>,
    )

    await user.click(screen.getByRole('button', { name: /reset/i }))

    expect(screen.getByRole('alert')).toHaveTextContent(
      'Something broke in Test surface.',
    )
  })
})

describe('RouteErrorBoundary', () => {
  it('renders the same crash fallback for a router-level failure (e.g. a failed loader on the AppShell route)', async () => {
    const router = createMemoryRouter(
      [
        {
          path: '/',
          element: <div>should never render</div>,
          errorElement: <RouteErrorBoundary component="App shell" />,
          loader: () => {
            throw new Error('loader exploded')
          },
        },
      ],
      { initialEntries: ['/'] },
    )

    render(<RouterProvider router={router} />)

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Something broke in App shell.',
    )
  })
})
