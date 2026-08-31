# PRD: Daily Margin & Growth Copilot

**Date:** 2026-08-28 · **Author:** Jair Abner de Araujo · **Status:** Approved for build (Product A decision, `docs/product-strategy.md`) · **Version:** 1.0

## 1. Overview

### 1.1 Executive Summary

Independent restaurants and bars can't see, daily, whether they made money — reconciliation across delivery-platform, POS, and cost data is tedious enough that it happens at month-end, if at all, and promotional spend runs with no visibility into whether it's profitable. This product ingests those exports, deterministically reconciles margin and flags underperforming promotions, and answers natural-language questions with provenance — refusing rather than guessing when data is missing or a question is ambiguous.

### 1.2 Problem Statement

**User pain:** Independent restaurant/bar owners run margins of 3–5% [Sourced], face 15–30% delivery commissions plus opaque "adjustments" deducted from payout with no line-item breakdown [Sourced], and spend ~12 hrs/week on manual reconciliation that still misses 2–5% of delivery revenue to discrepancies [Sourced]. The same opacity applies to promotional spend: sponsored-placement costs run 5–10% of monthly revenue on top of commission, deducted from payout the same opaque way [Sourced]. Full citations: `docs/product-strategy.md`.

**Why now:** Prosus/Just Eat Takeaway already launched ToqanClaw (June 2026) to its ~5M-merchant ecosystem, with real Dutch case studies (Lebkov & Sons, Burger & Frites, Poke Perfect) proving this class of product works [Sourced]. This is a deepening of that existing initiative, not a new market bet.

### 1.3 Goals

**OKR Objective:** Increase restaurant/bar partners' profitability by growing revenue without eroding margin (`docs/product-strategy.md`).

**Success Metrics** (this build; see also post-launch metrics in `product-strategy.md`):

| Metric | Baseline | Target | Measurement | Measured (2026-08-31, 3 runs) |
|---|---|---|---|---|
| Accuracy (KR1) | N/A — not yet run | Measured & reported, incl. failures | `evaluation/promptfoo/accuracy.yaml`, ~15–20 questions | **14/15 · 15/15 · 14/15** |
| Consistency (KR1) | N/A | Measured & reported | 5 questions × 3 phrasings | **14/15 · 15/15 · 15/15** |
| Refusal-correctness (KR1) | N/A | 100% on ~5 unanswerable questions | Refusal harness | **4/5 · 4/5 · 4/5** (12/15) |
| Reconciliation correctness (KR2) | N/A | Zero silent data loss on the deliberately messy test data | Table-driven tests + quickstart validation | **0 defects**, now asserted by `TestOpeningWindow_PersistedWithZeroSilentDataLoss` |
| Promo-ROI flagging (KR3) | N/A | ≥1 negative-ROI promo correctly flagged end-to-end | quickstart validation | **1 flagged** (−$450.75), now asserted by `TestListNegativeRoiPromotions_RealDataset_FlagsLunchfixWithProvenance` |
| Cost per interaction (KR4) | N/A | Under a stated threshold (e.g. $0.05), instrumented | Instrumentation log | **$0.0313/question** — 70 questions, 142 model calls, $2.1931 |

No target is pre-committed for KR1's exact percentages — per Constitution Principle V, real numbers are reported including failures, not asserted in advance.

**How the measured column was obtained, and why three figures per row.** The
full harness was run against a dedicated backend on `:8092` started with
`cmd/server -eval-no-answer-cache`. The flag matters: without it the harness
instance shares the product's `answer_cache` table and a re-run is served
largely from the previous run's cached answers (25 of 35 questions,
measured), which grades the cache rather than the model and makes the
apparent cost per question a fraction of the real one. An initial two-run
measurement (14/15 and 14/15 accuracy, 13/15 and 15/15 consistency, 5/5 and
4/5 refusal) was superseded by a third full run once it became clear two
data points aren't enough to call a pattern when the model layer isn't
deterministic — all three runs are reported rather than the best one.
Aggregate across all three suites, all three runs: **99/105 (94.3%)**.

Every failure behind those numbers was hand-read from the raw JSON, and none
is a wrong number. The one that matters is **A15** ("delivery revenue on
2024-08-02, net of the refund?"), which failed in every uncached run: the
model returns gross $446.25 and the $62.25 refund with provenance, then
declines to net them to $384.00 because no tool returns a net-of-refund
delivery figure and it will not present its own arithmetic as a computed
result. That is Constitution Principle I working as designed, and it names a
real **gap in the tool contract** — `get_daily_summary` exposes gross and
refunds separately and nothing exposes the net. Open, not fixed. The other
three are one genuine consistency divergence (C1a) and two cases of a
grading regex rejecting a correct answer (C3, R1), both deliberately left
unfixed: tuning a grader after watching it fail is how a number stops
meaning anything.

Cumulative real API spend across the whole build, all three ledgers, on the
same date: **$14.87** across **1,166** logged model calls ($14.7741 over
1,129 `question_interaction` rows, $0.0401 over 29 `paraphrase_match` rows,
$0.0558 over 8 `business_insight_interaction` rows). Note the unit — a row
is one *model call*, and an answered question writes two (gate, then
explain), so the KR4 figure above is measured over a known question count
rather than by averaging the ledger.

## 2. Users & Use Cases

**Primary Persona: Independent restaurant/bar owner-operator.** Time-poor, already "app-rich but insight-poor" [Sourced], juggling POS + one or more delivery platforms + spreadsheets. Needs a tool that's quick to adopt and glanceable, not a deep dashboard requiring a sit-down analysis session.

**Use Case 1 — Daily close:** Owner opens the app; sees today's reconciled margin with provenance, no question required. (spec.md User Story 1)

**Use Case 2 — Ask a question:** Owner asks "how did we do this week vs last?"; gets a grounded, provenanced answer. (User Story 2)

**Use Case 3 — Refusal:** Owner asks something ambiguous or unanswerable; gets a clarifying question or explicit refusal, never a guess. (User Story 3)

**Use Case 4 — Promo flag:** Owner is shown (or asks about) a promotion losing money; gets the ROI and provenance, or an honest "can't attribute this" if data is incomplete. (User Story 4)

## 3. Solution

High-level architecture and module design: `docs/architecture.html` (published artifact) and `specs/001-margin-reconciliation-qa/plan.md`. Not duplicated here — this PRD is the product view; the RFC below is the technical view.

### 3.3 Functional Requirements

Full list with acceptance criteria: `specs/001-margin-reconciliation-qa/spec.md` (FR-001–FR-013). Summary: deterministic ingestion + reconciliation + promo-ROI computation, provenance on every number, an ambiguity gate before any answer, refusal over guessing, and full per-interaction instrumentation.

**Explicitly out of scope for this build** (see `docs/product-strategy.md`'s roadmap sections for why):
- Multi-tenant / multi-location support
- Real delivery-platform API integrations (CSV exports only — superseded in part by section 12's *simulated* connector layer, which is explicitly not a real integration)
- Growth, Engagement, and Campaign-Creation badge categories (Reconciliation category only is built)
- The semantic-memory/cache/LLMOps harness discussed and explicitly deferred as a Phase 2 vision, not part of this build
- Non-Prosus-customer market segment (Segment 2 in the market-sizing section)

## 4. Design & Experience

**Product name: My Business Steward.** Final brand mark: batwing café doors (tall frame posts, hexagonal panels hinged mid-frame, tapering to a point at the center gap — the real anatomy of a swinging kitchen/bar door), rendered in a prosperity-emerald green (`#0E6E52` light / `#1FA876` dark), replacing the earlier iFood-red exploration — green plays on the "in the red / in the green" financial idiom this product's whole job is to cross. Chat UI via custom components (not shadcn AI Elements, per the actual frontend build), with a visible provenance citation and running cost panel on every answer, and quiet (not arcade-style) badge acknowledgment per the B2B gamification research in `product-strategy.md`. Full logo exploration/rationale: `docs/brand.md` (or the published artifact, if still live).

## 5. Technical Considerations

See the companion Technical RFC (`docs/technical-rfc.md`) for architecture, data model, and the ports-and-adapters module design. Summary: Go backend (deterministic core + MCP tool layer), PostgreSQL, React frontend, Anthropic API (Claude Sonnet 5 gate and explain, Claude Haiku 4.5 for paraphrase-match caching) — no agent framework.

## 6. Go-to-Market

See `docs/product-strategy.md`'s "Market sizing & launch strategy" section — Segment 1 (Prosus/ToqanClaw customers, ~724k+ restaurants on iFood + Just Eat Takeaway) only. Not duplicated here.

## 7. Risks & Mitigation

| Risk | Impact | Probability | Mitigation |
|---|---|---|---|
| Real harness numbers are worse than hoped | High (deliverable #2 depends on real numbers) | Medium | Report honestly including failures — this is explicitly valued by the evaluator, per the brief |
| Real-file compatibility breaks on an actual owner's export | Medium | Medium | Named in `research.md` as a design constraint; logged to `docs/plan.md`'s mistakes log if it happens, not silently patched |
| Timeline slips past Tuesday | High | Medium | Constitution's fixed build order + Day 4 hard stop on new features (`docs/plan.md`) |

## 8. Open Questions

None blocking — all prior open questions (scope, growth lever, badge scope, market segment) were resolved earlier in `docs/product-strategy.md` and are not re-litigated here.

## 10. Success Criteria

Launch criteria for *this build*: all functional requirements met (tasks.md, 39 tasks), evaluation harness run with real numbers reported, `quickstart.md` validated end-to-end including a real-file trial if available before Tuesday.

## 11. Roadmap items promoted to specs (post-launch expansion)

Section 3's "explicitly out of scope" list is being worked through, not abandoned. Each item below has its own full spec/plan under `specs/`, rather than being described only here:

- **Growth, Engagement, Campaign-Creation badge categories** — `specs/002-badge-expansion/`. Campaign-Creation is deliberately reframed from the original "integrates with Prosus's promotional tooling" concept (no API access exists for that) to a real in-app action this build can actually verify; Engagement is built on real, honestly-near-zero usage tracking rather than any simulated signal.
- **The cross-platform economics comparator** (Product D from the original 5-products comparison, `docs/product-strategy.md`) — `specs/003-platform-comparator/`. Re-scored as buildable now that both platforms' real, distinct commission economics already exist in the ingested data — the original blocker ("needs two platform data models") no longer applies.
- **The semantic-memory/LLMOps harness vision** — concretized as `specs/004-semantic-cache/`, extending the build's own answer cache to recognize paraphrased repeat questions, using a bounded Claude Haiku classification rather than a new embeddings vendor (Anthropic has none; adding one would reopen the single-vendor decision this project's constitution already made once).
- **Segment 2 (non-Prosus customers)** — implies real multi-tenancy. `specs/005-multi-tenant/spec.md` plus `docs/rfc-multi-tenant.md` define what this requires; **implementation is explicitly gated on review of that RFC**, not bundled with the other three, given a tenant-isolation defect is a data-breach class of bug, not a UX defect.
- **Cost sheet upload through the web UI** — `specs/007-cost-sheet-upload/`. Closes the "a developer with terminal access is required to update input costs" gap the CLI-only `-ingest` flag left open; a zero-model feature (pure deterministic parsing/validation, reusing `internal/ingest.ParseCostSheet` unchanged) that turns the one input source the owner personally produces (supplier billing, on an irregular cadence) into something they can update themselves.
- **Business Insight Advisor** — `specs/009-business-insight-advisor/`. The first feature that deliberately produces probabilistic content, built so the deterministic/probabilistic split runs through its middle: WHETHER an insight exists (a discrepancy flag, a money-losing promotion, a premium-band commission rate, a day-of-month expense spike, a material margin decline) is decided in plain Go at zero cost on every answered question, while the advice TEXT is a separate, owner-initiated, re-verified, and individually-ledgered Claude Sonnet 5 call — rendered in its own visually-distinct "AI suggestion" bubble with its real cost shown, never blended into the provenance-backed answer. Trigger thresholds and prompts are grounded in researched industry practice (commission tiers, payout-dispute mechanics, ordering-cadence cost control), tagged Sourced vs. Judgment per `product-strategy.md`'s discipline.
- **Platform Connector Proxy (simulated)** — `specs/010-platform-connector-proxy/`. Section 3 lists real delivery-platform API integrations as out of scope, and they still are: this builds the *connector layer* for real — two mock upstreams with incompatible wire formats, one normalizing proxy, the unchanged reconciliation engine behind it — while stubbing the only part that cannot be built without credentials, and labels the result as emulated in five independent places. Full rationale, including what is deliberately not built (OAuth, retries, webhooks, a plugin registry), in section 12 below.
- **POS connector, and cross-source deduplication** — `specs/012-pos-connector-dedup/`. Extends the connector with the two thirds of revenue spec 010 left out, and then solves the problem that adding it creates: a POS integrated with a delivery aggregator records that aggregator's orders as its own tickets, so the same real-world order arrives twice and a naive sum inflates gross sales every day. A deterministic two-tier matcher resolves what it can and **refuses to guess at the rest**, because a wrong merge deletes real revenue just as surely as a missed one double-counts it. Section 12 below.
- **A named BFF boundary for the owner app** — `specs/013-bff-layer/`. The request was to unify the "main backend" with the "platform connector"; the finding was that **there were never two backends** — the connector is an in-process Go package on the same mux, same binary, same origin. So no service was added. What was added is the boundary discipline that was missing: one route table declared as data, from which the CORS preflight, the 405 policy and the startup log are *derived* rather than hand-maintained beside it. Section 13 below.

## 12. Connector Proxy — simulated iFood, Just Eat Takeaway and POS

`specs/010-platform-connector-proxy/` and `specs/012-pos-connector-dedup/`. The one section of this PRD whose most important sentence is about what the feature *is not*.

### The problem

Delivery-platform revenue is roughly a third of this restaurant's gross sales (`cmd/gendata`: 17% iFood + 17% Just Eat Takeaway against 66% POS), and until now it could reach the product exactly one way — somebody exports `delivery_platform_export.csv` from each merchant portal and it gets ingested from disk. That is a real gap for a daily-close product: the whole value proposition is "know today's margin today", and today's margin depends on a file a human has to remember to fetch.

The obvious fix is the platforms' partner APIs. This project has no iFood partner-API credentials and no Just Eat Takeaway partner-API credentials, and will not get them for a take-home prototype. Section 3 listed "real delivery-platform API integrations" as out of scope for exactly that reason, and that remains true.

### The solution

Build the connector layer for real; stub only the part that cannot be built.

- **Two simulated upstreams** emitting deliberately incompatible wire formats. iFood: page-numbered JSON, `snake_case`, decimal-string amounts in nested currency objects, RFC 3339 timestamps, `CONCLUDED`/`CANCELLED`, cancelled orders reported with *positive* amounts plus a cancellation block. Just Eat Takeaway: cursor-paginated JSON, `camelCase`, integer minor units, epoch-millisecond UTC timestamps, `DELIVERED`/`REFUNDED`, refunds reported already *negative*, and **no commission rate reported at all**.
- **One proxy** (`backend/internal/platformconnector`) that dispatches per platform and normalizes both into `ingest.DeliveryRecord` — the exact type the CSV parser already produces — then verifies six contract properties on every record before it is allowed downstream.
- **One reconciliation engine, unchanged.** `internal/reconcile` has a zero-line diff from this feature. A connector-sourced day and a CSV-sourced day are indistinguishable to reconciliation, to the MCP tools, to chat answers, and to badges — except by their provenance strings.

The normalization is genuine work, not a rename. Two examples worth naming because both are silent-failure shaped: Just Eat Takeaway reports no commission rate, so the connector derives it in basis points — get it wrong and every JET order raises a `commission_mismatch` flag, burying the real discrepancies under integration noise. And the two platforms disagree about the sign of a refund, so one adapter negates and the other must not — get *that* wrong and a refund is counted as revenue, raising a day's margin with nothing anywhere to explain why.

### Why this is honest rather than deceptive

A synthetic number presented as a settled platform payout would be the single most damaging thing this product could ship, given that its stated bar is "a confidently wrong margin figure is worse than a refusal". The disclosure is therefore redundant by design — five independent statements, any four of which survive the removal of the fifth:

1. The tab is labeled **"Connected platforms (simulated)"**, so the word arrives before the panel is opened.
2. A persistent, non-dismissible notice sits above every control: *"These connections are simulated. No real iFood or Just Eat Takeaway account is connected."*
3. Each platform row carries its own "Simulated connection" marker, so a screenshot cropped past the banner still discloses.
4. Every API response body carries a top-level `"simulated": true` and the same notice text, so a client that ignores the UI entirely still cannot render these numbers undisclosed.
5. Every record's provenance is a `simulated://ifood-partner-api/...` URI rather than a plausible-looking filename — and that prefix is *enforced* by the proxy's contract check, not merely intended.

The values are synthetic, but they are not a guess at real figures, and nothing in the product presents them as one. Determinism is part of the honesty: the same platform and date always produce the same orders, so a demo, a re-run, and an evaluator all see identical numbers rather than a figure that quietly changes each time it is looked at.

### Explicitly out of scope

- **Real OAuth, token refresh, or credential storage.** There is no credential. An auth flow against a fake upstream validates nothing and would misrepresent the integration's maturity.
- **Retries, backoff, rate limiting, circuit breakers.** In-process function calls do not fail transiently; simulating flakiness so resilience code has something to catch would be fiction stacked on fiction.
- **Webhooks or push delivery.** Pull-on-demand only.
- **A fourth source or a plugin registry.** Exactly three, registered explicitly.
- **Historical backfill.** A sync covers an owner-chosen range, capped at 31 days.
- **A simulated supplier API.** Input costs still come from the cost-sheet upload.
- **Persisting raw platform payloads.** The raw envelopes live inside one function call; storing them would imply an audit trail this data does not deserve.

## 12b. The POS connector, and the problem it creates

`specs/012-pos-connector-dedup/`. The POS is **two thirds** of this restaurant's gross sales (`cmd/gendata`: 66% POS against 17% + 17% delivery), and spec 010 deliberately left it out — its own Assumptions say so: *"There is no simulated POS API."* So the connector story was two thirds unfinished on the revenue side, and it was the larger two thirds.

Adding a third mocked upstream is the easy half. The hard half is what adding it exposes.

### Same order, two systems

Modern restaurant POS systems integrate with delivery aggregators precisely so front-of-house sees every order — dine-in, counter, and delivery — on one screen and one kitchen printer. When that integration is in place, a delivery order is *pushed into the POS* and becomes a POS ticket with its own POS order number. The same real-world order then appears once in the platform's settlement feed and once in the POS's ticket feed.

Sum both and you double-count it. On a restaurant where delivery is a third of sales and the POS integrates with one aggregator, that is not a rounding error: it is a systematically inflated gross-sales figure, an understated cost ratio, and a margin percentage that looks better than reality every single day. **Measured on this product's own simulated data for 2026-08-18..20: naively summing the three sources gives $14,285.89 of gross sales against a true $12,442.88 — $1,843.01 of revenue counted twice, and a three-day margin overstated by 20.3%.**

### The inverse failure, which is the harder one

A deduplication rule that is too eager merges two genuinely different orders that happen to share a price and a rough time. On a 20-to-30-order evening at a $32 mean ticket, that is not exotic; it is expected. The result is **real revenue deleted** from the day, unrecoverable from the reconciliation output, and invisible — the day is simply lower, and nothing says why.

Dropping a real order and double-counting a duplicate one are the same financial-integrity failure wearing different signs. This feature is designed against both, and the second is the one the design spends its effort on.

### The rule, stated so it can be argued with

> A POS ticket is a duplicate of a delivery order **only if the POS itself said** the ticket arrived through a delivery channel. Within that set: if the ticket carries the platform's order reference and that reference resolves, they are the same order. Otherwise they are the same order only if they share a platform, a calendar date and an **exact amount in cents**, their times are within **15 minutes**, and no other reading of the day's tickets is equally consistent.

Three consequences worth naming:

- **No matching on amount and time alone.** Without an assertion from one of the two systems that a delivery channel was involved, the evidence is "these numbers are similar", and acting on that deletes revenue. An untagged dine-in ticket is ineligible at any amount, at any time. That is a deliberate false negative, accepted so the false-positive rate can be bounded.
- **Ambiguity is disclosed, never resolved by preference.** Where more than one reading is equally consistent — one ticket with two candidate orders, or two tickets contesting one — nothing merges, every record survives, and the day carries a flag naming the candidates. The pairing must be the unique solution *from both directions*, which is also what makes the result independent of iteration order rather than a coin flip dressed as an answer.
- **The delivery record wins.** It knows the commission, the rate, the payout and the refund state; the POS ticket knows only a gross amount. Dropping the delivery side would zero that order's commission and move margin *up* — a wrong number in the flattering direction, the worst shape an error in this product can take.

Zero model involvement: integer-cent equality, case-folded string equality, and a minute difference. Every decision can be recomputed by hand from the two records.

### The simulation is built to make the rule work for its living

The POS mock does not invent delivery-looking tickets. For each date it calls the *same* generator the iFood mock calls and echoes those actual orders, so a duplicate the matcher finds is causally real rather than a coincidence the mock arranged. Around it sit the deliberate difficulties:

- **Only iFood is integrated into the POS, not Just Eat Takeaway.** A common real configuration, and the useful one: it puts a control group inside every fetch — JET orders that must never be touched, beside iFood orders that must be.
- **A quarter of echoed tickets carry no partner reference.** Real integrations do record it; assuming they always do would make the second matching tier decoration. Stated as a modelling choice, not a finding.
- **Campaign-discounted orders disagree on amount**, because the POS rings the menu price and never saw the platform's promotion — giving the amount-mismatch flag a real cause and the amount-and-time tier a real "no counterpart found" case it has to disclose.
- **Ambiguity is not manufactured.** The mock does not arrange a collision so the unresolved flag has something to do; that path is proven by test, and its real incidence is reported as measured.

**Measured over August 2026 (all 31 days, three sources): 689 duplicates resolved, 40 overlaps left unresolved and flagged, and 2,569 in-house tickets — every one of them intact.** Zero false positives is a test that runs on every build, not a claim.

### Nothing is silently corrected

Every outcome reaches the day it affected as a discrepancy flag in the vocabulary `internal/reconcile` already uses: `cross_source_duplicate_removed`, `cross_source_duplicate_unresolved`, `cross_source_amount_mismatch`. Each names both sides — which ticket was removed, which order it merged into, at which row of which source. An unresolved overlap says the consequence out loud: *"this day may count that order twice."* The connected-sources panel reports the removals and the unresolved overlaps side by side, because reporting only the removals would let the product claim a clean close it did not achieve.

The `pos_export.csv` upload path is untouched, and deliberately so: a CSV a human exported from a POS with no delivery integration has no overlap to find, and this feature has no way to know whether a given file's POS was integrated. Guessing would be estimating.

- **Inline Grounded Advice (widening of the Business Insight Advisor)** — `specs/011-inline-grounded-advice/`, an **evolution of spec 009, not a rewrite**: everything 009 shipped and verified (the five deterministic insight kinds, the zero-cost teaser, the tap-to-fetch endpoint, its re-verification gate) is preserved byte-for-byte as the proactive path. What changed, in the product owner's own words: *"the advisor should advise whatever the customer asks and use the data in context for it — not bringing wrong data or hallucination, but using an advisor that gets all the rich data we have and brings suggestions is something of value to the product strategy and vision."* So a second avenue into the same advisor now exists: when the owner **explicitly asks** for a suggestion ("how can I improve my margin overall?", "should I push delivery or dine-in?"), the ambiguity gate emits a typed advice-requested signal, the normal tool-calling narration answers the data core first with full provenance, and one bounded advisor call then runs inline in the same turn — grounded exclusively in the tool results that very answer computed, its prompt assembled in plain Go from researched-practice sections selected by which tools actually ran (prime-cost decomposition, menu engineering per Kasavana & Smith 1982, direct-channel steering — sourced in the spec's plan.md), its cost ledgered in the same `business_insight_interaction` table (kind `question_advice`) and shown in the reply. **Explicitly out of scope, and the boundary that makes the widening safe**: this is not a general-purpose business consultant. Advice must always trace to real, tool-computed data from the same interaction; a request nothing in the tool set can ground — staff pay, hiring, team motivation, opening a location — is still refused plainly, exactly as before. The "refuse rather than guess" discipline is what the widening was designed around, not what it traded away.

---

## 13. A named BFF boundary for the owner app

`specs/013-bff-layer/`. The section whose most useful content is a finding that contradicted the request.

### The request, and what was actually there

The ask was to put a backend-for-frontend layer in front of two things: the main backend and the platform-connector proxy, so the frontend stops reasoning about two separate backend concerns.

There were not two backends. `backend/internal/platformconnector` is an ordinary in-process Go package — same binary, same `http.ServeMux`, same origin, same `/api/*` prefix as everything else. The frontend has had exactly one base URL since spec 001. A BFF *service* here would have bought a boundary that already held, and charged a network hop, a deployment pipeline and an SLO for it. The BFF pattern's own literature is direct about this case: Azure names "only one interface interacts with the backend" as an explicit non-fit for adopting the service, and its cost table lists the modular shape — per-experience modules, one deployable — as the right answer until independent scaling, cadence or language is actually exercised. None of the three is, here.

`internal/httpapi` **was already this product's BFF**: one experience, one consumer, shaping rather than deciding, no arithmetic. It had simply never been named as one — and because nothing named it, nothing enforced the properties that make the boundary worth having.

### The defect that had already cost something

Seventeen routes were registered by hand in `cmd/server/main.go`, and the CORS preflight's `Access-Control-Allow-Methods` was a single hand-maintained string literal covering the whole mux. The two had to be kept in sync from memory, and once they were not: **`PUT /api/profile` shipped broken from the browser**, because the literal listed only `GET, POST, OPTIONS`.

The failure mode is the part worth recording. A blocked CORS preflight is invisible in the browser — there is no 405 to see, just a request that does not happen — and it is *invisible to `curl`*, which talks to the handler directly and succeeded every time. Nothing automated could have caught it either, because `main` is not an importable package, so no test could enumerate the surface to compare the two lists. The fix at the time was a regression test pinning the literal, which was the right response to the incident and the wrong response to the defect: it asserted that today's string contained today's four methods, and therefore could not fail for route eighteen.

### What shipped

The API surface is now **data**, in `internal/bff`, declared once and keyed by HTTP method. The methods a route advertises and the methods it can dispatch are the same map keys — one fact, so they cannot drift. Everything else follows from it: the preflight, the 405 (now uniform and applied before the handler, replacing seventeen copies of a guard plus a `methodSplit` helper that sent unknown verbs to whichever handler happened to be the fallback), and the startup log. A handler panic, which previously reached the client as a dropped socket, now arrives as the same `{error, detail}` envelope every other failure produces.

The tightening is real and intended: on `main` every route advertised the union `GET, POST, PUT, OPTIONS`, so `GET /api/reconciliation` told the browser it accepted `PUT`. Each route now advertises exactly what it serves.

### The seam the owner actually felt

Bringing new source data into the reconciliation engine is **one job** — stage it, look at it, then let it change the numbers — and `connector_sync.go`'s own doc comment says so: it mirrors `ingest_cost_sheet.go`'s shape "because it is the same job". It was nonetheless exposed as `/api/ingest/cost-sheet/*` and `/api/connectors/*`, two prefixes with two response vocabularies, which is exactly why `UploadPage.tsx` is one page whose two tabs were written against two different API idioms.

So the two concerns were never "main backend" and "connector" — they were **upload** and **connect**, and they are the same concern. `GET /api/sources` unifies the read side into one vocabulary. It carries a `kind` field (`file_upload` | `connector`) so the list can be uniform *without pretending the difference away*: a file upload waits for a person and a connector pull does not, and flattening that would be lying to make a list look tidy.

One honesty property had to be defended here. A uniform list is precisely the shape that tempts you to hoist `simulated: true` and the emulation notice onto the envelope, once, where they read cleanly — which would make the disclosure a *sixth* place it can be cropped from, by a screenshot of one row or a client rendering a single entry. Both fields are carried **per source**, and a test asserts it. Symmetrically, the supplier cost sheet is *not* marked simulated: claiming emulation where there is none devalues the claim where there is.

### Decided against, and why

- **A separate BFF deployable.** No second experience, no second team, no independent cadence, and no token-confinement need (there is no login). All four adoption triggers absent.
- **An aggregate page endpoint.** The textbook BFF win is collapsing a screen's fan-out; this app's worst fan-out is *two* calls, both on localhost, and `HomePage` already holds independent error state per call and renders correctly when one fails. Composing them server-side would move working per-section degradation from a place it is correct to a place it would have to be rebuilt, to save one round trip.
- **Retries, circuit breakers, bulkheads, hedging.** That spine assumes a *network* upstream; this one is a function call in the same process. Section 12's existing decision — "simulating flakiness so resilience code has something to catch would be fiction stacked on fiction" — stands unchanged, and this work explicitly declined to overturn it.
- **Renaming the write paths** into one unified family. The logical end of the argument, and a breaking change to two working components days before a deadline. Recorded as knowingly unfinished rather than quietly skipped.
- **Any MCP change.** `internal/mcptools` has a zero-line diff. The tool boundary constrains what the *model* can reach; the BFF boundary shapes what the *frontend* sees. Different boundaries, different reasons, and only one of them was in scope.

**One constitutional note, recorded because it is where a pattern and this project's rules genuinely collide.** The BFF pattern's partial-failure ladder permits degrading a failed response section to a "static or safe default". This constitution forbids that rung outright for any numeric section — a confidently wrong margin figure is worse than a refusal. The pattern was applied with that rung removed, which is also part of why no aggregation endpoint was adopted: aggregation is where the temptation to reach for it lives.
