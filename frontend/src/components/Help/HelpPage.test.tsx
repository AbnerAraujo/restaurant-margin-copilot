import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import {
  CAPABILITY_SUMMARY,
  COVERAGE_PERIOD,
  EXAMPLE_QUESTIONS,
} from '@/components/Chat/exampleQuestions'
import HelpPage from './HelpPage'

// HelpPage links to other routes with <Link>, which requires a router
// context — the same reason other routed components' tests wrap in
// MemoryRouter rather than rendering the bare component.
function renderHelpPage() {
  return render(
    <MemoryRouter>
      <HelpPage />
    </MemoryRouter>,
  )
}

describe('HelpPage', () => {
  it('renders the page header', () => {
    renderHelpPage()

    expect(screen.getByRole('heading', { name: 'Help' })).toBeInTheDocument()
  })

  it('states the real benefits from the README framing: deterministic engine and refuse-rather-than-guess', () => {
    renderHelpPage()

    expect(
      screen.getByText(/every number is computed in go, never by a model/i),
    ).toBeInTheDocument()
    expect(
      screen.getByText(/refuses rather than guesses/i),
    ).toBeInTheDocument()
    expect(
      screen.getByText(/discrepancies are caught, not buried/i),
    ).toBeInTheDocument()
  })

  it('renders the capability summary and coverage period from the single source of truth, not a hand-typed copy', () => {
    renderHelpPage()

    expect(screen.getByText(CAPABILITY_SUMMARY)).toBeInTheDocument()
    expect(screen.getByText(COVERAGE_PERIOD)).toBeInTheDocument()
  })

  it('lists every example question and its answering tool, so it can never drift from the chat surface', () => {
    renderHelpPage()

    for (const question of EXAMPLE_QUESTIONS) {
      expect(
        screen.getByText(`“${question.text}”`),
      ).toBeInTheDocument()
      expect(screen.getByText(question.tool)).toBeInTheDocument()
    }
  })

  it('links to every real page it walks through', () => {
    renderHelpPage()

    expect(screen.getByRole('link', { name: /home/i })).toHaveAttribute('href', '/')
    expect(screen.getByRole('link', { name: /^ask/i })).toHaveAttribute('href', '/ask')
    expect(screen.getByRole('link', { name: /^promotions/i })).toHaveAttribute(
      'href',
      '/promotions',
    )
    expect(screen.getByRole('link', { name: /^points/i })).toHaveAttribute('href', '/points')
    expect(screen.getByRole('link', { name: /^platforms/i })).toHaveAttribute(
      'href',
      '/platforms',
    )
    expect(screen.getByRole('link', { name: /upload costs/i })).toHaveAttribute(
      'href',
      '/upload',
    )
  })

  it('describes the real points-payment option on Promotions', () => {
    renderHelpPage()

    expect(
      screen.getByText(/pay its spend with your earned steward points instead of cash/i),
    ).toBeInTheDocument()
  })

  it('explains the refusal discipline instead of leaving a first-time refusal unexplained', () => {
    renderHelpPage()

    expect(screen.getByText('Why it refuses sometimes')).toBeInTheDocument()
    expect(
      screen.getByText(/answerable, ambiguous, or unanswerable/i),
    ).toBeInTheDocument()
    // The phrase appears twice by design — once as a benefit bullet, once
    // restated in the refusal section itself — so assert presence rather
    // than uniqueness.
    expect(
      screen.getAllByText(/confidently wrong margin figure is worse/i).length,
    ).toBeGreaterThan(0)
  })

  it('never invents a support/contact mechanism, screenshot, or video that does not exist', () => {
    renderHelpPage()

    expect(screen.queryByText(/contact support/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/support ticket/i)).not.toBeInTheDocument()
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
    expect(screen.queryByText(/watch a video/i)).not.toBeInTheDocument()
  })
})
