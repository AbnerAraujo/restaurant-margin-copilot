import {
  ArrowLeftRight,
  CalendarCheck,
  CalendarRange,
  Coins,
  FileWarning,
  HelpCircle,
  Megaphone,
  MessagesSquare,
  Scale,
  ShieldAlert,
  ShieldCheck,
  TrendingDown,
  UploadCloud,
  type LucideIcon,
} from 'lucide-react'
import { Link } from 'react-router-dom'

import {
  buildCapabilitySummary,
  EXAMPLE_QUESTIONS,
  type ExampleQuestion,
} from '@/components/Chat/exampleQuestions'
import { Chip, PageContainer, PageHeader, Panel, PanelHeader } from '@/components/ui/page'
import { useDataCoverage } from '@/lib/useDataCoverage'

/**
 * `/help` — in-app documentation for a first-time owner.
 *
 * Every claim here is sourced from something that already exists elsewhere
 * in the codebase, not written fresh: the "what you can ask" list imports
 * `EXAMPLE_QUESTIONS`/`CAPABILITY_SUMMARY` from `Chat/exampleQuestions.ts`
 * (the same single source of truth the chat surface itself renders from,
 * per that file's own doc comment — a model was deliberately never asked to
 * describe its own capabilities, since that's a hallucination surface), the
 * "how to use it" walkthrough describes only real, routed pages, and the
 * refusal-discipline section restates CLAUDE.md's own hard limit rather than
 * inventing new language for it. There is no support/contact mechanism, no
 * screenshot, and no video here — this is a no-auth, single-owner prototype
 * (see `SettingsPage.tsx`) with nowhere for a support request to go.
 */
export default function HelpPage() {
  return (
    <PageContainer className="flex flex-col gap-5">
      <PageHeader
        eyebrow="My Business Steward"
        title="Help"
        meta={
          <>
            <Chip icon={HelpCircle}>How this app works</Chip>
            <Chip>No account or setup required</Chip>
          </>
        }
      />

      <WhatThisDoesPanel />
      <WhatYouCanAskPanel />
      <HowToUseItPanel />
      <WhyItRefusesPanel />
    </PageContainer>
  )
}

/**
 * Section 1 — benefits, grounded in the README's own framing: daily
 * reconciliation across POS, delivery platforms, and supplier costs; a
 * deterministic engine that owns every number; a model layer that only
 * narrates; provenance on every figure; refuse-rather-than-guess as the
 * hard limit, not an afterthought.
 */
function WhatThisDoesPanel() {
  return (
    <Panel aria-label="What this does" className="p-5 sm:p-6">
      <PanelHeader eyebrow="Why this exists" title="What this does" />
      <p className="mt-3 max-w-prose-measure text-sm leading-relaxed text-muted-foreground">
        Sales come in from delivery platforms and the in-house POS. Supplier
        invoices come in separately. Nobody reconciles these against each
        other daily, because doing it by hand is tedious — so margin
        slippage is usually only discovered when the month closes, too late
        to act on. This app ingests those files, reconciles them
        deterministically every day, and answers plain-language questions
        about what happened and why — flagging duplicate orders, refunds,
        and numbers outside the usual range as they occur, not a month
        later.
      </p>
      <ul className="mt-4 grid grid-cols-1 gap-3 border-t border-border pt-4 sm:grid-cols-2">
        <BenefitItem
          icon={ShieldCheck}
          title="Every number is computed in Go, never by a model"
          description="Reconciliation, margin, week-over-week deltas, and ROI are deterministic, unit-tested code. The model's only job is to call a fixed set of typed tools and narrate the result — it never does arithmetic."
        />
        <BenefitItem
          icon={FileWarning}
          title="Discrepancies are caught, not buried"
          description="Duplicate orders, refunds, and out-of-range figures are flagged the day they're reconciled, with the specific rows behind the flag — not left for a month-end surprise."
        />
        <BenefitItem
          icon={HelpCircle}
          title="Plain-language answers, with provenance"
          description="Ask about a day, a week, a campaign, or a platform in your own words. Every number in the answer names which file, which rows, and which period it came from."
        />
        <BenefitItem
          icon={ShieldAlert}
          title="Refuses rather than guesses"
          description="When a question can't be answered from the data on file, the system says so and explains why, instead of estimating. A confidently wrong margin figure is worse than an honest 'I don't have that.'"
        />
      </ul>
    </Panel>
  )
}

function BenefitItem({
  icon: Icon,
  title,
  description,
}: {
  icon: LucideIcon
  title: string
  description: string
}) {
  return (
    <li className="flex gap-3">
      <span className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary">
        <Icon className="size-4" aria-hidden="true" />
      </span>
      <div className="min-w-0">
        <p className="text-sm font-medium text-foreground">{title}</p>
        <p className="mt-0.5 text-xs leading-relaxed text-muted-foreground">
          {description}
        </p>
      </div>
    </li>
  )
}

/** One icon per MCP tool, reused from the same visual language other pages
 * already use for these exact concepts (daily close, week-over-week,
 * discrepancies, campaign ROI, negative-ROI campaigns, platform comparison,
 * period totals) — chosen for consistency, not invented fresh here. */
const TOOL_ICON: Record<ExampleQuestion['tool'], LucideIcon> = {
  get_daily_summary: CalendarCheck,
  get_margin_delta: ArrowLeftRight,
  list_discrepancies: ShieldAlert,
  get_promotion_roi: Megaphone,
  list_negative_roi_promotions: TrendingDown,
  compare_platform_economics: Scale,
  get_period_totals: CalendarRange,
}

/**
 * Section 2 — pulled directly from `EXAMPLE_QUESTIONS`/`buildCapabilitySummary`,
 * the exact source the chat surface (`ChatPanel.tsx`) renders its own
 * suggestion chips and capability blurb from. Adding an eighth MCP tool and
 * its example question there updates this page automatically — nothing
 * here is a hand-typed second copy of that list. The coverage range itself
 * comes from `useDataCoverage` (a live fetch), not a hardcoded date string —
 * see that hook's doc comment for why this page used to go stale.
 */
function WhatYouCanAskPanel() {
  const coverage = useDataCoverage()
  return (
    <Panel aria-label="What you can ask" className="overflow-hidden">
      <div className="p-5 sm:p-6">
        <PanelHeader eyebrow="On the Ask page" title="What you can ask" />
        {coverage.label ? (
          <>
            <p className="mt-3 max-w-prose-measure text-sm leading-relaxed text-muted-foreground">
              {buildCapabilitySummary(coverage.label)}
            </p>
            <div className="mt-3">
              <Chip icon={CalendarRange}>{coverage.label}</Chip>
            </div>
          </>
        ) : null}
      </div>
      <ul className="divide-y divide-border border-t border-border">
        {EXAMPLE_QUESTIONS.map((question) => {
          const Icon = TOOL_ICON[question.tool]
          return (
            <li key={question.text} className="flex items-start gap-3 px-5 py-4 sm:px-6">
              <span className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary">
                <Icon className="size-4" aria-hidden="true" />
              </span>
              <div className="min-w-0">
                <p className="text-sm font-medium text-foreground">
                  &ldquo;{question.text}&rdquo;
                </p>
                <p className="mt-1 flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
                  <Chip tone="brand">{question.topic}</Chip>
                  <span>
                    answered by{' '}
                    <span className="font-mono text-xs">{question.tool}</span>
                  </span>
                </p>
              </div>
            </li>
          )
        })}
      </ul>
      <p className="border-t border-border p-5 text-xs leading-relaxed text-muted-foreground sm:px-6">
        These seven tools are the complete, fixed set of things this system
        can compute — there is no open-ended chat behind them. A question
        outside this list, or outside the data on file, gets a refusal
        instead of a guess (see below).
      </p>
    </Panel>
  )
}

interface PageWalkthrough {
  to: string
  icon: LucideIcon
  title: string
  description: string
}

const PAGE_WALKTHROUGHS: PageWalkthrough[] = [
  {
    to: '/',
    icon: CalendarCheck,
    title: 'Home',
    description:
      "Today's numbers at a glance: the latest reconciled margin, how many days are reconciled, how many carried a flag, and your Steward points total — all read live, nothing placeholder.",
  },
  {
    to: '/ask',
    icon: MessagesSquare,
    title: 'Ask',
    description:
      'A chat for the questions listed above, in your own words. Every answer is grounded in one of the seven typed tools and carries the source rows behind its numbers, or it refuses and says why.',
  },
  {
    to: '/promotions',
    icon: Megaphone,
    title: 'Promotions',
    description:
      "Every campaign's ROI, including the ones flagged as losing money. Log a new campaign directly, optionally marking it as replacing a flagged one, and pay its spend with your earned Steward points instead of cash if your balance covers it.",
  },
  {
    to: '/points',
    icon: Coins,
    title: 'Points',
    description:
      'How your Steward points balance was earned — from clean closes, caught discrepancies, real usage days, and profitable or corrective campaigns — and what is available to spend right now.',
  },
  {
    to: '/platforms',
    icon: Scale,
    title: 'Platforms',
    description:
      'iFood vs. Just Eat Takeaway, side by side, for the period: gross sales, commission paid, effective commission rate, and promo spend.',
  },
  {
    to: '/upload',
    icon: UploadCloud,
    title: 'Upload costs',
    description:
      'Upload a corrected or new supplier cost sheet, preview every row before anything is saved, and see the exact before/after margin effect once committed.',
  },
]

/**
 * Section 3 — a short walkthrough of the real, routed pages, each linking
 * to the page it describes.
 */
function HowToUseItPanel() {
  return (
    <Panel aria-label="How to use it" className="overflow-hidden">
      <div className="p-5 sm:p-6">
        <PanelHeader eyebrow="Around the app" title="How to use it" />
      </div>
      <ul className="divide-y divide-border border-t border-border">
        {PAGE_WALKTHROUGHS.map(({ to, icon: Icon, title, description }) => (
          <li key={to}>
            <Link
              to={to}
              className="flex items-center gap-3 px-5 py-4 transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 sm:px-6"
            >
              <span className="flex size-9 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary">
                <Icon className="size-4" aria-hidden="true" />
              </span>
              <span className="min-w-0 flex-1">
                <span className="text-sm font-medium text-foreground">{title}</span>
                <span className="mt-0.5 block text-xs leading-relaxed text-muted-foreground">
                  {description}
                </span>
              </span>
            </Link>
          </li>
        ))}
      </ul>
    </Panel>
  )
}

/**
 * Section 4 — why a refusal is a feature, not a bug. Restates CLAUDE.md's
 * hard limit ("the system refuses rather than estimates when data is
 * missing or incomplete... a confidently wrong margin figure is worse than
 * a refusal") with the two real, currently-shipping refusal shapes an owner
 * can actually run into: the Ask page's pre-processing gate, and a
 * promotion whose ROI can't be attributed (PromotionsPage's `roi: null`
 * state, FR-013).
 */
function WhyItRefusesPanel() {
  return (
    <Panel tone="muted" aria-label="Why it refuses sometimes" className="p-5 sm:p-6">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <Chip icon={ShieldAlert} tone="warning">
          By design
        </Chip>
        <h2 className="text-sm font-semibold tracking-tight text-foreground">
          Why it refuses sometimes
        </h2>
      </div>
      <p className="mt-3 max-w-prose-measure text-sm leading-relaxed text-muted-foreground">
        Before answering any question on the Ask page, the system classifies
        it as answerable, ambiguous, or unanswerable — before any expensive
        reasoning runs. An ambiguous question (for example, one where
        &ldquo;the weekend&rdquo; could or could not include Friday) gets a
        clarifying question back instead of a guessed assumption. A question
        the data on file genuinely cannot answer gets a plain refusal with
        the reason, rather than a plausible-sounding estimate.
      </p>
      <p className="mt-3 max-w-prose-measure text-sm leading-relaxed text-muted-foreground">
        You may also see this on the Promotions page: a campaign whose
        incremental revenue can&apos;t be attributed shows as unattributable
        rather than a fabricated $0 or a made-up ROI number. In both cases
        this is the same rule applied consistently — a confidently wrong
        margin figure is worse than an honest &ldquo;I don&apos;t have
        that,&rdquo; so the system is built to say so instead of estimating.
      </p>
    </Panel>
  )
}
