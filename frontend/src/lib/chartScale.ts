// ---------------------------------------------------------------------------
// Axis scale maths, shared by every chart that draws a value axis.
//
// Why this exists: each chart used to pick its own tick step from a short
// hardcoded ladder — `span > 2000 ? 500 : span > 800 ? 200 : 100`. That was
// tuned to the 14-day opening window, where a span of a few hundred dollars
// was the whole world. Against the live dataset a bucketed period spans well
// past $36,000 (a bad week is −$12,900, a good one +$23,400), and the ladder
// still capped the step at $500 — so the chart drew ~75 gridlines, each 4px
// apart, with unformatted labels stacked on top of one another. The reported
// symptom ("the precision of the lines on the Y axis is bad") is exactly that.
//
// The fix is the standard "nice numbers" tick algorithm rather than another
// tier in the ladder: pick the step from the 1/2/5 × 10^n family that lands
// closest (in log space) to `span / targetTickCount`. It is scale-free, so it
// behaves identically at $50, $650, or $36,000, and it is the same algorithm
// D3's `d3.ticks` / `tickIncrement` uses — see Heckbert, "Nice Numbers for
// Graph Labels", Graphics Gems (1990).
// ---------------------------------------------------------------------------

/**
 * Geometric midpoints between the 1/2/5/10 candidates. Comparing the
 * normalised step against these — rather than against 2, 5 and 10 directly —
 * rounds to the NEAREST nice number in log space instead of always rounding
 * up, which is what keeps the realised tick count inside the target band
 * instead of drifting to half of it.
 */
const SQRT_50 = Math.sqrt(50)
const SQRT_10 = Math.sqrt(10)
const SQRT_2 = Math.sqrt(2)

/**
 * A tick step of 5 aims the realised tick count at roughly 5–8 gridlines once
 * the domain is rounded outwards to whole steps on both ends. That is the
 * density the `dataviz` skill's mark specs ask for: enough gridlines that a
 * bar's value can be read off the axis, few enough that they stay recessive.
 */
export const DEFAULT_TARGET_TICK_COUNT = 5

/**
 * The smallest 1/2/5 × 10^n step that divides `span` into approximately
 * `targetTickCount` intervals.
 *
 * Scale-free by construction: `niceTickStep(span * 10, n)` is always
 * `niceTickStep(span, n) * 10`.
 */
export function niceTickStep(
  span: number,
  targetTickCount: number = DEFAULT_TARGET_TICK_COUNT,
): number {
  if (!Number.isFinite(span) || span <= 0) return 1
  const rough = span / Math.max(1, targetTickCount)
  const power = Math.floor(Math.log10(rough))
  const normalised = rough / 10 ** power
  const multiple =
    normalised >= SQRT_50 ? 10 : normalised >= SQRT_10 ? 5 : normalised >= SQRT_2 ? 2 : 1
  // Multiplying by a negative power of ten accumulates binary error
  // ($0.30000000000000004 ticks); dividing by the positive power does not.
  return power >= 0 ? multiple * 10 ** power : multiple / 10 ** -power
}

export interface LinearTickScale {
  /** Domain floor, rounded outwards to a whole step. */
  min: number
  /** Domain ceiling, rounded outwards to a whole step. */
  max: number
  step: number
  /** Every tick from `min` to `max` inclusive. */
  ticks: number[]
}

/**
 * A full axis domain: a nice step, the raw extent rounded outwards to whole
 * multiples of it, and the ticks in between. `rawMin`/`rawMax` are the real
 * data extent — callers that need a zero baseline pass a `rawMin`/`rawMax`
 * that already include 0, so the baseline is a real tick rather than an
 * arbitrary line.
 */
export function buildLinearTickScale(
  rawMin: number,
  rawMax: number,
  targetTickCount: number = DEFAULT_TARGET_TICK_COUNT,
): LinearTickScale {
  const safeMin = Number.isFinite(rawMin) ? rawMin : 0
  const safeMax = Number.isFinite(rawMax) ? rawMax : 0
  const low = Math.min(safeMin, safeMax)
  const high = Math.max(safeMin, safeMax)
  // A flat series (every value identical, or a single point) has no span to
  // divide. Give it a symmetric unit window so it renders as one flat line
  // against a readable axis instead of dividing by zero.
  const span = high - low || Math.abs(high) || 1

  const step = niceTickStep(span, targetTickCount)
  let min = Math.floor(low / step) * step
  let max = Math.ceil(high / step) * step
  // A flat series whose value happens to sit exactly on a step boundary
  // (every day closed at $500) collapses min and max onto the same tick,
  // which would render a single gridline and divide by zero in the pixel
  // mapping. Open one step of air either side so the flat line has somewhere
  // to sit and the axis still reads.
  if (min === max) {
    min -= step
    max += step
  }

  const ticks: number[] = []
  const tickCount = Math.round((max - min) / step)
  for (let index = 0; index <= tickCount; index++) {
    // `min + index * step` rather than a running `tick += step` accumulator:
    // the accumulator drifts by a cent or two over a long axis and renders
    // "$4,999.999999" as a gridline label.
    ticks.push(roundToStepPrecision(min + index * step, step))
  }

  return { min, max, step, ticks }
}

/**
 * Snaps a computed tick back onto the decimal precision its own step implies,
 * clearing the binary dust that `min + index * step` leaves behind.
 */
function roundToStepPrecision(value: number, step: number): number {
  const decimals = Math.max(0, Math.min(10, -Math.floor(Math.log10(step))))
  const factor = 10 ** decimals
  return Math.round(value * factor) / factor
}

/**
 * Axis-tick money, sized so the label fits the gutter and reads at a glance.
 *
 * The `dataviz` skill asks axis ticks to be "round to clean numbers
 * (0 / 1,000 / 2,000), thousands-comma'd" and stat values to auto-compact.
 * At the live dataset's scale a literal "-12500" (what these charts printed
 * before) is both unreadable and wider than the axis gutter, so anything from
 * a thousand up compacts to K/M and everything below keeps its comma.
 *
 * `step` drives the decimal places so no two adjacent ticks can ever collapse
 * to the same label: a $500 step renders "$2.5K", a $5,000 step renders "$25K".
 *
 * Uses U+2212 MINUS SIGN, matching the signed-currency formatting the charts
 * already use in tooltips and tables — a hyphen reads as a dash next to a
 * currency symbol at 10px.
 */
export function formatAxisCurrency(value: number, step: number): string {
  if (value === 0) return '$0'
  const sign = value < 0 ? '−' : ''
  const magnitude = Math.abs(value)

  const unit =
    magnitude >= 1_000_000
      ? { divisor: 1_000_000, suffix: 'M' }
      : magnitude >= 1_000
        ? { divisor: 1_000, suffix: 'K' }
        : { divisor: 1, suffix: '' }

  const scaledStep = step / unit.divisor
  // A step finer than a hundredth of the unit cannot be shown compactly
  // without two ticks colliding on the same label — fall back to the full
  // comma'd number, which is short anyway at that step size.
  if (unit.divisor > 1 && scaledStep < 0.01) {
    return `${sign}$${magnitude.toLocaleString('en-US', { maximumFractionDigits: 0 })}`
  }

  const decimals = scaledStep >= 1 ? 0 : scaledStep >= 0.1 ? 1 : 2
  const scaled = (magnitude / unit.divisor).toLocaleString('en-US', {
    minimumFractionDigits: 0,
    maximumFractionDigits: decimals,
  })
  return `${sign}$${scaled}${unit.suffix}`
}

/**
 * Axis-tick percentages. Same contract as `formatAxisCurrency`: the step
 * decides the decimal places, so a 0.5-point step reads "12.5%" while a
 * 5-point step reads "15%" rather than the misleading "15%" a blind
 * `toFixed(0)` printed for a gridline actually sitting at 15.37%.
 */
export function formatAxisPercent(value: number, step: number): string {
  const decimals = step >= 1 ? 0 : step >= 0.1 ? 1 : 2
  return `${value.toFixed(decimals)}%`
}
