import { useEffect, useState } from 'react'
import { Maximize, Minimize } from 'lucide-react'

/**
 * A real, working fullscreen toggle — not automatic on launch, because no
 * installed web app can force that: the Fullscreen API requires an actual
 * user gesture in every major browser, a deliberate rule against a site
 * hijacking the whole screen the moment it loads. One click here fills the
 * viewport with no browser/window chrome at all, same as the presentation
 * deck's own "F" fullscreen affordance — this is that same real OS-level
 * fullscreen, not a CSS trick.
 */
export default function FullscreenToggle() {
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

  return (
    <button
      type="button"
      onClick={toggle}
      aria-pressed={isFullscreen}
      aria-label={isFullscreen ? 'Exit full screen' : 'Enter full screen'}
      title={isFullscreen ? 'Exit full screen' : 'Enter full screen'}
      className="fixed top-4 right-4 z-20 flex items-center justify-center rounded-full border border-border bg-card/95 p-2 text-muted-foreground shadow-sm backdrop-blur-sm transition-colors hover:bg-card hover:text-foreground focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
    >
      {isFullscreen ? (
        <Minimize className="size-4" aria-hidden="true" />
      ) : (
        <Maximize className="size-4" aria-hidden="true" />
      )}
    </button>
  )
}
