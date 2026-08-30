import * as React from 'react'
import { createPortal } from 'react-dom'
import { ArrowLeft, Lightbulb, Sparkles, X } from 'lucide-react'

import { ADVISORY_CAPABILITIES, type BusinessInsightKind } from '@/capabilities'
import { Button } from '@/components/ui/button'
import { Input, Select } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { getJson } from '@/lib/api'
import {
  GUIDED_CATEGORIES,
  KNOWN_PLATFORMS,
  advisoryPeriodCount,
  composeAdviceRequest,
  composeGuidedQuestion,
  dateRangeErrorMessage,
  toGuidedParams,
  type DateRange,
  type GuidedAdviceRequest,
  type GuidedCampaign,
  type GuidedCategoryId,
  type GuidedDraft,
} from '@/components/Chat/guidedQuestion'

// ---------------------------------------------------------------------------
// A guided, step-by-step way to reach anything this product can do, for an
// owner staring at a blank composer with no idea what's available. What it
// can offer is not decided here: Step 1 renders `@/capabilities`, the one
// authoritative catalog, whose contract test holds it against the real Go
// tool registry — so this dialog cannot quietly fall behind the product the
// way the Help page's tool count and exampleQuestions.ts both did.
//
// The catalog has two kinds of entry, and this dialog keeps them separate all
// the way down because they are different KINDS of thing, not two flavors of
// the same thing:
//
//   * A COMPUTED capability (8 of them, one per typed MCP tool) assembles a
//     natural-language STRING from structured choices and hands it to the
//     exact same `/api/ask` flow every typed or example question already goes
//     through (`onAsk` → ChatPanel's `submitQuestion`). Every one of those
//     questions is answerable by construction.
//
//   * The ADVISORY capability (5 insight kinds) is not a data lookup at all —
//     it is probabilistic guidance from one billed model call, and it emits a
//     `GuidedAdviceRequest` through a SEPARATE callback (`onRequestAdvice`),
//     never an `onAsk` string. It carries the established
//     BusinessInsightChip visual language — dashed warning border, lightbulb,
//     "AI suggestion" — from Step 1 onward, so an owner can tell before
//     reading a word that this path ends somewhere other than a computed
//     fact.
// ---------------------------------------------------------------------------

/**
 * Every element type a Tab key press can land the browser's focus on.
 * Deliberately excludes anything with `tabindex="-1"` — the dialog's own
 * root is focusable via `.focus()` for the initial "focus lands somewhere
 * sensible on open" behavior, but is never meant to be a Tab *stop* once
 * inside it, so it's excluded here on purpose rather than by omission.
 */
const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',')

/** Every focusable element currently inside `container`, in DOM/tab order. */
function getFocusableElements(container: HTMLElement): HTMLElement[] {
  return Array.from(container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR))
}

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
  // The `min`/`max` attributes below get the browser's own date picker UI
  // and its `validity.rangeOverflow`/`rangeUnderflow` right for free, but
  // that validity state is never surfaced to the user by the native
  // control on its own — a date outside it still lands in `value`
  // unchanged. This is the actual gate: a plain string comparison (see
  // `dateRangeErrorMessage`) that both drives this visible error AND, via
  // `toGuidedParams`, decides whether Continue may be pressed at all.
  const errorId = `${id}-error`
  const error = dateRangeErrorMessage(value, { minDate, maxDate })
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
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? errorId : undefined}
        onChange={(event) => onChange(event.target.value)}
      />
      {error ? (
        <p id={errorId} role="alert" className="text-xs text-destructive-text">
          {error}
        </p>
      ) : null}
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

/**
 * A tile for anything on the advisory path, carrying BusinessInsightChip's
 * established visual language rather than a second one invented here: the
 * dashed warning-tinted surface, the lightbulb, and the literal "AI
 * suggestion" label. An owner who has already met the chip in an answer
 * should recognise this as the same category of thing on sight — that
 * recognition is the whole point of not inventing new styling for it.
 */
function AdvisoryTile({
  label,
  description,
  onClick,
}: {
  label: string
  description: string
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex h-full w-full flex-col items-start gap-1 rounded-lg border border-dashed border-warning/50 bg-warning/5 p-3 text-left transition-colors hover:bg-warning/10 focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
    >
      <span className="flex items-center gap-1.5">
        <Lightbulb className="size-3.5 shrink-0 text-warning-text" aria-hidden="true" />
        <span className="text-micro font-semibold uppercase tracking-wide text-warning-text">
          AI suggestion
        </span>
      </span>
      <span className="text-sm font-medium text-foreground">{label}</span>
      <span className="text-xs text-muted-foreground">{description}</span>
    </button>
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
  /**
   * Hands off a request for business ADVICE — a deliberately separate
   * callback from `onAsk`, not a variant of it, because it is a different
   * action with a different destination (`POST /api/business-insight`, one
   * billed model call, probabilistic output) reached through a grounding
   * question rather than being one.
   *
   * Optional, and the advisory path is hidden entirely when it is absent: a
   * host that cannot resolve advice must not advertise that it can.
   */
  onRequestAdvice?: (request: GuidedAdviceRequest) => void
  /** Bounds every date picker to the real, live data range (`useDataCoverage`). */
  minDate?: string | null
  maxDate?: string | null
  /** Overridable for tests — defaults to the real `GET /api/promotions` call. */
  fetchCampaigns?: () => Promise<GuidedCampaign[]>
}

type Step = 'category' | 'advisory_topic' | 'params' | 'review'

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
  onRequestAdvice,
  minDate,
  maxDate,
  fetchCampaigns = fetchKnownCampaigns,
}: QuestionComposerProps) {
  const [step, setStep] = React.useState<Step>('category')
  const [categoryId, setCategoryId] = React.useState<GuidedCategoryId | null>(null)
  // Mutually exclusive with `categoryId` by construction — selecting either
  // one clears the other, so no state can ever describe both a computed and
  // an advisory walk at once.
  const [advisoryKind, setAdvisoryKind] = React.useState<BusinessInsightKind | null>(
    null,
  )
  const [draft, setDraft] = React.useState<GuidedDraft>({})
  const [questionText, setQuestionText] = React.useState('')

  const [campaigns, setCampaigns] = React.useState<GuidedCampaign[] | null>(null)
  const [campaignsLoading, setCampaignsLoading] = React.useState(false)
  const [campaignsError, setCampaignsError] = React.useState<string | null>(null)

  const dialogRef = React.useRef<HTMLDivElement>(null)
  const titleId = React.useId()

  // A stable target to portal the dialog into, created once and never
  // recreated across re-renders. The dialog renders into this node rather
  // than inline in the tree so it ends up a sibling of the rest of the app
  // under `document.body` — see the effect below for why that placement is
  // what makes the "mark everything else `inert`" half of this dialog's
  // focus containment possible at all.
  const [portalNode] = React.useState(() => document.createElement('div'))

  // Every open starts a fresh walk through the steps — a half-built question
  // from a previous, abandoned attempt should never resurface silently.
  React.useEffect(() => {
    if (!open) return
    setStep('category')
    setCategoryId(null)
    setAdvisoryKind(null)
    setDraft({})
    setQuestionText('')
  }, [open])

  // `role="dialog"` + `aria-modal="true"` is a promise that nothing outside
  // this dialog is reachable while it's open. Two things keep that promise:
  // mounting the dialog under a node appended directly to `document.body`
  // (so it sits beside the rest of the app, not inside it), and marking
  // every OTHER top-level child of `document.body` `inert` for as long as
  // the dialog is open — which drops the sidebar nav, the fullscreen
  // toggle, and the cost pill from both the tab order and assistive tech's
  // view of the page, without the visual disruption `display:none` would
  // cause. Everything is restored, element by element, exactly as found —
  // and focus returns to whatever opened the composer — on close.
  React.useEffect(() => {
    if (!open) return
    const previouslyFocused = document.activeElement as HTMLElement | null

    document.body.appendChild(portalNode)
    const outsideElements = Array.from(document.body.children).filter(
      (element) => element !== portalNode,
    ) as HTMLElement[]
    // Set via the `inert` attribute directly, not the `.inert` IDL property:
    // the attribute is what the HTML spec's inert algorithm actually keys
    // off of (the property setter is just a reflection of it), so this gets
    // identical real-browser behavior while staying observable in
    // environments — jsdom included — that don't implement that reflection.
    const previouslyInert = outsideElements.map((element) => element.hasAttribute('inert'))
    outsideElements.forEach((element) => {
      element.setAttribute('inert', '')
    })

    dialogRef.current?.focus()

    return () => {
      outsideElements.forEach((element, index) => {
        if (previouslyInert[index]) {
          element.setAttribute('inert', '')
        } else {
          element.removeAttribute('inert')
        }
      })
      portalNode.parentNode?.removeChild(portalNode)
      previouslyFocused?.focus?.()
    }
  }, [open, portalNode])

  // Escape must close the dialog per the WAI-ARIA modal dialog pattern,
  // regardless of where focus currently sits. A React `onKeyDown` on the
  // dialog's own container only fires for a keydown that bubbles up
  // through that container's subtree — but QA found that clicking a Step 1
  // category button (or the in-dialog Back button) unmounts that button on
  // the step change, which drops focus to `document.body`. A keydown on
  // `body` never bubbles into the dialog's own container, so the
  // container-scoped handler silently never sees it. Listening at the
  // document level sidesteps that entirely: this fires for Escape no
  // matter which element (or lack of one) currently has focus.
  React.useEffect(() => {
    if (!open) return
    function handleDocumentKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        onClose()
      }
    }
    document.addEventListener('keydown', handleDocumentKeyDown)
    return () => {
      document.removeEventListener('keydown', handleDocumentKeyDown)
    }
  }, [open, onClose])

  /** Tab/Shift+Tab cycling, scoped to the dialog's own focusable elements. */
  function trapTabKey(event: React.KeyboardEvent<HTMLDivElement>) {
    if (event.key !== 'Tab') return
    const container = dialogRef.current
    if (!container) return

    const focusable = getFocusableElements(container)
    if (focusable.length === 0) {
      event.preventDefault()
      return
    }

    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    const activeIndex = focusable.indexOf(document.activeElement as HTMLElement)

    if (event.shiftKey) {
      if (activeIndex <= 0) {
        event.preventDefault()
        last.focus()
      }
    } else if (activeIndex === -1 || activeIndex === focusable.length - 1) {
      event.preventDefault()
      first.focus()
    }
  }

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
  const advisory =
    ADVISORY_CAPABILITIES.find((c) => c.insightKind === advisoryKind) ?? null
  const params = categoryId
    ? toGuidedParams(categoryId, draft, { minDate, maxDate })
    : null
  const adviceRequest = advisoryKind
    ? composeAdviceRequest(advisoryKind, draft, { minDate, maxDate })
    : null
  // The one gate on Continue, whichever walk is in progress: a null here
  // means the form is incomplete or carries a date the backend has no data
  // for, exactly as before.
  const canContinue = advisoryKind ? adviceRequest !== null : params !== null

  function selectCategory(id: GuidedCategoryId) {
    setCategoryId(id)
    setAdvisoryKind(null)
    setDraft(defaultDraftFor(id))
    setStep('params')
  }

  function selectAdvisoryTopic(kind: BusinessInsightKind) {
    setAdvisoryKind(kind)
    setCategoryId(null)
    setDraft({})
    setStep('params')
  }

  function goToReview() {
    if (advisoryKind) {
      if (!adviceRequest) return
      setQuestionText(adviceRequest.question)
      setStep('review')
      return
    }
    if (!categoryId || !params) return
    setQuestionText(composeGuidedQuestion(params))
    setStep('review')
  }

  function goBack() {
    if (step === 'review') {
      setStep('params')
      return
    }
    if (step === 'params') {
      setStep(advisoryKind ? 'advisory_topic' : 'category')
      return
    }
    setStep('category')
  }

  function handleAsk() {
    const trimmed = questionText.trim()
    if (!trimmed) return
    onAsk(trimmed)
  }

  // Never `onAsk` with the grounding question: the caller has to know an
  // advisory outcome was asked for, or it cannot tell the difference between
  // "no pattern found" and "advice never requested".
  function handleRequestAdvice() {
    if (!adviceRequest || !onRequestAdvice) return
    onRequestAdvice(adviceRequest)
  }

  return createPortal(
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-4"
      onKeyDown={trapTabKey}
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
                onClick={goBack}
                className="mb-1 flex items-center gap-1 text-xs font-medium text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:rounded-sm"
              >
                <ArrowLeft className="size-3" aria-hidden="true" />
                Back
              </button>
            ) : null}
            <h2 id={titleId} className="text-base font-semibold text-foreground">
              {step === 'category'
                ? 'Build a question'
                : step === 'advisory_topic'
                  ? 'Get business advice'
                  : step === 'params'
                    ? (advisory?.label ?? category?.label)
                    : advisory
                      ? 'Review this advice request'
                      : 'Review your question'}
            </h2>
            <p className="mt-0.5 text-xs text-muted-foreground">
              {step === 'category'
                ? 'What do you want to know?'
                : step === 'advisory_topic'
                  ? 'What would you like advice about?'
                  : step === 'params'
                    ? (advisory?.description ?? category?.description)
                    : advisory
                      ? "We'll compute the pattern first — advice is yours to request after."
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
            <div className="flex flex-col gap-4">
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

              {/* The advisory path, kept below a rule and visually apart from
                  the computed categories above it — the separation is the
                  message. Hidden entirely without `onRequestAdvice`: a host
                  that cannot resolve advice must not offer it. */}
              {onRequestAdvice ? (
                <div className="border-t border-border pt-4">
                  <p className="mb-2 text-xs text-muted-foreground">
                    Or ask for general business advice, grounded in a pattern
                    we compute from your own numbers — a suggestion, not a
                    calculated fact.
                  </p>
                  <AdvisoryTile
                    label="Get business advice"
                    description="What owners typically do about a pattern in your data."
                    onClick={() => setStep('advisory_topic')}
                  />
                </div>
              ) : null}
            </div>
          ) : null}

          {step === 'advisory_topic' ? (
            <ul className="grid grid-cols-1 gap-2">
              {ADVISORY_CAPABILITIES.map((c) => (
                <li key={c.insightKind}>
                  <AdvisoryTile
                    label={c.label}
                    description={c.description}
                    onClick={() => selectAdvisoryTopic(c.insightKind)}
                  />
                </li>
              ))}
            </ul>
          ) : null}

          {step === 'params' && advisory ? (
            <div className="flex flex-col gap-4">
              {advisoryPeriodCount(advisory) === 2 ? (
                <>
                  <PeriodFields
                    idPrefix="guided-advice-period-a"
                    legend="First period"
                    value={draft.periodA}
                    minDate={minDate ?? undefined}
                    maxDate={maxDate ?? undefined}
                    onChange={(next) => setDraft((d) => ({ ...d, periodA: next }))}
                  />
                  <PeriodFields
                    idPrefix="guided-advice-period-b"
                    legend="Second period"
                    value={draft.periodB}
                    minDate={minDate ?? undefined}
                    maxDate={maxDate ?? undefined}
                    onChange={(next) => setDraft((d) => ({ ...d, periodB: next }))}
                  />
                </>
              ) : (
                <PeriodFields
                  idPrefix="guided-advice-period"
                  value={draft.period}
                  minDate={minDate ?? undefined}
                  maxDate={maxDate ?? undefined}
                  onChange={(next) => setDraft((d) => ({ ...d, period: next }))}
                />
              )}
            </div>
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

          {step === 'review' && !advisory ? (
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

          {/* The advisory review deliberately does NOT offer the computed
              path's editable textarea. This question is machinery, not the
              owner's words: it exists to compute the pattern the advice must
              be grounded in, and an edited version could stop producing that
              pattern while the request still claimed it. Shown read-only
              because the owner is still entitled to see exactly what will be
              asked on their behalf. */}
          {step === 'review' && advisory ? (
            <div className="flex flex-col gap-3">
              <div className="flex flex-col gap-1.5">
                <span className="text-xs font-medium text-foreground">
                  First, we compute this
                </span>
                <p className="rounded-md border border-border bg-background px-3 py-2 text-sm text-foreground">
                  {questionText}
                </p>
                <p className="text-xs text-muted-foreground">
                  A normal, provenance-backed answer with the source rows
                  behind every number.
                </p>
              </div>
              <div className="space-y-1.5 rounded-lg border border-dashed border-warning/40 bg-warning/5 px-3.5 py-3">
                <p className="flex items-center gap-1.5">
                  <Lightbulb
                    className="size-3.5 shrink-0 text-warning-text"
                    aria-hidden="true"
                  />
                  <span className="text-micro font-semibold uppercase tracking-wide text-warning-text">
                    AI suggestion
                  </span>
                </p>
                <p className="text-xs leading-relaxed text-foreground">
                  Then, if that answer shows the pattern, you can tap once for{' '}
                  {advisory.label.replace(/^Advice on /i, 'advice on ')}. That
                  step is a billed model call and shows its own cost — general
                  industry practice connected to your numbers, never a computed
                  fact about your business.
                </p>
                <p className="text-xs leading-relaxed text-muted-foreground">
                  If the pattern isn&apos;t there, we&apos;ll say so and
                  nothing extra is charged.
                </p>
              </div>
            </div>
          ) : null}
        </div>

        {step === 'params' || step === 'review' ? (
          <div className="flex items-center justify-end gap-2 border-t border-border px-5 py-3">
            {step === 'params' ? (
              <Button type="button" onClick={goToReview} disabled={!canContinue}>
                Continue
              </Button>
            ) : advisory ? (
              <Button type="button" onClick={handleRequestAdvice} disabled={!adviceRequest}>
                <Lightbulb aria-hidden="true" />
                Compute this and offer advice
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
    </div>,
    portalNode,
  )
}
