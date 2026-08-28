# CLAUDE.md — Restaurant Margin Copilot

Take-home prototype: daily close and margin alert for an independent restaurant.
The owner receives sales data from delivery platforms and the in-house POS, and
carries input costs. Nobody reconciles this daily because it's tedious — margin
slippage is discovered when the month closes, too late to act. This product
ingests the files, reconciles them, and answers natural-language questions about
the day and the week, flagging what changed and why.

## Non-goals
- Not a general restaurant assistant. Not an empty "ask me anything" chat box.
- Not a multi-agent architecture for its own sake.
- Not a production system — a prototype that demonstrates judgment, built to be
  handed to and opened by someone else.

## Architecture: the deterministic/probabilistic split must be visible

**Deterministic (Go, no model):** all arithmetic. Ingestion, parsing,
reconciliation, margin calculation, week-over-week deltas, anomaly thresholds.
Numbers are never produced by an LLM.

**Probabilistic (model, via MCP tools):** interpreting the user's question,
explaining a computed result in plain language, flagging what deserves
attention. The model never computes — it calls typed MCP tools backed by the Go
reconciliation engine and narrates the result.

This split must be documented and easy to point at in a demo.

## Stack

- **Backend:** Go — ingestion, reconciliation engine, margin/delta calculation,
  the API the frontend and MCP server both sit on.
- **Database:** PostgreSQL — raw ingested records, computed reconciliations,
  per-interaction instrumentation log.
- **MCP server:** exposes the Go reconciliation engine as a fixed set of typed
  tools (e.g. `get_daily_summary`, `get_margin_delta`, `list_discrepancies`).
  No open SQL, no free-form computation tool — defined, typed operations only.
- **Frontend:** React — chat-style Q&A, plus a visible cost/token panel and
  provenance display (which file, which rows, which period, for every number
  shown).
- **LLM:** Anthropic API, called directly with the MCP tool definitions — no
  agent framework. Model choice per step is a documented decision: **Claude
  Haiku 4.5** for the ambiguity gate (cheap classification task), **Claude
  Sonnet 5** for the explanation step (narrates an already-computed number —
  doesn't need frontier reasoning, so no Opus/Fable here).

## Pre-processing gate before execution
Before anything runs, evaluate the question in isolation:
- Is it answerable with the data available?
- Is it ambiguous? (e.g. "how was the weekend" — does it include Friday?)
- If ambiguous: either ask a clarifying question, or state the assumption taken.

## Hard limits where error is expensive
- The system **refuses** rather than estimates when data is missing or incomplete.
- No open SQL / no free-form computation against the data. Defined, typed
  MCP tools only.
- Timeouts on any tool call. Explicit cap on loop iterations.
- Every number shown must carry its provenance — which file, which rows, which
  period.

A confidently wrong margin figure is worse than a refusal.

## Soft limits where flexibility helps
Nudge the model toward the right framing without hard-coding the answer, so it
can still self-correct on phrasing, follow-ups, and unexpected question shapes.

## Instrumentation (build from the first API call, not at the end)
Log per interaction, in Postgres:
- input tokens, output tokens, model used
- estimated cost in USD
- latency
- whether the clarifying-question path fired
- whether a refusal fired

Surface a running total in the React UI.

## Evaluation plan (build it, don't just describe it)
A test harness in `evaluation/` that produces real numbers for the write-up:
- **Accuracy:** ~15–20 questions with known correct answers computed
  independently from the fixture data.
- **Consistency:** ~5 questions asked in 3 phrasings each — do the answers agree?
- **Refusal correctness:** ~5 questions that cannot be answered from the data —
  correct behavior is refusal, not a plausible guess.

Report all three with actual numbers, including failures. Do not hide them.

## Fixture data (`fixtures/`)
Realistic synthetic CSVs — delivery platform export, POS export, supplier cost
sheet. Include the mess on purpose: a duplicate order, a refund, a missing day,
an inconsistent date format. The mess is what makes the reconciliation logic
worth showing.

## Deliverables, in order of importance
1. A working prototype that can be opened and used.
2. A one-page reasoning document: job chosen and why, deterministic/probabilistic
   boundary, hard/soft limits, evaluation numbers (including failures), cost per
   interaction measured, and what was decided *not* to build, and why.
3. A short demo, including at least one case where the system refuses.

## Build order (do not reorder — this is the whole point)
1. Fixture data first, including the deliberate mess.
2. Deterministic reconciliation layer in Go, proven with tests — before any LLM
   call exists.
3. MCP server wrapping the reconciliation engine as typed tools.
4. Model layer, starting with the ambiguity gate.
5. Instrumentation, from the first API call.
6. Evaluation harness — before polishing the interface.
7. React frontend last.

If the demo is beautiful and the numbers are wrong, the challenge is failed.
