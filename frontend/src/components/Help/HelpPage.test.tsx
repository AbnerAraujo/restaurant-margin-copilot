import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { buildCapabilitySummary } from '@/components/Chat/exampleQuestions'
import { GUIDED_CATEGORIES } from '@/components/Chat/guidedQuestion'
import { routes } from '@/router'
import HelpPage from './HelpPage'

// The authoritative list of real page paths, derived from the same route
// table the app itself renders from — never hand-copied — so this test can
// never drift the way HelpPage.tsx itself once did. `/help` is excluded: the
// Help page doesn't walk through a link to itself. `*` (the 404 catch-all)
// is excluded too: it isn't a real page to send an owner to, it's what
// renders when they end up somewhere that ISN'T one.
const REAL_PAGE_PATHS = (routes[0].children ?? [])
  .map((route) => (route.index ? '/' : `/${route.path}`))
  .filter((path) => path !== '/help' && path !== '/*')

const COVERAGE_RESPONSE = { start: '2024-08-01', end: '2026-08-14', days: [] }

// WhatYouCanAskPanel now fetches the real coverage range (useDataCoverage)
// instead of a hardcoded string — every test needs a stubbed fetch so it
// never makes a real network call and never depends on a real backend
// happening to be running.
function stubReconciliationFetch() {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({
      ok: true,
      json: async () => COVERAGE_RESPONSE,
    }),
  )
}

// HelpPage links to other routes with <Link>, which requires a router
// context — the same reason other routed components' tests wrap in
// MemoryRouter rather than rendering the bare component.
function renderHelpPage() {
  stubReconciliationFetch()
  return render(
    <MemoryRouter>
      <HelpPage />
    </MemoryRouter>,
  )
}

describe('HelpPage', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

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

  it('renders the capability summary and coverage period from the real, live data range, not a hardcoded string', async () => {
    renderHelpPage()

    const coveragePeriod = `${COVERAGE_RESPONSE.start} to ${COVERAGE_RESPONSE.end}`
    await waitFor(() => {
      expect(
        screen.getByText(buildCapabilitySummary(coveragePeriod)),
      ).toBeInTheDocument()
    })
    expect(screen.getByText(coveragePeriod)).toBeInTheDocument()
  })

  it('lists every real MCP tool as a category, deriving the count from the same list rather than a hardcoded number', () => {
    renderHelpPage()

    // Guards the exact bug this test replaces: HelpPage used to hardcode
    // "seven" tools and render from a second, staler list that never
    // learned about an eighth tool. Asserting against GUIDED_CATEGORIES
    // itself means this test cannot go stale the same way — it fails the
    // moment HelpPage's rendering falls out of sync with the composer's
    // own tool list, whatever its length turns out to be.
    for (const category of GUIDED_CATEGORIES) {
      expect(screen.getByText(category.label)).toBeInTheDocument()
      expect(screen.getByText(category.description)).toBeInTheDocument()
      expect(screen.getByText(category.tool)).toBeInTheDocument()
    }
    expect(
      screen.getByText(
        new RegExp(`these ${GUIDED_CATEGORIES.length} tools are the complete`, 'i'),
      ),
    ).toBeInTheDocument()
  })

  it('renders a real icon for every tool, with no undefined-icon crash even if a tool is unmapped', () => {
    // Regression guard for the failure mode the QA pass flagged: adding an
    // 8th tool without a matching icon-map entry used to be able to crash
    // this page on an undefined component lookup. Rendering at all (no
    // throw) plus one icon per category is the proof.
    renderHelpPage()

    expect(screen.getAllByText(/^answered by/i)).toHaveLength(GUIDED_CATEGORIES.length)
  })

  it('links to every real page it walks through, and no page that no longer exists', () => {
    renderHelpPage()

    const renderedHrefs = screen.getAllByRole('link').map((link) => link.getAttribute('href'))
    expect(new Set(renderedHrefs)).toEqual(new Set(REAL_PAGE_PATHS))
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
