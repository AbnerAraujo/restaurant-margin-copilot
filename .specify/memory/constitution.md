<!--
Sync Impact Report
- Version change: 1.1.0 → 1.2.0
- Modified principles: none renamed/removed
- Modified sections: Technology & Scope Constraints — the ambiguity gate
  moved from Claude Haiku 4.5 to Claude Sonnet 5 on 2026-08-29. Real,
  reproducible cause: once the live dataset grew to a multi-year range
  (backend/cmd/gendata's synthetic history on top of the fixture), Haiku's
  classification calls repeatedly misclassified fully in-range, explicitly
  dated questions as unanswerable — a genuine date-comparison failure across
  a year boundary, not a prompt-wording issue (three prompt-only fixes were
  tried and verified not to resolve it; swapping the same call to Sonnet
  fixed it immediately). Claude Haiku 4.5 is retained for the narrower
  paraphrase-match cache classifier (`internal/paraphrase`), which showed no
  evidence of the same failure. Full rationale: `internal/llmclient/cost.go`.
- Added sections: none
- Removed sections: none
- Templates requiring updates: none pending.
- Follow-up TODOs: none.

Prior report (1.1.0, superseded above):
- Version change: 1.0.0 → 1.1.0
- Modified sections: Technology & Scope Constraints — LLM vendor changed from
  OpenAI API to Anthropic API (Claude Haiku 4.5 for the ambiguity gate, Claude
  Sonnet 5 for explanation), reflecting the builder's existing Anthropic
  subscription/API account rather than a separate OpenAI account.

Prior report (1.0.0, superseded above):
- Version change: (none) → 1.0.0
- Added sections: Core Principles (I–VI), Technology & Scope Constraints,
  Development Workflow, Governance
-->

# Restaurant Margin Copilot Constitution

## Core Principles

### I. Deterministic Arithmetic, Probabilistic Narration
All arithmetic — ingestion, parsing, reconciliation, margin calculation,
week-over-week deltas, anomaly thresholds — MUST be produced by Go code
against PostgreSQL, never by a model. The model MUST be restricted to
interpreting the user's question and narrating an already-computed result in
plain language. A number the LLM invents or recalculates independently is a
constitution violation, not a bug to patch later. Rationale: this split is
the direct, demonstrable answer to whether AI is the right tool for each
step of this product — the central question the evaluation framework tests
for — and a blurred line here silently fails that test.

### II. Refuse Rather Than Guess
The system MUST refuse, or explicitly ask a clarifying question, rather than
estimate or assume when data is missing, incomplete, or the question is
ambiguous. A confidently wrong margin figure is a worse outcome than a
refusal. Every refusal and every clarifying question fired MUST be logged.
Rationale: a restaurant owner acts on these numbers; an error here has real
financial consequence, not just a worse conversion rate.

### III. Typed Tools Only, No Open Computation
The model MUST reach the reconciliation engine only through a fixed set of
typed MCP tools (e.g. `get_daily_summary`, `get_margin_delta`,
`list_discrepancies`). Open SQL, free-form computation tools, or any path
that lets the model query the database directly are prohibited. Every tool
call MUST carry a timeout, and the number of tool calls per interaction MUST
be capped. Rationale: this is the enforceable boundary that makes Principle I
real rather than aspirational.

### IV. Provenance on Every Number
Every number shown to the user MUST carry its provenance: which file, which
rows, which period it was computed from. An answer without provenance MUST
NOT be presented as fact. Rationale: provenance is what makes "trustworthy"
falsifiable instead of asserted, and is the cheapest possible trust signal
to build.

### V. Test-First for the Deterministic Core
Build order is fixed and MUST NOT be reordered: (1) fixture data, including
deliberate messiness (duplicate order, refund, missing day, inconsistent
date format), (2) the Go reconciliation engine, proven with tests, before
(3) any LLM call exists. The model layer, instrumentation, and evaluation
harness follow only after the deterministic core is proven. Rationale: if
the interface is polished before the numbers are right, the project has
failed regardless of how it looks.

### VI. Instrument From the First API Call
Every model interaction MUST log input/output tokens, model used, estimated
cost in USD, latency, whether the clarifying-question path fired, and
whether a refusal fired — from the first API call made, not retrofitted at
the end. A running cost total MUST be visible in the UI. Rationale: token
discipline and real-time cost visibility are treated as a first-class
product requirement, not an afterthought metric.

## Technology & Scope Constraints

Stack: Go (backend, reconciliation engine, MCP tool layer via
`mark3labs/mcp-go`), PostgreSQL (data + instrumentation log), React
(frontend), Anthropic API (direct calls via the official Go SDK, no agent
framework — LangChain-style orchestration is explicitly rejected in favor of
defined tools called directly). Model choice per step is deliberate: Claude
Sonnet 5 for the ambiguity gate and for explanation, Claude Haiku 4.5 for the
narrower paraphrase-match cache classifier — chosen against a real
pricing/capability comparison, not defaulted. The gate itself started on
Haiku 4.5 and moved to Sonnet 5 on 2026-08-29 after Haiku proved unreliable
at multi-year date comparison once the live dataset grew past a single year
(`internal/llmclient/cost.go` documents the full rationale) — "cheapest
model that clears the bar" is only honest if the bar is re-checked as the
data the system reasons over changes. This is a
single-tenant prototype demonstrating judgment, not
production infrastructure: no Kubernetes, no multi-tenant concerns, no
deployment pipeline are in scope. The interview this project is built for is
scheduled for Tuesday; scope decisions MUST be weighed against that fixed
date rather than expanded for its own sake.

## Development Workflow

Development follows Spec-Driven Development via GitHub Spec Kit
(`/speckit-specify` → `/speckit-plan` → `/speckit-tasks` →
`/speckit-implement`), with this constitution taking precedence over any
individual spec, plan, or task when they conflict. `CLAUDE.md` at the repo
root carries the operational architecture summary for day-to-day agent
context; this constitution is the authority when the two diverge. Any
material not intended for the evaluator to see — personal analysis of the
interviewer, interview strategy, or similar — MUST NOT be committed to this
repository, enforced via `.gitignore`, regardless of repository visibility.
The evaluation harness (accuracy, consistency, refusal-correctness) MUST be
built and run before interface polish work begins, and its results,
including failures, MUST be reported rather than hidden.

## Governance

This constitution supersedes CLAUDE.md and any other project guidance where
they conflict. Amendments require: a stated rationale, a version bump under
semantic versioning (MAJOR for principle removal/redefinition, MINOR for a
new or materially expanded principle/section, PATCH for wording/clarity
fixes), and an updated Sync Impact Report at the top of this file. Any
`/speckit-plan` or `/speckit-tasks` output that conflicts with a principle
above MUST be revised before implementation proceeds — complexity or
convenience is not sufficient justification to override a principle.

**Version**: 1.2.0 | **Ratified**: 2026-08-27 | **Last Amended**: 2026-08-29
