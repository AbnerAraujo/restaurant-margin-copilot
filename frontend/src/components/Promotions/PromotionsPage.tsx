import { useCallback, useEffect, useState } from 'react'
import { AlertTriangle, Megaphone, ShieldAlert, TrendingDown } from 'lucide-react'
import { useNavigate } from 'react-router-dom'

import {
  ASK_PAGE_PATH,
  buildPromoRoiFollowUpQuestion,
  type AskPageNavigationState,
  type PromoRoiDataPointClick,
} from '@/components/Charts/chartFollowUpQuestion'
import PromoRoiChart, {
  type PromotionRoiDatum,
} from '@/components/Charts/PromoRoiChart'
import LogReplacementForm from '@/components/Promotions/LogReplacementForm'
import type { SourceRowRef } from '@/components/Provenance/ProvenanceTag'
import { Chip, PageContainer, PageHeader, Panel } from '@/components/ui/page'
import { StatGroup, StatSkeleton } from '@/components/ui/stat'
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
  /** "ingested" (file pipeline) or "owner_created" (POST /api/promotions,
   * spec 002 User Story 3) — and, only on the latter with a replacement
   * claim, the flagged campaign it names. */
  origin?: string
  replaces_campaign_id?: string | null
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
    // Passed through so the chart can tell FR-013's permanent refusal
    // ("attribution_unavailable") apart from an owner-created promotion
    // that simply hasn't been through attribution yet ("not_yet_attributed")
    // — see PromoRoiChart's PromotionRoiDatum.reason doc comment.
    reason: promotion.reason,
    sourceRefs: collapseRefs(promotion.source_row_refs, promotion.period),
  }
}

/**
 * `/promotions` — "Promotion ROI", read live from Postgres. A campaign whose
 * incremental revenue cannot be attributed arrives with `roi: null` and
 * renders as an explicit unattributable/refused state, never a $0 bar.
 */
export default function PromotionsPage() {
  const navigate = useNavigate()
  // Spec 008 FR-001: `/promotions` and `/ask` are separate routes with no
  // shared chat context, so a chart click navigates to `/ask` carrying the
  // built question as router state (see chartFollowUpQuestion.ts's doc
  // comment, and ClosePage.tsx for the identical pattern).
  function handleChartDataPointClick(point: PromoRoiDataPointClick) {
    navigate(ASK_PAGE_PATH, {
      state: {
        autoSubmitQuestion: buildPromoRoiFollowUpQuestion(point),
      } satisfies AskPageNavigationState,
    })
  }

  const [promotions, setPromotions] = useState<PromotionApi[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  const loadPromotions = useCallback(() => {
    return getJson<PromotionsApiResponse>('/api/promotions')
      .then((response) => {
        setPromotions(response.promotions)
        setError(null)
      })
      .catch((caught: unknown) => {
        setError(caught instanceof Error ? caught.message : String(caught))
      })
  }, [])

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

  // Counts, not sums. Counting how many records carry a flag is not
  // arithmetic on money, so it stays on the honest side of the line the
  // toChartDatum comment draws: net is never recomputed here from spend and
  // revenue, because that would put a second implementation of the ROI
  // calculation on the client.
  const negativeCount =
    promotions?.filter((promotion) => promotion.flagged_negative).length ?? 0
  const unattributableCount =
    promotions?.filter((promotion) => promotion.roi === null).length ?? 0

  // The flagged rows this page already shows — LogReplacementForm's
  // "replaces" dropdown is populated ONLY from this list (SC-003: no step
  // requiring data the owner doesn't already have on screen).
  const flaggedCampaigns = (promotions ?? [])
    .filter((promotion) => promotion.flagged_negative)
    .map((promotion) => ({
      campaignId: promotion.campaign_id,
      platform: promotion.platform,
    }))

  // Steward-style proactive insight (spec 008 FR-010): a flagged campaign
  // "needs action" only until some OTHER campaign's replaces_campaign_id
  // names it — at that point the owner has already acted, and re-flagging
  // it would be nagging about a closed loop. Pure frontend derivation over
  // data already fetched; no backend change.
  const needsActionCampaigns = (promotions ?? []).filter(
    (promotion) =>
      promotion.flagged_negative &&
      !(promotions ?? []).some(
        (other) => other.replaces_campaign_id === promotion.campaign_id,
      ),
  )

  return (
    <PageContainer className="flex flex-col gap-5">
      <PageHeader
        eyebrow="Campaign performance"
        title="Promotion ROI"
        meta={
          promotions && promotions.length > 0 ? (
            <>
              <Chip icon={Megaphone}>{promotions.length} campaigns</Chip>
              {negativeCount > 0 ? (
                <Chip icon={TrendingDown} tone="destructive">
                  {negativeCount} lost money
                </Chip>
              ) : null}
              {unattributableCount > 0 ? (
                <Chip icon={ShieldAlert} tone="warning">
                  {unattributableCount} unattributable
                </Chip>
              ) : null}
            </>
          ) : null
        }
      />

      {error ? (
        <Panel role="alert" className="p-4 text-sm text-muted-foreground">
          Couldn&apos;t load campaigns from the backend, so there are no ROI
          figures to show: <span className="font-mono text-xs">{error}</span>
        </Panel>
      ) : null}

      {/* Steward-style proactive insight (spec 008 FR-010) — surfaced above
          the chart, without the owner having to ask, so an un-replaced
          losing campaign is never just one row among many. */}
      {needsActionCampaigns.length > 0 ? (
        <Panel
          role="status"
          className="flex flex-col gap-2 border-warning/25 bg-warning/10 p-4 sm:p-5"
        >
          <div className="flex items-center gap-2 text-sm font-semibold text-warning-text">
            <AlertTriangle className="size-4 shrink-0" aria-hidden="true" />
            {needsActionCampaigns.length === 1
              ? '1 campaign needs a decision'
              : `${needsActionCampaigns.length} campaigns need a decision`}
          </div>
          <ul className="flex flex-col gap-1.5 text-sm">
            {needsActionCampaigns.map((promotion) => (
              <li
                key={promotion.campaign_id}
                className="flex flex-wrap items-center justify-between gap-x-3 gap-y-0.5"
              >
                <span className="font-medium text-foreground">
                  {promotion.campaign_id}
                </span>
                <span className="text-xs text-muted-foreground">
                  {promotion.platform} · lost money, not yet replaced
                </span>
              </li>
            ))}
          </ul>
        </Panel>
      ) : null}

      {!error && promotions?.length === 0 ? (
        <Panel className="p-4 text-sm text-muted-foreground">
          No promotion records on file yet. Run{' '}
          <code className="font-mono text-xs">-ingest-promo</code> and this page
          fills in from the real ad-spend export.
        </Panel>
      ) : null}

      {!error && !promotions ? (
        <Panel className="p-5 sm:p-6">
          <StatGroup>
            <StatSkeleton />
            <StatSkeleton />
            <StatSkeleton />
            <StatSkeleton />
          </StatGroup>
        </Panel>
      ) : null}

      {promotions && promotions.length > 0 ? (
        <PromoRoiChart
          data={promotions.map(toChartDatum)}
          // Spend and attributed incremental revenue were already in the API
          // response and in this chart's own table, but that table sat behind
          // a "View as table" toggle. The reader could see that LUNCHFIX lost
          // $165 and not that it lost it on $220 of spend. On the dedicated
          // route there is room to show both without a click, so it opens
          // expanded. No figure is re-rendered anywhere else on this page,
          // which is what keeps one campaign from having two on-screen
          // renderings that could drift.
          defaultTableOpen
          onDataPointClick={handleChartDataPointClick}
        />
      ) : null}

      {/* User Story 3's write path: log a new promotion, optionally framed
          as replacing one of the flagged rows above. Rendered once
          `promotions` has loaded (successfully or empty) — a skeleton state
          for the form itself would be a form with nothing to reference yet. */}
      {!error && promotions ? (
        <LogReplacementForm
          flaggedCampaigns={flaggedCampaigns}
          onCreated={() => {
            void loadPromotions()
          }}
        />
      ) : null}
    </PageContainer>
  )
}
