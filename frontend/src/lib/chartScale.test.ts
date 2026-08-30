import { describe, expect, it } from 'vitest'

import {
  buildLinearTickScale,
  formatAxisCurrency,
  formatAxisPercent,
  niceTickStep,
} from './chartScale'

/**
 * These are the pure functions behind every value axis in the app. They are
 * tested directly, at the scales the live data actually reaches, rather than
 * through a rendered chart — a snapshot of an SVG tells you the markup
 * changed, not whether the axis is readable.
 *
 * The spans below are real: `$50` is a single quiet day, `~$650` is the
 * hand-authored 14-day opening window this chart was originally built
 * against, and `~$36,300` is the span of the live 759-day dataset once
 * MarginTrendChart buckets it into weekly totals (min −$12,913.26, max
 * +$23,409.48 as of 2026-08-29). The old hardcoded ladder capped its step at
 * $500 for every one of them, which is 73 gridlines at the largest.
 */

const REAL_WORLD_SPANS = [
  { name: 'a single quiet day', span: 50 },
  { name: 'the hand-authored 14-day opening window', span: 650 },
  { name: 'a full month of daily margins', span: 8_000 },
  { name: 'the live dataset bucketed into weekly totals', span: 36_322.74 },
  { name: 'a multi-year monthly roll-up', span: 128_400 },
  { name: 'cents-scale rounding differences', span: 0.4 },
  { name: 'a promotion ROI range', span: 784.23 },
]

describe('niceTickStep', () => {
  // Rounding to the nearest nice number in log space bounds the realised
  // interval count to targetTickCount × [1/√2, √2] — 3.5 to 7.1 at the
  // default target of 5 — for ANY span, which is the whole point: no tier of
  // a hardcoded ladder can be outgrown.
  it.each(REAL_WORLD_SPANS)(
    'divides $name ($span) into 3.5 to 7.1 intervals',
    ({ span }) => {
      const intervals = span / niceTickStep(span)
      expect(intervals).toBeGreaterThanOrEqual(3.5)
      expect(intervals).toBeLessThanOrEqual(7.1)
    },
  )

  it.each(REAL_WORLD_SPANS)('picks a 1, 2 or 5 × 10^n step for $name', ({ span }) => {
    // The leading digit of the step is the property that makes every tick a
    // round number a reader can do arithmetic with.
    const leadingDigit = Number(niceTickStep(span).toExponential().split('e')[0])
    expect([1, 2, 5]).toContain(leadingDigit)
  })

  it('is scale-free — a 10x larger span yields a 10x larger step', () => {
    for (const { span } of REAL_WORLD_SPANS) {
      expect(niceTickStep(span * 10)).toBeCloseTo(niceTickStep(span) * 10, 6)
      expect(niceTickStep(span / 10)).toBeCloseTo(niceTickStep(span) / 10, 6)
    }
  })

  it('honours a requested tick count', () => {
    // 12 intervals over 1200 wants ~100; 3 wants ~500.
    expect(niceTickStep(1200, 12)).toBe(100)
    expect(niceTickStep(1200, 3)).toBe(500)
  })

  it('falls back to a unit step for a zero or non-finite span', () => {
    expect(niceTickStep(0)).toBe(1)
    expect(niceTickStep(-5)).toBe(1)
    expect(niceTickStep(Number.NaN)).toBe(1)
  })
})

describe('buildLinearTickScale', () => {
  it.each(REAL_WORLD_SPANS)(
    'keeps $name to at most 9 gridlines',
    ({ span }) => {
      const { ticks } = buildLinearTickScale(-span / 3, (span * 2) / 3)
      expect(ticks.length).toBeGreaterThanOrEqual(4)
      // Rounding the domain outwards on BOTH ends can add one tick per end on
      // top of the target band, so 9 is the true ceiling, not 8.
      expect(ticks.length).toBeLessThanOrEqual(9)
    },
  )

  it.each(REAL_WORLD_SPANS)('emits only whole multiples of the step for $name', ({ span }) => {
    const { ticks, step } = buildLinearTickScale(-span / 3, (span * 2) / 3)
    for (const tick of ticks) {
      expect(Math.abs(tick / step - Math.round(tick / step))).toBeLessThan(1e-9)
    }
  })

  it('brackets the real extent so no value is ever clamped to the axis edge', () => {
    const { min, max } = buildLinearTickScale(-12_913.26, 23_409.48)
    expect(min).toBeLessThanOrEqual(-12_913.26)
    expect(max).toBeGreaterThanOrEqual(23_409.48)
  })

  it('puts a real tick on zero when the domain crosses it', () => {
    const { ticks } = buildLinearTickScale(-12_913.26, 23_409.48)
    expect(ticks).toContain(0)
  })

  it('gives the live weekly-bucket range a readable $10K step', () => {
    const scale = buildLinearTickScale(-12_913.26, 23_409.48)
    expect(scale.step).toBe(10_000)
    expect(scale.ticks).toEqual([-20_000, -10_000, 0, 10_000, 20_000, 30_000])
  })

  it('leaves the hand-authored 14-day window on the $200 step it always had', () => {
    // 2024-08-01..14, min margin 331.52, max 1019.45, zero-baselined.
    const scale = buildLinearTickScale(0, 1019.45)
    expect(scale.step).toBe(200)
    expect(scale.ticks).toEqual([0, 200, 400, 600, 800, 1000, 1200])
  })

  it('does not divide by zero on a flat series', () => {
    const { ticks, step } = buildLinearTickScale(500, 500)
    expect(step).toBeGreaterThan(0)
    expect(ticks.length).toBeGreaterThanOrEqual(2)
    expect(ticks).toContain(500)
  })

  it('handles a single zero-valued point', () => {
    const { ticks } = buildLinearTickScale(0, 0)
    expect(ticks).toContain(0)
    expect(ticks.every((tick) => Number.isFinite(tick))).toBe(true)
  })

  it('carries no floating-point dust into sub-unit ticks', () => {
    const { ticks } = buildLinearTickScale(0, 1)
    for (const tick of ticks) {
      expect(tick.toString()).not.toMatch(/\d{6,}$/)
    }
  })
})

describe('formatAxisCurrency', () => {
  it('renders zero as a bare $0', () => {
    expect(formatAxisCurrency(0, 5000)).toBe('$0')
  })

  it('compacts thousands so the label fits the axis gutter', () => {
    expect(formatAxisCurrency(20_000, 10_000)).toBe('$20K')
    expect(formatAxisCurrency(-20_000, 10_000)).toBe('−$20K')
  })

  it('keeps a decimal when the step needs one to stay distinct', () => {
    expect(formatAxisCurrency(2500, 500)).toBe('$2.5K')
    expect(formatAxisCurrency(3000, 500)).toBe('$3K')
  })

  it('compacts millions', () => {
    expect(formatAxisCurrency(2_500_000, 500_000)).toBe('$2.5M')
  })

  it('commas hundreds rather than compacting them', () => {
    expect(formatAxisCurrency(800, 200)).toBe('$800')
    expect(formatAxisCurrency(-400, 200)).toBe('−$400')
  })

  it('never renders two adjacent ticks with the same label', () => {
    for (const { span } of REAL_WORLD_SPANS) {
      const { ticks, step } = buildLinearTickScale(-span / 3, (span * 2) / 3)
      const labels = ticks.map((tick) => formatAxisCurrency(tick, step))
      expect(new Set(labels).size).toBe(labels.length)
    }
  })
})

describe('formatAxisPercent', () => {
  it('drops decimals when the step is a whole point', () => {
    expect(formatAxisPercent(15, 5)).toBe('15%')
  })

  it('keeps a decimal when the step is finer than a point', () => {
    expect(formatAxisPercent(12.5, 0.5)).toBe('12.5%')
  })

  it('never labels two adjacent ticks identically', () => {
    const { ticks, step } = buildLinearTickScale(0, 3.4, 4)
    const labels = ticks.map((tick) => formatAxisPercent(tick, step))
    expect(new Set(labels).size).toBe(labels.length)
  })
})
