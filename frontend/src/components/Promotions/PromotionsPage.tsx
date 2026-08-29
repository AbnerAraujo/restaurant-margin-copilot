import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  AlertTriangle,
  ArrowDownNarrowWide,
  ArrowUpNarrowWide,
  Megaphone,
  ShieldAlert,
  TrendingDown,
} from 'lucide-react'
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
import { Button } from '@/components/ui/button'
import { Chip, PageContainer, PageHeader, Panel } from '@/components/ui/page'
import { Stat, StatGroup, StatSkeleton } from '@/components/ui/stat'
import { getJson } from '@/lib/api'
import { cn } from '@/lib/utils'

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
  /** "money" or "points" (spec 007's points-payment feature). Absent on
   * records ingested before that feature shipped — never assume "money". */
  payment_method?: string
  /** Only meaningful when `payment_method === 'points'`. */
  points_spent?: number
}

interface PromotionsApiResponse {
  promotions: PromotionApi[]
}

type RoiSortDirection = 'desc' | 'asc'

function formatSignedUsd(value: number): string {
  const magnitude = Math.abs(value).toLocaleString('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
  return value < 0 ? `−${magnitude}` : `+${magnitude}`
}

/**
 * Spec 008 FR-011: a platform's aggregate is the sum of only its
 * REAL, attributed campaigns' ROI — a platform with campaigns on file but
 * none attributed yet gets `sumRoi: null` (renders as "Not available" via
 * `Stat`), never a fabricated $0. Order follows first appearance in
 * `promotions`, which is itself the API's own order — no extra sort here.
 */
function aggregateRoiByPlatform(
  promotions: PromotionApi[],
): { platform: string; sumRoi: number | null }[] {
  const platforms = [...new Set(promotions.map((promotion) => promotion.platform))]
  return platforms.map((platform) => {
    const attributed = promotions.filter(
      (promotion) => promotion.platform === platform && promotion.roi !== null,
    )
    if (attributed.length === 0) return { platform, sumRoi: null }
    const sumRoi = attributed.reduce(
      (sum, promotion) => sum + Number(promotion.roi),
      0,
    )
    return { platform, sumRoi }
  })
}

/**
 * Spec 008 FR-012. Deliberately NOT a single comparator with a `-Infinity`
 * sort key for null `roi` (tasks.md's first-pass suggestion) — a shared
 * comparator puts nulls at opposite ends depending on `direction` (first
 * when ascending, last when descending), which is "consistent" only within
 * one direction, not the "sorted consistently to one end" FR-012 actually
 * asks for. Instead: sort only the real, attributed campaigns by `direction`,
 * then always append the unattributable/not-yet-attributed ones after —
 * they stay at the same end regardless of which direction the owner picks.
 */
function sortPromotionsByRoi(
  promotions: PromotionApi[],
  direction: RoiSortDirection,
): PromotionApi[] {
  const attributed = promotions.filter((promotion) => promotion.roi !== null)
  const unattributed = promotions.filter((promotion) => promotion.roi === null)
  attributed.sort((a, b) => {
    const diff = Number(a.roi) - Number(b.roi)
    return direction === 'asc' ? diff : -diff
  })
  return [...attributed, ...unattributed]
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
  // null = the API's own order (default); set only once the owner explicitly
  // asks to sort (FR-012) — never sorted implicitly on load.
  const [roiSortDirection, setRoiSortDirection] =
    useState<RoiSortDirection | null>(null)

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

  // Spec 008 FR-011: aggregate ROI per platform, real attributed campaigns
  // only. Spec 008 FR-012: sort toggle over the same list the chart/table
  // render, so "the list" (spec.md's wording) means one ordering, not two.
  const platformAggregates = useMemo(
    () => aggregateRoiByPlatform(promotions ?? []),
    [promotions],
  )
  const displayedPromotions = useMemo(() => {
    if (!promotions) return promotions
    return roiSortDirection
      ? sortPromotionsByRoi(promotions, roiSortDirection)
      : promotions
  }, [promotions, roiSortDirection])

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

      {!error && promotions && promotions.length > 0 ? (
        <Panel className="flex flex-col gap-4 p-5 sm:p-6">
          {/* Spec 008 FR-011: how each platform is doing IN AGGREGATE, not
              just per-campaign — a platform with campaigns on file but none
              attributed yet shows "Not available" via `Stat`, never a
              fabricated $0. */}
          <StatGroup>
            {platformAggregates.map(({ platform, sumRoi }) => (
              <Stat
                key={platform}
                label={`${platform} ROI`}
                value={sumRoi === null ? null : formatSignedUsd(sumRoi)}
                tone={
                  sumRoi === null ? 'neutral' : sumRoi < 0 ? 'negative' : 'positive'
                }
                tooltip={`Sum of ROI across ${platform}'s attributed campaigns — unattributable or not-yet-attributed campaigns are excluded, never counted as zero.`}
              />
            ))}
          </StatGroup>

          {/* Spec 008 FR-012: sort toggle over the same list the chart/table
              below render — one ordering, not two. Unattributable/not-yet-
              attributed campaigns stay at the end in EITHER direction; see
              sortPromotionsByRoi's doc comment for why. */}
          <div className="flex flex-wrap items-center gap-2 border-t border-border pt-4">
            <span className="text-xs font-medium text-muted-foreground">
              Sort by ROI:
            </span>
            <Button
              type="button"
              variant="outline"
              size="sm"
              aria-pressed={roiSortDirection === 'desc'}
              className={cn(roiSortDirection === 'desc' && 'bg-accent')}
              onClick={() =>
                setRoiSortDirection((current) =>
                  current === 'desc' ? null : 'desc',
                )
              }
            >
              <ArrowDownNarrowWide aria-hidden="true" />
              Highest first
            </Button>
            <Button
              type="button"
              variant="outline"
              size="sm"
              aria-pressed={roiSortDirection === 'asc'}
              className={cn(roiSortDirection === 'asc' && 'bg-accent')}
              onClick={() =>
                setRoiSortDirection((current) =>
                  current === 'asc' ? null : 'asc',
                )
              }
            >
              <ArrowUpNarrowWide aria-hidden="true" />
              Lowest first
            </Button>
          </div>
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

      {displayedPromotions && displayedPromotions.length > 0 ? (
        <PromoRoiChart
          data={displayedPromotions.map(toChartDatum)}
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
