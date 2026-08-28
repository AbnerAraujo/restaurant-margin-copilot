import { useEffect, useState } from 'react'

import { getJson } from '@/lib/api'
import { POINTS_PER_BADGE } from './pointValues'

/**
 * Wire shape of GET /api/badges (backend `internal/badges`). Points are
 * DERIVED at read time from the badges in the same payload — there is no
 * points table and nothing accrues a balance in the background.
 */
export interface PointsLine {
  code: 'clean_close' | 'discrepancy_catcher'
  name: string
  count: number
  points_each: number
  points: number
}

export interface BadgesResponse {
  badges: { date: string; code: string }[]
  points: { total: number; breakdown: PointsLine[] }
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
