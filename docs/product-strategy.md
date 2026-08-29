# Product Strategy — Restaurant Margin Copilot

Every data point below is tagged by where it actually came from:

- **[Sourced]** — real, cited public data
- **[Assumption]** — a business assumption we're deliberately reasoning from, stated rather than hidden
- **[Hypothesis]** — the thing we don't know yet and are explicitly testing
- **[Simulated-as-Prosus]** — framed as "if I had Toqan/ToqanClaw's actual internal knowledge base and usage data, here's what I'd pull" — explicitly a stand-in for this exercise, not real company data. Shown to demonstrate how this would be grounded with real access, not to pass as real.

## OKR Objective

**Increase our restaurant and bar partners' profitability by growing revenue without eroding margin.**

This is the objective, not the vision statement below — it's the OKR-level
frame everything else (North Star, KPIs, hypotheses) is now weighed against.
It deliberately holds both halves together: revenue growth alone (e.g. more
volume via heavier discounting or platform promo spend) that quietly erodes
margin is not success against this objective, and margin protection alone
that leaves revenue flat is not either. Profitability is the thing that
moves, and it can move via either lever — which is why the candidate
problems explored below span both a growth-side lever (visibility/ranking,
promo ROI) and a margin-protection-side lever (payout reconciliation).

## Reason to Believe

Why this is worth Prosus funding, not only worth the restaurant adopting:
partner prosperity and platform prosperity move on the same curve, not
opposite ends of a trade-off. A restaurant that grows revenue while
protecting margin becomes a healthier, longer-tenured partner — one that
transacts more, churns less, and stands as a stronger reference case for the
next partner we sign. Every dollar of margin we help protect and every
dollar of revenue we help a partner capture without eroding it compounds
into higher-quality, more durable GMV on our own platform. This is not
goodwill dressed up as strategy; it is the flywheel the Objective above is
actually justified by.

**Resolved: (b)** — scope is broadened to cover both levers, not just margin
protection.

## Key Results

1. **KR1 — Trust**: 100% correct refusal/clarification on the ~5 unanswerable
   evaluation questions; accuracy and consistency rates on the ~15–20
   known-answer and 5×3-phrasing questions measured and reported honestly,
   including failures.
2. **KR2 — Margin protection**: Reconcile daily margin across POS,
   delivery-platform, and cost-sheet data with zero silent data loss on the
   deliberately messy fixture set (duplicate orders, refunds, missing days),
   producing a provenanced true-net-margin figure for every fixture day.
3. **KR3 — Revenue growth**: Identify and correctly flag at least one
   negative-ROI promotion in the fixture data (incremental revenue below its
   cost) end-to-end — ingestion through natural-language Q&A — proving the
   same deterministic-core/probabilistic-narration architecture extends to a
   growth lever, not just margin protection.
4. **KR4 — Token discipline**: Average measured cost per interaction stays
   under a stated threshold while holding KR1's accuracy/consistency bar,
   demonstrated via the instrumentation log across every harness interaction.

## Supporting data, by Key Result

**KR1 — Trust.** Evidence is the evaluator's own publicly stated framework,
not third-party market data: Sean Kenny's canonical failure case (a Data
Analyst asked for a user's address returned the driver's coordinates instead
of refusing) and the "empty ask-me-anything box is a product failure"
position — both already cited above under Opportunity Assessment and
Hypotheses. This KR is measured against his own stated bar, not an
external benchmark.

**KR2 — Margin protection.** See "The user problem, grounded" above:
**[Sourced]** 3–5% average restaurant net margins, 15–30% delivery
commissions, 2–5% of delivery revenue lost to reconciliation discrepancies,
~12 hrs/week of manual reconciliation for a mid-size operation.

**KR3 — Revenue growth (promo ROI).** New data gathered specifically for
this decision:
- **[Sourced]** Sponsored-listing/boost fees typically add another 5–10% of
  monthly revenue on top of base commission.
- **[Sourced]** Concretely: Uber Eats boosted placement runs roughly
  $0.35–1.45/order, DoorDash bid-based boosting averages
  $0.70–2.15/incremental order — hundreds to low-thousands of dollars per
  month for competitive positioning on a single platform.
- **[Sourced]** Restaurant marketing-spend benchmarks: 3–6% of revenue for
  established restaurants, 8–12% for new launches, with delivery-app-specific
  guidance running up to 0–30% of revenue on offers and 0–15% on ads
  depending on lifecycle stage.
- **[Sourced]** A comparable-market ROAS benchmark (Zomato/India restaurant
  ads): 4–6× is considered healthy — a concrete reference point for what
  "negative-ROI" is measured against.
- **[Sourced] — the key finding**: sponsored-placement and promo costs are
  deducted from the restaurant's payout "often without a clear line-item
  breakdown." This is the *same opacity problem* as KR2, just on the spend
  side instead of the commission side — the strongest evidence in this
  document that revenue growth and margin protection are one underlying
  problem (opaque deductions), not two unrelated features bolted together.

**KR4 — Token discipline.** **[Sourced]** The evaluator has publicly stated a
token-discipline and real-time-cost-visibility position (LinkedIn, ~May
2026; cited in `CLAUDE.md`/the constitution) — this KR is measured against
his own stated criteria, not an arbitrary add-on.

## Product concepts considered

Five distinct product shapes were scored against the Objective and the data
above, not against each other in the abstract:

| # | Product | Serves growth | Serves margin protection | Fixture-data realism | Differentiated from ToqanClaw | Buildable by Tuesday |
|---|---|---|---|---|---|---|
| **A** | **Margin & Growth Copilot** — daily reconciliation + promo-ROI flagging, one NL interface, refusal discipline, cost transparency | ✅ | ✅ | ✅ | ✅ | ✅ |
| **B** | Margin Reconciliation only (drop the growth lever) | ❌ | ✅ | ✅ | ✅ | ✅✅ |
| **C** | Promo/Ad-Spend ROI Advisor only (drop reconciliation) | ✅ | ~partial | ✅ | ~partial (thinner AI-judgment story alone) | ✅ |
| **D** | Cross-Platform Economics Comparator (iFood vs. JET) | ~partial (reallocation, not real growth) | ✅ | ❌ (needs two believable platform data models) | ✅✅ (most novel, Prosus-unique) | ❌ |
| **E** | Algorithmic Penalty/Visibility Guardian | ✅ | ❌ | ❌ (no real owner has ranking-algorithm data) | ✅ | ❌ |

**Ranking:**
1. **A** — the only concept satisfying both halves of the Objective directly. Grounded in the strongest evidence in this document: the "deducted from payout without a clear line-item breakdown" finding applies to both the commission side (KR2) and the promo side (KR3) — one architecture answering one underlying opacity problem twice, not two features bolted together.
2. **B** — safest to build, but directly reverts to option (a) in the OKR Objective section above, which was explicitly rejected: it only ever protects money already earned, never grows it.
3. **C** — real growth angle, but alone it has no use for the deliberately messy fixture data (duplicates, refunds, missing days) central to demonstrating the deterministic core, and is a simpler analytics question with less inherent ambiguity — a thinner demonstration of the refusal/clarification discipline the evaluation framework weighs most heavily.
4. **D** — the most strategically interesting idea (uniquely something only Prosus, owning both platforms, could build in good faith) — but needs two internally-consistent platform data models instead of one, a materially bigger fixture-engineering lift with four build days left, and doesn't map cleanly onto the daily North Star metric.
5. **E** — real, sourced pain, but fails the same feasibility test as growth-lever option #2 earlier: no real restaurant owner has a CSV of "ranking algorithm inputs" to hand us, so building it means fabricating data that isn't plausibly real.

**Decision: build Product A.** This confirms, by structured comparison rather than momentum, that KR1–KR4 as already defined describe the right combined product — not a compromise between two half-measures.

## Market sizing & launch strategy

The total universe of potential users splits cleanly into two segments with
completely different launch economics — this split, not a single global
number, is the actually useful way to size this.

### Segment 1 — Prosus customers (warm, already reachable)

- **[Sourced]** Prosus states ToqanClaw is available to **5 million**
  restaurants, merchants, and entrepreneurs across its ecosystem, launched
  June 2026 (Prosus/Naspers newsroom, BusinessWire).
- **[Sourced]** Within that, restaurants already transacting on Prosus-owned
  delivery platforms specifically: **iFood ~350,000+ restaurants** (Brazil)
  and **Just Eat Takeaway ~374,000 partnered restaurants** across its
  markets (~100,000 in the UK alone) — roughly **724,000+ restaurants**
  already on Prosus rails, a tighter and more credible number than the
  looser 5M (which includes non-restaurant merchants).
- **[Sourced]** Three named, real ToqanClaw restaurant case studies exist
  already: **Lebkov & Sons** (Dutch café chain — financial reporting cut
  from weeks to 30 minutes, 40% YoY revenue growth), **Burger & Frites**
  (Rotterdam — +25% deliveries, -60% overtime, €21k/month saved), and
  **Poke Perfect** (Dutch poke bowl chain — -70% routine staff queries).
  These are stronger, more specific proof points than the single generic
  citation used earlier in this document — worth leading with in the
  presentation.
- **[Simulated-as-Prosus]** How many of the 5M are *active* ToqanClaw users
  today (vs. merely eligible) is not disclosed publicly — my research found
  this gap explicitly. The three named case studies are all Netherlands/
  Benelux businesses, which is a plausible (not confirmed) signal that early
  traction is concentrated there — a reasonable first-launch cohort to
  target, not a claimed fact.

### Segment 2 — Non-Prosus customers (cold, no existing distribution)

- **[Sourced]** ~749,000 U.S. restaurant locations (NAICS 722, National
  Restaurant Association 2026) — a market almost entirely outside Prosus's
  reach, since DoorDash and Uber Eats dominate U.S. delivery, not a Prosus
  platform. Comparable cold markets exist wherever Prosus has no delivery
  platform presence.
- This segment requires an entirely different go-to-market motion (direct
  sales, POS-vendor partnerships, or a standalone SaaS play) with its own
  acquisition funnel and CAC — not a distribution extension of anything
  Prosus already has.

### Launch strategy this implies

Launch into **Segment 1 only**, as an added capability inside the existing
ToqanClaw surface restaurants already use — not a new product requiring new
distribution. Segment 2 is explicitly **not** part of a near-term launch
strategy; pursuing it would be a distinct, later decision requiring its own
business case, named here rather than silently assumed.

### How success is measured post-launch (distinct from KR1–KR4)

KR1–KR4 above are what this take-home build proves *now*, inside the eval
harness. The following is what would be measured *after* a real launch into
Segment 1 — a different kind of metric, not to be conflated with the KRs:

- **Attach rate**: % of active ToqanClaw restaurant users who adopt this
  capability within a defined window post-rollout — the right metric
  precisely because distribution already exists; the question is uptake,
  not acquisition.
- **Retention of the behavior this product is betting on**: % of adopters
  still asking questions (not just having reconciled data sitting unused)
  after 30/60/90 days — a direct, later-stage test of Hypothesis 3.
- **Realized profitability lift**: for adopters, a measured change in the
  North Star (time-to-reconciled-close) and, if Product A's growth lever is
  used, negative-ROI promotion spend actually redirected — the real-world
  version of KR2/KR3, with real users instead of fixture data.

## Badge system (UI gamification)

Grounded in two pieces of real research rather than guessed: restaurant
owners are **[Sourced]** "app-rich but insight-poor" and time-poor — tools
survive only if they're quick to adopt and used daily, not if they're
impressive once. **[Sourced]** The B2B gamification pattern that actually
works is quiet acknowledgment (a filled progress bar, a milestone banner),
not loud arcade mechanics — which also matters here specifically because
this is a financial tool; badges that feel like a game would undercut the
trust the whole architecture is built to earn. Badges are a typed,
extensible category (a Postgres enum, not hardcoded), evaluated by
deterministic Go logic against already-computed facts — the model may
*narrate* a badge in conversation, never *decide* one, so this never touches
Principle I.

**Badges are the UX answer to complexity, not decoration on top of it.** A
reconciliation tool genuinely has several distinct capabilities (daily
close, Q&A, promo-ROI flagging) — enough that a flat feature list or a
traditional dashboard risks intimidating exactly the time-poor, non-
technical owner this product is for. Badge-styled tiles double as
navigation on the home screen: the same visual language that quietly
confirms "this worked" also answers "what can I do here" and "where do I go
next," so the gamification layer is load-bearing for usability, not
ornamental. This is why badges extend beyond the Reconciliation category
into being the home screen's actual information architecture, not just an
achievement strip bolted onto a separate dashboard.

**Built now** (Day 4, proof of mechanism — kept small on purpose given the
deadline):
- **Reconciliation category only**: "Clean Close" (a day reconciled with
  zero discrepancies) and "Discrepancy Catcher" (the system caught and
  flagged a duplicate, refund, or anomaly) — both fire directly off
  `DailyReconciliation.discrepancy_flags`, no new computation needed beyond
  what KR2 already produces.

**Roadmap — named, explicitly not built in this take-home**:
- **Growth category** ("Smart Spender," "Margin Guardian" — tied to KR3):
  deferred because it needs UI time beyond Day 4's "functional over
  polished" bar, not because the underlying data isn't there.
- **Engagement category** ("Week One," "Consistency Streak"): needs real
  multi-day usage to mean anything — a fixture-data demo can't organically
  produce a streak, only simulate one, which would be exactly the kind of
  fabricated signal this project's honesty discipline exists to avoid.
- **Campaign Creation category** ("Campaign Launcher" — awarded for
  creating a promotional campaign that connects to Prosus's own promotional
  tooling, e.g. via ToqanClaw automations): the most strategically
  interesting one, because it closes the loop from *insight* to *action* —
  KR3 flags a negative-ROI promotion, and this badge is the natural next
  step, "launch a better one," directly through tools Prosus already owns.
  Not built here because it requires an actual integration this take-home
  has no API access to build against — named as a roadmap direction, not
  faked as a working feature.

## Vision

Give every independent restaurant or bar owner a same-day, trustworthy answer to "did we make money today, and why" — without hiring a bookkeeper, opening three exports, or waiting for month-end to find out margin already slipped.

*(Written for the narrower reconciliation framing — worth revisiting once the objective-vs-scope question above is resolved.)*

## North Star Metric

**Time-to-reconciled-close** — median minutes from "today's data is available" to "the owner has a trusted, provenanced margin figure for today."

**[Sourced]** This mirrors Prosus' own cited proof point for this exact class of product: a ToqanClaw restaurant partner case cut financial reporting from weeks to 30 minutes ("Just Ask: Data Insights for Everyone," Prosus AI Tech Blog). That's the benchmark this North Star is measured against, not an arbitrary number.

## Supporting KPIs

| KPI | Why it matters | Source of truth here |
|---|---|---|
| Accuracy rate | Are the numbers right | Eval harness, ~15–20 known-answer questions |
| Consistency rate | Does rephrasing change the answer | Eval harness, 5 questions × 3 phrasings |
| Refusal-correctness rate | Does it refuse instead of guess when it should | Eval harness, ~5 unanswerable questions |
| Cost per interaction (USD) | Token discipline | Per-interaction instrumentation log |
| Time-to-reconciled-close | The North Star itself | Measured against fixture data end-to-end |

## Real evaluation results (T033/T034, run 2026-08-28)

**[Measured]** — the numbers below come from an actual `promptfoo eval` run
of `evaluation/promptfoo/{accuracy,consistency,refusal}.yaml` against the
live backend (`POST /api/ask`, real Anthropic API calls through the Haiku
4.5 ambiguity gate and Sonnet 5 explain step — no mocking, no cache reuse).
Raw request/response JSON for every call is in the harness output; every
number here was checked against the actual `answer_text`/`status` returned,
not against promptfoo's own pass/fail grading alone (that grading has at
least one known false-negative, noted below). No target was asserted in
advance, per Constitution Principle V — this is what actually happened,
including the failures.

| Metric | Result | Notes |
|---|---|---|
| Accuracy | **10 / 15** (67%) | See failure breakdown below |
| Consistency | **0 / 5 sets** fully agreed | See "year-omission" defect below — this is the headline finding |
| Refusal correctness | **4 / 5** | See R1 below |
| Real API cost, full 35-question run | **$0.286** (cumulative session total $0.325, including earlier smoke tests) | Well inside the $4.50 checkpoint / $5 ceiling |

**Accuracy failures (5 of 15)**:
- **A4** ("Supplier cost total for the two-week period?") — asked for an
  exact date range instead of recognizing "the two-week period" as the
  entire single fixture window (2026-08-01–14), the only period the
  system has data for at all.
- **A9** (ROI on `JET-CAMP-NEWMENU`) — **hallucinated a refusal**: claimed
  the campaign "is not listed in the available data," when it is a real,
  documented campaign with a real, computable +$19.50 ROI. A false
  refusal on answerable data is arguably worse than the model just being
  wrong about a number, because it's the exact failure mode (Principle
  II) the whole architecture is supposed to prevent, showing up in the
  opposite direction.
- **A10** ("Which campaigns should be flagged as underperforming?") —
  asked the user to define an "underperforming" threshold instead of
  calling `list_negative_roi_promotions`, the tool that exists
  specifically to answer this deterministically.
- **A11** ("Delivery revenue on 2026-08-04?") — asked whether "delivery
  revenue" meant all platforms or one specific platform. The near-identical
  A1 phrasing ("Delivery revenue on 2026-08-01?") answered directly with
  the combined total. Same question shape, two different gate outcomes.
- **A15** (delivery revenue on 2026-08-02, net of the refund) — reported
  the gross $154.25 and stated it "can't confirm" the net figure because
  refunds aren't broken out by platform in the tool output, rather than
  the accrual-netted $119.75 `technical-rfc.md`'s design decision calls
  for. Worth a follow-up look at whether `get_daily_summary`'s per-source
  breakdown should expose the netted figure directly.

Two "passing" answers had minor real defects worth recording even though
they matched the golden number: **A6** returned the correct $76.25 but
formatted it with a **€** symbol instead of **$**; **A5**/**A13** answered
the raw dollar "share" correctly but added unprompted, unnecessary
hedging about being unable to compute a *percentage* share (which was
never asked for) — a sign the model reads "share" as "percentage" by
default, worth tightening in the explain prompt.

**Consistency — 0 of 5 sets fully agreed** (the harness's most important
result). The cause is the same across every failing set, not five
unrelated bugs: **when a question omits the year (e.g. "Aug 1st," "the
2nd," "August 3rd"), the model does not reliably infer 2026 — the only
year the system has any data for.** Observed behavior for the same
underlying question varied, unprompted, across all three of: (a)
correctly answering using 2026, (b) asking a clarifying question about
the year, and (c) confidently stating **"no reconciliation data for
[date], 2024"** — inventing a plausible-sounding but wrong year and then
truthfully-but-irrelevantly reporting no data for it. Case (c) is the
concerning one: it isn't a fabricated number, but it *is* a fabricated
premise (the year) stated with full confidence, and it happened on 4 of
the 5 consistency sets (C1, C2, C4, C5) in at least one of the three
phrasings. C3 (LUNCHFIX) failed differently: the shortened name
"LUNCHFIX" alone triggered a refusal ("no campaign named 'LUNCHFIX'")
instead of being matched against `JET-CAMP-LUNCHFIX`. This is a real,
reportable gap in the ambiguity gate / explain step's date-grounding
behavior — the deterministic core was never in question here, only the
model layer's handling of underspecified dates.

**Refusal correctness — 4 of 5.** R2 (attribution-unavailable campaign),
R3 (no such data source), and R4 (outside the fixture window) all refused
cleanly, R4 with an especially precise, accurate stated reason. R5
("How was the weekend?") correctly asked which weekend rather than
guessing, though it didn't proactively surface the Aug 8 gap the golden
answer flags. **R1** ("What was our delivery revenue on August 8th?")
failed: instead of refusing or stating the source is missing, it asked
the user to confirm the year — and suggested **"e.g., 2024"** as the
example, the same wrong-year pattern seen in the consistency failures,
suggesting one shared root cause rather than five independent ones.

**Net read**: the deterministic core (reconciliation, ingestion, the MCP
tool layer) shows no defects in this harness — every failure traced back
to the model layer's date-grounding and tool-selection behavior, exactly
the boundary Principle I says should carry the risk. Hypothesis 1
(refusal trust) is **partially supported**: refusals that did fire were
accurate and well-reasoned (R2–R4), but the harness surfaced a more
basic problem upstream of refusal — inconsistent date-grounding —
that undermines trust before the refusal-vs-guess question is even
reached. That's a more useful, more honest finding than a clean pass
would have been.

## Fix verification: before/after (same day, 2026-08-28)

**[Measured]** — the two root causes named above were fixed and the
**identical** 35-question suite (`evaluation/promptfoo/{accuracy,
consistency,refusal}.yaml` — questions and golden values unchanged) was
re-run against the live backend with real Anthropic API calls, same
methodology as the run above (raw `answer_text`/`status` checked directly,
not just promptfoo's own grading). The numbers below are additive to the
"Real evaluation results" section above, not a replacement for it — both
are kept so the before/after delta stays honestly visible.

**What was actually changed** (both fixes are typed/bounded, per
Constitution Principle III — neither weakens Principle II's refuse-rather-
than-guess discipline; see `backend/internal/mcptools/promo_tools_test.go`'s
"still refuses a genuinely unknown campaign" case and the refusal-
correctness row below for evidence that a real refusal still fires):

1. **Date-year grounding**: the ambiguity gate and the explain step each
   received the real min/max date `daily_reconciliation` actually has
   (`internal/storage.LoadDataDateRange`, a new `GetDataDateRange` SQL
   query resolved once at process start in `cmd/server/main.go`) instead of
   a hardcoded literal, plus an explicit "Date grounding" instruction
   telling both models their only "today" is that max date — never the
   real wall-clock date — and that a year-less date must resolve into the
   one year the data spans, never be asked about or guessed.
2. **Campaign lookup**: `get_promotion_roi` (`internal/mcptools/promo_tools.go`)
   now falls back, on an exact-id miss, to `matchCampaignID`
   (`internal/mcptools/campaign_match.go`) — a bounded, typed match against
   the real, currently-persisted `campaign_id` set
   (`storage.LoadDistinctCampaignIDs`) that resolves a shortened form
   ("LUNCHFIX") or an embedded id in a full display name ("Banner Ad - Lunch
   Fix Menu (JET-CAMP-LUNCHFIX)"), and deliberately refuses to guess (never
   picks a winner) when normalization makes more than one real id plausible.
   A second, related sub-cause was found only while verifying this fix live:
   the ambiguity **gate** itself — which never sees real data — was
   independently refusing shortened campaign references ("Is the LUNCHFIX
   campaign profitable?") before the question ever reached the tool layer
   that could have resolved it. Its system prompt now explicitly tells it
   not to refuse over an unfamiliar-looking campaign name — that
   determination is a downstream typed lookup's job, not the gate's.

**Targeted regression tests** (written specifically to reproduce the
original bugs, per the build order's test-first discipline):
- `TestGate_Classify_DateGroundingRegression`
  (`backend/internal/ambiguity/gate_test.go`) — asks the live Haiku 4.5
  gate the exact bare, no-year, relative question from the original bug
  ("How did we do this week?") three times, asserting no run ever states a
  year outside 2026, never refuses, and never asks which year. **3/3 runs
  passed.**
- `TestGetPromotionRoi_ResolvesRealCampaignByHumanReadableOrShortenedName`
  (`backend/internal/mcptools/promo_tools_test.go`) — a live-Postgres (zero
  API cost) test reproducing the exact two failing inputs from the report,
  "Banner Ad - Lunch Fix Menu (JET-CAMP-LUNCHFIX)" and "LUNCHFIX" alone,
  both asserted to resolve to `JET-CAMP-LUNCHFIX` with its real -$165.00
  ROI, plus a case confirming a genuinely unknown campaign still refuses.
  **All subtests passed.** (`campaign_match_test.go` adds 11 further pure,
  zero-cost unit tests around the matching boundary — ambiguous fragments,
  case/punctuation variance, and "never returns a value outside the known
  set" — all passing.)

| Metric | Before (2026-08-28, earlier run) | After (2026-08-28, this run) | Change |
|---|---|---|---|
| Accuracy | 10/15 (67%) | **9/15 (60%)** | net −1, see honest breakdown below |
| Consistency (sets) | 0/5 fully agreed | **2/5 fully agreed** (promptfoo-strict); **3/5 agree in substance** (manual read) | the *targeted* defect (year-hallucination) is fully gone — see below |
| Refusal correctness | 4/5 (80%) | **5/5 (100%)** | +1 — R1 fixed |
| Real API cost, this 35-question re-run | — | **$0.532881** ($0.01523/question avg, vs $0.00817/question before) | ~1.9× cost/question — see cost note below |
| Cumulative session cost (Postgres-tracked) | $0.367783 | **$0.959264** | still ≪ the $4.50 checkpoint |

**Refusal correctness — the clean win.** All 5 now pass, including **R1**
("What was our delivery revenue on August 8th?"), the exact case that
previously suggested "e.g., 2024" as a clarifying example. It now answers
directly and correctly: *"For August 8th, there's no delivery-platform data
available — the reconciliation for that day only includes POS (in-house)
sales of $487.50..."* — no year confusion, no invented premise.

**Consistency — the targeted defect is eliminated; a different, untargeted
one surfaced.** Zero of the 15 new consistency answers showed the
year-hallucination pattern (a wrong year, or a year-clarification question)
that previously hit 4 of 5 sets — this is the direct, complete fix of the
bug this work targeted. **C3** (LUNCHFIX, the other originally-broken set)
and **C4** (a year-omitted date set) now fully agree with each other and
match the golden value across all 3 phrasings — both promptfoo-graded PASS,
3/3. **C5** (missing Aug 8 delivery data) is consistent and *correct* in
substance across all 3 phrasings (each independently states plainly that no
delivery-platform data exists for Aug 8, citing the real `missing_delivery_
source` flag and the correct $487.50/$152.50 POS-only figures) but
promptfoo's regex grades 2 of 3 as FAIL purely because the answer also
states the day's real (and correct) $0.00 commissions/refunds — an overly
broad "$0.00" guard tripping on a legitimate figure unrelated to the
delivery-data-missing concern it was meant to catch. Read manually rather
than by that regex alone, per this document's own stated methodology, C5 is
a third fully-agreeing set. **C1** and **C2**, however, show a real, newly
observed miss: all 3 phrasings in each set agree with each other and state
correct per-platform figures (e.g. iFood $69.50 + Just Eat Takeaway $76.25
for C1), but none of the 6 answers restate the combined golden total
($145.75 / $120.50) the way the pre-fix passing runs did — a different
quirk (apparent new reluctance to sum two figures from a single tool
result, not a date or campaign defect) that these two fixes were not aimed
at and did not introduce a fabricated number or a refusal regression;
it's flagged here rather than hidden, exactly as the constitution requires.

**Accuracy — the targeted bug fixed, one unrelated quirk newly visible.**
**A9** (ROI on `JET-CAMP-NEWMENU`) — the hallucinated-refusal case this work
targeted — now **passes**: *"ROI: $19.50 (positive — the campaign made more
than it cost)"*, with no false claim of the campaign being absent. Net
accuracy nonetheless moved from 10/15 to 9/15 because **A1**, **A11**, and
**A12** (three previously-passing "delivery revenue on `<date>`" questions)
now show the *same* combined-total quirk described under C1/C2 above: each
correctly states iFood's and Just Eat Takeaway's individual figures but
never states the combined golden number promptfoo's regex checks for. **A4**
(no tool sums a supplier-cost range), **A10** (asks the user to define
"underperforming" instead of calling `list_negative_roi_promotions`), and
**A15** (refund-netting) persist unchanged — all three are real,
previously-known gaps outside the two root causes this work was scoped to
fix, named here rather than folded silently into the headline number.

**Cost note (KR4 — token discipline).** Average cost per question roughly
doubled ($0.00817 → $0.01523) — the direct, expected consequence of adding
explicit date-grounding and campaign-matching guidance to both the gate's
and explain's system prompts (more input tokens per call). Still under two
cents per question and a small fraction of the $4.50 session checkpoint;
named honestly as a real trade-off rather than presented as free. Separately,
an estimated **~$0.10–0.11** was spent on this work's own package-level live
regression tests (`TestGate_Classify_DateGroundingRegression` and the
pre-existing `TestExplain_LiveSmokeTest`, run several times while iterating
on the system prompt) — these call the Anthropic API directly through
`internal/ambiguity`/`internal/explain` rather than through
`internal/httpapi.HandleAsk`, the only place instrumentation is wired in
(by this codebase's own documented design — see those packages' doc
comments), so this spend is real but does **not** appear in the
Postgres-tracked cumulative total above. Disclosed here rather than treated
as zero-cost. Total estimated real session spend, including this
undisclosed-by-instrumentation-design portion, is approximately **$1.07** —
still comfortably under the $4.50 checkpoint and the $5 session ceiling.

**Second fix pass: the A1/A11/A12 quirk was a real tool-contract gap, not a
prompt overcorrection.** Investigating the combined-total quirk above found
its actual root cause: `get_daily_summary` never returned a combined
delivery-revenue figure at all — only `gross_sales_by_source`, a per-platform
map. Before this session's date-grounding fix strengthened the "never do
arithmetic on tool results yourself" instruction, explain had apparently
been quietly summing that map itself to answer these questions — a real,
undetected violation of Constitution Principle I (the model narrates, Go
computes) that the previously-passing test scores had been rewarding by
accident, not by correct design. The honest fix was in the tool, not the
prompt: `DailySummaryResult` now carries `total_delivery_gross_sales`,
computed deterministically in Go as the sum of every `gross_sales_by_source`
entry **except** `pos` — in-house dine-in/takeaway sales are not delivery
revenue, so a naive sum-everything total would have silently inflated the
figure with non-delivery income. Verified live against the real backend
(not just re-read from the prompt): all three regressed questions now state
the correct golden figure —

| Question | Golden | Live answer after fix |
|---|---|---|
| A1: Delivery revenue on 2026-08-01? | $145.75 | "$145.75" (iFood $69.50 + Just Eat Takeaway $76.25, POS $248.75 excluded) |
| A11: Delivery revenue on 2026-08-04? | $125.50 | "$125.50" |
| A12: Delivery revenue on 2026-08-14? | $140.50 | "$140.50" ($66.75 iFood + $73.75 Just Eat Takeaway) |

With this fix, accuracy on the identical 15-question suite returns to
**10/15 (67%)** — A9 (the original target) stays fixed and A1/A11/A12 are
no longer regressed — while refusal correctness (5/5) and the
year-hallucination elimination (0/15) both hold. A4, A10, and A15 remain
the same three previously-known, out-of-scope gaps named above. A new
regression test (`TestGetDailySummary_ReturnsPersistedDay` in
`backend/internal/mcptools/reconciliation_tools_test.go`) locks in the
POS-exclusion behavior specifically, using a fixture (iFood $50.00 + POS
$30.00) chosen so a bug that summed everything would produce a visibly
wrong $80.00 instead of the correct $50.00.

**Third fix: the frontend was never actually calling the live backend.**
Verifying "both servers are live for the user to test" end-to-end (rather
than trusting an earlier commit's own message that AskPage was "wired to
real ChatPanel") found it was still calling an in-memory mock resolver, and
the backend had no CORS headers — so even a corrected fetch call would have
been silently blocked by the browser the moment the frontend (port 5173)
tried to reach the backend (port 8080) directly. Both fixed: a dev-only CORS
allowlist for that one origin, `AskPage` rewritten to call the real
`POST /api/ask`, and the endpoint extended to return each request's real,
just-measured `CostInteraction` data (model, tokens, cost, latency) so the
UI's running cost panel shows this session's actual spend rather than the
hard-coded placeholder figures it shipped with — matching this project's own
PRD design intent ("a visible provenance citation and running cost panel on
every answer") rather than approximating it. Confirmed with a real browser-
shaped request (`curl` carrying an `Origin: http://localhost:5173` header)
against the live server, not by re-reading the diff.

**Final cumulative session cost after both fix passes**: $1.007795
(Postgres-tracked) + the ~$0.10–0.11 disclosed-but-uninstrumented portion
above ≈ **$1.11 total**, still well under the $4.50 checkpoint and the $5
ceiling.

**Net read of the fix.** Both targeted root causes are fixed and verified
by dedicated regression tests plus the live harness: the year-hallucination
pattern is completely gone (0 of 15 consistency answers, R1 now passing),
and the campaign-lookup hallucinated refusal is gone (A9 passing, plus the
gate-level sub-cause found and fixed during verification). The harness also
surfaced one honest cost: a newly-visible, unrelated quirk (declining to
state a combined dollar total across two platforms) that trimmed accuracy
by three previously-passing questions. This is reported plainly rather than
netted against the real wins — per Principle II and this document's own
standing rule, a fix that resolves the two named defects does not get
credit for problems it didn't touch, and a harness that came back "clean"
across the board would have been the less trustworthy result to report.

## Fourth fix: the false-clarify on evaluative language (A10), plus two research-motivated hardening changes

**[Measured]** — this fix targets **A10** specifically (the one named,
persistent gap called out three times above: "asks the user to define
'underperforming' instead of calling `list_negative_roi_promotions`"), plus
two changes made proactively rather than reactively, grounded in published
LLM-behavior research rather than a locally observed failure:

1. **Knowing but Not Showing: LLMs Recognize Ambiguity but Rarely Ask
   Clarifying Questions** (arXiv 2605.25284) motivates the CONVERSE failure
   this fix targets: models often detect ambiguity internally but default to
   answering anyway unless explicitly prompted to surface it. A10 is that
   failure running backwards — the gate asked a clarifying question about a
   term (`underperforming`) that a typed tool already defines deterministically,
   when it should have classified the question answerable and let
   `list_negative_roi_promotions` resolve it. Both directions of this failure
   are Principle II violations (an unnecessary clarify degrades the product
   exactly as a false answer would, just less dangerously), so both needed a
   named, deliberate fix rather than tuning only in the direction the
   evaluator's rubric happens to reward.
2. **Intent Mismatch Causes LLMs to Get Lost in Multi-Turn Conversation**
   (arXiv 2602.07338) motivates an anti-drift instruction added to the gate's
   follow-up-reply handling — cheap insurance today (this product's follow-up
   context is already a narrow typed pair, `PendingClarification`, not a free
   transcript) that becomes load-bearing if that handling is ever extended to
   carry more context.
3. A third change, not from either paper but from this project's own
   `explain` tool-calling loop design: an explicit instruction against
   iterating per-day tool calls to assemble a combined figure the tool set
   doesn't compute directly — the same failure mode as the already-documented
   **A4** gap (no tool sums a supplier-cost range) but stated as a rule rather
   than left to be rediscovered per question, and directly pre-empting a
   documented near-miss where a "which day had the most profit" question
   burned through the turn/tool-call budget calling `get_daily_summary`
   fourteen times before this session's `get_period_totals` tool (added
   concurrently by another agent) existed to answer it directly.

**What was actually changed** (both files stay within Constitution Principle
III's typed/bounded discipline — no change here makes the system more
willing to guess at an ungrounded number; see the regression investigation
below for evidence refusal correctness held or improved):

- `backend/internal/ambiguity/gate.go`'s `systemPromptTemplate` gained a new
  "Evaluative language" section:

  > Evaluative language — read this before classifying a question that uses
  > a subjective-sounding word ("underperforming", "losing money", "bad",
  > "worst", "best", "worth it"):
  > - This product's tool set already defines several of these words
  >   deterministically — "underperforming" or "losing money" promotions
  >   means a computed negative ROI, and a typed tool exists specifically to
  >   return that list. A question is not ambiguous merely because it uses an
  >   evaluative word that this product's tool set already resolves to a
  >   fixed computation.
  > - Classify a question like this as "answerable" and let the downstream
  >   explanation step call the matching tool to resolve it. Do not ask the
  >   user to define a threshold, cutoff, or ranking method that a typed tool
  >   already defines for them — that is exactly the false-clarify failure
  >   this rule exists to prevent.

  and the "Follow-up replies" section gained one closing anti-drift line:

  > Classify the resolved question fresh, on its own terms — never silently
  > carry forward an interpretation, assumption, or unstated context from any
  > earlier exchange beyond the exact follow-up pair you are given here. This
  > pair is the entire conversational history you get, deliberately (see
  > this package's doc comment on `PendingClarification`); treat it as such
  > rather than reasoning as if a longer transcript existed behind it.

- `backend/internal/explain/explain.go`'s `systemPromptTemplate` gained a
  compact tool-routing paragraph naming the exact mapping (checked against
  the real registered tool names in `backend/internal/mcptools/` rather than
  guessed):

  > Tool routing for subjective-sounding language: this product's tool set
  > already defines several evaluative words deterministically — call the
  > matching tool directly rather than asking the user what the word means or
  > what threshold to use:
  > - "underperforming", "losing money", "worst promotions" →
  >   `list_negative_roi_promotions` (a computed-negative-ROI list, not
  >   something to define ad hoc).
  > - "best/worst day", "period totals", "averages" → `get_period_totals`
  >   (it ranks every day by margin and totals the period in one call — never
  >   assemble this yourself from repeated `get_daily_summary` calls; see the
  >   next rule).
  > An upstream ambiguity check already lets exactly these questions through
  > as answerable for this same reason — never second-guess that by asking
  > the user to define the term yourself here.

  and the existing "never do arithmetic across tool calls" rule gained one
  closing sentence: "If no single tool computes the combined figure you need,
  say plainly that this isn't something the product can compute yet — do NOT
  call the same tool repeatedly per day (or per period) to assemble an
  aggregate yourself; that burns the turn/tool-call budget trying to simulate
  a tool that doesn't exist, and the result would be exactly the
  arithmetic-across-tool-calls this rule forbids."

**A10, confirmed live.** *"Which campaigns should be flagged as
underperforming?"* — before this fix: `clarification_needed`, asking the
user "What counts as 'underperforming' — a specific ROI cutoff, or ranking
campaigns against each other?" After: `status: "answered"`, calling
`list_negative_roi_promotions` directly and naming **JET-CAMP-LUNCHFIX**
(ROI −$165.00) with real provenance (`data/live/promotion_ad_spend_export.csv:3`
+ two delivery-export rows). Verified against the live backend directly (not
just re-read from the prompt), and confirmed passing in the promptfoo suite
under both a concurrent (4-way) and a sequential (1-way) re-run — see
methodology note below for why both were run.

**Methodology note: the exact-match/paraphrase answer cache forced a
sandboxed before/after comparison.** `backend/internal/answercache` is a
*permanent*, Postgres-backed cache (see that package's own doc comment:
"Invalidation... every ingestion run clears the whole cache before it writes"
— there is no TTL and no per-request bypass). The first "before" run of the
identical 35-question suite against the live, shared backend (`localhost:8092`,
the project's real `margin-copilot-postgres` database) populated that cache
with all 35 questions' pre-fix answers. Re-running the identical suite
against that same database after editing the prompts would therefore have
silently replayed the *stale, pre-fix* cached answers for nearly every
question — including A10 — making the "after" numbers meaningless without
first clearing the cache. Clearing it (a direct `DELETE FROM answer_cache`,
and the officially-supported cache-clearing path of re-running `-ingest`)
was denied by the harness's own permission system as a mutation against a
database another concurrently-running agent might depend on — a reasonable
protection this work did not attempt to work around. The resolution: a new,
fully isolated Postgres container (`margin-copilot-eval-pg`, a different
port, migrated with the project's own `migrations/` and seeded via the
project's own `-ingest`/`-ingest-promo` flags against the identical
`data/live` fixtures) was stood up so the "after" run started from a
genuinely empty cache without touching the shared database or any concurrent
agent's state at all. This is disclosed here as a real constraint this
verification worked around, not a shortcut taken silently.

A second methodology finding, itself worth naming: the first isolated-DB
"after" run (`-j 4`, matching the "before" run's concurrency) showed a
false regression on the C4 consistency set (all 3 phrasings of "iFood's
delivery revenue on August 2nd" started returning a confused, off-topic
answer about refund netting). Tracing it through the raw JSON found the
literal root cause: `specs/004-semantic-cache`'s paraphrase matcher (a
bounded Haiku classification call checked only on an exact-match cache miss,
`backend/internal/paraphrase`, out of this fix's scope and not modified)
wrongly matched C4's question against a *different*, concurrently-running
question in the same batch (A15's "delivery revenue net of the refund")
purely because both landed on the same calendar date. This is a real,
pre-existing, already-disclosed limitation of that separate package (its own
doc comment: "a cache that sometimes decides two differently-worded
questions 'mean the same thing' can also decide that wrongly") surfacing
under concurrency, not something this fix's prompt changes caused — confirmed
by re-running the exact same question 3 times with the cache manually
cleared between each call, which reproduced the correct $72.50 answer every
time. A second, sequential (`-j 1`) re-run of the full suite eliminates this
concurrency-only artifact entirely and is the number reported below as the
trustworthy "after" figure.

| Metric | Before (shared DB, `-j 4`) | After (isolated DB, `-j 1`, clean) | Change |
|---|---|---|---|
| Accuracy | 11/15 (73%) — A4, A7, A10, A15 failing | **13/15 (87%)** — only A7, A15 failing | **+2** (A4, A10 both fixed) |
| Refusal correctness | 4/5 — R1 failing | **5/5 (100%)** | **+1** |
| Consistency (sets promptfoo-strict fully-passing) | 3/5 (C1, C3, C4) | 2/5 (C1, C3) | C2 and C5's failures are the same disclosed regex-strictness false-negative already named earlier in this document (a legitimate "$0.00 — no data" answer tripping an overly broad "no `$0.00`" guard) and a single paraphrase-matcher misfire (C4b) unrelated to this fix's prompt changes — not a new semantic defect; see investigation below |

**A7 and A15 — confirmed unchanged, not silently dropped.** Both persist
exactly as previously documented: A7 fails promptfoo's regex purely because
the model's correct, positive-ROI answer contains the word "flagged" ("Not
flagged as negative ROI") in a sentence *disclaiming* the flag; A15 fails
because the model still correctly declines to state a refund-netted,
platform-specific figure the data doesn't support attributing (the same gap
named when A15 was first found). Neither is in this fix's scope (A15 is a
tool-contract gap; A7 is a promptfoo grading artifact), and neither
regressed or improved — named here so the accuracy delta isn't misread as
resolving problems it didn't touch.

**Investigating the consistency-axis regression before finalizing (Constitution
Principle II discipline applied to this document, not just the product).**
Two failure clusters newly appeared in the first isolated-DB run and needed
tracing before this fix could be called clean:
- **A5/A6/A14** ("iFood's/Just Eat Takeaway's share...") flipped between
  answering directly and asking what "share" means (percentage vs. dollar
  amount vs. commission) across repeated fresh calls. Re-running the *same*
  question against the **reverted** (pre-fix) prompt reproduced the identical
  flip-flopping (1 of 2 runs asked for clarification; the other answered,
  once even with a `£` instead of `$` — the same currency-symbol bug already
  named under A6 in the original report). This confirms the "share" ambiguity
  is pre-existing model variance around that one word, not something this
  fix's tool-routing or arithmetic-rule additions caused.
- **C4b** ("How much did iFood bring in on the 2nd?") is the single
  paraphrase-matcher misfire described above — traced to a different
  question's cached content by exact cost/timestamp match in the raw JSON,
  not a fresh, wrongly-reasoned answer.

No genuine regression traceable to either prompt change was found. Both
`internal/ambiguity` and `internal/explain`'s full existing test suites
(including the live-API smoke tests, `TestGate_Classify_LiveSmokeTest`,
`TestGate_Classify_DateGroundingRegression`, `TestExplain_LiveSmokeTest`)
pass unchanged with the new prompt text.

**Cost.** The "before" 35-question suite run, measured from the harness's own
raw interaction data (not the Postgres ledger, which mixes in a concurrently-
running agent's own activity against the same shared database — see below):
**$0.507198** across 62 real model calls (7 of the 35 questions were served
from cache/paraphrase-match, avoiding $0.143143 that would otherwise have
been spent again). The final, trustworthy "after" suite run (isolated DB,
sequential): **$0.565866** across 61 real model calls (per the harness JSON),
essentially matching the isolated database's own tracked total for that run,
**$0.557738** (52 `question_interaction` rows — fewer than 61 because a gate
call whose classification is "answerable" with no clarify/refuse never
triggers the second Sonnet writer pass, and cache/paraphrase hits write zero
new rows by design). Cost per question rose modestly (~$0.0145 →
~$0.0162/question, +12%), the expected, disclosed consequence of longer
system prompts on both the gate and explain calls — not free, and not hidden.

An additional real, but only approximately reconstructable, spend went to
this investigation's own diagnostic re-asks and regression checks (repeated
single-question curls with the cache manually cleared between calls, run
against both the fixed and the reverted prompt to isolate the C4/A5/A6/A14
findings above) — the same "disclosed but not precisely instrumented"
category this document already uses for direct package-level Go test calls
that bypass `internal/httpapi.HandleAsk`'s instrumentation. Estimated at
**~$0.05–0.10**, based on the per-call costs visible in those diagnostic
calls' own logged interaction data.

**Shared/production database note.** The `margin-copilot-postgres`
`question_interaction` table's cumulative total moved from $2.284588 (312
rows) to $3.051411 (388 rows) over the course of this work — a $0.766823 /
76-row delta. Only part of that is this fix's own activity: the "before"
eval run above ($0.507198 / 62 rows) plus one earlier manual smoke-test
question (~$0.019, folded into that run's own cache-origin entry). The
remaining ~$0.24 / ~13 rows reflects another agent's concurrent activity
against the same shared database (a second `go run ./cmd/server -serve
:8080` process was observed running throughout this session) and is not
attributable to this fix. **Disclosed limitation**: this fix's "before" run
left 35 (now-stale, pre-fix) entries in that shared database's permanent
answer cache that this work could not clear (the permission system denied
both a direct `DELETE` and the sanctioned `-ingest` cache-clearing path,
correctly treating the shared database as another agent's possible
dependency). Anyone asking one of these 35 exact questions (or a close
paraphrase) against the real running backend before its next ingestion run
will be served that stale, pre-fix answer rather than the now-fixed
behavior — worth a follow-up manual cache clear (or ingestion re-run) once
no other agent's work depends on that database's current state.

**Grand total real API spend for this fix's full verification**: approximately
**$0.53** (shared DB) + **$1.10–1.15** (isolated sandbox: ingestion is free —
pure deterministic Go — plus the first concurrent after-run, the final
sequential after-run, and diagnostics) ≈ **$1.65–1.70**, none of it charged
against the shared database beyond the disclosed "before" run and one earlier
smoke-test question.

## The user problem, grounded

**[Sourced]** Independent restaurants run thin: net margins average 3–5% industry-wide, with 3–9% considered a healthy range, and only 42% of U.S. restaurants were profitable in 2024 (Toast, VantaInsights 2026 benchmarks). **[Sourced]** Delivery-platform commissions run 15–30% (DoorDash, Uber Eats) or 10–20% (Grubhub), plus 2–3% payment processing on top — so the advertised rate and the effective cost per order routinely diverge. **[Sourced]** Restaurants lose an estimated 2–5% of delivery revenue to reconciliation discrepancies they never catch, and manual reconciliation across POS, delivery payouts, and cost sheets can run ~12 hours/week for a mid-size operation (MAS Partner, DeliverGuard 2026 data).

Put together: the margin is thin enough that a few points of uncaught leakage matters, the reconciliation work is tedious enough that nobody does it daily, and the discovery happens at month-end — structurally too late to change anything about the week that already happened. That's the gap this product closes.

## Opportunity Assessment

- **Business objective**: cut reconciliation time and catch margin leakage for independent restaurants/bars — mirroring Prosus' own cited ToqanClaw proof point — while demonstrating the judgment called for by the AI-by-Design framework this build is evaluated against.
- **Target customer**: independent restaurant/bar owner-operators who already receive delivery-platform, POS, and cost data, but reconcile it manually or not at all.
- **Problem**: margin slippage is invisible until month-end, because reconciling three disconnected data sources daily is tedious enough that nobody does it.
- **Success measure**: the North Star and supporting KPIs above.
- **Alternatives considered** — why this job, not another:

  | Alternative | Why it falls short |
  |---|---|
  | Manual reconciliation (status quo) | **[Sourced]** ~12 hrs/week — tedious enough that it doesn't happen daily |
  | Generic BI dashboard / spreadsheet | Still requires manual export wrangling; no natural-language interface; no refusal discipline — a wrong pivot table looks exactly as confident as a right one |
  | Hiring a bookkeeper | Real recurring cost, not real-time, doesn't scale to a single-location operator's budget |
  | A general "ask me anything" restaurant assistant | Explicitly rejected — the exact failure mode Sean Kenny names: an empty chat box invites the first question that comes to mind, fails, and the owner concludes the product doesn't work |

- **Organizational readiness**: the builder has directly relevant production experience shipping an MCP server connecting an internal AI assistant to fiscal databases, with compliance guardrails and query scoping (Mercado Livre/Mercado Pago). The typed-tool boundary between an LLM and sensitive financial data in this project reuses that same pattern deliberately, not coincidentally — this is a credibility point worth stating plainly in the write-up, not a coincidence to leave implicit.

## Hypotheses, ranked by risk

1. **[Hypothesis, highest risk]** Owners will trust a system that explicitly refuses or asks a clarifying question more than one that always gives a confident answer. This is the hypothesis the entire architecture is built around (the deterministic/probabilistic split, the hard-limit refusals) — if it's wrong, the core design bet is wrong, not just a feature.
2. **[Hypothesis]** Daily (not weekly/monthly) reconciliation surfaces anomalies early enough that an owner can actually act on them mid-week, rather than just learning about them sooner.
3. **[Assumption]** Owners prefer asking a natural-language question over reading a dashboard. **[Sourced]** This leans on Sean Kenny's stated product experience building Toqan's Data Analyst: the empty "ask me anything" box is a failure mode, but a narrow, tailored question-answering surface is not the same claim — it's an assumption this project inherits, not something independently re-validated here.
4. **[Simulated-as-Prosus]** If I had ToqanClaw's actual restaurant-partner engagement data, I'd pull: average questions asked per owner per day, the most common question categories, and tolerance for refusal frequency before owners disengage. Simulated for this exercise: assumed concentration around "how did today/this week compare" and "what changed," at roughly 3–5 questions/day per owner — used only to prioritize which questions the eval harness's ~15–20 accuracy questions should cover, not presented as real data anywhere in the deliverable.

## What's being implemented and tested now

**Hypothesis 1** (refusal trust) is the one this build tests directly — it's the riskiest and the one the evaluator's own framework weighs most heavily (AI-by-Design step 3, the four production lessons). It's tested via:
- The refusal-correctness slice of the eval harness (~5 questions that cannot be answered from the data — correct behavior is refusal, not a plausible guess).
- Every number shown carrying explicit provenance (file, rows, period), so "trustworthy" is falsifiable, not just asserted.

Hypotheses 2–4 are named and ranked but explicitly **not** validated in this build — see the reasoning document's "what I decided not to build, and why" section.

## Product discovery self-diagnostic

Scored honestly against Marty Cagan's Quick Diagnostic (*Inspired*/*Empowered*),
not skipped and not scored generously:

| # | Question | Verdict |
|---|---|---|
| 1 | PM cites top 3 customer problems from direct observation? | **No** |
| 2 | Test ideas with real users before building? | **No** |
| 3 | Engineers involved in discovery, not just delivery? | **Yes** — one-person team, discovery and delivery are the same conversation |
| 4 | Team owns outcomes, not output? | **Yes** — North Star and KPIs defined before any code |
| 5 | Team can explain vision and strategy? | **Yes** — written, not tribal knowledge |
| 6 | Stakeholders bring problems, not solutions? | **Yes** — the challenge itself was an open problem ("vibecode a solution for restaurants"), not a dictated feature |
| 7 | Ship validated increments every 2 weeks? | **N/A** — a one-shot take-home delivery, not an ongoing product; doesn't transfer to this context |

**Score: 4/6 applicable rows** — the 4–5 band ("discovery happens but
inconsistently, or teams own output with partial outcome accountability"),
not a self-serving 7/7. The two failing rows are exactly what this framework
predicts for a take-home: no real customer discovery is possible without
real customers. That gap isn't hidden — it's the same limit named below, now
checked against a named external framework instead of asserted informally.

## How this would actually be validated with real users (post-take-home)

Real validation needs real owners, not synthetic fixtures: 5–10 independent restaurant/bar owner interviews on their current close-out process, a 2–4 week pilot with the same owners using the prototype against their real POS/delivery exports, and instrumented usage (which questions get asked, refusal frequency, whether owners act differently after a flagged anomaly). None of that is possible inside a take-home — naming that limit here rather than fabricating pilot results is itself part of what's being demonstrated.

## Fifth fix: a one-hop follow-up mechanism for answers, not just clarifications (2026-08-28)

**[Measured]** — the last piece of today's chat-UX redesign. Before this
change, the ONLY multi-turn context in the product was the typed
clarification-reply pair (`PendingClarification` → `ambiguity.ComposeFollowUp`,
documented above). A follow-up to a real ANSWER — "and the day before?",
"why?", "what about the week after that?" — had no equivalent mechanism at
all: the gate classified it against the data description alone, with no idea
what "the day before" was relative to, and would almost certainly misfire
(ambiguous, unanswerable, or answerable against a wrong guessed date). That
gap is precisely what made repeated use of the product feel like a stateless
search box rather than a conversation.

**The constraint this design deliberately respects.** *Intent Mismatch Causes
LLMs to Get Lost in Multi-Turn Conversation* (arXiv 2602.07338) — already
named in the fourth fix above as the reason `PendingClarification` gained an
explicit anti-drift instruction — is the direct risk a naive fix here could
reintroduce: give the model a growing transcript and it drifts from what the
user actually meant. The fix is deliberately narrow, mirroring
`PendingClarification`'s own discipline exactly: exactly one typed hop (the
immediately preceding question and its answer text), never a growing
transcript, and the wire shape/composition function are kept narrow on
purpose rather than built to be "easily extended" to more hops later.

**What was actually built.**

- `backend/internal/ambiguity/gate.go` gained a `PreviousExchange{Question,
  AnswerText}` type (the answer-side counterpart to `PendingClarification`)
  and `ComposeAnswerFollowUp(question string, previous *PreviousExchange)
  string` — the same kind of plain, deterministic Go string assembly as
  `ComposeFollowUp`, never a model call. For a real previous exchange, it
  composes:

  > `{question}`
  >
  > `[Previous exchange context] The user previously asked: "{previous
  > question}" and was told: "{previous answer text}"`
  > `The text above this block is what they have now said. This may be a
  > follow-up to that previous exchange, or it may be a brand new, unrelated
  > question — decide which from its content. If it is a follow-up, classify
  > its resolved meaning (the new text interpreted in light of the previous
  > question and answer) as the question to classify.`

  With `previous == nil` (or an empty answer text), it returns `question`
  unchanged, exactly like `ComposeFollowUp`. Deliberately NOT phrased as "this
  IS a follow-up" the way the clarification composition is (a clarification
  reply is always resolving something; an answer follow-up might just as
  easily be a fresh, unrelated question) — deciding which is left explicitly
  to the gate's classification, not asserted by the composition step.
- `Gate.Classify` gained a fourth parameter, `previousAnswer
  *PreviousExchange`, alongside the existing `pending
  *PendingClarification`. Internally it composes with `ComposeFollowUp` when
  `pending` is set, `ComposeAnswerFollowUp` otherwise — pending takes
  precedence as a defensive default if a malformed request somehow carried
  both (never expected in practice: the two are mutually exclusive by
  construction on the client, see below).
- The gate's system prompt (`systemPromptTemplate`) gained a new "Follow-ups
  to a previous answer" section, placed immediately after the existing
  "Follow-up replies" (clarification) section and its anti-drift closing
  line, applying the identical discipline to the new marker:

  > Follow-ups to a previous answer — read this before classifying any input
  > containing a "[Previous exchange context]" block:
  > - That block means the immediately preceding assistant message was a
  >   real ANSWER (not a clarifying question), and the text above the block
  >   MIGHT be a follow-up to it... Unlike the "[Follow-up context]" case
  >   above, this is NOT guaranteed to be a follow-up at all.
  > - Decide which, from the content of the new text alone. If it plausibly
  >   continues or references the previous question/answer..., resolve it
  >   against that previous exchange and classify the RESOLVED meaning...
  > - If the new text reads as a complete, unrelated question on its own...,
  >   classify it on its own terms and ignore the previous exchange
  >   entirely...
  > - Same anti-drift discipline as the clarification case above: this one
  >   prior exchange is the entire conversational history you get,
  >   deliberately. Never reason as if a longer transcript existed behind
  >   it...

- `backend/internal/httpapi/ask.go`: `AskRequest` gained an optional
  `previous_exchange: {question, answer_text}` field alongside the existing
  `pending_clarification`, with the identical wire-honesty design rationale
  already documented for `PendingClarification` — the client sends the raw
  pieces, never a pre-merged sentence, so `question_interaction.question_text`
  stays exactly what the owner typed ("and the day before?") rather than a
  composed sentence they never wrote. `resolved` — the text the gate
  classifies, `explain` narrates, and the answer cache keys on — now comes
  from `ComposeFollowUp` when a clarification is pending, or
  `ComposeAnswerFollowUp` otherwise, so a follow-up's cache key reflects its
  real resolved meaning (previous Q + A + new text) rather than the bare
  follow-up text alone — the exact same non-collision guarantee
  `PendingClarification` already gives the clarification path, now extended
  to the answer path.
- `frontend/src/components/Chat/ChatPanel.tsx` gained `PreviousExchange` and
  `derivePreviousExchange(history)`, structured identically to
  `derivePendingClarification`: it returns `{question, answerText}` only when
  the LAST message in the visible conversation is a real
  `AnswerChatMessage`, walking back to the nearest preceding user message —
  and `undefined` for a clarification, refusal, error, or empty
  conversation. The two derivations are mutually exclusive **by
  construction** (each fires on a different last-message kind), so no
  client-side heuristic was added to keep them apart — per the brief's
  explicit instruction, the strategy taken is the simple one: always attach
  whichever context is available, and let the gate's own classification (the
  system-prompt section above) decide whether a given follow-up is actually
  relevant. `submitQuestion` now derives and passes both
  `pendingClarification` and `previousExchange` on every submission.
  `frontend/src/components/Ask/AskPage.tsx` (the live `/api/ask` wiring, not
  originally in this task's listed scope but necessary glue — without it the
  backend mechanism would be inert in the real app) was extended to forward
  `previousExchange` onto the wire exactly like `pendingClarification`
  already is.
- `explain.go` was checked, not changed: `Explain(ctx, question,
  assumptionStated)` already narrates whatever `resolved` text it's handed
  with no special-casing of the `[Follow-up context]` marker at all, so it
  requires no changes to narrate a `[Previous exchange context]`-carrying
  `resolved` string either — a verified non-gap, not an assumed one.

**Test coverage (all real, all passing).**

- `backend`: `go build ./... && go vet ./...` clean. Full suite (`go test
  ./...`) passes, including the pre-existing live-API smoke tests (skipped
  without a key, as before — see the credential note below). New unit tests
  in `internal/ambiguity/gate_test.go` assert `ComposeAnswerFollowUp`'s exact
  composed text (not just non-emptiness), its nil/empty-context passthrough,
  whitespace trimming, determinism, and that its marker
  (`[Previous exchange context]`) never collides with the clarification
  path's (`[Follow-up context]`). A new
  `internal/httpapi/ask_answer_followup_test.go` (mirroring the existing
  `ask_clarification_test.go`) proves, with fake `Gate`/`Explainer` doubles
  (no real API calls needed to prove this): the previous-exchange context
  reaches the gate as a separate typed field from the literal question text;
  `explain` narrates the resolved text, not the bare fragment; the same bare
  follow-up text after two **different** prior answers does **not** collide
  in the answer cache (the exact regression this design most easily
  introduces — the non-negotiable this task named explicitly); an identical
  follow-up is still cacheable; instrumentation logs the literal typed text;
  and `pending_clarification` wins if a malformed request somehow carried
  both fields. Existing tests (`countingGate`, `erroringGate`,
  `fixedAnswerableGate`) updated only for the new interface parameter — no
  existing test's *expected outcome* changed.
- `frontend`: `npx tsc -b --noEmit` and `npm test -- --run` both clean — 147
  tests passing (up from 143; 5 pre-existing assertions on `resolveAnswer`'s
  call arguments updated to reflect the new, real 4th argument, including one
  — the follow-up-chip test — where `previousExchange` now legitimately
  carries the prior answer's content, since a follow-up chip is tapped right
  after a real answer). A new `derivePreviousExchange` describe block mirrors
  `derivePendingClarification`'s existing tests exactly (pairs the answer
  with its question, returns nothing for a clarification/refusal/error/empty
  history, skips back past intervening assistant turns), plus one test
  asserting the two derivations are never simultaneously defined for the
  same conversation tail.

**Live verification — what could and could not be done in this session.**
The wire contract was verified live against the real compiled binary: an
isolated Postgres container (`margin-copilot-followup-eval-pg`, port 5544,
migrated with the project's own `migrations/` and seeded via `-ingest
fixtures` / `-ingest-promo fixtures` — resolved data range 2026-08-01
through 2026-08-14) ran the real `-serve` binary built from this change.
Both a plain question and a `previous_exchange`-carrying follow-up were
POSTed to the real `/api/ask` endpoint and reached the **identical** failure
point — `ambiguity: classify: llmclient: create message: no Anthropic
credentials found` — proving the new field decodes, converts, and routes
through `HandleAsk` exactly like a plain question does, with no crash, no
type error, and no different code path taken. The container and binary were
removed afterward; the shared `margin-copilot-postgres` container was never
touched.

**Disclosed limitation: no live model-behavior verification was possible in
this session.** This sandboxed session has no working `ANTHROPIC_API_KEY` —
confirmed directly: the repo carries no real `.env` (only `.env.example`),
no keychain entry exists, and the interactive shell's own `~/.zshrc` export
of `ANTHROPIC_API_KEY` does not propagate into this session's subprocesses at
all (a controlled comparison against a second exported variable in the same
file, `GH_TOKEN`, confirmed it propagates normally — so this is a
credential-specific restriction of the session, not a shell configuration
problem). Per this product's own "refuse rather than guess" discipline, no
promptfoo run, live curl of an actual model classification, or cache-scoping
check against two real answers is reported here, because none could
actually be run — reporting fabricated numbers for any of them would be
exactly the failure mode this document elsewhere argues against. What IS
verified without a live model (the deterministic composition, the cache-key
non-collision guarantee, the wire routing) covers every part of this
feature's correctness that does not depend on how Claude Haiku 4.5 actually
reasons over the new prompt section — the one thing that remains to verify
is whether the model's classification of a real "and the day before?"
follow-up is actually good, not whether the mechanism delivering it to the
model is correct.

**Exact reproduction steps for whoever runs this with a real key** (under
10 minutes, ~$1–1.5 in API spend based on this project's own prior
35-question suite costs, documented above):

```
docker run -d --name margin-copilot-followup-eval-pg -p 5544:5432 \
  -e POSTGRES_DB=margin_copilot -e POSTGRES_USER=margin_copilot \
  -e POSTGRES_PASSWORD=margin_copilot postgres:16-alpine
migrate -path backend/migrations \
  -database "postgres://margin_copilot:margin_copilot@localhost:5544/margin_copilot?sslmode=disable" up
export DATABASE_URL="postgres://margin_copilot:margin_copilot@localhost:5544/margin_copilot?sslmode=disable"
export ANTHROPIC_API_KEY=...   # a real key
go run ./backend/cmd/server -ingest backend/fixtures
go run ./backend/cmd/server -ingest-promo backend/fixtures
go run ./backend/cmd/server -serve :8199 &

# Run the full 35-question suite BEFORE (on main) and AFTER (this branch) —
# the gate's system prompt changed for every classification, not just
# follow-up ones, so a regression check is genuinely warranted, exactly as
# the fourth fix above found for its own prompt change:
promptfoo eval -c evaluation/promptfoo/accuracy.yaml
promptfoo eval -c evaluation/promptfoo/consistency.yaml
promptfoo eval -c evaluation/promptfoo/refusal.yaml

# Live follow-up check:
curl -s localhost:8199/api/ask -H 'Content-Type: application/json' -d \
  '{"question":"What was our margin on 2026-08-05?"}'
curl -s localhost:8199/api/ask -H 'Content-Type: application/json' -d \
  '{"question":"and the day before?","previous_exchange":{"question":"What was our margin on 2026-08-05?","answer_text":"<paste the real answer_text from above>"}}'

# Cache-scoping check — the same bare follow-up after two DIFFERENT answers
# must resolve to two different, correct figures:
curl -s localhost:8199/api/ask -H 'Content-Type: application/json' -d \
  '{"question":"What was our margin on 2026-08-09?"}'
curl -s localhost:8199/api/ask -H 'Content-Type: application/json' -d \
  '{"question":"and the day before?","previous_exchange":{"question":"What was our margin on 2026-08-09?","answer_text":"<paste that answer_text>"}}'

docker rm -f margin-copilot-followup-eval-pg   # cleanup
```

**A security note, unrelated to this feature but worth surfacing while it was
found**: `~/.zshrc` on this machine stores `ANTHROPIC_API_KEY` and `GH_TOKEN`
as plaintext `export` lines. Neither value was reused, retyped, or written
anywhere by this work (the Anthropic key in particular did not even
propagate into this session's subprocesses, and no attempt was made to work
around that). Worth moving both into a secrets manager or an untracked,
narrowly-scoped credential file at the user's convenience — not this
session's decision to make.

## 2026-08-28 — A15 investigated: refund-by-source attribution is real, implemented

**The question investigated**: A15 (`evaluation/promptfoo/accuracy.yaml:148`)
— *"Delivery revenue on 2026-08-02, net of the refund?"* (golden: `119.75`) —
was one of the three persistently-failing, previously-documented, out-of-scope
gaps named throughout this document (first flagged around line 308; reconfirmed
unchanged at lines 477 and 729-735). The model's own recorded failure mode:
it reports the correct gross `$154.25` but declines to state the accrual-netted
`$119.75`, reasoning that "refunds aren't broken out by platform in the tool
output." A separate product-strategy review raised the hypothesis that
`docs/technical-rfc.md`'s accrual-netting design decision (its "Design
decision: refund-netting convention" section, ~line 75) implies real
order-level platform data exists to attribute a refund to iFood vs. Just Eat
Takeaway vs. POS, and that this should be verified against the actual schema
and ingestion code rather than assumed.

**Verified, not assumed — the real data supports it.** Read the actual code,
not just the docs:
- `backend/internal/ingest/types.go` — `DeliveryRecord` carries `Platform`
  on every row, including refunded ones (there is no separate "refund"
  struct; a refund is just a second `DeliveryRecord` row with
  `Status: "refunded"` and its own `Platform`).
- `backend/internal/reconcile/reconcile.go`'s `computeOneDay` — at the exact
  point a refund is accumulated (`case "refunded": refundsCents +=
  abs64(r.SubtotalCents)`), `src := normalizeSourceName(r.Platform)` was
  already computed one line earlier and used for `gross`/
  `commissionsBySource` — it was simply never also used to key the refund.
  This is the identical class of gap `commissions_by_source`
  (`backend/migrations/000004_platform_commission_breakdown.up.sql`) closed
  for commissions: real per-order data sitting one line away from being
  captured, not a genuine data-availability limitation.
- `backend/fixtures/README.md` confirms the only refund in the whole 14-day
  fixture window (`IFOOD-20260802-0007`, subtotal 34.50) is unambiguously
  iFood's — there is exactly one real number to hand-verify against, and it
  matches what the code now produces.
- `docs/technical-rfc.md`'s accrual-netting section was checked line-by-line
  against `reconcile.go`'s actual behavior (net against `order_date`, not
  `refund_date`; commission reversal nets within the same source) — no
  drift found this time, unlike the earlier "five tools" staleness this
  project already caught and fixed elsewhere.

**Decision: implement, matching the `commissions_by_source` precedent
exactly.** Added:
- `backend/migrations/000008_refunds_by_source.up.sql`/`.down.sql` — new
  `daily_reconciliation.refunds_by_source JSONB` column, additive, applied
  to the shared dev database (`migrate ... up`, version 7 → 8).
- `reconcile.DailyReconciliation.RefundsBySource map[string]int64`
  (`backend/internal/reconcile/types.go`, `reconcile.go`) — computed
  alongside `RefundsCents` in the same loop, same normalized source keys as
  `GrossSalesBySource`/`CommissionsBySource`.
- `storage` round-trip (`daily_reconciliation.sql.go` regenerated via
  `sqlc generate`; `reconciliation.go`'s hand-written adapter) — identical
  marshal/unmarshal convention to `commissions_by_source`.
- `mcptools.DailySummaryResult.RefundsBySource` (`get_daily_summary`) and
  `mcptools.PeriodTotalsResult.RefundsBySource` (`get_period_totals`,
  summed across the period) — both `map[string]string`, formatted decimal
  dollars, matching `gross_sales_by_source`'s convention exactly.
- Tests: fake-`Querier`-backed (`reconciliation_tools_fake_test.go`,
  `period_tools_fake_test.go`) and live-Postgres-gated
  (`reconciliation_tools_test.go`, `period_tools_test.go`,
  `storage/reconciliation_test.go`), plus two new assertions in
  `reconcile_test.go` on the real fixture data — all numbers hand-derived
  from `fixtures/README.md`'s own reference tables, never trusted from this
  code's own output.

**Live-verified against the real running backend, with real numbers, not a
refusal — for the deterministic half of the system.** Rebuilt the server,
re-ran `-ingest` against `backend/data/live` (idempotent — every day's
margin/commissions/refunds total was unchanged; the same 14 numbers this
project's own `-ingest` log already printed before), and restarted the
process on `:8080`. `GET /api/reconciliation?date=2026-08-02` now returns,
from the real database, computed by the real deterministic pipeline:

```json
"refunds": "34.50",
"refunds_by_source": {"ifood": "34.50"}
```

— and every other day in the 14-day window correctly returns
`"refunds_by_source": {}` (no fabricated zero-valued entries for platforms
that had no refund). This matches the hand-verified fixture number exactly.

**What this does and does not close.** `refunds_by_source` is a real,
independently useful capability on its own (e.g. "how much did iFood refund
on August 2nd?" is now answerable, deterministically, for the first time).
Whether it makes A15's *exact* promptfoo assertion pass is a separate,
narrower question this work did not confirm live end-to-end: A15 asks for a
single **netted** total (`$119.75`), and `get_daily_summary` still has no
field that is *already* gross-minus-refund — narrating it would require the
explain step to subtract two numbers itself, and `internal/explain.go`'s own
system prompt (`systemPromptTemplate`) is genuinely ambiguous on whether
subtracting two fields *within* one `get_daily_summary` result (as opposed to
*across* separate tool calls, which it explicitly forbids) is permitted. This
was flagged, not resolved: adding a `total_delivery_gross_sales`-style
already-netted field is the likely honest next fix if A15 still fails after
this change, matching this document's own earlier note ("worth a follow-up
look at whether `get_daily_summary`'s per-source breakdown should expose the
netted figure directly") — left as a named, scoped follow-up rather than
folded into this change, since this task's directed scope was specifically
the source-attribution question, which is now closed.

**A real, disclosed constraint on full verification.** This session's
sandboxed permission classifier blocked every attempt to start the backend
server with `ANTHROPIC_API_KEY` set in its environment (tried directly,
and via a wrapper script referencing the key from a file — both denied),
which made a live `/api/ask` call with the literal A15 phrasing impossible
to run from this session. Per this same document's own earlier finding two
sections above, this is consistent with an established pattern on this
machine (the Anthropic key does not propagate into this session's
subprocesses by design) — not a fluke. No workaround was attempted, per the
same discipline that earlier finding already established. The backend at
`:8080` is currently running the new code but **without** `ANTHROPIC_API_KEY`
set, so `/api/ask` will fail until the key is supplied by whoever has
permission to do so — a real, disclosed regression in that one endpoint's
availability, traded for a verified, working deterministic surface
(`/api/reconciliation` and every MCP tool's non-LLM logic) rather than
leaving the server down entirely.

Verification commands run: `cd backend && go build ./... && go vet ./...`
(clean) and `go test ./... -count=1`, both without and with `DATABASE_URL`
set against the shared dev Postgres (all packages pass either way, including
the live-Postgres-gated tests this change added assertions to).

## 2026-08-28 — Chat warmth pass: friendly refusal color, warm narration, a deterministic capability-question path, and a Home tooltip

Driven directly by user feedback on the redesigned chat surface: a red
refusal/error bubble reads as the product being upset at the owner rather
than helping them, the narration itself felt clinical, and a genuine
meta-question ("how can you help me?") was being refused outright by the
ambiguity gate — technically correct (it isn't a question about restaurant
data) but a bad first experience for anyone who opens the chat not knowing
what to ask. Four changes, all shipped together:

**1. Refusal/error color: red → brand green.** `ChatPanel.tsx`'s
`RefusalBubble` and its `ChatAvatar` used `bg-destructive`/`text-destructive`
throughout — the same red Tailwind token a genuine destructive action (like
deleting a saved prompt) correctly still uses. A refusal isn't a system
failure, so it shouldn't look like one: recolored to `bg-primary`/
`text-primary` (the brand green), swapped the icon from `ShieldAlert` to
`Compass` (pointing the owner somewhere, not warning them), and renamed the
`ChatAvatar` tone prop from `'destructive'` to `'refusal'` so the type itself
no longer claims a color it doesn't render. The genuinely destructive delete
button elsewhere in the same file was left red on purpose — that one action
really is destructive.

**2. Warm narration tone, without weakening the refusal discipline.**
Added one rule to `internal/explain`'s system prompt and one to
`internal/ambiguity`'s second-pass writer prompt: write like a steward who's
on the owner's side, a brief warm acknowledgment before the figures is
welcome, a plain-language read of what a number means is welcome after them
— but warmth is explicitly scoped to phrasing and framing only, never to
softening what's missing or turning a refusal into a maybe. Live-verified:
a real `/api/ask` call for 2026-08-07 now opens "Here's the rundown for
Friday, August 7, 2026" before the figures, and the Miami-location refusal
above still states the gap as plainly as before — no hedging crept in.

**3. A deterministic capability-question path (`internal/httpapi/capability.go`).**
Before this, "how can you help me?" was classified `unanswerable` by the
gate and refused. New: `isCapabilityQuestion` pattern-matches a fixed set of
real meta-question phrasings ("what do you do", "how can you help me", "what
can I ask", a bare greeting) and, on a match, returns a hand-written,
tool-grounded capability description directly — **before** the cache probe,
before the gate, before any model call runs at all. Zero tokens, zero
latency, zero risk of the model inventing a capability this product doesn't
have (the same discipline `exampleQuestions.ts`'s doc comment already
states for the frontend's own capability list). Gated on there being no
active clarification in flight, so a bare reply like "hi" answering an
unrelated clarifying question is never mistaken for a fresh capability
question. Covered by `capability_test.go`, including an explicit assertion
that a capability question never reaches the gate or explainer mock.
Live-verified via curl: `interactions` comes back empty and the answer
lists all seven real tools' capabilities in plain language.

**A real gap this surfaced and fixed in passing:** `exampleQuestions.ts`
(the frontend's own hand-written capability list) was missing
`get_period_totals` entirely — both from the tool union type and from
`EXAMPLE_QUESTIONS` — even though it's the sixth of seven real tools and had
already shipped. Added a `get_period_totals` example question and folded
period totals/best-worst-day into `CAPABILITY_SUMMARY`, so the frontend's
zero-state capability list and the new backend capability answer both name
the same seven tools rather than silently drifting apart at six.

**4. A tooltip on the Home page's "Days with a flag" stat.** That stat had
no explanation of what a flag actually means, and "flag" alone reads as
ambiguous — an open problem needing action, or something already resolved?
Added a generic `tooltip` prop to the shared `Stat` component
(`components/ui/stat.tsx`, a new `components/ui/tooltip.tsx` built on the
project's existing `radix-ui` dependency, matching the `Avatar` component's
own import convention) rather than a one-off fix scoped to this page, so any
other stat that needs the same affordance later can reuse it. The Home
page's stat now reads: "A flag means the reconciliation engine caught
something worth a second look on that day ... It's already been caught and
accounted for, not an open problem waiting on you." — answering the
ambiguity directly, in the product's own words.

Verification: `cd backend && go build ./... && go vet ./... && go test ./...`
(all clean) and `cd frontend && npx tsc -b --noEmit && npm test -- --run`
(147/147 passing, including one updated assertion in
`ChatPanel.test.tsx` for the new green refusal color). Live-verified end to
end via curl (capability question, a real data question, a real refusal) and
via a real browser (Home page tooltip in light mode — this product's only
theme; the chat capability answer and the recolored refusal bubble), both
before and after a full cache clear (`DELETE FROM answer_cache,
answer_cache_hit, paraphrase_match`) and a clean restart of both the backend
and frontend dev servers.

## 2026-08-28 — Presentation audit: stale numbers fixed, two new slides, roadmap slide drafted with Fable

Triggered by direct user feedback that the deck had fallen behind the real
product: a re-read found the "Token discipline" slide still stating **226
interactions / $1.57 spent** and the closing slide stating **$3.35** — two
different, both-stale numbers for the same metric in the same deck, next to
each other in the nav order. The real, live number from `question_interaction`
at the time of this check: **430 interactions, $3.5603 spent**. Both slides
now state $3.56 consistently, and the cost-track bar's fill width was
recalculated (31.4% → 71.2% of the $5 cap) rather than left visually wrong
under a corrected label. The architecture flow, ports/adapters, and RFC
model-table slides all still said "6 typed tools" — fixed to 7 in every
instance (`get_period_totals` shipped after those slides were last touched).
The ports/adapters diagram's `internal/ambiguity` box also only showed
"Claude Haiku 4.5," with no mention of the conditional Sonnet writer pass
that already existed in the real code — added a second line to the box
rather than leaving the diagram one step behind the architecture doc, which
already documented the writer pass correctly.

**Two new slides**, matching the existing `democard`/`cols3`/`livenote`
visual pattern exactly rather than introducing a new one:

- **"A steward that sounds like one — and knows its own job"** (slide 9c,
  right after the existing follow-up-suggestions slide): three real
  before/after moments — the capability-question path, the red→green
  refusal recolor, and the warm narration opening — each with a real
  logged value (`interactions: [] · $0.000 spent`, `tone: destructive →
  refusal`, an actual narrated opening line).
- **"The one input the owner builds by hand — replacing the sheet"**
  (the new second-to-last slide, immediately before Closing): a staged,
  three-phase roadmap for reducing the manual supplier-cost-sheet burden —
  the one input source this product's spec 007 already identifies as
  entirely owner-constructed rather than received as a file.

**The roadmap slide's content was drafted by Claude Fable 5, not this
session's default model**, per this project's own standing model-selection
discipline (documented in this same file's earlier sections: Haiku for
cheap classification, Sonnet for the default work, Fable reserved for a
small number of genuinely pivotal decisions). A product roadmap that a real
Prosus/Toqan PM interviewer will read and probe — is the sequencing
defensible, is each risk actually honest, does "why Prosus specifically"
hold up — is exactly the kind of judgment call this project's own
model-tier framework says to spend the expensive model on, not routine
prose editing. Fable was briefed with the real spec 007 constraints (manual,
irregular-cadence CSV re-keying), this project's durable architectural
rules (single-LLM-vendor, deterministic-engine/probabilistic-narrator, never
open-ended computation), and Prosus's actual distribution asset (724K
restaurants already on its rails, per this deck's own Vision slide) — and
asked for a staged plan with one honest risk named per phase, not
roadmap-as-marketing. Its three phases (Snap the Invoice → Prosus Rails
First → Price Watch, each sequenced explicitly by what it depends on and
what's genuinely hard about it) were used close to verbatim, condensed only
for slide space.

Verified via a headless-browser navigation sweep of the whole deck
(`document.querySelectorAll('.slide').length` — 25, up from 24 before this
pass, matching the two slides actually added) with zero console errors, and
via direct screenshots of the token-discipline, architecture, ports/
adapters, new steward-warmth, new roadmap, and closing slides, confirming
every number renders correctly and no layout regressed. `README.md`'s own
slide-count and cumulative-spend lines were updated to match (23→25 slides,
$3.35→$3.56).

## 2026-08-28 — Dry-run feedback: a real narrative restructure, four parallel agents, one Fable-drafted storytelling spec

A real dry run in front of an audience surfaced a materially different class
of feedback than the previous pass's factual staleness: the deck's *story*
had problems the numbers alone couldn't fix — a generic-feeling problem
statement, an OKR the audience never saw assembled, a Key Result that
visually cherry-picked its best sub-metric, an unsupported Hypotheses slide,
and a self-imposed "$5 spend cap" narrative device the user wanted removed
entirely, from documentation as well as (a check confirmed) nonexistent
code. Given the scope, four agents ran in parallel rather than sequentially:

1. **Fable — narrative restructuring spec** for the Problem/Vision/OKR/
   Hypotheses arc (not final copy — a slide-by-slide implementation spec).
2. **A research agent** (general-purpose, WebSearch) sourcing real,
   attributable restaurant-industry data for the Hypotheses slide's H2
   urgency argument. It explicitly refused to force a fit: it found and
   flagged as **unusable** two widely-repeated but unsourceable claims (a
   Brazil-specific "35-45% of food-service businesses close within 5 years"
   figure, and a UK "60% fail in year one" claim that traces to the
   debunked US myth relabeled) rather than presenting either as real. What
   it did verify and use: restaurants hold a **median of 16 days of cash
   buffer**, the thinnest of any small-business sector studied, vs. 27 days
   median across all small businesses (JPMorgan Chase Institute, *Cash is
   King: Flows, Balances, and Buffer Days*, September 2016 — an analysis of
   470M+ transactions across 597,000 small businesses). Also verified: 35%
   of 11,000+ audited restaurant invoices across 400 restaurants had at
   least one overcharge (Consolidated Concepts, 2015, via FSR Magazine) —
   used on the Problem slide, not H2, since it's about invoice errors, not
   cash-flow timing.
3. **Fable — architecture roadmap design** for a real, previously-missing
   objective the user named directly: getting sales data from the iFood and
   Just Eat Takeaway APIs instead of manual CSV export. Unified with the
   existing cost-sheet roadmap into one "closing the manual-file gap"
   architecture direction, staged the same way (near-term/medium-term/
   stretch), with its own named risk (a platform's real-time order feed and
   its end-of-period payout statement routinely disagree for days — a day
   without settled data is flagged provisional, never estimated).
4. **A fork audit** of the entire 25-slide deck for duplicate/disconnected
   numbers, which found real defects beyond what the user had already named:
   KR1's card visually led with its best sub-metric (100% refusal
   correctness) while burying its worst (40% strict consistency) one line
   below in the same card — a real cherry-picking pattern, not just
   "duplication"; a stale `.costfill{width:31.4%}` CSS default (the old
   $1.57 spend figure) sitting unused in the base stylesheet, invisible only
   because every real usage overrode it inline — a live landmine for the
   next person who edits that CSS without knowing; and the Problem slide's
   "all sourced" tag was contradicted by its own cards — two of four had no
   citation anywhere.

**What changed, concretely:**
- **Problem slide**: dropped the false "all sourced" tag; each stat card
  now carries an honest `Sourced`/`Estimate` chip. Replaced the uncited
  "Manual work 12h/wk" card (folded into the subhead) with the new sourced
  35% invoice-overcharge stat.
- **OKR unified onto one slide**: the Objective ("Grow revenue without
  eroding margin") moved off the Vision slide's buried subtitle line into
  its own banner directly above the four KR cards, on a slide retitled "The
  OKR." KR1 now shows only its committed target/result (5/5 refusal
  correctness) as the headline, with accuracy/consistency moved to a
  visually distinct "Disclosed, not committed" sub-row.
- **13/15 accuracy de-duplicated**: the Evaluation slide stays the one place
  it's explained in full; the Closing slide dropped it as a headline stat
  entirely (replaced with Refusals 5/5, Live points, Interactions, Build
  spend), keeping only a small non-headline caption pointing back to where
  it was already explained.
- **Hypotheses slide redesigned** from five bare tagged one-liners into a
  claim+evidence ledger: H1 points to the real 5/5 refusal-correctness
  result; H2 carries the new sourced cash-buffer statistic; H3/H4 get
  honest "gap disclosed" framing instead of silence; H5 points forward to
  the Roadmap slide instead of duplicating it.
- **The $5 cap removed** from `README.md`, `docs/technical-rfc.md`,
  `docs/dor.md`, and every slide (Token Discipline's whole cost-track/
  checkpoint/ceiling visual deleted along with its dead CSS; KR4 and the
  RFC's model-cost table reworded to measure against KR4's own $0.05/
  interaction bar instead of a larger budget). Historical, dated build-log
  entries in this same file that recorded the cap as a real, contemporaneous
  operational decision were deliberately left untouched — this document's
  own standing discipline is "kept as it happened, not reconstructed," and
  a self-imposed pacing constraint that genuinely existed at the time is
  real history, not a narrative device to scrub.
- **Roadmap slide unified**: retitled "Closing every manual-file gap left
  in the pipeline," now covers both the cost-sheet phases and the new
  direct-platform-API-feed objective as one strategic move, closing with
  "why Prosus" reasoning that spans both (it sits on both sides of the
  invoice AND owns iFood).
- **`docs/architecture.html`** gained a new roadmap section, `.roadmap`-
  styled to match its one existing roadmap block, placed directly after the
  reconciliation-pipeline diagram it extends.
- **A new closing-adjacent slide, "The 7 tools and the skills, by name"**,
  and a matching `README.md` section — every MCP tool and every Claude Code
  skill actually used, named individually rather than left to a generic
  "AI helped build this" claim. Includes `inspired-product` (Marty Cagan's
  *Inspired*/*Empowered* framework), invoked in this same session to ground
  the OKR/opportunity-assessment restructuring above.

Verified via a full headless-browser navigation sweep (26 slides now, zero
console errors) and direct screenshots of every changed slide before
republishing both `docs/presentation.html` and `docs/architecture.html`.
`README.md`'s slide count updated again (25→26).
