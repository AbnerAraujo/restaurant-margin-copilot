# Feature Specification: POS connector, and cross-source order deduplication

**Feature Branch**: `feature/012-pos-connector-dedup`

**Created**: 2026-08-30

**Status**: Draft

**Input**: Product owner: *"In the proxy also add another mocked service for POS. Create a solution to make sure the POS and iFood and Just Eat Takeaway orders are not duplicates."*

## Spec number

`012`. `011-inline-grounded-advice` exists on `feature/011-inline-advice` and has not merged to `develop`, so `011` is taken even though it is not visible on the `develop` tree. Spec 010 established the precedent that the next free number is the next one nobody has claimed on **any** branch, not the next one absent from `develop`.

## Background

`specs/010-platform-connector-proxy` gave delivery revenue a second route into the product: two simulated partner APIs behind one normalizing proxy, feeding the unchanged reconciliation engine. It deliberately stopped there. Its own Assumptions section says so in as many words: *"POS and supplier costs still come from the dataset. There is no simulated POS API."*

That leaves the in-house POS — **two thirds of this restaurant's gross sales** (`cmd/gendata`: `posShare` 0.66 against `ifoodShare` 0.17 + `jetShare` 0.17) — reachable only by somebody exporting `pos_export.csv` by hand. The connector story is two thirds unfinished on the revenue side, and it is the *larger* two thirds.

Adding a third mocked upstream is the easy half. The hard half is what adding it exposes.

### The problem a POS connector creates

Today's reconciliation treats POS and delivery-platform revenue as two disjoint, additive buckets. `GrossSalesBySource` has a `pos` key and an `ifood` key and a `just_eat_takeaway` key, and nothing in `internal/reconcile` has ever had to ask whether a row in one bucket describes the same real-world order as a row in another. It has never had to ask because, with two CSV exports produced by two unrelated systems, the product simply assumed no overlap.

That assumption is wrong in the real world, and it is wrong in a specific, well-known way. Modern restaurant POS systems integrate with delivery aggregators precisely so that front-of-house sees every order — dine-in, counter, and delivery — on one screen and one kitchen printer. When that integration is in place, a delivery order is **pushed into the POS** and becomes a POS ticket with its own POS order number. The same real-world order then appears twice:

1. once in the delivery platform's own settlement feed, and
2. once in the POS's own ticket feed.

Summing both double-counts that order's revenue. On a restaurant where delivery is a third of sales and the POS integrates with one aggregator, that is not a rounding error — it is a systematically inflated gross sales figure, an understated cost ratio, and a margin percentage that looks better than reality every single day. It is exactly the class of confidently-wrong number this product exists to refuse (`CLAUDE.md`).

The inverse failure is just as bad and much easier to commit accidentally. A deduplication rule that is too eager will merge two genuinely different orders that happen to share a price and a rough time — a real and unremarkable event on a busy evening with a $32 mean ticket — and **delete real revenue** from the day with no way for the owner to recover it. Dropping a real order and double-counting a duplicate one are the same failure wearing different signs.

This feature therefore has two deliverables that must ship together: a POS upstream, and a deduplication mechanism whose false-positive behavior is designed for, stated, and tested — not assumed away.

### What this feature does not touch

The `pos_export.csv` upload path is unchanged. A hand-uploaded POS export continues to be treated exactly as it is today: additive, no assumed overlap, no dedup pass. That is correct, because a CSV a human exported from a POS that has no delivery integration has no overlap to find, and this feature has no way to know whether the POS that produced a given file was integrated or not. Guessing would be estimating.

Deduplication applies **only within one connector sync**, over records the connector itself fetched and can therefore reason about with full knowledge of both sides.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Pull in-house POS revenue from the connector too (Priority: P1)

An owner opens the connected-sources tab and syncs a date range. Alongside the two delivery platforms, the POS terminal is now a source they can pull from, so a full day's revenue — dine-in, counter, and delivery — arrives without anyone exporting a file.

**Why this priority**: It is the requested feature, and it is the two-thirds of revenue the previous connector work left out.

**Independent Test**: Sync a 3-day range including the POS, then re-read `GET /api/reconciliation` for those days and confirm the `pos` gross-sales bucket now reflects the connector's own tickets, and that days outside the range are unchanged.

**Acceptance Scenarios**:

1. **Given** the connected-sources surface, **When** it renders, **Then** the POS appears as a third source alongside iFood and Just Eat Takeaway, individually labelled as simulated.
2. **Given** a sync covering the POS, **When** the owner previews it, **Then** the preview reports the POS's order count and gross total per day, with no commission and no refund column value invented for it (the POS charges no commission and this feature models no POS refunds).
3. **Given** a sync that includes the POS, **When** it commits, **Then** the POS revenue for exactly those days is replaced by the connector's tickets and every other day keeps its CSV-sourced POS revenue.
4. **Given** a sync that does **not** include the POS, **When** it commits, **Then** POS revenue for those days is untouched — a delivery-only sync must never clear POS revenue.

---

### User Story 2 - The same order is never counted twice (Priority: P1)

An owner syncs the POS and the delivery platforms together for a range in which the POS recorded some orders that also arrived through a delivery platform. The resulting margin counts each of those orders once, not twice.

**Why this priority**: This is the actual request. A POS connector without it makes the product's headline number worse than it was before the feature shipped.

**Independent Test**: Sync a range with all three sources and compare gross sales against the sum of the three sources' raw reported totals. The difference must equal exactly the sum of the removed duplicates' amounts, and must be explained line by line by that day's discrepancy flags.

**Acceptance Scenarios**:

1. **Given** a POS ticket and a delivery-platform order that are the same real-world order, **When** the sync commits, **Then** the day's gross sales include that order's amount once.
2. **Given** the duplicate above, **When** the day's discrepancy flags are read, **Then** a flag names both sides of the match — which POS ticket was removed, which platform order it was matched to, and on what evidence.
3. **Given** the duplicate above, **When** the reconciliation is inspected, **Then** the **delivery-platform** record is the one kept, so the platform's commission on that order is still charged against margin and the revenue is still attributed to the platform's own source bucket.
4. **Given** a POS ticket for a genuinely in-house order (dine-in or counter) that happens to share an amount and a nearby time with a delivery order, **When** the sync commits, **Then** it is **not** removed and no duplicate flag is raised for it.

---

### User Story 3 - An uncertain match is disclosed, never guessed (Priority: P1)

When the evidence does not uniquely identify a duplicate, the owner is told so, on the day it affects, in the same place every other reconciliation discrepancy already appears.

**Why this priority**: Equal to User Story 2. A dedup rule that silently guesses under ambiguity is worse than no dedup rule, because its errors are invisible and unrecoverable. This story is what makes User Story 2 safe to trust.

**Independent Test**: Construct a day in which one POS ticket tagged as a delivery-channel order matches two different delivery orders equally well. Confirm nothing is merged, both delivery orders survive, the POS ticket survives, and a flag names the ambiguity and both candidates.

**Acceptance Scenarios**:

1. **Given** a delivery-channel POS ticket with more than one equally good platform counterpart, **When** the sync commits, **Then** nothing is merged, and a flag states that the day may double-count that ticket and names every candidate.
2. **Given** a POS ticket the POS itself tagged as arriving through a delivery platform, **When** no counterpart is found in that platform's feed for the day, **Then** a flag states that the ticket claims a delivery origin whose order could not be located, so the owner knows a possible double-count is present and where.
3. **Given** a matched pair whose two sides report **different** amounts, **When** the sync commits, **Then** the duplicate is still resolved (identity was established independently of amount) **and** a separate flag reports both amounts, so a platform-funded discount or a POS-side correction surfaces instead of being absorbed.

---

### User Story 4 - The POS mock is a real exercise, not a rubber stamp (Priority: P2)

A technical reviewer can see that the deduplication logic is solving a problem the simulation genuinely poses, rather than a problem arranged to be solvable.

**Why this priority**: The owner does not need it. The claim "I built cross-source deduplication" is worth much less if the mock was built to make it easy.

**Independent Test**: Read the POS mock's raw payload beside the two delivery mocks' and confirm a third, distinct wire format; then confirm that the overlapping orders it emits are derived from the *actual* orders the delivery mocks emit for the same date, not from an independent generator that happens to agree.

**Acceptance Scenarios**:

1. **Given** the three simulated upstreams, **When** their raw payloads are compared, **Then** the POS's differs from both delivery mocks in envelope, pagination model, money representation, timestamp encoding, and status vocabulary.
2. **Given** a date, **When** the POS mock's delivery-channel tickets are compared with that date's delivery-platform orders, **Then** each is an echo of a specific real order from that feed, so a duplicate the matcher finds is a true duplicate and not a coincidence the mock arranged.
3. **Given** a date, **When** the POS mock's output is inspected, **Then** it also contains a majority of genuinely in-house tickets that have no delivery counterpart at all and must survive the dedup pass untouched.

### Edge Cases

- **A POS ticket the POS never tagged as a delivery order.** Not a candidate for matching, at any amount, at any time. See FR-011: this feature refuses to match on amount and time alone.
- **Two POS tickets carrying the same platform order reference.** Reference equality is identity, so both are removed and each removal is flagged separately.
- **A POS ticket whose platform reference names an order not present in the fetch.** Flagged as unresolved, and *not* then matched by amount and time — a reference that does not resolve means the picture is known to be incomplete, and guessing on top of a known gap is worse than admitting it.
- **A POS ticket tagged with a delivery platform that was not part of this sync.** Left alone, unflagged. The connector is authoritative only for what it fetched, exactly as spec 010 established for date ranges.
- **A sync that includes the POS but no delivery platform.** No matching is possible; POS tickets are committed as reported. The delivery-channel tickets among them are not flagged, because their counterparts were never in scope.
- **A day where the POS reports nothing.** An empty answer is a real answer. Nothing is invented, and the day reconciles from whatever sources are present.
- **A matched pair split across midnight** (a platform order placed at 23:58 whose POS ticket rings at 00:03). Out of scope: matching is scoped to a single calendar day. The simulated data cannot produce this case, and the limitation is recorded rather than papered over.

## Requirements *(mandatory)*

### Functional Requirements

**The POS upstream**

- **FR-001**: System MUST provide a third simulated upstream shaped as a plausible POS terminal API, emitting a wire format materially different from **both** existing delivery mocks (envelope, pagination model, money representation, timestamp encoding, and status vocabulary at minimum).
- **FR-002**: The POS connector MUST normalize into the **same `POSRecord` type `internal/ingest` already produces from `pos_export.csv`** — no parallel POS data model.
- **FR-003**: The POS upstream MUST be deterministic per calendar date, on the same terms as the delivery upstreams (FR-005 of spec 010).
- **FR-004**: The POS upstream MUST emit, for a controlled and deterministic subset of a day's tickets, orders that are **echoes of that same date's real delivery-platform orders** — same underlying order, recorded a second time by the POS — so the deduplication logic has genuine duplicates to find.
- **FR-005**: The POS upstream MUST also emit a majority of genuinely in-house tickets with no delivery counterpart, so a rule that over-matches is detectable.
- **FR-006**: A sync MUST be able to include or exclude the POS independently of the delivery platforms, and a sync that excludes the POS MUST leave POS revenue for those days untouched.

**Deduplication**

- **FR-007**: The system MUST identify, within a single connector fetch, POS tickets and delivery-platform orders that describe the same real-world order.
- **FR-008**: The matching rule MUST be deterministic, explainable, and expressible as plain auditable logic. **No model, no probabilistic scoring, no fuzzy similarity** may participate in the decision (Constitution Principle I).
- **FR-009**: When a POS ticket carries the delivery platform's own order reference, that reference MUST be treated as identity, and MUST take precedence over any other evidence.
- **FR-010**: When a POS ticket declares a delivery-platform origin but carries no usable reference, the system MAY match it to a platform order **only** when all of the following hold: same platform, same calendar date, **exactly equal** amount in cents, order times within a stated bounded window, and the pairing is the **unique** solution from both directions — the POS ticket has exactly one candidate, and that candidate is the candidate of no other POS ticket.
- **FR-011**: The system MUST NOT match a POS ticket that the POS did not itself tag as arriving through a delivery channel, regardless of how well its amount and time align with a delivery order.
- **FR-012**: When the evidence does not uniquely identify a pairing, the system MUST NOT merge anything, MUST leave every record involved intact, and MUST raise a discrepancy flag naming the ambiguity and its candidates.
- **FR-013**: When a duplicate is resolved, the **delivery-platform** record MUST be the one kept and the POS ticket MUST be the one dropped, so the platform's commission on that order continues to be charged and the revenue stays attributed to the platform's source bucket.
- **FR-014**: Every deduplication outcome — a removal, an unresolved ambiguity, an unlocatable counterpart, an amount disagreement — MUST surface as a discrepancy flag on the affected day, using the vocabulary `internal/reconcile` already establishes. Nothing may be silently corrected (Constitution Principle II).
- **FR-015**: A resolved duplicate whose two sides report different amounts MUST raise a separate flag reporting both amounts.
- **FR-016**: The sync preview MUST show the post-deduplication figures — what will actually land — including how many duplicates were removed and how many could not be resolved.

**Scope and safety**

- **FR-017**: The existing `pos_export.csv` upload and `-ingest` paths MUST be behaviourally unchanged. No deduplication pass may run over CSV-sourced records.
- **FR-018**: Deduplication MUST be confined to records fetched within a single sync. POS tickets referencing a platform outside that fetch MUST be left alone.
- **FR-019**: Zero model involvement anywhere in the fetch, normalization, matching, reconciliation, or persistence path for this feature.
- **FR-020**: The POS connector MUST disclose that it is simulated at the same bar spec 010 set for the delivery connectors: the package doc, the provenance string, the API response body, the source list row, and the persistent UI notice — five independent disclosures, any one of which removed still leaves the fact stated.
- **FR-021**: The POS upstream MUST cap the number of tickets it will return for one day and refuse beyond that cap, rather than iterating over an unbounded response (`CLAUDE.md`: "explicit cap on loop iterations").

### Key Entities

- **POS ticket**: One order as the in-house POS recorded it. Carries a service type (dine-in, counter, or delivery-partner), and for a delivery-partner ticket, the name of the platform and — sometimes — the platform's own order reference.
- **Cross-source match**: A decided pairing of one POS ticket with one delivery-platform order, together with the evidence that decided it.
- **Unresolved overlap**: A POS ticket the system believes may be a duplicate but cannot pair with confidence. Never merged; always disclosed.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For any synced range, gross sales equal the three sources' raw reported totals minus exactly the amounts of the removed duplicates — a difference fully accounted for, line by line, by that range's discrepancy flags.
- **SC-002**: Zero in-house POS tickets are removed by the dedup pass, over the full simulated dataset — measured, not assumed.
- **SC-003**: A reader can state the matching rule in three sentences and predict its output on any given pair without running it.
- **SC-004**: Syncing the same range N times produces byte-identical persisted reconciliations, including identical discrepancy flags in identical order, for any N.
- **SC-005**: A reader of the three mock upstreams' raw payloads can name at least five format differences between the POS mock and either delivery mock without reading the normalization code.
- **SC-006**: The `pos_export.csv` upload path produces byte-identical results before and after this feature.
- **SC-007**: Every number the POS connector contributes is reachable only through a surface that has already stated the data is simulated, and carries a provenance identifier containing an unmistakable simulation marker.

## Assumptions

Chosen rather than clarified, each the smallest reasonable default.

- **One aggregator is integrated into the POS, not both.** The simulation models iFood orders being pushed into the POS and Just Eat Takeaway orders not being. This is the common real configuration — restaurants integrate the aggregator they do most volume with — and it is also the most useful one to build against, because it gives the matcher a control group inside the same fetch: JET orders that must never be touched, sitting beside iFood orders that must be.
- **The cross-reference field is a fair simulation, not a shortcut.** Real POS/aggregator integrations do record the partner's order id on the ticket; that is how the order got there. Assuming it is *always* present would be the shortcut, so the mock deliberately omits it on a quarter of echoed tickets, and the matcher has to earn those without it. Stated honestly: if the reference were present on 100% of tickets, the amount-and-time tier would be decoration.
- **±15 minutes is the matching window.** Wide enough to cover the gap between a platform's order-placed timestamp and a POS ticket time (injection lag, kitchen acceptance, clock skew between two systems); narrow enough that, combined with exact-cent equality and channel tagging, an accidental unique match is rare. The number is a stated modelling choice, not a derived constant.
- **Ambiguity resolves toward disclosure, not toward merging.** When the rule cannot decide, both records survive and the day is flagged. Rationale: a wrong merge destroys revenue irrecoverably from the reconciliation output, whereas a missed merge leaves an inflated figure that the flag tells the owner exactly where to check. Between two disclosed errors, prefer the recoverable one.
- **No POS refunds, no POS commission.** The POS charges no commission, and this feature models no POS-side refund. A non-completed POS ticket is excluded from gross exactly as `internal/reconcile`'s existing `pos_non_completed_row_excluded` flag already handles for the CSV path.
- **No fourth source, no plugin registry.** Exactly three, registered explicitly, for the same reason spec 010 gave for two.
