import { act, renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { useColumnFilters } from './useColumnFilters'

const COLUMNS = ['Invoice ID', 'Supplier', 'Category', 'Amount']
const ROWS = [
  ['INV-001', 'Acme Produce', 'Produce', '$120.00'],
  ['INV-002', 'Acme Produce', 'Dairy', '$45.50'],
  ['INV-003', 'Northside Meats', 'Meat', '$310.25'],
  ['INV-004', 'Northside Meats', 'Produce', '—'],
]

describe('useColumnFilters', () => {
  it('returns every row unfiltered when no column has a configured filter', () => {
    const { result } = renderHook(() =>
      useColumnFilters({ columns: COLUMNS, rows: ROWS, specs: {} }),
    )
    expect(result.current.filteredRows).toEqual(ROWS)
    expect(result.current.isFiltered).toBe(false)
  })

  it('lists a categorical column\'s distinct values in first-seen order, never a hardcoded list', () => {
    const { result } = renderHook(() =>
      useColumnFilters({ columns: COLUMNS, rows: ROWS, specs: { 1: 'categorical' } }),
    )
    expect(result.current.getOptions(1)).toEqual(['Acme Produce', 'Northside Meats'])
  })

  it('narrows to rows matching a selected categorical value, and reports the column as active', () => {
    const { result } = renderHook(() =>
      useColumnFilters({ columns: COLUMNS, rows: ROWS, specs: { 1: 'categorical' } }),
    )
    act(() => result.current.toggleCategoricalValue(1, 'Northside Meats'))
    expect(result.current.filteredRows).toEqual([ROWS[2], ROWS[3]])
    expect(result.current.isColumnActive(1)).toBe(true)
    expect(result.current.isFiltered).toBe(true)
  })

  it('unions multiple selected values within the same categorical column', () => {
    const { result } = renderHook(() =>
      useColumnFilters({ columns: COLUMNS, rows: ROWS, specs: { 2: 'categorical' } }),
    )
    act(() => result.current.toggleCategoricalValue(2, 'Dairy'))
    act(() => result.current.toggleCategoricalValue(2, 'Meat'))
    expect(result.current.filteredRows).toEqual([ROWS[1], ROWS[2]])
  })

  it('toggling a value off removes it from the selection and widens the results back out', () => {
    const { result } = renderHook(() =>
      useColumnFilters({ columns: COLUMNS, rows: ROWS, specs: { 1: 'categorical' } }),
    )
    act(() => result.current.toggleCategoricalValue(1, 'Northside Meats'))
    act(() => result.current.toggleCategoricalValue(1, 'Northside Meats'))
    expect(result.current.filteredRows).toEqual(ROWS)
    expect(result.current.isColumnActive(1)).toBe(false)
  })

  it('composes two active column filters with AND, not OR', () => {
    const { result } = renderHook(() =>
      useColumnFilters({
        columns: COLUMNS,
        rows: ROWS,
        specs: { 1: 'categorical', 2: 'categorical' },
      }),
    )
    act(() => result.current.toggleCategoricalValue(1, 'Acme Produce'))
    act(() => result.current.toggleCategoricalValue(2, 'Dairy'))
    expect(result.current.filteredRows).toEqual([ROWS[1]])
  })

  it('matches a text filter case-insensitively as a substring', () => {
    const { result } = renderHook(() =>
      useColumnFilters({ columns: COLUMNS, rows: ROWS, specs: { 0: 'text' } }),
    )
    act(() => result.current.setTextQuery(0, 'inv-00'))
    expect(result.current.filteredRows).toHaveLength(4)
    act(() => result.current.setTextQuery(0, '002'))
    expect(result.current.filteredRows).toEqual([ROWS[1]])
  })

  it('a blank text query clears the filter', () => {
    const { result } = renderHook(() =>
      useColumnFilters({ columns: COLUMNS, rows: ROWS, specs: { 0: 'text' } }),
    )
    act(() => result.current.setTextQuery(0, '002'))
    act(() => result.current.setTextQuery(0, '  '))
    expect(result.current.isColumnActive(0)).toBe(false)
    expect(result.current.filteredRows).toEqual(ROWS)
  })

  it('parses formatted currency cells for a numeric range filter and excludes an unparseable one', () => {
    const { result } = renderHook(() =>
      useColumnFilters({ columns: COLUMNS, rows: ROWS, specs: { 3: 'numeric' } }),
    )
    act(() => result.current.setNumericRange(3, '100', ''))
    // Row 4's Amount is "—" (unparseable) — refused, not guessed into range.
    expect(result.current.filteredRows).toEqual([ROWS[0], ROWS[2]])
  })

  it('supports an open-ended numeric range on either bound', () => {
    const { result } = renderHook(() =>
      useColumnFilters({ columns: COLUMNS, rows: ROWS, specs: { 3: 'numeric' } }),
    )
    act(() => result.current.setNumericRange(3, '', '100'))
    expect(result.current.filteredRows).toEqual([ROWS[1]])
  })

  it('clearColumn resets only that column, leaving other active filters in place', () => {
    const { result } = renderHook(() =>
      useColumnFilters({
        columns: COLUMNS,
        rows: ROWS,
        specs: { 1: 'categorical', 2: 'categorical' },
      }),
    )
    act(() => result.current.toggleCategoricalValue(1, 'Acme Produce'))
    act(() => result.current.toggleCategoricalValue(2, 'Dairy'))
    act(() => result.current.clearColumn(2))
    expect(result.current.isColumnActive(1)).toBe(true)
    expect(result.current.isColumnActive(2)).toBe(false)
    expect(result.current.filteredRows).toEqual([ROWS[0], ROWS[1]])
  })

  it('clearAll resets every active column filter at once', () => {
    const { result } = renderHook(() =>
      useColumnFilters({
        columns: COLUMNS,
        rows: ROWS,
        specs: { 1: 'categorical', 3: 'numeric' },
      }),
    )
    act(() => result.current.toggleCategoricalValue(1, 'Acme Produce'))
    act(() => result.current.setNumericRange(3, '200', ''))
    act(() => result.current.clearAll())
    expect(result.current.isFiltered).toBe(false)
    expect(result.current.filteredRows).toEqual(ROWS)
  })
})
