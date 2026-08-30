import * as React from 'react'
import { createPortal } from 'react-dom'
import { AlertTriangle } from 'lucide-react'

import { Button } from '@/components/ui/button'

/**
 * Every element type a Tab key press can land the browser's focus on.
 * Mirrors `QuestionComposer.tsx`'s own `FOCUSABLE_SELECTOR` — the only
 * other real modal dialog in this app — rather than a second, slightly
 * different definition of "focusable".
 */
const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',')

function getFocusableElements(container: HTMLElement): HTMLElement[] {
  return Array.from(container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR))
}

export interface ConfirmDialogProps {
  open: boolean
  title: string
  description?: string
  confirmLabel: string
  cancelLabel?: string
  onConfirm: () => void
  onCancel: () => void
}

/**
 * A small, generic yes/no confirmation dialog — today only the "discard
 * this in-progress work?" guard on `LogReplacementForm` and `UploadPage`
 * (see `useUnsavedChangesGuard`), written with no knowledge of either
 * caller so the next destructive-action confirmation reaches for this
 * instead of hand-rolling another modal. This was the only reusable
 * confirmation pattern in the codebase to build against —
 * `QuestionComposer.tsx`'s dialog is a full multi-step wizard, not a
 * generic confirm — so this mirrors its accessibility mechanics rather
 * than its shape: a portal node appended to `document.body`, every other
 * top-level `body` child marked `inert` for the duration (removing them
 * from both Tab order and assistive tech's view without the visual jump
 * `display: none` would cause), and focus restored to whatever opened it
 * on close.
 *
 * Copy is the caller's job (ux-writing's confirmation-dialog formula: the
 * title asks, the confirm button restates the action in matching words —
 * "Discard this campaign draft?" / "Discard draft", never a bare "OK").
 */
export function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel,
  cancelLabel = 'Cancel',
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  const dialogRef = React.useRef<HTMLDivElement>(null)
  const titleId = React.useId()
  const descriptionId = React.useId()
  const [portalNode] = React.useState(() => document.createElement('div'))

  React.useEffect(() => {
    if (!open) return
    const previouslyFocused = document.activeElement as HTMLElement | null

    document.body.appendChild(portalNode)
    const outsideElements = Array.from(document.body.children).filter(
      (element) => element !== portalNode,
    ) as HTMLElement[]
    const previouslyInert = outsideElements.map((element) => element.hasAttribute('inert'))
    outsideElements.forEach((element) => {
      element.setAttribute('inert', '')
    })

    dialogRef.current?.focus()

    return () => {
      outsideElements.forEach((element, index) => {
        if (previouslyInert[index]) {
          element.setAttribute('inert', '')
        } else {
          element.removeAttribute('inert')
        }
      })
      portalNode.parentNode?.removeChild(portalNode)
      previouslyFocused?.focus?.()
    }
  }, [open, portalNode])

  /** Tab/Shift+Tab cycling, scoped to the dialog's own focusable elements. */
  function trapTabKey(event: React.KeyboardEvent<HTMLDivElement>) {
    if (event.key !== 'Tab') return
    const container = dialogRef.current
    if (!container) return

    const focusable = getFocusableElements(container)
    if (focusable.length === 0) {
      event.preventDefault()
      return
    }

    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    const activeIndex = focusable.indexOf(document.activeElement as HTMLElement)

    if (event.shiftKey) {
      if (activeIndex <= 0) {
        event.preventDefault()
        last.focus()
      }
    } else if (activeIndex === -1 || activeIndex === focusable.length - 1) {
      event.preventDefault()
      first.focus()
    }
  }

  if (!open) return null

  return createPortal(
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-4"
      onKeyDown={(event) => {
        if (event.key === 'Escape') {
          onCancel()
          return
        }
        trapTabKey(event)
      }}
    >
      {/* Clicking the backdrop is a Cancel, not a silent close: this dialog
          only ever gates a destructive discard, so "dismissed with no
          decision" must land on the safe side. */}
      <div aria-hidden="true" className="absolute inset-0 bg-black/40" onClick={onCancel} />
      <div
        ref={dialogRef}
        role="alertdialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={description ? descriptionId : undefined}
        tabIndex={-1}
        className="relative z-10 w-full max-w-sm rounded-xl border border-border bg-card p-5 shadow-xl outline-none"
      >
        <div className="flex items-start gap-2.5">
          <AlertTriangle
            className="mt-0.5 size-4 shrink-0 text-warning-text"
            aria-hidden="true"
          />
          <h2 id={titleId} className="text-sm font-semibold text-foreground">
            {title}
          </h2>
        </div>
        {description ? (
          <p id={descriptionId} className="mt-2 text-xs text-muted-foreground">
            {description}
          </p>
        ) : null}
        <div className="mt-4 flex justify-end gap-2">
          <Button type="button" variant="outline" size="sm" onClick={onCancel}>
            {cancelLabel}
          </Button>
          <Button type="button" variant="destructive" size="sm" onClick={onConfirm}>
            {confirmLabel}
          </Button>
        </div>
      </div>
    </div>,
    portalNode,
  )
}
