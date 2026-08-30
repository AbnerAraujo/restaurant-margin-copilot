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
        { file: 'data/live/pos_export.csv', row: 2 },
        { file: 'data/live/pos_export.csv', row: 5 },
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
      source_row_refs: [{ file: 'data/live/pos_export.csv', row: 9 }],
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

  // Reported live: a day with ~40 cross_source_duplicate_removed flags
  // (a POS-heavy connector sync) rendered its Discrepancy Catcher badge as
  // every flag's raw `detail` sentence joined with " · " — internal type
  // names, simulated:// provenance URIs, and row numbers, all in one
  // paragraph-length pill meant to carry one line of owner-facing context.
  describe('Discrepancy Catcher summarizes many flags in plain language, never the raw dump', () => {
    function dayWith(flags: { type: string; detail: string }[]) {
      return {
        start: '2026-08-30',
        end: '2026-08-30',
        days: [
          {
            date: '2026-08-30',
            gross_sales_by_source: { pos: '2901.46', ifood: '747.37', just_eat_takeaway: '747.34' },
            total_delivery_gross_sales: '1494.71',
            commissions: '304.97',
            refunds: '76.49',
            input_costs: '0.00',
            margin: '4014.71',
            discrepancy_flags: flags,
            source_row_refs: [],
          },
        ],
      }
    }

    it('groups many same-type flags into one counted phrase, never one line per flag', async () => {
      const manyDuplicates = Array.from({ length: 43 }, (_, i) => ({
        type: 'cross_source_duplicate_removed',
        detail: `POS ticket POS-SIM-${i} carries iFood's own order reference IFOOD-SIM-${i}, so it is the same order. simulated://pos-terminal-api/pos/v1/terminals/SIMULATED-TERMINAL-02/day-close?date=2026-08-30`,
      }))
      stubFetch(dayWith(manyDuplicates))
      renderPage()

      const summary = await screen.findByRole('region', { name: /latest reconciled day/i })
      expect(within(summary).getByText('43 duplicates counted once')).toBeInTheDocument()
      // Never the raw technical detail, and never a wall built from it.
      expect(within(summary).queryByText(/simulated:\/\//)).not.toBeInTheDocument()
      expect(within(summary).queryByText(/POS-SIM-0/)).not.toBeInTheDocument()
    })

    it('orders mixed flag types by owner-actionability and caps at two, folding the rest into a count', async () => {
      stubFetch(
        dayWith([
          ...Array.from({ length: 30 }, () => ({
            type: 'cross_source_duplicate_removed',
            detail: 'raw dedup detail, not shown',
          })),
          ...Array.from({ length: 5 }, () => ({
            type: 'cross_source_amount_mismatch',
            detail: 'raw mismatch detail, not shown',
          })),
          { type: 'anomaly_threshold_exceeded', detail: 'raw anomaly detail, not shown' },
          { type: 'pos_non_completed_row_excluded', detail: 'raw void detail, not shown' },
        ]),
      )
      renderPage()

      const summary = await screen.findByRole('region', { name: /latest reconciled day/i })
      // Amount mismatches and the anomaly outrank the already-resolved
      // duplicates and the void exclusion — the top two shown, two folded.
      expect(
        within(summary).getByText(
          '5 orders with a promotion-driven amount difference, an unusual change in revenue, and 2 more things flagged',
        ),
      ).toBeInTheDocument()
    })

    it('reports a single unresolved overlap in singular, ungrouped language', async () => {
      stubFetch(
        dayWith([{ type: 'cross_source_duplicate_unresolved', detail: 'raw, not shown' }]),
      )
      renderPage()

      const summary = await screen.findByRole('region', { name: /latest reconciled day/i })
      expect(
        within(summary).getByText('1 possible duplicate left unresolved'),
      ).toBeInTheDocument()
    })

    it('still counts a flag type this list does not name, rather than silently dropping it', async () => {
      stubFetch(
        dayWith([
          { type: 'some_future_flag_type', detail: 'raw, not shown' },
          { type: 'some_future_flag_type', detail: 'raw, not shown' },
        ]),
      )
      renderPage()

      const summary = await screen.findByRole('region', { name: /latest reconciled day/i })
      expect(within(summary).getByText('2 other items flagged')).toBeInTheDocument()
    })

    it('carries the same summarized text into the accessible label, not the raw dump', async () => {
      const manyDuplicates = Array.from({ length: 10 }, () => ({
        type: 'cross_source_duplicate_removed',
        detail: 'raw detail with a simulated:// URI, not shown',
      }))
      stubFetch(dayWith(manyDuplicates))
      renderPage()

      const summary = await screen.findByRole('region', { name: /latest reconciled day/i })
      const badge = within(summary).getByLabelText(/discrepancy catcher.*10 duplicates counted once/i)
      expect(badge).toBeInTheDocument()
    })
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

  // Bug fix: "Margin ... 2 sources" (distinct provenance FILES, via
  // ProvenanceTag) used to sit right beside "Gross sales ... 3 sources"
  // (distinct sales CHANNELS) — same word, same row, different denominators.
  // A day whose file count and channel count actually differ makes this
  // checkable: the two captions must use different, unambiguous nouns.
  it('labels margin\'s source-file count and gross sales\' channel count with different, unambiguous words', async () => {
    stubFetch({
      start: '2026-08-01',
      end: '2026-08-01',
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
          // Two DISTINCT files, deliberately fewer than the three gross-sales
          // channels above, so a matching count would be a coincidence a
          // test could miss — mismatched counts make the two captions'
          // independence checkable.
          source_row_refs: [
            { file: 'data/live/pos_export.csv', row: 2 },
            { file: 'data/live/ifood_export.csv', row: 5 },
          ],
        },
      ],
    })
    renderPage()

    const summary = await screen.findByRole('region', {
      name: /latest reconciled day/i,
    })

    expect(
      within(summary).getByRole('button', { name: '2 source files' }),
    ).toBeInTheDocument()
    expect(within(summary).getByText('3 channels')).toBeInTheDocument()
    // Neither caption should render the bare, ambiguous "sources" word.
    expect(within(summary).queryByText(/^\d+ sources$/)).not.toBeInTheDocument()
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
        source_row_refs: [{ file: 'data/live/pos_export.csv', row: i + 2 }],
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
        text: async () =>
          JSON.stringify({
            error: 'query_failed',
            detail: 'ERROR: canceling statement due to user request (SQLSTATE 57014)',
          }),
      }),
    )
    renderPage()

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/couldn't load reconciled days/i)
  })

  it('gives a failed load a next step instead of a raw Postgres error', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        text: async () =>
          JSON.stringify({
            error: 'query_failed',
            detail: 'ERROR: canceling statement due to user request (SQLSTATE 57014)',
          }),
      }),
    )
    renderPage()

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/reload this page/i)
    expect(alert).not.toHaveTextContent(/SQLSTATE/i)
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
  // changed too. The fix replaces per-field auto-fetch with an explicit
  // "Show results" confirm; editing either field fetches nothing at all.
  //
  // Landing ON Period is a separate concern from EDITING while already
  // there: switching into the view auto-applies whatever range is
  // currently set (seeding "last week of real data" the first time) so
  // the owner never faces a blank state they have to click through —
  // that's the one exception to "Period never fetches without a click."
  describe('Period view always shows results for its current range, but requires an explicit "Show results" click to apply an edit', () => {
    it('auto-applies the seeded default range on entering Period, then fetches nothing on date edits until Show results is clicked', async () => {
      stubFetch(RECONCILIATION_RESPONSE)
      renderPage()

      // The initial "latest" fetch happens immediately, same as before.
      await screen.findByText('-$120.26')
      expect(fetch).toHaveBeenCalledTimes(1)

      fireEvent.click(screen.getByRole('button', { name: 'Period' }))
      // Switching to Period seeds rangeStart/rangeEnd from the real data's
      // own bounds (2026-08-01..03) and immediately applies that range —
      // no click required for the range the app already chose.
      await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2))
      expect(fetch).toHaveBeenLastCalledWith(
        expect.stringContaining('start=2026-08-01'),
      )
      expect(fetch).toHaveBeenLastCalledWith(
        expect.stringContaining('end=2026-08-03'),
      )

      const fromInput = screen.getByLabelText('From')
      fireEvent.change(fromInput, { target: { value: '2026-08-02' } })
      expect(fetch).toHaveBeenCalledTimes(2)

      const toInput = screen.getByLabelText('To')
      fireEvent.change(toInput, { target: { value: '2026-08-03' } })
      expect(fetch).toHaveBeenCalledTimes(2)

      // A real, unmocked wait — long enough to have caught the old 500ms
      // debounce firing on its own. Still nothing.
      await new Promise((resolve) => setTimeout(resolve, 700))
      expect(fetch).toHaveBeenCalledTimes(2)

      fireEvent.click(screen.getByRole('button', { name: 'Show results' }))

      await waitFor(() => expect(fetch).toHaveBeenCalledTimes(3))
      expect(fetch).toHaveBeenLastCalledWith(
        expect.stringContaining('start=2026-08-02'),
      )
      expect(fetch).toHaveBeenLastCalledWith(
        expect.stringContaining('end=2026-08-03'),
      )
    })

    it('shows a "choose dates" prompt once the owner clears a field while already in Period view', async () => {
      stubFetch(RECONCILIATION_RESPONSE)
      renderPage()

      await screen.findByText('-$120.26')
      fireEvent.click(screen.getByRole('button', { name: 'Period' }))
      // The auto-applied default range resolves first.
      await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2))

      const fromInput = screen.getByLabelText('From')
      fireEvent.change(fromInput, { target: { value: '' } })

      expect(
        await screen.findByText(/show results to load that period/i),
      ).toBeInTheDocument()
      // Never the generic loading skeleton — no request is in flight.
      expect(screen.queryByRole('status')).not.toBeInTheDocument()
    })

    it('shows the real loading state (not the "choose dates" prompt) once Show results is clicked after an edit', async () => {
      let resolveEditFetch!: (value: unknown) => void
      vi.stubGlobal(
        'fetch',
        vi
          .fn()
          .mockResolvedValueOnce({
            ok: true,
            json: async () => RECONCILIATION_RESPONSE,
          }) // initial "latest" fetch
          .mockResolvedValueOnce({
            ok: true,
            json: async () => RECONCILIATION_RESPONSE,
          }) // Period's auto-applied default range
          .mockImplementationOnce(
            () =>
              new Promise((resolve) => {
                resolveEditFetch = resolve
              }),
          ),
      )
      renderPage()

      await screen.findByText('-$120.26')
      fireEvent.click(screen.getByRole('button', { name: 'Period' }))
      await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2))

      const fromInput = screen.getByLabelText('From')
      fireEvent.change(fromInput, { target: { value: '2026-08-02' } })
      fireEvent.click(screen.getByRole('button', { name: 'Show results' }))

      expect(
        screen.queryByText(/show results to load that period/i),
      ).not.toBeInTheDocument()

      resolveEditFetch({ ok: true, json: async () => RECONCILIATION_RESPONSE })
      await waitFor(() => expect(fetch).toHaveBeenCalledTimes(3))
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

    // QA-round-5 finding: this row (From label, From date, To label, To
    // date, Show results) had no flex-wrap, so at 375px it overflowed
    // <main> horizontally instead of breaking onto a second line — pushing
    // Show results off-canvas entirely, reachable only by the browser's own
    // focus-follows-scroll behavior, never by anything a touch user could
    // see or tap (see AppShell.tsx's overflow-x-hidden doc comment for the
    // full mechanism). jsdom has no real layout engine, so this asserts the
    // fix's actual contract — the row can break onto multiple lines — via
    // its className, rather than a pixel measurement jsdom cannot produce;
    // the fix was verified visually with real Playwright screenshots at
    // 375px/768px.
    it('lets the From/To/Show-results row wrap onto multiple lines instead of overflowing', async () => {
      stubFetch(RECONCILIATION_RESPONSE)
      renderPage()

      await screen.findByText('-$120.26')
      fireEvent.click(screen.getByRole('button', { name: 'Period' }))

      const fromInput = screen.getByLabelText('From')
      const row = fromInput.closest('div')
      expect(row).not.toBeNull()
      expect(row).toHaveClass('flex-wrap')
    })
  })
})
