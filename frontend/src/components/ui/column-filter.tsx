import { useRef, useState, type KeyboardEvent } from 'react'
import { Popover as PopoverPrimitive } from 'radix-ui'
import { Filter } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'

/**
 * The Excel/Sheets-style small filter affordance a column header renders,
 * plus its dropdown — the single reusable piece every column-filterable
 * table (`DataGrid` today) wires up per column, rather than a bespoke
 * popover per table. Built on the same Radix `Popover` primitive
 * `ui/tooltip.tsx` already builds `Tooltip` on (a project dependency, not a
 * new one), which supplies the accessible plumbing a hand-rolled popover
 * would otherwise have to reimplement: focus moves into the panel on open,
 * `Escape` closes it and returns focus to this trigger, and a click outside
 * closes it too.
 *
 * Three interchangeable bodies, one per `useColumnFilters` filter type:
 * `categorical` (a checklist of the column's distinct values — applies each
 * checkbox immediately, same as the existing status/ROI filter chips
 * elsewhere in this app, since a single discrete choice doesn't warrant a
 * confirm step), `text` and `numeric` (both stage input locally and apply
 * only on Enter or the Apply button — the same discipline the shared
 * `FilterSearchInput` follows for the filter-bar's own search box, so typing
 * a column filter never narrows the grid mid-keystroke).
 */

type ColumnFilterButtonProps =
  | {
      type: 'categorical'
      columnLabel: string
      options: string[]
      selected: Set<string>
      onToggle: (value: string) => void
      onClear: () => void
    }
  | {
      type: 'text'
      columnLabel: string
      query: string
      onApply: (query: string) => void
      onClear: () => void
    }
  | {
      type: 'numeric'
      columnLabel: string
      min: string
      max: string
      onApply: (min: string, max: string) => void
      onClear: () => void
    }

function isActive(props: ColumnFilterButtonProps): boolean {
  if (props.type === 'categorical') return props.selected.size > 0
  if (props.type === 'text') return props.query.trim() !== ''
  return props.min.trim() !== '' || props.max.trim() !== ''
}

function activeSummary(props: ColumnFilterButtonProps): string | null {
  if (!isActive(props)) return null
  if (props.type === 'categorical') {
    return `${props.selected.size} ${props.selected.size === 1 ? 'value' : 'values'} selected`
  }
  if (props.type === 'text') return `"${props.query.trim()}"`
  const { min, max } = props
  if (min && max) return `${min}–${max}`
  if (min) return `${min} or more`
  return `${max} or less`
}

export function ColumnFilterButton(props: ColumnFilterButtonProps) {
  const [open, setOpen] = useState(false)
  const active = isActive(props)
  const summary = activeSummary(props)

  return (
    <PopoverPrimitive.Root open={open} onOpenChange={setOpen}>
      <PopoverPrimitive.Trigger asChild>
        <button
          type="button"
          aria-label={
            active
              ? `Filter by ${props.columnLabel}, active: ${summary}`
              : `Filter by ${props.columnLabel}`
          }
          className={cn(
            // size-6 (24px) is WCAG 2.2's 2.5.8 minimum target size — found
            // live at size-5 (20px), under the minimum and only 22px
            // centre-to-centre from the adjacent info button (also under
            // 24px), which the spacing exception doesn't rescue since it
            // requires >=24px between centres. The icon itself stays
            // size-3 (see below); only the hit target grows.
            'inline-flex size-6 shrink-0 items-center justify-center rounded-sm transition-colors',
            'focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50',
            active
              ? 'text-primary'
              : 'text-muted-foreground/70 hover:text-foreground',
          )}
        >
          <span className="relative inline-flex">
            <Filter className="size-3" aria-hidden="true" />
            {active ? (
              <span
                aria-hidden="true"
                className="absolute -right-0.5 -top-0.5 size-1.5 rounded-full bg-primary"
              />
            ) : null}
          </span>
        </button>
      </PopoverPrimitive.Trigger>
      <PopoverPrimitive.Portal>
        <PopoverPrimitive.Content
          role="dialog"
          aria-label={`Filter by ${props.columnLabel}`}
          sideOffset={6}
          align="start"
          collisionPadding={8}
          className={cn(
            'z-50 w-56 rounded-lg border border-border bg-popover p-3 text-popover-foreground shadow-lg',
            'animate-in fade-in-0 zoom-in-95 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95',
          )}
        >
          <ColumnFilterPanel {...props} onRequestClose={() => setOpen(false)} />
        </PopoverPrimitive.Content>
      </PopoverPrimitive.Portal>
    </PopoverPrimitive.Root>
  )
}

function ColumnFilterPanel(props: ColumnFilterButtonProps & { onRequestClose: () => void }) {
  if (props.type === 'categorical') return <CategoricalPanel {...props} />
  if (props.type === 'text') return <TextPanel {...props} />
  return <NumericPanel {...props} />
}

function PanelHeading({ columnLabel }: { columnLabel: string }) {
  return (
    <p className="mb-2 text-xs font-medium text-foreground">Filter by {columnLabel}</p>
  )
}

function ClearLink({ onClear, disabled }: { onClear: () => void; disabled: boolean }) {
  return (
    <button
      type="button"
      onClick={onClear}
      disabled={disabled}
      className="mt-2 text-xs font-medium text-primary underline-offset-2 hover:underline focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:pointer-events-none disabled:text-muted-foreground disabled:no-underline"
    >
      Clear filter
    </button>
  )
}

function CategoricalPanel({
  columnLabel,
  options,
  selected,
  onToggle,
  onClear,
}: Extract<ColumnFilterButtonProps, { type: 'categorical' }>) {
  // "Clear filter" disables itself the instant selected.size hits 0 (see its
  // `disabled` prop below) -- found live: a button that goes disabled while
  // it still holds focus can't hold focus any longer, and the browser drops
  // it all the way to <body>, silently losing the keyboard user's place in
  // the panel. Moving focus onto this panel's own root (a stable element
  // that never disables) keeps it inside the popover instead.
  const panelRef = useRef<HTMLDivElement>(null)
  function handleClear() {
    onClear()
    panelRef.current?.focus()
  }
  return (
    <div ref={panelRef} tabIndex={-1} className="outline-none">
      <PanelHeading columnLabel={columnLabel} />
      {options.length === 0 ? (
        <p className="text-xs text-muted-foreground">No values to filter by yet.</p>
      ) : (
        <ul className="flex max-h-48 flex-col gap-1 overflow-y-auto" role="group" aria-label={columnLabel}>
          {options.map((option) => {
            const inputId = `column-filter-${columnLabel}-${option}`.replace(/\s+/g, '-')
            return (
              <li key={option}>
                <label htmlFor={inputId} className="flex cursor-pointer items-center gap-2 rounded-sm px-1 py-1 text-xs text-foreground hover:bg-accent">
                  <input
                    id={inputId}
                    type="checkbox"
                    checked={selected.has(option)}
                    onChange={() => onToggle(option)}
                    className="size-3.5 shrink-0 rounded-sm border-input accent-primary focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
                  />
                  <span className="truncate">{option || '—'}</span>
                </label>
              </li>
            )
          })}
        </ul>
      )}
      <ClearLink onClear={handleClear} disabled={selected.size === 0} />
    </div>
  )
}

function TextPanel({
  columnLabel,
  query,
  onApply,
  onClear,
}: Extract<ColumnFilterButtonProps, { type: 'text' }>) {
  const [draft, setDraft] = useState(query)
  // Adjusted during render, not an effect — see FilterSearchInput's own
  // identical comment in `filter-bar.tsx` for the full reasoning.
  const [lastSeenQuery, setLastSeenQuery] = useState(query)
  if (query !== lastSeenQuery) {
    setLastSeenQuery(query)
    setDraft(query)
  }

  function apply() {
    onApply(draft)
  }

  // See CategoricalPanel's identical comment: "Clear filter" disables itself
  // the moment the query empties, and a focused element that goes disabled
  // drops focus to <body> rather than staying inside the popover.
  const panelRef = useRef<HTMLDivElement>(null)
  function handleClear() {
    onClear()
    panelRef.current?.focus()
  }

  return (
    <div ref={panelRef} tabIndex={-1} className="outline-none">
      <PanelHeading columnLabel={columnLabel} />
      <div className="flex items-center gap-1.5">
        <Input
          autoFocus
          aria-label={`Contains, ${columnLabel}`}
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault()
              apply()
            }
          }}
          placeholder="Contains…"
          className="h-8 flex-1 text-xs"
        />
        <Button type="button" size="sm" onClick={apply}>
          Apply
        </Button>
      </div>
      <ClearLink onClear={handleClear} disabled={query.trim() === ''} />
    </div>
  )
}

function NumericPanel({
  columnLabel,
  min,
  max,
  onApply,
  onClear,
}: Extract<ColumnFilterButtonProps, { type: 'numeric' }>) {
  const [draftMin, setDraftMin] = useState(min)
  const [draftMax, setDraftMax] = useState(max)
  // Adjusted during render, not an effect — see FilterSearchInput's own
  // identical comment in `filter-bar.tsx` for the full reasoning.
  const [lastSeenMin, setLastSeenMin] = useState(min)
  const [lastSeenMax, setLastSeenMax] = useState(max)
  if (min !== lastSeenMin) {
    setLastSeenMin(min)
    setDraftMin(min)
  }
  if (max !== lastSeenMax) {
    setLastSeenMax(max)
    setDraftMax(max)
  }

  function apply() {
    onApply(draftMin, draftMax)
  }

  function handleKeyDown(event: KeyboardEvent) {
    if (event.key === 'Enter') {
      event.preventDefault()
      apply()
    }
  }

  // See CategoricalPanel's identical comment: "Clear filter" disables itself
  // the moment both bounds empty, and a focused element that goes disabled
  // drops focus to <body> rather than staying inside the popover.
  const panelRef = useRef<HTMLDivElement>(null)
  function handleClear() {
    onClear()
    panelRef.current?.focus()
  }

  return (
    <div ref={panelRef} tabIndex={-1} className="outline-none">
      <PanelHeading columnLabel={columnLabel} />
      <div className="flex items-center gap-1.5">
        <Input
          autoFocus
          type="number"
          inputMode="decimal"
          aria-label={`Minimum, ${columnLabel}`}
          value={draftMin}
          onChange={(event) => setDraftMin(event.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Min"
          className="h-8 w-full text-xs"
        />
        <span className="text-xs text-muted-foreground" aria-hidden="true">
          &ndash;
        </span>
        <Input
          type="number"
          inputMode="decimal"
          aria-label={`Maximum, ${columnLabel}`}
          value={draftMax}
          onChange={(event) => setDraftMax(event.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Max"
          className="h-8 w-full text-xs"
        />
      </div>
      <Button type="button" size="sm" className="mt-2 w-full" onClick={apply}>
        Apply
      </Button>
      <ClearLink onClear={handleClear} disabled={min.trim() === '' && max.trim() === ''} />
    </div>
  )
}
