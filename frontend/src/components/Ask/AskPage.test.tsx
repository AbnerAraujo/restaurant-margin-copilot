import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Outlet, Route, Routes } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

import type { ShellOutletContext } from '@/components/Shell/AppShell'
import CostPanel from '@/components/CostPanel/CostPanel'
import { useSpendLedger } from '@/lib/useSpendLedger'
import AskPage from './AskPage'

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

const HAIKU_GATE_USD = 0.00051
const SONNET_EXPLAIN_USD = 0.00476

function formatUsd(amount: number): string {
  return `$${amount.toFixed(3)}`
}

/**
 * Stands in for `AppShell`, using the same durable spend ledger the real
 * shell now reads (`useSpendLedger`) rather than a local in-memory array.
 * That is the point of the harness: cost reaches the pill by exactly the
 * path it takes in the app — attached to the assistant message, written to
 * the ledger by `ChatPanel`'s commit — so a regression that reintroduced
 * the ephemeral side-effect reporting would fail here.
 */
function ShellHarness() {
  const interactions = useSpendLedger()

  return (
    <>
      <Outlet context={{ interactions } satisfies ShellOutletContext} />
      <CostPanel interactions={interactions} />
    </>
  )
}

function renderAskPage() {
  return render(
    <MemoryRouter initialEntries={['/ask']}>
      <Routes>
        <Route element={<ShellHarness />}>
          <Route path="/ask" element={<AskPage />} />
        </Route>
      </Routes>
    </MemoryRouter>,
  )
}

function mockAskResponse(body: Record<string, unknown>) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({
      ok: true,
      json: async () => body,
    }),
  )
}

describe('AskPage', () => {
  beforeEach(() => {
    // The chat thread and the spend ledger are both durable now, so a test
    // that did not clear them would inherit the previous test's answers and
    // the previous test's total.
    window.localStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders the chat panel with an empty conversation, never the demo seed', () => {
    renderAskPage()

    expect(
      screen.getByRole('heading', { name: /ask about your margin/i }),
    ).toBeInTheDocument()
    // The live surface must not display ChatPanel's fabricated demo thread:
    // those figures have no provenance behind them and are styled exactly
    // like real answers.
    expect(
      screen.queryByText(/today's reconciled margin was \$612\.40/i),
    ).not.toBeInTheDocument()
    expect(
      screen.getByText(/ask anything about your reconciled numbers/i),
    ).toBeInTheDocument()
  })

  it('offers starter questions that submit straight to the backend', async () => {
    mockAskResponse({
      status: 'answered',
      answer_text: 'Margin on 2026-08-07 was $375.82.',
      provenance_refs: [],
      interactions: [],
    })
    const user = userEvent.setup()
    renderAskPage()

    await user.click(
      screen.getByRole('button', { name: /how did we do on 2026-08-07/i }),
    )

    expect(
      await screen.findByText('Margin on 2026-08-07 was $375.82.'),
    ).toBeInTheDocument()
  })

  it('starts the shared model-spend total at zero', () => {
    renderAskPage()

    expect(screen.getByText(formatUsd(0))).toBeInTheDocument()
  })

  it('reports a full gate+explain pair to the shell cost total after a new grounded answer', async () => {
    mockAskResponse({
      status: 'answered',
      answer_text: 'Margin for that period was $1,842.60.',
      provenance_refs: ['data/live/daily_reconciliation.csv:18'],
      interactions: [
        { model_used: 'claude-haiku-4-5', input_tokens: 420, output_tokens: 18, estimated_cost_usd: HAIKU_GATE_USD, latency_ms: 310 },
        { model_used: 'claude-sonnet-5', input_tokens: 1180, output_tokens: 240, estimated_cost_usd: SONNET_EXPLAIN_USD, latency_ms: 1420 },
      ],
    })
    const user = userEvent.setup()
    renderAskPage()

    const costTrigger = screen.getByRole('button', { name: /model spend/i })

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'How did we do yesterday?')
    await user.click(screen.getByRole('button', { name: /send question/i }))

    const expectedAfter = HAIKU_GATE_USD + SONNET_EXPLAIN_USD
    expect(
      await screen.findByText(formatUsd(expectedAfter)),
    ).toBeInTheDocument()
    expect(costTrigger).toBeInTheDocument()
  })

  it('reports a gate-only cost to the shell total after a clarification fires', async () => {
    mockAskResponse({
      status: 'clarification_needed',
      clarifying_question: '"Weekend" could mean Friday–Sunday or just Saturday–Sunday — which did you mean?',
      interactions: [
        { model_used: 'claude-haiku-4-5', input_tokens: 420, output_tokens: 18, estimated_cost_usd: HAIKU_GATE_USD, latency_ms: 310 },
      ],
    })
    const user = userEvent.setup()
    renderAskPage()

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'How was the weekend last month?')
    await user.click(screen.getByRole('button', { name: /send question/i }))

    expect(
      await screen.findByText(formatUsd(HAIKU_GATE_USD)),
    ).toBeInTheDocument()
  })

  /**
   * The reported repro: the chat thread is deliberately durable, but the
   * running total that paid for it used to be per-mount React state, so a
   * reload showed $0.000 above answers that had demonstrably cost money —
   * and two tabs showed two different totals, neither of them real.
   */
  it('keeps the model-spend total consistent with the conversation across a reload', async () => {
    mockAskResponse({
      status: 'answered',
      answer_text: 'Margin for that period was $1,842.60.',
      provenance_refs: ['data/live/daily_reconciliation.csv:18'],
      interactions: [
        { model_used: 'claude-sonnet-5', input_tokens: 420, output_tokens: 18, estimated_cost_usd: HAIKU_GATE_USD, latency_ms: 310 },
        { model_used: 'claude-sonnet-5', input_tokens: 1180, output_tokens: 240, estimated_cost_usd: SONNET_EXPLAIN_USD, latency_ms: 1420 },
      ],
    })
    const user = userEvent.setup()
    const first = renderAskPage()

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'How did we do yesterday?{Enter}')

    const expectedTotal = HAIKU_GATE_USD + SONNET_EXPLAIN_USD
    expect(await screen.findByText(formatUsd(expectedTotal))).toBeInTheDocument()

    // A reload: everything in memory goes, only storage survives.
    first.unmount()
    renderAskPage()

    // The answer is still on screen...
    expect(
      await screen.findByText('Margin for that period was $1,842.60.'),
    ).toBeInTheDocument()
    // ...so the total that produced it has to be too, and it must not have
    // been counted a second time by the remount either.
    expect(screen.getByText(formatUsd(expectedTotal))).toBeInTheDocument()
  })
})
