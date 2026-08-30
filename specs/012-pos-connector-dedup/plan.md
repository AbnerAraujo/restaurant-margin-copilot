# Implementation Plan: POS connector, and cross-source order deduplication

**Spec**: [spec.md](./spec.md) · **Status**: Ready for implementation

## Technical Context

Two additions on top of `specs/010-platform-connector-proxy`, which this extends rather than reworks:

1. A third simulated upstream — a POS terminal API — behind the same package, with its own wire format and its own adapter.
2. A deterministic cross-source matcher that runs once per fetch, after all three upstreams have answered, inside `platformconnector`.

Plus the plumbing to carry POS records and dedup decisions through the pipeline into `internal/reconcile`'s existing discrepancy-flag machinery.

Zero model involvement anywhere.

## Constitution Check

- **Principle I (deterministic core)**: The matcher is integer-cent equality, string equality, and a minute-difference comparison. There is no scoring function, no threshold on a similarity metric, and no model. Every decision it makes can be recomputed by hand from the two records. ✅
- **Principle II (refuse rather than guess)**: The matcher's entire ambiguity behaviour is a refusal — it declines to merge and raises a flag. It never picks the "best" candidate. FR-011 additionally refuses a whole class of matching (untagged POS tickets) rather than accepting a rule with a false-positive rate it cannot bound. ✅
- **Principle III (typed tools)**: No new MCP tool. Syncing is an owner action behind an explicit confirmation, as spec 010 established. ✅
- **Principle IV (provenance)**: Every POS connector record carries a `simulated://` `SourceRowRef`. Every dedup flag names the file and row of **both** sides of the decision, so the owner can find the removed ticket and the order it was merged into. ✅
- **Honesty**: Five independent disclosures for POS, matching spec 010's bar exactly — package doc, provenance scheme (enforced by the contract check, not merely intended), `"simulated": true` in every API body, the per-source UI row, and the persistent panel notice.

No violations requiring justification.

## The interface question, decided against the obvious answer

The natural move is to make the POS mock implement the existing `Client` interface, whose `FetchDeliveryRevenue` returns `[]ingest.DeliveryRecord`. **Rejected**, and this is the plan's most consequential decision, so it is recorded here rather than left in a code comment.

A POS ticket forced into a `DeliveryRecord` would carry `CommissionRateBps = 0` and `CommissionCents = 0`. `reconcile.computeOneDay` sums `commissionsBySource[src]` over **every** delivery record, so the day would grow a `commissionsBySource["pos"] = 0` entry. That entry is not cosmetic: `RefundsBySource` and `CommissionsBySource` both carry a documented invariant in `internal/reconcile/types.go` that `"pos" never appears here`, and `compare_platform_economics` (specs/003) reads those maps to rank platforms by economics. The POS would appear in the platform comparator as a delivery platform charging 0% commission — a new, wrong answer in a part of the product that never mentions connectors. The type would also silently discard `Channel` and `PaymentMethod`, which is the exact information the dedup rule depends on.

So the package gains a **peer** interface instead, sharing the half that is genuinely common:

```go
type connector interface {
    Platform() Platform
    Describe() Description
}

type Client interface {                    // unchanged
    connector
    FetchDeliveryRevenue(ctx, date) ([]ingest.DeliveryRecord, error)
}

type POSClient interface {                 // new
    connector
    FetchPOSOrders(ctx, date) ([]POSOrder, error)
}
```

`Describe()` is shared, so `GET /api/connectors/platforms` enumerates all three uniformly and the UI needs no special case. The `Proxy` holds both registries and `FetchRange` takes `PlatformPOS` in its `platforms` slice like any other key.

`POSOrder` wraps `ingest.POSRecord` with the two matching signals the shared type has no field for:

```go
type POSOrder struct {
    Record           ingest.POSRecord
    DeliveryPlatform Platform  // "" for an in-house ticket
    PartnerOrderRef  string    // "" when the integration did not populate it
    PlacedAt         time.Time // in merchantZone
}
```

Considered and rejected: adding `PartnerOrderRef` to `ingest.POSRecord`. It would be permanently empty on the CSV path (no such column exists in `pos_export.csv`), so it would be a connector-only concern parked in a shared type. Keeping it inside the connector means `ingest` has a zero-line diff.

## The POS mock's wire format

Third format, disagreeing with both existing mocks on every decision either of them makes.

| | iFood mock | JET mock | **POS mock (new)** |
|---|---|---|---|
| Envelope | `{"merchant_id","page":{…},"orders":[…]}` | `{"data":[…],"cursor":{…}}` | **NDJSON — no envelope at all**, one ticket per line |
| Pagination | page number + `total_pages` | opaque base64 cursor | **none** — a terminal returns the whole business day |
| Field case | `snake_case` | `camelCase` | `snake_case`, but different names throughout |
| Money | decimal strings `"42.00"` | integer minor units `4200` | **pt-BR decimal strings: `"1.234,56"`** — thousands dot, decimal comma |
| Timestamp | RFC 3339 with offset | epoch ms, UTC | **zone-less local `"2026-08-20 19:35:00"`** |
| Status | `CONCLUDED` / `CANCELLED` | `DELIVERED` / `REFUNDED` | `PAID` / `VOID` |
| Channel | n/a | n/a | `service_type`: `DINE_IN` / `COUNTER` / `DELIVERY_PARTNER` |
| Cross-ref | n/a | n/a | `delivery_partner: {name, partner_order_ref?}` |

Two of those are traps with teeth, in the same spirit as the JET mock's derived rate and iFood's refund sign:

**The pt-BR amount.** `money.ParseCents` handles `"1234.56"`. Handing it `"1.234,56"` does not error in an obvious way — it is a plausible-looking string that a naive parser reads as **$1.23**, understating a ticket by three orders of magnitude. The adapter converts explicitly (strip thousands dots, comma to point) and refuses anything that does not fit the shape, rather than best-efforting it.

**The zone-less timestamp.** `time.Parse("2006-01-02 15:04:05", …)` yields UTC. The calendar *date* survives that mistake for every ticket this mock emits (an 19:35 local ticket read as 19:35 UTC is still the 20th), which is exactly what makes it dangerous — nothing downstream would look wrong. What it silently destroys is the **three-hour offset in every ticket time**, which is the input to the ±15-minute matching window. Every amount-and-time match in the product would stop firing, duplicates would flow through, and gross sales would quietly inflate with no error anywhere. `time.ParseInLocation(…, merchantZone)` is the whole fix, and `TestPOSAdapter_TicketTimeIsReadInTheMerchantZone` is the whole proof.

### The controlled-overlap mechanism (FR-004)

The POS mock does **not** invent orders that look like delivery orders. For a given date it calls the same `simulateDay(PlatformIFood, date, ifoodCommissionBps)` the iFood mock calls, and echoes those actual orders as POS tickets. Because `simulateDay` is deterministic per (platform, date), the echo carries the same cents and the same reconstructible order id as the order the iFood adapter will independently return in the same fetch. The duplicate is therefore causally real — the same simulated order recorded twice — not two generators arranged to agree.

The modelling choices, each stated so the reviewer can judge them:

- **iFood is integrated into the POS; Just Eat Takeaway is not.** Every iFood order for a date is echoed; no JET order ever is. This is the common real configuration (a restaurant integrates the aggregator it does most volume with), and it buys the matcher a control group inside every single fetch: JET orders that must never be touched sitting beside iFood orders that must be.
- **The partner reference is present on ~75% of echoed tickets, absent on ~25%**, drawn from the POS day's own seeded RNG. Real integrations do record the partner order id; assuming they always do would make the amount-and-time tier decoration. The 25% stands for the ordinary ways a reference goes missing — a ticket re-fired after a printer failure, a manual re-entry. This is a deliberate choice to make the harder tier reachable, and it is disclosed as such rather than presented as measured behaviour.
- **An echoed order that carried a platform campaign records a *different* amount on the POS side.** The POS never saw the platform's promotion, so it rings the undiscounted menu price. Modelled as the platform subtotal grossed back up by a fixed 10% campaign discount. This gives the amount-mismatch flag (FR-015) a real cause and gives the amount-and-time tier a real "no counterpart found" case to disclose.
- **In-house tickets are the majority.** POS is 66% of gross against iFood's 17%, so a day's POS feed is roughly two thirds dine-in and counter tickets that must survive untouched. The ratio is taken from `cmd/gendata`'s own shares rather than picked.
- **Ambiguity is not manufactured.** The mock does not arrange a two-candidate collision so the unresolved flag has something to do. That path is proven by unit test with hand-built records; whether it also fires against the simulated dataset is an empirical question, reported as measured rather than engineered.

## The matching rule

Stated in full, because SC-003 requires a reader be able to predict it.

> A POS ticket is a duplicate of a delivery order **only if** the POS itself said the ticket arrived through a delivery channel. Within that set: if the ticket carries the platform's order reference and that reference resolves, they are the same order. Otherwise they are the same order only if they share a platform, a calendar date, and an exact amount in cents, their times are within 15 minutes, and no other reading of the day's tickets is equally consistent.

Implemented as two passes over one fetch:

**Pass 1 — reference (identity).** For each POS ticket with a `PartnerOrderRef`, find delivery records on the same date whose `OrderID` matches case-insensitively after trimming, and whose platform matches the ticket's declared platform. A resolved reference is identity: merge, regardless of amount. If two POS tickets carry the same reference, both merge — reference equality means both describe that one order. If the reference resolves to nothing, flag `unresolved` and **do not fall through to pass 2**: a reference that does not resolve is proof the picture is incomplete, and matching by amount on top of a known gap is guessing twice.

**Pass 2 — channel + exact amount + bounded time (inference).** Eligible tickets: declared a delivery platform present in this fetch, no reference. For each, the candidate set is every unclaimed delivery record with the same platform, same date, `SubtotalCents == GrossCents` exactly, and `|ticketMinutes − orderMinutes| ≤ 15`. Then, symmetrically:

- ticket has exactly one candidate, **and** that candidate appears in no other ticket's candidate set → merge.
- ticket has zero candidates → `unresolved_no_counterpart`.
- anything else → `unresolved_ambiguous`, for **every** ticket involved, and the contested orders stay unclaimed.

The symmetry is the point. A rule that merged whenever the ticket had one candidate would be order-dependent: whichever ticket happened to be processed first would take the order and the second would be left "unmatched", which reads like a clean result and is actually a coin flip. Requiring the pairing to be unique from both directions makes the outcome independent of iteration order and makes "we could not tell" an outcome the rule can express.

### Why not match on amount and time alone (FR-011)

A day carries 16–29 orders per delivery platform plus a comparable POS volume, drawn from a ~$17.60–$60.80 band, concentrated into a lunch and a dinner window. Exact-cent collisions inside a 30-minute span are not exotic on that distribution; they are expected. Matching an untagged dine-in ticket against a delivery order on that evidence would delete real revenue, and the owner would have no way to notice — the day would simply be lower. The channel tag is what turns the rule from "these numbers are similar" into "the POS asserted this order came from iFood, and here is the iFood order it must be". Without an assertion from one of the two systems, this feature declines to match at all, and says so.

### Why the delivery side wins (FR-013)

The POS ticket knows only a gross amount. The delivery record knows the subtotal, the commission rate, the commission charged, the net payout, and the refund state. Dropping the delivery record would zero that order's commission, and margin would move **up** — a wrong number in the flattering direction, with no flag able to explain it, which is the worst shape this product's errors can take. Keeping the delivery record also keeps the revenue in the platform's own `GrossSalesBySource` bucket, where it belongs, so the platform comparator stays true.

## Carrying the decisions to the day

`platformconnector` emits neutral `DedupDecision` values (kind, date, both order ids, both refs, both amounts, candidates). It does **not** import `internal/reconcile` — the mapping from a connector decision to a reconciliation flag belongs in the layer that already knows both, which is `internal/pipeline`.

`internal/reconcile` gains three constants and one function:

```go
FlagCrossSourceDuplicateRemoved    = "cross_source_duplicate_removed"
FlagCrossSourceDuplicateUnresolved = "cross_source_duplicate_unresolved"
FlagCrossSourceAmountMismatch      = "cross_source_amount_mismatch"

func ComputeDailyReconciliationsWithFlags(delivery, pos, costs, extraFlags map[string][]DiscrepancyFlag) []DailyReconciliation
```

`ComputeDailyReconciliations` becomes a one-line delegate with a nil map — the same shape `RunIngestionPipeline` already took when the delivery overlay landed. `httpapi.humanizeFlagType` renders new snake_case flags without a change, by construction.

`internal/pipeline` generalizes `DeliveryOverlay` into `ConnectorOverlay`:

```go
type ConnectorOverlay struct {
    From, To        time.Time
    DeliveryActive  bool
    Delivery        []ingest.DeliveryRecord
    POSActive       bool
    POS             []ingest.POSRecord
    Decisions       []platformconnector.DedupDecision
}
```

The two `*Active` booleans are load-bearing and not redundant with a nil slice. "I synced the POS and it reported nothing" and "I did not sync the POS" must produce different outcomes: the first clears POS revenue for the range (and the day inherits its existing flags), the second leaves the CSV rows in place. A delivery-only sync must never zero a day's POS revenue — that is spec's US1.4, and the boolean is what makes it structural rather than a caller's discipline.

`RunIngestionPipelineWithDeliveryOverlay` stays, delegating with `DeliveryActive: true, POSActive: false`, so spec 010's tests and any existing caller are untouched.

## Preview and totals

`Proxy.FetchRange` runs the matcher **before** computing `PlatformDayTotals`, so the preview shows what will actually land rather than a pre-dedup figure the commit then contradicts. `PlatformDayTotals` gains `DuplicatesRemoved` and `UnresolvedOverlaps`, populated on the POS row for the day.

## HTTP and frontend

- `POST /api/connectors/sync/preview` and `/sync` gain `duplicates_removed`, `unresolved_overlaps`, and a `dedup` array describing each decision in plain language. The `"simulated": true` marker and the notice text are extended to name the POS.
- `ConnectedPlatformsTab.tsx` picks up the POS row automatically (the list is server-driven), gains a duplicates column in the preview table, and gains a plain-language summary of what deduplication did and what it could not resolve. Copy is updated from "iFood or Just Eat Takeaway" to name all three sources.
- No new endpoint. No new page.

## Testing strategy

| Level | Test | Proves |
|---|---|---|
| `platformconnector` | The POS mock's raw payload is NDJSON with no envelope, pt-BR amounts, zone-less timestamps, `PAID`/`VOID` | FR-001 / SC-005 |
| `platformconnector` | `"1.234,56"` normalizes to `123456` cents; a malformed amount is refused, not best-guessed | The pt-BR trap |
| `platformconnector` | A 19:35 ticket keeps 19:35 in the merchant zone | The zone-less-timestamp trap |
| `platformconnector` | Echoed tickets correspond to that date's actual iFood orders; no JET order is ever echoed | FR-004 / US4.2 |
| `platformconnector` | A referenced duplicate merges, the delivery record survives, the POS ticket does not | FR-009 / FR-013 |
| `platformconnector` | A channel-tagged, reference-less duplicate merges on exact amount + window | FR-010 |
| `platformconnector` | **Two distinct in-house tickets sharing an amount and a nearby time with delivery orders are not merged** | FR-011 — the false-positive bar |
| `platformconnector` | **Two channel-tagged tickets contesting one delivery order: nothing merges, both flagged, order survives** | FR-012 — the ambiguity bar |
| `platformconnector` | Reversing the input order changes no outcome | The symmetry claim |
| `platformconnector` | Every merge produces a decision; no merge is silent | FR-014 |
| `platformconnector` | A resolved pair with differing amounts merges **and** reports both amounts | FR-015 |
| `platformconnector` | A ticket whose reference resolves to nothing is flagged and not amount-matched | Edge case |
| `pipeline` | POS overlay replaces in-range POS rows only when `POSActive`; a delivery-only sync leaves POS untouched | US1.4 |
| `pipeline` | Dedup decisions arrive as discrepancy flags on the right day | FR-014 |
| `reconcile` | `ComputeDailyReconciliations` is byte-identical to the nil-flag delegate | FR-017 |
| `httpapi` | A three-source sync's gross equals raw totals minus removed duplicates | SC-001 |
| frontend | The POS row renders as simulated; the dedup summary renders | FR-020 |

## Documentation

`docs/prd.md` (section 12 extended), `docs/architecture.html` (the connector section only — the third upstream and the matcher; the competitive-positioning narrative is not touched), `docs/openapi.yaml`, `CHANGELOG.md`.
