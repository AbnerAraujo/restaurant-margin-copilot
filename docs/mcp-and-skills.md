# MCP typed tools, and the Claude Code skills used to build this

**Status:** Reference · **Scope:** `backend/internal/mcptools/`,
`backend/internal/explain/`, `.claude/skills/`, `~/.claude/skills/`

This is the grep-able, in-editor companion to `docs/architecture.html`'s
existing MCP coverage — the ask-pipeline diagram's "MCP tool layer" node, the
"boundary enforced by the import graph" table (`internal/mcptools` row: "no
import path to a model, read-only Postgres"), and the "Hard limits" card's
"Eight defined, typed MCP tools, and nothing else" line — that card has
already been corrected twice as the tool count grew (it briefly read "Six",
then "Seven" after `get_period_totals` shipped, before
`get_expense_pattern_by_day_of_month` became the eighth; see Section 1
below for the disclosed inconsistency this doc itself carried the same
way). That HTML is
the diagram; this document is for someone with the code open, the same relationship
`docs/frontend.md` has to `docs/architecture.html`'s design-system tab. Where
the two overlap, this doc cross-references the HTML section by name rather
than re-describing the same diagram in prose.

The second half of this document is a different kind of artifact entirely:
not the product's code, but the record of which Claude Code skills actually
shaped that code and this repo's own documents, cited against real commits —
not a list of everything installed.

## 1. The MCP typed tool layer

### Why MCP at all, and why in-process

`backend/internal/mcptools` is, per its own package doc comment
(`types.go`), "the ONLY path from the model layer to `internal/reconcile`'s
persisted output" — the code-level enforcement of Constitution Principle
III. It's built on `mark3labs/mcp-go`, and it runs **in-process**: there is
no separate MCP server binary, no network transport, no localhost port. The
whole server is a Go value (`*server.MCPServer`) built by
`mcptools.RegisterMCPServer(q storage.Querier)` and handed straight to
`internal/explain.New`.

The reason for in-process isn't just "simpler for a prototype" — it's
structural, and `explain.go`'s own doc comment on `New` states it precisely
enough to quote rather than paraphrase:

> Explainer talks to it via mcp-go's in-process client
> (`client.NewInProcessClient`), which is what actually routes every tool
> call through the timeout+call-cap middleware `RegisterMCPServer` installs
> (`mcptools/limits.go`): calling a `*ServerTool`'s Handler directly,
> bypassing the client, would skip that middleware entirely, defeating
> Constitution Principle III's enforcement.

In other words: `mcp-go`'s `server.MCPServer` exposes registered tools as
`*ServerTool` values, and nothing in Go stops a careless caller from
reaching in and invoking a tool's `Handler` field directly — except that
doing so would silently skip the timeout and call-cap middleware
(`s.Use(...)` in `server.go`) that only fires on the dispatch path the
*client* uses. Routing every call through `client.NewInProcessClient`
(`explain.go` line 127) rather than holding a reference to the server's
tools is what makes bypassing the middleware require a deliberate, visible
new code path rather than an easy shortcut. `explain.go`'s `callTool`
(line 345) confirms the discipline is followed: every tool invocation goes
through `e.mcpClient.CallTool(...)`, never a direct handler call.

`explain.go`'s wiring: `New` builds the in-process client, calls
`Initialize` and `ListTools` against it once, and turns the result into
Anthropic tool definitions (`anthropicTools(listed.Tools)`) — so the tool
schemas the model actually sees are generated from the live MCP server's own
registrations, not a hand-maintained second copy.

### The exact 8 typed tools

`server.go`'s `RegisterMCPServer` calls exactly five registrars —
`registerReconciliationTools`, `registerPromoTools`,
`registerPlatformComparisonTool`, `registerPeriodTools`,
`registerDayOfMonthPatternTools` — which together
call `s.AddTool` eight times. Confirmed by reading each `register*`
function directly. `docs/architecture.html` has since been corrected to
match (it read "Six defined, typed MCP tools", then "Seven" after
`get_period_totals` shipped, before `get_expense_pattern_by_day_of_month`
became the eighth):

| # | Tool | File | Purpose | Refuses to do |
|---|---|---|---|---|
| 1 | `get_daily_summary` | `reconciliation_tools.go` | One calendar date's full `DailyReconciliation`: gross sales by source, `total_delivery_gross_sales` (Go-summed, POS excluded — see the field's own doc comment on why naming it `TotalGrossSales` would be wrong), commissions, refunds, input costs, margin, discrepancy flags, source-row provenance. | Returns `{"error":"no_data"}` — never a partial or estimated summary — if no `DailyReconciliation` row was ever computed for that date. |
| 2 | `get_margin_delta` | `reconciliation_tools.go` | Margin delta between two `{start,end}` periods (`period_b` minus `period_a`), each side carrying its own `days_included` and `source_row_refs`. | `periodMargin` walks every calendar day in each period; if **any** day is missing a persisted reconciliation, returns `{"error":"insufficient_data","missing":[...dates...]}` and computes nothing — never a delta over partial coverage. |
| 3 | `list_discrepancies` | `reconciliation_tools.go` | `discrepancy_flags` for one date or one period, days with zero flags omitted (it surfaces exceptions, not a full calendar). | `invalid_input` if both `date` and `period` are given, or neither — never guesses which the caller meant. |
| 4 | `get_promotion_roi` | `promo_tools.go` | ROI lookup by exact/fuzzy `campaign_id`, or by `platform`+`period`. Accepts a shortened form or a full display name via `campaign_match.go`'s bounded matcher. | `roi` is `null` with `reason: "attribution_unavailable"` (FR-013) when incremental revenue can't be attributed — never a computed-looking number. `matchCampaignID` returns `""` (no match) rather than guess when a fragment is ambiguous across more than one known campaign. |
| 5 | `list_negative_roi_promotions` | `promo_tools.go` | Every campaign with negative ROI in a period. | A promotion with unattributable ROI is never included — "not known to be negative" is a different fact from "negative," and the tool doesn't conflate them. |
| 6 | `compare_platform_economics` | `platform_comparison_tools.go` | iFood vs. Just Eat Takeaway side by side for one period: gross sales, commission paid, effective rate, promo spend, combined cost/rate. The one tool a comparison question must resolve to — its own description tells the model never to reconstruct a comparison from two single-platform calls. | `effective_rate`/`combined_effective_rate` are `null`, never a fabricated `"0.00%"`, when `gross_sales` is zero (divide-by-zero guarded in `effectiveRatePercent`). `insufficient_data` if any calendar day in the period has no persisted reconciliation. |
| 7 | `get_period_totals` | `period_tools.go` | Sums and ranks an ENTIRE period's `DailyReconciliation` rows in one call: per-source gross sales, `total_delivery_gross_sales`, commissions, refunds (with a new `refunds_by_source` per-platform breakdown), input costs, margin total, `avg_daily_margin` (via `money.DivRoundHalfUp`), and which single day was `best_day`/`worst_day` by margin — all carrying one combined `source_row_refs` list. Its own file doc comment names the real gap it closes: a failing period-total eval question, and an observed case where "which day had the most profit and why" burned through the per-interaction tool-call budget calling `get_daily_summary` once per day, since `get_margin_delta` only sums margin as one side of a two-period delta — no per-source breakdown, no best/worst-day ranking. | `{"error":"insufficient_data","missing":[...dates...]}` and totals nothing if any calendar day in `[start,end]` has no persisted reconciliation — the same policy `periodMargin` already enforces for `get_margin_delta`. On an exact best/worst-day margin tie, the chronologically earliest date wins both slots — this tool's own documented tie-break, since `contracts/mcp-tools.md` doesn't prescribe one. |
| 8 | `get_expense_pattern_by_day_of_month` | `day_of_month_pattern_tools.go` | Groups every reconciled day in a period by its DAY-OF-MONTH (1st–31st) and averages total expense (commissions + refunds + input costs) for each day-of-month across however many months in the period contain it, ranking which position in the month runs highest/lowest on average — a RECURRING-position grouping `get_period_totals` cannot produce, since that tool ranks by one specific calendar date within a single period. Added after a real live question ("is the 15th typically my worst day?") that no existing tool could answer without reconstructing the grouping client-side. | Returns `{"error":"insufficient_data"}` — never a pattern computed against partial coverage — if any calendar day in the period has no persisted reconciliation. |

Every result struct renders money as a `FormatCents`-style decimal string
(`"-12.34"`), the same convention `internal/storage`'s JSON surfaces
already use — the model receives numbers pre-formatted the way a human
reads them, never a raw integer-cents value it might misread as dollars.

Five of the eight (`get_promotion_roi`, `list_negative_roi_promotions`,
`compare_platform_economics`, `get_period_totals`,
`get_expense_pattern_by_day_of_month`) use
`mcp.NewTypedToolHandler` / `mcp.NewToolResultStructuredOnly`, a newer,
typed adapter pattern than the first three's hand-written
`req.BindArguments` + `jsonResult`/`errorResult` helpers
(`reconciliation_tools.go`'s bottom section) — both are the same
package convention (a `(*Result, *ToolError, error)` three-way return from a
"core" function, per `types.go`'s doc comment), just two tool-file
generations of the same design. `period_tools.go`'s own doc comment
confirms it deliberately joined the newer generation: "Follows
platform_comparison_tools.go's/promo_tools.go's current convention (this
package's most recently added tools) ... rather than
reconciliation_tools.go's older hand-rolled BindArguments/jsonResult pair,
which predates that typed-handler helper's introduction into this
package."

### The middleware-enforced timeout and call cap

`limits.go` defines two constants read directly from source:

- `DefaultToolTimeout = 5 * time.Second` — bounds every tool call.
  `contracts/mcp-tools.md` states 5s explicitly for `get_daily_summary`
  only; this package applies the same bound to all eight uniformly, per the
  constant's own comment, "since none of them do anything `get_daily_summary`
  doesn't already do (a handful of indexed Postgres reads)."
- `DefaultMaxToolCallsPerInteraction = 8` — the hard per-interaction
  tool-call cap. `explain.go`'s `MaxTurns` constant is
  `mcptools.DefaultMaxToolCallsPerInteraction + 3` = **11** turns, matching
  `docs/architecture.html`'s own "Capped at 1024 output tokens and 11
  turns" line for `internal/explain`.

Both are enforced in exactly one place: `timeoutAndBudgetMiddleware`,
installed via `s.Use(timeoutAndBudgetMiddleware(DefaultToolTimeout))` in
`RegisterMCPServer` — "applied to every `AddTool`'d handler, not something
a caller could accidentally bypass by holding a `*ServerTool` directly," per
`server.go`'s own comment, the same discipline `explain.go`'s in-process-client
choice depends on.

**The real bug this session fixed** (commit `abea19a`, "Add fake-Querier
tests for mcptools refusal logic; fix timeout misreport"): before this
commit, `timeoutAndBudgetMiddleware` reported *any* context-ending
condition — a genuine 5-second deadline expiry, or the parent context being
canceled because the browser closed or the HTTP request was aborted
upstream — as `ErrToolCallTimeout`, worded "exceeded its 5s timeout." That's
a false claim about what happened in the cancellation case. The fix
distinguishes the two with `errors.Is`:

```go
switch {
case errors.Is(cctx.Err(), context.DeadlineExceeded):
    // the 5s bound was actually hit
    return errorResult(ToolError{Error: ErrToolCallTimeout, ...})
case errors.Is(cctx.Err(), context.Canceled):
    // the PARENT context was canceled — never reached the 5s bound
    return errorResult(ToolError{Error: ErrToolCallCanceled, ...})
}
```

`limits.go`'s own comment on `ErrToolCallCanceled` states why this
distinction matters here specifically: "reporting it as [a timeout] would be
a false claim about what happened, which this codebase's refuse-rather-than-
guess principle forbids just as much for an internal status report as for a
margin figure." `limits_test.go` (added in the same commit) has a dedicated
regression pair proving both directions — `TestTimeoutAndBudgetMiddleware_
DeadlineExceeded_ReportsTimeout` uses a deliberately short 20ms bound so a
genuine deadline is what fires; `TestTimeoutAndBudgetMiddleware_
ParentCanceled_DoesNotReportTimeout` uses a deliberately generous 5s bound
(so a flaky race with the deadline can't make the test pass for the wrong
reason) and asserts `ErrToolCallCanceled`, `require.NotEqual(t,
ErrToolCallTimeout, ...)`, and `require.NotContains(t, toolErr.Reason,
"timeout")`.

### No-open-SQL constraint

Enforced structurally, not by convention: every tool function in the
package takes `storage.Querier` — a `sqlc`-generated interface, not the
concrete `*storage.Queries` — and reaches Postgres only through
`internal/storage`'s hand-written adapters (`LoadDailyReconciliation`,
`LoadDailyReconciliationsInPeriod`, `LoadPromotionRoiRecordsByCampaign`,
etc.). Verified with a real grep for SQL keywords across the package:

```
$ grep -rniE "SELECT|INSERT INTO|UPDATE |DELETE FROM" backend/internal/mcptools/*.go
reconciliation_tools_test.go:50:  _, err := conn.Exec(context.Background(), "DELETE FROM daily_reconciliation WHERE date = $1", date)
reconciliation_tools_test.go:98:  err := conn.QueryRow(context.Background(), "SELECT margin::text FROM daily_reconciliation WHERE date = $1", sentinelDate).Scan(&gotMargin)
```

Two hits, both in `reconciliation_tools_test.go` and both `DATABASE_URL`-gated
integration-test scaffolding, not the tool logic itself: the original
cleanup step (deleting a sentinel row it wrote), plus `assertCanonicalDatasetFingerprint`
— the dataset-drift sentinel added 2026-08-30, which reads the hand-authored
sentinel day's margin once per live-Postgres connection and fails loudly if
it doesn't match the canonical value, catching a locally-drifted `data/live`
before it produces a confusing downstream test failure. Zero raw SQL anywhere in the eight tools, their
handlers, `campaign_match.go`, or `limits.go` — `period_tools.go` and
`day_of_month_pattern_tools.go` included,
confirmed by the same grep (it reaches Postgres only through
`storage.LoadDailyReconciliationsInPeriod`, the same adapter
`get_margin_delta` already uses).

### The recent testability fix: `fake_querier_test.go`

Before commit `abea19a`, every test in `reconciliation_tools_test.go`,
`platform_comparison_tools_test.go`, and `promo_tools_test.go` gated on
`DATABASE_URL` and skipped by default — per the new file's own comment,
"Finding 4: ... leaving this package's refuse-rather-than-guess logic
(`periodMargin`'s missing-day check, most importantly) completely dark in
a normal `go test ./...` run."

The fix is possible specifically **because** every tool function already
took `storage.Querier` (the interface) rather than `*storage.Queries` (the
concrete `sqlc` type) — the same design point `RegisterMCPServer`'s own doc
comment makes about itself. `fake_querier_test.go` defines a hand-written
`fakeQuerier` that embeds `storage.Querier` left `nil` (so any method this
package doesn't explicitly override panics rather than silently returning a
zero value — "a real bug... left to panic... rather than silently returning
a zero value that could mask that bug") and implements only the ~7 methods
the tool functions actually reach: `GetDailyReconciliationByDate`,
`ListDailyReconciliationsInPeriod`, `UpsertDailyReconciliation`,
`GetPromotionRoiByCampaign`, `GetPromotionRoiByPlatformAndPeriod`,
`ListNegativeRoiPromotions`, `ListDistinctCampaignIDs`,
`UpsertPromotionRoiRecord`. Seeding goes through the real production write
path (`storage.SaveDailyReconciliation` / `storage.SavePromotionRoiRecord`),
so a test's setup can't drift from how a real row is actually shaped — only
the SQL underneath is faked.

This added **20 new fake-backed tests** with zero Postgres dependency
(confirmed by `grep -c "^func Test" backend/internal/mcptools/*_fake_test.go`:
10 in `reconciliation_tools_fake_test.go`, 6 in `promo_tools_fake_test.go`,
4 in `platform_comparison_tools_fake_test.go`), plus the 2 in
`limits_test.go` above — 22 total, none of which ran in a default `go test
./...` before this commit. The specific refusal path the task calls out —
`get_margin_delta`'s missing-day check — is directly exercised by
`TestGetMarginDelta_InsufficientDataWhenPeriodHasMissingDay_Fake` and
`TestGetMarginDelta_InsufficientDataWhenPeriodIsEntirelyMissing_Fake`,
which previously only ran when a developer happened to have `DATABASE_URL`
set locally.

The same commit made a second, related fix: `RegisterMCPServer` was
changed to take `storage.Querier` instead of the concrete `*storage.Queries`
— "matching every function it calls" — which is what let a *server-level*
test (not just the individual core functions) run against the fake too.

### The MCP-specific skill: not used, checked honestly

`.claude/skills/` (project) and `~/.claude/skills/` (user) were both
checked for anything MCP-server-specific. There's a real plugin available —
`mcp-server-dev` at
`~/.claude/plugins/marketplaces/claude-plugins-official/plugins/mcp-server-dev`
— but a repo-wide grep for `mcp-server-dev` returns zero hits, in commit
messages, `docs/plan.md`, or `docs/tooling.md`. `docs/tooling.md`'s own
skills inventory (commit `8668758`, written the evening before
`9b5907a`'s "Add reconciliation MCP tools" — i.e. drafted with the MCP
layer's skill needs already in view, not backfilled after the fact)
doesn't list it either, in either its installed table or its explicit
"explicitly not installed" section. The commit history for the original six tools
(`9b5907a`, `d3d67e4`, `35fafdd`, `a51e968`, `abea19a`) reads as a
hand-built implementation
following `specs/001-margin-reconciliation-qa/contracts/mcp-tools.md`'s own
written contract through the SDD `plan → tasks → implement` flow (task IDs
T018–T020, T027, T028–T032 appear directly in those commit messages), not
scaffolding generated by a plugin. The seventh, `get_period_totals`
(`c7e4060`), shipped later and outside that original task list — no task ID
appears in its commit message — but its own file doc comment still opens
by naming itself "`contracts/mcp-tools.md`'s seventh entry," so the
contract-first discipline continued even without a numbered SDD task behind
it. The eighth, `get_expense_pattern_by_day_of_month`, shipped in a single
commit (`aadbb5d`, "add get_expense_pattern_by_day_of_month, the 8th
tool") that added both the Go tool and its `contracts/mcp-tools.md` entry
together — unlike the seventh's staggered history, so no ordering claim is
made either way for this one. Its own file doc comment does still open by
naming itself "`contracts/mcp-tools.md`'s eighth entry," consistent with
every other tool in this package. Reported honestly either way: no evidence
`mcp-server-dev` was used for any of the eight.

## 2. Claude Code skills actually used to build this

This is a filtered inventory, not the full install list — `docs/tooling.md`
already documents everything installed and why. Each entry below has a real,
cited artifact: a commit, a file, or a quoted doc comment. A skill available
in `~/.claude/skills/` with no such citation (`ux-writing` is the clearest
case — installed, listed in `docs/tooling.md`'s conceptual neighborhood, but
zero hits for it across every commit message and every doc/code comment in
this repo) is left out rather than described generically.

### `sdd` — the whole build process

The entire codebase was built through GitHub Spec Kit's `constitution →
specify → plan → tasks → analyze → implement` flow, not written and
retrofitted with specs after the fact:

- `c7f1e47` ratifies the constitution (v1.0.0); `532b99d` bumps it to
  v1.1.0 when the LLM vendor switched from OpenAI to Anthropic.
- `2bbc5ef` generates `tasks.md` — 39 tasks, 4 user stories — and fixes 2
  `speckit-analyze` findings before implementation starts (an
  instrumentation gap on the gate, a missing badge endpoint).
- `c6e39f2` — "Mark all 39 build-order tasks complete in tasks.md" — and a
  direct grep confirms it: `specs/001-margin-reconciliation-qa/tasks.md`
  has exactly 39 `- [x]` lines and 0 remaining `- [ ]` lines today.
- Four more specs followed the same `speckit-specify`/`plan`/`tasks`
  cadence after 001 shipped: `specs/002-badge-expansion`,
  `specs/003-platform-comparator`, `specs/004-semantic-cache` (all shipped,
  per `README.md`'s spec table), `specs/005-multi-tenant` (spec + RFC only,
  deliberately not built), and `specs/007-cost-sheet-upload` (shipped,
  `3e0ccfa`).
- Every project-local skill under `.claude/skills/` except
  `question-recovery-design` and `proactive-guidance-design` (both covered
  in their own subsections below) is one of the ten `speckit-*` commands
  (`speckit-constitution`, `-specify`, `-plan`, `-tasks`, `-implement`,
  `-clarify`, `-analyze`, `-checklist`, `-converge`, `-taskstoissues`),
  installed via `specify init --integration claude` per `docs/tooling.md`.

### `dataviz` — the chart/palette rules

Two commits name the skill directly and each pins a specific, checkable
rule to a specific file:

- `b1aff4b` ("Spec the routed shell + chart redesign... per the dataviz
  skill's form-then-color-then-validate procedure") ran the skill's real
  palette validator against the resolved `--success`/`--destructive` hex
  values and reports "a genuine CVD-separation FAIL for that pair in both
  themes (ΔE 5.4 light / 0.8 dark, floor 6.0)" — disclosed rather than
  shipped quietly, with non-color mitigations (zero baseline, text-labeled
  legend, signed direct labels) prescribed instead of new token values.
- `26e3ad5` ("Add diverging bar charts for margin trend and promotion ROI
  (dataviz)") implements exactly those mitigations by hand in SVG, and
  re-confirms the same CVD failure "matching redesign-spec.md §5" —
  consistent evidence across two commits, not a one-off claim.
- The rule enforced in code: `backend/internal/httpapi/visualization.go`
  defines `MinPieSlices = 3` and `MaxPieSlices = 6` with the comment "per
  the dataviz skill" directly above the mapping rationale — this is the
  actual mechanism behind `docs/plan.md`'s dated example (line 464) of the
  `get_daily_summary` pie exception: a day's revenue splitting across 3–6
  sources gets a pie, verified live against `2026-08-05`'s real
  iFood $64.50 / Just Eat Takeaway $71.25 / in-house POS $218.25 split,
  and a 2-slice case never renders one.

### `design-review`, `redesign`, `apply-aesthetic`, `design-component` — the frontend revamp

Commit `e137388` ("Frontend revamp: application UI over prose, real
accessibility fixes") is the real outcome, named directly in its own
message: "`design-review` scored the app 5.6/10 against Nielsen's
heuristics and found concrete, verified defects... a live axe-core run
found 6 serious violations," fixed by "applying linear-app's
layout/density/interaction patterns... via the newly-installed
design-review/redesign/apply-aesthetic/design-component skills." The
before/after is measured, not asserted: 6 serious axe violations to 0
across all 5 routes in both themes; `--muted-foreground` 4.46:1 → 4.88:1,
`--success-text` 4.49:1 → 6.40:1, `--warning-text` → 6.36:1 (this last pair
of numbers is the same contrast fix `docs/frontend.md`'s tokens section
documents from the `index.css` side). The commit also discloses the honest
cost, not just the win: hard-coded-value lint went from 61 to 75 flags
(offset by the new `--text-micro` token `docs/frontend.md` describes), and
one pre-existing ESLint error in `CompositionPieChart.tsx` was left
untouched rather than folded in and hidden. Screenshots at 1512×982 for
all 5 routes are checked into `docs/screenshots/{before,after}/`.

### `make-slide` — the presentation (22 slides at conversion, 24 today)

`docs/plan.md`'s original presentation spec (line 88) called for an "HTML,
landing-page style" format, explicitly "not slides/PPT," and named
`artifact-design` and `design` as the skills to check before building — no
generic deck look. That plan changed: commit `4be415b` ("Rebuild
presentation as a real slide deck using make-slide, update to current
state") names the actual pivot and the actual skill — "converted from the
landing-page/snap-scroll format to an actual slide deck (22 slides) using
the make-slide skill's real implementation as the foundation — its
data-focus theme, its navigation.js copied verbatim (arrow keys, Space,
Home/End, F fullscreen, S speaker notes, swipe, click-thirds)." `docs/
presentation.html`'s own CSS header comment confirms this in the checked-in
artifact itself: `/* make-slide · data-focus theme foundation, carrying My
Business Steward's brand tokens */`. The commit also records real,
independent verification rather than trusting the build: "pressed
ArrowRight/ArrowLeft/Home/End with Playwright and confirmed the slide
counter and rendered content changed correctly at each step." `guizang-
ppt-skill` and `frontend-slides` were both available (per the skills
listing) but neither is named anywhere in this history — `make-slide` is
the one actually used.

### `ux-writing` — rewriting the chat's refusal and error copy

Commit `768492f` ("Fix the floating composer overlapping the last chat
message") names the skill directly in its own message: the refusal,
clarification, and connection-error bubble titles in `ChatPanel.tsx` were
rewritten "into the Steward's first-person, forward-looking voice per the
ux-writing skill's error-copy formula." The real before/after, read
straight from the diff: `"Can't answer this one"` → `"I'll help you find
what you need"`, `"Needs a quick clarification"` → `"Let me make sure
I've got this right"`, and `"Couldn't reach the reconciliation engine"` →
`"I couldn't reach your data just now"` — the last one also fixed a stray
exposure of internal terminology ("reconciliation engine") to the owner.
The same commit applied the identical `"Couldn't reach the reconciliation
engine"` → `"I couldn't reach your data just now"` fix to `PointsCard.tsx`'s
matching error state, so the two places in the app that report the same
class of failure say it the same, kinder way. The trigger was a real user
report read from `question_interaction`'s own log — "the sense of the
answers is not kind" — not a speculative polish pass.

A later pass extended the same discipline from copy to color: the refusal
bubble and its avatar used `bg-destructive`/`text-destructive` — the same
red token a genuine destructive action (deleting a saved prompt) correctly
still uses — which reads as the product being upset rather than helping.
Recolored to the brand green (`bg-primary`/`text-primary`), swapped
`ShieldAlert` for `Compass`, and renamed the `ChatAvatar` tone prop from
`'destructive'` to `'refusal'` so the type no longer claims a color it
doesn't render. The narration prompts in `internal/explain` and
`internal/ambiguity`'s writer pass got the matching warmth rule — phrasing
and framing only, never softening what's actually missing.

### `skill-creator` and `question-recovery-design` — the skill this project created

Unlike every other skill in this section, `question-recovery-design`
wasn't consumed — it was **built**, by this project, as a deliverable in
its own right. Commit `732a9d1` ("Add project README and
question-recovery-design skill") states it directly: "The skill generalizes
this project's ambiguity-gate and refusal/clarification UX into a reusable
methodology, built with the skill-creator plugin and validated by its own
`quick_validate.py`."

`.claude/skills/question-recovery-design/SKILL.md` generalizes six concrete
moves this codebase actually implements — not abstract advice invented
for the skill:

1. **Classify before you answer, cheaply** — the ambiguity gate (Sonnet 5
   as of 2026-08-29, moved off Haiku 4.5 after a multi-year date-comparison
   bug; see `internal/llmclient/cost.go`) running before any tool-calling
   loop, on narrow input, fixed output shape.
2. **Never fabricate — refuse with a specific, honest reason** — the
   `no_data`/`insufficient_data`/`invalid_input` typed errors this
   document's Section 1 documents at the tool level.
3. **Ambiguous is not automatically a dead end** — the gate's forced choice
   between asking a clarifying question or stating an assumption and
   proceeding, never both, never neither.
4. **Every refusal or clarification carries a next step** — quick-reply
   options and the capability-list pattern in `ChatPanel.tsx`.
5. **Show capabilities proactively** — the same suggested-question
   component appearing on the empty state, not only after a failure.
6. **Log every refusal as a backlog signal** — the `question_interaction`
   instrumentation table, populated from the first real API call.

The skill's own "Worked example" section is explicit that it's a case
study, not a template: "adapt the *shape* of the pattern to your own
stack, not these exact files" — and it names two related skills
(`api-design`, `ux-writing`) and one boundary condition (skip it for a
fixed-schema form input, where `api-design` already covers validation
error handling) rather than presenting itself as universally applicable.
Commit `86f1499` ("Document the Sonnet writer pass and question-recovery
skill in architecture.html") is where this project's own architecture
diagram was updated to point at the skill by name, closing the loop between
the code and the methodology it was generalized from.

The skill later grew a new worked example from a real fix: `internal/
httpapi/capability.go`'s deterministic meta-question path. Before it
existed, "how can you help me?" reached the same Haiku classifier as every
data question, was correctly judged out-of-scope, and refused — technically
honest but a poor first experience. The fix pattern-matches a fixed set of
real phrasings and answers directly from the hand-written capability list,
*before* the classifier or any model call runs, at zero cost. Documented in
Move 5 of the skill (`## The five moves`) as an extension of "show
capabilities proactively," with its own checklist item and anti-pattern-
table row.

### `skill-creator` and `proactive-guidance-design` — the success-half companion skill

Like `question-recovery-design`, this skill wasn't consumed — it was
**built**, the same day, as this project's second self-authored
deliverable. Commit `8b87a39` ("Add proactive-guidance-design skill: the
success half of chat UX") states the relationship directly: "Companion to
question-recovery-design (the failure-recovery half): covers zero-state
capability transparency and deriving post-answer follow-up suggestions
deterministically from the real tool call that just ran, never a second
model call. Cross-referenced in both directions. Validated via
skill-creator's quick_validate.py." The cross-reference is real, not just
claimed: `.claude/skills/question-recovery-design/SKILL.md` line 255 names
`proactive-guidance-design` as "the mirror-image skill," and
`proactive-guidance-design/SKILL.md`'s own "Related skills" section names
`question-recovery-design` back.

`.claude/skills/proactive-guidance-design/SKILL.md` generalizes the
mirror-image half of the same underlying insight — `question-recovery-design`
covers what a chat surface shows when a question goes *wrong*; this one
covers what it shows *before* the first question and right *after* a
successful one — grounded in this same codebase, not abstract advice:

1. **Zero-state capability transparency** —
   `frontend/src/components/Chat/exampleQuestions.ts`'s `EXAMPLE_QUESTIONS`,
   each entry paired with the exact MCP tool name that answers it, and its
   own doc comment's rule that "suggesting a question the product cannot
   answer is the same class of lie as inventing a number."
2. **Grounded, deterministic follow-up suggestions** —
   `backend/internal/httpapi/suggestions.go`'s `deriveFollowUpSuggestions`,
   which takes the real `[]explain.ToolInvocation`, the asked question, and
   the real `dataStart`/`dataEnd` range, and picks a template by a fixed,
   narrowest-subject-wins tool priority mirroring `deriveVisualization`'s own
   convention — never a second model call asking "what should the owner ask
   next?", which the skill's own text calls "exactly the hallucination
   surface `exampleQuestions.ts` already forbids." The function's own doc
   comment confirms `get_period_totals` was folded into that same priority
   order the day it shipped: "inserted just above `get_daily_summary` — a
   period-level summary, but still the least specific 'just tell me what
   happened' shape among the seven tools."
3. **A capped, zoom-in/zoom-out shape** — `MaxFollowUpSuggestions = 3`
   (`suggestions.go`, citing Perplexity's and ChatGPT's follow-up row as
   precedent), with each tool's template pairing one narrower "zoom in"
   suggestion (e.g., after `get_daily_summary`, "Were there any
   discrepancies on {date}?") with one broader "zoom out" suggestion (a
   week-over-week comparison, only offered when the prior week actually
   exists within `[dataStart, dataEnd]` — `weekOverWeekAround` omits it
   entirely otherwise, never clamping it into a misleadingly short span).
4. **One shared presentation component, no bespoke styling** —
   `frontend/src/components/Chat/SuggestionChips.tsx`, already reused across
   `EmptyState`, `RefusalBubble`, and the composer's "Ideas" panel, every
   placement routing `onSelect` through the same `submitQuestion` function
   the composer itself calls. A follow-up chip, once it renders in the
   frontend, is documented as a fourth placement of the same component, not
   a new one.

The skill is explicit about its own current-state honesty, which is worth
quoting because it's a real, checkable gap rather than a hypothetical: its
own text states that "`AnswerBubble` in `ChatPanel.tsx` still ends after its
provenance tag and cache badge with no chip rendered from that field, so a
real, computed follow-up already exists on the wire and simply isn't
shown" — `AskResponse.SuggestedFollowUps` is populated by
`deriveFollowUpSuggestions` for every successfully answered interaction,
but as of this writing the frontend has not caught up to render it. The
skill names this precisely as "the gap that motivated this skill," not a
defect it claims to have already closed.

One disambiguation worth stating plainly, since this codebase now uses
"follow-up" for two unrelated mechanisms shipped the same day:
`deriveFollowUpSuggestions`' chips (documented above) are deterministic,
Go-computed *suggestions for the next question to ask*, rendered after an
answer lands. `ambiguity.ComposeAnswerFollowUp` (see
`backend/internal/ambiguity/gate.go`) is a different thing entirely — it
resolves what the user just typed (e.g. a bare "and the day before?")
against the immediately preceding real answer, so the gate can classify it
correctly instead of misfiring on an unclassifiable fragment. One is a
suggestion the user might click; the other is how the gate makes sense of
what the user already typed. Neither skill in this document owns that
second mechanism — it belongs to `internal/ambiguity`, not to a chat-surface
skill.

The skill's own worked-example section names two related skills
(`ux-writing`, for a follow-up suggestion's copy tone; `api-design`, for the
equivalent discipline at a pure API boundary with no chat surface to render
a chip into) and one boundary condition (skip it for a single-purpose form
with no "next question" concept), matching `question-recovery-design`'s own
pattern of naming adjacent skills and an explicit non-applicability case
rather than presenting itself as universally applicable.

### One pivotal decision: model tier per activity, not by default

Per this project's own standing discipline about matching model cost to
task difficulty, the `sdd` process itself is a good illustration in
miniature: `speckit-clarify`/`speckit-analyze` are narrow, mechanical
consistency checks (a good fit for a cheaper tier), while ratifying the
constitution (`speckit-constitution`, commit `c7f1e47`) and the
`skill-creator`-driven generalization of a real implementation into a
reusable methodology (`732a9d1`) are the kind of judgment call worth a
stronger model's attention — the same per-step reasoning this repo already
applies to Haiku-vs-Sonnet model selection in `CLAUDE.md` and
`internal/ambiguity`/`internal/explain`, just applied one level up, to the
build process rather than the product.

### Skills checked and deliberately left out

- **`artifact-design`** — real evidence it was *considered* (`docs/plan.md`
  line 93: "Before building: check `artifact-design` and `design` skills")
  for the presentation, but the presentation ultimately used `make-slide`
  instead (see above). `docs/architecture.html`, `docs/presentation.html`,
  and `docs/api.html` are genuine published Claude Artifacts (each carries
  a `PUBLISHED ARTIFACT` redeploy-instructions comment naming its own live
  URL), but no comment in any of the three attributes a specific design
  decision to `artifact-design` by name, so it isn't given its own cited
  subsection here.
- **`inspired-product`, `one-pager-prd`** — real evidence of *installation
  intent* tied to a specific purpose (`docs/tooling.md`: `one-pager-prd`
  for turning `product-strategy.md` into the one-page reasoning doc;
  `inspired-product` for Cagan's empowered-teams framework) and a `docs/
  plan.md` checklist item naming the diagnostic directly, but no commit or
  doc confirms either diagnostic was actually run to completion (the
  `inspired-product` checklist item in `docs/plan.md` is an unchecked
  `- [ ]`). Named here for completeness of the honest accounting, not
  included as a confirmed-applied skill.
- **`mcp-server-dev`** — see Section 1's dedicated finding: available as a
  plugin, zero evidence of use for `internal/mcptools`.

## How the two halves connect

Section 1 is a structural discipline enforced in *code*: a fixed interface
(`storage.Querier`), a single registration entry point
(`RegisterMCPServer`), middleware that can't be routed around without a
visible new code path, and a package doc comment stating outright that this
is "the ONLY path from the model layer" to persisted data. Section 2's `sdd`
skill is the same discipline applied to the *process* that produced that
code: a constitution ratified before implementation, specs and contracts
(`contracts/mcp-tools.md`) written before the tools existed, and a tasks
list checked off — 39/39, then four more specs — rather than a build
narrated after the fact. `question-recovery-design` is the point where the
two meet directly: a real refusal/clarification mechanism built to satisfy
the constitution's Principle II, then deliberately generalized back out into
a reusable skill via `skill-creator` — code discipline becoming process
discipline becoming a shareable artifact in its own right.
`proactive-guidance-design` is the same pattern's second instance, built
the same way and the same day: a real, already-landed mechanism
(`deriveFollowUpSuggestions`) generalized via `skill-creator` into a
reusable methodology, explicitly cross-referenced back to
`question-recovery-design` as its mirror-image half rather than treated as
an unrelated addition.
