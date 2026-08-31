import { useEffect, useRef, useState } from 'react'

// Tolerates the sub-pixel rounding a browser's own scroll math can leave a
// fraction of a pixel short of "fully scrolled" — without this, the fade
// can flicker back on at the exact right edge on some zoom levels/displays.
const SCROLL_EDGE_SLACK_PX = 1

/**
 * Shared scroll-fade affordance for any horizontally-scrollable strip this
 * app renders narrower than its content — first built for `MobileNavBar`
 * (Sidebar.tsx), then found needed again for column-filterable tables: a
 * plain `overflow-x-auto` wrapper handles the overflow without visually
 * breaking the layout, but nothing about it tells a first-time visitor
 * there IS more content to the right. Found live at 375px: the Recent
 * Closes table's own Margin column (with its column-header filter) sat up
 * to 139px off-screen with no scrollbar hint, no fade, nothing — the table
 * looked complete with two columns, and a shipped filter was entirely
 * unreachable.
 *
 * Returns a ref to attach to the scrolling element and `canScrollRight`,
 * true only while there is real unscrolled content past the right edge —
 * never a permanent decoration that would misleadingly persist after the
 * strip is fully scrolled, or appear at all on a strip that never
 * overflows in the first place.
 */
export function useHorizontalScrollFade<T extends HTMLElement>() {
  const ref = useRef<T>(null)
  const [canScrollRight, setCanScrollRight] = useState(false)

  useEffect(() => {
    const el = ref.current
    if (!el) return

    const updateFadeVisibility = () => {
      setCanScrollRight(el.scrollLeft + el.clientWidth < el.scrollWidth - SCROLL_EDGE_SLACK_PX)
    }

    updateFadeVisibility()
    el.addEventListener('scroll', updateFadeVisibility, { passive: true })
    // ResizeObserver, not just a mount-time check: rotating the device,
    // resizing a desktop window down into this breakpoint, or content
    // loading in after this first paints can all change whether the strip
    // overflows at all, without ever firing a 'scroll' event.
    const observer = new ResizeObserver(updateFadeVisibility)
    observer.observe(el)

    return () => {
      el.removeEventListener('scroll', updateFadeVisibility)
      observer.disconnect()
    }
  }, [])

  return { ref, canScrollRight }
}
