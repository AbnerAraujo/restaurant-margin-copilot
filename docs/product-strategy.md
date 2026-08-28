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
