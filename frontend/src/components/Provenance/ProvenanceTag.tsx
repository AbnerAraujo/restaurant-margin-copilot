import { useId, useState } from 'react'
import { FileText, X } from 'lucide-react'

import { cn } from '@/lib/utils'

/**
 * Mirrors `DailyReconciliation.source_row_refs` / `PromotionRoiRecord.source_row_refs`
 * from data-model.md: "File + row references — provenance (FR-005)". The
 * backend stores this as jsonb; these are the fields FR-005 requires be
 * shown for every number (source file, rows, period).
 */
export interface SourceRowRef {
  source_file: string
  row_start: number
  row_end: number
  /**
   * ISO 8601 date (YYYY-MM-DD), or omitted. Equal to period_end for a
   * single-day ref. The live `/api/ask` endpoint's `provenance_refs` are
   * flat "file:row" strings with no period info (httpapi.AskResponse), so a
   * ref built from a live answer has no period — omit rather than fake one.
   */
  period_start?: string
  period_end?: string
}

export interface ProvenanceTagProps {
  /**
   * The source rows this number was computed from. Per data-model.md,
   * `refusal_fired = true` implies no provenance — pass an empty array (or
   * omit) and this component renders nothing rather than a fake citation.
   */
  refs: SourceRowRef[]
  className?: string
}

function formatRowRange(ref: SourceRowRef): string {
  return ref.row_start === ref.row_end
    ? `row ${ref.row_start}`
    : `rows ${ref.row_start}–${ref.row_end}`
}

function formatMonthDay(isoDate: string): string {
  const date = new Date(`${isoDate}T00:00:00Z`)
  return date.toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    timeZone: 'UTC',
  })
}

function formatPeriod(ref: SourceRowRef): string | null {
  if (!ref.period_start || !ref.period_end) {
    return null
  }
  if (ref.period_start === ref.period_end) {
    return formatMonthDay(ref.period_start)
  }

  const start = new Date(`${ref.period_start}T00:00:00Z`)
  const end = new Date(`${ref.period_end}T00:00:00Z`)
  const sameMonth =
    start.getUTCFullYear() === end.getUTCFullYear() &&
    start.getUTCMonth() === end.getUTCMonth()

  return sameMonth
    ? `${formatMonthDay(ref.period_start)}–${end.getUTCDate()}`
    : `${formatMonthDay(ref.period_start)} – ${formatMonthDay(ref.period_end)}`
}

function citationLabel(ref: SourceRowRef): string {
  const period = formatPeriod(ref)
  return period
    ? `${ref.source_file} · ${formatRowRange(ref)} · ${period}`
    : `${ref.source_file} · ${formatRowRange(ref)}`
}

/**
 * The provenance citation attached to a computed number (FR-005). Renders as
 * a small dotted-underline trigger — a deliberately quiet affordance per
 * design-tokens.md §2 — that expands into a detail panel listing every
 * source row this figure traces back to, so it reads as a trust signal a
 * time-poor owner can check without it competing with the number itself.
 */
function ProvenanceTag({ refs, className }: ProvenanceTagProps) {
  const [open, setOpen] = useState(false)
  const panelId = useId()

  if (refs.length === 0) {
    return null
  }

  // "source FILES", not the bare "sources" this used to say — on `/close`
  // this citation's own trigger sits right beside "Gross sales" own
  // "N sources" caption (Close/ClosePage.tsx), which counts distinct SALES
  // CHANNELS, a completely different denominator. Two adjacent captions
  // both saying "sources" read as if a source were missing from one of them
  // when neither was wrong — a QA pass found exactly that confusion. This
  // component always counts distinct provenance FILES (`source_file` per
  // ref), so naming that explicitly disambiguates it everywhere it renders,
  // not just on this one page.
  const triggerLabel =
    refs.length === 1 ? citationLabel(refs[0]) : `${refs.length} source files`

  return (
    <span className={cn('relative inline-block', className)}>
      <button
        type="button"
        aria-expanded={open}
        aria-controls={panelId}
        onClick={() => setOpen((wasOpen) => !wasOpen)}
        className="inline-flex items-center gap-1 text-xs text-muted-foreground underline decoration-dotted underline-offset-2 hover:text-foreground focus-visible:text-foreground focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:rounded-sm"
      >
        <FileText className="size-3" aria-hidden="true" />
        {triggerLabel}
      </button>

      {open && (
        <div
          id={panelId}
          role="group"
          aria-label="Source citations"
          className="absolute z-10 mt-1.5 w-max max-w-xs rounded-md border border-border bg-popover p-2.5 shadow-sm"
        >
          <div className="mb-1.5 flex items-center justify-between gap-4">
            <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
              Source file{refs.length > 1 ? 's' : ''}
            </span>
            <button
              type="button"
              onClick={() => setOpen(false)}
              aria-label="Dismiss citation detail"
              className="rounded-sm text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
            >
              <X className="size-3" aria-hidden="true" />
            </button>
          </div>
          <ul className="space-y-1">
            {refs.map((ref, index) => {
              const period = formatPeriod(ref)
              return (
                <li
                  key={`${ref.source_file}-${ref.row_start}-${ref.row_end}-${index}`}
                  className="text-xs leading-relaxed text-popover-foreground"
                >
                  <span className="font-medium">{ref.source_file}</span>
                  <span className="text-muted-foreground">
                    {' '}
                    · {formatRowRange(ref)}
                    {period ? ` · ${period}` : ''}
                  </span>
                </li>
              )
            })}
          </ul>
        </div>
      )}
    </span>
  )
}

export default ProvenanceTag
