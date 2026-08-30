import { useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'

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

/** Query-string key the search box reads/writes. Namespaced with a `tf-`
 * prefix so it can never collide with a dimension key a page defines (e.g.
 * a hypothetical `search` dimension) or with an unrelated param some other
 * feature adds to the same URL later. */
const SEARCH_PARAM_KEY = 'tf-search'

/**
 * One shared client-side filter over a grid's already-fetched rows — never a
 * new backend query. Every page wiring a grid filter (Promotions, Points,
 * Home, Platforms) goes through this hook so "matches the search box" and
 * "matches the selected dimension value" behave identically everywhere,
 * rather than five bespoke `.filter()` calls that could each drift.
 *
 * State lives in the URL's search params (`useSearchParams`), not local
 * `useState` — reported live: this app has a deliberately designed flow
 * (spec 008 FR-001) where clicking a chart point navigates away to `/ask`
 * with the expectation that Back returns to what the owner was studying.
 * With filter state as local `useState`, that Back press remounted the page
 * fresh and silently dropped the filter — the exact narrowed view the owner
 * had clicked through FROM was gone. Sourcing state from the URL fixes this
 * for free: a POP navigation (Back/Forward) restores whatever query string
 * the browser already remembers for that history entry, with no separate
 * cache to maintain here, and the filtered view becomes a real bookmarkable
 * link as a side benefit. Every write uses `{ replace: true }` so typing in
 * the search box coalesces into the CURRENT history entry instead of
 * pushing one new entry per keystroke — the whole point is that Back should
 * leave the filtered page in one step, not need N presses to undo N
 * keystrokes first. A normal in-app navigation to the page (e.g. clicking
 * "Promotions" in the sidebar) still starts unfiltered, since that `<Link>`
 * targets the plain path with no query string of its own — nothing here
 * needs to special-case that.
 */
export function useTableFilter<T>({
  rows,
  getSearchableText,
  dimensions = [],
}: UseTableFilterOptions<T>): UseTableFilterResult<T> {
  const [searchParams, setSearchParams] = useSearchParams()

  const searchQuery = searchParams.get(SEARCH_PARAM_KEY) ?? ''

  const filterValues = useMemo(() => {
    const out: Record<string, string | null> = {}
    for (const dimension of dimensions) {
      out[dimension.key] = searchParams.get(dimension.key)
    }
    return out
    // Same rationale as `dimensionOptions`/`filteredRows` below: `dimensions`
    // is expected to be a fresh array each render, so keying off it would
    // recompute every render for no reason. `searchParams` is the only input
    // that actually needs to trigger a recompute.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [searchParams])

  function setSearchQuery(value: string) {
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current)
        if (value) next.set(SEARCH_PARAM_KEY, value)
        else next.delete(SEARCH_PARAM_KEY)
        return next
      },
      { replace: true },
    )
  }

  function setFilterValue(key: string, value: string | null) {
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current)
        if (value) next.set(key, value)
        else next.delete(key)
        return next
      },
      { replace: true },
    )
  }

  function clearFilters() {
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current)
        next.delete(SEARCH_PARAM_KEY)
        for (const dimension of dimensions) next.delete(dimension.key)
        return next
      },
      { replace: true },
    )
  }

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
