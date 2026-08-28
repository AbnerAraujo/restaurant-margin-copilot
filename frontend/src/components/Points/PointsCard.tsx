import { useEffect, useState } from 'react'
import {
  ArrowRight,
  BadgeCheck,
  Coins,
  ShieldCheck,
  Sparkles,
  type LucideIcon,
} from 'lucide-react'

import { getJson } from '@/lib/api'
import { cn } from '@/lib/utils'

// ---------------------------------------------------------------------------
// Wire shape of GET /api/badges (backend `internal/badges`). Points are
// DERIVED at read time from the badges in the same payload — there is no
// points table and nothing accrues a balance in the background. Every number
// this card shows is recomputed from `daily_reconciliation.discrepancy_flags`
// on each request, which is why a re-ingestion silently corrects it.
// ---------------------------------------------------------------------------

interface PointsLine {
  code: 'clean_close' | 'discrepancy_catcher'
  name: string
  count: number
  points_each: number
  points: number
}

interface BadgesResponse {
  badges: { date: string; code: string }[]
  points: { total: number; breakdown: PointsLine[] }
}

const LINE_ICON: Record<PointsLine['code'], LucideIcon> = {
  clean_close: BadgeCheck,
  discrepancy_catcher: ShieldCheck,
}

const LINE_TONE: Record<PointsLine['code'], string> = {
  clean_close: 'text-success-text',
  discrepancy_catcher: 'text-warning-text',
}

const LINE_BLURB: Record<PointsLine['code'], string> = {
  clean_close: 'days closed with nothing out of place',
  discrepancy_catcher: 'days where it caught something before you paid for it',
}

/**
 * Steward Points on the home screen: the real, derived balance plus the case
 * for why running the close is worth doing again tomorrow.
 *
 * The honesty line this card is built around: the balance is real — it is a
 * deterministic function of closes already run — and redemption is not built.
 * Those two facts are stated in the same card, in the same voice, because the
 * pitch only works if the reader can tell which half is shipping today. The
 * roadmap block is written as a destination, not as a disabled button: there
 * is nothing to click, and pretending otherwise would be exactly the
 * fabricated capability this whole product refuses to ship.
 */
export default function PointsCard({ className }: { className?: string }) {
  const [data, setData] = useState<BadgesResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    getJson<BadgesResponse>('/api/badges')
      .then((response) => {
        if (!cancelled) setData(response)
      })
      .catch((caught: unknown) => {
        if (!cancelled) {
          setError(caught instanceof Error ? caught.message : String(caught))
        }
      })
    return () => {
      cancelled = true
    }
  }, [])

  const total = data?.points.total ?? 0
  const breakdown = data?.points.breakdown ?? []
  const daysClosed = data?.badges.length ?? 0

  return (
    <section
      aria-label="Steward Points"
      className={cn(
        'overflow-hidden rounded-lg border border-border bg-card shadow-sm',
        className,
      )}
    >
      <div className="border-b border-border/60 bg-primary/[0.04] p-5">
        <p className="flex items-center gap-1.5 text-xs font-medium uppercase tracking-wide text-primary">
          <Coins className="size-3.5" aria-hidden="true" />
          Steward Points
        </p>
        <h2 className="mt-1 text-lg font-semibold tracking-tight text-foreground">
          Every close you run pays you twice.
        </h2>
        <p className="mt-1 max-w-prose text-sm leading-relaxed text-muted-foreground">
          Once in the margin this finds before it walks out the door. Again in
          points you bank toward winning it back.
        </p>
      </div>

      <div className="p-5">
        {error ? (
          <p className="text-sm text-muted-foreground">
            Couldn&apos;t reach the reconciliation engine, so there is no
            balance to show. Rather than a placeholder number:{' '}
            <span className="font-mono text-xs">{error}</span>
          </p>
        ) : (
          <>
            {/* The visible figure is split across two type sizes; the group
                carries one label so a screen reader hears the whole sentence
                instead of a bare number followed by a fragment. */}
            <div
              className="flex flex-wrap items-baseline gap-x-3 gap-y-1"
              role="status"
              aria-label={`${total} Steward Points from ${daysClosed} ${daysClosed === 1 ? 'day' : 'days'} already reconciled`}
            >
              <span
                aria-hidden="true"
                className="text-4xl font-semibold tabular-nums tracking-tight text-foreground"
              >
                {total.toLocaleString('en-US')}
              </span>
              <span aria-hidden="true" className="text-sm text-muted-foreground">
                points from{' '}
                <span className="font-medium text-foreground">
                  {daysClosed}
                </span>{' '}
                {daysClosed === 1 ? 'day' : 'days'} already reconciled
              </span>
            </div>

            {breakdown.length > 0 ? (
              <ul className="mt-4 space-y-2.5">
                {breakdown.map((line) => {
                  const Icon = LINE_ICON[line.code]
                  return (
                    <li
                      key={line.code}
                      className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 text-sm"
                    >
                      <Icon
                        className={cn('size-4 shrink-0 translate-y-0.5', LINE_TONE[line.code])}
                        aria-hidden="true"
                      />
                      <span className="font-medium text-foreground">
                        {line.count} × {line.name}
                      </span>
                      <span className="text-muted-foreground">
                        — {LINE_BLURB[line.code]}
                      </span>
                      <span className="ml-auto shrink-0 tabular-nums font-medium text-foreground">
                        +{line.points.toLocaleString('en-US')}
                        <span className="ml-1 text-xs font-normal text-muted-foreground">
                          ({line.points_each} each)
                        </span>
                      </span>
                    </li>
                  )
                })}
              </ul>
            ) : (
              <p className="mt-3 text-sm text-muted-foreground">
                No closes on file yet. Run a day&apos;s reconciliation and the
                first points land immediately — nothing here is awarded for
                signing up.
              </p>
            )}

            <p className="mt-4 border-t border-border/60 pt-3 text-xs leading-relaxed text-muted-foreground">
              A caught discrepancy is worth more than a quiet day
              ({/* keep the weights auditable inline, not buried in a tooltip */}
              {LINE_POINTS_HINT}) because that&apos;s where the money is. Both
              are computed from the closes themselves — no streak bonuses, no
              points for logging in.
            </p>
          </>
        )}
      </div>

      {/* Roadmap — visually and verbally separate from the earned balance
          above, so nothing here can be mistaken for a live feature. */}
      <div className="border-t border-border bg-muted/40 p-5">
        <p className="flex items-center gap-1.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">
          <Sparkles className="size-3.5" aria-hidden="true" />
          Where this is going — not built yet
        </p>
        <p className="mt-1.5 max-w-prose text-sm leading-relaxed text-foreground">
          Points are designed to become campaign credit. This system already
          tells you which promotion lost money; the next step is funding its
          replacement with the closes you&apos;ve already done — insight paid
          for in the currency it earned.
        </p>
        <p className="mt-2 flex max-w-prose items-start gap-1.5 text-xs leading-relaxed text-muted-foreground">
          <ArrowRight
            className="mt-0.5 size-3.5 shrink-0"
            aria-hidden="true"
          />
          <span>
            There is no redemption flow in this prototype and nothing to click
            here. Spending points needs an integration with real promotional
            tooling that this build has no API access to — so it&apos;s stated
            as the direction, not shipped as a button that does nothing. The
            balance above is the part that&apos;s real today.
          </span>
        </p>
      </div>
    </section>
  )
}

/**
 * The weights, spelled out in the copy rather than hidden. Kept as a constant
 * so the sentence can't drift away from the backend's own values without
 * someone editing this line deliberately.
 */
const LINE_POINTS_HINT = '25 vs 10'
