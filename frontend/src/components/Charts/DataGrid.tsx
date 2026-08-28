import { cn } from '@/lib/utils'

export interface DataGridProps {
  title: string
  subtitle?: string
  columns: string[]
  rows: string[][]
  /** Names the tool whose deterministic result these rows came from. */
  sourceTool?: string
  className?: string
}

/**
 * Tabular rendering of an answer's underlying deterministic result — the form
 * the `dataviz` form heuristic picks when the data is several columns of
 * mixed text rather than a magnitude to compare (a flagged-day list, a single
 * campaign's detail row).
 *
 * Deliberately plain: no sorting, no filtering, no pagination. These grids
 * carry a handful of rows scoped to one answer, and every interactive
 * affordance added here would be a control the reader has to understand
 * before trusting the number.
 */
export default function DataGrid({
  title,
  subtitle,
  columns,
  rows,
  sourceTool,
  className,
}: DataGridProps) {
  return (
    <figure
      className={cn(
        'rounded-lg border border-border bg-background/60 p-3',
        className,
      )}
    >
      <figcaption className="mb-2">
        <p className="text-sm font-medium text-foreground">{title}</p>
        {subtitle ? (
          <p className="text-xs text-muted-foreground">{subtitle}</p>
        ) : null}
      </figcaption>

      {/* Wide content scrolls inside its own container so a long detail cell
          never forces the whole chat bubble to scroll sideways. */}
      <div className="overflow-x-auto">
        <table className="w-full text-left text-xs">
          <caption className="sr-only">{title}</caption>
          <thead>
            <tr className="border-b border-border text-muted-foreground">
              {columns.map((column) => (
                <th
                  key={column}
                  scope="col"
                  className="whitespace-nowrap py-1.5 pr-4 font-medium last:pr-0"
                >
                  {column}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((row, rowIndex) => (
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
            ))}
          </tbody>
        </table>
      </div>

      {sourceTool ? (
        <p className="mt-2 border-t border-border/60 pt-2 text-[11px] text-muted-foreground">
          Computed by <code className="font-mono">{sourceTool}</code>
        </p>
      ) : null}
    </figure>
  )
}
