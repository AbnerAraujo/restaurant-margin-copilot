package platformconnector

import (
	"context"
	"time"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
)

// Client is the one shape the rest of the product sees. Everything above
// this interface — the HTTP handlers, the pipeline, internal/reconcile —
// is written against exactly these three methods and has no idea whether
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
	// Platform is the platform this client fetches from.
	Platform() Platform

	// Describe returns the wire-format facts this connector normalizes.
	// It exists so the UI can show a reviewer WHAT is being normalized
	// (spec US3) rather than asking them to take "I built a proxy" on
	// faith, and so the emulation is disclosed in the API surface itself.
	Describe() Description

	// FetchDeliveryRevenue returns every order the platform reports for
	// one calendar date, normalized. A date the platform has no orders
	// for returns an empty slice and a nil error — a closed day is a real
	// answer, not a failure, and internal/reconcile's existing
	// missing_delivery_source flag is what surfaces it. This function
	// never fabricates an order to avoid an empty day.
	FetchDeliveryRevenue(ctx context.Context, date time.Time) ([]ingest.DeliveryRecord, error)
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
