import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import DataGrid from './DataGrid'

const COLUMNS = ['Invoice ID', 'Supplier', 'Category', 'Amount']
const ROWS = [
  ['INV-001', 'Acme Produce', 'Produce', '$120.00'],
  ['INV-002', 'Acme Produce', 'Dairy', '$45.50'],
  ['INV-003', 'Northside Meats', 'Meat', '$310.25'],
]

describe('DataGrid', () => {
  it('renders every row plainly with no filter affordance when columnFilters is omitted', () => {
    render(<DataGrid title="Parsed cost sheet rows" columns={COLUMNS} rows={ROWS} />)
    expect(screen.getAllByRole('row')).toHaveLength(4) // header + 3 rows
    expect(screen.queryByRole('button', { name: /filter by/i })).not.toBeInTheDocument()
  })

  it('renders a filter button only for columns named in columnFilters', () => {
    render(
      <DataGrid
        title="Parsed cost sheet rows"
        columns={COLUMNS}
        rows={ROWS}
        columnFilters={{ 1: 'categorical' }}
      />,
    )
    expect(screen.getByRole('button', { name: /filter by supplier/i })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /filter by category/i })).not.toBeInTheDocument()
  })

  it('opening a categorical column filter and checking a value narrows the visible rows', async () => {
    render(
      <DataGrid
        title="Parsed cost sheet rows"
        columns={COLUMNS}
        rows={ROWS}
        columnFilters={{ 1: 'categorical' }}
        filterEmptyLabel="No cost sheet rows match these filters."
      />,
    )
    await userEvent.click(screen.getByRole('button', { name: /filter by supplier/i }))
    await userEvent.click(await screen.findByRole('checkbox', { name: 'Northside Meats' }))

    expect(screen.getByText('INV-003')).toBeInTheDocument()
    expect(screen.queryByText('INV-001')).not.toBeInTheDocument()
    expect(screen.queryByText('INV-002')).not.toBeInTheDocument()
    expect(screen.getByText('1 of 3 shown')).toBeInTheDocument()
  })

  it('shows the empty state, not a blank table, when a column filter matches nothing', async () => {
    render(
      <DataGrid
        title="Parsed cost sheet rows"
        columns={COLUMNS}
        rows={ROWS}
        columnFilters={{ 0: 'text' }}
        filterEmptyLabel="No cost sheet rows match these filters."
      />,
    )
    await userEvent.click(screen.getByRole('button', { name: /filter by invoice id/i }))
    const input = await screen.findByLabelText(/contains, invoice id/i)
    await userEvent.type(input, 'not-a-real-id{Enter}')

    expect(screen.getByText('No cost sheet rows match these filters.')).toBeInTheDocument()
  })

  it('"Clear filters" restores every row and removes the active-filter indicator', async () => {
    render(
      <DataGrid
        title="Parsed cost sheet rows"
        columns={COLUMNS}
        rows={ROWS}
        columnFilters={{ 1: 'categorical' }}
      />,
    )
    await userEvent.click(screen.getByRole('button', { name: /filter by supplier/i }))
    await userEvent.click(await screen.findByRole('checkbox', { name: 'Northside Meats' }))
    expect(screen.getByRole('button', { name: /filter by supplier, active/i })).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: /clear filters/i }))

    expect(screen.getByText('INV-001')).toBeInTheDocument()
    expect(screen.getByText('INV-002')).toBeInTheDocument()
    expect(screen.getByText('INV-003')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Filter by Supplier' })).toBeInTheDocument()
  })

  it('a numeric range filter parses formatted currency cells and applies on Enter, not per keystroke', async () => {
    render(
      <DataGrid
        title="Parsed cost sheet rows"
        columns={COLUMNS}
        rows={ROWS}
        columnFilters={{ 3: 'numeric' }}
      />,
    )
    await userEvent.click(screen.getByRole('button', { name: /filter by amount/i }))
    const min = await screen.findByLabelText(/minimum, amount/i)
    await userEvent.type(min, '100')
    // Typing alone must not narrow the grid yet.
    expect(screen.getByText('INV-002')).toBeInTheDocument()

    await userEvent.type(min, '{Enter}')
    expect(screen.queryByText('INV-002')).not.toBeInTheDocument()
    expect(screen.getByText('INV-001')).toBeInTheDocument()
    expect(screen.getByText('INV-003')).toBeInTheDocument()
  })

  it('is keyboard-operable: Tab reaches the filter trigger and Enter opens it', async () => {
    render(
      <DataGrid
        title="Parsed cost sheet rows"
        columns={COLUMNS}
        rows={ROWS}
        columnFilters={{ 1: 'categorical' }}
      />,
    )
    await userEvent.tab()
    expect(screen.getByRole('button', { name: /filter by supplier/i })).toHaveFocus()
    await userEvent.keyboard('{Enter}')
    expect(await screen.findByRole('dialog', { name: /filter by supplier/i })).toBeInTheDocument()
  })
})
