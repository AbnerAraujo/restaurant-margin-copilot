import type { ReactNode } from 'react'
import { act, renderHook } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
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

// State now lives in the URL's search params (`useSearchParams`), so every
// call needs a Router ancestor — `initialEntries` doubles as the way to
// simulate a POP navigation landing back on a URL that already carries
// filter state, rather than a fresh, unfiltered mount.
function setup(rows: Row[] = ROWS, initialEntries: string[] = ['/promotions']) {
  function wrapper({ children }: { children: ReactNode }) {
    return <MemoryRouter initialEntries={initialEntries}>{children}</MemoryRouter>
  }
  return renderHook(
    () =>
      useTableFilter({
        rows,
        getSearchableText: (row) => [row.id, row.platform],
        dimensions: [{ key: 'platform', getValue: (row) => row.platform }],
      }),
    { wrapper },
  )
}

// `MemoryRouter` never touches the real `window.location` — proving a write
// landed in the URL means reading the in-memory router's OWN location back
// out via `useLocation`, alongside the filter hook, rather than the global.
function setupWithLocation(rows: Row[] = ROWS) {
  function wrapper({ children }: { children: ReactNode }) {
    return <MemoryRouter initialEntries={['/promotions']}>{children}</MemoryRouter>
  }
  return renderHook(
    () => ({
      filter: useTableFilter({
        rows,
        getSearchableText: (row) => [row.id, row.platform],
        dimensions: [{ key: 'platform', getValue: (row) => row.platform }],
      }),
      location: useLocation(),
    }),
    { wrapper },
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

  it('writes filter changes into the URL search params, not just local state', () => {
    const { result } = setupWithLocation()
    // Two separate `act()` calls, matching how these actually happen in the
    // app — a search keystroke and a dropdown pick are always two distinct
    // event handlers, never one batch — so each write sees the OTHER
    // write's already-committed search params rather than a stale snapshot.
    act(() => result.current.filter.setSearchQuery('weekend'))
    act(() => result.current.filter.setFilterValue('platform', 'iFood'))
    // The whole point of syncing to the URL: the browser's own history
    // entry (what Back/Forward reads) carries this, not just this hook's
    // in-memory return value.
    expect(result.current.location.search).toContain('tf-search=weekend')
    expect(result.current.location.search).toContain('platform=iFood')
  })

  it('composes two filter writes issued in the same React batch, instead of the second clobbering the first (regression: reported clobber when two writes land under ~30ms apart)', () => {
    // Reported live: type a search term, then quickly clear it and
    // immediately change a dropdown filter. `setSearchParams`'s functional
    // updater is handed the last-COMMITTED params, not a live queue of
    // pending writes — so two calls inside the SAME batch could both read
    // the same stale `current` and the second write's result would silently
    // drop the first write's change once both committed. A single `act()`
    // wrapping both calls reproduces that same-batch timing directly,
    // rather than relying on a real clock.
    const { result } = setupWithLocation()

    act(() => {
      result.current.filter.setSearchQuery('weekend')
    })
    expect(result.current.location.search).toContain('tf-search=weekend')

    act(() => {
      // Clearing the search box and picking a platform, back to back, in
      // one batch — the exact two-controls-at-once sequence from the report.
      result.current.filter.setSearchQuery('')
      result.current.filter.setFilterValue('platform', 'iFood')
    })

    // Both writes must be reflected — the cleared search AND the new
    // platform filter — never just whichever call happened to commit last.
    expect(result.current.filter.searchQuery).toBe('')
    expect(result.current.filter.filterValues.platform).toBe('iFood')
    expect(result.current.location.search).not.toContain('tf-search=weekend')
    expect(result.current.location.search).toContain('platform=iFood')
  })

  it('composes two writes to DIFFERENT dimensions issued in the same batch, in either call order', () => {
    // The report's repro clears one field while setting another, but the
    // same staleness could equally clobber two dimensions set together
    // (e.g. platform then ROI sign) — this guards the general case, not
    // just the search-plus-dropdown one.
    const { result } = setupWithLocation()

    act(() => {
      result.current.filter.setFilterValue('platform', 'iFood')
      result.current.filter.setSearchQuery('camp')
    })

    expect(result.current.filter.filterValues.platform).toBe('iFood')
    expect(result.current.filter.searchQuery).toBe('camp')
    expect(result.current.location.search).toContain('platform=iFood')
    expect(result.current.location.search).toContain('tf-search=camp')
  })

  it('restores search and dimension filters from the URL on mount — the POP/Back-navigation case (spec 008 FR-001)', () => {
    // Stands in for the real bug: the owner filtered Promotions down to one
    // campaign, clicked a chart point to `/ask`, then pressed the browser's
    // real Back button. That POP navigation remounts this hook against
    // whatever URL browser history already has for `/promotions` — which,
    // since every write above uses `{ replace: true }`, is the SAME entry
    // the filters were written into, not a fresh unfiltered one.
    const { result } = setup(ROWS, [
      '/promotions?tf-search=weekend&platform=iFood',
    ])

    expect(result.current.searchQuery).toBe('weekend')
    expect(result.current.filterValues.platform).toBe('iFood')
    expect(result.current.isFiltered).toBe(true)
    expect(result.current.filteredRows).toEqual([ROWS[1]])
  })

  it('starts unfiltered on a plain URL with no query string — an ordinary in-app navigation, not a restored one', () => {
    const { result } = setup(ROWS, ['/promotions'])

    expect(result.current.searchQuery).toBe('')
    expect(result.current.filterValues.platform ?? null).toBeNull()
    expect(result.current.isFiltered).toBe(false)
    expect(result.current.filteredRows).toEqual(ROWS)
  })
})
