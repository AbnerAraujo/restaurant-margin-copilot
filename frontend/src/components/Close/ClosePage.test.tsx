import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import ClosePage from './ClosePage'

// ClosePage now calls useNavigate() (spec 008 FR-001, chart click-to-ask
// navigates to /ask) — every render needs a Router ancestor.
function renderPage() {
  return render(
    <MemoryRouter>
      <ClosePage />
    </MemoryRouter>,
  )
}

/** Two real `/api/reconciliation` days, with 2026-08-02 deliberately absent. */
const RECONCILIATION_RESPONSE = {
  start: '2026-08-01',
  end: '2026-08-03',
  days: [
    {
      date: '2026-08-01',
      gross_sales_by_source: {
        pos: '248.75',
        ifood: '69.50',
        just_eat_takeaway: '76.25',
      },
      total_delivery_gross_sales: '145.75',
      commissions: '31.24',
      refunds: '0.00',
      input_costs: '320.00',
      margin: '43.26',
      discrepancy_flags: [],
      source_row_refs: [
        { file: 'fixtures/pos_export.csv', row: 2 },
        { file: 'fixtures/pos_export.csv', row: 5 },
      ],
    },
    {
      date: '2026-08-03',
      gross_sales_by_source: {
        pos: '195.00',
        ifood: '55.25',
        just_eat_takeaway: '65.25',
      },
      total_delivery_gross_sales: '120.50',
      commissions: '25.85',
      refunds: '12.00',
      input_costs: '300.00',
      margin: '-120.26',
      discrepancy_flags: [
        { type: 'duplicate_order_removed', detail: 'order 4412 appeared twice' },
      ],
      source_row_refs: [{ file: 'fixtures/pos_export.csv', row: 9 }],
    },
  ],
}

function stubFetch(body: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({ ok: true, json: async () => body }),
  )
}

describe('ClosePage', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('fetches the real reconciliation endpoint rather than rendering hardcoded figures', async () => {
    stubFetch(RECONCILIATION_RESPONSE)
    renderPage()

    await screen.findByText('-$120.26')
    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/reconciliation'),
    )
  })

  it('summarises the LATEST reconciled day, with its own badge and provenance', async () => {
    stubFetch(RECONCILIATION_RESPONSE)
    renderPage()

    const summary = await screen.findByRole('region', {
      name: /latest reconciled day/i,
    })
    // 2026-08-03 is the latest served day, and it carries a flag, so it earns
    // a Discrepancy Catcher rather than a Clean Close.
    //
    // The date moved out of the summary card's caption sentence and into a
    // page-header chip, so it is asserted at page scope rather than inside
    // the region — it must still be on screen next to the figure it dates.
    expect(screen.getAllByText(/2026-08-03/).length).toBeGreaterThan(0)
    expect(within(summary).getByText('-$120.26')).toBeInTheDocument()
    expect(
      within(summary).getByText(/discrepancy catcher/i),
    ).toBeInTheDocument()
    expect(
      within(summary).getByRole('button', { name: /pos_export\.csv/ }),
    ).toBeInTheDocument()
  })

  it('draws a calendar day the backend omitted as an explicit gap, never a $0 bar', async () => {
    stubFetch(RECONCILIATION_RESPONSE)
    renderPage()

    await screen.findByRole('region', { name: /latest reconciled day/i })
    // 2026-08-02 is inside the served period but absent from `days`.
    expect(
      screen.getByRole('button', { name: /Aug 2: no data/i }),
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: /Aug 2:.*\$0\.00/ }),
    ).not.toBeInTheDocument()
  })

  it('reports a backend failure instead of an empty page that looks like "no data"', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        text: async () => 'query_failed',
      }),
    )
    renderPage()

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/couldn't load reconciled days/i)
    expect(alert).toHaveTextContent(/query_failed/)
  })

  it('says plainly when nothing has been ingested yet', async () => {
    stubFetch({ start: '', end: '', days: [] })
    renderPage()

    expect(
      await screen.findByText(/no reconciled days on file yet/i),
    ).toBeInTheDocument()
  })
})
