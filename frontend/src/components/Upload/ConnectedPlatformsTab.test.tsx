import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import ConnectedPlatformsTab from './ConnectedPlatformsTab'

// specs/010-platform-connector-proxy. The assertions that matter most here
// are not about the sync working — they are about the simulation being
// impossible to miss. A connector tab that silently looked like a real
// iFood integration would be the single most damaging thing this product
// could ship, given that its whole claim is that it refuses rather than
// misleads.

const PLATFORMS_RESPONSE = {
  simulated: true,
  notice:
    'Emulated connection. No real iFood or Just Eat Takeaway account is connected — these orders are generated locally for demonstration.',
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
  ],
}

const PREVIEW_RESPONSE = {
  simulated: true,
  notice: PLATFORMS_RESPONSE.notice,
  from: '2026-08-18',
  to: '2026-08-18',
  order_count: 41,
  gross_sales: '1310.50',
  refunds: '0.00',
  commissions: '281.75',
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
    expect(notice).toHaveTextContent(/no real ifood or just eat takeaway account is connected/i)

    // The notice must come first in document order — before the controls
    // and before anything that could render a number.
    const body = document.body.textContent ?? ''
    expect(body.indexOf('These connections are simulated')).toBeGreaterThanOrEqual(0)
    expect(body.indexOf('These connections are simulated')).toBeLessThan(body.indexOf('From'))

    // Nothing in the notice offers a way to close it.
    expect(within(notice).queryByRole('button')).toBeNull()
  })

  it('labels every platform as simulated individually, not only in the banner', async () => {
    render(<ConnectedPlatformsTab />)

    // One per platform, so a screenshot cropped past the banner still
    // discloses.
    const markers = await screen.findAllByText(/simulated connection/i)
    expect(markers).toHaveLength(2)

    expect(await screen.findByText('iFood')).toBeInTheDocument()
    expect(await screen.findByText('Just Eat Takeaway')).toBeInTheDocument()
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
    expect(screen.getByText(/41 simulated orders/i)).toBeInTheDocument()
    // Both platforms' rows are present in the table.
    expect(await screen.findAllByText('iFood')).not.toHaveLength(0)
    expect(screen.getByText('22')).toBeInTheDocument()
    expect(screen.getByText('19')).toBeInTheDocument()

    // The confirm button restates the action AND the object, and keeps the
    // qualifier: it is the last thing read before the numbers change.
    expect(screen.getByRole('button', { name: /sync simulated orders/i })).toBeEnabled()
  })

  it('a Platform column filter narrows the preview to just that connector\'s day rows', async () => {
    const user = userEvent.setup()
    render(<ConnectedPlatformsTab />)
    await user.click(screen.getByRole('button', { name: /preview orders/i }))
    await screen.findByText(/nothing has been saved yet/i)

    await user.click(screen.getByRole('button', { name: /filter by platform/i }))
    await user.click(await screen.findByRole('checkbox', { name: 'Just Eat Takeaway' }))

    expect(screen.getByText('19')).toBeInTheDocument()
    expect(screen.queryByText('22')).not.toBeInTheDocument()
    expect(screen.getByText('1 of 2 shown')).toBeInTheDocument()
  })

  it('an Orders numeric range filter stages the typed bound and only applies once submitted', async () => {
    const user = userEvent.setup()
    render(<ConnectedPlatformsTab />)
    await user.click(screen.getByRole('button', { name: /preview orders/i }))
    await screen.findByText(/nothing has been saved yet/i)

    await user.click(screen.getByRole('button', { name: /filter by orders/i }))
    const min = await screen.findByLabelText(/minimum, orders/i)
    await user.type(min, '20')
    // Not applied yet — both rows still visible mid-type.
    expect(screen.getByText('19')).toBeInTheDocument()
    expect(screen.getByText('22')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Apply' }))
    expect(screen.getByText('22')).toBeInTheDocument()
    expect(screen.queryByText('19')).not.toBeInTheDocument()
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
