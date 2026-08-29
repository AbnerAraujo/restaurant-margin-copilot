# Overnight Build — Test Plan & Verification Checklist

Use this in the morning to verify what the overnight agents actually built,
independent of what any agent's own self-report claims. Every check below
maps to a real test file and a real acceptance scenario already defined in
`specs/001-margin-reconciliation-qa/spec.md` — nothing here is new scope.

## What can be verified tonight (no Docker, no API key needed)

| User Story | Test file | Run with | Passes when |
|---|---|---|---|
| US1 — reconciled margin | `backend/internal/ingest/ingest_test.go` | `go test ./internal/ingest/...` | Duplicate order, refund, missing day, inconsistent date format all handled per spec.md Acceptance Scenarios 1–3 |
| US1 — reconciled margin | `backend/internal/reconcile/reconcile_test.go` | `go test ./internal/reconcile/...` | Margin figure matches independently hand-computed value; discrepancy flags fire correctly |
| US4 — promo ROI | `backend/internal/reconcile/promo_test.go` | `go test ./internal/reconcile/...` | Negative-ROI case flagged; missing-attribution case returns `roi: null`, never a guessed number (FR-013) |
| US2/US3 (partial) | `backend/internal/mcptools/*_test.go`, `backend/internal/ambiguity/gate_test.go` | `go test ./internal/mcptools/... ./internal/ambiguity/...` | Tool logic and ambiguity classification correct against mocked storage/responses — this tests routing logic, not live model calls |
| Frontend | `frontend/src/components/**/*.test.tsx` | `npm test` (Vitest) | Components render correctly against mocked API responses shaped like the real contracts |
| Full backend | — | `go build ./...` | Compiles cleanly — this is where the two parallel tracks (US2/US3 and US4) get merged; a build failure here means a real integration conflict, not a false alarm |

## What CANNOT be verified until you resolve the two real blockers

| Blocker | Unblocks | Then run |
|---|---|---|
| Start Docker Desktop, `docker compose up -d` | Postgres-backed storage tests, T006 migrations, T038 full quickstart | `migrate -path backend/migrations -database "$DATABASE_URL" up`, then `go test ./internal/storage/...` |
| Set `ANTHROPIC_API_KEY` (Console API account, not Claude Pro/Max) | Live ambiguity gate (Sonnet 5) and explain (Sonnet 5) calls, T034 eval harness | `promptfoo eval -c evaluation/promptfoo/accuracy.yaml` (and consistency.yaml, refusal.yaml) |

## Acceptance scenarios to manually re-check against spec.md

Don't just trust "tests pass" — walk these by hand once Docker + the API key
are live, since they're the actual acceptance criteria, not just unit tests:

1. **US1, Scenario 2** (`spec.md`): a day with a duplicate order and a refund — confirm the duplicate isn't double-counted and the refund is netted, *visibly*, in the provenance trail shown in the UI, not just correct in the database.
2. **US2, Scenario 2**: ask the same underlying question in 3 phrasings — confirm all 3 agree.
3. **US3, Scenario 1**: ask about data not present in any fixture file — confirm an explicit refusal naming what's missing, not a plausible number.
4. **US4, Scenario 3**: ask about a promotion with incomplete attribution — confirm refusal, not an estimated ROI.
5. **SC-004**: pick any 3 numbers shown anywhere in the UI at random — confirm each has a checkable source citation, zero exceptions.

## Honesty check on the agents' own reports

Each overnight agent was instructed to report what it verified vs. what it
could not, and to never fake a passing result. When reviewing their reports
in the morning:
- If an agent claims a test "passed" for something that needs the live API
  key or Postgres, that's a red flag — re-run it yourself before trusting it.
- If `T039`'s mistakes-log entry in `docs/plan.md` is suspiciously empty
  ("no mistakes found"), read the actual test output yourself rather than
  accepting that at face value — some friction is expected on a from-scratch
  build this size.
