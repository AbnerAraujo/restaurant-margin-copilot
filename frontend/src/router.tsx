import type { ReactNode } from 'react'
import { createBrowserRouter, type RouteObject } from 'react-router-dom'

import AppShell from '@/components/Shell/AppShell'
import AskPage from '@/components/Ask/AskPage'
import ClosePage from '@/components/Close/ClosePage'
import ErrorBoundary, { RouteErrorBoundary } from '@/components/ErrorBoundary'
import HelpPage from '@/components/Help/HelpPage'
// Real, already-built capability-tile grid (a parallel agent's work) — not a
// stub, reused as-is per redesign-spec.md §3.
import HomePage from '@/components/Home/HomePage'
import { clearThreadStorage } from '@/lib/chatStorage'
import NotFoundPage from '@/components/NotFound/NotFoundPage'
import PlatformsPage from '@/components/Platforms/PlatformsPage'
import PointsPage from '@/components/Points/PointsPage'
import ProfilePage from '@/components/Profile/ProfilePage'
import PromotionsPage from '@/components/Promotions/PromotionsPage'
import SettingsPage from '@/components/Settings/SettingsPage'
import UploadPage from '@/components/Upload/UploadPage'

/**
 * Each page gets its OWN error boundary rather than one wrapping the whole
 * shell: a crash in one route (the exact class of bug that motivated
 * building this — a stale localStorage shape breaking the chat renderer)
 * should not force a reload of the sidebar and every other working page,
 * and the report is more useful when it names the specific page that broke
 * rather than "somewhere in the app".
 */
function withBoundary(name: string, element: ReactNode) {
  return <ErrorBoundary component={name}>{element}</ErrorBoundary>
}

/**
 * Route table per redesign-spec.md §1. Exported separately from the
 * `createBrowserRouter` instance so tests can build a `createMemoryRouter`
 * from the same route objects instead of depending on real browser history.
 */
export const routes: RouteObject[] = [
  {
    path: '/',
    // The shell itself (sidebar, mobile nav bar, the pinned CostPanel) sits
    // outside every per-page boundary above, so a crash in the chrome —
    // not any one routed page — used to take down the whole app with
    // React's default blank-screen behaviour and no `/api/client-errors`
    // report. Wrapped the same way every page already is.
    element: (
      <ErrorBoundary component="App shell">
        <AppShell />
      </ErrorBoundary>
    ),
    // Covers the other gap: a failed loader or a malformed route object
    // never reaches `<AppShell />`'s render at all, so the boundary above
    // can't catch it — only `errorElement` sees a router-level failure.
    errorElement: <RouteErrorBoundary component="App shell" />,
    children: [
      { index: true, element: withBoundary('Home', <HomePage />) },
      { path: 'close', element: withBoundary('Today’s Close', <ClosePage />) },
      { path: 'upload', element: withBoundary('Upload cost sheet', <UploadPage />) },
      {
        path: 'ask',
        // Direct wiring rather than `withBoundary`, which has no `onReset`
        // parameter: this is the one page persisting anything a crash could
        // itself have poisoned (see chatStorage.ts's doc comment for the
        // real incident), so Reset here must also clear that thread history
        // before re-rendering, or it reruns straight into the same crash.
        element: (
          <ErrorBoundary component="Ask" onReset={clearThreadStorage}>
            <AskPage />
          </ErrorBoundary>
        ),
      },
      { path: 'promotions', element: withBoundary('Promotions', <PromotionsPage />) },
      { path: 'platforms', element: withBoundary('Platforms', <PlatformsPage />) },
      { path: 'points', element: withBoundary('Points', <PointsPage />) },
      { path: 'profile', element: withBoundary('Profile', <ProfilePage />) },
      { path: 'settings', element: withBoundary('Settings', <SettingsPage />) },
      { path: 'help', element: withBoundary('Help', <HelpPage />) },
      // Catch-all: any URL that doesn't match one of the routes above (a
      // typo, a stale bookmark) renders NotFoundPage instead of falling
      // through to the root `errorElement` above — see that component's doc
      // comment. Must stay last: React Router matches route order, and a
      // bare `*` ahead of a real path would shadow it.
      { path: '*', element: <NotFoundPage /> },
    ],
  },
]

export const router = createBrowserRouter(routes)
