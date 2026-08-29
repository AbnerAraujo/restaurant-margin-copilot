import { useEffect, useState } from 'react'
import type { AnimationEvent, TransitionEvent } from 'react'

import Logo from '@/components/Logo/Logo'

/**
 * A real launch splash, not a tiny sidebar-icon tweak. The batwing-door
 * swing (Logo's `doorAnimation="once"`) is the exact same CSS as
 * docs/presentation.html's .coverMark, and that swing is genuinely
 * imperceptible at the sidebar's 36-40px logo size — the same relative
 * scaleX motion that reads as a clear "doors opening" gesture at the
 * deck's 150px cover mark is a few pixels of movement at app-icon size.
 * This shows the mark at that same large, centered scale, on its own,
 * the way the deck's title slide does, so the gesture is actually visible.
 *
 * Phase advances are driven by the REAL DOM events for the swing
 * (`animationend`, matched on `door-swing` — index.css's keyframe name)
 * and the fade (`transitionend` on `opacity`), not by a JS timer duplicating
 * index.css's 2.4s + 0.3s-delay swing duration and this component's own
 * 300ms fade. Reported live: a purely timer-driven version blocks every
 * click for the full duration it copies from CSS regardless of what the
 * overlay is actually doing on screen at that moment, and the two numbers
 * have no mechanism keeping them in sync if either changes independently.
 * Listening to the actual animation/transition means pointer-events release
 * the instant the overlay itself starts fading — never later than what's
 * visually true, and automatically correct if the CSS timing ever changes.
 *
 * The setTimeout calls below are a safety net only (a browser that never
 * fires the expected event would otherwise leave the splash permanently
 * blocking the app), guarded so a late fallback firing can only ever move
 * `phase` forward, never re-open an already-advanced phase.
 */
const SWING_TOTAL_MS = 2700 // 2.4s swing + .3s delay (index.css .door-swing-once) — fallback only.
const FADE_MS = 300 // this component's own transition-opacity duration — fallback only.
const FALLBACK_SAFETY_MARGIN_MS = 500

function prefersReducedMotion(): boolean {
  return (
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches
  )
}

/** Advances phase forward only — a stale/late fallback timer firing after
 * the real event already advanced things further must never revert it. */
function advancePast(
  from: 'visible' | 'fading',
  to: 'fading' | 'gone',
): (current: 'visible' | 'fading' | 'gone') => 'visible' | 'fading' | 'gone' {
  return (current) => (current === from ? to : current)
}

export default function SplashScreen() {
  const [phase, setPhase] = useState<'visible' | 'fading' | 'gone'>('visible')

  useEffect(() => {
    if (prefersReducedMotion()) {
      // index.css's global reduced-motion rule collapses the swing (and
      // this component's own fade transition) to ~0.01ms. Rather than
      // trust a browser to reliably fire animationend for a near-zero
      // animation, skip straight to fading — there is no swing left to
      // wait for, and the fade below still resolves near-instantly via
      // its own transitionend.
      setPhase(advancePast('visible', 'fading'))
      return
    }
    // Fallback only: the primary path is the door swing's own
    // `animationend` in the JSX below.
    const fallback = setTimeout(
      () => setPhase(advancePast('visible', 'fading')),
      SWING_TOTAL_MS + FALLBACK_SAFETY_MARGIN_MS,
    )
    return () => clearTimeout(fallback)
  }, [])

  useEffect(() => {
    if (phase !== 'fading') return
    // Fallback only: the primary path is `transitionend` in the JSX below.
    const fallback = setTimeout(
      () => setPhase(advancePast('fading', 'gone')),
      FADE_MS + FALLBACK_SAFETY_MARGIN_MS,
    )
    return () => clearTimeout(fallback)
  }, [phase])

  if (phase === 'gone') return null

  return (
    <div
      aria-hidden="true"
      onAnimationEnd={(event: AnimationEvent<HTMLDivElement>) => {
        if (event.animationName === 'door-swing') {
          setPhase(advancePast('visible', 'fading'))
        }
      }}
      onTransitionEnd={(event: TransitionEvent<HTMLDivElement>) => {
        if (event.propertyName === 'opacity') {
          setPhase(advancePast('fading', 'gone'))
        }
      }}
      className={`fixed inset-0 z-50 flex items-center justify-center bg-background transition-opacity duration-300 ${
        phase === 'fading' ? 'pointer-events-none opacity-0' : 'opacity-100'
      }`}
    >
      <Logo variant="icon" size={140} doorAnimation="once" />
    </div>
  )
}
