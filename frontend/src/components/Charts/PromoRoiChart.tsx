import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { ShieldAlert, X } from 'lucide-react'

import { buildLinearTickScale, formatAxisCurrency } from '@/lib/chartScale'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { ColumnFilterButton } from '@/components/ui/column-filter'
import { FilterEmptyState } from '@/components/ui/filter-bar'
import ProvenanceTag, {
  type SourceRowRef,
} from '@/components/Provenance/ProvenanceTag'
import { useColumnFilters, type ColumnFilterSpecs } from '@/lib/useColumnFilters'
import PinnedValueAxis from './PinnedValueAxis'

// ---------------------------------------------------------------------------
// Data — the exact 4 campaigns of the dataset's hand-authored opening
// window (see backend/cmd/gendata/opening/README.md "Promotion ROI"
// table). `net: null` for
// IFOOD-CAMP-WEEKEND is the FR-013 refusal case: zero delivery-platform
// orders carry that campaign_id, so incremental revenue — and therefore
// net — cannot be computed, and must never be estimated or shown as $0.
// ---------------------------------------------------------------------------

export interface PromotionRoiDatum {
  campaignId: string
  campaignName: string
  platform: string
  spend: number
  /** Null when attribution is unavailable — the FR-013 refusal case. */
  attributedIncrementalRevenue: number | null
  /** Null exactly when `attributedIncrementalRevenue` is null. */
  net: number | null
  /**
   * Backend reason when `net` is null — the two are different facts, not one
   * conflated "no number" state. "attribution_unavailable" (FR-013): a
   * permanent refusal after trying — zero delivery-platform orders carry
   * this campaign_id, so it never resolves, and this chart draws it as the
   * dashed "Unattributable" bar below. "not_yet_attributed": an
   * owner-created promotion that hasn't been through attribution at all
   * yet — nothing to refuse or plot, so `buildBars` below excludes it from
   * the bars entirely; it still appears in the table further down, since
   * every logged campaign belongs there.
   */
  reason?: string
  sourceRefs: SourceRowRef[]
}

/** See PromotionRoiDatum.reason — the "hasn't run yet" case, never plotted. */
const NOT_YET_ATTRIBUTED = 'not_yet_attributed'

export const DEFAULT_PROMOTION_ROI: PromotionRoiDatum[] = [
  {
    campaignId: 'IFOOD-CAMP-BOOST01',
    campaignName: 'In-App Boost — Weekday Lunch',
    platform: 'iFood',
    spend: 180.0,
    attributedIncrementalRevenue: 214.0,
    net: 34.0,
    sourceRefs: [
      {
        source_file: 'promotion_ad_spend_export.csv',
        row_start: 2,
        row_end: 2,
        period_start: '2026-08-01',
        period_end: '2026-08-07',
      },
      {
        source_file: 'delivery_platform_export.csv',
        row_start: 2,
        row_end: 28,
        period_start: '2026-08-01',
        period_end: '2026-08-07',
      },
    ],
  },
  {
    campaignId: 'JET-CAMP-LUNCHFIX',
    campaignName: 'Banner Ad — Lunch Fix Menu',
    platform: 'Just Eat Takeaway',
    spend: 220.0,
    attributedIncrementalRevenue: 55.0,
    net: -165.0,
    sourceRefs: [
      {
        source_file: 'promotion_ad_spend_export.csv',
        row_start: 3,
        row_end: 3,
        period_start: '2026-08-04',
        period_end: '2026-08-10',
      },
      {
        source_file: 'delivery_platform_export.csv',
        row_start: 18,
        row_end: 26,
        period_start: '2026-08-04',
        period_end: '2026-08-10',
      },
    ],
  },
  {
    campaignId: 'IFOOD-CAMP-WEEKEND',
    campaignName: 'Featured Placement — Weekend Boost',
    platform: 'iFood',
    spend: 95.0,
    attributedIncrementalRevenue: null,
    net: null,
    reason: 'attribution_unavailable',
    // Only the ad-spend export is cited — proving the campaign itself is
    // sourced, but zero delivery_platform_export.csv rows carry this
    // campaign_id, which is why attribution — and net — is refused.
    sourceRefs: [
      {
        source_file: 'promotion_ad_spend_export.csv',
        row_start: 4,
        row_end: 4,
        period_start: '2026-08-08',
        period_end: '2026-08-09',
      },
    ],
  },
  {
    campaignId: 'JET-CAMP-NEWMENU',
    campaignName: 'Sponsored Listing — New Menu Launch',
    platform: 'Just Eat Takeaway',
    spend: 60.0,
    attributedIncrementalRevenue: 79.5,
    net: 19.5,
    sourceRefs: [
      {
        source_file: 'promotion_ad_spend_export.csv',
        row_start: 5,
        row_end: 5,
        period_start: '2026-08-11',
        period_end: '2026-08-14',
      },
      {
        source_file: 'delivery_platform_export.csv',
        row_start: 42,
        row_end: 54,
        period_start: '2026-08-11',
        period_end: '2026-08-14',
      },
    ],
  },
]

// ---------------------------------------------------------------------------
// "View as table, with sources" — column filters (spec 015 extension)
// ---------------------------------------------------------------------------

/** Header labels for the table below, in column order — passed through to
 *  `useColumnFilters` only to keep that call symmetric with `DataGrid`'s own
 *  `columns`/`rows` props; the hook itself looks rows up by index, never by
 *  this array. */
const PROMO_TABLE_COLUMNS = [
  'Campaign',
  'Platform',
  'Spend',
  'Incremental revenue',
  'Net',
  'Sources',
]

/**
 * Platform (a small closed set) gets a checklist; Spend and Net (real
 * dollar ranges) get min/max. Campaign is an id better suited to text
 * search if that's ever added, not this pass; Incremental revenue is left
 * unfiltered because its frequent nulls (any campaign not yet attributed)
 * would read as a second, confusing numeric filter right next to Net's; and
 * Sources renders a `ProvenanceTag`, not filterable data.
 */
const PROMO_TABLE_COLUMN_FILTERS: ColumnFilterSpecs = {
  1: 'categorical', // Platform
  2: 'numeric', // Spend
  4: 'numeric', // Net
}

/**
 * `useColumnFilters`'s `getCell` for this table — reads the RAW number as a
 * string rather than this table's already-formatted display string
 * (`formatUsd`/`formatSignedUsd`), so `parseNumericCell`'s digit/sign/decimal
 * strip never has to contend with a currency symbol, thousands separator, or
 * — for a negative net — the U+2212 minus sign `formatSignedUsd` renders,
 * which is not in `parseNumericCell`'s `[^0-9.-]` allowlist and would
 * otherwise silently strip the sign off a negative net (turning "−$165.00"
 * into a parsed +165). A null net (the FR-013 refusal) returns '' here,
 * which deliberately fails `parseNumericCell` and is excluded from a
 * numeric-filtered result rather than guessed at.
 */
function getPromoTableCell(datum: PromotionRoiDatum, columnIndex: number): string {
  switch (columnIndex) {
    case 1:
      return datum.platform
    case 2:
      return String(datum.spend)
    case 4:
      return datum.net === null ? '' : String(datum.net)
    default:
      return ''
  }
}

export interface PromoRoiChartProps {
  data?: PromotionRoiDatum[]
  className?: string
  /**
   * Start with the underlying table already expanded.
   *
   * The chart plots `net` alone, so spend and attributed revenue — both
   * already in the API response and both needed to judge whether a $34 gain
   * was worth chasing — were reachable only behind a click. On the dedicated
   * `/promotions` route there is room to show them outright; inside a chat
   * answer bubble there is not, so this stays opt-in rather than becoming the
   * default everywhere.
   */
  defaultTableOpen?: boolean
  /**
   * Spec 008 FR-001: called with the campaign of whichever bar the owner
   * clicked or activated via keyboard, so the caller can turn it into a
   * real follow-up question. Omitted entirely (no click affordance beyond
   * the existing hover/focus tooltip) when not provided.
   */
  onDataPointClick?: (point: { campaignId: string; campaignName: string }) => void
  /**
   * Reported live: at the real ~30-campaign scale every campaign was
   * already reaching the chart as a real bar (see `chartableData` below —
   * nothing here has ever sliced or capped `data`), but a plain
   * `overflow-x-auto` viewport defaults to scrolled-to-the-LEFT, i.e. the
   * OLDEST campaigns — with nothing on screen suggesting more content sits
   * off to the right. The owner read that as "not all campaigns are in the
   * chart," which is a fair reading: the newest, most actionable campaigns
   * (the ones a "needs a decision" reader most wants) are also the ones
   * most likely to be scrolled out of view on first load.
   *
   * Default `true`: `data`'s own natural order is chronological, oldest
   * first (see `PromotionsPage`'s `toChartDatum`/API ordering comment) —
   * the same left-to-right time convention `MarginTrendChart` already
   * uses, which is why THAT chart already mounts scrolled to its own right
   * edge ("today first, history a deliberate scroll away"). This applies
   * the identical fix here. The one case that must opt OUT: ROI-sorted
   * order (`PromotionsPage`'s "Highest first"/"Lowest first" toggle) reorders
   * `data` so the campaign the owner explicitly asked to see first is
   * already at index 0 (the left) — auto-scrolling right there would hide
   * exactly what sorting was for. `PromotionsPage` passes `false` while a
   * sort is active for that reason.
   */
  initialScrollToEnd?: boolean
}

// ---------------------------------------------------------------------------
// Geometry
// ---------------------------------------------------------------------------

const CHART_WIDTH = 560
const CHART_HEIGHT = 300
// bottom: 44 -> 56 to fit an explicit "Campaigns" x-axis title beneath the
// campaign-id ticks (mirroring "Net (USD)" on the y-axis) — the reported
// "the x-axis has no meaning" gap was partly that nothing named what the
// bars even ARE, not just that too few of them were labeled.
// `left` is both the plot's own left inset AND the pixel width of the pinned
// value-axis gutter beside it — the two must stay equal so the first gridline
// begins exactly where the gutter ends. 48 -> 56 for the compacted currency
// labels the nice-number axis now emits.
const MARGIN = { top: 44, right: 16, bottom: 56, left: 56 }
const PLOT_HEIGHT = CHART_HEIGHT - MARGIN.top - MARGIN.bottom
/** Roughly the rendered height of the hover tooltip, used to decide whether
 *  it has room to sit above a bar's tip or must flip below it. */
const TOOLTIP_HEIGHT = 76

const BAR_WIDTH = 24
const BAR_RADIUS = 4
const REFUSAL_BOX_WIDTH = 64
const REFUSAL_BOX_HEIGHT = 28

// ---------------------------------------------------------------------------
// Scale — this chart was built and tested against a 4-campaign sample,
// where a fixed 560px canvas and a label on every bar both made sense. The
// real dataset carries 25-30+ campaigns: `PLOT_WIDTH / data.length` with a
// fixed 24px BAR_WIDTH means bars start overlapping their neighbors past
// ~20 campaigns, and a value label plus an x-axis campaign-id label on
// EVERY bar collapses into an illegible smear at that count (this is what
// the owner saw and reported as "the chart is on the left" — the bars
// overlap into one dense, unreadable mass on the left/center of a canvas
// that never grows to fit them, while the fixed-560px SVG sits flush-left
// in a much wider panel with dead space beside it).
//
// Same two-part fix as MarginTrendChart, scoped to fire only once there is
// real data to warrant it — at <= LABEL_ALL_MAX campaigns (the 4-campaign
// sample included) this renders pixel-identical to before:
//
//  1. The canvas grows with campaign count (MIN_SLOT_WIDTH per bar) instead
//     of staying pinned to 560px, so bars get real room and the existing
//     `overflow-x-auto` wrapper turns a wide chart into a horizontal scroll
//     rather than a squeeze.
//  2. Past LABEL_ALL_MAX campaigns, only the two extreme bars (best/worst
//     ROI) get a direct value label, and the x-axis stops printing every
//     campaign id underneath its bar — the same "label the extreme, not
//     every point" rule MarginTrendChart already applies. Every campaign's
//     identity stays reachable via hover tooltip, the legend-adjacent
//     provenance list, and the full table below; nothing is hidden, only
//     decluttered off the chart face itself.
// ---------------------------------------------------------------------------

const MIN_SLOT_WIDTH = 28 // BAR_WIDTH + a visible gutter between neighbors
const LABEL_ALL_MAX = 8
// Reported live at 29 real campaigns: REFUSAL_BOX_WIDTH (64px) is more than
// two slot-widths at MIN_SLOT_WIDTH (28px), so the "Unattributable" marker
// spilled into its neighbors' slots on both sides — the dashed box and its
// text label visually crowded whatever bar sat next to a refused campaign.
// Same fix as the value/campaign-id labels below: full box + text label only
// while there's room (<= LABEL_ALL_MAX, where slots are wide); past that, a
// small icon-only marker sized to fit its own slot — full detail stays one
// hover away via the existing tooltip and aria-label, never lost.
const COMPACT_REFUSAL_BOX_SIZE = 20
// Evenly-spaced axis ticks past LABEL_ALL_MAX, same discipline
// MarginTrendChart already applies to its own x-axis at scale (see that
// file's `tickLabelStep`) — a fixed cap on how many campaign-id ticks ever
// render, rather than "only the 2 ROI extremes," so the x-axis still reads
// as a real axis instead of a nearly-blank one past a handful of bars.
// Lower than MarginTrendChart's 14: campaign ids ("IFOOD-CAMP-001") run
// much longer than that chart's day-of-month digits, so fewer, better-
// spaced ticks read cleanly where more, tighter ones would collide.
const MAX_AXIS_TICKS = 8
// Reported live at 29 real campaigns: each bar's invisible hover/click
// hit-target was BAR_WIDTH (24) + 14px of padding on EACH side (52px
// total) — a generous, easy-to-click affordance that made sense at the
// original 4-campaign sample's wide slots (124px+), but at MIN_SLOT_WIDTH
// (28px) that 52px-wide hit-rect overlaps BOTH neighbors' hit-rects by a
// large margin, so hovering near a slot boundary could trigger the WRONG
// bar's tooltip ("getting data from the left bar"). HIT_RECT_MAX_PADDING
// is now a ceiling, not a fixed value — the real padding used per-render
// is clamped so adjacent hit-rects never overlap (see hitRectPadding
// below), while still using the full 14px at low campaign counts where
// slots are wide enough to afford it.
const HIT_RECT_MAX_PADDING = 14
const HIT_RECT_MIN_PADDING = 2

// Tolerates the sub-pixel rounding a browser's own scroll math can leave
// behind (scrollLeft can land at e.g. 0.4 or scrollWidth-0.6 on a display
// with a fractional device pixel ratio) — without it, "is there real
// unscrolled content past this edge" could stay permanently true (or
// false) by less than a pixel. Same constant, same rationale, as
// Shell/Sidebar.tsx's MobileNavBar fade — the same affordance, applied here.
const SCROLL_EDGE_SLACK_PX = 1

// Reported live at 29 real campaigns, scrolled all the way to the chart's
// own right edge: the last axis tick ("JET-CAMP-NEWMENU") rendered as
// "JET-CAMP-NEWM" — not a scroll problem (the bar itself was already fully
// scrollable into view, see the wrapper's overflow-x-auto), but the tick
// TEXT itself, centered (`textAnchor="middle"`) on a slotCenterX that sits
// only MARGIN.right (16px) from the chart's own edge, with nowhere close to
// half a real campaign id's rendered width (a 16-19 char id like
// "IFOOD-CAMP-BOOST01" runs ~100px+ at this 9.5px font) to grow into on
// that side. The SVG root's own default `overflow: hidden` then clips
// mid-word — invisible in the small-sample/short-synthetic-id tests that
// exercised this campaign COUNT but never an id long enough to hit an edge.
// Any label whose center falls within this many px of the plot's left/right
// boundary anchors from that boundary inward instead of centering outward
// past it (the same "first/last tick anchors from the edge" rule most
// charting libraries apply) — see edgeAwareTextAnchor below.
const AXIS_LABEL_EDGE_GUARD = 58

/**
 * A `textAnchor="middle"` label centered on `x` draws equally in both
 * directions — fine mid-chart, but a label whose center sits within
 * AXIS_LABEL_EDGE_GUARD of the plot's own left/right boundary has nowhere
 * to grow on the boundary side, and the SVG clips whatever spills past it
 * (see AXIS_LABEL_EDGE_GUARD's comment for the real campaign-id this was
 * reported against). Anchoring from the boundary itself, inward, keeps the
 * full label on-canvas regardless of how long the text turns out to be.
 */
function edgeAwareTextAnchor(
  x: number,
  chartWidth: number,
): { x: number; textAnchor: 'start' | 'middle' | 'end' } {
  const leftEdge = MARGIN.left
  const rightEdge = chartWidth - MARGIN.right
  if (x - leftEdge < AXIS_LABEL_EDGE_GUARD) {
    return { x: leftEdge, textAnchor: 'start' }
  }
  if (rightEdge - x < AXIS_LABEL_EDGE_GUARD) {
    return { x: rightEdge, textAnchor: 'end' }
  }
  return { x, textAnchor: 'middle' }
}

// Px-per-character at the 9.5px tick font — no real text metrics are
// available here (no canvas measureText, no DOM ref timing that would
// survive SSR-free but still synchronous render), so a constant stands in.
//
// 5.6 was a guess, and it under-measured: the last two campaign-id ticks
// still touched. Measured against the rendered SVG (`getBBox().width` on
// every live campaign-id tick, 2026-08-30): 6.08-6.24 px/char, mean 6.16.
// 6.3 rounds UP on purpose — over-estimating a label's width drops one tick
// too many, which reads fine; under-estimating collides two labels, which
// is the bug this exists to prevent.
const AXIS_LABEL_CHAR_WIDTH_PX = 6.3
// Minimum breathing room between two adjacent tick labels' estimated edges.
const AXIS_LABEL_GAP_PX = 6

function estimatedLabelWidth(text: string): number {
  return text.length * AXIS_LABEL_CHAR_WIDTH_PX
}

/**
 * Reported live immediately after edgeAwareTextAnchor shipped: pinning the
 * LAST tick's full text on-canvas fixed the clipped-word bug, but that same
 * full-length label ("JET-CAMP-NEWMENU") now visually overlapped the tick
 * immediately before it ("IFOOD-CAMP-025") — tickLabelStep spaces ticks
 * evenly assuming short labels fit the gap, which held for the synthetic
 * "CAMP-0".."CAMP-28" test ids but not a real ~17-19 char campaign id.
 * Rather than shrink MAX_AXIS_TICKS globally (most gaps have plenty of
 * room), this walks the candidate ticks RIGHT TO LEFT — so the rightmost
 * one (the newest campaign, and the one this bug was specifically reported
 * against) always wins a collision — dropping any earlier candidate whose
 * estimated extent would overlap the nearest tick already kept to its
 * right. The same greedy label-collision-avoidance most charting libraries
 * apply, just without measured text metrics to drive it.
 */
function selectVisibleTickIndices(
  tickCandidates: BarGeometry[],
  chartWidth: number,
): Set<number> {
  const visible = new Set<number>()
  let leftEdgeOfNearestKeptLabel = Infinity
  for (let i = tickCandidates.length - 1; i >= 0; i--) {
    const bar = tickCandidates[i]
    const { x, textAnchor } = edgeAwareTextAnchor(bar.slotCenterX, chartWidth)
    const width = estimatedLabelWidth(bar.datum.campaignId)
    const left =
      textAnchor === 'start' ? x : textAnchor === 'end' ? x - width : x - width / 2
    const right = left + width
    if (right > leftEdgeOfNearestKeptLabel - AXIS_LABEL_GAP_PX) continue
    visible.add(bar.index)
    leftEdgeOfNearestKeptLabel = left
  }
  return visible
}

/**
 * Y scale derived from the data, not hard-coded. The previous fixed
 * [-200, 60] domain was tuned to one sample; against live /api/promotions
 * rows a campaign outside it would be CLAMPED to the axis edge and drawn
 * smaller than the loss it represents.
 *
 * The step now comes from `buildLinearTickScale`'s nice-number algorithm
 * rather than the 50/200/500 ladder this used to carry — see
 * `lib/chartScale.ts`, where that maths is unit-tested directly across every
 * span the live data reaches. Exported so the geometry is testable without
 * rendering an SVG.
 */
export function buildScale(data: PromotionRoiDatum[]) {
  const values = data
    .map((datum) => datum.net)
    .filter((value): value is number => value !== null)
  // Always zero-baselined: a net-ROI bar only means anything read against
  // zero, and profit/loss is which side of zero the bar sits on.
  const { min, max, step, ticks } = buildLinearTickScale(
    Math.min(0, ...values),
    Math.max(0, ...values),
  )

  const yToPixel = (value: number) => {
    const clamped = Math.min(Math.max(value, min), max)
    return MARGIN.top + ((max - clamped) / (max - min || 1)) * PLOT_HEIGHT
  }
  return { ticks, step, yToPixel, baselineY: yToPixel(0) }
}

function roundedBarPath(
  x: number,
  y: number,
  width: number,
  height: number,
  roundedEdge: 'top' | 'bottom',
): string {
  if (height <= 0) return ''
  const r = Math.min(BAR_RADIUS, height, width / 2)
  if (roundedEdge === 'top') {
    return `M${x},${y + height} L${x},${y + r} Q${x},${y} ${x + r},${y} L${x + width - r},${y} Q${x + width},${y} ${x + width},${y + r} L${x + width},${y + height} Z`
  }
  return `M${x},${y} L${x + width},${y} L${x + width},${y + height - r} Q${x + width},${y + height} ${x + width - r},${y + height} L${x + r},${y + height} Q${x},${y + height} ${x},${y + height - r} Z`
}

function formatUsd(value: number): string {
  return value.toLocaleString('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
}

function formatSignedUsd(value: number): string {
  const magnitude = formatUsd(Math.abs(value))
  return value < 0 ? `−${magnitude}` : `+${magnitude}`
}

interface BarGeometry {
  datum: PromotionRoiDatum
  index: number
  slotCenterX: number
  barX: number
  isRefused: boolean
  isPositive: boolean
  barY: number
  barHeight: number
}

/**
 * Per-reason copy for a refused (`net === null`) bar's focus label and hover
 * tooltip. The two `PromotionRoiDatum.reason` values are different facts
 * (see that field's doc comment) — `attribution_unavailable` is a permanent,
 * already-tried-and-failed refusal (FR-013), `not_yet_attributed` just
 * hasn't been checked yet — so the accessible/hover text says which one
 * this is rather than wording every refused bar as an active refusal.
 */
function refusalCopy(reason: string | undefined): { focus: string; tooltip: string } {
  if (reason === NOT_YET_ATTRIBUTED) {
    return {
      focus: 'not yet attributed — awaiting incremental-order data',
      tooltip: 'Not yet attributed — awaiting incremental-order data',
    }
  }
  return {
    focus: 'unattributable, ROI refused — no incremental orders on file',
    tooltip: 'Unattributable — refusing to estimate (FR-013)',
  }
}

function buildBars(
  data: PromotionRoiDatum[],
  yToPixel: (value: number) => number,
  plotWidth: number,
): BarGeometry[] {
  const slotWidth = plotWidth / data.length
  return data.map((datum, index) => {
    const slotCenterX = MARGIN.left + slotWidth * (index + 0.5)
    const barX = slotCenterX - BAR_WIDTH / 2
    const isRefused = datum.net === null
    const isPositive = (datum.net ?? 0) >= 0
    const barTopY = isRefused ? 0 : yToPixel(Math.max(datum.net as number, 0))
    const barBottomY = isRefused
      ? 0
      : yToPixel(Math.min(datum.net as number, 0))
    return {
      datum,
      index,
      slotCenterX,
      barX,
      isRefused,
      isPositive,
      barY: barTopY,
      barHeight: barBottomY - barTopY,
    }
  })
}

/**
 * Diverging bar chart of promotion net ROI (spend vs. attributed incremental
 * revenue), zero-baselined. `IFOOD-CAMP-WEEKEND` renders as an explicit
 * "Unattributable" refusal state (FR-013) rather than a bar of any height —
 * the same visual language `ChatPanel`'s `RefusalBubble` uses for a chat
 * refusal, because this is an active policy refusal to estimate, not a
 * missing-input gap (contrast `MarginTrendChart`'s hatched "No data" state).
 */
function PromoRoiChart({
  data = DEFAULT_PROMOTION_ROI,
  className,
  defaultTableOpen = false,
  onDataPointClick,
  initialScrollToEnd = true,
}: PromoRoiChartProps) {
  const [hoveredIndex, setHoveredIndex] = useState<number | null>(null)
  const [tableOpen, setTableOpen] = useState(defaultTableOpen)

  // Column filters for the "view as table, with sources" fallback below —
  // scoped to this chart's own embedded table only. Deliberately independent
  // of PromotionsPage's page-level campaign-card filter bar (`useTableFilter`)
  // that narrows `data` before it ever reaches this component: that surface
  // narrows the campaign list rendered as cards above the chart, this one
  // narrows only the rows of this chart's own table, per spec 015's
  // per-chart scoping. Kept as the chart's own rich `PromotionRoiDatum[]`
  // (via `getCell`) rather than routed through `DataGrid`, so Net's
  // conditional coloring/refusal text and the `ProvenanceTag` in Sources
  // never get flattened to plain strings.
  const columnFilterState = useColumnFilters<PromotionRoiDatum>({
    columns: PROMO_TABLE_COLUMNS,
    rows: data,
    specs: PROMO_TABLE_COLUMN_FILTERS,
    getCell: getPromoTableCell,
  })

  // Reported live: with few enough campaigns that the data-driven width
  // stays under the panel's real available width, the chart rendered at
  // its minimum computed size and left visible dead space to the right
  // inside its own card, instead of using the space it actually has.
  // Measured via ResizeObserver on the scroll-viewport div below (the one
  // sized `w-full` by its parent card) rather than hard-coding a guess —
  // this project's cards aren't a fixed width (sidebar/viewport-dependent),
  // so only a real measurement is honest. Null until the first observation
  // fires post-mount; chartWidth falls back to the data/CHART_WIDTH floor
  // for that one initial render, matching this chart's behavior before this
  // fix existed.
  const containerRef = useRef<HTMLDivElement>(null)
  const [containerWidth, setContainerWidth] = useState<number | null>(null)

  useEffect(() => {
    const el = containerRef.current
    // jsdom (this project's test environment) has no ResizeObserver at
    // all — same guard ChatPanel.tsx already uses for its own observers,
    // rather than every test file needing to stub one just to mount this
    // chart. Real browsers all have it; this only ever short-circuits
    // under test, where chartWidth simply stays at its data/floor value.
    if (!el || typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver((entries) => {
      const width = entries[0]?.contentRect.width
      if (width) setContainerWidth(width)
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [])

  // Scroll-fade affordance state — see the effect below `chartWidth`'s own
  // computation, which is what this actually needs to react to.
  const [canScrollLeft, setCanScrollLeft] = useState(false)
  const [canScrollRight, setCanScrollRight] = useState(false)

  // Reported live (QA pass): a `not_yet_attributed` campaign used to be
  // filtered out of the chart entirely here — "nothing to plot yet" — which
  // meant the chart's own bar count and accessible description silently
  // undercounted the table below by one, and the campaign got no visual
  // marker at all despite the table (and its "N unattributable" header chip)
  // counting it. Both refusal reasons share `net: null`, so both now flow
  // through unfiltered and render as the same dashed "Unattributable"
  // no-bar marker `isRefused` already draws below — matching this chart's
  // own legend, which already describes that marker as a single
  // "Unattributable — refused" bucket rather than two. The per-bar focus
  // label and hover tooltip still branch on `reason` (see `refusalCopy`
  // below) so a campaign that simply hasn't been attributed YET is never
  // worded as an active refusal.
  const chartableData = data
  const dataWidth =
    MARGIN.left + MARGIN.right + chartableData.length * MIN_SLOT_WIDTH
  // The larger of: the base floor, what the data actually needs, and the
  // real container width. When data needs more room than the container has
  // (many campaigns), this stays exactly the old data-driven value and the
  // wrapper below scrolls horizontally, unchanged. When the container has
  // MORE room than the data needs (few campaigns, a wide panel), this now
  // grows to fill it instead of leaving dead space — bars spread out to
  // use the real width, via `plotWidth`/`slotWidth` below, which are both
  // already derived from `chartWidth`.
  const chartWidth = Math.max(CHART_WIDTH, dataWidth, containerWidth ?? 0)
  const plotWidth = chartWidth - MARGIN.left - MARGIN.right
  const labelEveryBar = chartableData.length <= LABEL_ALL_MAX
  // Same formula as MarginTrendChart's tickLabelStep: 1-for-1 at small
  // counts, otherwise evenly spaced so at most MAX_AXIS_TICKS labels ever
  // render regardless of how many campaigns there are.
  const tickLabelStep = labelEveryBar
    ? 1
    : Math.max(1, Math.ceil(chartableData.length / MAX_AXIS_TICKS))
  const slotWidth = plotWidth / chartableData.length
  // Never wider than half the room actually left in a slot after the bar
  // itself, so adjacent bars' hit-rects can't overlap — clamped between
  // HIT_RECT_MIN_PADDING (always a little easier to hit than the bar's
  // bare pixels) and HIT_RECT_MAX_PADDING (the original generous value,
  // still used whenever slots are wide enough to afford it).
  const hitRectPadding = Math.min(
    HIT_RECT_MAX_PADDING,
    Math.max(HIT_RECT_MIN_PADDING, (slotWidth - BAR_WIDTH) / 2),
  )
  const refusalBoxWidth = labelEveryBar
    ? REFUSAL_BOX_WIDTH
    : Math.min(REFUSAL_BOX_WIDTH, Math.max(COMPACT_REFUSAL_BOX_SIZE, slotWidth - 4))
  const refusalBoxHeight = labelEveryBar ? REFUSAL_BOX_HEIGHT : COMPACT_REFUSAL_BOX_SIZE
  const { ticks, step, yToPixel, baselineY } = buildScale(chartableData)
  const bars = buildBars(chartableData, yToPixel, plotWidth)
  const hovered = hoveredIndex === null ? null : bars[hoveredIndex]
  // The tip of the hovered mark, in viewBox units: where the tooltip points.
  const tooltipAnchorX = hovered?.slotCenterX ?? 0
  const tooltipAnchorY = hovered
    ? Math.min(
        hovered.isRefused ? baselineY - refusalBoxHeight / 2 : hovered.barY,
        baselineY,
      ) - 4
    : 0
  const tooltipBelow = tooltipAnchorY < TOOLTIP_HEIGHT
  // The tickLabelStep-selected candidates, further pruned for estimated
  // overlap — see selectVisibleTickIndices's doc comment. Labeling every
  // bar (<= LABEL_ALL_MAX) skips this: tight, uniform slot widths there are
  // already proven to fit (the 4-campaign sample), and running collision
  // pruning over real campaign ids at that width could start dropping
  // labels that were never actually reported as overlapping.
  const visibleTickIndices = labelEveryBar
    ? null
    : selectVisibleTickIndices(
        bars.filter((bar) => bar.index % tickLabelStep === 0),
        chartWidth,
      )

  const netValues = chartableData
    .map((d) => d.net)
    .filter((v): v is number => v !== null)
  const maxNet = netValues.length > 0 ? Math.max(...netValues) : null
  const minNet = netValues.length > 0 ? Math.min(...netValues) : null
  const maxIndex = chartableData.findIndex((d) => d.net === maxNet)
  const minIndex = chartableData.findIndex((d) => d.net === minNet)
  const refusedCount = bars.filter((bar) => bar.isRefused).length

  // Mount (and every genuinely new order) scrolled to the RIGHT edge — the
  // newest campaigns — rather than the oldest history a plain
  // `overflow-x-auto` container defaults to. Exactly `MarginTrendChart`'s
  // own fix for the identical problem (see that chart's matching
  // `useLayoutEffect` and its "today first, history a deliberate scroll
  // away" comment); `initialScrollToEnd`'s own doc comment above explains
  // the one case (`PromotionsPage`'s ROI sort) that opts out. `useLayoutEffect`,
  // not `useEffect`, so this runs before paint — no visible flash of the
  // oldest campaigns before jumping to the newest. Keyed on the first/last
  // campaign actually plotted (not `data`'s own array identity, a fresh
  // reference on every parent re-render even for the SAME campaigns) so
  // re-scrolling fires only when the plotted order genuinely changes —
  // never on an unrelated re-render that would yank a reader back to the
  // right after they scrolled left on purpose to review history.
  const firstPlottedId = chartableData[0]?.campaignId
  const lastPlottedId = chartableData[chartableData.length - 1]?.campaignId
  useLayoutEffect(() => {
    if (!initialScrollToEnd) return
    const container = containerRef.current
    if (!container) return
    container.scrollLeft = container.scrollWidth
  }, [initialScrollToEnd, firstPlottedId, lastPlottedId, chartableData.length, chartWidth])

  // Scroll-fade affordance: whether there is real unscrolled content past
  // either edge of the viewport RIGHT NOW. Same pattern Shell/Sidebar.tsx's
  // MobileNavBar already established for an identical problem (an
  // `overflow-x-auto` row that gives no visual reason to suspect there's
  // more) — this codebase's one shared answer, applied here rather than a
  // second bespoke one. Runs after the scroll-to-end layout effect above
  // (layout effects flush before effects), so the very first read already
  // reflects where that effect left `scrollLeft`. Recomputed on every
  // scroll (dragging the chart) and whenever `chartWidth` changes (new
  // data, or a container resize) — `chartWidth` is what actually drives
  // `scrollWidth`, so it's the one dependency that matters here.
  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    const updateScrollFade = () => {
      setCanScrollLeft(el.scrollLeft > SCROLL_EDGE_SLACK_PX)
      setCanScrollRight(
        el.scrollLeft + el.clientWidth < el.scrollWidth - SCROLL_EDGE_SLACK_PX,
      )
    }
    updateScrollFade()
    el.addEventListener('scroll', updateScrollFade, { passive: true })
    return () => el.removeEventListener('scroll', updateScrollFade)
  }, [chartWidth])

  return (
    <figure
      aria-label="Promotion ROI"
      // See MarginTrendChart: min-w-0 keeps a definite-width plot from
      // widening an `auto` grid track and scrolling the whole page.
      className={cn(
        'min-w-0 rounded-lg border border-border bg-card p-4 sm:p-5',
        className,
      )}
    >
      <figcaption className="mb-4">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Promotions
        </p>
        <h2 className="text-lg font-semibold tracking-tight text-foreground">
          Promotion ROI
        </h2>
      </figcaption>

      {/* Two nested boxes, not one: the outer is the scroll VIEWPORT
          (always the panel's own width), the inner is the scrollable
          CONTENT sized to match the svg's own real width exactly. The
          refusal-marker and hover-tooltip overlays below are positioned by
          percentage against their nearest `relative` ancestor — if that
          ancestor were the outer viewport (as it was before this fix),
          those percentages would resolve against the wrong, narrower box
          the moment a bar chart with 20+ campaigns grows past CHART_WIDTH
          and starts scrolling, so every overlay would render squeezed
          into the visible viewport instead of tracking its actual bar. */}
      {/* Relative wrapper solely so the two scroll-fade overlays below can
          be positioned against the viewport's own edges regardless of
          scroll offset — they are siblings of the scrolling div, not
          children of it, so they never scroll away with the content they're
          hinting at. */}
      <div className="relative">
      <div
        ref={containerRef}
        className="flex w-full overflow-x-auto overscroll-x-contain"
      >
        <PinnedValueAxis
          ticks={ticks}
          step={step}
          yToPixel={yToPixel}
          chartHeight={CHART_HEIGHT}
          width={MARGIN.left}
          title="Net (USD)"
          formatTick={formatAxisCurrency}
        />
        <div
          className="relative shrink-0"
          style={{ width: plotWidth + MARGIN.right }}
        >
        <svg
          // Cropped to start where the frozen axis gutter ends, so every
          // coordinate below stays in the original whole-chart space.
          viewBox={`${MARGIN.left} 0 ${plotWidth + MARGIN.right} ${CHART_HEIGHT}`}
          // See CategoryBarChart: role="img" cannot contain the focusable
          // per-campaign targets below (axe nested-interactive).
          role="group"
          aria-label={`Bar chart of net ROI across ${chartableData.length} promotion campaign${
            chartableData.length === 1 ? '' : 's'
          }${refusedCount > 0 ? `, with ${refusedCount} flagged as unattributable and refused` : ''}`}
          // Rendered 1:1 in real pixels rather than scaled to fit: that is
          // what keeps the frozen axis's labels aligned with their own
          // gridlines at any panel width, and keeps 10px type at 10px.
          width={plotWidth + MARGIN.right}
          height={CHART_HEIGHT}
          className="block"
        >
          {/* Y gridlines — recessive solid hairlines, one step off the
              surface. Their labels live in the frozen axis beside this SVG. */}
          {ticks.map((tick) => (
            <line
              key={tick}
              x1={MARGIN.left}
              x2={chartWidth - MARGIN.right}
              y1={yToPixel(tick)}
              y2={yToPixel(tick)}
              stroke="var(--border)"
              strokeWidth={1}
              opacity={tick === 0 ? 0 : 0.6}
            />
          ))}

          <line
            x1={MARGIN.left}
            x2={chartWidth - MARGIN.right}
            y1={baselineY}
            y2={baselineY}
            stroke="var(--border)"
            strokeWidth={1}
          />

          {bars.map((bar) => {
            const { datum, index, slotCenterX, barX, isRefused, isPositive } =
              bar
            const isExtreme = index === maxIndex || index === minIndex
            const showValueLabel = !isRefused && (labelEveryBar || isExtreme)
            const focusLabel = isRefused
              ? `${datum.campaignName}: ${refusalCopy(datum.reason).focus}`
              : `${datum.campaignName}: net ${formatSignedUsd(datum.net as number)}`

            const handleActivate = onDataPointClick
              ? () =>
                  onDataPointClick({
                    campaignId: datum.campaignId,
                    campaignName: datum.campaignName,
                  })
              : undefined

            return (
              <g
                key={datum.campaignId}
                tabIndex={0}
                role="button"
                aria-label={
                  handleActivate ? `${focusLabel}. Ask about this.` : focusLabel
                }
                onMouseEnter={() => setHoveredIndex(index)}
                onMouseLeave={() => setHoveredIndex(null)}
                onFocus={() => setHoveredIndex(index)}
                onBlur={() => setHoveredIndex(null)}
                onClick={handleActivate}
                onKeyDown={
                  handleActivate
                    ? (event) => {
                        if (event.key === 'Enter' || event.key === ' ') {
                          event.preventDefault()
                          handleActivate()
                        }
                      }
                    : undefined
                }
                className="cursor-pointer [outline:none] [&:focus-visible]:[outline:2px_solid_var(--ring)] [&:focus-visible]:[outline-offset:2px]"
              >
                <rect
                  x={barX - hitRectPadding}
                  y={MARGIN.top}
                  width={BAR_WIDTH + hitRectPadding * 2}
                  height={PLOT_HEIGHT}
                  fill="transparent"
                />

                {!isRefused ? (
                  <path
                    d={roundedBarPath(
                      barX,
                      bar.barY,
                      BAR_WIDTH,
                      bar.barHeight,
                      isPositive ? 'top' : 'bottom',
                    )}
                    fill={isPositive ? 'var(--success)' : 'var(--destructive)'}
                    opacity={hoveredIndex === index ? 1 : 0.92}
                  />
                ) : null}

                {/* Direct label mandatory on every bar at <= LABEL_ALL_MAX
                    categories (the 4-campaign sample qualifies); past that,
                    only the two extremes get one — see the Scale comment
                    above for why a label on all 25-30+ bars stopped being
                    readable. */}
                {showValueLabel ? (
                  <text
                    {...edgeAwareTextAnchor(slotCenterX, chartWidth)}
                    y={
                      isPositive
                        ? bar.barY - 8
                        : bar.barY + bar.barHeight + 14
                    }
                    className={cn(
                      'text-micro font-semibold tabular-nums',
                      isPositive ? 'fill-success-text' : 'fill-destructive-text',
                    )}
                  >
                    {formatSignedUsd(datum.net as number)}
                  </text>
                ) : null}

                {/* Per-bar campaign-id tick. Every bar below LABEL_ALL_MAX;
                    past that, evenly spaced at tickLabelStep (same
                    discipline as MarginTrendChart's x-axis) rather than
                    only the 2 ROI extremes, so the axis still reads as an
                    axis instead of going nearly blank at real scale. Every
                    campaign stays identifiable via the hover tooltip, the
                    table below, and its Sources column regardless. */}
                {(visibleTickIndices
                  ? visibleTickIndices.has(index)
                  : index % tickLabelStep === 0) ? (
                  <text
                    {...edgeAwareTextAnchor(slotCenterX, chartWidth)}
                    y={CHART_HEIGHT - MARGIN.bottom + 16}
                    className="fill-muted-foreground text-[9.5px] font-medium"
                  >
                    {datum.campaignId}
                  </text>
                ) : null}
              </g>
            )
          })}

          {/* X-axis title — the counterpart to the frozen "Net (USD)" label
              on the value axis, naming what the bars ARE (one per campaign)
              rather than leaving the axis to be inferred from whichever ids
              happen to be labeled. */}
          <text
            x={(MARGIN.left + chartWidth - MARGIN.right) / 2}
            y={CHART_HEIGHT - 8}
            textAnchor="middle"
            className="fill-muted-foreground text-[10px]"
          >
            Campaigns
          </text>
        </svg>

        {/* Refusal state — dashed outline + icon (+ text label while there's
            room), never a bar of any height, matching RefusalBubble's
            visual language for the same "we won't estimate" policy (see
            FR-013). REFUSAL_BOX_WIDTH (64px) is wider than a real dataset's
            slot width (28px at MIN_SLOT_WIDTH) — reported live as the
            marker spilling into neighboring campaigns' slots on both sides.
            Past LABEL_ALL_MAX this renders at refusalBoxWidth/Height
            instead (capped to fit its own slot) with the text label
            dropped — the marker's pointer-events-none, decorative role is
            unchanged, so hiding this text costs no accessible name: the
            bar's own aria-label and the hover tooltip both already state
            "unattributable, ROI refused" in full. */}
        {bars
          .filter((bar) => bar.isRefused)
          .map((bar) => (
            <div
              key={bar.datum.campaignId}
              className="pointer-events-none absolute flex -translate-x-1/2 -translate-y-1/2 flex-col items-center gap-1"
              // Real pixels against the 1:1 plot layer. These used to be
              // percentages of `chartWidth` resolved against the scroll
              // VIEWPORT, so on a scrolling chart every marker drifted off
              // the campaign it belongs to.
              style={{
                left: bar.slotCenterX - MARGIN.left,
                top: baselineY,
                width: refusalBoxWidth,
              }}
            >
              <div
                className="flex items-center justify-center rounded-md border border-dashed border-destructive/40 bg-destructive/5"
                style={{ height: refusalBoxHeight, width: '100%' }}
              >
                <ShieldAlert
                  className={labelEveryBar ? 'size-3.5 text-destructive-text' : 'size-3 text-destructive-text'}
                  aria-hidden="true"
                />
              </div>
              {labelEveryBar ? (
                <span className="text-[10px] font-medium text-destructive-text">
                  Unattributable
                </span>
              ) : null}
            </div>
          ))}

        {hovered ? (
          <div
            role="status"
            // Real pixels against the 1:1 plot layer, and flipped below the
            // tip when a tall bar leaves no room above it — percentages here
            // resolved against the scroll viewport, not the scrolled content.
            className={cn(
              'pointer-events-none absolute z-20 w-max -translate-x-1/2 rounded-md border border-border bg-popover px-2.5 py-1.5 text-xs shadow-md',
              tooltipBelow ? 'translate-y-2' : '-translate-y-full',
            )}
            style={{ left: tooltipAnchorX - MARGIN.left, top: tooltipAnchorY }}
          >
            <p className="font-medium text-muted-foreground">
              {hovered.datum.campaignName}
            </p>
            <p className="text-muted-foreground">
              {hovered.datum.platform} · spend {formatUsd(hovered.datum.spend)}
            </p>
            {hovered.isRefused ? (
              <p className="font-semibold text-destructive-text">
                {refusalCopy(hovered.datum.reason).tooltip}
              </p>
            ) : (
              <p
                className={cn(
                  'text-sm font-semibold tabular-nums',
                  hovered.isPositive ? 'text-success-text' : 'text-destructive-text',
                )}
              >
                net {formatSignedUsd(hovered.datum.net as number)}
              </p>
            )}
            <p className="text-muted-foreground">
              {hovered.datum.sourceRefs[0]?.source_file ?? 'no source on file'}
            </p>
          </div>
        ) : null}
        </div>
      </div>

      {/* Scroll-fade affordance (Shell/Sidebar.tsx's MobileNavBar pattern):
          shown only while there is real unscrolled content past that edge,
          never a permanent decoration that would misleadingly persist once
          the reader has actually scrolled all the way there. The left fade
          starts at MARGIN.left, right where the pinned value-axis gutter
          ends — it must never bleed over the axis's own opaque numbers. */}
      {canScrollLeft ? (
        <div
          aria-hidden="true"
          data-testid="promo-roi-chart-scroll-fade-left"
          className="pointer-events-none absolute inset-y-0 w-8 bg-gradient-to-r from-card to-transparent"
          style={{ left: MARGIN.left }}
        />
      ) : null}
      {canScrollRight ? (
        <div
          aria-hidden="true"
          data-testid="promo-roi-chart-scroll-fade-right"
          className="pointer-events-none absolute inset-y-0 right-0 w-8 bg-gradient-to-l from-card to-transparent"
        />
      ) : null}
      </div>

      {/* Legend — mandatory secondary encoding, same CVD reasoning as
          MarginTrendChart. Three states, each icon/shape + label. */}
      <ul
        aria-label="Chart legend"
        className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1.5"
      >
        <li className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <span className="size-2.5 rounded-sm bg-success" aria-hidden="true" />
          Positive ROI
        </li>
        <li className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <span className="size-2.5 rounded-sm bg-destructive" aria-hidden="true" />
          Negative ROI
        </li>
        <li className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <span
            className="size-2.5 rounded-sm border border-dashed border-destructive/50"
            aria-hidden="true"
          />
          Unattributable — refused
        </li>
      </ul>

      {/* Per-campaign provenance used to render here unconditionally, one
          "CampaignID: N sources" tag per campaign — fine at the original
          4-campaign sample, unreadable at 25-30 real campaigns (reported
          live). Provenance is Constitution Principle IV, non-optional, but
          "always visible" and "readable at real scale" turned out to
          conflict — folded into the already-collapsed table below (a
          Sources column) instead of a second, always-open rendering of the
          same per-campaign list. */}
      <div className="mt-3 flex items-center justify-end border-t border-border/60 pt-2.5">
        <button
          type="button"
          onClick={() => setTableOpen((wasOpen) => !wasOpen)}
          aria-expanded={tableOpen}
          className="shrink-0 text-xs text-muted-foreground underline decoration-dotted underline-offset-2 hover:text-foreground focus-visible:text-foreground focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:rounded-sm"
        >
          {tableOpen ? 'Hide table' : 'View as table, with sources'}
        </button>
      </div>

      {tableOpen ? (
        <div className="mt-2">
          {columnFilterState.isFiltered ? (
            <div className="mb-1.5 flex items-center justify-end gap-2">
              <span className="text-xs text-muted-foreground" aria-live="polite">
                {columnFilterState.filteredRows.length} of {data.length} shown
              </span>
              <Button type="button" variant="ghost" size="sm" onClick={columnFilterState.clearAll}>
                <X aria-hidden="true" />
                Clear filters
              </Button>
            </div>
          ) : null}

          {data.length > 0 && columnFilterState.filteredRows.length === 0 ? (
            <FilterEmptyState
              label="No campaigns match these filters."
              onClear={columnFilterState.clearAll}
            />
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[480px] text-left text-xs">
                <caption className="sr-only">Promotion ROI by campaign</caption>
                <thead>
                  <tr className="border-b border-border text-muted-foreground">
                    <th scope="col" className="py-1.5 pr-3 font-medium">
                      Campaign
                    </th>
                    <th scope="col" className="py-1.5 pr-3 font-medium">
                      <span className="inline-flex items-center gap-1">
                        Platform
                        <ColumnFilterButton
                          type="categorical"
                          columnLabel="Platform"
                          options={columnFilterState.getOptions(1)}
                          selected={columnFilterState.getCategoricalSelection(1)}
                          onToggle={(value) => columnFilterState.toggleCategoricalValue(1, value)}
                          onClear={() => columnFilterState.clearColumn(1)}
                        />
                      </span>
                    </th>
                    <th scope="col" className="py-1.5 pr-3 font-medium">
                      <span className="inline-flex items-center gap-1">
                        Spend
                        <ColumnFilterButton
                          type="numeric"
                          columnLabel="Spend"
                          {...columnFilterState.getNumericRange(2)}
                          onApply={(min, max) => columnFilterState.setNumericRange(2, min, max)}
                          onClear={() => columnFilterState.clearColumn(2)}
                        />
                      </span>
                    </th>
                    <th scope="col" className="py-1.5 pr-3 font-medium">
                      Incremental revenue
                    </th>
                    <th scope="col" className="py-1.5 pr-3 font-medium">
                      <span className="inline-flex items-center gap-1">
                        Net
                        <ColumnFilterButton
                          type="numeric"
                          columnLabel="Net"
                          {...columnFilterState.getNumericRange(4)}
                          onApply={(min, max) => columnFilterState.setNumericRange(4, min, max)}
                          onClear={() => columnFilterState.clearColumn(4)}
                        />
                      </span>
                    </th>
                    <th scope="col" className="py-1.5 pr-3 font-medium">
                      Sources
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {columnFilterState.filteredRows.map((datum) => (
                    <tr key={datum.campaignId} className="border-b border-border/60">
                      <td className="py-1.5 pr-3 text-foreground">
                        {datum.campaignId}
                      </td>
                      <td className="py-1.5 pr-3 text-foreground">
                        {datum.platform}
                      </td>
                      <td className="py-1.5 pr-3 tabular-nums text-foreground">
                        {formatUsd(datum.spend)}
                      </td>
                      <td className="py-1.5 pr-3 tabular-nums text-foreground">
                        {datum.attributedIncrementalRevenue === null
                          ? 'Unattributable'
                          : formatUsd(datum.attributedIncrementalRevenue)}
                      </td>
                      <td
                        className={cn(
                          'py-1.5 pr-3 font-medium tabular-nums',
                          datum.net === null
                            ? 'text-destructive-text'
                            : datum.net >= 0
                              ? 'text-success-text'
                              : 'text-destructive-text',
                        )}
                      >
                        {datum.net === null
                          ? 'Refused — cannot attribute (FR-013)'
                          : formatSignedUsd(datum.net)}
                      </td>
                      <td className="py-1.5 pr-3">
                        <ProvenanceTag refs={datum.sourceRefs} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      ) : null}
    </figure>
  )
}

export default PromoRoiChart
