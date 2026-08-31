# Feature Specification: Cost Sheet Upload

**Feature Branch**: `007-cost-sheet-upload`

**Created**: 2026-08-28

**Status**: Draft

**Input**: User description: "implement the feature to upload the sheet with the costs, create the user interface with validation, preview, template — let the restaurant owner upload their supplier cost-sheet CSV through the web UI (not just the CLI `-ingest` flag), with real validation, a preview before committing, and a downloadable template showing the expected format."

## Background

Every other path onto this product's data today is the CLI `-ingest`/`-ingest-promo` flags (`backend/cmd/server/main.go`) — a data-directory scan a developer runs from a terminal. The owner this product is built for cannot run a CLI flag against a live server. The supplier cost sheet specifically is also the one input source this product cannot obtain any other way: delivery-platform and POS exports are *received* from the platforms/POS vendor, but the cost sheet is something the owner personally re-keys or exports themselves whenever supplier billing lands (`backend/cmd/gendata/opening/README.md`: irregular real-world supplier cadences — owner-driven, not a scheduled feed). Until this feature exists, updating input costs at all requires a developer with terminal access, which defeats the "opened and used by someone else" deliverable this project's constitution names as priority #1.

`backend/internal/ingest.ParseCostSheet` and `backend/internal/pipeline.RunIngestionPipeline` already implement real, tolerant parsing/validation and a directory-based re-ingest that already handles a missing delivery/POS source gracefully. This spec is about exposing that already-correct deterministic logic to the owner through the web UI — not writing new validation or reconciliation logic.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Upload a corrected cost sheet and see margin recomputed (Priority: P1)

An owner just received a new or corrected supplier invoice batch (a missed delivery, a price correction, a new supplier) and wants the product's margin figures to reflect it today, without asking anyone to run a command for them.

**Why this priority**: This is the whole point of the feature — closing the "developer required to update input costs" gap. Every other part of this spec (preview, template, error display) exists in service of this one outcome landing safely.

**Independent Test**: Upload a modified cost sheet CSV (one changed `amount` value) through the web UI, confirm the preview shows the new value before anything is written, confirm-and-commit, then verify the affected day's margin in `GET /api/reconciliation` reflects the new figure and no other day changed.

**Acceptance Scenarios**:

1. **Given** a well-formed cost sheet CSV with a header alias `ParseCostSheet` already accepts (e.g. `invoice_number` instead of `invoice_id`), **When** the owner uploads it, **Then** the preview succeeds and shows every parsed row with its recognized fields, exactly as `ParseCostSheet` interpreted them.
2. **Given** a valid preview is on screen, **When** the owner clicks "Confirm & Ingest", **Then** the file becomes the product's live cost sheet, the full ingestion pipeline re-runs, and the response states how many days were reconciled and what the period's total margin was before and after the change.
3. **Given** a first-ever upload with no prior ingested data in the database, **When** the owner commits, **Then** the pipeline runs successfully against the generated dataset directory (present because `cmd/gendata` has been run) and a before/after comparison is still shown, with "before" honestly reported as no prior data rather than a fabricated zero.

---

### User Story 2 - Get a specific, actionable error on a bad file (Priority: P1)

An owner uploads a CSV that's missing a required column, has a malformed date, or has unparsable currency in one row — the exact mess this product exists to catch elsewhere, now happening at the owner's own keyboard instead of in curated test data.

**Why this priority**: Equal priority to User Story 1 — a silent failure or a generic "something went wrong" here directly contradicts this project's "refuse rather than guess" constitution, and would make this exact feature worse than doing nothing (an owner who "fixes" the wrong thing based on a vague error has made their data worse, not better).

**Independent Test**: Upload a CSV with the `amount` column deleted, confirm the response names the specific missing column and never proceeds to a preview; upload a CSV with a malformed date in row 6, confirm the response names row 6 and the malformed value specifically.

**Acceptance Scenarios**:

1. **Given** a CSV missing a column `ParseCostSheet` requires (no `invoice_id`/`invoice_number`/`id` alias present at all), **When** the owner uploads it for preview, **Then** the response is the specific error `ParseCostSheet` produced (naming the missing column), not a generic failure message, and no preview or write occurs.
2. **Given** a CSV where row 7's `amount` is not a valid decimal, **When** the owner uploads it for preview, **Then** the response names row 7 and the offending field specifically, matching `ParseCostSheet`'s own row-numbered error.
3. **Given** a file that previewed cleanly, **When** the owner uploads a *different* file (or the same file re-edited) to commit, **Then** commit re-validates those exact bytes from scratch — a stale "it looked fine a minute ago" is never trusted.

---

### User Story 3 - Know the expected format before building the file (Priority: P2)

A new owner, or one troubleshooting a rejected upload, wants to see exactly what a correct cost sheet looks like rather than guessing at column names from an error message alone.

**Why this priority**: Materially reduces how often User Story 2's error path is even needed, but the product is usable without it (the error messages alone are specific enough to self-correct from) — hence P2, not P1.

**Independent Test**: Download the template with no other action taken; confirm it opens as a valid CSV with the real required headers and at least one realistic example row, and that uploading the downloaded template back through preview succeeds with no changes.

**Acceptance Scenarios**:

1. **Given** the owner has not yet uploaded anything, **When** they request the template, **Then** they receive a downloadable CSV with the real column headers (`invoice_id,invoice_date,supplier,category,amount,notes`) and example rows, obtainable from the upload page without needing to have first attempted (and failed) an upload.
2. **Given** the downloaded template, **When** it is uploaded back unmodified, **Then** it previews successfully (it is not a display-only convenience file — it is a genuinely valid input).

### Edge Cases

- What happens when the uploaded file is empty (zero bytes, or header row only)? `ParseCostSheet` already refuses this ("is empty") — the UI surfaces that message rather than treating zero rows as a valid, if boring, upload.
- What happens when the uploaded file is not a CSV at all (e.g. an Excel `.xlsx`, a PDF, an image)? The parser reads it as CSV regardless of extension; a non-CSV binary will fail with a parse error (Go's `encoding/csv` reader error, e.g. on a NUL byte or wildly inconsistent field count) — surfaced the same as any other parse failure, not specially detected upfront, since this product does not maintain a MIME/extension allowlist.
- What happens if the same file is uploaded and committed twice in a row? The second commit re-validates and re-runs the pipeline against identical bytes; because `RunIngestionPipeline` is a full re-derivation from source files (not an incremental diff), the result is byte-identical to the first commit's result — a harmless no-op, not an error.
- What happens if the dataset directory has never been generated (first request on a fresh clone)? The commit refuses with an explicit instruction to run `cmd/gendata` first — an upload committed into an empty directory would re-ingest a one-file "dataset", silently replacing the whole reconciled timeline, which is exactly the silent-data-loss failure mode this product refuses everywhere else. (Originally this seeded a baseline copy automatically; the contract changed when the product moved to one generated dataset.)
- What happens if two uploads race (two commits in flight at once)? Out of scope for this spec — this is a single-owner prototype with no concurrent-user story anywhere else in the product (see `docs/rfc-multi-tenant.md`'s own scoping); the second commit's file write and pipeline re-run simply happen after the first's, whichever wins the race, consistent with this product's un-locked, single-operator posture everywhere else.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST let the owner upload a cost-sheet CSV file through the web UI via a file picker (click-to-browse at minimum).
- **FR-002**: System MUST validate an uploaded file using the exact same parsing/validation logic (`ingest.ParseCostSheet`) the CLI ingestion path already uses — no second, UI-specific validation implementation that could disagree with it.
- **FR-003**: On a validation failure, system MUST return the specific, real error `ParseCostSheet` produced (including row number and field name where available), never a generic "upload failed" message, and MUST NOT write the file or run the reconciliation pipeline.
- **FR-004**: On successful validation, system MUST show the owner a preview of every parsed row (invoice ID, date, supplier, category, amount, notes) before anything is persisted or reconciled.
- **FR-005**: System MUST NOT persist the file or trigger reconciliation as a side effect of preview alone — preview is read-only.
- **FR-006**: System MUST let the owner download a template CSV, at any time, containing the real required headers and at least one realistic example row that itself parses successfully.
- **FR-007**: System MUST re-validate the file's actual bytes at commit time using the same `ParseCostSheet` logic, independent of whatever the client displayed during preview — a client-side "it was fine before" claim is never trusted.
- **FR-008**: On a successful commit, system MUST merge the newly uploaded file into the live cost sheet BY EXACT CALENDAR DATE — every date the upload covers has its prior rows for that date fully superseded by the upload's own rows for it; every other date's previously-committed rows MUST be left untouched — then re-run the full ingestion pipeline (`pipeline.RunIngestionPipelineWithConnectorOverlay`) against the live-data directory. *(Amended 2026-08-31: originally specified as a wholesale file replacement. Live reproduction showed a single realistic one-invoice upload correctly following that original wording while zeroing input costs on every one of the other 758 previously-committed days — a $1.28M margin overstatement from one intended, disclosed action. The commit endpoint's own "Replace cost sheet" button label and copy remain accurate under the new wording: from the owner's perspective, they are still replacing their record for the day(s) they're uploading — the fix is that "replace" now means "replace what this upload is actually about," not "replace everything ever committed." See CHANGELOG.md 2026-08-31 for the full incident.)*
- **FR-009**: System MUST report, after a successful commit, how many days were reconciled and the period's total margin before and after the change, so the owner can see the concrete effect of what they just uploaded.
- **FR-010**: System MUST NEVER write to, or otherwise modify, the git-tracked hand-authored opening window (`backend/cmd/gendata/opening/`) as a result of this feature — all uploads land in the git-ignored generated dataset directory (`backend/data/live/`).
- **FR-011**: The dataset directory's path MUST be a fixed, hardcoded location, never derived from any request input (filename, header, query parameter) — eliminating path traversal or overwriting checked-in data as a possible outcome of a malicious or malformed request by construction, not by a runtime check.
- **FR-012**: System MUST verify the generated dataset directory exists and holds data before committing an upload, and refuse with an explicit, actionable instruction (`go run ./cmd/gendata -out data/live`) when it does not — never auto-create an empty directory a lone cost sheet could then be re-ingested from. (Superseded the original auto-seed-from-checked-in-copy behavior when the product unified on one generated dataset.)

### Key Entities

- **CostSheetPreview**: The parsed-but-uncommitted result of validating an uploaded cost sheet — either a list of parsed rows (invoice ID, date, supplier, category, amount, notes) or a specific validation error. Never persisted; exists only for the duration of the preview response.
- **CostSheetCommitResult**: The outcome of a successful commit — days reconciled, and the period's total margin before and after the new file was ingested. Derived entirely from already-persisted `DailyReconciliation` reads before and after re-running the pipeline, never a separately-computed figure.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An owner can go from "has a corrected cost sheet CSV file" to "margin figures reflect it" using only the web UI, in under one minute, with zero terminal/CLI steps.
- **SC-002**: 100% of malformed uploads (missing column, malformed date, malformed amount) are rejected with the same specific, row-referenced error message `ParseCostSheet` itself would produce for a CLI ingest of the same file — never a generic failure.
- **SC-003**: the git-tracked `backend/cmd/gendata/opening/*.csv` files are byte-for-byte unchanged after any number of uploads, previews, or commits through this feature — this feature only ever writes inside `backend/data/live/`.
- **SC-004**: A downloaded template file, uploaded back unmodified, previews successfully 100% of the time.
- **SC-005**: A commit's before/after margin comparison matches an independent re-query of `GET /api/reconciliation` for the same period, to the cent.

## Assumptions

- This feature is scoped to the supplier cost sheet only, per the person requesting this build's own framing ("the sheet with the costs") — uploading delivery-platform or POS exports through the web UI is a natural follow-on but out of scope here; both remain CLI-only (`-ingest`) for now.
- "The live-data directory" is a single, unscoped location (`backend/data/live/`), not per-user or per-session — consistent with this being a single-owner prototype with no multi-tenancy (per `docs/rfc-multi-tenant.md`'s explicit gating of that concern to its own reviewed decision).
- A commit replaces the *entire* cost sheet, not a merge/append of new invoices into the existing one — matching how the CLI `-ingest` flag already works (a full directory re-scan and re-derivation, not an incremental update). An owner who wants to add one new invoice re-uploads the whole updated sheet, the same file they would otherwise hand to a developer to re-run `-ingest` with.
- The before/after comparison in FR-009 is a total-margin diff over whatever period is currently persisted, not a per-day diff or a highlighted list of which days changed — a full per-day diff view is a reasonable future enhancement but adds meaningfully more scope (matching two day-lists rather than reading two sums) for a first version of this feature.
- No authentication/authorization is added by this feature, consistent with every other endpoint in this build (`internal/httpapi/client_errors.go`'s own documented "unauthenticated, matching every other endpoint in this single-owner prototype").
