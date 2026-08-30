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

| Metric | Baseline | Target | Measurement |
|---|---|---|---|
| Accuracy (KR1) | N/A — not yet run | Measured & reported, incl. failures | `evaluation/promptfoo/accuracy.yaml`, ~15–20 questions |
| Consistency (KR1) | N/A | Measured & reported | 5 questions × 3 phrasings |
| Refusal-correctness (KR1) | N/A | 100% on ~5 unanswerable questions | Refusal harness |
| Reconciliation correctness (KR2) | N/A | Zero silent data loss on the deliberately messy test data | Table-driven tests + quickstart validation |
| Promo-ROI flagging (KR3) | N/A | ≥1 negative-ROI promo correctly flagged end-to-end | quickstart validation |
| Cost per interaction (KR4) | N/A | Under a stated threshold (e.g. $0.05), instrumented | Instrumentation log |

No target is pre-committed for KR1's exact percentages — per Constitution Principle V, real numbers are reported including failures, not asserted in advance.

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
- Real delivery-platform API integrations (CSV exports only)
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
