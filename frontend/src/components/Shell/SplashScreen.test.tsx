import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import SplashScreen from './SplashScreen'

/**
 * Regression coverage for a reported defect: the splash blocked every
 * click for a fixed 2.7s timer, disconnected from what the overlay was
 * actually doing on screen. The fix drives phase changes off the real
 * `animationend`/`transitionend` events instead, with the timers kept only
 * as a safety net (see `src/test/setup.ts` for the `AnimationEvent`/
 * `TransitionEvent` polyfill this relies on — without it, jsdom can't fire
 * these events at all).
 */

function stubMatchMedia(reducedMotion: boolean) {
  vi.stubGlobal('matchMedia', vi.fn().mockReturnValue({ matches: reducedMotion }))
}

function getOverlay() {
  const overlay = screen.getByRole('img', { hidden: true }).closest('[aria-hidden="true"]')
  if (!overlay) throw new Error('overlay not found')
  return overlay
}

describe('SplashScreen', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    stubMatchMedia(false)
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('blocks pointer events while fully visible', () => {
    render(<SplashScreen />)
    expect(getOverlay()).not.toHaveClass('pointer-events-none')
  })

  it('releases pointer events the instant the real door-swing animation ends, not on a fixed timer', () => {
    render(<SplashScreen />)

    // Advance almost, but not all the way, to the old fixed 2.7s duration —
    // if phase were still driven only by that timer this would still be
    // blocking, which is correct; the point is the *next* step, where the
    // real animation event (not the timer) is what releases it.
    act(() => vi.advanceTimersByTime(2000))
    expect(getOverlay()).not.toHaveClass('pointer-events-none')

    fireEvent.animationEnd(getOverlay(), { animationName: 'door-swing' })

    expect(getOverlay()).toHaveClass('pointer-events-none')
    expect(getOverlay()).toHaveClass('opacity-0')
  })

  it('ignores an unrelated animationend event', () => {
    render(<SplashScreen />)

    fireEvent.animationEnd(getOverlay(), { animationName: 'some-other-animation' })

    expect(getOverlay()).not.toHaveClass('pointer-events-none')
  })

  it('unmounts once the real fade transition ends', () => {
    render(<SplashScreen />)

    fireEvent.animationEnd(getOverlay(), { animationName: 'door-swing' })
    fireEvent.transitionEnd(getOverlay(), { propertyName: 'opacity' })

    expect(screen.queryByRole('img', { hidden: true })).not.toBeInTheDocument()
  })

  it('falls back to a timer if the browser never fires the animation/transition events', () => {
    render(<SplashScreen />)

    // No animationend ever fired — the safety net must still eventually
    // release the app rather than blocking it forever.
    act(() => vi.advanceTimersByTime(2700 + 500))
    expect(getOverlay()).toHaveClass('pointer-events-none')

    act(() => vi.advanceTimersByTime(300 + 500))
    expect(screen.queryByRole('img', { hidden: true })).not.toBeInTheDocument()
  })

  it('goes straight to fading under prefers-reduced-motion, without waiting for the swing', () => {
    stubMatchMedia(true)
    render(<SplashScreen />)
    expect(getOverlay()).toHaveClass('pointer-events-none')
  })

  it('a late fallback timer cannot re-open a phase the real events already advanced past', () => {
    render(<SplashScreen />)

    // Real events fire promptly...
    fireEvent.animationEnd(getOverlay(), { animationName: 'door-swing' })
    fireEvent.transitionEnd(getOverlay(), { propertyName: 'opacity' })
    expect(screen.queryByRole('img', { hidden: true })).not.toBeInTheDocument()

    // ...but the mount-time safety timer is still pending underneath and
    // will fire later regardless. It must be a no-op, never reviving the
    // splash back onto the screen.
    act(() => vi.advanceTimersByTime(2700 + 500))
    expect(screen.queryByRole('img', { hidden: true })).not.toBeInTheDocument()
  })
})
