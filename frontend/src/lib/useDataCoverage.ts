import { useEffect, useState } from 'react'

import { getJson } from '@/lib/api'

// The real, current answerable date range — fetched live rather than
// hardcoded, since it changes whenever the ingested dataset changes (it
// used to be a single 14-day window; it is now one continuous
// multi-year synthetic history, and will change again the next time data is
// regenerated). A hardcoded string here is exactly the kind of stale claim
// this product's own discipline argues against: the chat's empty state and
// the Help page both used to assert "the only period this data covers" with
// a date range that stopped being true the moment the live dataset grew.
interface ReconciliationRangeApi {
  start: string
  end: string
}

export interface DataCoverage {
  /** e.g. "2024-08-01 to 2026-08-14" — null until the real range loads. */
  label: string | null
  start: string | null
  end: string | null
}

const FALLBACK: DataCoverage = { label: null, start: null, end: null }

/**
 * Fetches the real, live data date range once per mount from the same
 * GET /api/reconciliation endpoint the Home page already reads. Returns the
 * fallback (all null) until it resolves — callers render nothing coverage-
 * specific rather than a stale placeholder while loading, matching this
 * product's own "never show a fabricated number, degrade honestly instead"
 * rule (Constitution Principle V) applied to a UI string, not just a figure.
 */
export function useDataCoverage(): DataCoverage {
  const [coverage, setCoverage] = useState<DataCoverage>(FALLBACK)

  useEffect(() => {
    let cancelled = false
    getJson<ReconciliationRangeApi>('/api/reconciliation')
      .then((data) => {
        if (cancelled) return
        setCoverage({
          label: `${data.start} to ${data.end}`,
          start: data.start,
          end: data.end,
        })
      })
      .catch(() => {
        // Network/backend failure: stay at the fallback rather than guess.
      })
    return () => {
      cancelled = true
    }
  }, [])

  return coverage
}
