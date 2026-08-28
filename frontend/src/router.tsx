import { createBrowserRouter, type RouteObject } from 'react-router-dom'

import AppShell from '@/components/Shell/AppShell'
import AskPage from '@/components/Ask/AskPage'
import ClosePage from '@/components/Close/ClosePage'
// Real, already-built capability-tile grid (a parallel agent's work) — not a
// stub, reused as-is per redesign-spec.md §3.
import HomePage from '@/components/Home/HomePage'
import PlatformsPage from '@/components/Platforms/PlatformsPage'
import PointsPage from '@/components/Points/PointsPage'
import PromotionsPage from '@/components/Promotions/PromotionsPage'

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
      { index: true, element: <HomePage /> },
      { path: 'close', element: <ClosePage /> },
      { path: 'ask', element: <AskPage /> },
      { path: 'promotions', element: <PromotionsPage /> },
      { path: 'platforms', element: <PlatformsPage /> },
      { path: 'points', element: <PointsPage /> },
    ],
  },
]

export const router = createBrowserRouter(routes)
