import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import AnswerVisualizationView from './AnswerVisualizationView'
import type { AnswerVisualization } from './answerVisualization'

const discrepancyTable: AnswerVisualization = {
  kind: 'table',
  title: 'Flagged days',
  subtitle: '2 of 14 reconciled days carried a discrepancy flag',
  source_tool: 'list_discrepancies',
  columns: ['Date', 'Discrepancy', 'Detail'],
  rows: [
    ['2026-08-03', 'Duplicate order removed', 'order 4412 appeared twice'],
    ['2026-08-08', 'Missing delivery source', 'no delivery-platform rows'],
  ],
}

const roiBar: AnswerVisualization = {
  kind: 'bar',
  title: 'Promotion ROI by campaign',
  subtitle: 'Attributed incremental revenue minus spend',
  value_label: 'Net ROI (USD)',
  source_tool: 'get_promotion_roi',
  points: [
    { label: 'IFOOD-CAMP-BOOST01', value: 34, display: '$34.00' },
    { label: 'JET-CAMP-LUNCHFIX', value: -165, display: '−$165.00' },
    {
      label: 'IFOOD-CAMP-WEEKEND',
      value: 0,
      display: 'Unattributable',
      unavailable: true,
      reason: 'attribution_unavailable',
    },
  ],
}

const revenuePie: AnswerVisualization = {
  kind: 'pie',
  title: "Where the day's revenue came from",
  subtitle: 'Gross sales by source, 2026-08-07',
  value_label: 'Gross sales (USD)',
  source_tool: 'get_daily_summary',
  points: [
    { label: 'In-house POS', value: 266.25, display: '$266.25' },
    { label: 'iFood', value: 74.25, display: '$74.25' },
    { label: 'Just Eat Takeaway', value: 65.5, display: '$65.50' },
  ],
}

describe('AnswerVisualizationView', () => {
  it('renders a table kind as a real data grid with the backend’s own columns and rows', () => {
    render(<AnswerVisualizationView visualization={discrepancyTable} />)

    const table = screen.getByRole('table')
    expect(within(table).getByRole('columnheader', { name: 'Date' })).toBeInTheDocument()
    expect(
      within(table).getByRole('columnheader', { name: 'Discrepancy' }),
    ).toBeInTheDocument()
    expect(within(table).getAllByRole('row')).toHaveLength(3) // header + 2 days
    expect(screen.getByText('order 4412 appeared twice')).toBeInTheDocument()
  })

  it('renders a bar kind with every value direct-labelled from the backend display string', () => {
    render(<AnswerVisualizationView visualization={roiBar} />)

    expect(screen.getByText('$34.00')).toBeInTheDocument()
    expect(screen.getByText('−$165.00')).toBeInTheDocument()
    // role="group", not role="img": each bar is a focusable role="button" for
    // keyboard readers, and role="img" is not allowed focusable descendants
    // (axe nested-interactive). The chart still carries the same full text
    // alternative as its accessible name, which is what this asserts.
    expect(
      screen.getByRole('group', { name: /promotion roi by campaign/i }),
    ).toBeInTheDocument()
  })

  it('renders an unattributable campaign as a refusal, never as a zero-value bar', () => {
    render(<AnswerVisualizationView visualization={roiBar} />)

    expect(
      screen.getByRole('button', {
        name: /IFOOD-CAMP-WEEKEND: no figure — attribution_unavailable/i,
      }),
    ).toBeInTheDocument()
    expect(screen.getByText(/no figure — refused, not zero/i)).toBeInTheDocument()
    // The zero it carries for geometry must never surface as a money figure.
    expect(screen.queryByText('$0.00')).not.toBeInTheDocument()
  })

  it('offers a table-view fallback for a bar chart', async () => {
    const user = userEvent.setup()
    render(<AnswerVisualizationView visualization={roiBar} />)

    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /view as table/i }))

    const table = screen.getByRole('table')
    expect(within(table).getByText('JET-CAMP-LUNCHFIX')).toBeInTheDocument()
    expect(
      within(table).getByText(/Unattributable — attribution_unavailable/),
    ).toBeInTheDocument()
  })

  it('renders a pie kind as a labelled composition with shares and a stated total', () => {
    render(<AnswerVisualizationView visualization={revenuePie} />)

    const legend = screen.getByRole('list', { name: /chart legend/i })
    expect(within(legend).getByText('In-house POS')).toBeInTheDocument()
    expect(within(legend).getByText('iFood')).toBeInTheDocument()
    expect(within(legend).getByText('Just Eat Takeaway')).toBeInTheDocument()
    // 266.25 + 74.25 + 65.50 = 406.00, stated in the donut hole rather than
    // left for the reader to add up.
    expect(screen.getByText('$406.00')).toBeInTheDocument()
    expect(within(legend).getByText('66%')).toBeInTheDocument()
  })

  it('renders nothing for an unrecognized kind rather than guessing a form', () => {
    const { container } = render(
      <AnswerVisualizationView
        visualization={
          {
            kind: 'sankey',
            title: 'From a newer backend',
            source_tool: 'some_future_tool',
          } as unknown as AnswerVisualization
        }
      />,
    )
    expect(container).toBeEmptyDOMElement()
  })

  it('renders nothing when a chart kind arrives with no points', () => {
    const { container } = render(
      <AnswerVisualizationView
        visualization={{
          kind: 'bar',
          title: 'Empty',
          source_tool: 'get_margin_delta',
          points: [],
        }}
      />,
    )
    expect(container).toBeEmptyDOMElement()
  })
})
