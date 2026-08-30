package platformconnector

import (
	"fmt"
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

// --- The simulated POS day model --------------------------------------------
//
// The POS's day is NOT an independent draw. Two thirds of it are in-house
// tickets this model invents; the rest are ECHOES of the very orders the
// iFood mock will return for the same date, produced by calling the same
// simulateDay this file already exposes. That is the whole point of the
// mechanism: a duplicate dedup.go finds has to be the same simulated order
// recorded twice, not two generators that happened to agree. If the POS
// invented its own delivery-looking tickets, the matcher would be scored
// against a fiction arranged to be solvable.
//
// Every constant below is a MODELLING CHOICE, not a measurement, and each
// says so. A reviewer should be able to disagree with any of them.

const (
	// posIntegratedPlatform is the one aggregator this restaurant's POS is
	// integrated with. iFood orders are pushed into the POS and become
	// tickets; Just Eat Takeaway orders are not.
	//
	// A choice, and the most consequential one in this file. Restaurants
	// commonly integrate the aggregator they do most volume with and leave
	// the other on its own tablet, so this is a realistic configuration —
	// but it is also the most USEFUL one to build against, because it puts
	// a control group inside every single fetch: JET orders that must never
	// be matched, sitting beside iFood orders that must be. A mock where
	// both platforms were integrated would let a matcher that over-matches
	// pass every test.
	posIntegratedPlatform = PlatformIFood

	// posInHouseSharePct is how much of a day's POS ticket COUNT is
	// genuine in-house business (dine-in and counter) with no delivery
	// counterpart at all.
	//
	// Derived rather than picked: cmd/gendata models posShare 0.66 against
	// ifoodShare 0.17, so in-house revenue is roughly four times one
	// platform's. At a comparable ticket size that is about 80% of POS
	// tickets being in-house once the echoed iFood orders are added in.
	// Rounded to 78 so the number does not read as a suspiciously tidy 80.
	//
	// It matters that this is a large majority. spec 012 SC-002 requires
	// ZERO in-house tickets to be removed by the dedup pass over the whole
	// dataset — a rule that over-matches has hundreds of chances per week
	// to prove it.
	posInHouseSharePct = 78

	// posRefPresentPct is how often an echoed ticket carries the
	// platform's own order reference.
	//
	// A choice, and an admitted one. Real POS/aggregator integrations do
	// record the partner order id — that is the mechanism by which the
	// order reached the POS at all. But assuming it is ALWAYS there would
	// make dedup.go's second tier (channel + exact amount + time window)
	// decoration that never runs. The missing quarter stands for the
	// ordinary ways a reference goes missing: a ticket re-fired after a
	// printer failure, a manual re-entry by staff, an older integration
	// build. Set this to 100 and the harder half of the matcher stops
	// being exercised, which is exactly why it is not 100.
	posRefPresentPct = 75

	// posCampaignDiscountBps is the platform-funded promotion the POS never
	// sees.
	//
	// When an order carried a campaign code, the platform charged the
	// customer a discounted subtotal while the POS rang the full menu
	// price. Modelled as a flat 10% so the two sides of a confirmed match
	// disagree on amount for a REAL reason — which is what gives spec 012
	// FR-015's amount-mismatch flag a genuine cause instead of a
	// manufactured one, and what gives the amount-and-time tier an honest
	// "no counterpart found" case it must disclose rather than force.
	posCampaignDiscountBps = 1000

	// posVoidChancePerTicket is how often an in-house ticket is voided
	// (a mis-ring, a walked table). It exercises internal/reconcile's
	// existing pos_non_completed_row_excluded flag on the connector path
	// exactly as the CSV path already does. Echoed tickets are never
	// voided: a void on one side of a cross-source pair is a genuinely
	// harder reconciliation question than this feature claims to solve,
	// and inventing it would be simulating a problem the matcher does not
	// address.
	posVoidChancePerTicket = 0.015

	// posTicketLagMaxMinutes is how far a POS ticket time can trail the
	// platform's order-placed time on an echoed order: the aggregator's
	// injection into the POS, plus the moment before someone accepts it.
	//
	// Deliberately well inside dedup.go's own matchWindowMinutes. The mock
	// is not trying to stress the window's edge — a mock that generated
	// lags straddling the threshold would make the matcher's results
	// depend on the threshold's exact value, which is a modelling
	// constant, not a fact. The window's behaviour at its boundary is
	// proven by unit test with hand-built records instead.
	posTicketLagMaxMinutes = 9
)

// posInHouseChannels are the service types a genuine in-house ticket
// carries. Neither value names a delivery platform, which is what keeps
// them permanently ineligible for matching (spec 012 FR-011).
var posInHouseChannels = []string{"dine_in", "counter"}

// simulatedTicket is one POS ticket as the simulated terminal recorded it,
// BEFORE the mock has expressed it in its own wire format.
type simulatedTicket struct {
	Seq        int
	PlacedAt   time.Time // in merchantZone
	GrossCents int64     // positive, always
	Channel    string    // "dine_in", "counter", or a delivery Platform key
	Payment    string
	Voided     bool

	// EchoOf, when non-empty, is the delivery platform whose order this
	// ticket is a second recording of.
	EchoOf Platform
	// PartnerOrderRef is the platform's own order id, or "" when the
	// integration did not record it on this ticket.
	PartnerOrderRef string
}

// simulatePOSDay produces the terminal's tickets for one calendar date.
// Deterministic for a given date — see dayRNG.
//
// Order of construction matters and is fixed: in-house tickets first, then
// the echoed delivery orders in the delivery feed's own sequence, then a
// single deterministic interleave by ticket time. Without the final sort
// the echoes would all sit at the end of the day's feed, which no real
// terminal would produce and which would let a matcher accidentally
// depend on position.
func simulatePOSDay(date time.Time) []simulatedTicket {
	rng := dayRNG(PlatformPOS, date)
	day := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, merchantZone)

	echoed := simulateDay(posIntegratedPlatform, date, platformCommissionBps(posIntegratedPlatform))

	// In-house count is chosen so that echoed tickets are the remaining
	// (100 - posInHouseSharePct)% of the day, keeping the mix honest to
	// cmd/gendata's revenue shares regardless of how busy the platform was.
	inHouseCount := len(echoed) * posInHouseSharePct / (100 - posInHouseSharePct)

	tickets := make([]simulatedTicket, 0, inHouseCount+len(echoed))

	for i := 0; i < inHouseCount; i++ {
		t := simulatedTicket{
			PlacedAt:   placeOrderTime(day, rng),
			GrossCents: int64(float64(meanTicketCents) * (0.55 + rng.Float64()*1.35)),
			Channel:    posInHouseChannels[rng.Intn(len(posInHouseChannels))],
			Payment:    simulatedPayment(rng),
		}
		if rng.Float64() < posVoidChancePerTicket {
			t.Voided = true
		}
		tickets = append(tickets, t)
	}

	for _, o := range echoed {
		// The POS records the MENU price. When the platform ran a
		// campaign, the customer paid less on the platform than the menu
		// says, so the two sides legitimately disagree — see
		// posCampaignDiscountBps.
		gross := o.SubtotalCents
		if o.CampaignCode != "" {
			gross = divRoundHalfUp(o.SubtotalCents*10000, 10000-posCampaignDiscountBps)
		}

		t := simulatedTicket{
			PlacedAt:   o.PlacedAt.Add(time.Duration(1+rng.Intn(posTicketLagMaxMinutes)) * time.Minute),
			GrossCents: gross,
			Channel:    string(posIntegratedPlatform),
			Payment:    "delivery_partner",
			EchoOf:     posIntegratedPlatform,
		}
		if rng.Intn(100) < posRefPresentPct {
			t.PartnerOrderRef = deliveryOrderID(posIntegratedPlatform, date, o.Seq)
		}
		tickets = append(tickets, t)
	}

	// A stable interleave. sortByPlacedAt is a plain insertion by minute
	// with the construction order as the tie-break, so the result is
	// identical on every run and on every machine.
	sortTicketsByPlacedAt(tickets)
	for i := range tickets {
		tickets[i].Seq = i + 1
	}
	return tickets
}

// sortTicketsByPlacedAt orders tickets by their placed time, keeping the
// original relative order of ties (a stable sort). Written out rather than
// reaching for sort.SliceStable so the tie-break rule — construction
// order, which is in-house-then-echo — is visible at the call site's own
// level of detail, because it is part of what makes the feed byte-stable.
func sortTicketsByPlacedAt(tickets []simulatedTicket) {
	for i := 1; i < len(tickets); i++ {
		cur := tickets[i]
		j := i - 1
		for j >= 0 && tickets[j].PlacedAt.After(cur.PlacedAt) {
			tickets[j+1] = tickets[j]
			j--
		}
		tickets[j+1] = cur
	}
}

// simulatedPayment picks a tender type. Cosmetic — nothing reconciles on
// it — but a POS export with one payment method for every ticket reads as
// generated the moment anyone opens it.
func simulatedPayment(rng *rand.Rand) string {
	switch rng.Intn(10) {
	case 0, 1:
		return "cash"
	case 2:
		return "pix"
	default:
		return "card"
	}
}

// platformCommissionBps is the rate each delivery platform charges. It
// exists so simulatePOSDay can call simulateDay for the platform it
// echoes without the two mocks' rate constants having to be threaded
// through the POS model.
func platformCommissionBps(p Platform) int64 {
	if p == PlatformJustEatTakeaway {
		return jetCommissionBps
	}
	return ifoodCommissionBps
}

// deliveryOrderID reconstructs the order id a delivery mock will emit for
// a given (platform, date, sequence).
//
// This is the single most load-bearing coupling in the simulation, and it
// is a coupling on purpose. The POS's cross-reference has to name the
// order the delivery adapter will independently return in the same fetch,
// or the "duplicate" would not be one. Both mocks now derive their ids
// from here rather than each formatting their own string, so the two can
// never drift apart silently — if this format changes, every echoed
// reference changes with it, and dedup.go keeps matching.
func deliveryOrderID(p Platform, date time.Time, seq int) string {
	prefix := "IFOOD-SIM"
	if p == PlatformJustEatTakeaway {
		prefix = "JET-SIM"
	}
	return fmt.Sprintf("%s-%s-%04d", prefix, date.Format("20060102"), seq)
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
