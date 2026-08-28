import { useState } from 'react'
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
  sourceRefs: SourceRowRef[]
}

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
}

// ---------------------------------------------------------------------------
// Geometry
// ---------------------------------------------------------------------------

const CHART_WIDTH = 560
const CHART_HEIGHT = 300
const MARGIN = { top: 44, right: 16, bottom: 44, left: 48 }
const PLOT_WIDTH = CHART_WIDTH - MARGIN.left - MARGIN.right
const PLOT_HEIGHT = CHART_HEIGHT - MARGIN.top - MARGIN.bottom

const Y_DOMAIN: [number, number] = [-200, 60]
const Y_TICKS = [-150, -100, -50, 0, 50]

const BAR_WIDTH = 24
const BAR_RADIUS = 4
const REFUSAL_BOX_WIDTH = 64
const REFUSAL_BOX_HEIGHT = 28

function yToPixel(value: number): number {
  const [min, max] = Y_DOMAIN
  const clamped = Math.min(Math.max(value, min), max)
  return MARGIN.top + ((max - clamped) / (max - min)) * PLOT_HEIGHT
}

const BASELINE_Y = yToPixel(0)

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

function buildBars(data: PromotionRoiDatum[]): BarGeometry[] {
  const slotWidth = PLOT_WIDTH / data.length
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
function PromoRoiChart({ data = DEFAULT_PROMOTION_ROI, className }: PromoRoiChartProps) {
  const [hoveredIndex, setHoveredIndex] = useState<number | null>(null)
  const [tableOpen, setTableOpen] = useState(false)

  const bars = buildBars(data)
  const hovered = hoveredIndex === null ? null : bars[hoveredIndex]

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

      <div className="relative w-full overflow-x-auto">
        <svg
          viewBox={`0 0 ${CHART_WIDTH} ${CHART_HEIGHT}`}
          role="img"
          aria-label="Bar chart of net ROI across four promotion campaigns, with one campaign flagged as unattributable and refused"
          className="h-auto w-full min-w-[360px]"
        >
          {Y_TICKS.map((tick) => (
            <g key={tick}>
              <line
                x1={MARGIN.left}
                x2={CHART_WIDTH - MARGIN.right}
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
            x2={CHART_WIDTH - MARGIN.right}
            y1={BASELINE_Y}
            y2={BASELINE_Y}
            stroke="var(--border)"
            strokeWidth={1}
          />

          {bars.map((bar) => {
            const { datum, index, slotCenterX, barX, isRefused, isPositive } =
              bar
            const focusLabel = isRefused
              ? `${datum.campaignName}: unattributable, ROI refused — no incremental orders on file`
              : `${datum.campaignName}: net ${formatSignedUsd(datum.net as number)}`

            return (
              <g
                key={datum.campaignId}
                tabIndex={0}
                role="button"
                aria-label={focusLabel}
                onMouseEnter={() => setHoveredIndex(index)}
                onMouseLeave={() => setHoveredIndex(null)}
                onFocus={() => setHoveredIndex(index)}
                onBlur={() => setHoveredIndex(null)}
                className="cursor-pointer outline-none"
              >
                <rect
                  x={barX - 14}
                  y={MARGIN.top}
                  width={BAR_WIDTH + 28}
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

                {/* Direct label mandatory on every bar at only 4 categories */}
                {!isRefused ? (
                  <text
                    x={slotCenterX}
                    y={
                      isPositive
                        ? bar.barY - 8
                        : bar.barY + bar.barHeight + 14
                    }
                    textAnchor="middle"
                    className={cn(
                      'text-[11px] font-semibold tabular-nums',
                      isPositive ? 'fill-success-text' : 'fill-destructive-text',
                    )}
                  >
                    {formatSignedUsd(datum.net as number)}
                  </text>
                ) : null}

                <text
                  x={slotCenterX}
                  y={CHART_HEIGHT - MARGIN.bottom + 16}
                  textAnchor="middle"
                  className="fill-muted-foreground text-[9.5px] font-medium"
                >
                  {datum.campaignId}
                </text>
              </g>
            )
          })}

          <text
            x={CHART_WIDTH - MARGIN.right}
            y={MARGIN.top - 16}
            textAnchor="end"
            className="fill-muted-foreground text-[10px]"
          >
            Net (USD)
          </text>
        </svg>

        {/* Refusal state — dashed outline + icon + text, never a bar of any
            height, matching RefusalBubble's visual language for the same
            "we won't estimate" policy (see FR-013). */}
        {bars
          .filter((bar) => bar.isRefused)
          .map((bar) => (
            <div
              key={bar.datum.campaignId}
              className="pointer-events-none absolute flex -translate-x-1/2 -translate-y-1/2 flex-col items-center gap-1"
              style={{
                left: `${(bar.slotCenterX / CHART_WIDTH) * 100}%`,
                top: `${(BASELINE_Y / CHART_HEIGHT) * 100}%`,
                width: `${(REFUSAL_BOX_WIDTH / CHART_WIDTH) * 100}%`,
              }}
            >
              <div
                className="flex items-center justify-center rounded-md border border-dashed border-destructive/40 bg-destructive/5"
                style={{ height: REFUSAL_BOX_HEIGHT, width: '100%' }}
              >
                <ShieldAlert
                  className="size-3.5 text-destructive-text"
                  aria-hidden="true"
                />
              </div>
              <span className="text-[10px] font-medium text-destructive-text">
                Unattributable
              </span>
            </div>
          ))}

        {hovered ? (
          <div
            role="status"
            className="pointer-events-none absolute z-10 -translate-x-1/2 -translate-y-full rounded-md border border-border bg-popover px-2.5 py-1.5 text-xs shadow-md"
            style={{
              left: `${(hovered.slotCenterX / CHART_WIDTH) * 100}%`,
              top: `${(Math.min(hovered.isRefused ? BASELINE_Y - REFUSAL_BOX_HEIGHT / 2 : hovered.barY, BASELINE_Y) / CHART_HEIGHT) * 100 - 1}%`,
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
            <p className="text-muted-foreground">promotion_ad_spend_export.csv</p>
          </div>
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

      <div className="mt-3 flex flex-wrap items-center justify-between gap-2 border-t border-border/60 pt-2.5">
        <ul
          aria-label="Per-campaign provenance"
          className="flex flex-wrap gap-x-4 gap-y-1"
        >
          {data.map((datum) => (
            <li key={datum.campaignId} className="flex items-center gap-1.5">
              <span className="text-xs text-muted-foreground">
                {datum.campaignId}:
              </span>
              <ProvenanceTag refs={datum.sourceRefs} />
            </li>
          ))}
        </ul>
        <button
          type="button"
          onClick={() => setTableOpen((wasOpen) => !wasOpen)}
          aria-expanded={tableOpen}
          className="shrink-0 text-xs text-muted-foreground underline decoration-dotted underline-offset-2 hover:text-foreground focus-visible:text-foreground focus-visible:outline-none"
        >
          {tableOpen ? 'Hide table' : 'View as table'}
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
