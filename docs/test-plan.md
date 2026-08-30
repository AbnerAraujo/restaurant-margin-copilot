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
3. **US3, Scenario 1**: ask about data not present in any source file — confirm an explicit refusal naming what's missing, not a plausible number.
4. **US4, Scenario 3**: ask about a promotion with incomplete attribution — confirm refusal, not an estimated ROI.
5. **SC-004**: pick any 3 numbers shown anywhere in the UI at random — confirm each has a checkable source citation, zero exceptions.

## Chart & visual-rendering quality (standing practice, added 2026-08-30)

The checks above prove the frontend is *functionally* correct: the right
numbers reach the right components, provenance is attached, refusals render
as refusals. They say nothing about whether a chart is *readable*. That gap
is real and has produced a string of separately-reported bugs — bars
overlapping into a solid block, an axis capped at 200+ cramped gridlines, a
month caption drawn thousands of pixels off-screen — each caught by a person
looking at the live app, one at a time, after it shipped.

Two checks close it. Both are cheap; neither replaces the functional passes.

### 1. Unit-test the chart maths (committed, runs in `npm test`)

Anywhere a chart computes visual output from data — tick generation, domain
and scale, label formatting, bar width and spacing — that is a pure function
and gets tested like backend logic, directly, with a table of inputs.
Rendering the component and eyeballing the SVG proves the markup changed; it
does not prove the axis is readable at $128,000.

- `frontend/src/lib/chartScale.ts` + `chartScale.test.ts` — the nice-number
  tick algorithm, domain rounding, and axis label formatting shared by every
  value axis. Tested against the spans the data actually reaches: a $50 day,
  the hand-authored $650 14-day window, the live $36,322 weekly-bucket range,
  a $128,400 multi-year roll-up, and a $0.40 cents-scale span. The assertions
  are properties, not golden values: tick count stays in band, every step is
  a 1/2/5 × 10^n, the domain always brackets the real extent, no two adjacent
  ticks ever render the same label.
- Each chart's own `buildScale` is exported for the same reason.

**When you add or change a chart:** if it does arithmetic to decide where
something is drawn, that arithmetic gets a test, and the test's input table
includes the largest scale the live dataset reaches — not a tidy fixture.

### 2. Render every chart against live data and look at it (ephemeral)

Run before shipping any chart change. Not a committed suite — it needs a
running backend, a running frontend, and a human (or agent) judging the
picture, which is not what CI is for. Playwright is installed ad hoc and not
added to `package.json`.

Procedure:

1. `cd frontend && npx vite --port 8099 --strictPort` (never 8080 — that is
   the live app) against the same Postgres.
2. Drive a headless Chromium to `/close`, `/promotions`, `/platforms` — every
   route carrying a chart — and screenshot each chart element in **both**
   themes (set `localStorage['mbs.theme.preference.v1']`). Wait for the splash
   overlay (`div.fixed.inset-0.z-50`) to detach first, or you will screenshot
   the splash.
3. Use **live `/api/*` data at current scale**, never a synthetic fixture with
   small tidy numbers. Every axis bug found so far was invisible at fixture
   scale and obvious at real scale.
4. **Actually open each screenshot and judge it** against the `dataviz`
   skill's `marks-and-anatomy.md` and `anti-patterns.md`: tick density and
   round numbers, label collisions, axis titles and units, mark thickness and
   spacing, legend necessity, gridline weight, contrast in both themes.
   "It rendered without crashing" is not the check.
5. Assert two things numerically rather than by eye, since both are easy to
   get wrong and easy to measure:
   - `document.documentElement.scrollWidth` must not exceed the viewport —
     a chart that widens the whole page is a layout bug.
   - **Frozen axis:** scroll the chart's own container with a real wheel
     event, then compare the axis label's viewport `x` before and after. It
     must be unchanged while a bar's `x` has moved.

Fix everything the pass turns up, then re-run it — the fixes themselves
introduce new visual defects often enough to be worth the second look.

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
