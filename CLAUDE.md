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
  the API the frontend and MCP server both sit on. One binary, one origin, in
  three named layers:
  - `internal/bff` — the **backend-for-frontend boundary** (`specs/013-bff-layer`).
    The composition root: one route table declared as data, from which the CORS
    preflight, the 405 policy, and the startup log are all *derived* rather than
    hand-maintained beside it. It shapes and routes; it computes nothing. This is
    a **modular** BFF, not a service — one experience, one consumer, one team, so
    a separate deployable would buy a boundary that already holds and charge a
    network hop and a pipeline for it. Also deliberately without retries,
    breakers, or bulkheads: the connector "upstream" is an in-process function
    call, and simulating flakiness so resilience code has something to catch
    would be fiction stacked on fiction.
  - `internal/httpapi` — the handlers behind that table: request shaping,
    orchestration, and rendering for the one client. No arithmetic, no domain
    rules.
  - The deterministic core — `internal/ingest`, `internal/reconcile`,
    `internal/pipeline`, `internal/platformconnector`, `internal/storage`. Every
    number lives here.
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
  Sonnet 5** for the ambiguity gate and the explanation step, **Claude
  Haiku 4.5** for the paraphrase-match cache classifier. The gate started on
  Haiku 4.5 (cheap classification task) and moved to Sonnet 5 on 2026-08-29
  after Haiku repeatedly misclassified in-range questions once the live
  dataset grew past a single year. Corrected record: that failure was
  date-range arithmetic, which no model should ever have owned — a
  deterministic Go pre-check (`internal/ambiguity/daterange.go`) now
  refuses explicit out-of-range dates before any model call and hands
  in-range verdicts to the gate as precomputed fact; Sonnet 5 stays only
  for the genuinely linguistic residual — see `internal/llmclient/cost.go`'s
  doc comment for the full rationale. A sibling pre-check,
  `internal/ambiguity/weekend.go`, catches a bare "the weekend" with no
  days named and no explicit date and asks which days count — the exact
  case named below, moved from a judgment call left to the gate to a
  deterministic definition this product actually has.

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
- Every number the model narrates is checked, not trusted. `internal/answerverify`
  extracts each money/percentage figure a narration states and requires it to
  match a value the tool results returned (or one Go can rederive from two of
  them in one operation) before the answer is served — a mismatch is a
  refusal (`numeric_validation_failed`), never a served answer.

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
  independently of the system, from the hand-authored test data.
- **Consistency:** ~5 questions asked in 3 phrasings each — do the answers agree?
- **Refusal correctness:** ~5 questions that cannot be answered from the data —
  correct behavior is refusal, not a plausible guess.

Report all three with actual numbers, including failures. Do not hide them.

## Test data (the dataset's hand-authored opening window)
Realistic synthetic CSVs — delivery platform export, POS export, supplier cost
sheet. Include the mess on purpose: a duplicate order, a refund, a missing day,
an inconsistent date format. The mess is what makes the reconciliation logic
worth showing. This lives as the first 14 days of the product's one
continuous dataset (`backend/cmd/gendata/opening/`, hand-authored and
independently verified before any reconciliation code touched it), with the
rest of the multi-year history generated behind it by `cmd/gendata` — one
dataset, one ingestion path, same realistic dollar scale throughout.

## Deliverables, in order of importance
1. A working prototype that can be opened and used.
2. A one-page reasoning document: job chosen and why, deterministic/probabilistic
   boundary, hard/soft limits, evaluation numbers (including failures), cost per
   interaction measured, and what was decided *not* to build, and why.
3. A short demo, including at least one case where the system refuses.

## Build order (do not reorder — this is the whole point)
1. Hand-authored test data first, including the deliberate mess.
2. Deterministic reconciliation layer in Go, proven with tests — before any LLM
   call exists.
3. MCP server wrapping the reconciliation engine as typed tools.
4. Model layer, starting with the ambiguity gate.
5. Instrumentation, from the first API call.
6. Evaluation harness — before polishing the interface.
7. React frontend last.

If the demo is beautiful and the numbers are wrong, the challenge is failed.
