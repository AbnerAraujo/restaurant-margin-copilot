import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import App from './App'

// The shell renders two nav landmarks with the same accessible name: the
// `lg`+ sidebar (`Sidebar`) and the `< lg` top bar (`MobileNavBar`) — only
// one is ever visually shown at a time via a CSS breakpoint, which jsdom
// (no stylesheet loaded in tests) doesn't evaluate. Scope to the first
// (the desktop sidebar, first in DOM order) for interaction assertions.
function getDesktopNav() {
  const [desktopNav] = screen.getAllByRole('navigation', {
    name: /primary navigation/i,
  })
  return desktopNav
}

describe('App', () => {
  it('renders the app shell at the root route: sidebar nav plus the home page', () => {
    render(<App />)

    const nav = getDesktopNav()
    expect(within(nav).getByText('Home')).toBeInTheDocument()
    // HomePage's real capability tiles, not shell markup.
    expect(
      screen.getByRole('link', { name: /ask about your margin/i }),
    ).toBeInTheDocument()
  })

  it('navigates to another route when its sidebar nav item is clicked', async () => {
    const user = userEvent.setup()
    render(<App />)

    const nav = getDesktopNav()
    await user.click(within(nav).getByRole('link', { name: /today's close/i }))

    expect(
      screen.getByRole('heading', { name: /today's close/i }),
    ).toBeInTheDocument()
  })

  it('keeps the session cost pill visible across every route', () => {
    render(<App />)

    expect(screen.getByRole('button', { name: /session cost/i })).toBeInTheDocument()
  })
})
