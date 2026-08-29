import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import {
  FilterBar,
  FilterChip,
  FilterEmptyState,
  FilterSearchInput,
  FilterSelect,
} from './filter-bar'

describe('FilterBar', () => {
  it('hides "Clear filters" and the result summary when nothing is filtered', () => {
    render(
      <FilterBar isFiltered={false} onClear={() => {}}>
        <span>controls</span>
      </FilterBar>,
    )
    expect(
      screen.queryByRole('button', { name: /clear filters/i }),
    ).not.toBeInTheDocument()
  })

  it('shows "Clear filters" and the result summary once filtered, and calls onClear', async () => {
    const onClear = vi.fn()
    render(
      <FilterBar isFiltered onClear={onClear} resultSummary="2 of 5 shown">
        <span>controls</span>
      </FilterBar>,
    )
    expect(screen.getByText('2 of 5 shown')).toBeInTheDocument()

    const button = screen.getByRole('button', { name: /clear filters/i })
    await userEvent.click(button)
    expect(onClear).toHaveBeenCalledOnce()
  })
})

describe('FilterSearchInput', () => {
  it('renders a real label (not just a placeholder) and reports typed text', async () => {
    const onChange = vi.fn()
    render(
      <FilterSearchInput
        id="test-search"
        label="Search campaigns"
        value=""
        onChange={onChange}
        placeholder="Search by campaign ID"
      />,
    )
    const input = screen.getByLabelText('Search campaigns')
    await userEvent.type(input, 'a')
    expect(onChange).toHaveBeenCalledWith('a')
  })
})

describe('FilterSelect', () => {
  it('lists only the given options plus an "all" option, and reports selection', async () => {
    const onChange = vi.fn()
    render(
      <FilterSelect
        id="test-select"
        label="Platform"
        value={null}
        onChange={onChange}
        options={['iFood', 'Just Eat Takeaway']}
        allLabel="All platforms"
      />,
    )
    const select = screen.getByLabelText('Platform')
    expect(screen.getByText('All platforms')).toBeInTheDocument()
    await userEvent.selectOptions(select, 'Just Eat Takeaway')
    expect(onChange).toHaveBeenCalledWith('Just Eat Takeaway')
  })
})

describe('FilterChip', () => {
  it('exposes its pressed state via aria-pressed and calls onClick', async () => {
    const onClick = vi.fn()
    render(
      <FilterChip pressed onClick={onClick}>
        Flagged
      </FilterChip>,
    )
    const chip = screen.getByRole('button', { name: 'Flagged' })
    expect(chip).toHaveAttribute('aria-pressed', 'true')
    await userEvent.click(chip)
    expect(onClick).toHaveBeenCalledOnce()
  })
})

describe('FilterEmptyState', () => {
  it('reassures rather than reading as a dead end, and offers the way back', async () => {
    const onClear = vi.fn()
    render(
      <FilterEmptyState label="No campaigns match these filters." onClear={onClear} />,
    )
    expect(
      screen.getByText('No campaigns match these filters.'),
    ).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /clear filters/i }))
    expect(onClear).toHaveBeenCalledOnce()
  })
})
