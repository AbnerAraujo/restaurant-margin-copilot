# Tooling inventory

## Claude Code skills

**Global** (`~/.claude/skills`, available in any project):

| Skill | Source | Purpose |
|---|---|---|
| `sdd` | [SpillwaveSolutions/sdd-skill](https://github.com/SpillwaveSolutions/sdd-skill) | Generic Spec-Driven Development guidance |
| `golang-project-layout` | [samber/cc-skills-golang](https://github.com/samber/cc-skills-golang) | cmd/internal/pkg conventions |
| `golang-error-handling` | samber/cc-skills-golang | Wrapped errors, sentinel errors |
| `golang-testing` | samber/cc-skills-golang | Table-driven tests, benchmarks |
| `golang-stretchr-testify` | samber/cc-skills-golang | assert/require/mock/suite patterns |
| `react-testing` | [citypaul/.dotfiles](https://github.com/citypaul/.dotfiles) | Vitest + React Testing Library patterns |
| `front-end-testing` | citypaul/.dotfiles | General UI testing patterns (paired with react-testing) |
| `tdd` | citypaul/.dotfiles | Red-green-refactor workflow/gate rules |
| `clean-names`, `clean-functions`, `clean-comments`, `clean-general`, `clean-tests`, `boy-scout`, `typescript-clean-code` | [ertugrul-dmr/clean-code-skills](https://github.com/ertugrul-dmr/clean-code-skills) (typescript track) | Clean-code checks for the React/TS frontend |
| `hexagonal-architecture` | citypaul/.dotfiles | Ports-and-adapters — keeps the deterministic reconciliation core isolated behind the MCP tool boundary (Principle III) |
| `domain-driven-design` | citypaul/.dotfiles | Modeling the reconciliation domain (margin, discrepancy, period) cleanly |
| `bff-design`, `bff-entry-points` | citypaul/.dotfiles | Shape of the API boundary between the React frontend and the Go backend |
| `api-design` | citypaul/.dotfiles | General API design principles for the Go backend's HTTP surface |
| `observability` | citypaul/.dotfiles | General observability patterns — pairs with the instrumentation requirement (Principle VI) |
| `twelve-factor` | citypaul/.dotfiles | Backend service hygiene (config, logs, dependencies) |
| `golang-design-patterns` | samber/cc-skills-golang | Idiomatic Go patterns (vs. porting OOP patterns verbatim) |
| `golang-dependency-injection` | samber/cc-skills-golang | DI approaches in Go, keeping the reconciliation engine testable |
| `golang-context` | samber/cc-skills-golang | `context.Context` propagation — underlies the per-tool-call timeouts Principle III requires |
| `golang-observability` | samber/cc-skills-golang | Go-specific structured logging/metrics for the instrumentation log |
| `golang-database` | samber/cc-skills-golang | Go/Postgres data-access patterns, pairs with `sqlc`/`pgx` |
| `golang-security` | samber/cc-skills-golang | Baseline Go backend security hygiene |

**Project-local** (`.claude/skills` in this repo, from `specify init --integration claude`):

`speckit-constitution`, `speckit-specify`, `speckit-plan`, `speckit-tasks`, `speckit-implement`, `speckit-clarify`, `speckit-analyze`, `speckit-checklist`, `speckit-converge`, `speckit-taskstoissues` — the actual Spec Kit workflow commands.

No Go equivalent of clean-code-skills was installed — `cc-skills-golang`'s own style/naming/refactoring skills already cover that ground for the backend, so nothing else was added to avoid duplicating guidance.

## CLI tools (Homebrew)

| Tool | Version | Purpose | Status |
|---|---|---|---|
| `promptfoo` | 0.122.0 | LLM eval harness — will structure `evaluation/`'s accuracy/consistency/refusal-correctness tests | Installed |
| `sqlc` | 1.31.1 | Generates type-safe Go from SQL queries | Installed |
| `golang-migrate` | 4.19.1 | Postgres schema migrations | Installed |

## Deferred — added when the relevant code exists, not before

These don't make sense to install standalone; they're pulled in as dependencies once the corresponding module/app is scaffolded.

| Item | When | Command |
|---|---|---|
| `pgx/v5` | When `backend/go.mod` is created | `go get github.com/jackc/pgx/v5` |
| `testify` | Same — pulled in by the `golang-stretchr-testify` skill's own conventions | `go get github.com/stretchr/testify` |
| Vitest + React Testing Library | When `frontend/package.json` is created | `npm install -D vitest @testing-library/react @testing-library/jest-dom` |
| shadcn AI Elements | When the chat UI is built (needs shadcn/ui + Tailwind initialized first) | `npx shadcn@latest init` then `npx ai-elements@latest` (or `add <component>` for one at a time, e.g. `conversation`, `message`) |

## Explicitly not installed

- **Kubernetes MCP servers** (`Flux159/mcp-server-kubernetes`, `containers/kubernetes-mcp-server`) — this is a single-tenant prototype with no live cluster to manage; the brief itself says "not a production system."
- **Any agent framework** (LangChain, etc.) — the brief explicitly calls for direct OpenAI API calls with defined tools, not a framework.
- **A database-access MCP server as a dev tool** — the MCP tool layer (`mark3labs/mcp-go`) is the product's own deliberate boundary between the LLM and Postgres; a second, separate DB-access MCP would duplicate or undermine that boundary.
