# Feature Specification: Dashboard & Chat Intelligence v2

**Feature Branch**: `008-dashboard-chat-intelligence-v2`

**Created**: 2026-08-29

**Status**: Draft

**Input**: User description: "A batch of 12 high-impact enhancements across the Chat, Home, Promotions, and Platforms surfaces, all derived from real, already-computed data — no new open-ended computation, no new model calls except where explicitly noted. Selected as the P1 tier from a larger 31-idea brainstorm, using this project's own established Double Diamond scoring discipline (impact x feasibility x architectural fit). The P2/P3 remainder is named explicitly as deferred roadmap, not silently dropped."

## Background

After the original take-home build (specs 001-007) shipped, the owner asked what would make the chat and dashboard surfaces more useful now that the product has real operating history behind it — a hand-authored 14-day window plus, as of this week, a 2-year synthetic history (`backend/cmd/gendata`) that finally makes period-over-period and year-over-year comparisons meaningful. Thirty-one candidate features were brainstormed across five surfaces (Chat, Home, Promotions, Points, Platforms). Rather than build all 31 at even quality, this spec applies the same scoring discipline the original Double Diamond process used to pick Product A over four other candidates (`docs/product-strategy.md`'s "Five products scored" — impact, feasibility-by-deadline, and architectural fit): every item here scored highest on all three axes. The other 19 are named explicitly in Assumptions as deferred, not silently dropped.

Every requirement in this spec is a **deterministic derivation from data already computed by an existing MCP tool or already persisted** (Constitution Principle I) — none add a second model call, and none add a new open-ended computation path.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Proactive guidance in the chat, in the moment (Priority: P1)

An owner reading a chat answer gets help going deeper — clicking directly into a chart's data point, seeing why an unusual day happened, and seeing exactly what data backed the answer — without having to compose a new question from scratch or take the answer on faith.

**Why this priority**: This is the direct continuation of the follow-up-suggestion mechanism already shipped (`deriveFollowUpSuggestions`) — it extends a pattern already proven, into new trigger points (chart clicks, flags) and a new trust surface (showing the tool call itself).

**Independent Test**: Ask a period-totals question, confirm the rendered chart's worst-day bar is clickable and submits a real, self-contained follow-up question about that date; ask a question whose underlying day carries a real discrepancy flag, confirm a "why is this different from usual?" chip appears; expand "show your work" under any answered question and confirm it shows the real tool name and JSON result already returned in that response.

**Acceptance Scenarios**:

1. **Given** a rendered bar/pie chart backed by a tool result with per-day or per-entity data, **When** the owner clicks a specific bar/segment, **Then** a new, real, self-contained question about that exact date/entity is submitted automatically (e.g. "What happened on 2028-08-05?").
2. **Given** an answer whose grounding day(s) carry at least one `discrepancy_flags` entry, **When** the answer renders, **Then** a follow-up chip offering to explain the flag is included among the suggested follow-ups (capped at the existing 3-suggestion limit — this adds a new suggestion source, it does not raise the cap).
3. **Given** any answered question, **When** the owner expands "show your work", **Then** the exact tool name(s) called and the raw JSON result(s) already present in that response's data are displayed, formatted, never re-fetched or re-computed.
4. **Given** a question with no discrepancy flags on its grounding day(s), **When** the answer renders, **Then** no flag-based follow-up chip appears (no fabricated flag).

---

### User Story 2 - Comparisons finally worth having (Priority: P1)

An owner asking about a period can jump straight to "how does this compare" without re-typing a full comparison question, and sees the same comparison reflected passively on the Home and Platforms pages — all newly meaningful now that 2 years of real history exist.

**Why this priority**: Directly unlocked by this week's 2-year dataset — this was low-value with only 14 days of history and is now one of the highest-value additions possible, reusing the existing `get_margin_delta` and `compare_platform_economics` tools unchanged.

**Independent Test**: Ask a period-totals question, confirm a "Compare to last period" action derives the correct equivalent prior period and returns a real `get_margin_delta` result; load Home, confirm the year-over-year tile shows a real, correctly-dated prior-year figure only when a full prior-year period exists in the data; load Platforms, confirm the effective-rate trend reflects real values across the full available date range.

**Acceptance Scenarios**:

1. **Given** an answered period-totals or daily-summary question, **When** the owner taps "Compare to last period", **Then** the system derives the immediately preceding period of the same length and re-asks a self-contained comparison question through the normal `/api/ask` path (no new endpoint, no bypass of the gate).
2. **Given** the derived prior period falls partially or fully outside the data's real min/max date range, **When** the comparison is requested, **Then** the system states plainly that the prior period is out of range rather than computing a partial comparison.
3. **Given** at least 12 months of continuous data ending on the same calendar month one year apart, **When** Home loads, **Then** the year-over-year tile shows both figures and their real delta; otherwise the tile is omitted entirely, never shown with a fabricated or partial value.
4. **Given** the full available date range, **When** the Platforms page loads, **Then** the effective-rate trend chart plots real values only for periods where `compare_platform_economics` would not return `insufficient_data`.

---

### User Story 3 - Steward-style proactive insight, without being asked (Priority: P1)

An owner opening Home or Promotions sees the product volunteer what's worth knowing — whether yesterday was better or worse than usual, which day this week deserves a look, and which underperforming campaign still needs a decision — the same "steward, not a report generator" persona already established in the chat.

**Why this priority**: Directly extends the proactive-guidance-design skill's own worked example (already-shipped chat follow-up chips) onto the two pages an owner is most likely to open first, closing a real gap: today, this insight only surfaces if the owner thinks to ask for it.

**Independent Test**: Load Home with at least 2 days of reconciled history, confirm the "Latest margin" stat shows a real trend arrow computed from `get_margin_delta` against yesterday or the same weekday last week; confirm the "biggest win / catch this week" card shows the real `best_day`/`worst_day` from `get_period_totals` for the trailing 7 days; load Promotions with a negative-ROI campaign that has never been referenced as a `replaces_campaign_id`, confirm a "needs action" indicator appears on it.

**Acceptance Scenarios**:

1. **Given** at least one prior reconciled day exists, **When** Home loads, **Then** the Latest Margin stat shows a directional indicator (up/down/flat) against a real comparison point, computed by `get_margin_delta`, never a model guess.
2. **Given** fewer than 7 reconciled days exist in the trailing week, **When** Home loads, **Then** the "biggest win/catch" card either scopes itself honestly to the days that do exist or is omitted — never padded with a day that has no data.
3. **Given** a campaign with `flagged_negative: true` and no other campaign's `replaces_campaign_id` pointing at it, **When** Promotions loads, **Then** it is visually marked as needing action; a campaign already referenced by a replacement is not marked.

---

### User Story 4 - Everyday usability polish on Promotions and Points (Priority: P1)

An owner managing several campaigns can see which platform is paying off in aggregate and sort the list by what matters, and can see a real record of every points redemption they've made.

**Why this priority**: The lowest-risk, highest-certainty items in this batch — pure presentation of data the backend already returns in full, with the one net-new piece (redemption history) a direct, obvious extension of the points-payment feature shipped this week.

**Independent Test**: Load Promotions with campaigns on both platforms, confirm an aggregate ROI-by-platform stat is correct against manual sums of the visible rows; sort by ROI ascending/descending and confirm order; load Points after at least one points-paid promotion exists, confirm it appears in a real redemption history list with the correct campaign, date, and points amount.

**Acceptance Scenarios**:

1. **Given** campaigns exist on both iFood and Just Eat Takeaway with known ROI values, **When** Promotions loads, **Then** the aggregate-by-platform stat equals the real sum of each platform's attributed ROI (excluding unattributable/not-yet-attributed campaigns from the sum, never treating them as zero).
2. **Given** the campaign list, **When** the owner chooses to sort by ROI, **Then** the list reorders correctly, with unattributable/not-yet-attributed campaigns sorted consistently to one end rather than interleaved by a fabricated value.
3. **Given** one or more promotions were logged with `payment_method: "points"`, **When** Points loads, **Then** each appears in a redemption history list with its real campaign id, date, and points amount, newest first.
4. **Given** no points-paid promotions exist yet, **When** Points loads, **Then** the redemption history section states so plainly rather than rendering an empty table with no explanation.

### Edge Cases

- What happens when a chart-click follow-up targets a date the ambiguity gate would classify as out-of-range (e.g. a stale cached chart rendered before an ingestion changed the data window)? → Resolved the same way any other question is: through the normal gate, which refuses or clarifies exactly as it would for a typed question.
- How does the "compare to last period" derivation handle a period that itself was ambiguous or assumption-based? → It re-derives from the ANSWERED question's actual resolved dates (already present in the response), never from the original raw question text.
- What happens to the Home year-over-year tile and the "biggest win/catch" card during the very first days of a fresh install, before enough history exists? → Both must degrade to omission, per FR-013, never a fabricated or zero-padded figure.
- What happens if two campaigns are tied for best/worst ROI within a platform aggregate? → No new tie-break is needed; the aggregate is a sum, not a ranking, so ties don't apply to FR-009 the way they do to `get_period_totals`'s existing best/worst-day tie-break.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST let an owner click a data point on a rendered chart (bar or pie, wherever the underlying tool result carries a per-day or per-entity date/identifier) and have it submit a new, real, self-contained question about that exact point through the existing `/api/ask` path.
- **FR-002**: The system MUST derive a "why is this different from usual?" follow-up suggestion, in Go, whenever an answered question's grounding day(s) carry at least one real `discrepancy_flags` entry — counted toward the existing 3-suggestion cap, not in addition to it.
- **FR-003**: The system MUST let an owner reveal, per answered question, the exact MCP tool name(s) invoked and the raw JSON result(s) already contained in that response — no additional tool call, no re-computation.
- **FR-004**: The system MUST let an owner request a "compare to last period" action on any answered period-totals or daily-summary question, which derives the immediately preceding period of equal length and re-asks a self-contained comparison question through the normal ambiguity-gate path.
- **FR-005**: The system MUST refuse (state plainly, never silently truncate) a "compare to last period" request whose derived prior period falls outside the real data date range.
- **FR-006**: The Home page MUST show a year-over-year comparison only when a full prior-year period of equal length exists in the data; otherwise it MUST be omitted entirely.
- **FR-007**: The Platforms page MUST show commission effective-rate as a trend across the real available date range, using only periods `compare_platform_economics` would not refuse.
- **FR-008**: The Home page MUST show a directional trend indicator on the Latest Margin stat, computed via `get_margin_delta` against a real, named comparison point (yesterday, or the same weekday one week prior) — never against a placeholder.
- **FR-009**: The Home page MUST show the real best/worst day of the trailing 7 reconciled days (via `get_period_totals`), scoped honestly to however many of those days actually exist.
- **FR-010**: The Promotions page MUST visually mark a `flagged_negative: true` campaign as needing action when no other campaign's `replaces_campaign_id` references it.
- **FR-011**: The Promotions page MUST show an aggregate ROI figure per platform, computed as the real sum of that platform's attributed campaigns' ROI, excluding (never zero-substituting) unattributable or not-yet-attributed campaigns.
- **FR-012**: The Promotions page MUST let an owner sort the campaign list by ROI, ascending or descending, with unattributable/not-yet-attributed campaigns sorted consistently to one end.
- **FR-013**: Every new proactive UI element introduced by this spec (trend arrow, biggest-win/catch card, year-over-year tile, effective-rate trend) MUST degrade to omission — never a zero, a placeholder, or a fabricated value — when the data it needs does not yet exist.
- **FR-014**: The Points page MUST show a real redemption history — every `payment_method: "points"` promotion, its campaign id, date, and points amount — newest first, with an honest empty state when none exist yet.

### Key Entities

- **Discrepancy-derived follow-up**: not a new persisted entity — a new deterministic Go derivation (alongside `deriveFollowUpSuggestions`) reading `DailyReconciliation.discrepancy_flags` already returned by `get_daily_summary`/`get_period_totals`.
- **Chart click target**: not persisted — a client-side mapping from a rendered chart mark back to the real date/identifier already present in that chart's own source tool result.
- **Comparison period**: not persisted — computed in Go from an already-resolved question's real start/end dates, the same way `ComposeFollowUp` already composes follow-up context deterministically.
- **Redemption history entry**: not a new entity — a read-time projection of `promotion_roi_record` rows where `payment_method = 'points'`, already persisted by the points-payment feature.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An owner can go from a chart on screen to a real, grounded answer about one specific data point in one click, with no retyping.
- **SC-002**: A period-over-period or year-over-year comparison that required manually computing and typing two exact date ranges before this feature now takes one tap.
- **SC-003**: 100% of new proactive UI elements (trend arrow, biggest-win/catch, year-over-year tile, effective-rate trend, needs-action mark) show either a real, correctly-sourced value or are cleanly omitted — zero instances of a placeholder, a fabricated zero, or a partial computation, verified by test coverage for the omission path of each.
- **SC-004**: An owner can verify, for any answer, exactly which tool computed it and see the underlying data, without leaving the chat.
- **SC-005**: Every redeemed point is traceable to a specific campaign and date from the Points page alone, with no need to cross-reference the Promotions page.

## Assumptions

- The multi-year dataset (`backend/cmd/gendata`, `backend/data/live/`) is assumed present for meaningful year-over-year testing of User Story 2, but every requirement here also degrades correctly against a much smaller window per FR-013 — this spec does not depend on any specific dataset size being loaded.
- "Show your work" (FR-003) surfaces data already present in the existing `/api/ask` response shape; if a future response shape omits raw tool JSON, that is a separate, out-of-scope concern for this spec.
- Chart click-to-ask (FR-001) applies to chart types that already carry a per-day or per-entity identifier in their source data (period-totals bar/pie charts, platform comparison, promotion ROI); a chart type with no such identifier is out of scope for this spec, not silently forced to support it.
- The following 19 candidates from the original 31-idea brainstorm are deliberately deferred, named here so they are not silently dropped: time-aware default chat suggestions, a morning digest card, click-through from Promotions/Home into chat, inline sparklines in chat answers, answer export/share, Portuguese-language input (deferred specifically because it touches the core ambiguity-gate/explain prompts directly, too close to the interview date to risk regressing carefully-tuned English prompts without a full re-evaluation run), Cmd+K global quick-ask, a consecutive Clean-Close streak, a badge-category completion tracker, a points-earned-per-month chart, a "campaigns worth repeating" template, a cumulative spend-vs-revenue trend, a "where to grow" recommendation tile, a commission-cost-as-%-of-margin trend, promo efficiency per platform, and a "switch cost" what-if calculator.
- This is an internal enhancement batch to an already-shipped product (specs 001-007) — the existing schema, the 7 MCP tools, and the established design system are all assumed available as-is; no new MCP tool is introduced by this spec (every requirement is served by `get_daily_summary`, `get_margin_delta`, `list_discrepancies`, `compare_platform_economics`, and `get_period_totals`, all of which already exist).
