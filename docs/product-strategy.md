# Product Strategy — Restaurant Margin Copilot

Every data point below is tagged by where it actually came from:

- **[Sourced]** — real, cited public data
- **[Assumption]** — a business assumption we're deliberately reasoning from, stated rather than hidden
- **[Hypothesis]** — the thing we don't know yet and are explicitly testing
- **[Simulated-as-Prosus]** — framed as "if I had Toqan/ToqanClaw's actual internal knowledge base and usage data, here's what I'd pull" — explicitly a stand-in for this exercise, not real company data. Shown to demonstrate how this would be grounded with real access, not to pass as real.

## Vision

Give every independent restaurant or bar owner a same-day, trustworthy answer to "did we make money today, and why" — without hiring a bookkeeper, opening three exports, or waiting for month-end to find out margin already slipped.

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

## How this would actually be validated with real users (post-take-home)

Real validation needs real owners, not synthetic fixtures: 5–10 independent restaurant/bar owner interviews on their current close-out process, a 2–4 week pilot with the same owners using the prototype against their real POS/delivery exports, and instrumented usage (which questions get asked, refusal frequency, whether owners act differently after a flagged anomaly). None of that is possible inside a take-home — naming that limit here rather than fabricating pilot results is itself part of what's being demonstrated.
