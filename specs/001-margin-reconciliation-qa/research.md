# Phase 0 Research: Daily Margin & Growth Copilot

No `NEEDS CLARIFICATION` markers remain from `plan.md`'s Technical Context —
every decision below was already made and evidenced earlier this session
(`CLAUDE.md`, the constitution, `docs/tooling.md`, `docs/product-strategy.md`).
This file consolidates them in the Decision/Rationale/Alternatives format for
traceability, not to re-litigate them.

## LLM vendor and model split

- **Decision**: Anthropic API, direct calls via `anthropic-sdk-go`. Claude Haiku 4.5 for the ambiguity gate; Claude Sonnet 5 for the explanation step.
- **Rationale**: Builder has an existing Anthropic subscription/API account rather than a separate OpenAI one. Haiku 4.5 ($1/$5 per MTok) is the cheapest current model, appropriate for a classification-shaped task (answerable/ambiguous?). Sonnet 5 ($2/$10 per MTok) is a real cost/quality comparison against Opus 5/Fable 5 — the explanation step narrates an already-computed number, which doesn't need frontier reasoning.
- **Alternatives considered**: OpenAI API (original brief default — rejected once the Anthropic subscription made it redundant to pay for two vendors). Opus 5/Fable 5 for explanation — rejected on cost given the task doesn't require their reasoning depth.

## MCP tool layer

- **Decision**: `mark3labs/mcp-go`, in-process with the Go reconciliation engine.
- **Rationale**: Keeps the typed-tool boundary (Constitution Principle III) in the same language and process as the domain logic it wraps — no second language or network hop for something that's supposed to be a hard boundary, not an integration point.
- **Alternatives considered**: TypeScript/Python MCP servers from `modelcontextprotocol/servers` (would require a second language and process); Anthropic's native MCP connector (`mcp_servers` + `mcp_toolset`, URL-based) — viable but requires the MCP server to be independently HTTP-reachable, an unnecessary deployment step for a local prototype; deferred as a possible simplification, not adopted now.

## Database access

- **Decision**: `sqlc` (generates typed Go from SQL) + `pgx/v5` (driver) + `golang-migrate` (schema migrations).
- **Rationale**: Type-safe, no ORM magic, matches the "typed operations only" ethos already applied to the MCP boundary — extended down into the DB layer.
- **Alternatives considered**: GORM/other ORMs — rejected as unnecessary abstraction for a schema this small; raw `database/sql` — rejected since `sqlc` gives the same safety with less boilerplate.

## Evaluation harness

- **Decision**: `promptfoo` structures `evaluation/promptfoo/` for the accuracy, consistency, and refusal-correctness suites.
- **Rationale**: Its assertions/repeat/redteam features map close to 1:1 onto the three required evaluation axes (spec SC-001–SC-003, SC-006).
- **Alternatives considered**: A hand-rolled Go harness (viable, more work for no clear benefit); `openai/evals` / `lm-evaluation-harness` — wrong shape (benchmark grading, not application-level eval).

## Frontend chat UI

- **Decision**: React + Vitest + React Testing Library; shadcn AI Elements for chat/tool-call presentational components.
- **Rationale**: AI Elements components make the deterministic-tool-call vs. LLM-narration split visible in the UI, reinforcing the project's central architectural claim rather than just implementing it invisibly.
- **Alternatives considered**: Hand-rolled chat UI (more work, no reinforcement benefit); a heavier agent-facing UI framework — rejected as scope creep for a UI that's presentational, not agentic, on the frontend side.

## Real-file compatibility (spec Assumptions)

- **Decision**: `internal/ingest/` parses generic, realistic CSV shapes per source type (column-name matching with reasonable tolerance for the delivery/POS/cost/promo formats), not exact-header-only columns.
- **Rationale**: The user wants to try this with a real restaurant/bar's actual export files, not only this project's own CSVs — ingestion needs to survive real-world column naming and ordering variance, not just one exact shape.
- **Alternatives considered**: Hard-coding this project's own column headers (fastest to build, fails the real-file goal) — rejected.
