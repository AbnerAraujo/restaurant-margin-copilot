import type { ReactNode } from 'react'
import { Search, X } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Input, Select } from '@/components/ui/input'
import { cn } from '@/lib/utils'

/**
 * The one filter-control row every grid page shares (dataviz skill: "one
 * row, above the charts — never inside a card, never per-chart"; the same
 * placement rule applies here even though a grid isn't a chart). Renders as
 * ordinary form controls, left-aligned, scoping everything below it.
 *
 * `resultSummary` is the live "N of M shown" readout — `aria-live="polite"`
 * so a screen reader hears the count change as the reader types or picks a
 * filter, without it being announced on every keystroke as an interruption.
 */
export function FilterBar({
  children,
  isFiltered,
  onClear,
  resultSummary,
  className,
}: {
  children: ReactNode
  isFiltered: boolean
  onClear: () => void
  resultSummary?: string
  className?: string
}) {
  return (
    <div
      role="search"
      aria-label="Filter this list"
      className={cn('flex flex-wrap items-center gap-3', className)}
    >
      {children}
      {isFiltered ? (
        <Button type="button" variant="ghost" size="sm" onClick={onClear}>
          <X aria-hidden="true" />
          Clear filters
        </Button>
      ) : null}
      {resultSummary ? (
        <span className="text-xs text-muted-foreground" aria-live="polite">
          {resultSummary}
        </span>
      ) : null}
    </div>
  )
}

/** A labeled control, laid out the same way ClosePage's date pickers already
 * are (visible label beside the control) — filters get no exception from
 * the "labels present, placeholders are not labels" rule. */
function FilterField({
  id,
  label,
  children,
}: {
  id: string
  label: string
  children: ReactNode
}) {
  return (
    <div className="flex items-center gap-1.5">
      <label htmlFor={id} className="text-xs font-medium text-muted-foreground">
        {label}
      </label>
      {children}
    </div>
  )
}

/** The shared text-search box: matches a row's visible primary fields (never
 * a hidden id), debounce-free since filtering is a cheap in-memory pass over
 * already-fetched data. */
export function FilterSearchInput({
  id,
  label,
  value,
  onChange,
  placeholder,
}: {
  id: string
  label: string
  value: string
  onChange: (value: string) => void
  placeholder: string
}) {
  return (
    <FilterField id={id} label={label}>
      <div className="relative">
        <Search
          className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
          aria-hidden="true"
        />
        <Input
          id={id}
          type="search"
          value={value}
          onChange={(event) => onChange(event.target.value)}
          placeholder={placeholder}
          className="h-8 w-48 pl-8"
        />
      </div>
    </FilterField>
  )
}

/** The shared categorical dropdown — for a dimension with enough distinct
 * values that a chip row would wrap awkwardly (e.g. platform). `options`
 * comes from `useTableFilter`'s `dimensionOptions`, so it only ever lists
 * values actually present in the data. */
export function FilterSelect({
  id,
  label,
  value,
  onChange,
  options,
  allLabel,
}: {
  id: string
  label: string
  value: string | null
  onChange: (value: string | null) => void
  options: string[]
  /** What "no filter selected" reads as, e.g. "All platforms". */
  allLabel: string
}) {
  return (
    <FilterField id={id} label={label}>
      <Select
        id={id}
        value={value ?? ''}
        onChange={(event) => onChange(event.target.value || null)}
        className="h-8 w-auto"
      >
        <option value="">{allLabel}</option>
        {options.map((option) => (
          <option key={option} value={option}>
            {option}
          </option>
        ))}
      </Select>
    </FilterField>
  )
}

/** The shared toggle chip — for a dimension with few enough values (2–4)
 * that a row of chips reads faster than a dropdown (e.g. clean vs. flagged,
 * or ROI sign). One chip in the group carries `aria-pressed`; the group
 * itself should carry `role="group"` with an `aria-label` from the caller. */
export function FilterChip({
  pressed,
  onClick,
  children,
}: {
  pressed: boolean
  onClick: () => void
  children: ReactNode
}) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      onClick={onClick}
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium transition-colors',
        'focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50',
        pressed
          ? 'border-primary/40 bg-primary/10 text-primary'
          : 'border-border bg-transparent text-muted-foreground hover:bg-accent hover:text-foreground',
      )}
    >
      {children}
    </button>
  )
}

/**
 * The "filtered to nothing" state — distinct from a genuinely empty grid.
 * There IS data; the current filters just don't match any of it, so the fix
 * is to undo the filter, not to go create data. Per the ux-writing skill's
 * empty-state rule this reassures and offers the one relevant action, rather
 * than a bare "No results".
 */
export function FilterEmptyState({
  label,
  onClear,
}: {
  label: string
  onClear: () => void
}) {
  return (
    <div className="flex flex-col items-center gap-3 p-6 text-center text-sm text-muted-foreground">
      <p>{label}</p>
      <Button type="button" variant="outline" size="sm" onClick={onClear}>
        Clear filters
      </Button>
    </div>
  )
}
