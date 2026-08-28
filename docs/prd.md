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
| Reconciliation correctness (KR2) | N/A | Zero silent data loss on messy fixture set | Table-driven tests + quickstart validation |
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
- Real delivery-platform API integrations (fixture/real-file CSV only)
- Growth, Engagement, and Campaign-Creation badge categories (Reconciliation category only is built)
- The semantic-memory/cache/LLMOps harness discussed and explicitly deferred as a Phase 2 vision, not part of this build
- Non-Prosus-customer market segment (Segment 2 in the market-sizing section)

## 4. Design & Experience

iFood-inspired visual direction (primary red ~#EA1D2C, third-party sourced, not an official guideline; used as inspiration, not the actual iFood trademark/logo). Chat UI via shadcn AI Elements, with a visible provenance citation and running cost panel on every answer, and quiet (not arcade-style) badge acknowledgment per the B2B gamification research in `product-strategy.md`.

## 5. Technical Considerations

See the companion Technical RFC (`docs/technical-rfc.md`) for architecture, data model, and the ports-and-adapters module design. Summary: Go backend (deterministic core + MCP tool layer), PostgreSQL, React frontend, Anthropic API (Claude Haiku 4.5 gate, Sonnet 5 explain) — no agent framework.

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
