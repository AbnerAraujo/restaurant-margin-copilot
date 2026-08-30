# Implementation Plan: Platform Connector Proxy (simulated iFood + Just Eat Takeaway)

**Spec**: [spec.md](./spec.md) · **Status**: Ready for implementation

## Technical Context

A second, parallel **source** for delivery-platform revenue — not a second computation path. `internal/reconcile` is untouched: connector-sourced records are `ingest.DeliveryRecord` values, the exact type `ingest.ParseDeliveryExport` already produces, so `reconcile.ComputeDailyReconciliations` cannot tell the difference and does not need to.

What is new:

1. `backend/internal/platformconnector/` — two simulated upstreams with deliberately incompatible wire formats, one `Client` interface, one `Proxy` that dispatches and enforces the output contract.
2. `pipeline.RunIngestionPipelineWithDeliveryOverlay` — the existing pipeline, with an optional range-scoped delivery overlay.
3. Two HTTP endpoints (`preview`, `sync`) plus a connector-status endpoint.
4. A second tab on the existing `/upload` page.

Zero model involvement anywhere in this feature.

## Constitution Check

- **Principle I (deterministic core)**: Every number this feature produces comes from `internal/reconcile` running over `ingest.DeliveryRecord` values. The mock upstreams generate order *values*, but they generate them the way `cmd/gendata` does — from a fixed seed, in plain Go, with no model anywhere. ✅
- **Principle II (refuse rather than guess)**: Four distinct refusals are added, not zero — an upstream whose own numbers don't reconcile (FR-010), a partial-platform failure (FR-011), an over-cap range or page count (FR-012), and an inverted/unbounded range. The connector never fills a gap: a day the upstream reports as empty stays empty and inherits the existing `missing_delivery_source` flag. ✅
- **Principle III (typed tools / no open computation)**: N/A, and deliberately so — this feature adds **no MCP tool**. The model must not be able to trigger a data-mutating platform sync; that is an owner action behind an explicit confirmation, matching the posture `promotions_create.go` and `ingest_cost_sheet.go` already take for every write endpoint in this product. ✅
- **Principle IV (provenance)**: Every connector record carries a `SourceRowRef` whose `File` is a `simulated://` URI naming the exact endpoint, query, and page, and whose `Row` is the record's 1-based index within that page. Provenance is *stronger* here than in the CSV path, because the identifier also discloses that the source is synthetic. ✅
- **Honesty (CLAUDE.md's stated bar)**: The provenance string, the API response, the tab label, the page banner, and the per-platform status row each independently state that this is simulated. Removing any one of them still leaves the disclosure present. That redundancy is the design, not an accident.

No violations requiring justification.

## Non-goals, recorded so their absence reads as a decision

- **No OAuth, no token refresh, no credential store.** There is no credential to store. An auth flow against a fake upstream tests nothing and would misrepresent the integration's maturity.
- **No retries, no backoff, no rate limiting, no circuit breaker.** In-process function calls do not fail transiently. Simulating flakiness so that resilience code has something to catch would be fiction stacked on fiction.
- **No webhooks / push.** Pull-on-demand only.
- **No third platform, no plugin registry.** Exactly two, registered explicitly.
- **No persistence of raw platform payloads.** The raw envelopes exist inside one function call. Storing them would imply an audit trail this data does not deserve.

## Package layout: `backend/internal/platformconnector/`

Follows this codebase's existing `internal/<noun>` convention (`livedata`, `paraphrase`, `answercache` are all single-purpose packages named for the thing they own).

| File | Responsibility |
|---|---|
| `platform.go` | The `Platform` type, the two constants, and the display names `reconcile.normalizeSourceName` already maps to `ifood` / `just_eat_takeaway`. |
| `client.go` | The `Client` interface and the output contract every implementation must satisfy. |
| `seed.go` | The deterministic per-(platform, date) order model shared by both mocks. |
| `ifood_mock.go` | The simulated iFood upstream (raw JSON producer) **and** its normalizing adapter. |
| `jet_mock.go` | The simulated Just Eat Takeaway upstream and its adapter. |
| `proxy.go` | `Proxy`: dispatch by platform, range iteration, cap enforcement, contract validation, partial-failure refusal. |

### The interface

```go
type Client interface {
    Platform() Platform
    Describe() Description                                   // wire-format facts, for the UI (FR/US3)
    FetchDeliveryRevenue(ctx context.Context, date time.Time) ([]ingest.DeliveryRecord, error)
}
```

`FetchDeliveryRevenue` returns the **already-normalized** type. Normalization lives in each adapter, not in the `Proxy`, because only the adapter knows its own wire shape — a central normalizer would need a type switch over every platform's payload, i.e. exactly the coupling the interface exists to remove. The `Proxy` owns what is genuinely shared: dispatch, ordering, caps, and **verification that the contract was honored** (see below).

### Why the mocks emit real JSON bytes

Each mock upstream marshals its own Go structs to `[]byte` JSON, and its adapter `json.Unmarshal`s them back. The round trip is not free, and skipping it (having the mock return normalized records directly) would be simpler — and would also make the entire feature vacuous. The heterogeneity has to exist at the wire level for the normalization to be real work rather than a struct copy. This is the one place this feature spends complexity on purpose.

There is no HTTP server, no `httptest`, no network. The upstream is a function that returns JSON bytes. That is the smallest thing that makes the format difference genuine.

### The two wire formats, side by side

Both describe the same order. This is the demonstrable core of the feature (spec SC-003).

| | iFood mock | Just Eat Takeaway mock |
|---|---|---|
| Envelope | `{"merchant_id", "page":{...}, "orders":[...]}` | `{"data":[...], "cursor":{...}}` |
| Pagination | page number + `total_pages` | opaque base64 cursor + `has_more` |
| Field case | `snake_case` | `camelCase` |
| Order id | `id` | `orderReference` |
| Timestamp | RFC 3339 with offset: `2026-08-20T12:05:00-03:00` | epoch milliseconds UTC: `1755691500000` |
| Money | decimal **strings** in a nested object: `{"currency":"USD","amount":"42.00"}` | integer **minor units**: `4200` |
| Commission | nested, with an explicit `rate_percent` | `commissionMinor` only — **no rate reported at all** |
| Status | `CONCLUDED` / `CANCELLED` | `DELIVERED` / `REFUNDED` |
| Refund sign | cancelled orders reported with **positive** amounts + a `cancellation` block | refunded orders reported with **already-negative** minor units |
| Refund date | inside `cancellation.cancelled_at` | top-level `refundedAtEpochMs` |
| Campaign | `campaign.code` | `marketingCampaignRef` |

Nine differences, six of which change the *meaning* of a number rather than its spelling. The two that matter most:

- **JET reports no commission rate.** The adapter must derive `CommissionRateBps` from `commissionMinor / grossAmountMinor`, because `ingest.DeliveryRecord.CommissionRateBps` is what `reconcile.recomputeCommissionCents` independently cross-checks against. Getting this wrong would fire a `commission_mismatch` flag on every JET order.
- **The two platforms disagree on the sign of a refund.** This repo's canonical delivery-CSV convention is a *negative* subtotal/commission/payout on a `refunded` row (`cmd/gendata/opening/delivery_platform_export.csv` line 12, and `reconcile.computeOneDay`'s `abs64(r.SubtotalCents)`). JET already matches it; iFood does not, so its adapter negates. If the iFood adapter forgot to negate, a refund would be counted as *revenue* — a silently wrong margin, the exact failure class this product exists to prevent.

### Determinism

Per (platform, date), seeded from a FNV-64a hash of a fixed salt plus the platform key plus `YYYY-MM-DD`:

```go
seed := fnv64a(connectorSeedSalt + "|" + string(p) + "|" + date.Format("2006-01-02"))
rng  := rand.New(rand.NewSource(int64(seed)))
```

Deliberately **not** `cmd/gendata`'s single-stream seed (`randSeed = 20260815`, one `*rand.Rand` consumed in file order). That pattern is correct for generating a whole dataset once, top to bottom; it is wrong here, because a connector fetch is random-access — an owner may sync 2026-08-20 alone, or 08-18..08-20, or the same day twice — and a shared stream would return different orders for the same day depending on what was fetched before it. Hashing the key into its own seed makes each day an independent, order-insensitive draw. Same discipline (`cmd/gendata`'s doc comment: "deterministic — same seed, same dataset, every regen"), different mechanism, for a stated reason.

Scale parameters are taken directly from `cmd/gendata`'s own constants — `ifoodCommissionPct = 23.0`, `jetCommissionPct = 20.0`, `meanTicket = 32.0`, `refundRatePerOrder = 0.02` — so a synced day is arithmetically comparable to a CSV-ingested day instead of an obvious outlier (spec FR-006).

### What the `Proxy` verifies

Before returning any record to the rest of the product, the `Proxy` re-checks each one against the contract, regardless of which adapter produced it:

1. Platform display name matches the requested platform (so a record cannot land in the wrong `GrossSalesBySource` bucket).
2. `OrderDate` matches the requested date.
3. `CommissionCents` equals `SubtotalCents × CommissionRateBps / 10000` within 1 cent — the same tolerance `reconcile.computeOneDay` uses, computed with the same `money.DivRoundHalfUp`.
4. `NetPayoutCents` equals `SubtotalCents − CommissionCents`.
5. A `refunded` record has a non-nil `RefundDate` and a non-positive `SubtotalCents`; a `completed` one has neither.
6. Provenance is present and carries the `simulated://` scheme.

This is not defensive paranoia about code we control. It is the assertion that makes the interface a *contract* rather than a convention: a future adapter (a real iFood client, when credentials exist) fails loudly at the boundary instead of quietly emitting records that fire `commission_mismatch` on every day of the year. Checks 3 and 4 are also spec FR-010's refusal.

## Pipeline change: a range-scoped delivery overlay

`internal/pipeline/pipeline.go` gains:

```go
type DeliveryOverlay struct {
    From, To time.Time                // inclusive, calendar dates
    Records  []ingest.DeliveryRecord
}

func RunIngestionPipelineWithDeliveryOverlay(dataDir string, store *storage.Queries, overlay *DeliveryOverlay) error
```

`RunIngestionPipeline(dataDir, store)` becomes a one-line call with a nil overlay — every existing caller (`cmd/server`'s `-ingest`, `HandleCommitCostSheet`) is unchanged.

Semantics: CSV-parsed delivery rows whose `OrderDate` falls inside `[From, To]` are **dropped** and replaced by `overlay.Records`; rows outside the range are kept as-is. POS and cost-sheet parsing are untouched. Then the same `reconcile.ComputeDailyReconciliations` runs over the merged set and the same `storage.SaveDailyReconciliation` persists it.

Two properties this shape buys:

- **Margin stays correct for synced days.** Margin needs POS revenue and supplier costs for the same day, which still come from the dataset. A sync that only recomputed delivery would have to invent a partial-day margin; this recomputes the whole day from all three sources, the only way the number stays true.
- **Days outside the range are provably untouched** (spec Acceptance Scenario US1.3) — they are reconciled from exactly the inputs they were reconciled from before.

Considered and rejected: having the connector **write a CSV** into `livedata.Dir` and re-running the plain pipeline. It would have reused more code, but `pipeline.findSourceFiles` matches one delivery file by filename keyword, so a connector-written file would either collide with `delivery_platform_export.csv` or be silently ignored — and normalizing records only to re-serialize them to CSV and re-parse them is a round trip that can lose information (`OrderTime` formatting, note text) for no benefit.

## HTTP endpoints (`internal/httpapi/connector_sync.go`)

Same conventions as `ingest_cost_sheet.go` (manual method check, `writeJSON`/`writeJSONError`, `*storage.Queries` because the pipeline requires it, an `answercache.Cache` invalidated at the start of the write).

### `GET /api/connectors/platforms`

Static description of both simulated connectors: platform key, display name, whether it is simulated (always `true`), the commission rate it applies, and a one-phrase description of its wire format (spec US3 acceptance 2). No database access.

### `POST /api/connectors/sync/preview`

Body: `{"from":"YYYY-MM-DD","to":"YYYY-MM-DD","platforms":["ifood","just_eat_takeaway"]}`. Fetches through the proxy, persists nothing, returns per-platform-per-day order counts and gross totals plus a range total, and echoes `"simulated": true` at the top level of the response body — so even a client that ignores every UI affordance cannot render this data without having been told.

### `POST /api/connectors/sync`

Same body. Re-fetches from scratch (a prior preview is never trusted, matching `HandleCommitCostSheet`'s FR-007 discipline), then, under the same serialization discipline as the cost-sheet commit:

1. Read the before margin snapshot (`loadMarginSnapshot`, reused unchanged).
2. `livedata.EnsureReady()`.
3. Clear the answer cache (FR-016).
4. `pipeline.RunIngestionPipelineWithDeliveryOverlay(livedata.Dir, store, overlay)`.
5. Read the after snapshot.

Response: `{simulated: true, orders_synced, days_affected, platforms, before, after}`.

**Serialization across features.** The cost-sheet commit's mutex is scoped to its own handler closure. Both handlers now write the same live dataset and re-run the same pipeline, so a package-level `ingestMu` in `internal/httpapi` replaces it and both take it. Two different endpoints reconciling the same directory concurrently is exactly the interleaving `HandleCommitCostSheet`'s own doc comment already describes, one endpoint wider.

### Caps (FR-012)

`maxSyncDays = 31` and `maxPagesPerDay = 20`, both named constants with the refusal message naming the cap. The page cap is a real loop bound on the adapters' pagination loops, per the Constitution's "explicit cap on loop iterations".

## Frontend changes

`frontend/src/components/Upload/UploadPage.tsx` gains a two-tab layout. The existing cost-sheet flow moves into `CostSheetTab` with **zero behavioral change**; the new `ConnectedPlatformsTab` is a sibling. Page-level state (which tab) lives in `UploadPage`; each tab owns its own flow state.

No tab primitive exists in `components/ui/`, and this is the only two-tab surface in the product — a segmented control built from the existing `Button` with `role="tablist"` / `role="tab"` / `aria-selected` is the right size. Adding a general `Tabs` component for one caller would be speculative.

The simulation disclosure, at four independent levels:

1. **Tab label** — names the simulation in the label itself, so the disclosure is present before the tab is even opened.
2. **A persistent notice directly under the tab strip**, above every control, non-dismissible.
3. **Per-platform rows** each carrying their own "simulated" marker, so a screenshot cropped past the banner still discloses.
4. **Provenance strings** in any row detail, which begin `simulated://`.

Copy is written against the `ux-writing` skill's voice system (clear, concise, useful, human, honest) and this project's established sentence-case, no-Title-Case, no-ampersand conventions.

## Testing strategy

| Level | Test | Proves |
|---|---|---|
| `platformconnector` | Both adapters converge on identical `ingest.DeliveryRecord` field semantics for the same logical order | FR-003 — normalization is real |
| `platformconnector` | Same (platform, date) fetched twice, and in different range orders, yields identical records | FR-005 / SC-002 |
| `platformconnector` | Both mocks' raw payloads decode as different JSON shapes (asserted on the raw bytes) | FR-001 / SC-003 |
| `platformconnector` | A refunded order from each platform normalizes to negative subtotal + non-nil refund date | The sign-convention trap above |
| `platformconnector` | A deliberately corrupted adapter is refused by the proxy's contract check | FR-010 |
| `platformconnector` | Over-cap range, inverted range, unknown platform are refused with specific messages | FR-011 / FR-012 |
| `pipeline` | Overlay replaces in-range delivery rows and leaves out-of-range rows untouched | US1.3 |
| `httpapi` | Preview persists nothing; sync changes margin; response carries `simulated: true` | FR-007 / FR-008 |
| `httpapi` | Records reaching reconciliation carry `simulated://` provenance | FR-009 |
| frontend | The simulation notice renders before any figure; tab switching preserves the cost-sheet flow | FR-013 / FR-014 |

The `httpapi` tests run against a real Postgres, the same way `ingest_cost_sheet_test.go` already does.

## Documentation

`docs/prd.md` (new section), `docs/architecture.html` (the Platform Connector Proxy component and its two mocked upstreams, drawn on the deterministic side of the split), `docs/openapi.yaml` (three endpoints), `README.md`, and `CHANGELOG.md`.
