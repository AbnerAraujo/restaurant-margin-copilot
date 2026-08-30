import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import ConnectedPlatformsTab from './ConnectedPlatformsTab'

// specs/010-platform-connector-proxy and specs/012-pos-connector-dedup. The
// assertions that matter most here are not about the sync working — they are
// about the simulation being impossible to miss, and about deduplication
// being honest in both directions. A connector tab that silently looked like
// a real iFood integration, or that reported its removals while staying quiet
// about the overlaps it could not resolve, would be the single most damaging
// thing this product could ship, given that its whole claim is that it
// refuses rather than misleads.

const PLATFORMS_RESPONSE = {
  simulated: true,
  notice:
    'Emulated connection. No real iFood account, Just Eat Takeaway account or POS terminal is connected — these orders are generated locally for demonstration.',
  platforms: [
    {
      platform: 'ifood',
      name: 'iFood',
      simulated: true,
      wire_format: 'page-numbered JSON, snake_case, amounts as decimal strings',
      commission_rate_pct: '23.00',
      endpoint: 'simulated://ifood-partner-api/v2/merchants/SIMULATED-MERCHANT-0417/orders',
    },
    {
      platform: 'just_eat_takeaway',
      name: 'Just Eat Takeaway',
      simulated: true,
      wire_format: 'cursor-paginated JSON, camelCase, amounts as integer minor units',
      commission_rate_pct: '20.00',
      endpoint: 'simulated://just-eat-takeaway-partner-api/partner/orders',
    },
    {
      platform: 'pos',
      name: 'POS',
      simulated: true,
      wire_format: 'newline-delimited JSON with no envelope, pt-BR amounts, zone-less timestamps',
      // Empty on purpose: the POS charges no commission at all, and a
      // "0.00%" chip would read as a platform that happens to be free.
      commission_rate_pct: '',
      endpoint: 'simulated://pos-terminal-api/pos/v1/terminals/SIMULATED-TERMINAL-02/day-close',
    },
  ],
}

const PREVIEW_RESPONSE = {
  simulated: true,
  notice: PLATFORMS_RESPONSE.notice,
  from: '2026-08-18',
  to: '2026-08-18',
  order_count: 96,
  gross_sales: '3120.50',
  refunds: '0.00',
  commissions: '281.75',
  duplicates_removed: 12,
  unresolved_overlaps: 2,
  dedup: [
    {
      kind: 'matched_by_reference',
      resolved: true,
      date: '2026-08-18',
      platform: 'ifood',
      pos_order_id: 'POS-SIM-20260818-0017',
      platform_order_id: 'IFOOD-SIM-20260818-0009',
      detail: 'POS ticket POS-SIM-20260818-0017 carries iFood’s own order reference.',
    },
    {
      kind: 'unresolved_ambiguous',
      resolved: false,
      date: '2026-08-18',
      platform: 'ifood',
      pos_order_id: 'POS-SIM-20260818-0026',
      detail: 'Two iFood orders match POS ticket POS-SIM-20260818-0026 equally well.',
    },
    {
      kind: 'unresolved_no_counterpart',
      resolved: false,
      date: '2026-08-18',
      platform: 'ifood',
      pos_order_id: 'POS-SIM-20260818-0057',
      detail: 'No iFood order for 2026-08-18 matches POS ticket POS-SIM-20260818-0057.',
    },
  ],
  days: [
    {
      platform: 'ifood',
      platform_name: 'iFood',
      date: '2026-08-18',
      order_count: 22,
      refund_count: 0,
      gross_sales: '705.25',
      refunds: '0.00',
      commissions: '162.21',
      duplicates_removed: 0,
      unresolved_overlaps: 0,
    },
    {
      platform: 'just_eat_takeaway',
      platform_name: 'Just Eat Takeaway',
      date: '2026-08-18',
      order_count: 19,
      refund_count: 1,
      gross_sales: '605.25',
      refunds: '42.00',
      commissions: '119.54',
      duplicates_removed: 0,
      unresolved_overlaps: 0,
    },
    {
      platform: 'pos',
      platform_name: 'POS',
      date: '2026-08-18',
      order_count: 55,
      refund_count: 0,
      gross_sales: '1810.00',
      refunds: '0.00',
      commissions: '0.00',
      duplicates_removed: 12,
      unresolved_overlaps: 2,
    },
  ],
}

/** Routes each stubbed response by URL, so mount-time and click-time fetches
 * can't consume each other's turn the way a `mockResolvedValueOnce` queue
 * would. */
function stubFetchByPath(routes: Record<string, { ok?: boolean; status?: number; body: unknown }>) {
  const mockFetch = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    const match = Object.keys(routes).find((path) => url.includes(path))
    if (!match) throw new Error(`unstubbed fetch: ${url}`)
    const route = routes[match]
    const ok = route.ok ?? true
    return {
      ok,
      status: route.status ?? (ok ? 200 : 422),
      json: async () => route.body,
      text: async () => JSON.stringify(route.body),
    }
  })
  vi.stubGlobal('fetch', mockFetch)
  return mockFetch
}

describe('ConnectedPlatformsTab', () => {
  beforeEach(() => {
    stubFetchByPath({
      '/api/connectors/platforms': { body: PLATFORMS_RESPONSE },
      '/api/connectors/sync/preview': { body: PREVIEW_RESPONSE },
    })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('states that the connections are simulated before showing any figure, with no way to dismiss it', () => {
    render(<ConnectedPlatformsTab />)

    const notice = screen.getByRole('note')
    expect(notice).toHaveTextContent(/these connections are simulated/i)
    expect(notice).toHaveTextContent(
      /no real ifood account, just eat takeaway account or pos terminal is connected/i,
    )

    // The notice must come first in document order — before the controls
    // and before anything that could render a number.
    const body = document.body.textContent ?? ''
    expect(body.indexOf('These connections are simulated')).toBeGreaterThanOrEqual(0)
    expect(body.indexOf('These connections are simulated')).toBeLessThan(body.indexOf('From'))

    // Nothing in the notice offers a way to close it.
    expect(within(notice).queryByRole('button')).toBeNull()
  })

  it('labels every source as simulated individually, not only in the banner', async () => {
    render(<ConnectedPlatformsTab />)

    // One per source, so a screenshot cropped past the banner still
    // discloses — and the POS is held to exactly the same bar as the two
    // delivery platforms, not treated as the boring one.
    const markers = await screen.findAllByText(/simulated connection/i)
    expect(markers).toHaveLength(3)

    expect(await screen.findByText('iFood')).toBeInTheDocument()
    expect(await screen.findByText('Just Eat Takeaway')).toBeInTheDocument()
    expect(await screen.findByText('POS')).toBeInTheDocument()

    // The POS charges no commission, and says so rather than showing a
    // "0.00%" that would read as a platform that happens to be free.
    expect(await screen.findByText('No commission')).toBeInTheDocument()
  })

  it('names each platform’s wire format, so the normalization is visible in the product', async () => {
    render(<ConnectedPlatformsTab />)

    expect(await screen.findByText(/page-numbered JSON, snake_case/)).toBeInTheDocument()
    expect(await screen.findByText(/cursor-paginated JSON, camelCase/)).toBeInTheDocument()
    // The provenance scheme is shown too — it is what a synced row carries.
    expect(await screen.findByText(/simulated:\/\/ifood-partner-api/)).toBeInTheDocument()
  })

  it('previews per-platform totals and says plainly that nothing has been saved', async () => {
    const user = userEvent.setup()
    render(<ConnectedPlatformsTab />)

    await user.click(screen.getByRole('button', { name: /preview orders/i }))

    expect(await screen.findByText(/nothing has been saved yet/i)).toBeInTheDocument()
    expect(screen.getByText(/96 simulated orders/i)).toBeInTheDocument()
    // Every source's row is present in the table.
    expect(await screen.findAllByText('iFood')).not.toHaveLength(0)
    expect(screen.getByText('22')).toBeInTheDocument()
    expect(screen.getByText('19')).toBeInTheDocument()
    expect(screen.getByText('55')).toBeInTheDocument()

    // The confirm button restates the action AND the object, and keeps the
    // qualifier: it is the last thing read before the numbers change.
    expect(screen.getByRole('button', { name: /sync simulated orders/i })).toBeEnabled()
  })

  // specs/012-pos-connector-dedup FR-014/US3. The removals alone would be
  // the flattering half of the story; the overlaps the backend declined to
  // resolve are the half that changes what the owner should trust, and they
  // have to be on screen beside the numbers they affect.
  it('reports both what deduplication resolved and what it could not', async () => {
    const user = userEvent.setup()
    render(<ConnectedPlatformsTab />)

    await user.click(screen.getByRole('button', { name: /preview orders/i }))

    expect(await screen.findByText(/12 POS tickets matched an order/i)).toBeInTheDocument()
    expect(screen.getByText(/counted once rather than twice/i)).toBeInTheDocument()

    // The unresolved ones are a warning, in their own region, and they say
    // what the consequence is rather than only that something happened.
    const warning = screen.getByText(/2 POS tickets look/i)
    expect(warning).toBeInTheDocument()
    expect(warning).toHaveTextContent(/left in rather than guessed at/i)
    expect(warning).toHaveTextContent(/may count those orders twice/i)

    // And each unresolved overlap is itemized, so the owner knows which
    // tickets to check rather than only how many there were.
    expect(
      screen.getByText(/Two iFood orders match POS ticket POS-SIM-20260818-0026 equally well/i),
    ).toBeInTheDocument()
    expect(
      screen.getByText(/No iFood order for 2026-08-18 matches POS ticket POS-SIM-20260818-0057/i),
    ).toBeInTheDocument()

    // The per-day table carries the same facts on the row they belong to.
    expect(screen.getByText('12 removed, 2 unresolved')).toBeInTheDocument()
  })

  it('refuses to make an empty range a one-click sync', async () => {
    stubFetchByPath({
      '/api/connectors/platforms': { body: PLATFORMS_RESPONSE },
      '/api/connectors/sync/preview': {
        body: { ...PREVIEW_RESPONSE, order_count: 0, days: [], gross_sales: '0.00' },
      },
    })
    const user = userEvent.setup()
    render(<ConnectedPlatformsTab />)

    await user.click(screen.getByRole('button', { name: /preview orders/i }))

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/reported no orders/i)
    // Syncing an empty range would CLEAR the delivery revenue on file for
    // those days, so the button must not be available.
    expect(screen.getByRole('button', { name: /sync simulated orders/i })).toBeDisabled()
  })

  it('surfaces the backend’s own specific refusal, never a generic failure', async () => {
    stubFetchByPath({
      '/api/connectors/platforms': { body: PLATFORMS_RESPONSE },
      '/api/connectors/sync/preview': {
        ok: false,
        body: {
          error: 'connector_fetch_failed',
          detail:
            'platformconnector: date range 2026-01-01..2026-06-30 covers 181 days, more than the 31-day limit on a single sync — sync a shorter range',
        },
      },
    })
    const user = userEvent.setup()
    render(<ConnectedPlatformsTab />)

    await user.click(screen.getByRole('button', { name: /preview orders/i }))

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/31-day limit/i)
    expect(alert).not.toHaveTextContent(/something went wrong/i)
  })
})
