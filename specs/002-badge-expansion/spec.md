# Feature Specification: Badge System Expansion (Growth, Engagement, Campaign-Creation)

**Feature Branch**: `002-badge-expansion`

**Created**: 2026-08-28

**Status**: Draft

**Input**: User description: "Badge system expansion: add the Growth badge category (tied to positive promo ROI outcomes, using real data — no fabrication), the Engagement badge category (built on REAL app-usage tracking that starts honestly near zero, never a simulated/fabricated streak), and a reframed Campaign-Creation badge category firing on a genuine in-app action — the owner creating a new promotion record within this product itself, especially one explicitly logged as a replacement for a promotion already flagged as underperforming. This is an EXTENSION of the existing 001-margin-reconciliation-qa feature's badge system, not a rewrite of it."

## Background

`docs/product-strategy.md`'s "Badge system" section named three categories as roadmap, explicitly not built in the original take-home, each for a stated reason:

- **Growth** ("Smart Spender", "Margin Guardian"): deferred for lack of UI time, not a data problem — the promo-ROI data these badges need already exists.
- **Engagement** ("Week One", "Consistency Streak"): deferred because a fixture-data demo cannot organically produce a real usage streak, and fabricating one would be exactly the kind of invented signal this project's honesty discipline exists to refuse.
- **Campaign Creation** ("Campaign Launcher"): originally scoped as firing on a real integration with Prosus's own promotional tooling (e.g. via ToqanClaw automations) — an API this build has no access to.

This feature builds all three for real, honoring the same constraint that blocked each originally:

- Growth is now buildable because the time constraint that deferred it no longer applies — no design change needed, just build it.
- Engagement is redesigned around **real, persisted app-usage events** rather than simulated ones. It will honestly start near zero for every install; that is the correct, non-fabricated behavior, not a defect.
- Campaign Creation is **reframed**, not built as originally scoped: since no external promotional-tooling API is available, the badge instead fires on a real, verifiable action fully owned by this product — the owner logging a new promotion campaign inside the app itself, particularly one that replaces a campaign `list_negative_roi_promotions` already flagged as underperforming. This still closes the "insight → action" loop the original badge was designed for, using an action this build can actually verify happened.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - See growth recognized when a promotion pays off (Priority: P1)

An owner who ran a promotion that turned out to be profitable (positive ROI, per the existing `get_promotion_roi` computation) sees a Growth badge acknowledging it — the same quiet, non-arcade acknowledgment style as the existing Reconciliation badges.

**Why this priority**: This is the most directly buildable of the three (data already exists via User Story 4 of the original feature) and closes a real gap: today, a positive-ROI promotion produces no acknowledgment at all, only a negative-ROI one does (via the discrepancy-style flag).

**Independent Test**: Ingest the existing promotion fixtures, confirm `IFOOD-CAMP-BOOST01` (+$34.00) and `JET-CAMP-NEWMENU` (+$19.50) each produce a Growth badge on their attribution date, and that the existing negative-ROI campaign (`JET-CAMP-LUNCHFIX`, -$165.00) does not.

**Acceptance Scenarios**:

1. **Given** a promotion with a positive, attributable ROI, **When** the owner views their badges (Home, Today's Close, or `GET /api/badges`), **Then** a Growth badge appears dated to that promotion's attribution period.
2. **Given** a promotion with a negative or unattributable ROI, **When** the owner views their badges, **Then** no Growth badge appears for that promotion.
3. **Given** two positive-ROI promotions in the same period, **When** badges are computed, **Then** each is acknowledged once (no double-counting, no combined "bonus" badge inventing a value neither promotion earned on its own).

---

### User Story 2 - See consistency recognized honestly, starting from real zero (Priority: P2)

An owner who opens the app on multiple different days sees an Engagement badge (e.g. "Week One" after 7 distinct days of real use) — but a brand-new install, or one used only once, shows no fabricated streak.

**Why this priority**: Directly answers the roadmap's own stated blocker (no organic usage signal exists yet) by building the real signal rather than working around its absence. Lower priority than Growth because it requires new persisted infrastructure (Growth does not).

**Independent Test**: Record real usage events across several distinct calendar days for a test account/session, confirm the resulting streak count matches the actual number of distinct days recorded — never inflated, never pre-seeded.

**Acceptance Scenarios**:

1. **Given** a fresh install with zero recorded usage events, **When** the owner views their badges, **Then** no Engagement badge appears and no streak count is fabricated or defaulted to a non-zero value.
2. **Given** real usage recorded on 7 distinct calendar days (not necessarily consecutive, per Assumptions below), **When** badges are computed, **Then** a "Week One" badge appears, dated to the 7th real day of use.
3. **Given** usage recorded twice on the same calendar day, **When** the streak is computed, **Then** that day counts once, not twice — the count is of distinct days used, never of raw events.

---

### User Story 3 - Close the insight-to-action loop by logging a replacement campaign (Priority: P3)

An owner who sees a promotion flagged as losing money (via `list_negative_roi_promotions`) creates a new promotion record in the app to replace it, and receives a Campaign-Creation badge for that real, in-app action.

**Why this priority**: The most strategically interesting of the three per `docs/product-strategy.md` (closes insight → action), but requires the most new surface area — a real create-promotion flow that does not exist in this product at all today — so it is sequenced last.

**Independent Test**: Create a new promotion record through the app referencing a specific existing flagged campaign as "replacing", confirm a Campaign-Creation badge fires and that the new promotion appears in the same list/table surfaces as ingested promotions.

**Acceptance Scenarios**:

1. **Given** a promotion already flagged by `list_negative_roi_promotions`, **When** the owner logs a new promotion record marked as replacing it, **Then** a Campaign-Creation badge appears dated to the creation of the new record.
2. **Given** the owner logs a new promotion NOT marked as replacing anything, **When** badges are computed, **Then** no Campaign-Creation badge fires — the badge specifically rewards responding to a flagged problem, not promotion-logging in general (this distinction is itself a modeling choice worth stating plainly, not silently assumed).
3. **Given** an owner attempts to mark a new promotion as "replacing" a campaign that was NOT flagged as negative-ROI, **When** they submit it, **Then** the system refuses that specific claim (consistent with Principle II: refuse rather than accept an unverifiable premise) while still allowing the promotion to be logged without the replacement claim.

### Edge Cases

- What happens when a promotion's ROI is unattributable (the existing `unavailable: true` case)? It must not fire a Growth badge, positive or otherwise — an unknown outcome is not a growth outcome.
- How does the system handle a re-ingestion that changes a promotion's computed ROI from negative to positive (or vice versa) after a Growth badge was already shown? Per the existing badges design (computed at read time, never persisted), the badge set simply reflects the corrected data on the next read — no stale badge lingers, and none needs to be "revoked" since none was ever stored.
- What happens to Engagement usage-event data if the same session double-submits the same visit? Deduplicated to one real day, per Acceptance Scenario 3 above.
- What happens when an owner deletes or the system re-ingests over a promotion that a Campaign-Creation badge was earned for creating? The badge is derived from the promotion record's own existence and its `replaces` reference; if the record is gone, the badge is honestly gone too on the next read — consistent with "computed at read time, never persisted" for the other two categories.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST compute a Growth badge for each promotion whose ROI (via the existing `get_promotion_roi`/`list_negative_roi_promotions` computation) is positive and attributable, at read time, with no new persisted state — mirroring the existing Reconciliation badges' design.
- **FR-002**: System MUST NOT compute a Growth badge for a promotion whose ROI is negative, zero, or unattributable.
- **FR-003**: System MUST record a real, timestamped usage event when the owner opens the application, sufficient to derive "which distinct calendar days has this app actually been used on."
- **FR-004**: System MUST compute Engagement badges (e.g. "Week One") strictly from the count of distinct calendar days with at least one real recorded usage event — never a default, placeholder, or pre-seeded value.
- **FR-005**: System MUST allow the owner to create a new promotion record directly in the application (fields at minimum: campaign identifier/name, platform, date range, spend — matching the shape already used by ingested promotion records so it renders through the same surfaces).
- **FR-006**: System MUST allow a newly-created promotion record to optionally reference an existing promotion it is intended to replace.
- **FR-007**: System MUST refuse a "replaces" reference to a promotion that was not itself flagged as negative-ROI by `list_negative_roi_promotions`, rather than accepting an unverified claim.
- **FR-008**: System MUST compute a Campaign-Creation badge when a new promotion record is created with a valid "replaces" reference to a flagged promotion, and MUST NOT compute one for a promotion created without such a reference.
- **FR-009**: All three new badge categories MUST be exposed through the same `GET /api/badges` surface the existing Reconciliation badges use, with each badge's category and computation basis distinguishable in the response (never merged into an undifferentiated list that hides which category produced which badge).
- **FR-010**: The existing Points system (`internal/badges`' point-per-badge derivation) MUST be extended with real, documented point values for the three new badge codes, consistent with the existing "weights are auditable on screen" discipline — not hidden constants.
- **FR-011**: The Home page's existing "roadmap, not earning yet" labels for Engagement and Campaign points (`frontend/src/components/Home/HomePage.tsx`'s `roadmapCategory` fields) MUST be updated to reflect that these categories now earn for real, once implemented — an honest label that says "roadmap" for a category that is actually live would itself be a fabrication in the other direction.

### Key Entities

- **UsageEvent**: A single real record of the application being opened, with at minimum a timestamp. Used only to derive distinct-days-used; not itself shown to the owner.
- **Promotion** (extension of the existing promotion/ROI record concept): gains an owner-created origin (as opposed to file-ingested) and an optional reference to the promotion it replaces.
- **Badge** (extension of the existing concept): gains three new category codes (`growth`, `engagement`, `campaign_creation`) alongside the existing `clean_close`/`discrepancy_catcher`.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Every promotion in the existing fixture set with a real, positive, attributable ROI produces exactly one Growth badge, verifiable against the independently-computed reference values in `backend/fixtures/README.md`.
- **SC-002**: A freshly-provisioned instance with zero usage history shows zero Engagement badges and no non-zero placeholder streak value anywhere in the UI.
- **SC-003**: An owner can go from viewing a flagged underperforming promotion to having logged its replacement and seeing a Campaign-Creation badge in under 2 minutes of interaction, with no step requiring data the owner doesn't already have on screen.
- **SC-004**: 100% of badges shown anywhere in the product (all five categories combined) can be traced to a real, inspectable data condition — zero badges are ever awarded from a hardcoded, default, or simulated value.

## Assumptions

- "Distinct calendar day used" for Engagement is based on the server's own resolved data date-grounding convention (the same real min/max data range already used for date-grounding elsewhere in this product), not the owner's local wall-clock date, for consistency with how "today" is already defined everywhere else in this system.
- This is a single-owner prototype (per the existing constitution's scope constraints — no multi-tenant support yet), so usage events need no per-user/per-account partitioning; that would be revisited under the separate multi-tenant expansion, if and when that is built.
- A "Week One" Engagement badge requires 7 distinct days of use, not necessarily consecutive; a stricter consecutive-streak variant is a reasonable future refinement but not required to satisfy this spec's User Story 2.
- Growth and Campaign-Creation badges remain computed at read time with no new badge-specific table, consistent with the existing `internal/badges` design philosophy (`badges.go`'s own documented rationale for why badges are never persisted). Engagement genuinely requires one new persisted table (`UsageEvent`) because "days used" is not a fact any existing table already records.
- The new promotion-creation flow is scoped to what User Story 3 needs (create a record, optionally reference a replacement) — it is not a full promotion-management feature (no edit/delete UI) unless a later spec expands it.
