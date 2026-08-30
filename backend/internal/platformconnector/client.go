package platformconnector

import (
	"context"
	"time"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
)

// Connector is the half every upstream in this package shares: it knows
// which source it is, and it can describe its own wire format for
// display. Client and POSClient each add exactly one fetch method on top,
// because that is the only thing about them that genuinely differs. It is
// exported so a caller outside this package can hold a mixed list of
// sources — which is exactly what NewProxy takes.
type Connector interface {
	// Platform is the source this client fetches from.
	Platform() Platform

	// Describe returns the wire-format facts this connector normalizes.
	// It exists so the UI can show a reviewer WHAT is being normalized
	// (spec 010 US3) rather than asking them to take "I built a proxy" on
	// faith, and so the emulation is disclosed in the API surface itself.
	Describe() Description
}

// Client is the shape every DELIVERY-platform upstream implements.
// Everything above this interface — the HTTP handlers, the pipeline,
// internal/reconcile — is written against exactly these three methods
// and has no idea whether
// the platform behind them pages by cursor or by page number, reports
// money as a decimal string or as minor units, or calls a refund
// "CANCELLED" or "REFUNDED".
//
// FetchDeliveryRevenue returns records that are ALREADY normalized.
// Normalization lives in each implementation, not in the Proxy, because
// only the implementation knows its own wire shape — a central normalizer
// would need a type switch over every platform's payload, which is exactly
// the coupling this interface exists to remove. What the Proxy owns
// instead is verification that the contract below was honored (see
// proxy.go's checkContract), so a broken adapter fails at the boundary
// rather than downstream in a margin figure.
//
// # The output contract
//
// Every returned record MUST satisfy all of the following. These are not
// suggestions; proxy.go enforces them on every fetch, including for the
// two mocks in this package.
//
//  1. Platform is Platform.DisplayName() for the fetched platform — see
//     that method's doc for what a typo here would silently do.
//  2. OrderDate is midnight in the merchant's own zone for the requested
//     calendar date, and equals the date that was requested.
//  3. SubtotalCents, CommissionCents and NetPayoutCents are integer cents
//     and internally consistent:
//     CommissionCents == round(SubtotalCents * CommissionRateBps / 10000)
//     within one cent (the same tolerance and the same rounding function
//     internal/reconcile.recomputeCommissionCents applies), and
//     NetPayoutCents == SubtotalCents - CommissionCents exactly.
//  4. A refunded order carries Status "refunded", a non-nil RefundDate,
//     and NON-POSITIVE money fields — this repository's canonical
//     delivery-export convention (see the refund row in
//     cmd/gendata/opening/delivery_platform_export.csv, and
//     reconcile.computeOneDay's abs64 of the subtotal). A completed order
//     carries Status "completed", a nil RefundDate, and positive money.
//  5. Ref.File carries the simulated:// scheme while these upstreams are
//     emulated, and Ref.Row is the record's 1-based position within the
//     page it arrived in.
//
// Rule 4 is the one with teeth. The two mocks in this package disagree
// about the sign of a refund on the wire — iFood reports a cancelled order
// with positive amounts plus a cancellation block, Just Eat Takeaway
// reports it with already-negative minor units — so an adapter that forgot
// to normalize would hand reconcile a positive "refund", which it would
// count as revenue. The day's margin would go UP because of a refund, with
// no flag anywhere. That is the exact class of confidently-wrong number
// this product exists to prevent, which is why it is checked rather than
// trusted.
type Client interface {
	Connector

	// FetchDeliveryRevenue returns every order the platform reports for
	// one calendar date, normalized. A date the platform has no orders
	// for returns an empty slice and a nil error — a closed day is a real
	// answer, not a failure, and internal/reconcile's existing
	// missing_delivery_source flag is what surfaces it. This function
	// never fabricates an order to avoid an empty day.
	FetchDeliveryRevenue(ctx context.Context, date time.Time) ([]ingest.DeliveryRecord, error)
}

// POSClient is Client's peer for the in-house point-of-sale terminal.
//
// # Why this is a separate interface and not just another Client
//
// The obvious move is to have the POS mock implement Client and return
// ingest.DeliveryRecord values with a zero commission. It was rejected,
// and the reason is worth stating here rather than in a plan document,
// because the next person to look at this file will have the same idea.
//
// reconcile.computeOneDay sums commissionsBySource[src] over EVERY
// delivery record it is given. A POS ticket wearing a DeliveryRecord
// would therefore add a commissionsBySource["pos"] = 0 entry to the day.
// That entry is not cosmetic: internal/reconcile/types.go documents, on
// both CommissionsBySource and RefundsBySource, that "pos" never appears
// in them — and specs/003's compare_platform_economics reads exactly
// those maps to rank platforms by economics. The POS would show up in
// the platform comparator as a delivery platform charging 0%
// commission: a new, wrong answer in a corner of the product that never
// mentions connectors. Forcing the type would also discard Channel and
// PaymentMethod, which is precisely the information dedup.go needs.
//
// So: two interfaces, one shared half (Connector), and a Proxy that
// holds both registries. The cost is one more interface. The alternative
// cost was a silently wrong platform comparison.
type POSClient interface {
	Connector

	// FetchPOSOrders returns every ticket the terminal reports for one
	// calendar date, normalized. A date with no tickets returns an empty
	// slice and a nil error — a closed day is a real answer, not a
	// failure. This function never fabricates a ticket to avoid an empty
	// day.
	//
	// # The output contract
	//
	//  1. Record.OrderDate is midnight in the merchant's own zone for the
	//     requested calendar date, and equals the date that was requested.
	//  2. Record.GrossCents is integer cents and positive.
	//  3. Record.Status is "completed" or a non-completed status
	//     reconcile.computeOneDay will exclude with its existing
	//     pos_non_completed_row_excluded flag. This connector models no
	//     POS refund, so there is no negative-amount convention here the
	//     way there is for delivery records.
	//  4. Record.Ref.File carries the simulated:// scheme while this
	//     upstream is emulated, and Record.Ref.Row is the ticket's 1-based
	//     position in the response.
	//  5. DeliveryPlatform is either "" or a Platform for which
	//     IsDelivery() is true. A ticket may not claim to have arrived
	//     "through the POS".
	//  6. PlacedAt is in the merchant's own zone and falls on OrderDate.
	//     Rule 6 has teeth: PlacedAt is the input to dedup.go's time
	//     window, so an adapter that read a zone-less timestamp as UTC
	//     would shift every ticket by the merchant's offset, no matching
	//     would ever fire, and duplicate revenue would flow through with
	//     nothing to flag it. proxy.go's checkPOSContract enforces it.
	FetchPOSOrders(ctx context.Context, date time.Time) ([]POSOrder, error)
}

// POSOrder is one normalized POS ticket plus the two cross-source
// matching signals ingest.POSRecord has no field for.
//
// Those two fields deliberately do NOT live on ingest.POSRecord. The CSV
// POS export has no partner-reference column and never will, so adding
// them to the shared type would park a connector-only concern in a type
// the whole product depends on, permanently empty on the path that
// produces most of its rows. They stay here, inside the connector, and
// internal/ingest keeps a zero-line diff from this feature.
type POSOrder struct {
	// Record is what leaves this package: the exact type
	// ingest.ParsePOSExport produces from a CSV row.
	Record ingest.POSRecord

	// DeliveryPlatform is the delivery platform the POS itself says this
	// ticket arrived through, or "" for an in-house order (dine-in,
	// counter). It is an assertion made by the POS, not an inference —
	// which is what makes it usable as a matching precondition. dedup.go
	// will not consider a ticket at all unless this is set (spec 012
	// FR-011).
	DeliveryPlatform Platform

	// PartnerOrderRef is the delivery platform's own order id as the POS
	// recorded it, or "" when the integration did not populate it. When
	// present and resolvable it is treated as identity, outranking every
	// other signal (FR-009).
	PartnerOrderRef string

	// PlacedAt is the ticket time in the merchant's own zone. Carried
	// separately from Record.OrderTime (an "HH:MM" string) because it is
	// arithmetic input, and re-parsing a display string to do arithmetic
	// on it is how a formatting change becomes a matching bug.
	PlacedAt time.Time
}

// Description is a plain-language summary of one platform connector, for
// display. Every field is a fact about the wire format the adapter
// normalizes, not marketing copy.
type Description struct {
	Platform Platform `json:"platform"`
	Name     string   `json:"name"`

	// Simulated is always true for the connectors in this package, and is
	// a field rather than a constant precisely so that a future real
	// client sets it to false and every consumer's disclosure updates
	// itself instead of needing to be found and edited.
	Simulated bool `json:"simulated"`

	// WireFormat is a one-phrase description of the upstream's payload,
	// e.g. "page-numbered JSON, decimal-string amounts, RFC 3339
	// timestamps". Shown in the UI so the heterogeneity the proxy
	// reconciles is visible in the product, not only in the source tree.
	WireFormat string `json:"wire_format"`

	// CommissionRatePct is the rate this platform charges, as a decimal
	// string ("23.00"). Rendered, never used in arithmetic — the
	// authoritative per-order rate travels on each record.
	CommissionRatePct string `json:"commission_rate_pct"`

	// Endpoint is the simulated endpoint records are attributed to. It
	// begins simulated:// on purpose; see provenanceRef.
	Endpoint string `json:"endpoint"`
}
