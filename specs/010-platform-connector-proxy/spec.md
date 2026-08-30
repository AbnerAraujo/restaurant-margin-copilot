# Feature Specification: Platform Connector Proxy (simulated iFood + Just Eat Takeaway)

**Feature Branch**: `feature/010-platform-connector-proxy`

**Created**: 2026-08-30

**Status**: Draft

**Input**: User description: "I want the revenue part to come from iFood and Just Eat Takeaway, but we don't have those APIs nowadays. I want you to create a proxy that solves the two different APIs and have a mocked service that returns [a] random value to the reconciliation, so I can mention that I'm emulating the platform connected with iFood and Just Eat Takeaway, and when [I] do the reconciliation in the upload file it gets the data from the proxy that gets from the API mocked."

## Background

Delivery-platform revenue is roughly a third of this restaurant's gross sales (`backend/cmd/gendata`: `ifoodShare` 0.17 + `jetShare` 0.17 against `posShare` 0.66), and today it can only reach the product one way: somebody exports `delivery_platform_export.csv` from each platform's merchant portal and it gets ingested from disk (`internal/ingest.ParseDeliveryExport`). That is the honest state of the world — this project has no iFood partner-API credentials and no Just Eat Takeaway partner-API credentials, and will not have them for a take-home prototype.

The product's real target architecture does not look like that. In a shipped version, delivery revenue arrives over each platform's partner API, on its own schedule, in its own format, and something in the middle has to reconcile two vendors that agree on nothing: different field names, different money representations, different date encodings, different pagination, different status vocabularies, different notions of what a refund even is. That "something in the middle" is the part worth designing and worth showing — and it is designable and showable **without** real credentials, provided the simulation is labeled as a simulation everywhere it surfaces.

This feature therefore builds the connector layer for real, and stubs only the thing that cannot be built: the two upstreams. Two mock platform APIs emit genuinely different wire formats; one connector proxy normalizes both into the record type `internal/ingest` already produces from CSV rows; and the existing, unmodified reconciliation engine consumes the result. No new arithmetic, no new margin path, no model involvement anywhere.

The honesty constraint is not decoration. This product's whole differentiating claim (`CLAUDE.md`: "a confidently wrong margin figure is worse than a refusal") collapses if a demo viewer can mistake randomly generated numbers for live iFood settlement data. Every surface this feature touches must say "simulated" before it says anything else.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Pull delivery revenue from the connected platforms instead of a CSV (Priority: P1)

An owner opens the ingestion page, switches from "upload a file" to the connected-platforms tab, picks a date range, and pulls delivery revenue straight from iFood and Just Eat Takeaway. Margin for those days recomputes from what the platforms reported, with no file anywhere in the flow.

**Why this priority**: This is the feature. Every other story exists to make this one safe or honest.

**Independent Test**: With the dataset already ingested, run a connector sync over a 3-day range, then re-read `GET /api/reconciliation` for those days and confirm gross sales, commissions, and margin changed to values derived from the connector's own reported orders — and that days outside the range are byte-identical to before.

**Acceptance Scenarios**:

1. **Given** both simulated platforms are reachable, **When** the owner syncs 2026-08-18..2026-08-20, **Then** the system reports, per platform per day, how many orders it received and what they totalled, before anything is persisted.
2. **Given** a preview is on screen, **When** the owner confirms the sync, **Then** the delivery revenue for exactly those days is replaced by the connector's records, the full reconciliation pipeline re-runs, and the response states total margin before and after.
3. **Given** a sync has been committed for 2026-08-18..2026-08-20, **When** the owner inspects a day *outside* that range, **Then** that day's numbers are unchanged — a range sync is authoritative only for the range it covered.
4. **Given** the same date range is synced twice, **When** the second sync commits, **Then** it produces identical numbers to the first (the simulated upstreams are deterministic per platform per day), not a second set of random figures.

---

### User Story 2 - Never mistake simulated revenue for real revenue (Priority: P1)

Anyone looking at this product — the owner, an interviewer, a colleague clicking around — can tell at a glance that the connected-platform numbers are emulated, without reading documentation and without hovering anything.

**Why this priority**: Equal to User Story 1. A margin figure sourced from a random generator and presented as a settled platform payout is precisely the failure mode this product's constitution names as unacceptable. Shipping User Story 1 without User Story 2 would be worse than shipping nothing.

**Independent Test**: Open the connected-platforms surface with no prior context and confirm the words identifying it as simulated appear before any dollar figure does; commit a sync and confirm the resulting provenance strings (which file, which rows — Constitution Principle IV) name the simulation rather than a filename that looks like a real export.

**Acceptance Scenarios**:

1. **Given** the connected-platforms surface, **When** it first renders, **Then** a persistent, non-dismissible notice states that these connections are emulated and that no real platform account is connected — positioned above the controls, not below the results.
2. **Given** a committed sync, **When** any resulting number's provenance is inspected, **Then** the source identifier is self-evidently synthetic (it names a simulated endpoint, not a plausible-looking export filename) and can never be confused with a real ingested file.
3. **Given** the connected-platforms surface, **When** the owner reads the platform status rows, **Then** each platform is labeled as a simulated connection, individually — not covered only by one global banner that could be scrolled past or screenshotted around.

---

### User Story 3 - See that the proxy actually reconciles two different APIs (Priority: P2)

A technical reviewer wants evidence that the proxy is doing real work — that the two upstreams genuinely differ and that normalization is not a rename.

**Why this priority**: The owner does not need this to close their day; the product still works without it. But the requester's stated purpose is to be able to say "I built a proxy that solves the two different APIs", and an unfalsifiable claim is worth less than a demonstrable one.

**Independent Test**: Read the two mock upstreams' raw payloads side by side and confirm they share no envelope shape, no money representation, no date encoding, and no status vocabulary; then confirm a single normalization test proves both converge on one identical record shape.

**Acceptance Scenarios**:

1. **Given** the two simulated upstreams, **When** their raw responses are compared, **Then** they differ in at least: response envelope, pagination style, money representation, date/time encoding, status vocabulary, and whether a commission *rate* is reported at all.
2. **Given** a sync preview, **When** the owner looks at a platform's row, **Then** the surface names that platform's wire format in one plain phrase, so the difference is visible in the product and not only in the source tree.

### Edge Cases

- **A requested date the upstream has no orders for.** The mock upstreams model a real platform: a closed day returns an empty order list, not an error. The reconciliation engine's existing `missing_delivery_source` flag then fires for that day exactly as it does for a gap in a CSV export — the connector never invents an order to avoid an empty day.
- **One platform reachable, the other failing.** A partial sync is a refusal, not a partial commit: committing iFood alone for a range would silently zero Just Eat Takeaway's revenue for those days and *reduce* margin with no flag explaining why. The system refuses the whole commit and names the platform that failed.
- **An upstream whose own numbers do not add up.** If a platform reports a commission that does not match its own stated subtotal and rate, or a payout that is not subtotal minus commission, the proxy refuses that fetch rather than normalizing a number it cannot justify. (The existing `commission_mismatch` flag covers a *file* that disagrees with itself; an API that disagrees with itself is a bug in the integration, and passing it through would launder it into the margin.)
- **A date range that runs backwards, or is unbounded.** Refused with a specific message. An unbounded range against a paginated upstream is an unbounded number of calls, which this product caps everywhere else (`CLAUDE.md`: "explicit cap on loop iterations").
- **A sync range covering days the CSV export also covers.** The connector wins for its own range, by construction — that is what "authoritative for the range it synced" means. Days outside the range keep their CSV-sourced delivery rows.
- **Committing a sync while a cost-sheet upload is committing.** Both write the same live dataset and re-run the same pipeline; they must not interleave, for the same reason `HandleCommitCostSheet` already serializes against itself.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST provide two simulated delivery-platform upstreams — one shaped as a plausible iFood partner API, one as a plausible Just Eat Takeaway partner API — that emit **materially different** wire formats from each other (differing envelope, pagination, money representation, timestamp encoding, and status vocabulary at minimum).
- **FR-002**: System MUST expose both upstreams to the rest of the product through **one** internal interface, such that no code outside the connector layer knows which platform a record came from except by reading its normalized platform field.
- **FR-003**: The connector MUST normalize both upstreams into the **same record type `internal/ingest` already produces from delivery CSV rows** — no parallel data model, no "API-sourced" variant of a reconciliation record.
- **FR-004**: Connector-sourced records MUST flow through the existing reconciliation engine unmodified. No reconciliation, margin, commission, refund, or anomaly logic may be special-cased on whether a record came from a file or an API.
- **FR-005**: The simulated upstreams MUST be deterministic: the same platform and the same calendar date MUST produce identical orders on every call, every process, and every machine — so a demo re-run and an evaluator's re-run see the same numbers.
- **FR-006**: Simulated order values MUST land at the same realistic scale as the product's existing dataset (same commission rates, comparable ticket sizes and daily volumes) so a synced day reconciles into a plausible margin rather than an obvious outlier.
- **FR-007**: System MUST let the owner preview a sync — per platform, per day: order count and gross total — without persisting anything.
- **FR-008**: System MUST let the owner commit a sync, which replaces delivery revenue **for the synced date range only**, re-runs the full reconciliation pipeline, and reports total margin before and after.
- **FR-009**: Every connector-sourced record MUST carry provenance identifying the simulated endpoint and the position of the record within that response — and that identifier MUST be self-evidently synthetic (Constitution Principle IV, plus this feature's honesty requirement).
- **FR-010**: The proxy MUST refuse a fetch whose upstream numbers are internally inconsistent (reported commission disagrees with reported subtotal × rate, or reported payout disagrees with subtotal − commission) rather than normalizing them.
- **FR-011**: The proxy MUST refuse a commit if **any** requested platform fails, rather than committing a partial range that would silently understate delivery revenue.
- **FR-012**: System MUST cap the number of days a single sync may cover and the number of upstream pages a single day may fetch, and refuse beyond those caps with a specific message.
- **FR-013**: The user interface MUST disclose, persistently and above the controls, that these platform connections are emulated and that no real iFood or Just Eat Takeaway account is connected — and MUST additionally label each individual platform as simulated.
- **FR-014**: The user interface MUST NOT present a connected-platform sync as a real integration in any copy: no "Connected", "Live", "Syncing from iFood" phrasing that omits the simulation qualifier.
- **FR-015**: Zero model involvement: no LLM call may occur anywhere in the fetch, normalization, reconciliation, or persistence path for this feature.
- **FR-016**: Committing a connector sync MUST invalidate the answer cache, for the same reason a cost-sheet commit already does — new source data can change any previously cached answer.
- **FR-017**: The connector sync commit MUST NOT interleave with another commit that writes the same live dataset.
- **FR-018**: System MUST NEVER modify the git-tracked hand-authored opening window as a result of a sync.

### Key Entities

- **Platform**: One of exactly two simulated delivery platforms. Carries the display name the reconciliation engine already normalizes into its source keys (`ifood`, `just_eat_takeaway`), so connector-sourced revenue lands in the same buckets CSV-sourced revenue does.
- **Raw platform response**: A single simulated upstream's own wire payload, in that platform's own format. Exists only inside the connector layer; never crosses its boundary.
- **Normalized delivery record**: The existing ingest delivery record. The connector's entire output contract.
- **Connector sync preview**: Per-platform, per-day order counts and gross totals for a requested range. Never persisted.
- **Connector sync result**: Rows committed, days affected, and total margin before and after — all derived from re-reading already-persisted reconciliations, never separately computed.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An owner can go from "no delivery data for this week" to "margin reflects delivery revenue for this week" using only the web UI and zero files, in under 30 seconds.
- **SC-002**: Syncing the same date range N times produces byte-identical persisted reconciliations every time, for any N.
- **SC-003**: A reader of the two mock upstreams' raw payloads can name at least six concrete format differences between them without reading the normalization code.
- **SC-004**: 100% of connector-sourced numbers displayed anywhere in the product are reachable only via a surface that has already stated the data is simulated, and carry a provenance identifier containing an unmistakable simulation marker.
- **SC-005**: The reconciliation engine's source files are unchanged by this feature — verifiable as a zero-line diff in `internal/reconcile`.
- **SC-006**: A day synced from the connector and a day ingested from CSV are indistinguishable to every downstream consumer (reconciliation, MCP tools, chat answers, badges) except by their provenance strings.

## Assumptions

These were chosen rather than clarified, because the requester's intent was unambiguous on the goal and silent on the details. Each is a smallest-reasonable default.

- **Two platforms, not a plugin system.** iFood and Just Eat Takeaway only, named explicitly, registered explicitly. A generic "add any platform" registry would be architecture for its own sake (`CLAUDE.md` non-goal) when the requirement names exactly two.
- **No authentication of any kind against the mock upstreams.** No OAuth, no token refresh, no credential storage, no retry/backoff, no rate limiting. These are the parts of a real integration that cannot be validated against a fake upstream — building them would produce untested ceremony that looks like production readiness without being it. The plan records this as an explicit non-goal so its absence reads as a decision, not an oversight.
- **"Random value" means deterministic-per-day pseudorandom, not fresh-random-per-call.** The requester asked for a mock returning random values; this project already established (`cmd/gendata`, seed `20260815`) that demo data must be re-runnable to the same numbers. A generator that changes on every call would make the demo unrepeatable and every test flaky. Interpreted as: unpredictable-looking, fixed per (platform, date).
- **Sync replaces the range, it does not merge into it.** Consistent with how `-ingest` and the cost-sheet commit already work: full re-derivation, not incremental update.
- **POS and supplier costs still come from the dataset.** This feature covers delivery-platform revenue only, which is what the requester asked for. There is no simulated POS API and no simulated supplier API.
- **The connected-platforms surface lives on the existing ingestion page**, as a second tab beside the cost-sheet upload — per the requester's own framing that reconciliation "in the upload file" should be able to source from the proxy. A separate top-level nav item would split one job ("get data into the product") across two places.
- **No historical backfill beyond a bounded range.** A sync covers a range the owner picks, capped. There is no "import everything since 2024" path.
- **Single-owner, unauthenticated, like every other endpoint in this prototype** (`internal/httpapi/client_errors.go`'s documented posture).
