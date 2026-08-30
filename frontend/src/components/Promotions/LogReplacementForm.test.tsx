import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import LogReplacementForm, { type FlaggedCampaign } from './LogReplacementForm'
import { KNOWN_PLATFORMS } from '@/components/Chat/guidedQuestion'

const FLAGGED: FlaggedCampaign[] = [
  { campaignId: 'JET-CAMP-LUNCHFIX', platform: 'Just Eat Takeaway' },
]

function stubFetchOnce(status: number, body: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({
      ok: status >= 200 && status < 300,
      status,
      json: async () => body,
      text: async () => JSON.stringify(body),
    }),
  )
}

/**
 * Routes fetch by URL substring — needed once a test cares about the LIVE
 * points balance specifically, since GET /api/badges (usePoints, for the
 * points-needed preview) and POST /api/promotions are two different calls
 * that `stubFetchOnce`'s single shared response can't tell apart.
 */
function stubFetchRoutes(routes: Record<string, unknown>) {
  vi.stubGlobal(
    'fetch',
    vi.fn((url: RequestInfo | URL) => {
      const match = Object.keys(routes).find((path) => String(url).includes(path))
      const body = match ? routes[match] : {}
      return Promise.resolve({
        ok: true,
        status: 200,
        json: async () => body,
        text: async () => JSON.stringify(body),
      })
    }),
  )
}

/**
 * The form now also fetches GET /api/badges on mount (usePoints, for the
 * live points-balance preview) — a real, additional call every one of these
 * tests' fetch mocks now sees. Find the /api/promotions POST specifically,
 * rather than assuming it's the first (or only) call fetch received.
 */
function findPromotionsPostCall(fetchMock: ReturnType<typeof vi.fn>) {
  const call = fetchMock.mock.calls.find(([url]) =>
    String(url).includes('/api/promotions'),
  )
  if (!call) throw new Error('no call to /api/promotions was made')
  return call
}

/**
 * `LogReplacementForm` now calls `useUnsavedChangesGuard`, which calls
 * React Router's `useBlocker` — only valid inside a data router
 * (`createMemoryRouter`/`RouterProvider`, matching `router.test.tsx`'s own
 * pattern), never a bare `render()`. A second route stands in for "anywhere
 * else in the app" so tests can drive an in-app navigation away and observe
 * whether the discard-confirmation guard actually fires.
 */
function renderForm(props: {
  flaggedCampaigns: FlaggedCampaign[]
  onCreated: () => void
}) {
  const router = createMemoryRouter(
    [
      { path: '/promotions', element: <LogReplacementForm {...props} /> },
      { path: '/close', element: <p>Today's Close</p> },
    ],
    { initialEntries: ['/promotions'] },
  )
  render(<RouterProvider router={router} />)
  return router
}

async function fillRequiredFields(
  user: ReturnType<typeof userEvent.setup>,
  overrides: { campaignId?: string; spend?: string } = {},
) {
  await user.selectOptions(screen.getByLabelText(/platform/i), 'Just Eat Takeaway')
  await user.type(
    screen.getByLabelText(/campaign identifier/i),
    overrides.campaignId ?? 'JET-CAMP-NEWREPLACEMENT',
  )
  await user.type(screen.getByLabelText(/period start/i), '2026-08-15')
  await user.type(screen.getByLabelText(/period end/i), '2026-08-21')
  const spendInput = screen.getByLabelText(/spend/i)
  await user.clear(spendInput)
  await user.type(spendInput, overrides.spend ?? '75')
}

describe('LogReplacementForm', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders every FR-005 field plus the optional replaces dropdown', () => {
    renderForm({ flaggedCampaigns: FLAGGED, onCreated: vi.fn() })

    expect(screen.getByLabelText(/platform/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/campaign identifier/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/period start/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/period end/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/spend/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/replacing a flagged campaign/i)).toBeInTheDocument()
  })

  it('constrains the platform field to a <select> offering ONLY the known platforms, never free text', () => {
    renderForm({ flaggedCampaigns: FLAGGED, onCreated: vi.fn() })

    const platformField = screen.getByLabelText<HTMLSelectElement>(/^platform$/i)
    // A real <select>, not a text <input> an owner could type "Ifood" into
    // — the exact free-text gap that let "iFood" and "Ifood" diverge into
    // two distinct database values (see the QA report this fixes).
    expect(platformField.tagName).toBe('SELECT')

    const optionLabels = Array.from(platformField.querySelectorAll('option')).map(
      (option) => option.textContent,
    )
    // The blank placeholder, plus exactly the known platforms — one entry
    // per platform, nothing invented and nothing duplicated.
    expect(optionLabels).toEqual([
      'Choose a platform',
      ...KNOWN_PLATFORMS.map((p) => p.label),
    ])
  })

  it('populates the replaces dropdown ONLY from the flagged campaigns already on screen', () => {
    renderForm({ flaggedCampaigns: FLAGGED, onCreated: vi.fn() })

    const select = screen.getByLabelText(/replacing a flagged campaign/i)
    expect(
      screen.getByRole('option', { name: /JET-CAMP-LUNCHFIX/ }),
    ).toBeInTheDocument()
    // Exactly the "no replacement" default plus the one flagged campaign —
    // never an invented option.
    expect(select.querySelectorAll('option')).toHaveLength(2)
  })

  it('disables the dropdown and says so plainly when there are no flagged campaigns', () => {
    renderForm({ flaggedCampaigns: [], onCreated: vi.fn() })

    const select = screen.getByLabelText<HTMLSelectElement>(
      /replacing a flagged campaign/i,
    )
    expect(select).toBeDisabled()
    expect(screen.getByText(/no flagged campaigns on file/i)).toBeInTheDocument()
  })

  it('submits with the chosen replaces campaign and reports the earned Campaign Launcher badge', async () => {
    const user = userEvent.setup()
    stubFetchOnce(201, {
      promotion: {
        campaign_id: 'JET-CAMP-NEWREPLACEMENT',
        origin: 'owner_created',
        replaces_campaign_id: 'JET-CAMP-LUNCHFIX',
      },
      earned_campaign_creation_badge: true,
    })
    const onCreated = vi.fn()
    renderForm({ flaggedCampaigns: FLAGGED, onCreated })

    await fillRequiredFields(user)
    await user.selectOptions(
      screen.getByLabelText(/replacing a flagged campaign/i),
      'JET-CAMP-LUNCHFIX',
    )
    await user.click(screen.getByRole('button', { name: /log promotion/i }))

    await waitFor(() => expect(onCreated).toHaveBeenCalledTimes(1))
    expect(
      await screen.findByText(/campaign launcher badge earned/i),
    ).toBeInTheDocument()

    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/promotions'),
      expect.objectContaining({ method: 'POST' }),
    )
    const [, requestInit] = findPromotionsPostCall(fetch as ReturnType<typeof vi.fn>)
    const body = JSON.parse(requestInit.body as string)
    expect(body).toMatchObject({
      platform: 'Just Eat Takeaway',
      campaign_id: 'JET-CAMP-NEWREPLACEMENT',
      period: { start: '2026-08-15', end: '2026-08-21' },
      spend: '75.00',
      payment_method: 'money',
      replaces: 'JET-CAMP-LUNCHFIX',
    })
  })

  it('submits with NO replaces field when logged independently, and reports no badge earned', async () => {
    const user = userEvent.setup()
    stubFetchOnce(201, {
      promotion: {
        campaign_id: 'JET-CAMP-STANDALONE',
        origin: 'owner_created',
        replaces_campaign_id: null,
      },
      earned_campaign_creation_badge: false,
    })
    renderForm({ flaggedCampaigns: FLAGGED, onCreated: vi.fn() })

    await fillRequiredFields(user, { campaignId: 'JET-CAMP-STANDALONE' })
    await user.click(screen.getByRole('button', { name: /log promotion/i }))

    expect(
      await screen.findByText(/no campaign launcher badge this time/i),
    ).toBeInTheDocument()

    const [, requestInit] = findPromotionsPostCall(fetch as ReturnType<typeof vi.fn>)
    const body = JSON.parse(requestInit.body as string)
    expect(body).not.toHaveProperty('replaces')
  })

  it('surfaces the real FR-007 refusal from the backend rather than a generic error', async () => {
    const user = userEvent.setup()
    stubFetchOnce(422, {
      error: 'replaces_not_flagged_negative',
      detail: 'campaign_id "IFOOD-CAMP-BOOST01" is not currently flagged negative-ROI.',
    })
    renderForm({ flaggedCampaigns: FLAGGED, onCreated: vi.fn() })

    await fillRequiredFields(user)
    await user.click(screen.getByRole('button', { name: /log promotion/i }))

    expect(
      await screen.findByRole('alert'),
    ).toHaveTextContent(/not currently flagged negative-roi/i)
  })

  // QA round 4 regression: this form used to render `caught.message`
  // straight from the thrown ApiError, bypassing lib/requestFailure.ts's
  // blocklist entirely (unlike PromotionsPage.tsx, which already routed
  // through explainRequestFailure). backend/internal/httpapi/promotions_
  // create.go's query_failed path writes the raw pgx error string as
  // `detail`, and it — like every other internal-only code — must never
  // reach this screen.
  it('never renders a raw internal error for an internal-only failure code', async () => {
    const user = userEvent.setup()
    stubFetchOnce(500, {
      error: 'query_failed',
      detail: 'ERROR: relation "promotion" does not exist (SQLSTATE 42P01)',
    })
    renderForm({ flaggedCampaigns: FLAGGED, onCreated: vi.fn() })

    await fillRequiredFields(user)
    await user.click(screen.getByRole('button', { name: /log promotion/i }))

    const alert = await screen.findByRole('alert')
    expect(alert).not.toHaveTextContent(/SQLSTATE/i)
    expect(alert).not.toHaveTextContent(/relation "promotion"/i)
    expect(alert).toHaveTextContent(/server ran into a problem/i)
  })

  it('refuses a negative spend without ever calling the backend', async () => {
    const user = userEvent.setup()
    const fetchSpy = vi.fn()
    vi.stubGlobal('fetch', fetchSpy)
    renderForm({ flaggedCampaigns: FLAGGED, onCreated: vi.fn() })

    await fillRequiredFields(user, { spend: '-5' })
    // The input's own min="0" constraint blocks native form submission
    // before the JS handler even runs (belt-and-braces with the handler's
    // own `spendNumber < 0` check, which guards the case a caller bypasses
    // the DOM constraint) — either way, the backend must never see this
    // submission.
    const spendInput = screen.getByLabelText<HTMLInputElement>(/spend/i)
    expect(spendInput.checkValidity()).toBe(false)

    await user.click(screen.getByRole('button', { name: /log promotion/i }))
    expect(
      fetchSpy.mock.calls.some(([url]) => String(url).includes('/api/promotions')),
    ).toBe(false)
  })

  // QA round 4 regression: `pointsNeededPreview` used to compute
  // `Math.ceil((spendNumberPreview * 100) / CENTS_PER_POINT)`. Plain JS
  // float multiplication lands `1.1 * 100` at 110.00000000000001, not 110,
  // so the ceiling division inflated the preview to 12 points for a spend
  // that costs exactly 11 — enough to wrongly disable submission for an
  // owner who has exactly enough points. The backend
  // (PointsNeededForSpend, backend/internal/badges/badges.go) never has
  // this problem because it operates on already-integer cents.
  it('never overstates the points-needed preview due to float multiplication drift', async () => {
    const user = userEvent.setup()
    stubFetchRoutes({
      '/api/badges': {
        badges: [],
        points: { total: 11, breakdown: [], spent: 0, available: 11 },
      },
    })
    renderForm({ flaggedCampaigns: FLAGGED, onCreated: vi.fn() })

    await fillRequiredFields(user, { spend: '1.10' })
    await user.click(screen.getByRole('radio', { name: /points/i }))

    expect(await screen.findByText(/11 points needed/i)).toBeInTheDocument()
    expect(screen.queryByText(/not enough to cover this/i)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /log promotion/i })).not.toBeDisabled()
  })

  describe('unsaved-changes guard', () => {
    it('does not warn navigating away from a genuinely untouched form', async () => {
      const router = renderForm({ flaggedCampaigns: FLAGGED, onCreated: vi.fn() })

      await router.navigate('/close')

      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
      expect(await screen.findByText(/today's close/i)).toBeInTheDocument()
    })

    it('warns before an in-app navigation discards a real in-progress draft, and Cancel keeps the draft and stays on the page', async () => {
      const user = userEvent.setup()
      const router = renderForm({ flaggedCampaigns: FLAGGED, onCreated: vi.fn() })

      await user.type(screen.getByLabelText(/campaign identifier/i), 'JET-CAMP-DRAFT')
      void router.navigate('/close')

      expect(await screen.findByRole('alertdialog')).toHaveTextContent(
        /discard this campaign draft/i,
      )

      await user.click(screen.getByRole('button', { name: 'Cancel' }))

      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
      expect(screen.getByLabelText(/campaign identifier/i)).toHaveValue('JET-CAMP-DRAFT')
      expect(router.state.location.pathname).toBe('/promotions')
    })

    it('discards the draft and completes the navigation on explicit confirm', async () => {
      const user = userEvent.setup()
      const router = renderForm({ flaggedCampaigns: FLAGGED, onCreated: vi.fn() })

      await user.type(screen.getByLabelText(/campaign identifier/i), 'JET-CAMP-DRAFT')
      void router.navigate('/close')

      await user.click(
        await screen.findByRole('button', { name: /discard draft/i }),
      )

      expect(await screen.findByText(/today's close/i)).toBeInTheDocument()
      expect(router.state.location.pathname).toBe('/close')
    })
  })
})
