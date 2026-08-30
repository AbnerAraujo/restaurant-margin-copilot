# Quickstart: Validating the Daily Margin & Growth Copilot

## Prerequisites

- Go 1.27, Node/npm, PostgreSQL running (`docker compose up -d` once
  `docker-compose.yml` exists — not yet created)
- `ANTHROPIC_API_KEY` set (Console API account, not the Claude Pro/Max subscription)
- Hand-authored test data present (Day 1 task; today: `backend/cmd/gendata/opening/`, embedded into the generated `backend/data/live/`)

## Validate User Story 1 — reconciled margin

```
migrate -path backend/migrations -database "$DATABASE_URL" up
cd backend && go run ./cmd/server -ingest data/live
```

(The Go module root is `backend/`, not the repo root — `go run
./backend/cmd/server ...` from the repo root fails with "cannot find main
module". QA finding, see `README.md`'s Getting Started section and
`CHANGELOG.md` for the same fix applied there.)

Expected: a `DailyReconciliation` row for 2026-08-01 whose `margin` matches
an independently hand-computed value from the same source files, with
`source_row_refs` pointing at real rows — including correct handling of the
day containing the deliberate duplicate order and refund.

## Validate User Story 2 — natural-language Q&A

Ask, via the frontend or a direct API call: *"how did this week compare to
last week?"* Expected: an answer citing `get_margin_delta`'s output, with
the specific date ranges used stated back to you.

## Validate User Story 3 — refusal

Ask a question referencing a supplier not present in any cost sheet.
Expected: an explicit refusal naming what's missing — not a plausible
number.

## Validate User Story 4 — promotion ROI flag

Ask about a promotion whose spend exceeds its attributed incremental
revenue. Expected: it's returned by `list_negative_roi_promotions` and the
NL answer states the negative ROI with provenance.

## Validate the evaluation harness (once built, Day 4)

```
promptfoo eval -c evaluation/promptfoo/accuracy.yaml
promptfoo eval -c evaluation/promptfoo/consistency.yaml
promptfoo eval -c evaluation/promptfoo/refusal.yaml
```

Expected: real pass/fail numbers reported, including failures — not a
target asserted in advance (Constitution Principle V, spec SC-001–SC-003).

## Full real-file trial (the "try with a real restaurant/bar" goal)

Point `-ingest` at a directory of a real owner's actual
delivery-platform, POS, and cost-sheet exports (promotion export optional).
Re-run ingestion. If a real column shape breaks parsing, that's a finding
for the "where the model/build got it wrong" section — log it in
`docs/plan.md`'s running mistakes log, don't silently patch around it.
