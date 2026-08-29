import { useEffect, useRef, useState } from 'react'
import { ShieldAlert } from 'lucide-react'

import { cn } from '@/lib/utils'
import ProvenanceTag, {
  type SourceRowRef,
} from '@/components/Provenance/ProvenanceTag'

// ---------------------------------------------------------------------------
// Data — the exact `promotion_ad_spend_export.csv` fixture (see
// backend/fixtures/README.md "Promotion ROI" table). `net: null` for
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
const MARGIN = { top: 44, right: 16, bottom: 56, left: 48 }
const PLOT_HEIGHT = CHART_HEIGHT - MARGIN.top - MARGIN.bottom

const BAR_WIDTH = 24
const BAR_RADIUS = 4
const REFUSAL_BOX_WIDTH = 64
const REFUSAL_BOX_HEIGHT = 28

// ---------------------------------------------------------------------------
// Scale — this chart was built and tested against the 4-campaign fixture,
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
// fixture included) this renders pixel-identical to before:
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
// original 4-campaign fixture's wide slots (124px+), but at MIN_SLOT_WIDTH
// (28px) that 52px-wide hit-rect overlaps BOTH neighbors' hit-rects by a
// large margin, so hovering near a slot boundary could trigger the WRONG
// bar's tooltip ("getting data from the left bar"). HIT_RECT_MAX_PADDING
// is now a ceiling, not a fixed value — the real padding used per-render
// is clamped so adjacent hit-rects never overlap (see hitRectPadding
// below), while still using the full 14px at low campaign counts where
// slots are wide enough to afford it.
const HIT_RECT_MAX_PADDING = 14
const HIT_RECT_MIN_PADDING = 2

/**
 * Y scale derived from the data, not hard-coded. The previous fixed
 * [-200, 60] domain was tuned to one fixture; against live /api/promotions
 * rows a campaign outside it would be CLAMPED to the axis edge and drawn
 * smaller than the loss it represents.
 */
function buildScale(data: PromotionRoiDatum[]) {
  const values = data
    .map((datum) => datum.net)
    .filter((value): value is number => value !== null)
  const rawMin = Math.min(0, ...values)
  const rawMax = Math.max(0, ...values)
  const span = rawMax - rawMin || 1
  const step = span > 2000 ? 500 : span > 800 ? 200 : 50
  const min = Math.floor(rawMin / step) * step
  const max = Math.ceil(rawMax / step) * step

  const ticks: number[] = []
  for (let tick = min; tick <= max; tick += step) ticks.push(tick)

  const yToPixel = (value: number) => {
    const clamped = Math.min(Math.max(value, min), max)
    return MARGIN.top + ((max - clamped) / (max - min || 1)) * PLOT_HEIGHT
  }
  return { ticks, yToPixel, baselineY: yToPixel(0) }
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
}: PromoRoiChartProps) {
  const [hoveredIndex, setHoveredIndex] = useState<number | null>(null)
  const [tableOpen, setTableOpen] = useState(defaultTableOpen)

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

  // A `not_yet_attributed` campaign has nothing to plot yet — not a refusal,
  // just "hasn't been checked yet" — so it never reaches the bars, the
  // y-scale, or the SVG's slot layout. `data` (unfiltered) still drives the
  // table and provenance list further down, since every logged campaign
  // belongs there.
  const chartableData = data.filter(
    (datum) => datum.reason !== NOT_YET_ATTRIBUTED,
  )
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
  const { ticks, yToPixel, baselineY } = buildScale(chartableData)
  const bars = buildBars(chartableData, yToPixel, plotWidth)
  const hovered = hoveredIndex === null ? null : bars[hoveredIndex]

  const netValues = chartableData
    .map((d) => d.net)
    .filter((v): v is number => v !== null)
  const maxNet = netValues.length > 0 ? Math.max(...netValues) : null
  const minNet = netValues.length > 0 ? Math.min(...netValues) : null
  const maxIndex = chartableData.findIndex((d) => d.net === maxNet)
  const minIndex = chartableData.findIndex((d) => d.net === minNet)
  const refusedCount = bars.filter((bar) => bar.isRefused).length

  return (
    <figure
      aria-label="Promotion ROI"
      className={cn('rounded-lg border border-border bg-card p-4 sm:p-5', className)}
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
      <div ref={containerRef} className="w-full overflow-x-auto">
        <div
          className="relative"
          style={chartWidth > CHART_WIDTH ? { width: chartWidth } : undefined}
        >
        <svg
          viewBox={`0 0 ${chartWidth} ${CHART_HEIGHT}`}
          // See CategoryBarChart: role="img" cannot contain the focusable
          // per-campaign targets below (axe nested-interactive).
          role="group"
          aria-label={`Bar chart of net ROI across ${chartableData.length} promotion campaign${
            chartableData.length === 1 ? '' : 's'
          }${refusedCount > 0 ? `, with ${refusedCount} flagged as unattributable and refused` : ''}`}
          // See MarginTrendChart: capped at its design width, which itself
          // grows with campaign count so bars get real room past the
          // 4-campaign fixture instead of overlapping or floating in dead
          // space to the right of a canvas that never grew to fit them. Past
          // the base 560px, a fixed pixel width (not `w-full`) is what makes
          // the wrapper's `overflow-x-auto` actually scroll instead of
          // silently rescaling every bar back down to the panel's width.
          style={
            chartWidth > CHART_WIDTH ? { width: chartWidth } : { maxWidth: chartWidth }
          }
          className={cn(
            'h-auto min-w-[360px]',
            chartWidth > CHART_WIDTH ? '' : 'w-full',
          )}
        >
          {ticks.map((tick) => (
            <g key={tick}>
              <line
                x1={MARGIN.left}
                x2={chartWidth - MARGIN.right}
                y1={yToPixel(tick)}
                y2={yToPixel(tick)}
                stroke="var(--border)"
                strokeWidth={1}
                opacity={tick === 0 ? 0 : 0.6}
              />
              <text
                x={MARGIN.left - 8}
                y={yToPixel(tick)}
                textAnchor="end"
                dominantBaseline="middle"
                className="fill-muted-foreground text-[10px] tabular-nums"
              >
                {tick}
              </text>
            </g>
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
              ? `${datum.campaignName}: unattributable, ROI refused — no incremental orders on file`
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
                    categories (the 4-campaign fixture qualifies); past that,
                    only the two extremes get one — see the Scale comment
                    above for why a label on all 25-30+ bars stopped being
                    readable. */}
                {showValueLabel ? (
                  <text
                    x={slotCenterX}
                    y={
                      isPositive
                        ? bar.barY - 8
                        : bar.barY + bar.barHeight + 14
                    }
                    textAnchor="middle"
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
                {index % tickLabelStep === 0 ? (
                  <text
                    x={slotCenterX}
                    y={CHART_HEIGHT - MARGIN.bottom + 16}
                    textAnchor="middle"
                    className="fill-muted-foreground text-[9.5px] font-medium"
                  >
                    {datum.campaignId}
                  </text>
                ) : null}
              </g>
            )
          })}

          <text
            x={chartWidth - MARGIN.right}
            y={MARGIN.top - 16}
            textAnchor="end"
            className="fill-muted-foreground text-[10px]"
          >
            Net (USD)
          </text>

          {/* X-axis title — same role as "Net (USD)" above, naming what the
              bars ARE (one per campaign) rather than leaving the axis to be
              inferred from whichever ids happen to be labeled. */}
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
              style={{
                left: `${(bar.slotCenterX / chartWidth) * 100}%`,
                top: `${(baselineY / CHART_HEIGHT) * 100}%`,
                width: `${(refusalBoxWidth / chartWidth) * 100}%`,
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
            className="pointer-events-none absolute z-10 -translate-x-1/2 -translate-y-full rounded-md border border-border bg-popover px-2.5 py-1.5 text-xs shadow-md"
            style={{
              left: `${(hovered.slotCenterX / chartWidth) * 100}%`,
              top: `${(Math.min(hovered.isRefused ? baselineY - refusalBoxHeight / 2 : hovered.barY, baselineY) / CHART_HEIGHT) * 100 - 1}%`,
            }}
          >
            <p className="font-medium text-muted-foreground">
              {hovered.datum.campaignName}
            </p>
            <p className="text-muted-foreground">
              {hovered.datum.platform} · spend {formatUsd(hovered.datum.spend)}
            </p>
            {hovered.isRefused ? (
              <p className="font-semibold text-destructive-text">
                Unattributable — refusing to estimate (FR-013)
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
          4-campaign fixture, unreadable at 25-30 real campaigns (reported
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
        <div className="mt-2 overflow-x-auto">
          <table className="w-full min-w-[480px] text-left text-xs">
            <caption className="sr-only">Promotion ROI by campaign</caption>
            <thead>
              <tr className="border-b border-border text-muted-foreground">
                <th scope="col" className="py-1.5 pr-3 font-medium">
                  Campaign
                </th>
                <th scope="col" className="py-1.5 pr-3 font-medium">
                  Platform
                </th>
                <th scope="col" className="py-1.5 pr-3 font-medium">
                  Spend
                </th>
                <th scope="col" className="py-1.5 pr-3 font-medium">
                  Incremental revenue
                </th>
                <th scope="col" className="py-1.5 pr-3 font-medium">
                  Net
                </th>
                <th scope="col" className="py-1.5 pr-3 font-medium">
                  Sources
                </th>
              </tr>
            </thead>
            <tbody>
              {data.map((datum) => (
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
      ) : null}
    </figure>
  )
}

export default PromoRoiChart
