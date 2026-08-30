# Changelog

All notable changes to My Business Steward (Restaurant Margin Copilot), in the
spirit of [Keep a Changelog](https://keepachangelog.com/), adapted for how this
project actually shipped: a single continuous take-home build across two real
dates, not versioned releases. Entries are grouped by date and then by
milestone, reverse-chronological. Every entry below is derived from this
repo's own `git log` (2026-08-27 through 2026-08-30) — nothing
here is invented, and honest bugs/regressions are named, not smoothed over,
matching this project's own stated documentation discipline (Constitution
Principle V: report what happened, including failures).

---

## 2026-08-30 — The evaluation harness had been grading its own cache

Every KR, accuracy, cost and points figure in the README, PRD and deck was
re-derived from the real database and real harness runs. Several moved
downward, and the reason is itself the finding. Full method, every failure
by name, and the reproducing SQL: `docs/product-strategy.md`, entry of the
same date.

- **Fixed** the measurement itself. The harness runs against its own backend
  on `:8092` so it never disturbs the running app, but that instance still
  shared the product's `answer_cache` table — so a re-run was served largely
  from the *previous* run's cached answers (**25 of 35 questions**, two
  suites finishing in **0s**). It was reporting a pass rate for the cache,
  not for the model path it exists to grade. `cmd/server` now takes
  **`-eval-no-answer-cache`**, wiring `POST /api/ask` with a nil `Cache` for
  that process only — a mode `httpapi.Deps` already supported. It never
  clears the shared cache.
- **Changed** the reported numbers accordingly, from two full uncached runs
  (both reported, not the better one, because the model layer is not
  deterministic): accuracy **14/15 and 14/15** (was 15/15), consistency
  **13/15 and 15/15** (was 15/15), refusal **5/5 and 4/5** (was 5/5), cost
  **$0.0313/question** over 70 questions and 142 model calls (was ~$0.025),
  against KR4's unchanged $0.05 bar.
- **Fixed** a real 502 the uncached runs exposed: "How was the weekend?" —
  the canonical ambiguity example in `CLAUDE.md` — failed **twice in four
  attempts** with `gate_failed`, because the ambiguity gate hit its
  1536-token output cap mid-clarification. An ambiguous verdict is the
  gate's true worst case: it must classify, then write both a clarifying
  question and its options in one budget. Cap raised to **2560**, the third
  such raise and, as with the previous two, made on measured evidence rather
  than precautionarily. Not recurred since.
- **Added** regression guards for the two key results that were true only by
  construction — nothing would have failed if the real data stopped
  satisfying them. `TestListNegativeRoiPromotions_RealDataset_FlagsLunchfixWithProvenance`
  (KR3) asserts the live database still yields a flagged negative-ROI
  campaign at the hand-computed −$450.75 with file+row provenance.
  `TestOpeningWindow_PersistedWithZeroSilentDataLoss` (KR2) reads the
  persisted opening window back and asserts all 14 days present exactly once
  plus one check per deliberate irregularity — the permanent form of the
  hand-run `psql` query that once caught 13 rows where there should have
  been 14.
- **Not fixed, and named instead.** Accuracy's **A15** failed in *every*
  uncached run: asked for delivery revenue net of the refund, the model
  returns gross $446.25 and the $62.25 refund with provenance and then
  declines to net them to $384.00, because no tool returns that figure and
  it will not present its own arithmetic as computed. That is Principle I
  working; the defect is a **gap in the tool contract**, left open. Two
  grading regexes that rejected correct answers (refusal R1's blanket
  "$0.00" guard, consistency C3's wording list) were also **deliberately
  left untuned** — editing a grader right after watching it fail makes the
  resulting number unreadable.
- **Corrected** a unit error the old figures rested on: a
  `question_interaction` row is one **model call**, and an answered question
  writes two, so "$7.3698 across 635 logged interactions" conflated calls
  with questions and understated a question's cost by roughly half. It also
  omitted the `business_insight_interaction` ledger. Current real totals:
  **$12.6855 across 1,006 model calls**.
- **Corrected** the live points figure everywhere, including a stale doc
  comment in `internal/badges` that quoted it: **12,345 points from 775
  badges** (458 Clean Close, 301 Discrepancy Catcher, 16 Growth), not the
  200 shown previously — that figure dated from when the database held 14
  days rather than 759. No badge logic changed.

## 2026-08-29 — Correction: date-range comparison was never the model's job

A review of the model-swap entry below surfaced an architectural problem the
swap had papered over, and this entry corrects the record rather than
quietly rewording it (Constitution Principle V).

- **Fixed** the actual root cause behind the "Haiku date-comparison bug"
  recorded below. Comparing an explicit, parseable date ("July 2026",
  "2026-08-05", a bare "2023") against the data's known min/max window is
  date **arithmetic** — Constitution Principle I work that belongs in Go,
  and the gate's own system prompt was instructing the model to "do the
  actual comparison explicitly and carefully". The three failed prompt
  fixes and the successful model swap were all attempts to make a model
  better at arithmetic that should never have been delegated to any model.
  A deterministic pre-check (`internal/ambiguity/daterange.go`) now parses
  explicit date forms in Go and: refuses a question whose explicit dates
  all fall outside the window **before any model call** (zero tokens, the
  refusal worded deterministically from the real facts, instrumented
  honestly as a no-model interaction); and, for explicit in-range dates,
  hands the model each reference's already-computed verdict as settled
  fact, so the model never re-derives range inclusion for a date Go could
  parse. Verified live: "What was our margin in July 2023?" refuses in
  ~9ms with zero tokens; "What was our margin for July 2026?" — the exact
  question that triggered the incident below — answers correctly.
- **Clarified** what the 2026-08-29 Sonnet swap actually bought: it stands,
  but only for what is genuinely a language job — relative/vague/year-less
  date resolution, date phrasings the conservative Go parser deliberately
  doesn't attempt, and the answerable/ambiguous/unanswerable judgment
  itself. The claim that the swap "fixed" the date-comparison bug is
  withdrawn: it made a model more often right at arithmetic the
  architecture says no model should perform. `internal/llmclient/cost.go`'s
  doc comment — the account every other doc points to — carries the same
  correction.

---

## 2026-08-29 — A critical, multi-year data-scale bug hunt

Extending the live dataset from the 14-day fixture to a 730-day synthetic
history (`backend/cmd/gendata`) surfaced real bugs the original build never
had reason to trigger. 5 commits.

- **Fixed** a genuine Claude Haiku 4.5 reasoning limit: the ambiguity gate
  repeatedly misclassified fully in-range, explicitly dated questions (e.g.
  "July 2026" inside a `2024-08-01..2026-08-14` window) as unanswerable once
  the data spanned multiple calendar years. Three prompt-only fixes were
  tried and verified NOT to resolve it; an A/B test swapping the same call
  to Claude Sonnet 5 fixed it on the first try. `ModelAmbiguityGate` moved
  to Sonnet 5 permanently — Claude Haiku 4.5 stays on the narrower
  paraphrase-match cache classifier (`ModelParaphraseMatch`, new constant),
  which showed no evidence of the same bug. Constitution amended to 1.2.0 to
  record the change. *(Corrected by the entry above: the underlying date
  comparison was arithmetic that should never have been a model's job; the
  deterministic pre-check is the real fix, and the swap stands only for the
  genuinely linguistic residual.)*
- **Fixed** a related, compounding bug in the same code path: the writer
  pass's output-token truncation wasn't detected before its (incomplete)
  JSON was parsed, letting garbled cut-off text leak into user-facing
  refusal reasons. Both `Classify` and `writeBetterText` now check
  `StopReason == MaxTokens` explicitly and treat any truncation as a hard
  failure, regardless of whether the cut-off text happens to parse; the
  writer's token cap was also raised 512→768 as a safety margin.
  `internal/livedata`'s test suite, previously untested against a
  real-sized live dataset, was hardened with a `withFreshDir` helper so its
  own tests can no longer operate destructively on this machine's real
  synthetic data.
- **Fixed** a critical data-integrity bug: `cmd/gendata`'s `startDate` was
  set one day after the fixture window ends, generating synthetic history
  through 2028-08-13 — over a year into the real-world future relative to
  the interview date. Moved `startDate` back two years so the generated
  history ends the day before the fixture begins, landing entirely in the
  past.
- **Fixed** Close/Promotions charts, which were built and tested against
  the ~14-day fixture and broke silently at 745 days of real data:
  fixed-pixel-per-bar SVG layouts either compressed into unreadable slivers
  or overflowed their containers. Both charts now compute a dynamic width
  and bucket/thin their labels based on the actual data volume; the Close
  page's per-day discrepancy badges collapse into a single "×N" summary
  that expands on click instead of repeating once per flagged day.
- **Added** a Help page (`/help`) with four content sections — what the app
  does, what you can ask it, how to use it, and why it sometimes refuses —
  reusing the chat's own example-question and capability-summary constants
  rather than retyping them.

---

## 2026-08-28 — Build day: engine to product, evaluated and fixed live

The overnight build (39 tasks across 4 user stories), a full evaluation-driven
fix cycle, three roadmap specs shipped same-day, a UX/accessibility pass, and
an evening polish session. ~106 commits.

### Deterministic core, brand, and the ask pipeline (continuing T011–T027)

- **Added** CSV ingestion (`internal/ingest`) for the delivery, POS, and
  supplier-cost exports, written test-first against the fixture set's four
  deliberate irregularities (duplicate order, cross-week refund, the missing
  2026-08-08 day, ISO-vs-`DD/MM/YYYY` date formats) — plus `internal/money`
  for exact fixed-point cents arithmetic after confirming a naive `float64`
  commission calc misrounds real fixture values (`34.50 * 23% = 7.935`).
- **Added** the deterministic reconciliation engine (`internal/reconcile`):
  duplicate collapsing, refund netting against `order_date`, missing-source
  handling that flags rather than zeroes, and commission recomputed from
  `subtotal * rate` and cross-checked against each file's own commission
  column. All golden values cross-verified against an independent
  from-scratch Python computation.
- **Added** persistence and the ingest→reconcile→persist CLI pipeline,
  proven against a live Postgres instance — the 14-day fixture period
  reconciles to a 482.05 total margin, matching the independent Python
  check exactly.
- **Added** the finalized brand (batwing café-door mark, prosperity-emerald
  primary color) and the Reconciliation-category badges (Clean Close,
  Discrepancy Catcher), plus provenance-citation and running-cost-panel
  components.
- **Added** the reconciliation chat UI (grounded answer / clarification /
  refusal as three visually distinct flows) and promotion-ROI reconciliation
  with its MCP tools (`get_promotion_roi`, `list_negative_roi_promotions`).
- **Added** the 3 core reconciliation MCP tools (`get_daily_summary`,
  `get_margin_delta`, `list_discrepancies`) behind a shared
  timeout-and-call-cap middleware; the Claude Haiku 4.5 ambiguity gate; the
  Sonnet 5 explanation step (a real tool-calling loop over the in-process MCP
  client); and `POST /api/ask`, wiring gate → explain → instrumentation on
  every branch.
- **Added** full HTTP integration (MCP server + ask handler + badges in one
  process). Verified live end-to-end against real Postgres and a real
  Anthropic key — cumulative spend after integration: **$0.0146**.
- **Fixed** a test-isolation bug where an integration test used a real
  fixture date as its own cleanup key and was deleting real pipeline data;
  retargeted to a sentinel date.
- **Logged** two real mistakes to the running build log: two agents
  concurrently editing `internal/mcptools` (resolved by conforming to the
  already-established convention rather than duplicating it), and
  `go test ./...` without `-v` silently hiding that every env-gated live
  test had been skipped (shell env vars weren't sourced by the
  non-interactive tool shell).

### Routed shell, charts, and the first evaluation run (T033–T039)

- **Added** the routed app shell (sidebar nav, `/`, `/close`, `/ask`,
  `/promotions`), the home page (capability tiles as navigation), and
  chart-first `/close`/`/promotions` pages with hand-rolled diverging bar
  charts — a real CVD-separation failure in the `--success`/`--destructive`
  color pair was caught by the dataviz palette validator (ΔE 5.4 light /
  0.8 dark, floor 6.0) and mitigated with non-color encoding (zero baseline,
  text-labeled legend, signed labels) rather than silently shipped.
- **Added** the promptfoo evaluation harness (35 real questions: accuracy,
  consistency, refusal) and ran it live against the backend with real
  Anthropic calls. First real numbers: **10/15 accuracy, 0/5 consistency
  sets fully agreed, 4/5 correct refusals**, cost **$0.286** for the run.
  Root cause of nearly every failure: the model didn't reliably infer 2026
  when a question omitted the year — sometimes hallucinating "2024" with
  full confidence.
- **Fixed** date-year grounding: gate and explain now receive the real
  min/max date range from the database (not a hardcoded literal) and an
  explicit instruction that a year-less date must resolve into the one year
  the data spans. Regression test run live 3/3 against Haiku 4.5.
- **Fixed** campaign lookup: `get_promotion_roi` now falls back to a
  bounded, typed fuzzy match (`matchCampaignID`) against the real persisted
  campaign-id set, so a shortened name ("LUNCHFIX") or full display name
  resolves instead of triggering a hallucinated refusal. Also fixed the
  ambiguity gate itself, which was independently refusing shortened
  campaign references before the question ever reached the tool layer.
- **Re-ran** the identical 35-question suite after both fixes: refusal
  4/5→5/5, the year-hallucination pattern fully eliminated, but accuracy
  moved 10/15→9/15 as three previously-passing questions surfaced an
  unrelated quirk (stating two platforms' figures without a combined
  total) — reported plainly rather than netted against the wins.
- **Fixed** the delivery-revenue combined-total regression by adding a real
  deterministic `total_delivery_gross_sales` field (accuracy back to
  10/15), and discovered — while verifying "both servers are live" —
  that the frontend was **still calling an in-memory mock**, not the real
  backend, and the backend had no CORS headers at all. Both fixed: dev CORS
  allowlist, `AskPage` rewired to `POST /api/ask` for real, with real
  per-call cost data replacing hardcoded placeholder figures.
- **Removed** the orphaned `ReportPage` and the retired single-page
  `MarginCopilotApp` once all three routed pages had real content.
- All 39 build-order tasks marked complete after verifying each phase's
  real artifacts, not as a batch check-off.

### Chat UX overhaul, live data pages, and the answer cache

- **Added**, in one large verified pass: dynamic in-chat visualization
  (chart type chosen deterministically in Go from tool name/shape, never a
  second model call); gamification points derived at read time from earned
  badges; real `GET /api/reconciliation` and `GET /api/promotions` endpoints
  backing the Close/Promotions pages with live data instead of hardcoded
  JSX; and an exact-match LLM answer cache, kept in a separate ledger table
  so a cache hit is never conflated with a real paid interaction.
- **Fixed** a real chat scroll bug (missing `min-h-0` on a flex Radix
  `ScrollArea`) and a concurrency bug (a single shared `pgx.Conn` across
  concurrent handlers, fixed by switching to `pgxpool`).
- **Fixed**, in a second UX pass: conversation memory (a clarification
  reply like "yes" was being refused because nothing carried the original
  question's context — fixed with a typed `PendingClarification` field);
  the scroll bug for real this time (the panel was a fixed 576px letterbox
  in a 982px viewport, screenshotted and measured, not just diffed); and
  answers rendering raw markdown asterisks (`**$328.82**`) instead of
  formatted text.
- **Added** real, tool-grounded capability guidance on empty/refusal states,
  a `/points` page, and localStorage conversation persistence with a
  capped recent-threads list.

### Design system rebuild and an accessibility pass

- **Rebuilt** `docs/architecture.html` as a 3-tab design system reference
  (design tokens, reconciliation engine, full architecture) after finding a
  real SVG layout bug (two connector lines routed through the same
  coordinates as later-added text, striking through labels). Also caught
  and fixed a stale doc claiming the abandoned iFood-red as the primary
  color, months after the brand had pivoted to prosperity-emerald.
- **Scored** the app 5.6/10 against Nielsen's heuristics and found concrete
  defects: four deterministic figures compressed into one 12px caption on
  `/close`, a ~250-word points essay pushing all navigation below the fold
  on Home, and 6 serious axe-core accessibility violations. **Fixed**: KPI
  rails replace the compressed captions; the points essay moved to its own
  page; axe violations went **6 → 0** across all 5 routes in both themes;
  contrast fixed at the token level (`--muted-foreground` 4.46:1→4.88:1,
  `--success-text` 4.49:1→6.40:1). Honestly disclosed: hard-coded-value
  lint went 61→75 (offset by a new `--text-micro` token, a net accessibility
  gain not a clean number).
- **Added** a dedicated Points summary section on Home, extracting the
  composition bar into a shared component so Home and `/points` render one
  implementation instead of two that could drift.

### Four roadmap items scoped and three shipped same day

- **Specified** badge expansion, the platform comparator, a paraphrase-aware
  answer cache, and multi-tenancy — each re-scoped from the original
  "not built" roadmap notes to what's honestly buildable. Multi-tenant was
  deliberately kept spec/RFC-only: a tenant-isolation defect is a
  data-breach class of bug, gated on explicit review rather than built same
  day.
- **Implemented** badge expansion (spec 002): Growth badges (real positive
  attributable ROI), Engagement badges (a genuinely new, DB-deduped
  `usage_event` table — verified live to show zero badges on a fresh table,
  never a fabricated streak), and Campaign-Creation badges (a real
  `POST /api/promotions` action logging a promotion that replaces a flagged
  negative-ROI one, server-side re-verified rather than trusting the
  client).
- **Implemented** the cross-platform economics comparator (spec 003):
  discovered mid-build that per-platform commission wasn't actually
  recoverable from what was persisted (one combined total), so added a
  migration and backfilled real per-order data rather than approximating
  with nominal rates — revealing iFood's **true effective rate is 22.06%**,
  not the nominal 23%, because of a real fixture refund netting against its
  own reversal. New `compare_platform_economics` MCP tool (6th typed tool)
  and `/platforms` page.
- **Implemented** the paraphrase-aware answer cache (spec 004): a bounded
  Haiku classification call against up to 20 recent cached questions (no
  embeddings — Anthropic has no first-party embeddings API, a documented
  vendor-constraint decision), double-verified against the live cache
  before ever serving a match, with real and avoided cost kept as two
  distinct, un-netted numbers.
- **Fixed** a Ports & Adapters diagram overlap and viewport-overflow bug in
  the presentation, and updated `architecture.html` to reflect all three
  newly-shipped features (badges, comparator, paraphrase cache) after it
  had gone stale mid-session.
- **Rebuilt** the presentation as a real 22-slide deck (`make-slide`
  skill), replacing the earlier landing-page format, with content updated
  to current shipped reality (live badge/point values, live comparator
  figures).

### Hardening, cost-accounting fixes, and documentation completeness

- **Added** cost-sheet upload (spec 007): preview/commit/template endpoints
  reusing the existing tolerant parser, writing to a fixed filename inside a
  git-ignored directory (never the client's uploaded filename, making path
  traversal structurally impossible), clearing the answer cache before
  re-ingesting.
- **Added** the project README, an OpenAPI spec + interactive Swagger docs
  for all 10 real routes (grounded against live curl responses, not
  inferred from handler code), MCP/skills documentation citing real commits
  and file paths, and a day/period picker on the Close page.
- **Added** a conditional Sonnet 5 writer pass that rewrites
  clarifying-question/refusal text (Haiku still owns classification alone
  and cannot be overridden).
- **Fixed**, in a self-review pass: `internal/money` had zero direct test
  coverage — added tests and caught a real bug (`ParseFixedPoint` silently
  accepted a trailing-dot value like `"34."` as zero-fraction instead of
  rejecting it); date-format resolution (MM/DD vs DD/MM) now locks per
  file instead of per row; the answer cache gained a schema-version column
  so a stale shape is treated as a miss, not served.
- **Fixed** `explain`'s turn-level error path discarding already-billed
  spend from earlier turns in the loop (a real API cost was going
  unrecorded); added a guard against narrating a currency-shaped answer
  that made zero tool calls and collected zero provenance.
- **Fixed** a timeout-misreporting bug: a canceled parent context (client
  disconnect) was being reported identically to a genuine 5s
  deadline-exceeded timeout. Added fake-`Querier` tests so MCP tool
  refusal paths run in default `go test ./...`, not only when
  `DATABASE_URL` is set.
- **Required** non-empty provenance on every "answered" assertion in the
  accuracy/consistency eval configs — previously a right number reached via
  a wrong or absent citation trail still passed.
- **Fixed** the floating chat composer overlapping the last message (the
  reserved padding was a fixed value; the composer's real height is
  dynamic) and rewrote refusal/error copy into the Steward's first-person
  voice per the ux-writing skill's error-copy formula.
- **Fixed** a truncated-response bug caught live: a "show me the day with
  the most profit" question was served as an unfinished planning sentence
  cut off by the output-token cap. Any `MaxTokens` stop with zero tool
  calls is now refused unconditionally.
- **Replaced** the deck's one U.S.-specific statistic with real, sourced
  Brazil/UK data matching the platforms actually used elsewhere in the deck.

### PWA, CORS, and launch polish

- **Added** an installable PWA (real manifest + service worker, precaching
  only hashed build assets — never `/api/*`, so every figure still comes
  from a live request) and then fixed it not appearing in dev mode
  (`vite-plugin-pwa` disables itself under `vite dev` by default).
- **Fixed** dev CORS being hard-coded to Vite's port only, which broke the
  installed PWA build (a different port) with a real browser CORS error —
  now reflects back any `localhost`/`127.0.0.1` origin.
- **Added**, then **fixed**, the app-open animation: the batwing-door swing
  first played on the small sidebar logo (correct but imperceptible at
  36–40px — root-caused live after a user report ruled out caching),
  replaced with a full-viewport launch splash at the presentation's own
  140px scale. The same door-swing now loops on the chat's assistant avatar
  while a real model call is in flight, and goes static the instant a real
  answer arrives.
- **Fixed** the composer overlap bug a second time: the existing
  `ResizeObserver` used the default `content-box`, which by definition
  excludes padding, so it never fired when only `padding-bottom` changed
  (e.g. opening the suggestions panel). Verified before/after: +231px
  overlap → clean −24px gap.
- **Added** a real fullscreen toggle button (the Fullscreen API requires a
  user gesture in every major browser, so no auto-fullscreen is possible)
  and shrank the composer's backdrop gradient, which was fading real
  conversation content that had nothing to do with the composer.

### Deterministic follow-ups, the 7th MCP tool, and an evaluative-language fix

- **Added** `compare_platform_economics` to the chat's own example-question
  list (it had shipped without ever being surfaced to the user) and
  `get_period_totals` — the 7th typed MCP tool — closing a real observed
  failure where "which day had the most profit" burned through the model's
  turn budget calling `get_daily_summary` once per day.
- **Fixed** A10 (a false clarify on evaluative language like
  "underperforming"), grounded in two published LLM-behavior papers plus
  this project's own observed turn-budget blowup: the gate now classifies
  evaluative-sounding questions as answerable when a typed tool already
  defines the term, rather than asking the user to define a threshold the
  product could already compute. Real before/after on the identical
  35-question suite: **accuracy 11/15 → 13/15, refusal 4/5 → 5/5**.
- **Added** deterministic follow-up chips after every answered question —
  generated purely in Go from the real tool call that grounded the answer,
  never a second model call — and a typed one-hop follow-up mechanism for
  answers (not just clarifications), so "and the day before?" resolves
  correctly without an accumulating transcript.
- **Fixed** part of A15: refunds weren't being attributed to their real
  source platform even though the data existed at the point they were
  accumulated — added `refunds_by_source`, mirroring the earlier
  per-platform-commission fix.
- **Added** a missing chart-type case for `get_period_totals` (one of the
  richer tool results was silently rendering no chart at all), and updated
  the MCP/skills reference, technical RFC, `architecture.html`, the
  presentation (real 13/15 eval delta, follow-up-chip demo slide), and the
  OpenAPI spec/`frontend.md` for everything shipped in this pass.

### Chat warmth, presentation restructuring, points-as-payment, and Settings

- **Added** a chat warmth pass: the refusal/error bubble recolored from red
  to brand green (a refusal isn't a system failure), warmer narration
  copy, and a deterministic zero-cost path for meta-questions like "how can
  you help me?" answered from a hand-written capability list before either
  model call runs.
- **Audited** the presentation for staleness (two different superseded
  spend figures in the same deck, "6 typed tools" after the 7th had
  shipped) and added a chat-warmth slide plus a cost-management roadmap
  slide drafted by Claude Fable 5 — reserved for this one high-stakes,
  interviewer-facing judgment call per this project's own model-tier
  discipline.
- **Restructured** the deck after a real audience dry run: consolidated the
  OKR (previously split across three slides) onto one slide with all four
  KRs together, separated KR1's committed target from disclosed context
  instead of leading with its best sub-metric, added real sourced evidence
  to every Hypotheses claim, unified the roadmap, and removed a
  self-imposed "$5 spend cap" framing that doesn't exist in code.
- **Fixed** truncated/overlapping labels in the architecture request-flow
  diagram (label text wider than its gap, clipped by a neighboring box).
- **Added** points-as-payment: `POST /api/promotions` now accepts
  `payment_method: "points"`, verified server-side against the real
  earned-minus-spent balance at a fixed, disclosed 10 cents/point rate,
  refusing on insufficient balance rather than trusting the client.
- **Fixed** the promotion ROI chart conflating two different facts as one
  visual: a freshly-logged, not-yet-checked promotion was rendering with
  the same "refused" dashed bar as a permanently-refused campaign. Now
  renders with no bar (but still listed in the table) until an attribution
  attempt actually happens.
- **Added** a real Settings page — only the one real display preference
  (fullscreen), an "About this build" panel linking real hosted docs, and a
  "Not built yet" panel naming only capabilities already documented as
  absent elsewhere (auth, tenant isolation, rate limiting). Verified no
  dark-mode/theme wiring exists anywhere in the frontend before deciding
  not to add a toggle for it.
- **Added** a 2-year (730-day) synthetic operating-history generator
  (`cmd/gendata`) into the live-exploration dataset (kept fully separate
  from the eval harness's small fixture set): a logistic growth curve from
  $14,000/mo to $33,500/mo gross revenue, ingested live with zero errors,
  25 promotion campaigns with a real positive/negative ROI mix.

---

## 2026-08-27 — Planning day: spec-driven strategy, constitution, and first build tasks

The strategy/spec-kit foundation and the earliest implementation tasks
(T001–T010) before the deterministic engine existed. ~35 commits.

### Strategy, constitution, and spec-kit setup

- **Added** the initial project scaffold, an architecture presentation
  outlining the deterministic/probabilistic split, a toolchain install
  script, and the GitHub Spec Kit (SDD) framework with Claude Code
  integration.
- **Added** `docs/product-strategy.md` and ratified **Constitution v1.0.0**
  (deterministic/probabilistic split, refusal discipline, typed tools,
  provenance, test-first, instrumentation) — later revised to **v1.1.0**
  when the LLM vendor switched from OpenAI to Anthropic (Claude Haiku 4.5
  for the ambiguity gate, Sonnet 5 for explanation).
- **Added** the day-by-day plan through the interview date, with a running
  mistakes log, and made product strategy visible in the plan via a recap
  section and skill-driven checkpoints.

### OKR framing and the 001 spec

- **Reframed** the objective as an OKR (grow profitability via revenue
  without eroding margin) and defined 4 Key Results (trust, margin
  protection, revenue growth, token discipline).
- **Recorded** a structured comparison of five candidate product concepts
  and the decision to build the combined reconciliation + promo-ROI
  product ("Product A") rather than either half alone.
- **Added** feature spec `001-margin-reconciliation-qa`, expanded with the
  promo-ROI user story (KR3), followed by Phase 0/1 plan artifacts
  (research, data model, MCP contracts, quickstart), an AI-by-Design
  justification for where the model is and isn't used, market sizing and
  launch strategy (Prosus vs. non-Prosus segments), a module architecture
  diagram, the badge system design, `tasks.md` (39 tasks across 4 user
  stories), and PRD/DOR/Technical RFC documents for the eventual
  presentation.

### First build tasks (T001–T010)

- **Added** the Go backend module (core dependencies: `mcp-go`, `pgx/v5`,
  `testify`, `anthropic-sdk-go`), the React/Vite/Tailwind/shadcn frontend
  scaffold, `docker-compose` for local Postgres, the `golang-migrate`
  schema for the three core tables (with DB-level `CHECK` constraints as a
  second gate on top of the Go layer), `sqlc`-generated typed Postgres
  access, and the shared Anthropic API client wrapper with deterministic
  cost estimation (Haiku 4.5 $1/$5, Sonnet 5 $2/$10 per MTok).
- **Fixed** a real bug hit while scaffolding: the shadcn CLI didn't resolve
  the `@/*` alias (declared only in `tsconfig.app.json`), writing a
  literal `./@/components/ui/button.tsx` directory instead of the intended
  path.
- **Generated** two weeks of fixture data (delivery, POS, cost-sheet,
  promotion exports) with five deliberate irregularities baked in on
  purpose: a duplicate order, a refund crossing into the next week, a
  fully missing delivery-platform day, an inconsistent date format between
  files, and one promotion with no attributable orders at all.
- **Added** the instrumentation writer (`internal/instrumentation`),
  written test-first against a package that didn't exist yet, confirmed
  red before being made green.
- **Logged** real friction from the overnight build's Setup+Foundational
  phase and documented the netting-convention decision plus cost-bounded
  live test scenarios, resolving the day's open DOR blockers.

---

*Compiled from `git log` on 2026-08-29. For the fuller, evaluation-grade
narrative behind any entry above — including exact before/after numbers,
root-cause traces, and what was deliberately not built — see
`docs/product-strategy.md` and `docs/plan.md`.*
