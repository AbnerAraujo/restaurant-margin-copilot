import { useMemo, useState } from 'react'

/**
 * One categorical dimension a grid can be filtered on (e.g. "platform",
 * "status"). `getValue` reads the dimension's value off a row; a row whose
 * value doesn't match the active selection is excluded. Options are derived
 * from the data itself (see `useTableFilter`), never hardcoded, so a filter
 * never lists a platform that isn't actually on file.
 */
export interface FilterDimension<T> {
  key: string
  getValue: (row: T) => string
}

export interface UseTableFilterOptions<T> {
  rows: T[]
  /**
   * The row's visible primary fields, joined and matched case-insensitively
   * against the search query — e.g. a campaign's id and platform. Only
   * fields already rendered on screen belong here; never a hidden id.
   */
  getSearchableText: (row: T) => string[]
  dimensions?: FilterDimension<T>[]
}

export interface UseTableFilterResult<T> {
  searchQuery: string
  setSearchQuery: (value: string) => void
  /** Active value per dimension key, or `null` when that dimension is unset. */
  filterValues: Record<string, string | null>
  setFilterValue: (key: string, value: string | null) => void
  /** Every distinct value present in `rows` for each dimension, in first-seen order. */
  dimensionOptions: Record<string, string[]>
  filteredRows: T[]
  /** True once any search text or dimension filter is active. */
  isFiltered: boolean
  clearFilters: () => void
  totalCount: number
  visibleCount: number
}

/**
 * One shared client-side filter over a grid's already-fetched rows — never a
 * new backend query. Every page wiring a grid filter (Promotions, Points,
 * Home, Platforms) goes through this hook so "matches the search box" and
 * "matches the selected dimension value" behave identically everywhere,
 * rather than five bespoke `.filter()` calls that could each drift.
 */
export function useTableFilter<T>({
  rows,
  getSearchableText,
  dimensions = [],
}: UseTableFilterOptions<T>): UseTableFilterResult<T> {
  const [searchQuery, setSearchQuery] = useState('')
  const [filterValues, setFilterValues] = useState<Record<string, string | null>>({})

  const dimensionOptions = useMemo(() => {
    const out: Record<string, string[]> = {}
    for (const dimension of dimensions) {
      const seen: string[] = []
      for (const row of rows) {
        const value = dimension.getValue(row)
        if (!seen.includes(value)) seen.push(value)
      }
      out[dimension.key] = seen
    }
    return out
    // `dimensions` is expected to be a fresh array/closures each render
    // (page components build it inline); keying off `rows` alone is what
    // actually matters.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows])

  const normalizedQuery = searchQuery.trim().toLowerCase()

  const filteredRows = useMemo(() => {
    return rows.filter((row) => {
      if (normalizedQuery) {
        const haystack = getSearchableText(row).join(' ').toLowerCase()
        if (!haystack.includes(normalizedQuery)) return false
      }
      for (const dimension of dimensions) {
        const active = filterValues[dimension.key]
        if (active && dimension.getValue(row) !== active) return false
      }
      return true
    })
    // Same rationale as above for `dimensions`/`getSearchableText`.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows, normalizedQuery, filterValues])

  const isFiltered =
    normalizedQuery !== '' || Object.values(filterValues).some((value) => Boolean(value))

  function setFilterValue(key: string, value: string | null) {
    setFilterValues((current) => ({ ...current, [key]: value }))
  }

  function clearFilters() {
    setSearchQuery('')
    setFilterValues({})
  }

  return {
    searchQuery,
    setSearchQuery,
    filterValues,
    setFilterValue,
    dimensionOptions,
    filteredRows,
    isFiltered,
    clearFilters,
    totalCount: rows.length,
    visibleCount: filteredRows.length,
  }
}
