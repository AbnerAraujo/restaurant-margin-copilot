import { useEffect, useState } from 'react'

import PromoRoiChart, {
  type PromotionRoiDatum,
} from '@/components/Charts/PromoRoiChart'
import type { SourceRowRef } from '@/components/Provenance/ProvenanceTag'
import { getJson } from '@/lib/api'

// ---------------------------------------------------------------------------
// Live wiring to GET /api/promotions (backend internal/httpapi/data.go),
// which serves the exact `mcptools.PromotionRoiView` shape the get_promotion_roi
// tool returns — so this page and a chat answer about the same campaign are
// reading one rendering of one record, not two.
// ---------------------------------------------------------------------------

interface SourceRowRefApi {
  file: string
  row: number
}

interface PromotionApi {
  platform: string
  campaign_id: string
  period: { start: string; end: string }
  spend: string
  attributed_incremental_orders: number | null
  attributed_incremental_revenue: string | null
  /** Null exactly when attribution is unavailable (FR-013), with a reason. */
  roi: string | null
  reason?: string
  flagged_negative: boolean
  source_row_refs: SourceRowRefApi[]
}

interface PromotionsApiResponse {
  promotions: PromotionApi[]
}

function collapseRefs(
  refs: SourceRowRefApi[],
  period: { start: string; end: string },
): SourceRowRef[] {
  const byFile = new Map<string, number[]>()
  for (const ref of refs) {
    byFile.set(ref.file, [...(byFile.get(ref.file) ?? []), ref.row])
  }
  return [...byFile.entries()].map(([file, rows]) => ({
    source_file: file,
    row_start: Math.min(...rows),
    row_end: Math.max(...rows),
    period_start: period.start,
    period_end: period.end,
  }))
}

/**
 * The chart takes a display name per campaign, which the API does not carry —
 * campaign_id is the only identifier the deterministic layer has. Rather than
 * invent a marketing-style name (the previous hardcoded page did exactly
 * that: "In-App Boost — Weekday Lunch" appears nowhere in the fixtures), the
 * id doubles as the name and the platform supplies the human context.
 */
function toChartDatum(promotion: PromotionApi): PromotionRoiDatum {
  const revenue =
    promotion.attributed_incremental_revenue === null
      ? null
      : Number(promotion.attributed_incremental_revenue)
  return {
    campaignId: promotion.campaign_id,
    campaignName: promotion.campaign_id,
    platform: promotion.platform,
    spend: Number(promotion.spend),
    attributedIncrementalRevenue: revenue,
    // `roi` is already the net (attributed incremental revenue minus spend),
    // computed in Go — never recomputed here from the two components, which
    // would put a second arithmetic implementation on the client.
    net: promotion.roi === null ? null : Number(promotion.roi),
    sourceRefs: collapseRefs(promotion.source_row_refs, promotion.period),
  }
}

/**
 * `/promotions` — "Promotion ROI", read live from Postgres. A campaign whose
 * incremental revenue cannot be attributed arrives with `roi: null` and
 * renders as an explicit unattributable/refused state, never a $0 bar.
 */
export default function PromotionsPage() {
  const [promotions, setPromotions] = useState<PromotionApi[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    getJson<PromotionsApiResponse>('/api/promotions')
      .then((response) => {
        if (!cancelled) setPromotions(response.promotions)
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

  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-4">
      <h1 className="text-2xl font-semibold tracking-tight text-foreground">
        Promotion ROI
      </h1>

      {error ? (
        <p
          role="alert"
          className="rounded-lg border border-border bg-card p-4 text-sm text-muted-foreground"
        >
          Couldn&apos;t load campaigns from the backend, so there are no ROI
          figures to show: <span className="font-mono text-xs">{error}</span>
        </p>
      ) : null}

      {!error && promotions?.length === 0 ? (
        <p className="rounded-lg border border-border bg-card p-4 text-sm text-muted-foreground">
          No promotion records on file yet. Run{' '}
          <code className="font-mono text-xs">-ingest-promo</code> and this page
          fills in from the real ad-spend export.
        </p>
      ) : null}

      {promotions && promotions.length > 0 ? (
        <PromoRoiChart data={promotions.map(toChartDatum)} />
      ) : null}
    </div>
  )
}
