package platformconnector

import (
	"hash/fnv"
	"io"
	"math/rand"
	"time"
)

// --- The simulated day model ------------------------------------------------
//
// Everything below generates the ORDERS a simulated platform reports for a
// given day. It is shared by both mocks on purpose: the two upstreams
// differ in how they SAY things (their wire formats), not in what kind of
// business they describe. Putting the business model here and the format
// differences in the two *_mock.go files keeps the demonstrable claim
// honest — the proxy's work is format normalization, and that is exactly
// what the tests exercise.
//
// Scale constants are lifted directly from cmd/gendata's own tuned values
// rather than invented, so a connector-synced day reconciles into a margin
// that is arithmetically comparable to a CSV-ingested day instead of
// standing out as an obvious outlier the moment it lands next to real
// dataset days on the Close page (spec FR-006).

const (
	// Commission rates, matching cmd/gendata's ifoodCommissionPct and
	// jetCommissionPct exactly — expressed here in basis points because
	// that is what ingest.DeliveryRecord.CommissionRateBps holds
	// (23% -> 2300).
	ifoodCommissionBps = 2300
	jetCommissionBps   = 2000

	// meanTicketCents matches cmd/gendata's meanTicket of $32.00.
	meanTicketCents = 3200

	// Orders per platform per day. cmd/gendata's end-of-curve scale is
	// roughly $124,700/month gross, of which each delivery platform takes
	// 17% — about $700/day per platform, which at a $32 mean ticket is
	// about 22 orders. This band brackets that.
	minOrdersPerDay  = 16
	orderCountSpread = 14

	// weekendLift is the Friday/Saturday bump. A flat order count every
	// day of the week would read as generated the instant anyone looked
	// at a week of it.
	weekendLift = 1.25

	// refundChancePerOrder matches cmd/gendata's refundRatePerOrder
	// (research: 1-3% of delivery orders are refunded). Kept because a
	// simulated feed with no refunds at all would never exercise the
	// refund-sign normalization that is this feature's sharpest edge.
	refundChancePerOrder = 0.02

	// campaignChancePerOrder is how often an order carries a marketing
	// campaign reference. See simulatedCampaignCode for why these codes
	// are deliberately non-matching.
	campaignChancePerOrder = 0.15
)

// connectorSeedSalt namespaces this package's seeds so they can never
// coincide with cmd/gendata's stream. Versioned in the string: changing it
// changes every simulated number, which is a deliberate, visible act, not
// something that should happen by accident.
const connectorSeedSalt = "my-business-steward/platform-connector/v1"

// dayRNG returns the pseudorandom source for one (platform, date) pair.
//
// This project already established that demo data must be re-runnable to
// the same numbers (cmd/gendata: "deterministic — same seed, same dataset,
// every regen") because a hiring evaluator re-running the harness must see
// what the demo showed. This follows that discipline with a different
// mechanism, for a reason worth stating.
//
// cmd/gendata seeds ONE stream (randSeed = 20260815) and consumes it in
// file order, top to bottom. That is correct for generating a whole
// dataset once. It is wrong here, because a connector fetch is random
// access: the owner may sync 2026-08-20 alone, or 08-18..08-20, or the
// same day twice in a row, or two platforms in either order. With a shared
// stream, what a day returned would depend on what had been fetched before
// it — the same date would reconcile to different margins depending on the
// order the owner happened to click in, which is precisely the
// irreproducibility the seed exists to prevent.
//
// Hashing the key into its own seed makes every (platform, date) an
// independent draw: order-insensitive, process-insensitive, and stable
// across machines (FNV-64a is a specified algorithm, not a Go-version
// implementation detail the way map iteration or a global rand source is).
func dayRNG(p Platform, date time.Time) *rand.Rand {
	h := fnv.New64a()
	// Hash.Write never returns an error (documented on hash.Hash), so
	// there is nothing to handle here.
	_, _ = io.WriteString(h, connectorSeedSalt+"|"+string(p)+"|"+date.Format(dateLayout))
	return rand.New(rand.NewSource(int64(h.Sum64())))
}

// simulatedOrder is one order as the simulated business produced it,
// BEFORE either upstream has expressed it in its own wire format. Money is
// always positive here, including on a refunded order: how a refund is
// signed on the wire is a per-platform disagreement (iFood reports it
// positive with a cancellation block, Just Eat Takeaway reports it already
// negative), and resolving that disagreement is the adapters' job, not
// this model's.
type simulatedOrder struct {
	Seq             int
	PlacedAt        time.Time // in merchantZone
	SubtotalCents   int64     // positive
	CommissionCents int64     // positive, = round(Subtotal * rateBps / 10000)
	PayoutCents     int64     // positive, = Subtotal - Commission
	Refunded        bool
	RefundedAt      time.Time // zero unless Refunded
	CampaignCode    string    // empty unless the order carried one
}

// simulateDay produces the platform's orders for one calendar date.
// Deterministic for a given (platform, date) — see dayRNG.
func simulateDay(p Platform, date time.Time, rateBps int64) []simulatedOrder {
	rng := dayRNG(p, date)
	day := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, merchantZone)

	count := minOrdersPerDay + rng.Intn(orderCountSpread)
	if wd := day.Weekday(); wd == time.Friday || wd == time.Saturday {
		count = int(float64(count) * weekendLift)
	}

	orders := make([]simulatedOrder, 0, count)
	for i := 1; i <= count; i++ {
		// A ticket band of roughly $17.60-$60.80 around the $32.00 mean —
		// wide enough that a table of orders reads as real, narrow enough
		// that no single order distorts a day.
		subtotal := int64(float64(meanTicketCents) * (0.55 + rng.Float64()*1.35))
		commission := divRoundHalfUp(subtotal*rateBps, 10000)

		o := simulatedOrder{
			Seq:             i,
			PlacedAt:        placeOrderTime(day, rng),
			SubtotalCents:   subtotal,
			CommissionCents: commission,
			PayoutCents:     subtotal - commission,
		}
		if rng.Float64() < refundChancePerOrder {
			o.Refunded = true
			// Real platform refunds settle days after the order, not the
			// same evening — the dataset's own hand-authored refund row
			// settles seven days later. The refund is still attributed to
			// the ORIGINAL order date, matching the convention
			// reconcile.ComputeDailyReconciliations documents.
			o.RefundedAt = day.AddDate(0, 0, 1+rng.Intn(7))
		}
		if rng.Float64() < campaignChancePerOrder {
			o.CampaignCode = simulatedCampaignCode(p, day)
		}
		orders = append(orders, o)
	}
	return orders
}

// placeOrderTime picks a plausible order time: a lunch window or a dinner
// window, dinner being the busier of the two, matching the dataset's own
// hand-authored opening rows (12:05, 19:35, 13:10, 20:20 ...).
func placeOrderTime(day time.Time, rng *rand.Rand) time.Time {
	if rng.Float64() < 0.38 {
		return day.Add(time.Duration(11*60+rng.Intn(150)) * time.Minute) // 11:00-13:29
	}
	return day.Add(time.Duration(18*60+rng.Intn(210)) * time.Minute) // 18:00-21:29
}

// simulatedCampaignCode deliberately produces a code that matches NO real
// campaign in the promotion ledger, and says so in the code itself.
//
// ingest.DeliveryRecord.CampaignID is what
// reconcile.ComputePromotionRoiRecords joins on to attribute revenue to a
// campaign. Emitting a code that collided with a real campaign would
// silently move attributed revenue — and therefore a campaign's reported
// ROI — based on simulated orders, in a part of the product that never
// tells the user anything about a connector. (In practice the promotion
// pipeline reads its own CSV export and is not fed by this overlay at all,
// so no collision is reachable today. This is belt-and-braces against a
// future wiring change, and costs one string prefix.)
func simulatedCampaignCode(p Platform, day time.Time) string {
	prefix := "IFOOD"
	if p == PlatformJustEatTakeaway {
		prefix = "JET"
	}
	return prefix + "-SIMULATED-" + day.Format("200601")
}

// divRoundHalfUp mirrors money.DivRoundHalfUp. It is duplicated here as an
// unexported helper rather than imported so that the SIMULATED upstreams
// (which stand in for third parties, and which the proxy independently
// cross-checks) do not share an implementation with the code that checks
// them — a shared rounding bug would otherwise agree with itself and the
// contract check in proxy.go would prove nothing.
func divRoundHalfUp(numerator, denominator int64) int64 {
	if denominator == 0 {
		return 0
	}
	neg := (numerator < 0) != (denominator < 0)
	n, d := abs64(numerator), abs64(denominator)
	q := (n + d/2) / d
	if neg {
		return -q
	}
	return q
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
