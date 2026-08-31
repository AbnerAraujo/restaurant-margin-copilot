import { X } from 'lucide-react'

import { ColumnFilterButton } from '@/components/ui/column-filter'
import { FilterEmptyState } from '@/components/ui/filter-bar'
import { Button } from '@/components/ui/button'
import { useColumnFilters, type ColumnFilterSpecs } from '@/lib/useColumnFilters'
import { useHorizontalScrollFade } from '@/lib/useHorizontalScrollFade'
import { cn } from '@/lib/utils'

export interface DataGridProps {
  title: string
  subtitle?: string
  columns: string[]
  rows: string[][]
  /** Names the tool whose deterministic result these rows came from. */
  sourceTool?: string
  className?: string
  /**
   * Opt-in Excel/Sheets-style header filters, keyed by column index — omitted
   * entirely by every caller that doesn't pass it (chat's answer grids,
   * `PlatformsPage`'s side-by-side comparison), which keeps this grid exactly
   * as plain as before for them. Only wired in for the two callers with real
   * scale AND a genuine categorical/textual dimension worth narrowing by
   * (`CostSheetTab`, `ConnectedPlatformsTab`) — see CHANGELOG for the survey
   * of every other table this was and wasn't added to, and why.
   */
  columnFilters?: ColumnFilterSpecs
  /** Required alongside `columnFilters`: what the "no rows match" empty
   *  state says once a column filter narrows this grid to nothing. */
  filterEmptyLabel?: string
}

/**
 * Tabular rendering of an answer's underlying deterministic result — the form
 * the `dataviz` form heuristic picks when the data is several columns of
 * mixed text rather than a magnitude to compare (a flagged-day list, a single
 * campaign's detail row).
 *
 * Deliberately plain by default: no sorting, no pagination, and no filtering
 * unless a caller opts in via `columnFilters`. Most callers render a handful
 * of rows scoped to one answer, where every interactive affordance added
 * here would be a control the reader has to understand before trusting the
 * number — `columnFilters` exists for the opposite case, a real
 * preview-before-commit table with dozens of rows and a genuine categorical
 * column worth narrowing by.
 */
export default function DataGrid({
  title,
  subtitle,
  columns,
  rows,
  sourceTool,
  className,
  columnFilters,
  filterEmptyLabel,
}: DataGridProps) {
  const specs = columnFilters ?? {}
  const columnFilterState = useColumnFilters({ columns, rows, specs })
  const { ref: scrollRef, canScrollRight } = useHorizontalScrollFade<HTMLDivElement>()
  const hasColumnFilters = Object.keys(specs).length > 0
  const visibleRows = hasColumnFilters ? columnFilterState.filteredRows : rows

  return (
    <figure
      className={cn(
        'rounded-lg border border-border bg-background/60 p-3',
        className,
      )}
    >
      <figcaption className="mb-2 flex flex-wrap items-start justify-between gap-2">
        <div>
          <p className="text-sm font-medium text-foreground">{title}</p>
          {subtitle ? (
            <p className="text-xs text-muted-foreground">{subtitle}</p>
          ) : null}
        </div>
        {hasColumnFilters && columnFilterState.isFiltered ? (
          <div className="flex items-center gap-2">
            <span className="text-xs text-muted-foreground" aria-live="polite">
              {visibleRows.length} of {rows.length} shown
            </span>
            <Button type="button" variant="ghost" size="sm" onClick={columnFilterState.clearAll}>
              <X aria-hidden="true" />
              Clear filters
            </Button>
          </div>
        ) : null}
      </figcaption>

      {
        // Wide content scrolls inside its own container so a long detail
        // cell never forces the whole chat bubble to scroll sideways. The
        // relative wrapper + fade (canScrollRight) is the same affordance
        // MobileNavBar established (useHorizontalScrollFade) -- found live
        // at 375px: a column-filterable grid's later columns (and their
        // filter buttons) sat far off-screen with no scrollbar hint.
        //
        // The header (with its filter buttons) always renders, even when
        // filters have narrowed the result to zero rows -- found live: this
        // used to swap the ENTIRE table (header included) for the empty
        // state, which took every OTHER column's filter trigger down with
        // it, leaving "Clear filters" (clear everything) as the only way
        // back rather than letting the owner loosen just the one filter
        // that over-narrowed. The empty state now renders as a single
        // full-width table row instead, so the header survives.
      }
      <div className="relative">
        <div ref={scrollRef} className="overflow-x-auto">
          <table className="w-full text-left text-xs">
            <caption className="sr-only">{title}</caption>
            <thead>
              <tr className="border-b border-border text-muted-foreground">
                {columns.map((column, columnIndex) => {
                  const filterType = specs[columnIndex]
                  return (
                    <th
                      key={column}
                      scope="col"
                      className="whitespace-nowrap py-1.5 pr-4 font-medium last:pr-0"
                    >
                      <span className="inline-flex items-center gap-1">
                        {column}
                        {filterType === 'categorical' ? (
                          <ColumnFilterButton
                            type="categorical"
                            columnLabel={column}
                            options={columnFilterState.getOptions(columnIndex)}
                            selected={columnFilterState.getCategoricalSelection(columnIndex)}
                            onToggle={(value) =>
                              columnFilterState.toggleCategoricalValue(columnIndex, value)
                            }
                            onClear={() => columnFilterState.clearColumn(columnIndex)}
                          />
                        ) : filterType === 'text' ? (
                          <ColumnFilterButton
                            type="text"
                            columnLabel={column}
                            query={columnFilterState.getTextQuery(columnIndex)}
                            onApply={(query) => columnFilterState.setTextQuery(columnIndex, query)}
                            onClear={() => columnFilterState.clearColumn(columnIndex)}
                          />
                        ) : filterType === 'numeric' ? (
                          <ColumnFilterButton
                            type="numeric"
                            columnLabel={column}
                            {...columnFilterState.getNumericRange(columnIndex)}
                            onApply={(min, max) =>
                              columnFilterState.setNumericRange(columnIndex, min, max)
                            }
                            onClear={() => columnFilterState.clearColumn(columnIndex)}
                          />
                        ) : null}
                      </span>
                    </th>
                  )
                })}
              </tr>
            </thead>
            <tbody>
              {hasColumnFilters && rows.length > 0 && visibleRows.length === 0 ? (
                <tr>
                  <td colSpan={columns.length} className="py-1.5">
                    <FilterEmptyState
                      label={filterEmptyLabel ?? 'No rows match these filters.'}
                      onClear={columnFilterState.clearAll}
                    />
                  </td>
                </tr>
              ) : (
                visibleRows.map((row, rowIndex) => (
                  <tr
                    key={`${row[0] ?? ''}-${rowIndex}`}
                    className="border-b border-border/60 last:border-b-0"
                  >
                    {row.map((cell, cellIndex) => (
                      <td
                        key={`${cellIndex}-${cell}`}
                        className={cn(
                          'py-1.5 pr-4 align-top text-foreground last:pr-0',
                          // First column is the row's identity (a date, a
                          // platform); money and counts read as numbers.
                          cellIndex === 0 ? 'whitespace-nowrap font-medium' : null,
                          /^[−-]?\$/.test(cell) ? 'tabular-nums' : null,
                        )}
                      >
                        {cell || '—'}
                      </td>
                    ))}
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
        {canScrollRight && (
          <div
            aria-hidden="true"
            className="pointer-events-none absolute inset-y-0 right-0 w-8 bg-gradient-to-r from-transparent to-card"
          />
        )}
      </div>

      {sourceTool ? (
        <p className="mt-2 border-t border-border/60 pt-2 text-micro text-muted-foreground">
          Computed by <code className="font-mono">{sourceTool}</code>
        </p>
      ) : null}
    </figure>
  )
}
