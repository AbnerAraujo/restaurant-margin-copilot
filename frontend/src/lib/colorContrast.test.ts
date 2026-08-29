import { describe, expect, it } from 'vitest'

import { contrastRatio } from './colorContrast'

const AA_NORMAL_TEXT_MINIMUM = 4.5

/**
 * `--primary` / `--primary-foreground` from `../index.css`, light and dark.
 * Not parsed out of the stylesheet at test time (no Node `fs` access is
 * available under this project's browser-only `tsconfig` types, and Vite's
 * CSS pipeline rewrites `?raw` imports of processed stylesheets rather than
 * returning the source verbatim) — kept as literals instead, mirroring the
 * exact values `index.css` ships. If either token changes, update both
 * places together.
 */
const LIGHT_MODE_PRIMARY = '#0e6e52'
const LIGHT_MODE_PRIMARY_FOREGROUND = '#ffffff'
const DARK_MODE_PRIMARY = '#1fa876'
const DARK_MODE_PRIMARY_FOREGROUND = '#052e14'

describe('contrastRatio', () => {
  it('matches known WCAG reference pairs', () => {
    expect(contrastRatio('#ffffff', '#000000')).toBeCloseTo(21, 0)
    expect(contrastRatio('#ffffff', '#ffffff')).toBeCloseTo(1, 5)
  })

  it('is symmetric regardless of argument order', () => {
    expect(contrastRatio('#123456', '#abcdef')).toBeCloseTo(
      contrastRatio('#abcdef', '#123456'),
      10,
    )
  })
})

describe('--primary / --primary-foreground contrast (index.css)', () => {
  it('clears AA (4.5:1) in light mode', () => {
    const ratio = contrastRatio(LIGHT_MODE_PRIMARY, LIGHT_MODE_PRIMARY_FOREGROUND)
    expect(ratio).toBeGreaterThanOrEqual(AA_NORMAL_TEXT_MINIMUM)
  })

  it('clears AA (4.5:1) in dark mode', () => {
    // Regression test for the dark-mode primary-button / chat-bubble
    // contrast defect: white-on-#1fa876 measured 3.03:1. Dark mode now gets
    // its own --primary-foreground (a near-black green, matching the
    // treatment already used for --success-foreground/--warning-foreground)
    // rather than inheriting light mode's pure white.
    const ratio = contrastRatio(DARK_MODE_PRIMARY, DARK_MODE_PRIMARY_FOREGROUND)
    expect(ratio).toBeGreaterThanOrEqual(AA_NORMAL_TEXT_MINIMUM)
  })

  it('would have failed before the fix (documents the regression, not a live assertion)', () => {
    const oldRatio = contrastRatio(DARK_MODE_PRIMARY, '#ffffff')
    expect(oldRatio).toBeLessThan(AA_NORMAL_TEXT_MINIMUM)
  })
})
