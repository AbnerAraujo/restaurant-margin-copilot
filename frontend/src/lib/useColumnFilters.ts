import { useMemo, useState } from 'react'

/**
 * Excel/Google-Sheets-style per-column header filters — a SECOND, additive
 * filtering surface that composes with whatever a page's own `useTableFilter`
 * (search box, dropdown, chips) already narrowed a grid to. This hook never
 * reads or writes the URL: unlike `useTableFilter`, the tables it's wired
 * into today (`CostSheetTab`, `ConnectedPlatformsTab`) are one-shot
 * preview-before-commit steps with no route of their own and no existing
 * synced-state precedent to extend — reloading the page discards the whole
 * staged upload (the in-memory `File`) regardless of what a URL might
 * remember, so persisting column-filter choices there would be a promise
 * this flow can't keep. A future caller that DOES already sync search state
 * to the URL (`useTableFilter`) should extend that same discipline to its
 * own column filters rather than reuse this hook's local-state approach
 * as-is — see CHANGELOG for this feature's date.
 *
 * Operates on the same plain `columns: string[]` / `rows: string[][]` shape
 * `DataGrid` already renders, so it drops into that component without
 * either side needing to know about the other's row type.
 */

export type ColumnFilterType = 'categorical' | 'text' | 'numeric'

/** Which columns (by index into `columns`/each row) get a filter, and what
 *  kind. Omitted columns render with no filter affordance at all — most
 *  columns in a grid (an id, a provenance tag, a currency total with no
 *  categorical meaning) shouldn't get one. */
export type ColumnFilterSpecs = Partial<Record<number, ColumnFilterType>>

interface CategoricalState {
  type: 'categorical'
  /** Empty set means "no filter" — every value passes. */
  selected: Set<string>
}

interface TextState {
  type: 'text'
  query: string
}

interface NumericState {
  type: 'numeric'
  /** Empty string means that bound is open. */
  min: string
  max: string
}

type ColumnState = CategoricalState | TextState | NumericState

function initialState(type: ColumnFilterType): ColumnState {
  if (type === 'categorical') return { type, selected: new Set() }
  if (type === 'text') return { type, query: '' }
  return { type, min: '', max: '' }
}

function isColumnStateActive(state: ColumnState): boolean {
  if (state.type === 'categorical') return state.selected.size > 0
  if (state.type === 'text') return state.query.trim() !== ''
  return state.min.trim() !== '' || state.max.trim() !== ''
}

/**
 * Reads a cell's displayed string as a number for the `numeric` filter type —
 * DataGrid cells arrive already formatted ("$1,234.56", "12", "—"), never a
 * raw number, so this strips everything but digits/sign/decimal point before
 * parsing. A cell that still doesn't parse (an em dash standing in for "no
 * value", a refused figure) returns `null` and is excluded from a numeric
 * filter's results rather than guessed at — the same refuse-over-estimate
 * discipline the rest of this product's numbers follow, applied here to a
 * client-side UI filter rather than a financial computation.
 */
function parseNumericCell(cell: string): number | null {
  const cleaned = cell.replace(/[^0-9.-]/g, '')
  if (cleaned === '' || cleaned === '-' || cleaned === '.') return null
  const value = Number(cleaned)
  return Number.isNaN(value) ? null : value
}

function matchesColumnState(cell: string, state: ColumnState): boolean {
  if (!isColumnStateActive(state)) return true
  if (state.type === 'categorical') return state.selected.has(cell)
  if (state.type === 'text') return cell.toLowerCase().includes(state.query.trim().toLowerCase())

  const value = parseNumericCell(cell)
  if (value === null) return false
  const min = state.min.trim() === '' ? -Infinity : Number(state.min)
  const max = state.max.trim() === '' ? Infinity : Number(state.max)
  return value >= min && value <= max
}

export interface UseColumnFiltersOptions {
  columns: string[]
  rows: string[][]
  specs: ColumnFilterSpecs
}

export interface UseColumnFiltersResult {
  filteredRows: string[][]
  /** True once any column carries an active filter. */
  isFiltered: boolean
  /** Every distinct value on file for a `categorical` column, in first-seen
   *  row order — never a hardcoded list, so a filter never offers a value
   *  that isn't actually in this grid's data. */
  getOptions: (columnIndex: number) => string[]
  isColumnActive: (columnIndex: number) => boolean
  getCategoricalSelection: (columnIndex: number) => Set<string>
  toggleCategoricalValue: (columnIndex: number, value: string) => void
  getTextQuery: (columnIndex: number) => string
  setTextQuery: (columnIndex: number, query: string) => void
  getNumericRange: (columnIndex: number) => { min: string; max: string }
  setNumericRange: (columnIndex: number, min: string, max: string) => void
  clearColumn: (columnIndex: number) => void
  clearAll: () => void
}

// `columns` is part of the public option shape (a filter spec is inherently
// about a named column, and it keeps this call symmetric with DataGrid's own
// `columns`/`rows` props at the call site) but isn't read here — every
// lookup below is by index into `rows`, never by column name.
export function useColumnFilters({ rows, specs }: UseColumnFiltersOptions): UseColumnFiltersResult {
  const [state, setState] = useState<Record<number, ColumnState>>({})

  function stateFor(columnIndex: number): ColumnState {
    const type = specs[columnIndex]
    if (!type) {
      // Only reachable if a caller queries a column it never configured —
      // treated as an always-inactive text filter rather than throwing, so
      // a stray call from a generic UI layer can't crash the grid.
      return state[columnIndex] ?? initialState('text')
    }
    return state[columnIndex] ?? initialState(type)
  }

  function updateColumn(columnIndex: number, next: ColumnState) {
    setState((current) => ({ ...current, [columnIndex]: next }))
  }

  const getOptions = useMemo(() => {
    return (columnIndex: number): string[] => {
      const seen: string[] = []
      for (const row of rows) {
        const value = row[columnIndex] ?? ''
        if (!seen.includes(value)) seen.push(value)
      }
      return seen
    }
  }, [rows])

  const activeColumns = Object.keys(specs).map(Number)

  const filteredRows = useMemo(() => {
    if (activeColumns.length === 0) return rows
    return rows.filter((row) =>
      activeColumns.every((columnIndex) => matchesColumnState(row[columnIndex] ?? '', stateFor(columnIndex))),
    )
    // `specs` is expected to be a fresh object per render (callers build it
    // inline); `state` and `rows` are what actually need to trigger a
    // recompute.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows, state])

  const isFiltered = activeColumns.some((columnIndex) => isColumnStateActive(stateFor(columnIndex)))

  return {
    filteredRows,
    isFiltered,
    getOptions,
    isColumnActive: (columnIndex) => isColumnStateActive(stateFor(columnIndex)),
    getCategoricalSelection: (columnIndex) => {
      const s = stateFor(columnIndex)
      return s.type === 'categorical' ? s.selected : new Set()
    },
    toggleCategoricalValue: (columnIndex, value) => {
      const current = stateFor(columnIndex)
      const selected = new Set(current.type === 'categorical' ? current.selected : [])
      if (selected.has(value)) selected.delete(value)
      else selected.add(value)
      updateColumn(columnIndex, { type: 'categorical', selected })
    },
    getTextQuery: (columnIndex) => {
      const s = stateFor(columnIndex)
      return s.type === 'text' ? s.query : ''
    },
    setTextQuery: (columnIndex, query) => {
      updateColumn(columnIndex, { type: 'text', query })
    },
    getNumericRange: (columnIndex) => {
      const s = stateFor(columnIndex)
      return s.type === 'numeric' ? { min: s.min, max: s.max } : { min: '', max: '' }
    },
    setNumericRange: (columnIndex, min, max) => {
      updateColumn(columnIndex, { type: 'numeric', min, max })
    },
    clearColumn: (columnIndex) => {
      const type = specs[columnIndex]
      if (!type) return
      updateColumn(columnIndex, initialState(type))
    },
    clearAll: () => setState({}),
  }
}
