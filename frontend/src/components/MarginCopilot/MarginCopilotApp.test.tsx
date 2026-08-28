import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeAll, describe, expect, it } from 'vitest'

import MarginCopilotApp from './MarginCopilotApp'

// jsdom has no ResizeObserver; Radix's ScrollArea (inside ChatPanel) needs
// one to mount. Scoped to this file like ChatPanel's own test.
beforeAll(() => {
  if (!('ResizeObserver' in globalThis)) {
    class ResizeObserverStub {
      observe() {}
      unobserve() {}
      disconnect() {}
    }
    globalThis.ResizeObserver = ResizeObserverStub
  }
})

// Mirrors the two cost constants MarginCopilotApp seeds/records with, so
// expected totals are computed the same way the component computes them —
// never by re-parsing an already-rounded `$0.012`-style display value,
// which would compound rounding error across assertions.
const HAIKU_GATE_USD = 0.00051
const SONNET_EXPLAIN_USD = 0.00476
const SEEDED_TOTAL_USD = 2 * (HAIKU_GATE_USD + SONNET_EXPLAIN_USD) + 2 * HAIKU_GATE_USD

function formatUsd(amount: number): string {
  return `$${amount.toFixed(3)}`
}

describe('MarginCopilotApp', () => {
  it("renders today's margin with its provenance citation and a Clean Close badge", () => {
    render(<MarginCopilotApp />)

    const summary = screen.getByLabelText(/today's reconciliation summary/i)
    expect(within(summary).getByText('$612.40')).toBeInTheDocument()
    expect(within(summary).getByText('Clean Close')).toBeInTheDocument()
    expect(
      within(summary).getByRole('button', { name: '2 sources' }),
    ).toBeInTheDocument()
  })

  it('renders the chat panel with its seeded conversation alongside the summary', () => {
    render(<MarginCopilotApp />)

    expect(
      screen.getByRole('heading', { name: /ask about your margin/i }),
    ).toBeInTheDocument()
    expect(
      screen.getByText(/today's reconciled margin was \$612\.40/i),
    ).toBeInTheDocument()
  })

  it('shows a running session cost seeded from the conversation already on screen', () => {
    render(<MarginCopilotApp />)

    // 2x (haiku gate + sonnet explain) + 2x haiku-gate-only, per the seeded
    // thread's two answers, one clarification, and one refusal.
    expect(screen.getByText(formatUsd(SEEDED_TOTAL_USD))).toBeInTheDocument()
  })

  it('grows the running cost total by a full gate+explain pair after a new grounded answer', async () => {
    const user = userEvent.setup()
    render(<MarginCopilotApp />)

    const costTrigger = screen.getByRole('button', { name: /session cost/i })

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'How did we do yesterday?')
    await user.click(screen.getByRole('button', { name: /send question/i }))

    const expectedAfter = SEEDED_TOTAL_USD + HAIKU_GATE_USD + SONNET_EXPLAIN_USD
    expect(
      await within(costTrigger).findByText(formatUsd(expectedAfter)),
    ).toBeInTheDocument()
  })

  it('grows the running cost total by a gate-only step after a clarification fires', async () => {
    const user = userEvent.setup()
    render(<MarginCopilotApp />)

    const costTrigger = screen.getByRole('button', { name: /session cost/i })

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'How was the weekend last month?')
    await user.click(screen.getByRole('button', { name: /send question/i }))

    const expectedAfter = SEEDED_TOTAL_USD + HAIKU_GATE_USD
    expect(
      await within(costTrigger).findByText(formatUsd(expectedAfter)),
    ).toBeInTheDocument()
  })
})
