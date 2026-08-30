import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Outlet, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { ShellOutletContext } from '@/components/Shell/AppShell'
import CostPanel from '@/components/CostPanel/CostPanel'
import { useSpendLedger } from '@/lib/useSpendLedger'
import AskPage from './AskPage'

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

/**
 * Like {@link mockAskResponse}, but a different `/api/ask` body per call, in
 * order. Routed by URL rather than by call count: `ChatPanel` also fetches
 * `GET /api/reconciliation` on mount (`useDataCoverage`), and that call
 * would otherwise silently steal one of the queued `/api/ask` bodies and
 * scramble which response answers which question.
 */
function mockAskResponses(bodies: Record<string, unknown>[]) {
  let nextIndex = 0
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const url =
        typeof input === 'string'
          ? input
          : input instanceof URL
            ? input.toString()
            : input.url
      if (!url.includes('/api/ask')) {
        return { ok: true, json: async () => ({ start: '2024-08-01', end: '2026-08-14' }) }
      }
      const body = bodies[Math.min(nextIndex, bodies.length - 1)]
      nextIndex += 1
      return { ok: true, json: async () => body }
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

  /**
   * The reported repro, exactly: ask a question, reload, ask a DIFFERENT
   * question. Before the fix, `ChatPanel`/`AskPage` minted message ids from
   * a `let messageSequence = 0` counter at module level, which reset to 0
   * on every reload — so the first message asked after a reload got the
   * identical id as the first message asked before it, and
   * `recordSpend`'s anti-double-billing dedupe (keyed on that id) silently
   * discarded the second question's real, billed cost. See
   * `lib/id.test.ts` for proof the replacement id generator does not
   * collide across a reload, and `lib/chatStorage.test.ts` for proof the
   * ledger correctly sums two non-colliding ids — this test is the
   * user-facing assembly of both: the pill must actually move.
   */
  it('records the second question\'s spend after a reload asks something new', async () => {
    mockAskResponses([
      {
        status: 'answered',
        answer_text: 'Margin on 2026-08-07 was $375.82.',
        provenance_refs: [],
        interactions: [
          { model_used: 'claude-sonnet-5', input_tokens: 400, output_tokens: 20, estimated_cost_usd: HAIKU_GATE_USD, latency_ms: 300 },
        ],
      },
      {
        status: 'answered',
        answer_text: 'Margin on 2026-08-08 was $410.15.',
        provenance_refs: [],
        interactions: [
          { model_used: 'claude-sonnet-5', input_tokens: 900, output_tokens: 150, estimated_cost_usd: SONNET_EXPLAIN_USD, latency_ms: 900 },
        ],
      },
    ])
    const user = userEvent.setup()
    const first = renderAskPage()

    const input = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(input, 'How did we do on 2026-08-07?{Enter}')
    expect(
      await screen.findByText(formatUsd(HAIKU_GATE_USD)),
    ).toBeInTheDocument()

    // A reload: everything in memory goes, only storage survives. Only the
    // spend ledger and thread history in `localStorage` carry over — a real
    // reload also re-evaluates every module, which is exactly the step a
    // plain unmount/remount in the same test process cannot reproduce (see
    // `lib/id.test.ts`, which exercises that half of the fix directly with
    // `vi.resetModules()`).
    first.unmount()
    const second = renderAskPage()

    const secondInput = screen.getByRole('textbox', {
      name: /ask a question about your margin/i,
    })
    await user.type(secondInput, 'How did we do on 2026-08-08?{Enter}')

    expect(
      await screen.findByText('Margin on 2026-08-08 was $410.15.'),
    ).toBeInTheDocument()

    // The pill must reflect BOTH questions' cost, not just the second one
    // (which would hide the first) and not just the first (the regression:
    // the second question's real, billed cost silently dropped).
    const expectedTotal = HAIKU_GATE_USD + SONNET_EXPLAIN_USD
    expect(await screen.findByText(formatUsd(expectedTotal))).toBeInTheDocument()

    second.unmount()
  })
})
