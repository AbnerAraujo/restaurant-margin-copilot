import { describe, expect, it, vi } from 'vitest'

import { createUniqueId } from './id'

describe('createUniqueId', () => {
  it('never returns the same value twice in a row', () => {
    const ids = Array.from({ length: 50 }, () => createUniqueId())
    expect(new Set(ids).size).toBe(ids.length)
  })

  /**
   * The regression this guards against: `ChatPanel.tsx` and `AskPage.tsx`
   * used to generate message ids from a `let messageSequence = 0` counter
   * at MODULE level. That counter resets to 0 every time its module is
   * re-evaluated — which is exactly what happens on a page reload — so the
   * first message asked after a reload got the identical id
   * (`assistant-1`, `user-1`, …) as the first message asked before it.
   * `vi.resetModules()` + a fresh dynamic `import()` is the jsdom
   * equivalent of that reload: it forces `id.ts` to be re-evaluated from
   * scratch, the same way the browser would on an actual reload.
   *
   * `createUniqueId` has no module-level state to reset, so this is really
   * a guard against ever reintroducing that pattern here: if a future edit
   * swapped `crypto.randomUUID()` back out for a counter, this test would
   * start failing.
   */
  it('produces ids that do not collide across two simulated page loads', async () => {
    vi.resetModules()
    const loadOne = await import('./id')
    const idsFromLoadOne = [loadOne.createUniqueId(), loadOne.createUniqueId()]

    vi.resetModules()
    const loadTwo = await import('./id')
    const idsFromLoadTwo = [loadTwo.createUniqueId(), loadTwo.createUniqueId()]

    for (const id of idsFromLoadOne) {
      expect(idsFromLoadTwo).not.toContain(id)
    }
  })

  it('falls back to a timestamp + random suffix when crypto.randomUUID is unavailable', () => {
    const originalRandomUUID = crypto.randomUUID
    // @ts-expect-error -- deliberately simulating a runtime without it.
    delete crypto.randomUUID

    try {
      const first = createUniqueId()
      const second = createUniqueId()
      expect(first).not.toBe(second)
      expect(first.length).toBeGreaterThan(0)
    } finally {
      crypto.randomUUID = originalRandomUUID
    }
  })
})
