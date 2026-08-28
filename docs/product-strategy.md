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

**Open question, not yet resolved**: the current build (`specs/001-...`) is
scoped narrowly to margin reconciliation — the protection-side lever only.
Under this broader objective, that build is one candidate Key Result, not
the whole strategy. Whether to (a) keep this build as-is and treat it as
"the specific, provable first step toward the objective," or (b) broaden
scope to also address a growth-side lever, is a decision to make explicitly,
not drift into.

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
