import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
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

  it('labels "Gross sales by source" rows with display names, never raw API keys', async () => {
    stubFetch(RECONCILIATION_RESPONSE)
    renderPage()

    const heading = await screen.findByRole('heading', {
      name: /gross sales by source/i,
    })
    const panel = heading.closest('div') ?? heading.parentElement
    if (!panel) throw new Error('Could not find the Gross sales by source panel')

    expect(within(panel).getByText('In-house POS')).toBeInTheDocument()
    expect(within(panel).getByText('iFood')).toBeInTheDocument()
    expect(within(panel).getByText('Just Eat Takeaway')).toBeInTheDocument()
    expect(within(panel).queryByText('pos')).not.toBeInTheDocument()
    expect(within(panel).queryByText('ifood')).not.toBeInTheDocument()
    expect(within(panel).queryByText('just_eat_takeaway')).not.toBeInTheDocument()
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

  // Reported live: "Latest" showed a "744-Day Margin Trend" bucketed into
  // weekly totals — the full multi-year history, not "the latest" at
  // day-level granularity — and weekly bucketing netted individual loss
  // days against a profitable week, so every bar rendered green even
  // though real days were in the red.
  it('scopes the "Latest" chart to a trailing 90-day window, not the full history (real day-level bars, loss days visible)', async () => {
    const totalDays = 150
    const days = Array.from({ length: totalDays }, (_, i) => {
      const date = new Date(Date.UTC(2026, 0, 1))
      date.setUTCDate(date.getUTCDate() + i)
      const iso = date.toISOString().slice(0, 10)
      // Every 10th day is a real loss — enough to prove loss days survive
      // scoping without asserting an exact count sensitive to bucketing.
      const isLossDay = i % 10 === 9
      return {
        date: iso,
        gross_sales_by_source: { pos: '100.00' },
        total_delivery_gross_sales: '0.00',
        commissions: '0.00',
        refunds: '0.00',
        input_costs: isLossDay ? '250.00' : '50.00',
        margin: isLossDay ? '-150.00' : '50.00',
        discrepancy_flags: [],
        source_row_refs: [{ file: 'fixtures/pos_export.csv', row: i + 2 }],
      }
    })
    stubFetch({ start: days[0].date, end: days[days.length - 1].date, days })
    renderPage()

    await screen.findByRole('region', { name: /latest reconciled day/i })

    // The heading reads "90-Day", not "150-Day" — the chart is genuinely
    // scoped, not just visually truncated.
    expect(
      screen.getByRole('heading', { name: /90-day margin trend/i }),
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('heading', { name: /150-day margin trend/i }),
    ).not.toBeInTheDocument()

    // At 90 real days, MarginTrendChart's own MAX_DISPLAY_BARS threshold
    // (120) never triggers weekly bucketing — every day, including a loss
    // day, renders as its own bar, so the "grouped into N-day totals"
    // caption must be absent and the range must start 89 days before the
    // last date (day index 60 of the 150 generated), not day index 0.
    expect(screen.queryByText(/grouped into/i)).not.toBeInTheDocument()
    const chartGroup = screen.getByRole('group', {
      name: /bar chart of daily reconciled margin/i,
    })
    // days[0] is 2026-01-01; day index 149 (the last) is 2026-05-30; the
    // trailing 90 days start at index 60 (149 - 89) = 2026-03-02.
    const expectedFirstDate = days[totalDays - 90].date
    expect(expectedFirstDate).toBe('2026-03-02')
    expect(chartGroup.getAttribute('aria-label')).toContain('Mar 2')
    expect(chartGroup.getAttribute('aria-label')).not.toContain('Jan 1')
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

  // Reported live: Period's two date fields each triggered their own
  // (debounced) fetch as soon as they changed — one for a range the owner
  // hadn't finished picking yet, then a second once the other field
  // changed too. The fix replaces auto-fetch with an explicit "Show
  // results" confirm; editing either field now fetches nothing at all.
  describe('Period view requires an explicit "Show results" click — no auto-fetch on date edits', () => {
    it('fetches nothing when Period\'s date fields change, only once Show results is clicked', async () => {
      stubFetch(RECONCILIATION_RESPONSE)
      renderPage()

      // The initial "latest" fetch happens immediately, same as before.
      await screen.findByText('-$120.26')
      expect(fetch).toHaveBeenCalledTimes(1)

      fireEvent.click(screen.getByRole('button', { name: 'Period' }))
      // Switching to Period pre-fills rangeStart/rangeEnd but must not fetch.
      expect(fetch).toHaveBeenCalledTimes(1)

      const fromInput = screen.getByLabelText('From')
      fireEvent.change(fromInput, { target: { value: '2026-08-01' } })
      expect(fetch).toHaveBeenCalledTimes(1)

      const toInput = screen.getByLabelText('To')
      fireEvent.change(toInput, { target: { value: '2026-08-03' } })
      expect(fetch).toHaveBeenCalledTimes(1)

      // A real, unmocked wait — long enough to have caught the old 500ms
      // debounce firing on its own. Still nothing.
      await new Promise((resolve) => setTimeout(resolve, 700))
      expect(fetch).toHaveBeenCalledTimes(1)

      fireEvent.click(screen.getByRole('button', { name: 'Show results' }))

      await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2))
      expect(fetch).toHaveBeenLastCalledWith(
        expect.stringContaining('start=2026-08-01'),
      )
      expect(fetch).toHaveBeenLastCalledWith(
        expect.stringContaining('end=2026-08-03'),
      )
    })

    it('shows a "choose dates" prompt rather than a loading skeleton for an unapplied Period range', async () => {
      stubFetch(RECONCILIATION_RESPONSE)
      renderPage()

      await screen.findByText('-$120.26')
      fireEvent.click(screen.getByRole('button', { name: 'Period' }))

      expect(
        await screen.findByText(/show results to load that period/i),
      ).toBeInTheDocument()
      // Never the generic loading skeleton — no request is in flight yet.
      expect(screen.queryByRole('status')).not.toBeInTheDocument()
    })

    it('shows the real loading state (not the "choose dates" prompt) once Show results is clicked', async () => {
      let resolvePeriodFetch!: (value: unknown) => void
      vi.stubGlobal(
        'fetch',
        vi
          .fn()
          .mockResolvedValueOnce({
            ok: true,
            json: async () => RECONCILIATION_RESPONSE,
          })
          .mockImplementationOnce(
            () =>
              new Promise((resolve) => {
                resolvePeriodFetch = resolve
              }),
          ),
      )
      renderPage()

      await screen.findByText('-$120.26')
      fireEvent.click(screen.getByRole('button', { name: 'Period' }))
      fireEvent.click(screen.getByRole('button', { name: 'Show results' }))

      expect(
        screen.queryByText(/show results to load that period/i),
      ).not.toBeInTheDocument()

      resolvePeriodFetch({ ok: true, json: async () => RECONCILIATION_RESPONSE })
      await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2))
    })

    it('disables Show results while a date field is empty', async () => {
      stubFetch(RECONCILIATION_RESPONSE)
      renderPage()

      await screen.findByText('-$120.26')
      fireEvent.click(screen.getByRole('button', { name: 'Period' }))

      const fromInput = screen.getByLabelText('From')
      fireEvent.change(fromInput, { target: { value: '' } })

      expect(screen.getByRole('button', { name: 'Show results' })).toBeDisabled()
    })
  })
})
