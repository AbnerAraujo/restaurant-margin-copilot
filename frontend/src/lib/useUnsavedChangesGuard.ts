import { useEffect } from 'react'
import { useBlocker } from 'react-router-dom'

/**
 * Reported live (QA pass): both `LogReplacementForm`'s "Log a replacement
 * campaign" fields and `UploadPage`'s staged-but-uncommitted CSV preview
 * silently discarded real in-progress work on any navigation away — an
 * in-app Back click, the sidebar, or a real browser Back/reload — with no
 * warning at all. On Upload specifically, the natural next step at the
 * preview step is to check `/close` to cross-reference the numbers before
 * committing, so losing the staged preview there was a real, repeatable
 * workflow cost, not just a hypothetical one.
 *
 * One hook covers both halves of "navigating away": `useBlocker` (this
 * app's router is built with `createBrowserRouter`, which is what makes
 * `useBlocker` available at all) intercepts in-app navigation so a
 * caller-rendered confirmation can run before the route actually changes,
 * and `beforeunload` covers the cases React Router never sees — a real tab
 * close, reload, or typed URL. Callers own the copy and the confirm UI
 * (see `ConfirmDialog`); this hook only owns the two subscriptions and
 * exposes the current blocked/idle state.
 *
 * `hasUnsavedChanges` is a plain boolean, not computed in here, on purpose:
 * "meaningful in-progress content" means something different on each form
 * (a touched field vs. a staged-but-uncommitted preview), and guessing at
 * that from inside a shared hook would either over-warn on a genuinely
 * untouched form or under-warn on a real one.
 */
export function useUnsavedChangesGuard(hasUnsavedChanges: boolean) {
  useEffect(() => {
    if (!hasUnsavedChanges) return

    function handleBeforeUnload(event: BeforeUnloadEvent) {
      // Chrome ignores a custom message and shows its own fixed copy
      // regardless — setting `returnValue` is still what triggers the
      // native prompt at all across every current engine.
      event.preventDefault()
      event.returnValue = ''
    }

    window.addEventListener('beforeunload', handleBeforeUnload)
    return () => window.removeEventListener('beforeunload', handleBeforeUnload)
  }, [hasUnsavedChanges])

  // Only blocks a navigation that actually leaves this page — a same-page
  // state change (e.g. a query-string update) never trips it.
  const blocker = useBlocker(
    ({ currentLocation, nextLocation }) =>
      hasUnsavedChanges && currentLocation.pathname !== nextLocation.pathname,
  )

  return {
    isBlocked: blocker.state === 'blocked',
    confirmDiscard: () => {
      if (blocker.state === 'blocked') blocker.proceed()
    },
    cancelDiscard: () => {
      if (blocker.state === 'blocked') blocker.reset()
    },
  }
}
