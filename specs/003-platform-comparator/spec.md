# Feature Specification: Cross-Platform Economics Comparator

**Feature Branch**: `003-platform-comparator`

**Created**: 2026-08-28

**Status**: Draft

**Input**: User description: "Product D from the original 5-products comparison — Cross-Platform Economics Comparator (iFood vs. Just Eat Takeaway). Originally scored as not buildable by the take-home deadline because it needed 'two internally-consistent platform data models' — but both platforms already exist in the real fixture data with genuinely different commission rates (iFood 23% flat, Just Eat Takeaway 20% flat, per backend/fixtures/README.md), so this is now a tractable incremental feature of the existing product, not a separate product requiring new data."

## Background

`docs/product-strategy.md`'s 5-products comparison scored this concept highest on strategic differentiation ("most novel, Prosus-unique — something only Prosus, owning both platforms, could build in good faith") but lowest on near-term feasibility, specifically because it appeared to need two separately-modeled platform economics that didn't yet exist. They already exist: `delivery_platform_export.csv` carries a real `commission_rate_pct` per order, and the two platforms in this fixture set carry genuinely different flat rates (iFood 23%, Just Eat Takeaway 20%). This feature surfaces that real difference as a first-class comparison, rather than leaving it implicit inside a single blended margin figure.

This is scoped as an extension of the existing reconciliation product (per the Segment 1 customer this build serves), not the standalone product concept originally described — the original concept envisioned a dedicated product; this spec asks the same underlying question ("which platform's economics actually favor my business?") as a feature of the product that already exists.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - See which platform is economically better, at a glance (Priority: P1)

An owner who sells through both iFood and Just Eat Takeaway wants to know, without doing the math themselves, which platform costs more in commission relative to what it brings in — a comparison that requires overlaying data from two different exports, something they don't already have.

**Why this priority**: This is the whole point of the feature and is fully deliverable from data already ingested — no new fixture engineering, no new ingestion pipeline.

**Independent Test**: Using the existing 14-day fixture period, verify the comparator reports iFood's and Just Eat Takeaway's actual gross sales, actual commission paid, and actual effective commission rate, each independently checkable against `backend/fixtures/README.md`'s own reference tables.

**Acceptance Scenarios**:

1. **Given** the existing 14-day reconciled period, **When** the owner opens the platform comparator, **Then** they see, side by side for iFood and Just Eat Takeaway: total gross sales, total commission paid, and effective commission rate — each a real, independently-verifiable figure, not an estimate.
2. **Given** a period where one platform has zero orders, **When** the comparator is shown, **Then** that platform is shown with real zero values and is never silently omitted (an owner comparing platforms needs to see "nothing happened here" as clearly as "here's what happened").
3. **Given** the owner asks a natural-language question comparing the two platforms (e.g. "which platform costs me more in commission?"), **When** the ambiguity gate and explain step process it, **Then** the answer is grounded in the same comparator computation, not independently re-derived by the narration model.

---

### User Story 2 - Understand promo-adjusted economics per platform (Priority: P2)

An owner wants to know not just base commission economics but the full picture including promotional spend on each platform — since `docs/product-strategy.md`'s own research finding is that sponsored-placement costs are deducted from payout with the same opacity as commission.

**Why this priority**: Extends User Story 1 with data that already exists (via the existing promotion/ROI records) but requires joining two already-separate computations, so it is more work than the base comparison.

**Independent Test**: Verify a platform's combined commission-plus-promo-spend total, and its net effective take rate, against an independent hand-computation from the same fixtures.

**Acceptance Scenarios**:

1. **Given** a platform with both commission charges and promotional spend in the period, **When** the comparator's promo-adjusted view is shown, **Then** the combined economic cost (commission + promo spend) and a combined effective rate are both shown as distinct, separately-sourced figures — never silently merged into a single number that hides which part came from which mechanism.
2. **Given** a platform with no promotional activity in the period, **When** the promo-adjusted view is shown, **Then** it is identical to the base commission-only view for that platform, with promo spend shown explicitly as zero rather than omitted.

### Edge Cases

- What happens when a period includes a day with a missing delivery-platform source (the existing "missing day" fixture case)? That day's contribution to both platforms' totals must be honestly absent, not zero-filled, consistent with how the existing reconciliation already handles this case (a discrepancy flag, not a silent zero).
- What happens if a future onboarding brings a restaurant that only uses one platform? The comparator must degrade to a single-column honest view (or a clear "not enough data to compare" state) rather than fabricating a second platform's figures.
- What happens when commission rates change over time (a real-world possibility this fixture set does not model, since both rates are flat for the whole period)? Out of scope for this spec — see Assumptions.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST compute, per platform, for a given period: total gross sales, total commission paid, and effective commission rate (commission ÷ gross sales), derived from already-ingested per-order records — never estimated or looked up from a hardcoded rate table.
- **FR-002**: System MUST present both platforms' figures side by side for the same period, so a comparison never requires the owner to mentally hold one platform's numbers while looking up the other's.
- **FR-003**: System MUST show a platform with zero activity in a period as a real zero, never omit it from the comparison.
- **FR-004**: System MUST compute a promo-adjusted view per platform combining commission and attributed promotional spend (from the existing promotion/ROI records), with the two cost components shown distinctly, not merged.
- **FR-005**: This comparison MUST be exposed as a new typed tool in the same fixed, typed MCP tool set the rest of this product uses (no open SQL, no ad hoc computation) so it can be both narrated in chat and rendered as a standalone view, from one source of truth.
- **FR-006**: A natural-language question comparing platforms MUST be answered by calling this new tool, never by the narration model computing a comparison itself from two separate single-platform tool calls (consistent with the existing "no client/model-side arithmetic" rule already enforced for other tools).
- **FR-007**: The comparator MUST be reachable both as its own page/view and as a chart-in-chat result when a question's shape matches a platform comparison (reusing the existing deterministic chart-type-decision mechanism).

### Key Entities

- **PlatformEconomicsSummary**: A period's gross sales, commission, and effective rate for one platform — derived, not stored, computed the same way `DailyReconciliation` already is.
- **PlatformComparison**: Two `PlatformEconomicsSummary` values for the same period, one per platform, plus the promo-adjusted combined view.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Every figure in the comparator for the existing 14-day fixture period matches an independent hand-computation from the same source CSVs, to the cent.
- **SC-002**: An owner can identify which platform has the higher effective commission rate without performing any arithmetic themselves.
- **SC-003**: A natural-language platform-comparison question returns an answer citing the same tool-computed figures as the dedicated comparator view — the two surfaces never disagree.
- **SC-004**: A period with one platform at zero activity is shown honestly (a real zero, not an omission or an error state) 100% of the time.

## Assumptions

- Commission rates are treated as they exist in the ingested data — this feature does not model rate changes over time, tiered/volume-based rates, or platform-specific promotional-fee structures beyond what the existing `PromotionRoiRecord` already captures. A rate-history feature is out of scope here.
- "Platform" for this comparison means the delivery platforms already present in `gross_sales_by_source` (excluding `pos`, which is not a platform to compare — it has no commission at all, which is itself a fact worth stating but not part of a "which platform costs more" comparison).
- This feature does not require new ingestion or fixture engineering — it is a new read/computation over data this product already has.
