import { useRef, useState, type KeyboardEvent } from 'react'
import { FileSpreadsheet, FlaskConical } from 'lucide-react'

import ConnectedPlatformsTab from '@/components/Upload/ConnectedPlatformsTab'
import CostSheetTab from '@/components/Upload/CostSheetTab'
import { PageContainer, PageHeader } from '@/components/ui/page'
import { cn } from '@/lib/utils'

// ---------------------------------------------------------------------------
// `/upload` is now two ways to get data into the product, not one:
//
//   - the supplier cost sheet, uploaded as a CSV (specs/007-cost-sheet-upload)
//   - delivery revenue, pulled from the platform connectors
//     (specs/010-platform-connector-proxy) — both of which are SIMULATED
//
// One page rather than a new nav item because it is one job: get today's
// numbers into the close. Splitting "upload a file" and "pull from a
// platform" across two places in the sidebar would ask the owner to know
// which mechanism a given source uses before they can find it.
//
// The tab label carries the word "simulated" itself, so the disclosure is
// present before the tab is ever opened — the outermost of the four
// independent places this feature states it (see ConnectedPlatformsTab).
// ---------------------------------------------------------------------------

const TABS = [
  { id: 'cost-sheet', label: 'Supplier cost sheet', icon: FileSpreadsheet },
  { id: 'connectors', label: 'Connected platforms (simulated)', icon: FlaskConical },
] as const

type TabId = (typeof TABS)[number]['id']

/**
 * `/upload` — the two data-entry surfaces, as a tab strip.
 *
 * Both panels stay MOUNTED and the inactive one is hidden, rather than
 * unmounting on switch. A staged cost-sheet preview and a staged connector
 * preview both represent real work the owner has done and not yet
 * committed; unmounting would discard either one silently on a tab click,
 * which is exactly the loss `useUnsavedChangesGuard` exists to prevent on
 * navigation. `hidden` is also what the WAI-ARIA tabs pattern asks for on
 * an inactive tabpanel, so the accessible behaviour and the state-
 * preservation come from the same decision.
 */
export default function UploadPage() {
  const [active, setActive] = useState<TabId>('cost-sheet')
  const tabRefs = useRef<Record<string, HTMLButtonElement | null>>({})

  // Roving focus: a tab strip is one tab stop, and arrow keys move between
  // the tabs inside it (WAI-ARIA tabs pattern). Without this, every tab is
  // its own tab stop and the arrow keys do nothing.
  function handleTabKeyDown(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    let nextIndex: number | null = null
    if (event.key === 'ArrowRight') nextIndex = (index + 1) % TABS.length
    if (event.key === 'ArrowLeft') nextIndex = (index - 1 + TABS.length) % TABS.length
    if (event.key === 'Home') nextIndex = 0
    if (event.key === 'End') nextIndex = TABS.length - 1
    if (nextIndex === null) return

    event.preventDefault()
    const nextTab = TABS[nextIndex]
    setActive(nextTab.id)
    tabRefs.current[nextTab.id]?.focus()
  }

  return (
    <PageContainer className="flex flex-col gap-5">
      <PageHeader eyebrow="Data sources" title="Add data" />

      <div
        role="tablist"
        aria-label="Data source"
        className="flex flex-wrap gap-1 rounded-lg border border-border bg-muted/40 p-1"
      >
        {TABS.map((tab, index) => {
          const isActive = tab.id === active
          const Icon = tab.icon
          return (
            <button
              key={tab.id}
              ref={(node) => {
                tabRefs.current[tab.id] = node
              }}
              type="button"
              role="tab"
              id={`upload-tab-${tab.id}`}
              aria-selected={isActive}
              aria-controls={`upload-panel-${tab.id}`}
              tabIndex={isActive ? 0 : -1}
              onClick={() => setActive(tab.id)}
              onKeyDown={(event) => handleTabKeyDown(event, index)}
              className={cn(
                'inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition-colors',
                // Selection is carried by the surface AND by aria-selected,
                // never by colour alone.
                isActive
                  ? 'bg-card text-foreground shadow-sm'
                  : 'text-muted-foreground hover:bg-card/60 hover:text-foreground',
              )}
            >
              <Icon className="size-3.5" aria-hidden="true" />
              {tab.label}
            </button>
          )
        })}
      </div>

      <div
        role="tabpanel"
        id="upload-panel-cost-sheet"
        aria-labelledby="upload-tab-cost-sheet"
        hidden={active !== 'cost-sheet'}
      >
        <CostSheetTab />
      </div>
      <div
        role="tabpanel"
        id="upload-panel-connectors"
        aria-labelledby="upload-tab-connectors"
        hidden={active !== 'connectors'}
      >
        <ConnectedPlatformsTab />
      </div>
    </PageContainer>
  )
}
