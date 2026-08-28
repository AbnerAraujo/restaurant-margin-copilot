import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import BadgeDisplay, { type ReconciliationBadge } from './BadgeDisplay'

// Realistic mocked badge-state data, shaped the way `GET /api/badges` would
// return Reconciliation-category badges evaluated off
// `DailyReconciliation.discrepancy_flags` (see
// specs/001-margin-reconciliation-qa/data-model.md and tasks.md T032).
const cleanCloseBadge: ReconciliationBadge = {
  id: '2026-08-21-clean_close',
  type: 'clean_close',
  date: '2026-08-21',
}

const discrepancyCatcherBadge: ReconciliationBadge = {
  id: '2026-08-25-discrepancy_catcher',
  type: 'discrepancy_catcher',
  date: '2026-08-25',
  detail: 'Caught 1 duplicate charge in the delivery export',
}

describe('BadgeDisplay', () => {
  it('renders nothing when no badges have fired', () => {
    const { container } = render(<BadgeDisplay badges={[]} />)

    expect(container).toBeEmptyDOMElement()
  })

  it('renders a Clean Close badge as a compact pill', () => {
    render(<BadgeDisplay badges={[cleanCloseBadge]} />)

    expect(screen.getByText('Clean Close')).toBeInTheDocument()
    expect(
      screen.getByLabelText(/Clean Close, Aug 21/i),
    ).toBeInTheDocument()
  })

  it('renders a Discrepancy Catcher badge with its detail as a single-line banner', () => {
    render(<BadgeDisplay badges={[discrepancyCatcherBadge]} />)

    expect(screen.getByText('Discrepancy Catcher')).toBeInTheDocument()
    expect(
      screen.getByText('Caught 1 duplicate charge in the delivery export'),
    ).toBeInTheDocument()
    expect(
      screen.getByLabelText(
        /Discrepancy Catcher, Aug 25: Caught 1 duplicate charge/i,
      ),
    ).toBeInTheDocument()
  })

  it('never uses the destructive/danger tone for a caught discrepancy — it is a quiet catch, not a failure', () => {
    render(<BadgeDisplay badges={[discrepancyCatcherBadge]} />)

    const banner = screen.getByLabelText(/Discrepancy Catcher/i)
    expect(banner.className).toContain('warning')
    expect(banner.className).not.toContain('destructive')
  })

  it('never renders loud/animated affordances — no alert role, no animation classes', () => {
    render(<BadgeDisplay badges={[cleanCloseBadge, discrepancyCatcherBadge]} />)

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    for (const el of document.querySelectorAll('li > *')) {
      expect(el.className).not.toMatch(/animate-/)
    }
  })

  it('renders multiple badges in the order given', () => {
    render(<BadgeDisplay badges={[cleanCloseBadge, discrepancyCatcherBadge]} />)

    const items = screen.getAllByRole('listitem')
    expect(items).toHaveLength(2)
    expect(items[0]).toHaveTextContent('Clean Close')
    expect(items[1]).toHaveTextContent('Discrepancy Catcher')
  })

  it('falls back to the raw string when a badge date is not parseable', () => {
    const malformed: ReconciliationBadge = {
      id: 'bad-date-clean_close',
      type: 'clean_close',
      date: 'not-a-date',
    }

    render(<BadgeDisplay badges={[malformed]} />)

    expect(screen.getByLabelText(/Clean Close, not-a-date/i)).toBeInTheDocument()
  })
})
