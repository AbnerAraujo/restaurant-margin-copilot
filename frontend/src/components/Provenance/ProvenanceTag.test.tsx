import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import ProvenanceTag, { type SourceRowRef } from './ProvenanceTag'

// Realistic mocked data shaped exactly like DailyReconciliation /
// PromotionRoiRecord `source_row_refs` (data-model.md): file + row range +
// period, per FR-005.
const posExportRef: SourceRowRef = {
  source_file: 'pos_export_2026-08-21.csv',
  row_start: 12,
  row_end: 47,
  period_start: '2026-08-21',
  period_end: '2026-08-21',
}

const deliveryPlatformRef: SourceRowRef = {
  source_file: 'ifood_delivery_export_wk34.csv',
  row_start: 3,
  row_end: 3,
  period_start: '2026-08-18',
  period_end: '2026-08-24',
}

const crossMonthRef: SourceRowRef = {
  source_file: 'supplier_cost_sheet.csv',
  row_start: 200,
  row_end: 210,
  period_start: '2026-08-28',
  period_end: '2026-09-02',
}

describe('ProvenanceTag', () => {
  it('renders nothing when there are no source refs (refusal case)', () => {
    const { container } = render(<ProvenanceTag refs={[]} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('shows a single-line citation label for one ref, collapsed by default', () => {
    render(<ProvenanceTag refs={[posExportRef]} />)

    const trigger = screen.getByRole('button', {
      name: /pos_export_2026-08-21\.csv.*rows 12–47.*Aug 21/,
    })
    expect(trigger).toBeInTheDocument()
    expect(trigger).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('group', { name: /source citations/i })).not.toBeInTheDocument()
  })

  it('formats a single-row reference as singular "row N", not a range', () => {
    render(<ProvenanceTag refs={[deliveryPlatformRef]} />)
    expect(screen.getByRole('button', { name: /row 3(?!–)/ })).toBeInTheDocument()
  })

  it('expands to show full source detail on click', async () => {
    const user = userEvent.setup()
    render(<ProvenanceTag refs={[posExportRef]} />)

    await user.click(screen.getByRole('button', { name: /pos_export/i }))

    const panel = screen.getByRole('group', { name: /source citations/i })
    expect(panel).toBeInTheDocument()
    expect(panel).toHaveTextContent('pos_export_2026-08-21.csv')
    expect(panel).toHaveTextContent('rows 12–47')
    expect(panel).toHaveTextContent('Aug 21')
  })

  it('collapses again when the dismiss control is clicked', async () => {
    const user = userEvent.setup()
    render(<ProvenanceTag refs={[posExportRef]} />)

    await user.click(screen.getByRole('button', { name: /pos_export/i }))
    expect(screen.getByRole('group', { name: /source citations/i })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /dismiss citation detail/i }))
    expect(screen.queryByRole('group', { name: /source citations/i })).not.toBeInTheDocument()
  })

  it('shows a source count trigger and lists every ref when multiple sources back one number', async () => {
    const user = userEvent.setup()
    render(<ProvenanceTag refs={[posExportRef, deliveryPlatformRef]} />)

    const trigger = screen.getByRole('button', { name: '2 sources' })
    await user.click(trigger)

    const panel = screen.getByRole('group', { name: /source citations/i })
    expect(panel).toHaveTextContent('pos_export_2026-08-21.csv')
    expect(panel).toHaveTextContent('ifood_delivery_export_wk34.csv')
  })

  it('formats a multi-day period within the same month as "Mon D–D"', () => {
    render(<ProvenanceTag refs={[deliveryPlatformRef]} />)
    expect(screen.getByRole('button', { name: /Aug 18–24/ })).toBeInTheDocument()
  })

  it('formats a period spanning two months as "Mon D – Mon D"', () => {
    render(<ProvenanceTag refs={[crossMonthRef]} />)
    expect(screen.getByRole('button', { name: /Aug 28 – Sep 2/ })).toBeInTheDocument()
  })
})
