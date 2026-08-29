import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import SettingsPage from './SettingsPage'

describe('SettingsPage', () => {
  it('renders the page header and states plainly that nothing here is server-side config', () => {
    render(<SettingsPage />)

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
    render(<SettingsPage />)

    const button = screen.getByRole('button', { name: /enter full screen/i })
    expect(button).toHaveAttribute('aria-pressed', 'false')

    await user.click(button)

    expect(requestFullscreen).toHaveBeenCalledTimes(1)
    expect(
      await screen.findByRole('button', { name: /exit full screen/i }),
    ).toHaveAttribute('aria-pressed', 'true')
  })

  it('links to the real, hosted docs named in the README rather than a fabricated version string', () => {
    render(<SettingsPage />)

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
    render(<SettingsPage />)

    expect(screen.getByText('Not built')).toBeInTheDocument()
    expect(
      screen.getByText(/accounts and authentication/i),
    ).toBeInTheDocument()
    expect(
      screen.getByText(/per-restaurant data isolation/i),
    ).toBeInTheDocument()
    // No dark-mode switch, notification preferences, or export button — none
    // of those exist anywhere in this backend, so none should render here.
    expect(screen.queryByText(/dark mode/i)).not.toBeInTheDocument()
    expect(screen.queryByRole('switch')).not.toBeInTheDocument()
    expect(screen.queryByText(/export/i)).not.toBeInTheDocument()
  })
})
