import { act, renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { useTableFilter } from './useTableFilter'

interface Row {
  id: string
  platform: string
}

const ROWS: Row[] = [
  { id: 'IFOOD-CAMP-BOOST01', platform: 'iFood' },
  { id: 'IFOOD-CAMP-WEEKEND', platform: 'iFood' },
  { id: 'JET-CAMP-LOSER', platform: 'Just Eat Takeaway' },
]

function setup(rows: Row[] = ROWS) {
  return renderHook(() =>
    useTableFilter({
      rows,
      getSearchableText: (row) => [row.id, row.platform],
      dimensions: [{ key: 'platform', getValue: (row) => row.platform }],
    }),
  )
}

describe('useTableFilter', () => {
  it('returns every row and reports not-filtered when no filter is active', () => {
    const { result } = setup()
    expect(result.current.filteredRows).toEqual(ROWS)
    expect(result.current.isFiltered).toBe(false)
    expect(result.current.totalCount).toBe(3)
    expect(result.current.visibleCount).toBe(3)
  })

  it('derives dimension options from the data, in first-seen order, with no duplicates', () => {
    const { result } = setup()
    expect(result.current.dimensionOptions.platform).toEqual([
      'iFood',
      'Just Eat Takeaway',
    ])
  })

  it('narrows rows by a case-insensitive search match against the searchable fields', () => {
    const { result } = setup()
    act(() => result.current.setSearchQuery('weekend'))
    expect(result.current.filteredRows).toEqual([ROWS[1]])
    expect(result.current.isFiltered).toBe(true)
  })

  it('narrows rows by an active dimension filter', () => {
    const { result } = setup()
    act(() => result.current.setFilterValue('platform', 'Just Eat Takeaway'))
    expect(result.current.filteredRows).toEqual([ROWS[2]])
    expect(result.current.isFiltered).toBe(true)
  })

  it('combines search and a dimension filter (both must match)', () => {
    const { result } = setup()
    act(() => {
      result.current.setSearchQuery('camp')
      result.current.setFilterValue('platform', 'iFood')
    })
    expect(result.current.filteredRows).toEqual([ROWS[0], ROWS[1]])
  })

  it('narrows to nothing when no row matches, without throwing', () => {
    const { result } = setup()
    act(() => result.current.setSearchQuery('does-not-exist'))
    expect(result.current.filteredRows).toEqual([])
    expect(result.current.visibleCount).toBe(0)
  })

  it('clearFilters resets both the search query and every dimension filter', () => {
    const { result } = setup()
    act(() => {
      result.current.setSearchQuery('weekend')
      result.current.setFilterValue('platform', 'iFood')
    })
    expect(result.current.isFiltered).toBe(true)

    act(() => result.current.clearFilters())

    expect(result.current.searchQuery).toBe('')
    expect(result.current.filterValues.platform ?? null).toBeNull()
    expect(result.current.isFiltered).toBe(false)
    expect(result.current.filteredRows).toEqual(ROWS)
  })
})
