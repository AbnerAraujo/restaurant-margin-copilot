/**
 * WCAG 2.x relative-luminance contrast ratio between two sRGB hex colors.
 * Used to verify (rather than eyeball) that a foreground/background token
 * pair clears the AA text threshold — see `colorContrast.test.ts` for the
 * `--primary`/`--primary-foreground` pair this was written to check.
 */
export function contrastRatio(hexA: string, hexB: string): number {
  const luminanceA = relativeLuminance(hexA)
  const luminanceB = relativeLuminance(hexB)
  const lighter = Math.max(luminanceA, luminanceB)
  const darker = Math.min(luminanceA, luminanceB)
  return (lighter + 0.05) / (darker + 0.05)
}

function relativeLuminance(hex: string): number {
  const { r, g, b } = parseHex(hex)
  const [rLin, gLin, bLin] = [r, g, b].map(toLinearChannel)
  return 0.2126 * rLin + 0.7152 * gLin + 0.0722 * bLin
}

function parseHex(hex: string): { r: number; g: number; b: number } {
  const normalized = hex.replace('#', '')
  return {
    r: parseInt(normalized.slice(0, 2), 16),
    g: parseInt(normalized.slice(2, 4), 16),
    b: parseInt(normalized.slice(4, 6), 16),
  }
}

function toLinearChannel(channel8bit: number): number {
  const channel = channel8bit / 255
  return channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4
}
