import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import UploadPage from './UploadPage'

// The tab shell itself. The two flows inside it have their own test files
// (CostSheetTab.test.tsx, ConnectedPlatformsTab.test.tsx); what is asserted
// here is that the shell keeps its promises: the simulation is disclosed in
// the tab label before the tab is ever opened, and switching tabs does not
// throw away work staged in the other one.

const PLATFORMS_RESPONSE = {
  simulated: true,
  notice: 'Emulated connection.',
  platforms: [
    {
      platform: 'ifood',
      name: 'iFood',
      simulated: true,
      wire_format: 'page-numbered JSON',
      commission_rate_pct: '23.00',
      endpoint: 'simulated://ifood-partner-api/v2/orders',
    },
  ],
}

const COST_SHEET_PREVIEW = {
  row_count: 1,
  total_amount: '100.00',
  rows: [
    {
      invoice_id: 'INV-TEST-001',
      invoice_date: '2026-08-01',
      supplier: 'Test Produce Co.',
      category: 'produce',
      amount: '100.00',
      notes: '',
      source_row_ref: { file: 'cost_sheet.csv', row: 2 },
    },
  ],
}

function stubFetchByPath(routes: Record<string, unknown>) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      const match = Object.keys(routes).find((path) => url.includes(path))
      if (!match) throw new Error(`unstubbed fetch: ${url}`)
      return {
        ok: true,
        status: 200,
        json: async () => routes[match],
        text: async () => JSON.stringify(routes[match]),
      }
    }),
  )
}

/** CostSheetTab calls `useUnsavedChangesGuard`, which needs a data router. */
function renderPage() {
  const router = createMemoryRouter(
    [
      { path: '/upload', element: <UploadPage /> },
      { path: '/close', element: <p>Today&apos;s Close</p> },
    ],
    { initialEntries: ['/upload'] },
  )
  render(<RouterProvider router={router} />)
}

describe('UploadPage', () => {
  beforeEach(() => {
    stubFetchByPath({
      '/api/connectors/platforms': PLATFORMS_RESPONSE,
      '/api/ingest/cost-sheet/preview': COST_SHEET_PREVIEW,
    })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('discloses the simulation in the tab label itself, before the tab is opened', () => {
    renderPage()

    const connectorTab = screen.getByRole('tab', { name: /connected platforms \(simulated\)/i })
    expect(connectorTab).toHaveAttribute('aria-selected', 'false')
    // The word is present while the panel is still closed.
    expect(connectorTab).toHaveTextContent(/simulated/i)
  })

  it('opens the cost sheet first — the tab that needs no disclaimer', () => {
    renderPage()

    expect(screen.getByRole('tab', { name: /supplier cost sheet/i })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    expect(screen.getByRole('tabpanel', { name: /supplier cost sheet/i })).toBeVisible()
  })

  it('switches panels on click, hiding rather than discarding the other one', async () => {
    const user = userEvent.setup()
    renderPage()

    await user.click(screen.getByRole('tab', { name: /connected platforms \(simulated\)/i }))

    expect(screen.getByRole('tab', { name: /connected platforms \(simulated\)/i })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    expect(await screen.findByText(/these connections are simulated/i)).toBeVisible()
  })

  it('keeps a staged cost-sheet preview alive across a tab switch', async () => {
    const user = userEvent.setup()
    renderPage()

    // Stage real work in the first tab...
    await user.upload(
      screen.getByLabelText(/choose a supplier cost sheet csv file/i),
      new File(['invoice_id,invoice_date,supplier,category,amount,notes\n'], 'cost_sheet.csv', {
        type: 'text/csv',
      }),
    )
    expect(await screen.findByText('INV-TEST-001')).toBeInTheDocument()

    // ...leave and come back. Unmounting the panel would silently discard
    // an uncommitted preview on a single tab click — the exact loss the
    // unsaved-changes guard exists to prevent on navigation.
    await user.click(screen.getByRole('tab', { name: /connected platforms \(simulated\)/i }))
    await user.click(screen.getByRole('tab', { name: /supplier cost sheet/i }))

    expect(screen.getByText('INV-TEST-001')).toBeInTheDocument()
  })

  it('moves between tabs with the arrow keys, as one tab stop', async () => {
    const user = userEvent.setup()
    renderPage()

    const costSheetTab = screen.getByRole('tab', { name: /supplier cost sheet/i })
    costSheetTab.focus()
    await user.keyboard('{ArrowRight}')

    expect(screen.getByRole('tab', { name: /connected platforms \(simulated\)/i })).toHaveAttribute(
      'aria-selected',
      'true',
    )
  })
})
