import { useCallback, useState } from 'react'

import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Outlet, Route, Routes } from 'react-router-dom'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'

import type { ShellOutletContext } from '@/components/Shell/AppShell'
import CostPanel, { type CostInteraction } from '@/components/CostPanel/CostPanel'
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
 * Stands in for `AppShell`: provides the same `ShellOutletContext` shape
 * (interactions + logInteractions) via a real `<Outlet>`, plus the same
 * `CostPanel` mounted at the shell root, so `AskPage`'s `useShellOutletContext`
 * call and its cost-reporting side effect are exercised exactly as they run
 * in the real app instead of being stubbed away.
 */
function ShellHarness() {
  const [interactions, setInteractions] = useState<CostInteraction[]>([])
  const logInteractions = useCallback((newInteractions: CostInteraction[]) => {
    setInteractions((previous) => [...previous, ...newInteractions])
  }, [])

  return (
    <>
      <Outlet
        context={{ interactions, logInteractions } satisfies ShellOutletContext}
      />
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
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders the chat panel with its seeded conversation', () => {
    renderAskPage()

    expect(
      screen.getByRole('heading', { name: /ask about your margin/i }),
    ).toBeInTheDocument()
    expect(
      screen.getByText(/today's reconciled margin was \$612\.40/i),
    ).toBeInTheDocument()
  })

  it('starts the shared session cost at zero — it does not replay the seed conversation\'s cost on mount', () => {
    renderAskPage()

    expect(screen.getByText(formatUsd(0))).toBeInTheDocument()
  })

  it('reports a full gate+explain pair to the shell cost total after a new grounded answer', async () => {
    mockAskResponse({
      status: 'answered',
      answer_text: 'Margin for that period was $1,842.60.',
      provenance_refs: ['fixtures/daily_reconciliation.csv:18'],
      interactions: [
        { model_used: 'claude-haiku-4-5', input_tokens: 420, output_tokens: 18, estimated_cost_usd: HAIKU_GATE_USD, latency_ms: 310 },
        { model_used: 'claude-sonnet-5', input_tokens: 1180, output_tokens: 240, estimated_cost_usd: SONNET_EXPLAIN_USD, latency_ms: 1420 },
      ],
    })
    const user = userEvent.setup()
    renderAskPage()

    const costTrigger = screen.getByRole('button', { name: /session cost/i })

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
})
