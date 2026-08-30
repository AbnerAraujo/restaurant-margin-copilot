import { useState, type ReactNode } from 'react'
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

/**
 * The shared text-search box: matches a row's visible primary fields (never
 * a hidden id).
 *
 * Reported live by the product owner: this used to call `onChange` — and so
 * narrow the grid — on every keystroke. The owner wants an explicit action
 * to apply a typed search instead of a live-as-you-type one (unlike the
 * dropdown/chip filters beside it, which stay instant: a single discrete
 * click doesn't warrant a confirm step the way open-ended typing does). This
 * is deliberately NOT a debounce — a debounce still applies automatically,
 * only delayed, which isn't what was asked for.
 *
 * `value`/`onChange` still name the APPLIED filter value (the prop contract
 * every caller already wires to `useTableFilter`'s `searchQuery`/
 * `setSearchQuery` is unchanged) — this component now stages what's typed in
 * local `draft` state and only calls `onChange` on Enter or the search-icon
 * button. `draft` re-syncs from `value` whenever the APPLIED value changes
 * out from under the user's own typing (Clear filters, a browser back/
 * forward restoring a different search from the URL) — that effect never
 * fires from typing itself, since typing only ever touches `draft`, so
 * nothing here can make the input's visible text lag a keystroke or fight
 * the user mid-edit.
 */
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
  const [draft, setDraft] = useState(value)
  // Adjusted during render, not an effect (React's own recommended pattern
  // for "reset local state when a prop changes"): an effect would commit the
  // stale `draft` to the screen for one frame before re-running, and would
  // also cost this component a second render on every APPLIED-value change.
  // Comparing against the last APPLIED value we've seen keeps this from ever
  // firing off the user's own typing, which only ever touches `draft`.
  const [lastSeenValue, setLastSeenValue] = useState(value)
  if (value !== lastSeenValue) {
    setLastSeenValue(value)
    setDraft(value)
  }

  function apply() {
    onChange(draft)
  }

  return (
    <FilterField id={id} label={label}>
      <div className="relative">
        <button
          type="button"
          onClick={apply}
          aria-label="Apply search"
          className="absolute left-1 top-1/2 flex size-6 -translate-y-1/2 items-center justify-center rounded-sm text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
        >
          <Search className="size-3.5" aria-hidden="true" />
        </button>
        <Input
          id={id}
          type="search"
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault()
              apply()
            }
          }}
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
