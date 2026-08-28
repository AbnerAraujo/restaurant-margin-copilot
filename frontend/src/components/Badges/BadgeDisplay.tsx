import { BadgeCheck, ShieldCheck, type LucideIcon } from 'lucide-react'

import { cn } from '@/lib/utils'

/**
 * Reconciliation category only, per docs/product-strategy.md's built-now
 * scope. Growth, Engagement, and Campaign-Creation categories are named
 * there as roadmap-only and are deliberately not represented here.
 */
export type ReconciliationBadgeType = 'clean_close' | 'discrepancy_catcher'

export interface ReconciliationBadge {
  /** Stable key, e.g. `${date}-${type}`. */
  id: string
  type: ReconciliationBadgeType
  /** ISO date (yyyy-mm-dd) of the DailyReconciliation row this badge fired for. */
  date: string
  /**
   * Optional one-line context (e.g. "Caught 1 duplicate charge in the
   * delivery export"). When present the badge renders with a beat of its
   * own (a subtle single-line banner); when absent it renders as a compact
   * inline pill.
   */
  detail?: string
  /**
   * When set to more than 1, this badge stands in for that many identical
   * per-day badges (e.g. a period view's clean days) rather than a single
   * day's. Renders as one pill labeled with the count instead of one pill
   * per day — a 14-day period should never stack 12 identical unlabeled
   * "Clean Close" pills. `date` is ignored for display when `count` is
   * set; callers should still pass a real date for the id/key.
   */
  count?: number
}

export interface BadgeDisplayProps {
  badges: ReconciliationBadge[]
  className?: string
}

const BADGE_COPY: Record<ReconciliationBadgeType, { label: string; icon: LucideIcon }> = {
  clean_close: { label: 'Clean Close', icon: BadgeCheck },
  discrepancy_catcher: { label: 'Discrepancy Catcher', icon: ShieldCheck },
}

const TONE_CLASSES: Record<ReconciliationBadgeType, string> = {
  clean_close: 'border-success/25 bg-success/10 text-success-text',
  discrepancy_catcher: 'border-warning/25 bg-warning/10 text-warning-text',
}

function formatBadgeDate(iso: string): string {
  const parsed = new Date(`${iso}T00:00:00`)
  if (Number.isNaN(parsed.getTime())) return iso
  return parsed.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
}

/**
 * Quiet acknowledgment of a Reconciliation-category badge firing
 * ("Clean Close" / "Discrepancy Catcher") — a small pill, or a single-line
 * subtle banner when there's a detail worth a beat of its own. Never a
 * modal, toast, or celebratory takeover: this is a financial tool for a
 * time-poor owner, not a game.
 *
 * Renders nothing when there are no fired badges — the quiet state for a
 * badge system is silence, not an empty placeholder.
 */
function BadgeDisplay({ badges, className }: BadgeDisplayProps) {
  if (badges.length === 0) return null

  return (
    <ul className={cn('flex flex-col gap-1.5', className)} aria-label="Reconciliation badges">
      {badges.map((badge) => {
        const { label: baseLabel, icon: Icon } = BADGE_COPY[badge.type]
        const tone = TONE_CLASSES[badge.type]
        const isCount = Boolean(badge.count && badge.count > 1)
        const label = isCount ? `${baseLabel} ×${badge.count}` : baseLabel
        const dateLabel = formatBadgeDate(badge.date)
        const accessibleLabel = isCount
          ? `${baseLabel}, ${badge.count} days`
          : badge.detail
            ? `${label}, ${dateLabel}: ${badge.detail}`
            : `${label}, ${dateLabel}`

        return (
          <li key={badge.id}>
            {badge.detail ? (
              <div
                className={cn(
                  'flex items-center gap-2 rounded-md border px-3 py-2 text-xs font-medium',
                  tone,
                )}
                aria-label={accessibleLabel}
              >
                <Icon className="size-3.5 shrink-0" aria-hidden="true" />
                <span>{label}</span>
                <span className="text-muted-foreground font-normal">{badge.detail}</span>
                <span className="text-muted-foreground ml-auto shrink-0 font-normal">
                  {dateLabel}
                </span>
              </div>
            ) : (
              <span
                className={cn(
                  'inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium',
                  tone,
                )}
                aria-label={accessibleLabel}
              >
                <Icon className="size-3" aria-hidden="true" />
                {label}
              </span>
            )}
          </li>
        )
      })}
    </ul>
  )
}

export default BadgeDisplay
