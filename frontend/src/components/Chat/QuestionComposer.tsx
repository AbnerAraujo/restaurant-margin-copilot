import * as React from 'react'
import { ArrowLeft, Sparkles, X } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Input, Select } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { getJson } from '@/lib/api'
import {
  GUIDED_CATEGORIES,
  KNOWN_PLATFORMS,
  composeGuidedQuestion,
  toGuidedParams,
  type DateRange,
  type GuidedCampaign,
  type GuidedCategoryId,
  type GuidedDraft,
} from '@/components/Chat/guidedQuestion'

// ---------------------------------------------------------------------------
// A guided, step-by-step way to build a well-formed question, for an owner
// staring at a blank composer with no idea what's answerable. Purely a
// frontend affordance: it never calls a model and never adds a backend
// endpoint — it only assembles a natural-language STRING from structured
// choices and hands that string to the exact same `/api/ask` flow every
// typed or example question already goes through (see ChatPanel's
// `submitQuestion`, wired via `onAsk`). The 8 categories in Step 1 map
// one-to-one onto the fixed MCP tool set in contracts/mcp-tools.md, so this
// flow can never compose a question the backend will just refuse for naming
// a capability that doesn't exist.
// ---------------------------------------------------------------------------

interface PromotionsApiResponse {
  promotions: { campaign_id: string; platform: string }[]
}

/**
 * Real campaign identifiers, live from `GET /api/promotions` — the same
 * endpoint PromotionsPage already reads. Never a free-text campaign field:
 * the whole point of this flow is avoiding an ambiguous or nonexistent
 * campaign id reaching the gate. Deduplicated by campaign id since the same
 * campaign never needs to appear twice in a picker.
 */
async function fetchKnownCampaigns(): Promise<GuidedCampaign[]> {
  const data = await getJson<PromotionsApiResponse>('/api/promotions')
  const byId = new Map<string, GuidedCampaign>()
  for (const promotion of data.promotions) {
    if (!byId.has(promotion.campaign_id)) {
      byId.set(promotion.campaign_id, {
        campaignId: promotion.campaign_id,
        platform: promotion.platform,
      })
    }
  }
  return [...byId.values()]
}

/** Sensible first sub-choice for a category that needs one, chosen on entry. */
function defaultDraftFor(category: GuidedCategoryId): GuidedDraft {
  if (category === 'discrepancies') return { scope: 'single_date' }
  if (category === 'promotion_roi') return { mode: 'campaign' }
  return {}
}

function updateRange(
  range: Partial<DateRange> | undefined,
  field: 'start' | 'end',
  value: string,
): Partial<DateRange> {
  return { ...range, [field]: value }
}

function FieldLabel({
  htmlFor,
  children,
}: {
  htmlFor: string
  children: React.ReactNode
}) {
  return (
    <label htmlFor={htmlFor} className="text-xs font-medium text-foreground">
      {children}
    </label>
  )
}

function DateField({
  id,
  label,
  value,
  onChange,
  minDate,
  maxDate,
}: {
  id: string
  label: string
  value: string
  onChange: (value: string) => void
  minDate?: string
  maxDate?: string
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        type="date"
        required
        value={value}
        min={minDate}
        max={maxDate}
        onChange={(event) => onChange(event.target.value)}
      />
    </div>
  )
}

function PeriodFields({
  idPrefix,
  legend,
  value,
  onChange,
  minDate,
  maxDate,
}: {
  idPrefix: string
  /** Optional heading above the pair — used when two periods are on screen at once. */
  legend?: string
  value: Partial<DateRange> | undefined
  onChange: (next: Partial<DateRange>) => void
  minDate?: string
  maxDate?: string
}) {
  return (
    <div className="flex flex-col gap-2">
      {legend ? (
        <p className="text-xs font-semibold text-foreground">{legend}</p>
      ) : null}
      <div className="grid grid-cols-2 gap-3">
        <DateField
          id={`${idPrefix}-start`}
          label="Start date"
          value={value?.start ?? ''}
          minDate={minDate}
          maxDate={maxDate}
          onChange={(next) => onChange(updateRange(value, 'start', next))}
        />
        <DateField
          id={`${idPrefix}-end`}
          label="End date"
          value={value?.end ?? ''}
          minDate={minDate}
          maxDate={maxDate}
          onChange={(next) => onChange(updateRange(value, 'end', next))}
        />
      </div>
      {value?.start && value.end && value.start > value.end ? (
        <p role="alert" className="text-xs text-destructive-text">
          End date must be on or after the start date.
        </p>
      ) : null}
    </div>
  )
}

/** The "one day" / "a date range" and "a campaign" / "a platform" sub-choices. */
function ScopeToggle<T extends string>({
  label,
  options,
  value,
  onChange,
}: {
  label: string
  options: { value: T; label: string }[]
  value: T | undefined
  onChange: (value: T) => void
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-xs font-medium text-foreground">{label}</span>
      <div role="radiogroup" aria-label={label} className="inline-flex w-fit overflow-hidden rounded-md border border-border">
        {options.map((option) => (
          <Button
            key={option.value}
            type="button"
            role="radio"
            aria-checked={value === option.value}
            variant={value === option.value ? 'default' : 'ghost'}
            size="sm"
            className="rounded-none"
            onClick={() => onChange(option.value)}
          >
            {option.label}
          </Button>
        ))}
      </div>
    </div>
  )
}

export interface QuestionComposerProps {
  open: boolean
  onClose: () => void
  /**
   * Hands the exact, owner-reviewed question string off to the normal ask
   * flow. This component never asks anything itself — see the module doc
   * comment above.
   */
  onAsk: (question: string) => void
  /** Bounds every date picker to the real, live data range (`useDataCoverage`). */
  minDate?: string | null
  maxDate?: string | null
  /** Overridable for tests — defaults to the real `GET /api/promotions` call. */
  fetchCampaigns?: () => Promise<GuidedCampaign[]>
}

type Step = 'category' | 'params' | 'review'

/**
 * "Build a question" — a guided, 3-step composer: pick what you want to
 * know (Step 1, the 8 real categories), fill in only the structured
 * parameters that category needs (Step 2), then review and, if needed, edit
 * the exact question before it's asked (Step 3). Nothing here is a free-text
 * field for something structured — every date, period, platform, and
 * campaign is a real picker fed from real known values, per this product's
 * "never guess, never let the model narrate an ambiguity" discipline applied
 * to the composer itself.
 */
export default function QuestionComposer({
  open,
  onClose,
  onAsk,
  minDate,
  maxDate,
  fetchCampaigns = fetchKnownCampaigns,
}: QuestionComposerProps) {
  const [step, setStep] = React.useState<Step>('category')
  const [categoryId, setCategoryId] = React.useState<GuidedCategoryId | null>(null)
  const [draft, setDraft] = React.useState<GuidedDraft>({})
  const [questionText, setQuestionText] = React.useState('')

  const [campaigns, setCampaigns] = React.useState<GuidedCampaign[] | null>(null)
  const [campaignsLoading, setCampaignsLoading] = React.useState(false)
  const [campaignsError, setCampaignsError] = React.useState<string | null>(null)

  const dialogRef = React.useRef<HTMLDivElement>(null)
  const titleId = React.useId()

  // Every open starts a fresh walk through the steps — a half-built question
  // from a previous, abandoned attempt should never resurface silently.
  React.useEffect(() => {
    if (!open) return
    setStep('category')
    setCategoryId(null)
    setDraft({})
    setQuestionText('')
  }, [open])

  React.useEffect(() => {
    if (open) dialogRef.current?.focus()
  }, [open])

  const loadCampaigns = React.useCallback(() => {
    setCampaignsLoading(true)
    setCampaignsError(null)
    fetchCampaigns()
      .then((result) => {
        setCampaigns(result)
      })
      .catch((caught: unknown) => {
        setCampaignsError(
          caught instanceof Error ? caught.message : String(caught),
        )
      })
      .finally(() => {
        setCampaignsLoading(false)
      })
  }, [fetchCampaigns])

  // Fetched once, lazily, the first time the campaign picker could actually
  // be needed — never on every open of the composer, and never for a
  // category that doesn't use it. `campaignsError` is part of the guard so a
  // failed load is never silently retried on the owner's behalf the instant
  // it fails (that would replace the error message with a second spinner
  // before anyone could read it) — retrying is the explicit "Try again"
  // button's job.
  React.useEffect(() => {
    if (categoryId !== 'promotion_roi') return
    if (campaigns !== null || campaignsLoading || campaignsError !== null) return
    loadCampaigns()
  }, [categoryId, campaigns, campaignsLoading, campaignsError, loadCampaigns])

  if (!open) return null

  const category = GUIDED_CATEGORIES.find((c) => c.id === categoryId) ?? null
  const params = categoryId ? toGuidedParams(categoryId, draft) : null

  function selectCategory(id: GuidedCategoryId) {
    setCategoryId(id)
    setDraft(defaultDraftFor(id))
    setStep('params')
  }

  function goToReview() {
    if (!categoryId || !params) return
    setQuestionText(composeGuidedQuestion(params))
    setStep('review')
  }

  function handleAsk() {
    const trimmed = questionText.trim()
    if (!trimmed) return
    onAsk(trimmed)
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-4"
      onKeyDown={(event) => {
        if (event.key === 'Escape') onClose()
      }}
    >
      <div
        aria-hidden="true"
        className="absolute inset-0 bg-black/40"
        onClick={onClose}
      />
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
        className="relative z-10 flex max-h-[85vh] w-full max-w-lg flex-col overflow-hidden rounded-xl border border-border bg-card shadow-xl outline-none"
      >
        <div className="flex items-start justify-between gap-3 border-b border-border px-5 py-4">
          <div className="min-w-0">
            {step !== 'category' ? (
              <button
                type="button"
                onClick={() => setStep(step === 'review' ? 'params' : 'category')}
                className="mb-1 flex items-center gap-1 text-xs font-medium text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:rounded-sm"
              >
                <ArrowLeft className="size-3" aria-hidden="true" />
                Back
              </button>
            ) : null}
            <h2 id={titleId} className="text-base font-semibold text-foreground">
              {step === 'category'
                ? 'Build a question'
                : step === 'params'
                  ? category?.label
                  : 'Review your question'}
            </h2>
            <p className="mt-0.5 text-xs text-muted-foreground">
              {step === 'category'
                ? 'What do you want to know?'
                : step === 'params'
                  ? category?.description
                  : "This is exactly what we'll ask. Edit it if you'd like."}
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close"
            className="shrink-0 rounded-sm p-1 text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
          >
            <X className="size-4" aria-hidden="true" />
          </button>
        </div>

        <div className="flex-1 overflow-y-auto px-5 py-4">
          {step === 'category' ? (
            <ul className="grid grid-cols-1 gap-2 sm:grid-cols-2">
              {GUIDED_CATEGORIES.map((c) => (
                <li key={c.id}>
                  <button
                    type="button"
                    onClick={() => selectCategory(c.id)}
                    className="flex h-full w-full flex-col items-start gap-1 rounded-lg border border-border bg-background p-3 text-left transition-colors hover:border-primary/40 hover:bg-primary/5 focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
                  >
                    <span className="text-sm font-medium text-foreground">
                      {c.label}
                    </span>
                    <span className="text-xs text-muted-foreground">
                      {c.description}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          ) : null}

          {step === 'params' && categoryId === 'daily_summary' ? (
            <DateField
              id="guided-date"
              label="Date"
              value={draft.date ?? ''}
              minDate={minDate ?? undefined}
              maxDate={maxDate ?? undefined}
              onChange={(value) => setDraft((d) => ({ ...d, date: value }))}
            />
          ) : null}

          {step === 'params' && categoryId === 'margin_delta' ? (
            <div className="flex flex-col gap-4">
              <PeriodFields
                idPrefix="guided-period-a"
                legend="First period"
                value={draft.periodA}
                minDate={minDate ?? undefined}
                maxDate={maxDate ?? undefined}
                onChange={(next) => setDraft((d) => ({ ...d, periodA: next }))}
              />
              <PeriodFields
                idPrefix="guided-period-b"
                legend="Second period"
                value={draft.periodB}
                minDate={minDate ?? undefined}
                maxDate={maxDate ?? undefined}
                onChange={(next) => setDraft((d) => ({ ...d, periodB: next }))}
              />
            </div>
          ) : null}

          {step === 'params' && categoryId === 'discrepancies' ? (
            <div className="flex flex-col gap-4">
              <ScopeToggle
                label="Check"
                options={[
                  { value: 'single_date', label: 'One day' },
                  { value: 'period', label: 'A date range' },
                ]}
                value={draft.scope}
                onChange={(scope) => setDraft((d) => ({ ...d, scope }))}
              />
              {draft.scope === 'period' ? (
                <PeriodFields
                  idPrefix="guided-discrepancies-period"
                  value={draft.period}
                  minDate={minDate ?? undefined}
                  maxDate={maxDate ?? undefined}
                  onChange={(next) => setDraft((d) => ({ ...d, period: next }))}
                />
              ) : (
                <DateField
                  id="guided-discrepancies-date"
                  label="Date"
                  value={draft.date ?? ''}
                  minDate={minDate ?? undefined}
                  maxDate={maxDate ?? undefined}
                  onChange={(value) => setDraft((d) => ({ ...d, date: value }))}
                />
              )}
            </div>
          ) : null}

          {step === 'params' && categoryId === 'promotion_roi' ? (
            <div className="flex flex-col gap-4">
              <ScopeToggle
                label="Look at"
                options={[
                  { value: 'campaign', label: 'A specific campaign' },
                  { value: 'platform_period', label: 'A platform over a period' },
                ]}
                value={draft.mode}
                onChange={(mode) => setDraft((d) => ({ ...d, mode }))}
              />
              {draft.mode === 'platform_period' ? (
                <>
                  <div className="flex flex-col gap-1.5">
                    <FieldLabel htmlFor="guided-platform">Platform</FieldLabel>
                    <Select
                      id="guided-platform"
                      value={draft.platform ?? ''}
                      onChange={(event) =>
                        setDraft((d) => ({ ...d, platform: event.target.value }))
                      }
                    >
                      <option value="" disabled>
                        Choose a platform
                      </option>
                      {KNOWN_PLATFORMS.map((platform) => (
                        <option key={platform.value} value={platform.value}>
                          {platform.label}
                        </option>
                      ))}
                    </Select>
                  </div>
                  <PeriodFields
                    idPrefix="guided-promo-period"
                    value={draft.period}
                    minDate={minDate ?? undefined}
                    maxDate={maxDate ?? undefined}
                    onChange={(next) => setDraft((d) => ({ ...d, period: next }))}
                  />
                </>
              ) : (
                <div className="flex flex-col gap-1.5">
                  <FieldLabel htmlFor="guided-campaign">Campaign</FieldLabel>
                  {campaignsLoading ? (
                    <p className="text-xs text-muted-foreground">
                      Loading your campaigns…
                    </p>
                  ) : campaignsError ? (
                    <div className="flex flex-col items-start gap-1.5">
                      <p role="alert" className="text-xs text-destructive-text">
                        We couldn&apos;t load your campaigns. Try again in a
                        moment.
                      </p>
                      <Button type="button" size="sm" variant="outline" onClick={loadCampaigns}>
                        Try again
                      </Button>
                    </div>
                  ) : campaigns && campaigns.length > 0 ? (
                    <Select
                      id="guided-campaign"
                      value={draft.campaignId ?? ''}
                      onChange={(event) =>
                        setDraft((d) => ({ ...d, campaignId: event.target.value }))
                      }
                    >
                      <option value="" disabled>
                        Choose a campaign
                      </option>
                      {campaigns.map((c) => (
                        <option key={c.campaignId} value={c.campaignId}>
                          {c.campaignId} ({c.platform})
                        </option>
                      ))}
                    </Select>
                  ) : (
                    <p className="text-xs text-muted-foreground">
                      No campaigns on file yet. Try a platform and period
                      instead.
                    </p>
                  )}
                </div>
              )}
            </div>
          ) : null}

          {step === 'params' &&
          (categoryId === 'negative_roi_promotions' ||
            categoryId === 'platform_economics' ||
            categoryId === 'period_totals' ||
            categoryId === 'expense_pattern_by_day') ? (
            <PeriodFields
              idPrefix="guided-period"
              value={draft.period}
              minDate={minDate ?? undefined}
              maxDate={maxDate ?? undefined}
              onChange={(next) => setDraft((d) => ({ ...d, period: next }))}
            />
          ) : null}

          {step === 'review' ? (
            <div className="flex flex-col gap-1.5">
              <FieldLabel htmlFor="guided-question-text">Your question</FieldLabel>
              <Textarea
                id="guided-question-text"
                value={questionText}
                onChange={(event) => setQuestionText(event.target.value)}
                rows={3}
              />
            </div>
          ) : null}
        </div>

        {step !== 'category' ? (
          <div className="flex items-center justify-end gap-2 border-t border-border px-5 py-3">
            {step === 'params' ? (
              <Button type="button" onClick={goToReview} disabled={!params}>
                Continue
              </Button>
            ) : (
              <Button
                type="button"
                onClick={handleAsk}
                disabled={questionText.trim().length === 0}
              >
                <Sparkles aria-hidden="true" />
                Ask this question
              </Button>
            )}
          </div>
        ) : null}
      </div>
    </div>
  )
}
