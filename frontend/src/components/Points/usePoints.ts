import { useEffect, useState } from 'react'

import { getJson } from '@/lib/api'
import { POINTS_PER_BADGE } from './pointValues'

/**
 * Wire shape of GET /api/badges (backend `internal/badges`). Points are
 * DERIVED at read time from the badges in the same payload — there is no
 * points table and nothing accrues a balance in the background.
 */
export type BadgeCode =
  | 'clean_close'
  | 'discrepancy_catcher'
  | 'growth'
  | 'engagement'
  | 'campaign_creation'

export interface PointsLine {
  code: BadgeCode
  name: string
  count: number
  points_each: number
  points: number
}

/** One entry in `badges` — every field FR-009 requires beyond date/code:
 * `category` distinguishes which of the four badge categories a code
 * belongs to (Reconciliation is the only one with two codes), and the rest
 * are populated only for the badge types they apply to. */
export interface BadgeEntry {
  date: string
  code: BadgeCode
  name: string
  category: 'reconciliation' | 'growth' | 'engagement' | 'campaign_creation'
  campaign_id?: string
  replaces_campaign_id?: string
  usage_days?: number
}

export interface BadgesResponse {
  badges: BadgeEntry[]
  points: {
    total: number
    breakdown: PointsLine[]
    /** Points already redeemed against a promotion's spend, all-time. */
    spent: number
    /** total - spent — what's actually left to redeem right now. */
    available: number
  }
}

export interface PointsState {
  data: BadgesResponse | null
  error: string | null
}

/**
 * Single fetch of the live points balance, shared by every surface that
 * shows one (the Home tiles, the Points page, the Points card).
 *
 * Each caller fetches independently rather than through a shared context:
 * the endpoint is a cheap read against Postgres, the value is recomputed
 * per request anyway, and a context provider would add a lifetime and a
 * staleness question this prototype does not need. What matters is that
 * every surface reads the SAME endpoint, so no screen can show a balance
 * derived differently from another.
 */
export function usePoints(): PointsState {
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

  return { data, error }
}

export { POINTS_PER_BADGE }
