import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

import { contrastRatio } from './colorContrast'

const AA_NORMAL_TEXT_MINIMUM = 4.5

const __dirname = dirname(fileURLToPath(import.meta.url))
const indexCssSource = readFileSync(join(__dirname, '..', 'index.css'), 'utf-8')

/**
 * Pulls a `--token: #hex;` value out of a specific CSS block (`:root { ... }`
 * or `.dark { ... }`) so these tests check the tokens actually shipped in
 * `index.css` rather than a copy that can silently drift from it.
 */
function readToken(blockSelector: string, token: string): string {
  const blockPattern = new RegExp(`${escapeRegExp(blockSelector)}\\s*{([^}]*)}`, 's')
  const block = indexCssSource.match(blockPattern)
  if (!block) {
    throw new Error(`Could not find CSS block "${blockSelector}" in index.css`)
  }
  const tokenPattern = new RegExp(`${escapeRegExp(token)}:\\s*(#[0-9a-fA-F]{6})`)
  const match = block[1].match(tokenPattern)
  if (!match) {
    throw new Error(`Could not find token "${token}" in CSS block "${blockSelector}"`)
  }
  return match[1]
}

function escapeRegExp(literal: string): string {
  return literal.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

describe('contrastRatio', () => {
  it('matches known WCAG reference pairs', () => {
    expect(contrastRatio('#ffffff', '#000000')).toBeCloseTo(21, 0)
    expect(contrastRatio('#ffffff', '#ffffff')).toBeCloseTo(1, 5)
  })
})

describe('--primary / --primary-foreground contrast (index.css)', () => {
  it('clears AA (4.5:1) in light mode', () => {
    const primary = readToken(':root', '--primary')
    const foreground = readToken(':root', '--primary-foreground')
    expect(contrastRatio(primary, foreground)).toBeGreaterThanOrEqual(AA_NORMAL_TEXT_MINIMUM)
  })

  it('clears AA (4.5:1) in dark mode', () => {
    // Regression test for the dark-mode primary button / chat-bubble
    // contrast defect: white-on-#1fa876 measured 3.03:1. Dark mode now gets
    // its own --primary-foreground rather than inheriting light mode's.
    const primary = readToken('.dark', '--primary')
    const foreground = readToken('.dark', '--primary-foreground')
    const ratio = contrastRatio(primary, foreground)
    expect(ratio).toBeGreaterThanOrEqual(AA_NORMAL_TEXT_MINIMUM)
  })
})
