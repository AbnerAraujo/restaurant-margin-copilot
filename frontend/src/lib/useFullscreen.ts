import { useEffect, useState } from 'react'

/**
 * Real, working state + toggle for the browser's Fullscreen API — extracted
 * out of `FullscreenToggle` (the floating top-right button rendered by
 * `AppShell` on every route) so the Settings page can give the same control
 * a labeled home there too, without re-implementing the
 * `document.fullscreenElement`/`fullscreenchange` wiring a second time. Both
 * call sites drive and observe the exact same OS-level fullscreen state.
 *
 * The Fullscreen API requires an actual user gesture in every major browser
 * (a page cannot force itself fullscreen on load), so `toggle` only ever
 * runs from a click handler.
 */
export function useFullscreen() {
  const [isFullscreen, setIsFullscreen] = useState(
    () => document.fullscreenElement !== null,
  )

  useEffect(() => {
    function handleChange() {
      setIsFullscreen(document.fullscreenElement !== null)
    }
    document.addEventListener('fullscreenchange', handleChange)
    return () => document.removeEventListener('fullscreenchange', handleChange)
  }, [])

  function toggle() {
    if (document.fullscreenElement) {
      document.exitFullscreen().catch(() => undefined)
    } else {
      document.documentElement.requestFullscreen().catch(() => undefined)
    }
  }

  return { isFullscreen, toggle }
}
