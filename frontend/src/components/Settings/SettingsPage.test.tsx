import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { THEME_STORAGE_KEY, ThemeProvider } from '@/lib/theme'
import SettingsPage from './SettingsPage'

// Every real render of this page sits under App's ThemeProvider; this
// mirrors that so ThemeToggle's useTheme() call has the context it needs
// instead of throwing "must be used within a ThemeProvider".
function renderSettingsPage() {
  return render(
    <ThemeProvider>
      <SettingsPage />
    </ThemeProvider>,
  )
}

describe('SettingsPage', () => {
  beforeEach(() => {
    window.localStorage.clear()
    document.documentElement.classList.remove('dark')
  })

  it('renders the page header and states plainly that nothing here is server-side config', () => {
    renderSettingsPage()

    expect(screen.getByRole('heading', { name: 'Settings' })).toBeInTheDocument()
    expect(screen.getByText('Single owner, no accounts')).toBeInTheDocument()
    expect(screen.getByText('Nothing here is stored server-side')).toBeInTheDocument()
  })

  it('toggles the real fullscreen control and reflects the resulting state', async () => {
    // jsdom has no Fullscreen implementation of its own — `fullscreenElement`
    // must be defined explicitly (starting `null`, exactly like a real
    // browser before entering fullscreen) or the hook's own
    // `document.fullscreenElement !== null` check reads `undefined !== null`
    // as true and starts the control in the wrong state.
    Object.defineProperty(document, 'fullscreenElement', {
      value: null,
      configurable: true,
    })
    const requestFullscreen = vi.fn().mockImplementation(() => {
      Object.defineProperty(document, 'fullscreenElement', {
        value: document.documentElement,
        configurable: true,
      })
      document.dispatchEvent(new Event('fullscreenchange'))
      return Promise.resolve()
    })
    document.documentElement.requestFullscreen = requestFullscreen

    const user = userEvent.setup()
    renderSettingsPage()

    const button = screen.getByRole('button', { name: /enter full screen/i })
    expect(button).toHaveAttribute('aria-pressed', 'false')

    await user.click(button)

    expect(requestFullscreen).toHaveBeenCalledTimes(1)
    expect(
      await screen.findByRole('button', { name: /exit full screen/i }),
    ).toHaveAttribute('aria-pressed', 'true')
  })

  it('links to the real, hosted docs named in the README rather than a fabricated version string', () => {
    renderSettingsPage()

    expect(
      screen.getByRole('link', { name: /source \(github\)/i }),
    ).toHaveAttribute('href', 'https://github.com/AbnerAraujo/restaurant-margin-copilot')
    expect(
      screen.getByRole('link', { name: /live presentation/i }),
    ).toHaveAttribute(
      'href',
      'https://claude.ai/code/artifact/17a46fdf-c587-45c6-b1d6-904f1a03bc70',
    )
    expect(
      screen.getByRole('link', { name: /live architecture diagram/i }),
    ).toHaveAttribute(
      'href',
      'https://claude.ai/code/artifact/dcda16f7-44d7-4160-8f72-d8593f432441',
    )
    expect(
      screen.getByRole('link', { name: /live api docs/i }),
    ).toHaveAttribute(
      'href',
      'https://claude.ai/code/artifact/6781bd96-bfa1-4fd7-821a-fe35cd3ac764',
    )
  })

  it('names real, specific future capabilities as not built, never a fake toggle', () => {
    renderSettingsPage()

    expect(screen.getByText('Not built')).toBeInTheDocument()
    expect(
      screen.getByText(/accounts and authentication/i),
    ).toBeInTheDocument()
    expect(
      screen.getByText(/per-restaurant data isolation/i),
    ).toBeInTheDocument()
    // Notification preferences and an export button still don't exist
    // anywhere in this backend, so neither should render here. Theme is
    // deliberately NOT in this list any more — it is a real control now,
    // asserted on its own below, not a stub this panel should disclaim.
    expect(screen.queryByRole('switch')).not.toBeInTheDocument()
    expect(screen.queryByText(/export/i)).not.toBeInTheDocument()
  })

  it('renders a real, working Light/Dark/System theme control', async () => {
    const user = userEvent.setup()
    renderSettingsPage()

    const group = screen.getByRole('radiogroup', { name: 'Theme' })
    const light = screen.getByRole('radio', { name: 'Light' })
    const dark = screen.getByRole('radio', { name: 'Dark' })
    const system = screen.getByRole('radio', { name: 'System' })
    expect(group).toBeInTheDocument()
    // Defaults to "system" (no stored preference in this browser yet).
    expect(system).toHaveAttribute('aria-checked', 'true')
    expect(light).toHaveAttribute('aria-checked', 'false')

    await user.click(dark)

    expect(dark).toHaveAttribute('aria-checked', 'true')
    expect(system).toHaveAttribute('aria-checked', 'false')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark')

    await user.click(light)

    expect(light).toHaveAttribute('aria-checked', 'true')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe('light')
  })
})
