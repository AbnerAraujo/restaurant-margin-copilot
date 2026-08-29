import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { THEME_STORAGE_KEY, ThemeProvider, useTheme } from './theme'

/**
 * jsdom implements no `matchMedia` of its own (the same gap `SplashScreen`'s
 * `prefers-reduced-motion` check already guards against with a `typeof
 * window.matchMedia === 'function'` check) — a `MediaQueryList` stand-in with
 * a real `addEventListener`/`removeEventListener` pair is required to
 * exercise "system" mode and its live-update listener at all.
 */
function stubMatchMedia(initiallyMatches: boolean) {
  let matches = initiallyMatches
  const listeners = new Set<(event: { matches: boolean }) => void>()
  const mediaQueryList = {
    get matches() {
      return matches
    },
    media: '(prefers-color-scheme: dark)',
    addEventListener: (_event: string, handler: (event: { matches: boolean }) => void) => {
      listeners.add(handler)
    },
    removeEventListener: (_event: string, handler: (event: { matches: boolean }) => void) => {
      listeners.delete(handler)
    },
  }
  vi.stubGlobal(
    'matchMedia',
    vi.fn().mockReturnValue(mediaQueryList),
  )
  return {
    setMatches(next: boolean) {
      matches = next
      listeners.forEach((handler) => handler({ matches }))
    },
  }
}

function TestConsumer() {
  const { preference, resolvedTheme, setPreference } = useTheme()
  return (
    <div>
      <p data-testid="preference">{preference}</p>
      <p data-testid="resolved">{resolvedTheme}</p>
      <button onClick={() => setPreference('light')}>light</button>
      <button onClick={() => setPreference('dark')}>dark</button>
      <button onClick={() => setPreference('system')}>system</button>
    </div>
  )
}

function renderWithProvider() {
  return render(
    <ThemeProvider>
      <TestConsumer />
    </ThemeProvider>,
  )
}

describe('ThemeProvider / useTheme', () => {
  beforeEach(() => {
    window.localStorage.clear()
    document.documentElement.classList.remove('dark')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    document.documentElement.classList.remove('dark')
  })

  it('defaults to "system" when nothing is stored, and does not crash without matchMedia', () => {
    renderWithProvider()

    expect(screen.getByTestId('preference')).toHaveTextContent('system')
    // No matchMedia stub in this test — the "no function" guard must fall
    // back to light rather than throw.
    expect(screen.getByTestId('resolved')).toHaveTextContent('light')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })

  it('applies the .dark class and persists the choice when "dark" is picked', async () => {
    const user = userEvent.setup()
    renderWithProvider()

    await user.click(screen.getByRole('button', { name: 'dark' }))

    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(screen.getByTestId('resolved')).toHaveTextContent('dark')
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark')
  })

  it('removes the .dark class and persists the choice when "light" is picked', async () => {
    const user = userEvent.setup()
    renderWithProvider()

    await user.click(screen.getByRole('button', { name: 'dark' }))
    await user.click(screen.getByRole('button', { name: 'light' }))

    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe('light')
  })

  it('resolves "system" from the OS preference via matchMedia', async () => {
    stubMatchMedia(true)
    const user = userEvent.setup()
    renderWithProvider()

    await user.click(screen.getByRole('button', { name: 'system' }))

    expect(screen.getByTestId('resolved')).toHaveTextContent('dark')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe('system')
  })

  it('follows a live OS preference change while "system" is active', async () => {
    const media = stubMatchMedia(false)
    const user = userEvent.setup()
    renderWithProvider()

    await user.click(screen.getByRole('button', { name: 'system' }))
    expect(screen.getByTestId('resolved')).toHaveTextContent('light')

    act(() => {
      media.setMatches(true)
    })

    expect(screen.getByTestId('resolved')).toHaveTextContent('dark')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })

  it('does not react to a live OS preference change once switched away from "system"', async () => {
    const media = stubMatchMedia(false)
    const user = userEvent.setup()
    renderWithProvider()

    await user.click(screen.getByRole('button', { name: 'system' }))
    await user.click(screen.getByRole('button', { name: 'light' }))

    act(() => {
      media.setMatches(true)
    })

    expect(screen.getByTestId('resolved')).toHaveTextContent('light')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })

  it('reads a previously stored preference on mount', () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, 'dark')

    renderWithProvider()

    expect(screen.getByTestId('preference')).toHaveTextContent('dark')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })

  it('falls back to "system" for a hand-edited, invalid stored value', () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, 'purple')

    renderWithProvider()

    expect(screen.getByTestId('preference')).toHaveTextContent('system')
  })

  it('throws when useTheme is called outside a ThemeProvider', () => {
    // Suppress the expected React error-boundary console noise for this one
    // assertion; the whole point of the test is that it throws.
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => undefined)

    expect(() => render(<TestConsumer />)).toThrow(
      'useTheme must be used within a ThemeProvider',
    )

    consoleError.mockRestore()
  })
})
