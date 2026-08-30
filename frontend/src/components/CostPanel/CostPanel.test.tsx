import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import CostPanel, { type CostInteraction } from './CostPanel'

// Realistic mocked data shaped exactly like the QuestionInteraction fields
// this panel aggregates (data-model.md), per FR-008/FR-009.
const ambiguityGateCall: CostInteraction = {
  model_used: 'claude-haiku-4-5',
  input_tokens: 420,
  output_tokens: 18,
  estimated_cost_usd: 0.00051,
  latency_ms: 310,
}

const explanationCall: CostInteraction = {
  model_used: 'claude-sonnet-5',
  input_tokens: 1180,
  output_tokens: 240,
  estimated_cost_usd: 0.00476,
  latency_ms: 1420,
}

describe('CostPanel', () => {
  it('shows a zeroed model-spend total with no interactions yet', () => {
    render(<CostPanel interactions={[]} />)
    expect(screen.getByText('Model spend')).toBeInTheDocument()
    expect(screen.getByText('$0.000')).toBeInTheDocument()
  })

  it('sums estimated_cost_usd across interactions into the glanceable total', () => {
    render(<CostPanel interactions={[ambiguityGateCall, explanationCall]} />)
    // 0.00051 + 0.00476 = 0.00527 -> displayed to 3 decimals
    expect(screen.getByText('$0.005')).toBeInTheDocument()
  })

  it('sums many fractional-cent interactions to the exact expected total', () => {
    // Summed via integer micro-dollars rather than a raw float `reduce`
    // (CostPanel.tsx's `sumCostUsd`) precisely so this kind of sum is exact
    // regardless of how many interactions accumulate over a session — at
    // these real-world magnitudes (llmclient.EstimateCostUSD prices a call
    // at a few hundredths of a cent) a naive float sum happens to render
    // identically at 3 decimals too, so this pins the correct total rather
    // than asserting a visible float-vs-fixed-point divergence.
    const oneHundredCalls: CostInteraction[] = Array.from({ length: 100 }, () => ({
      model_used: 'claude-haiku-4-5',
      input_tokens: 1,
      output_tokens: 1,
      estimated_cost_usd: 0.0001,
      latency_ms: 1,
    }))
    render(<CostPanel interactions={oneHundredCalls} />)
    // 100 * 0.0001 = 0.01 exactly.
    expect(screen.getByText('$0.010')).toBeInTheDocument()
  })

  it('keeps the detail panel (tokens/latency) collapsed until the pill is clicked', () => {
    render(<CostPanel interactions={[ambiguityGateCall, explanationCall]} />)
    expect(screen.queryByRole('group', { name: /model spend detail/i })).not.toBeInTheDocument()
  })

  it('expands to show interaction count, total tokens, and average latency', async () => {
    const user = userEvent.setup()
    render(<CostPanel interactions={[ambiguityGateCall, explanationCall]} />)

    await user.click(screen.getByRole('button', { name: /model spend/i }))

    const panel = screen.getByRole('group', { name: /model spend detail/i })
    expect(panel).toHaveTextContent('Interactions')
    expect(panel).toHaveTextContent('2')
    // total tokens: (420+18) + (1180+240) = 1858
    expect(panel).toHaveTextContent('1,858')
    // average latency: (310 + 1420) / 2 = 865ms
    expect(panel).toHaveTextContent('865ms')
  })

  it('shows an em dash for average latency when there are no interactions', async () => {
    const user = userEvent.setup()
    render(<CostPanel interactions={[]} />)

    await user.click(screen.getByRole('button', { name: /model spend/i }))
    expect(screen.getByRole('group', { name: /model spend detail/i })).toHaveTextContent('—')
  })

  it('toggles aria-expanded and collapses the detail panel on a second click', async () => {
    const user = userEvent.setup()
    render(<CostPanel interactions={[ambiguityGateCall]} />)

    const trigger = screen.getByRole('button', { name: /model spend/i })
    expect(trigger).toHaveAttribute('aria-expanded', 'false')

    await user.click(trigger)
    expect(trigger).toHaveAttribute('aria-expanded', 'true')

    await user.click(trigger)
    expect(trigger).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('group', { name: /model spend detail/i })).not.toBeInTheDocument()
  })
})
