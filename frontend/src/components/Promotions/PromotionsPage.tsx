import { useCallback, useEffect, useId, useMemo, useState } from 'react'
import {
  AlertTriangle,
  ArrowDownNarrowWide,
  ArrowUpNarrowWide,
  ChevronDown,
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
import {
  FilterBar,
  FilterChip,
  FilterEmptyState,
  FilterSearchInput,
  FilterSelect,
} from '@/components/ui/filter-bar'
import { Chip, PageContainer, PageHeader, Panel } from '@/components/ui/page'
import { Stat, StatGroup, StatSkeleton } from '@/components/ui/stat'
import { getJson } from '@/lib/api'
import { explainRequestFailure } from '@/lib/requestFailure'
import { useTableFilter } from '@/lib/useTableFilter'
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

// How many needs-action campaigns render before the "needs a decision"
// panel collapses the rest behind a "Show N more" toggle (spec 008 FR-010).
const NEEDS_ACTION_VISIBLE_COUNT = 3

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

type RoiSign = 'profitable' | 'lost' | 'unattributable'

/** The ROI-sign filter chip's dimension value for one campaign — the same
 * three-way split the header chips (`negativeCount`/`unattributableCount`)
 * already summarize, just per-row instead of counted. */
function roiSign(promotion: PromotionApi): RoiSign {
  if (promotion.roi === null) return 'unattributable'
  return Number(promotion.roi) < 0 ? 'lost' : 'profitable'
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
 * that: "In-App Boost — Weekday Lunch" appears nowhere in the dataset), the
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
  // Collapsed by default once the needs-action list grows past
  // NEEDS_ACTION_VISIBLE_COUNT — a real restaurant can accumulate many
  // never-replaced losing campaigns over a long operating history, and
  // stacking every single one into this alert makes it read as broken
  // rather than actionable. Same discipline as BadgeDisplay's
  // BadgeSummaryRow collapsing repeated Discrepancy Catcher days.
  const [needsActionExpanded, setNeedsActionExpanded] = useState(false)
  const roiSortLabelId = useId()

  const loadPromotions = useCallback(() => {
    return getJson<PromotionsApiResponse>('/api/promotions')
      .then((response) => {
        setPromotions(response.promotions)
        setError(null)
      })
      .catch((caught: unknown) => {
        setError(explainRequestFailure(caught))
      })
  }, [])

  useEffect(() => {
    let cancelled = false
    getJson<PromotionsApiResponse>('/api/promotions')
      .then((response) => {
        if (!cancelled) setPromotions(response.promotions)
      })
      .catch((caught: unknown) => {
        if (!cancelled) setError(explainRequestFailure(caught))
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

  // A flagged campaign counts as "already replaced" once some OTHER
  // campaign's replaces_campaign_id names it — computed once here so every
  // derivation below (the "needs a decision" panel AND the replaces
  // dropdown) agrees on the same fact. This fixes a real double-award bug a
  // QA pass found: the dropdown used to be built from `flagged_negative`
  // alone, so a campaign that had already been replaced once (and had
  // already dropped off the "needs a decision" panel) was still offered
  // again — selecting it a second time earned a SECOND Campaign Launcher
  // badge and a second points award for the same real replacement. The
  // backend now also refuses to create that second record server-side
  // (internal/httpapi/promotions_create.go), but the dropdown must not
  // offer the choice in the first place.
  const replacedCampaignIds = new Set(
    (promotions ?? [])
      .map((promotion) => promotion.replaces_campaign_id)
      .filter((id): id is string => Boolean(id)),
  )

  // The flagged, NOT-YET-replaced rows this page already shows —
  // LogReplacementForm's "replaces" dropdown is populated ONLY from this
  // list (SC-003: no step requiring data the owner doesn't already have on
  // screen), and must never offer a campaign that's already been replaced.
  const flaggedCampaigns = (promotions ?? [])
    .filter(
      (promotion) =>
        promotion.flagged_negative &&
        !replacedCampaignIds.has(promotion.campaign_id),
    )
    .map((promotion) => ({
      campaignId: promotion.campaign_id,
      platform: promotion.platform,
    }))

  // Steward-style proactive insight (spec 008 FR-010): a flagged campaign
  // "needs action" only until some OTHER campaign's replaces_campaign_id
  // names it — at that point the owner has already acted, and re-flagging
  // it would be nagging about a closed loop. Pure frontend derivation over
  // data already fetched; no backend change. Shares replacedCampaignIds with
  // flaggedCampaigns above so the two lists can never disagree about which
  // campaigns are still open.
  const needsActionCampaigns = (promotions ?? []).filter(
    (promotion) =>
      promotion.flagged_negative &&
      !replacedCampaignIds.has(promotion.campaign_id),
  )

  // Spec 008 FR-011: aggregate ROI per platform, real attributed campaigns
  // only. Spec 008 FR-012: sort toggle over the same list the chart/table
  // render, so "the list" (spec.md's wording) means one ordering, not two.
  const platformAggregates = useMemo(
    () => aggregateRoiByPlatform(promotions ?? []),
    [promotions],
  )

  // The grid/chart filter (ux-writing + dataviz skills): scopes only the
  // table/chart below, never the header chips or the platform-aggregate
  // stats above, which stay honest totals of every campaign on file
  // regardless of what the owner is currently browsing.
  const campaignFilter = useTableFilter({
    rows: promotions ?? [],
    getSearchableText: (promotion) => [promotion.campaign_id, promotion.platform],
    dimensions: useMemo(
      () => [
        { key: 'platform', getValue: (promotion: PromotionApi) => promotion.platform },
        { key: 'roiSign', getValue: (promotion: PromotionApi) => roiSign(promotion) },
      ],
      [],
    ),
  })

  const displayedPromotions = useMemo(() => {
    if (!promotions) return promotions
    return roiSortDirection
      ? sortPromotionsByRoi(campaignFilter.filteredRows, roiSortDirection)
      : campaignFilter.filteredRows
  }, [promotions, roiSortDirection, campaignFilter.filteredRows])

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
          We couldn&apos;t load your campaigns, so there are no ROI figures to
          show. {error}
        </Panel>
      ) : null}

      {/* Steward-style proactive insight (spec 008 FR-010) — surfaced above
          the chart, without the owner having to ask, so an un-replaced
          losing campaign is never just one row among many. Capped to
          NEEDS_ACTION_VISIBLE_COUNT rows by default (never every campaign
          unconditionally) so a long operating history's worth of flagged
          campaigns doesn't turn one proactive alert into its own scroll. */}
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
            {(needsActionExpanded
              ? needsActionCampaigns
              : needsActionCampaigns.slice(0, NEEDS_ACTION_VISIBLE_COUNT)
            ).map((promotion) => (
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
          {needsActionCampaigns.length > NEEDS_ACTION_VISIBLE_COUNT ? (
            <button
              type="button"
              onClick={() => setNeedsActionExpanded((was) => !was)}
              aria-expanded={needsActionExpanded}
              className="flex w-fit items-center gap-1 text-xs font-medium text-warning-text underline-offset-2 hover:underline focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
            >
              {needsActionExpanded
                ? 'Show fewer'
                : `Show ${needsActionCampaigns.length - NEEDS_ACTION_VISIBLE_COUNT} more`}
              <ChevronDown
                className={cn(
                  'size-3.5 transition-transform',
                  needsActionExpanded && 'rotate-180',
                )}
                aria-hidden="true"
              />
            </button>
          ) : null}
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
          {/* The visible label names the pair of toggles, and now names them
              programmatically too: HomePage's status filter already wraps its
              equivalent chip pair in a labelled `role="group"`, and this one
              predated that convention — two buttons sitting loose next to a
              <span> that nothing associated them with. Labelled by the span
              itself rather than a duplicated `aria-label`, so the two can
              never drift apart. The trailing colon is gone with it (labels
              take no terminal punctuation). */}
          <div
            role="group"
            aria-labelledby={roiSortLabelId}
            className="flex flex-wrap items-center gap-2 border-t border-border pt-4"
          >
            <span
              id={roiSortLabelId}
              className="text-xs font-medium text-muted-foreground"
            >
              Sort by ROI
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

          {/* The grid filter: search by campaign ID, plus platform and
              ROI-sign dimensions — scopes only the chart/table rendered
              below (dataviz skill: one row, above the content it scopes). */}
          <div className="flex flex-wrap items-center gap-3 border-t border-border pt-4">
            <FilterBar
              isFiltered={campaignFilter.isFiltered}
              onClear={campaignFilter.clearFilters}
              resultSummary={
                campaignFilter.isFiltered
                  ? `${campaignFilter.visibleCount} of ${campaignFilter.totalCount} campaigns shown`
                  : undefined
              }
            >
              <FilterSearchInput
                id="promotions-search"
                label="Search campaigns"
                value={campaignFilter.searchQuery}
                onChange={campaignFilter.setSearchQuery}
                placeholder="Search by campaign ID"
              />
              <FilterSelect
                id="promotions-platform-filter"
                label="Filter by platform"
                value={campaignFilter.filterValues.platform ?? null}
                onChange={(value) => campaignFilter.setFilterValue('platform', value)}
                options={campaignFilter.dimensionOptions.platform ?? []}
                allLabel="All platforms"
              />
              <div
                role="group"
                aria-label="Filter by ROI"
                className="flex items-center gap-1.5"
              >
                <FilterChip
                  pressed={campaignFilter.filterValues.roiSign === 'profitable'}
                  onClick={() =>
                    campaignFilter.setFilterValue(
                      'roiSign',
                      campaignFilter.filterValues.roiSign === 'profitable'
                        ? null
                        : 'profitable',
                    )
                  }
                >
                  Profitable
                </FilterChip>
                <FilterChip
                  pressed={campaignFilter.filterValues.roiSign === 'lost'}
                  onClick={() =>
                    campaignFilter.setFilterValue(
                      'roiSign',
                      campaignFilter.filterValues.roiSign === 'lost' ? null : 'lost',
                    )
                  }
                >
                  Lost money
                </FilterChip>
                <FilterChip
                  pressed={campaignFilter.filterValues.roiSign === 'unattributable'}
                  onClick={() =>
                    campaignFilter.setFilterValue(
                      'roiSign',
                      campaignFilter.filterValues.roiSign === 'unattributable'
                        ? null
                        : 'unattributable',
                    )
                  }
                >
                  Unattributable
                </FilterChip>
              </div>
            </FilterBar>
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

      {/* Filtered to nothing: there IS data, the filter just doesn't match
          any of it — distinct from the "no promotions ingested" empty state
          above. */}
      {promotions && promotions.length > 0 && displayedPromotions?.length === 0 ? (
        <Panel>
          <FilterEmptyState
            label="No campaigns match these filters."
            onClear={campaignFilter.clearFilters}
          />
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
