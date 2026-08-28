import type { ReactNode } from 'react'
import { createBrowserRouter, type RouteObject } from 'react-router-dom'

import AppShell from '@/components/Shell/AppShell'
import AskPage from '@/components/Ask/AskPage'
import ClosePage from '@/components/Close/ClosePage'
import ErrorBoundary from '@/components/ErrorBoundary'
// Real, already-built capability-tile grid (a parallel agent's work) — not a
// stub, reused as-is per redesign-spec.md §3.
import HomePage from '@/components/Home/HomePage'
import PlatformsPage from '@/components/Platforms/PlatformsPage'
import PointsPage from '@/components/Points/PointsPage'
import PromotionsPage from '@/components/Promotions/PromotionsPage'
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
    element: <AppShell />,
    children: [
      { index: true, element: withBoundary('Home', <HomePage />) },
      { path: 'close', element: withBoundary('Today’s Close', <ClosePage />) },
      { path: 'upload', element: withBoundary('Upload cost sheet', <UploadPage />) },
      { path: 'ask', element: withBoundary('Ask', <AskPage />) },
      { path: 'promotions', element: withBoundary('Promotions', <PromotionsPage />) },
      { path: 'platforms', element: withBoundary('Platforms', <PlatformsPage />) },
      { path: 'points', element: withBoundary('Points', <PointsPage />) },
    ],
  },
]

export const router = createBrowserRouter(routes)
