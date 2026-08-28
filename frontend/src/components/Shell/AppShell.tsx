import { useState } from 'react'
import { Outlet, useOutletContext } from 'react-router-dom'

import CostPanel, { type CostInteraction } from '@/components/CostPanel/CostPanel'
import Sidebar, { MobileNavBar } from '@/components/Shell/Sidebar'

/**
 * What a routed page can read/do with the shell-level running-cost total.
 * `CostPanel` is mounted once here (outside the router outlet, per
 * redesign-spec.md §1/§6) rather than per-page, so a page that logs new
 * interactions — today only `/ask`'s chat panel — reports them upward
 * through `useOutletContext` instead of each route owning its own cost
 * state and duplicating the pill.
 */
export interface ShellOutletContext {
  interactions: CostInteraction[]
  logInteractions: (newInteractions: CostInteraction[]) => void
}

/** Convenience hook for a routed page to read/report shell-level cost state. */
export function useShellOutletContext() {
  return useOutletContext<ShellOutletContext>()
}

/**
 * App shell: fixed left sidebar (desktop) / top icon bar (mobile) beside a
 * routed content area, with the session cost pill pinned at the shell root
 * so it stays visible across every route. Per redesign-spec.md §2.1 — the
 * root stays a row at `lg`+ (aside beside content) and stacks to a column
 * below it (mobile nav bar above `<main>`, not beside it).
 */
export default function AppShell() {
  const [interactions, setInteractions] = useState<CostInteraction[]>([])

  const logInteractions = (newInteractions: CostInteraction[]) => {
    setInteractions((previous) => [...previous, ...newInteractions])
  }

  return (
    <div className="flex min-h-screen flex-col bg-background lg:flex-row">
      <Sidebar />
      <div className="flex min-w-0 flex-1 flex-col">
        <MobileNavBar />
        <main className="flex-1 px-4 py-6 sm:px-6 lg:px-8">
          <Outlet context={{ interactions, logInteractions } satisfies ShellOutletContext} />
        </main>
      </div>
      <CostPanel interactions={interactions} />
    </div>
  )
}
