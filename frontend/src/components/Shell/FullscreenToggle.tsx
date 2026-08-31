import { Maximize, Minimize } from 'lucide-react'

import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { useFullscreen } from '@/lib/useFullscreen'

/**
 * A real, working fullscreen toggle — not automatic on launch, because no
 * installed web app can force that: the Fullscreen API requires an actual
 * user gesture in every major browser, a deliberate rule against a site
 * hijacking the whole screen the moment it loads. One click here fills the
 * viewport with no browser/window chrome at all, same as the presentation
 * deck's own "F" fullscreen affordance — this is that same real OS-level
 * fullscreen, not a CSS trick.
 *
 * The state + toggle logic itself lives in `useFullscreen` (`lib/`) so the
 * Settings page's labeled "Full screen" control drives this exact same
 * browser state rather than a second, disconnected implementation.
 *
 * The hint used to ride on a native `title=` attribute — this button sits
 * fixed over page content on every route, so its only label was whatever the
 * browser's own default tooltip happened to render, on the browser's own
 * timing. It now uses the app's styled Tooltip instead; `aria-label` (below)
 * remains the actual accessible name either way.
 */
export default function FullscreenToggle() {
  const { isFullscreen, toggle } = useFullscreen()
  const label = isFullscreen ? 'Exit full screen' : 'Enter full screen'

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          onClick={toggle}
          aria-pressed={isFullscreen}
          aria-label={label}
          // top-14 below `lg`, top-4 at `lg` and up: this button is fixed
          // to the viewport, so on mobile it always sits in the same
          // screen position regardless of MobileNavBar's own horizontal
          // scroll -- found live intercepting clicks on the "Promotions"
          // nav pill (and visually covering "Upload costs") at multiple
          // scroll positions, since MobileNavBar only exists below `lg`
          // and occupies that same top-right corner. Dropping below its
          // ~45px height, rather than reserving horizontal padding INSIDE
          // the scroller, is the only fix that holds at every scroll
          // position, not just the fully-scrolled-right one.
          className="fixed top-14 right-4 z-20 flex items-center justify-center rounded-full border border-border bg-card/95 p-2 text-muted-foreground shadow-sm backdrop-blur-sm transition-colors hover:bg-card hover:text-foreground focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 lg:top-4"
        >
          {isFullscreen ? (
            <Minimize className="size-4" aria-hidden="true" />
          ) : (
            <Maximize className="size-4" aria-hidden="true" />
          )}
        </button>
      </TooltipTrigger>
      <TooltipContent side="left">{label}</TooltipContent>
    </Tooltip>
  )
}
