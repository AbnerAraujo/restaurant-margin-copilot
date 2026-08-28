import { useEffect, useState } from 'react'

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
 */
const SWING_TOTAL_MS = 2700 // 2.4s swing + .3s delay, matching .coverMark exactly
const FADE_MS = 300

function prefersReducedMotion(): boolean {
  return (
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches
  )
}

export default function SplashScreen() {
  const [phase, setPhase] = useState<'visible' | 'fading' | 'gone'>('visible')

  useEffect(() => {
    // Reduced-motion users get the same near-instant collapse index.css's
    // global rule already applies to the door-swing itself — the splash
    // shouldn't sit on screen for 2.7s doing nothing once the swing it
    // exists to show has been collapsed to near-zero.
    const swingMs = prefersReducedMotion() ? 0 : SWING_TOTAL_MS
    const fadeTimer = setTimeout(() => setPhase('fading'), swingMs)
    const goneTimer = setTimeout(() => setPhase('gone'), swingMs + FADE_MS)
    return () => {
      clearTimeout(fadeTimer)
      clearTimeout(goneTimer)
    }
  }, [])

  if (phase === 'gone') return null

  return (
    <div
      aria-hidden="true"
      className={`fixed inset-0 z-50 flex items-center justify-center bg-background transition-opacity duration-300 ${
        phase === 'fading' ? 'pointer-events-none opacity-0' : 'opacity-100'
      }`}
    >
      <Logo variant="icon" size={140} doorAnimation="once" />
    </div>
  )
}
