import type { LucideIcon } from 'lucide-react'
import { forwardRef, type HTMLAttributes, type ReactNode } from 'react'

import { cn } from '@/lib/utils'

/**
 * Page and panel chrome, factored out of the five route components that each
 * hand-rolled `mx-auto flex max-w-3xl flex-col gap-4` and their own
 * `rounded-lg border border-border bg-card p-4 sm:p-5 shadow-sm`.
 *
 * Two things change with the extraction, both from the linear-app structural
 * direction (layout/density/interaction only — the palette stays this
 * product's own):
 *
 *  1. The measure goes from 768px to `--content-max` (1200px). At 1512px the
 *     old cap left roughly 55% of the content area empty, which is most of
 *     what "too little application" meant.
 *  2. Depth comes from a hairline border and a surface step, not from a
 *     drop shadow on every box. `shadow-sm` on every card is one of the
 *     banned defaults in the taste doctrine, and it was on every card here.
 */

export function PageContainer({
  children,
  className,
}: {
  children: ReactNode
  className?: string
}) {
  return (
    <div className={cn('mx-auto w-full max-w-content', className)}>
      {children}
    </div>
  )
}

export interface PageHeaderProps {
  /** Micro overline naming the surface. Real label, never "SECTION 01". */
  eyebrow?: string
  title: string
  /**
   * Chips, counts, or a period label. Deliberately typed as nodes rather than
   * a description string: the point of the redesign is that page context
   * arrives as structured metadata, not as a paragraph under the heading.
   */
  meta?: ReactNode
  /** Right-aligned controls. */
  actions?: ReactNode
  className?: string
}

export function PageHeader({
  eyebrow,
  title,
  meta,
  actions,
  className,
}: PageHeaderProps) {
  return (
    <header
      className={cn(
        'flex flex-wrap items-end justify-between gap-x-6 gap-y-3',
        className,
      )}
    >
      <div className="min-w-0">
        {eyebrow ? (
          <p className="text-micro font-medium uppercase tracking-wider text-muted-foreground">
            {eyebrow}
          </p>
        ) : null}
        <h1 className="mt-0.5 text-2xl font-semibold tracking-tight text-foreground">
          {title}
        </h1>
        {meta ? (
          <div className="mt-2 flex flex-wrap items-center gap-1.5">{meta}</div>
        ) : null}
      </div>
      {actions ? (
        <div className="flex shrink-0 items-center gap-2">{actions}</div>
      ) : null}
    </header>
  )
}

/**
 * A content surface. `tone="muted"` is the recessed step used for a block that
 * must read as clearly not-live (the points roadmap), so "this is not built"
 * is carried by the surface as well as by the words.
 */
export function Panel({
  children,
  className,
  tone = 'card',
  as: Element = 'section',
  ...rest
}: {
  children: ReactNode
  className?: string
  tone?: 'card' | 'muted'
  as?: 'section' | 'div' | 'article'
} & React.HTMLAttributes<HTMLElement>) {
  return (
    <Element
      className={cn(
        'rounded-xl border border-border',
        tone === 'muted' ? 'bg-muted/40' : 'bg-card',
        className,
      )}
      {...rest}
    >
      {children}
    </Element>
  )
}

export function PanelHeader({
  eyebrow,
  title,
  actions,
  className,
}: {
  eyebrow?: string
  title: string
  actions?: ReactNode
  className?: string
}) {
  return (
    <div
      className={cn(
        'flex flex-wrap items-start justify-between gap-x-4 gap-y-2',
        className,
      )}
    >
      <div className="min-w-0">
        {eyebrow ? (
          <p className="text-micro font-medium uppercase tracking-wider text-muted-foreground">
            {eyebrow}
          </p>
        ) : null}
        <h2 className="text-sm font-semibold tracking-tight text-foreground">
          {title}
        </h2>
      </div>
      {actions ? (
        <div className="flex shrink-0 items-center gap-2">{actions}</div>
      ) : null}
    </div>
  )
}

export type ChipTone = 'neutral' | 'brand' | 'success' | 'warning' | 'destructive'

const CHIP_TONE: Record<ChipTone, string> = {
  neutral: 'border-border bg-muted/60 text-muted-foreground',
  brand: 'border-primary/25 bg-primary/10 text-primary',
  success: 'border-success/25 bg-success/10 text-success-text',
  warning: 'border-warning/25 bg-warning/10 text-warning-text',
  destructive: 'border-destructive/25 bg-destructive/10 text-destructive-text',
}

/**
 * A metadata chip. Replaces the pattern of writing context into a sentence
 * ("for 2026-08-01 through 2026-08-14, the only period this data covers") when
 * the context is really a set of discrete facts: a period, a model name, a
 * tool name, a count.
 *
 * Every tone pairs colour with a word, and an icon where one is available, so
 * a chip is never read by hue alone.
 *
 * Forwards its ref and spreads any remaining `<span>` attributes so a caller
 * can wrap it in the shared `Tooltip` primitive (`ui/tooltip.tsx`) via
 * `<TooltipTrigger asChild>` — that needs a real DOM node to attach a ref and
 * pointer/focus handlers to, which a plain non-forwarding component can't
 * give it. `ChatPanel.tsx`'s tool/cache chips are the first callers to do
 * this, replacing a native `title=` tooltip (unstyled, mouse-only in most
 * browsers) with the app's own styled, keyboard-reachable one.
 */
export const Chip = forwardRef<
  HTMLSpanElement,
  {
    children: ReactNode
    icon?: LucideIcon
    tone?: ChipTone
    className?: string
  } & HTMLAttributes<HTMLSpanElement>
>(function Chip({ children, icon: Icon, tone = 'neutral', className, ...rest }, ref) {
  return (
    <span
      ref={ref}
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-micro font-medium',
        CHIP_TONE[tone],
        className,
      )}
      {...rest}
    >
      {Icon ? <Icon className="size-3 shrink-0" aria-hidden="true" /> : null}
      {children}
    </span>
  )
})
