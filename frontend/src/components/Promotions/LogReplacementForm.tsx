import { useState, type FormEvent } from 'react'
import { AlertTriangle, CheckCircle2, Coins, Rocket } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Input, Select } from '@/components/ui/input'
import { Panel, PanelHeader } from '@/components/ui/page'
import { ApiError, postJson } from '@/lib/api'
import { usePoints } from '@/components/Points/usePoints'
import { CENTS_PER_POINT } from '@/components/Points/pointValues'
import { KNOWN_PLATFORMS } from '@/components/Chat/guidedQuestion'

/**
 * The minimal shape spec 002-badge-expansion's User Story 3 needs (plan.md's
 * "Frontend changes"): a real create-promotion flow, reachable from a
 * flagged negative-ROI row, that closes the insight-to-action loop. Not a
 * full promotion-management feature — no edit/delete UI, matching spec.md's
 * own Assumptions ("scoped to what User Story 3 needs").
 */
export interface FlaggedCampaign {
  campaignId: string
  platform: string
}

interface CreatePromotionResponseApi {
  promotion: {
    campaign_id: string
    origin: string
    replaces_campaign_id?: string | null
  }
  earned_campaign_creation_badge: boolean
  points_balance_after?: number
}

const NO_REPLACEMENT = ''

type PaymentMethod = 'money' | 'points'

/**
 * "Log a replacement campaign" — the one net-new write surface this spec
 * adds to the product. Deliberately plain: five fields (platform, campaign
 * id, period start/end, spend) plus an optional "replaces" dropdown
 * populated ONLY from campaigns this page already shows as flagged
 * negative-ROI (SC-003: "no step requiring data the owner doesn't already
 * have on screen").
 *
 * FR-007 is enforced server-side, not here: this dropdown narrows the UI to
 * plausible choices, but the backend re-verifies the claim against live data
 * regardless (see internal/httpapi/promotions_create.go) — this form's job
 * is UX, not the actual guarantee.
 */
export default function LogReplacementForm({
  flaggedCampaigns,
  onCreated,
}: {
  flaggedCampaigns: FlaggedCampaign[]
  onCreated: () => void
}) {
  const [platform, setPlatform] = useState('')
  const [campaignId, setCampaignId] = useState('')
  const [periodStart, setPeriodStart] = useState('')
  const [periodEnd, setPeriodEnd] = useState('')
  const [spend, setSpend] = useState('')
  const [replaces, setReplaces] = useState(NO_REPLACEMENT)
  const [paymentMethod, setPaymentMethod] = useState<PaymentMethod>('money')

  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState<{
    campaignId: string
    earnedBadge: boolean
    pointsBalanceAfter?: number
  } | null>(null)

  // Live balance, shared with PointsCard/PointsPage — refetched by
  // `onCreated`'s parent reload after a points-paid submission, so this
  // preview never drifts from what the balance actually is after spending.
  const { data: pointsData } = usePoints()
  const availablePoints = pointsData?.points?.available ?? 0

  const spendNumberPreview = Number(spend)
  const pointsNeededPreview =
    Number.isFinite(spendNumberPreview) && spendNumberPreview > 0
      ? Math.ceil((spendNumberPreview * 100) / CENTS_PER_POINT)
      : 0
  const insufficientPointsPreview =
    paymentMethod === 'points' && pointsNeededPreview > availablePoints

  function resetFields() {
    setPlatform('')
    setCampaignId('')
    setPeriodStart('')
    setPeriodEnd('')
    setSpend('')
    setReplaces(NO_REPLACEMENT)
    setPaymentMethod('money')
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError(null)
    setSuccess(null)

    const spendNumber = Number(spend)
    if (!Number.isFinite(spendNumber) || spendNumber < 0) {
      setError('Spend must be a non-negative number.')
      return
    }

    setSubmitting(true)
    try {
      const response = await postJson<CreatePromotionResponseApi>(
        '/api/promotions',
        {
          platform,
          campaign_id: campaignId,
          period: { start: periodStart, end: periodEnd },
          spend: spendNumber.toFixed(2),
          payment_method: paymentMethod,
          ...(replaces ? { replaces } : {}),
        },
      )
      setSuccess({
        campaignId: response.promotion.campaign_id,
        earnedBadge: response.earned_campaign_creation_badge,
        pointsBalanceAfter: response.points_balance_after,
      })
      resetFields()
      onCreated()
    } catch (caught) {
      setError(
        caught instanceof ApiError
          ? caught.message
          : caught instanceof Error
            ? caught.message
            : String(caught),
      )
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Panel className="p-5 sm:p-6">
      <PanelHeader
        eyebrow="Close the loop"
        title="Log a replacement campaign"
      />
      <p className="mt-1 text-xs text-muted-foreground">
        A new promotion record, logged directly here — the same fields an
        ingested campaign carries. Mark it as replacing a flagged campaign
        below and it earns a Campaign Launcher badge once submitted. Pay its
        spend with your Steward points instead of cash if you have enough
        available.
      </p>

      <form
        onSubmit={(event) => {
          void handleSubmit(event)
        }}
        className="mt-4 grid grid-cols-1 gap-3 sm:grid-cols-2"
      >
        <div className="flex flex-col gap-1.5">
          <label htmlFor="lrf-platform" className="text-xs font-medium text-foreground">
            Platform
          </label>
          {/*
            A fixed <select> over KNOWN_PLATFORMS (the same list
            QuestionComposer.tsx's guided platform picker already uses,
            mirroring the backend's own knownComparatorPlatforms), not the
            free-text <Input> this used to be. That free-text field is
            exactly how "iFood" and "Ifood" ended up as two distinct,
            silently-diverging platform values in the database (duplicate
            "IFOOD ROI" stat cards, a filter that dropped half the
            platform's campaigns, under-reported spend) — the option's
            *value* is p.label (the exact display string
            reconcile.PromotionRoiRecord.Platform carries, e.g. "iFood"),
            not p.value (the lowercase comparator-tool source key), since
            that's what this endpoint persists and the backend now
            validates against (see promotions_create.go).
          */}
          <Select
            id="lrf-platform"
            required
            value={platform}
            onChange={(event) => setPlatform(event.target.value)}
          >
            <option value="">Choose a platform</option>
            {KNOWN_PLATFORMS.map((p) => (
              <option key={p.value} value={p.label}>
                {p.label}
              </option>
            ))}
          </Select>
        </div>

        <div className="flex flex-col gap-1.5">
          <label htmlFor="lrf-campaign-id" className="text-xs font-medium text-foreground">
            Campaign identifier
          </label>
          <Input
            id="lrf-campaign-id"
            required
            placeholder="e.g. IFOOD-CAMP-SPRINGMENU"
            value={campaignId}
            onChange={(event) => setCampaignId(event.target.value)}
          />
        </div>

        <div className="flex flex-col gap-1.5">
          <label htmlFor="lrf-period-start" className="text-xs font-medium text-foreground">
            Period start
          </label>
          <Input
            id="lrf-period-start"
            type="date"
            required
            value={periodStart}
            onChange={(event) => setPeriodStart(event.target.value)}
          />
        </div>

        <div className="flex flex-col gap-1.5">
          <label htmlFor="lrf-period-end" className="text-xs font-medium text-foreground">
            Period end
          </label>
          <Input
            id="lrf-period-end"
            type="date"
            required
            value={periodEnd}
            onChange={(event) => setPeriodEnd(event.target.value)}
          />
        </div>

        <div className="flex flex-col gap-1.5">
          <label htmlFor="lrf-spend" className="text-xs font-medium text-foreground">
            Spend (USD)
          </label>
          <Input
            id="lrf-spend"
            type="number"
            min="0"
            step="0.01"
            required
            placeholder="0.00"
            value={spend}
            onChange={(event) => setSpend(event.target.value)}
          />
        </div>

        <div className="flex flex-col gap-1.5 sm:col-span-2">
          <span className="text-xs font-medium text-foreground">Pay with</span>
          <div className="flex flex-wrap items-center gap-2">
            <div role="radiogroup" aria-label="Payment method" className="inline-flex overflow-hidden rounded-md border border-border">
              <Button
                type="button"
                role="radio"
                aria-checked={paymentMethod === 'money'}
                variant={paymentMethod === 'money' ? 'default' : 'ghost'}
                size="sm"
                className="rounded-none"
                onClick={() => setPaymentMethod('money')}
              >
                Money
              </Button>
              <Button
                type="button"
                role="radio"
                aria-checked={paymentMethod === 'points'}
                variant={paymentMethod === 'points' ? 'default' : 'ghost'}
                size="sm"
                className="rounded-none"
                onClick={() => setPaymentMethod('points')}
              >
                <Coins aria-hidden="true" />
                Points
              </Button>
            </div>
            {paymentMethod === 'points' ? (
              <p
                className={
                  'text-xs ' +
                  (insufficientPointsPreview ? 'text-destructive-text' : 'text-muted-foreground')
                }
              >
                {pointsNeededPreview > 0
                  ? `${pointsNeededPreview.toLocaleString('en-US')} points needed`
                  : 'Enter a spend amount to see the points needed'}
                {' · '}
                {availablePoints.toLocaleString('en-US')} available
                {insufficientPointsPreview ? ' — not enough to cover this' : ''}
              </p>
            ) : null}
          </div>
        </div>

        <div className="flex flex-col gap-1.5">
          <label htmlFor="lrf-replaces" className="text-xs font-medium text-foreground">
            Replacing a flagged campaign? (optional)
          </label>
          <Select
            id="lrf-replaces"
            value={replaces}
            onChange={(event) => setReplaces(event.target.value)}
            disabled={flaggedCampaigns.length === 0}
          >
            <option value={NO_REPLACEMENT}>
              {flaggedCampaigns.length === 0
                ? 'No flagged campaigns on file'
                : 'No — log independently'}
            </option>
            {flaggedCampaigns.map((c) => (
              <option key={c.campaignId} value={c.campaignId}>
                {c.campaignId} ({c.platform})
              </option>
            ))}
          </Select>
        </div>

        <div className="sm:col-span-2 flex flex-wrap items-center gap-3 pt-1">
          <Button type="submit" disabled={submitting || insufficientPointsPreview}>
            <Rocket aria-hidden="true" />
            {submitting ? 'Logging…' : 'Log promotion'}
          </Button>

          {error ? (
            <p role="alert" className="flex items-center gap-1.5 text-xs text-destructive-text">
              <AlertTriangle className="size-3.5 shrink-0" aria-hidden="true" />
              {error}
            </p>
          ) : null}

          {success ? (
            <p className="flex items-center gap-1.5 text-xs text-success-text">
              <CheckCircle2 className="size-3.5 shrink-0" aria-hidden="true" />
              Logged {success.campaignId}.{' '}
              {success.pointsBalanceAfter !== undefined
                ? `Paid with points — ${success.pointsBalanceAfter.toLocaleString('en-US')} left.`
                : 'Paid with money.'}{' '}
              {success.earnedBadge
                ? 'Campaign Launcher badge earned.'
                : 'No replacement claimed, so no Campaign Launcher badge this time.'}
            </p>
          ) : null}
        </div>
      </form>
    </Panel>
  )
}
