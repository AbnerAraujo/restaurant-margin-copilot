package platformconnector

import (
	"fmt"
	"hash/fnv"
	"io"
	"math/rand"
	"time"
)

// dayShape is the weekday demand curve, as a multiple of an ordinary
// Thursday. It replaces the flat weekendLift this file carried until
// 2026-08-30.
//
// The lift alone made five of every seven days statistically identical,
// which is not what a week in a restaurant looks like and — more to the
// point here — is not what a week of MARGINS looks like: the Monday and
// Tuesday troughs are where a real independent's thin days actually come
// from, and a model without them can only ever produce a flat healthy
// line. Thursday is the 1.00 anchor rather than a computed mean so the
// numbers below can be read directly as "a Monday does ~82% of a
// Thursday's covers".
//
// The mean of these seven is 1.026, deliberately close to the 1.071 the
// old (5 x 1.00 + 2 x 1.25) / 7 produced: this changes the SHAPE of a
// week, not the size of the business (the task's "maintain consistency
// with the historical data mass in terms of amount").
//
// [Assumption] The specific factors are this project's own judgment, in
// the same class as cmd/gendata's monthRegime multipliers and tagged the
// same way. No source here quantifies an independent restaurant's
// day-of-week covers curve precisely; the shape (midweek trough, Friday
// and Saturday peak, a moderate Sunday) is uncontroversial, the exact
// numbers are tuned.
var dayShape = [7]float64{
	time.Sunday:    1.04,
	time.Monday:    0.82,
	time.Tuesday:   0.86,
	time.Wednesday: 0.92,
	time.Thursday:  1.00,
	time.Friday:    1.24,
	time.Saturday:  1.30,
}

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

	// refundChancePerOrder matches cmd/gendata's refundRatePerOrder
	// (research: 1-3% of delivery orders are refunded). Kept because a
	// simulated feed with no refunds at all would never exercise the
	// refund-sign normalization that is this feature's sharpest edge.
	//
	// It is the ORDINARY-day rate. A day whose condition raises it (a
	// storm's cold and late deliveries, a kitchen failure's remakes)
	// multiplies this — see dayCondition.refundRateMult.
	refundChancePerOrder = 0.02

	// maxRefundChancePerOrder caps whatever a condition's multiplier
	// produces. A day where a third of the orders came back would stop
	// being a bad day and start being a business that is not operating —
	// and, more practically, would make the refund-netting arithmetic the
	// dominant term in the day's margin rather than one contributor to it.
	maxRefundChancePerOrder = 0.18

	// campaignChancePerOrder is how often an order carries a marketing
	// campaign reference. See simulatedCampaignCode for why these codes
	// are deliberately non-matching.
	campaignChancePerOrder = 0.15
)

// connectorSeedSalt namespaces this package's seeds so they can never
// coincide with cmd/gendata's stream. Versioned in the string: changing it
// changes every simulated number, which is a deliberate, visible act, not
// something that should happen by accident.
//
// v1 -> v2 on 2026-08-30, with the trading-day condition model below. Every
// simulated order count, amount, time and refund this package produces is
// different from what v1 produced, on purpose, and that is the deliberate,
// visible act this constant exists to record. Nothing outside this package
// is affected: cmd/gendata seeds its own stream from its own constant and
// its output does not move by a cent.
const connectorSeedSalt = "my-business-steward/platform-connector/v2"

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
	return seededRNG(string(p), date)
}

// seededRNG is dayRNG generalized over the namespace being seeded.
//
// There are now three independent draws about one calendar date, and they
// must not share a stream: the day's TRADING CONDITION (a property of the
// date alone — see conditionForDate), each platform's DEMAND for that date,
// and each platform's per-order DETAIL. Reusing one stream for two of them
// would correlate them — the day's order count would be readable off the
// first order's timestamp — and, worse, would make either one's draw order
// a hidden dependency of the other's numbers.
//
// The namespace is hashed with the date exactly the way dayRNG's own
// doc comment describes, so every property that comment argues for
// (order-insensitive, process-insensitive, stable across machines) holds
// for all three.
func seededRNG(namespace string, date time.Time) *rand.Rand {
	h := fnv.New64a()
	// Hash.Write never returns an error (documented on hash.Hash), so
	// there is nothing to handle here.
	_, _ = io.WriteString(h, connectorSeedSalt+"|"+namespace+"|"+date.Format(dateLayout))
	return rand.New(rand.NewSource(int64(h.Sum64())))
}

// --- The trading-day condition model ----------------------------------------
//
// Until 2026-08-30 this file generated every day inside one narrow, always
// healthy band: an order count of minOrdersPerDay + Intn(orderCountSpread),
// a Friday/Saturday lift, and nothing else. Reconciled against any
// plausible cost sheet, EVERY connector-synced day came out comfortably
// profitable — a flat line no restaurant has ever run, and a demo storyline
// in which nothing ever goes wrong for a product whose entire job is to
// tell the owner when something has.
//
// cmd/gendata already solved this problem for the main historical dataset,
// and its solution is the one followed here in spirit: a named, statable
// CAUSE — "January seasonal slump + cold-snap equipment strain", "Regional
// produce shortage — emergency re-sourcing" — rather than an unexplained
// multiplier. See cmd/gendata/main.go's monthlyRegimes and the research
// ledger above it.
//
// Two things are different here, both forced by the shape of this
// generator rather than chosen:
//
//  1. GRANULARITY. gendata plans a whole dataset up front and can afford to
//     tag calendar MONTHS and then pick a fixed COUNT of shock days inside
//     each. A connector fetch is random access — one date, or three, in any
//     order — so there is no "up front" in which to allocate a month's
//     budget. The unit here is therefore the DAY, and the mechanism is a
//     weighted draw seeded from the date itself, which gives the same
//     reproducibility with none of the sequencing.
//
//  2. WHAT A DEMAND DIP CAN DO. gendata's ledger records that a demand dip
//     alone can never flip one of ITS months negative, because every cost
//     it models scales proportionally with revenue, so it pairs each slump
//     with a cost-side shock. That constraint does not hold on this path,
//     and the reason is worth stating because it is the whole mechanism:
//     the connector supplies REVENUE ONLY. The costs a connector-synced day
//     reconciles against come from the supplier cost sheet, which is fixed —
//     the produce was ordered, delivered and invoiced before anyone knew
//     what the day would do. A demand collapse against already-committed
//     input costs is exactly how a real restaurant loses money on a
//     Tuesday, and it is the most honest lever available here. No
//     cost-side fiction is invented on this path; the connector never
//     writes an invoice.
//
// The conditions below are drawn from the DATE alone, never from
// (platform, date). Weather, a street closure, a broken fryer and a
// short-staffed shift are properties of the restaurant's day, so iFood,
// Just Eat Takeaway and the POS have to agree about them. Drawing them
// per-platform would produce a day where iFood was snowed in and Just Eat
// Takeaway was not, which is not a bad day — it is a bug that happens to
// look like one.
//
// [Assumption] Every weight and multiplier below is this project's own
// reasoned judgment, tagged the way cmd/gendata tags its equivalents. What
// IS sourced is the backdrop those judgments were tuned against, and it is
// the same ledger cmd/gendata already carries: Restaurant365's 2026 State
// of the Restaurant Industry survey puts 45% of operators unprofitable
// across 2025, and Toast/VantaInsights put only ~42% of U.S. restaurants
// profitable in 2024 — against which "never a losing day" was never a
// defensible model. The multipliers were then tuned empirically against
// the live dataset's own cost sheet until roughly one day in seven landed
// net-negative, which is checked by test rather than assumed (see
// TestSimulatedDays_ProduceBothProfitableAndLosingDays).

type dayConditionKind int

const (
	conditionOrdinary dayConditionKind = iota
	conditionNeighbourhoodEvent
	conditionQuietWeekdayLull
	conditionAggregatorOutage
	conditionShortStaffedShift
	conditionKitchenEquipmentFailure
	conditionSevereWeather
)

// dayCondition is one trading condition a simulated day can be in.
type dayCondition struct {
	kind dayConditionKind

	// Label is the cause, in the words the owner would use. Empty on an
	// ordinary day, because "nothing in particular happened" is not a
	// finding and a UI that printed it on five days out of seven would
	// train people to stop reading the column.
	//
	// It reaches the API as PlatformDayTotals.TradingNote, so a day whose
	// margin moved has a statable reason attached to it rather than being
	// an unexplained dip — the same discipline cmd/gendata follows when it
	// echoes a regime's label into that month's cost-sheet invoice notes.
	Label string

	// weight is this condition's share of a 1000-part draw.
	weight int

	// eligible gates a condition to the weekdays it makes sense on. A
	// condition drawn on a weekday it is not eligible for collapses to
	// conditionOrdinary rather than being re-rolled: re-rolling would make
	// the result depend on how many draws it took, and the collapse is
	// itself the honest statement ("a Saturday cannot have a midweek
	// lull").
	eligible func(time.Weekday) bool

	// deliveryMult and inHouseMult scale the day's order count on the two
	// sides of the business independently, because most real disruptions
	// hit them differently — an aggregator outage barely touches the
	// dining room, a storm empties the dining room faster than it empties
	// the delivery queue.
	deliveryMult float64
	inHouseMult  float64

	// refundRateMult multiplies refundChancePerOrder (1.0 = unchanged),
	// capped by maxRefundChancePerOrder.
	refundRateMult float64
}

func anyWeekday(time.Weekday) bool { return true }

// midweekOnly is Monday through Thursday — the days a "quiet lull" is a
// real pattern rather than an excuse.
func midweekOnly(wd time.Weekday) bool {
	return wd == time.Monday || wd == time.Tuesday || wd == time.Wednesday || wd == time.Thursday
}

// lateWeekOnly is Thursday through Sunday, when the neighbourhood events
// this models (a match, a street fair, a long weekend) actually happen.
func lateWeekOnly(wd time.Weekday) bool {
	return wd == time.Thursday || wd == time.Friday || wd == time.Saturday || wd == time.Sunday
}

// ordinaryDay is the no-event condition, and the fallback for a draw that
// lands on a condition the weekday is not eligible for.
func ordinaryDay() dayCondition {
	return dayCondition{kind: conditionOrdinary, deliveryMult: 1, inHouseMult: 1, refundRateMult: 1}
}

// dayConditions is the table, weights summing to conditionWeightTotal.
//
// Read the multipliers against the arithmetic they have to beat. A normal
// day at this file's scale reconciles to roughly $3,900 of gross across the
// three sources against roughly $2,500 of cost-sheet input costs and $300
// of commission — so a day only turns negative once combined demand falls
// to about 70% of normal. That is why the genuinely bad conditions sit near
// 0.5 rather than at a token 0.9: anything milder produces a thinner
// profit, not a loss, and the product owner asked for real losing days.
func dayConditions() []dayCondition {
	return []dayCondition{
		{
			kind:     conditionOrdinary,
			weight:   520,
			eligible: anyWeekday,
			// No label: see dayCondition.Label.
			deliveryMult: 1.00, inHouseMult: 1.00, refundRateMult: 1.0,
		},
		{
			kind:     conditionQuietWeekdayLull,
			weight:   155,
			eligible: midweekOnly,
			Label:    "Quiet midweek trade — no local draw, walk-ins well below normal",
			// The commonest bad day, and the least dramatic: nothing broke,
			// nobody came. Lands thin-to-slightly-negative on its own and
			// clearly negative when it falls on a Monday or Tuesday, which
			// is precisely how this compounds in a real week.
			deliveryMult: 0.78, inHouseMult: 0.72, refundRateMult: 1.0,
		},
		{
			kind:     conditionNeighbourhoodEvent,
			weight:   90,
			eligible: lateWeekOnly,
			Label:    "Neighbourhood event — unusually heavy local trade",
			// Upside variance is not decoration. A distribution with only
			// bad days is as flat a line as one with only good days, and
			// the week-over-week delta the product reports is only
			// interesting if it can move in both directions.
			deliveryMult: 1.30, inHouseMult: 1.42, refundRateMult: 1.0,
		},
		{
			kind:     conditionAggregatorOutage,
			weight:   60,
			eligible: anyWeekday,
			Label:    "Delivery apps down for part of service — orders fell back to the dining room",
			// The one condition that is not a bad day for the business, only
			// for one channel. It exists because this is a CONNECTOR demo:
			// a partner API having a bad afternoon while the restaurant has
			// a fine one is the single most connector-specific thing that
			// can go wrong, and it is worth being able to show the product
			// telling those two apart.
			deliveryMult: 0.35, inHouseMult: 1.05, refundRateMult: 1.0,
		},
		{
			kind:         conditionShortStaffedShift,
			weight:       70,
			eligible:     anyWeekday,
			Label:        "Short-staffed shift — online ordering paused through the dinner peak",
			deliveryMult: 0.64, inHouseMult: 0.70, refundRateMult: 2.5,
		},
		{
			kind:     conditionKitchenEquipmentFailure,
			weight:   60,
			eligible: anyWeekday,
			Label:    "Kitchen equipment failure — limited menu for most of the day",
			// Deliberately the same class of cause cmd/gendata's
			// regimeHeatWave and regimeSeasonalSlump already use ("cold-snap
			// equipment strain", "walk-in cooler compressor failure"), so the
			// two datasets tell one story about this restaurant rather than
			// two unrelated ones.
			deliveryMult: 0.58, inHouseMult: 0.52, refundRateMult: 4.0,
		},
		{
			kind:     conditionSevereWeather,
			weight:   45,
			eligible: anyWeekday,
			Label:    "Severe weather — couriers scarce and almost no walk-in trade",
			// Delivery falls by less than the dining room, and that ordering
			// is deliberate: in bad weather delivery DEMAND rises while
			// courier supply collapses, so what actually lands is a
			// moderate fall, not the near-total one the empty dining room
			// sees. Refunds spike with it — cold food, long waits,
			// cancellations.
			deliveryMult: 0.55, inHouseMult: 0.38, refundRateMult: 6.0,
		},
	}
}

// conditionWeightTotal is the denominator dayConditions' weights are shares
// of. Asserted against the table itself by test rather than kept in step by
// hand — a table whose weights no longer sum to this would silently make
// the last condition unreachable.
const conditionWeightTotal = 1000

// conditionForDate returns the trading condition for one calendar date.
// Deterministic, platform-independent, and random-access — see
// seededRNG and the model's doc comment above.
func conditionForDate(date time.Time) dayCondition {
	day := merchantDay(date)
	roll := seededRNG("trading-condition", date).Intn(conditionWeightTotal)
	for _, c := range dayConditions() {
		if roll < c.weight {
			if !c.eligible(day.Weekday()) {
				return ordinaryDay()
			}
			return c
		}
		roll -= c.weight
	}
	// Unreachable while the weights sum to conditionWeightTotal (proved by
	// TestDayConditions_WeightsSumToTotal). Returning the ordinary day
	// rather than panicking keeps a mis-summed table from taking a demo
	// down over a modelling constant.
	return ordinaryDay()
}

// TradingNoteForDate is the statable cause for one simulated date, or "" on
// an ordinary day. Exported so internal/httpapi can put a day's reason next
// to the day's numbers without this package having to know what an HTTP
// response looks like.
func TradingNoteForDate(date time.Time) string {
	return conditionForDate(date).Label
}

// merchantDay is the calendar date, at midnight in the restaurant's own
// zone. Every weekday decision in this file goes through it so a date
// parsed at UTC midnight and the same date built in merchantZone can never
// disagree about which day of the week it was.
func merchantDay(date time.Time) time.Time {
	return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, merchantZone)
}

// baseDemand is one platform's UNCONDITIONED order count for a date: the
// draw this file has always made, before the weekday shape or the day's
// condition touches it.
//
// It is a pure function of (platform, date) with its own seed namespace,
// which is what lets simulatePOSDay size the dining room against the same
// baseline the delivery feed was sized from without either one having to
// reach into the other's already-shaped result.
func baseDemand(p Platform, date time.Time) int {
	return minOrdersPerDay + seededRNG("demand|"+string(p), date).Intn(orderCountSpread)
}

// shapedOrderCount applies a demand multiplier to a base count.
//
// The floor of 1 is a modelling choice worth naming: a full closure (zero
// orders) is a real thing that happens to real restaurants, but on this
// path it would land as internal/reconcile's missing_delivery_source flag —
// "the platform reported nothing" is how this product says "I have no data
// for this day", and a simulated storm must not be able to impersonate a
// gap in the data. A very bad day here is a very small day, never an absent
// one.
func shapedOrderCount(base int, mult float64) int {
	n := int(float64(base)*mult + 0.5)
	if n < 1 {
		return 1
	}
	return n
}

// refundChanceFor is the per-order refund probability on a given day.
func refundChanceFor(cond dayCondition) float64 {
	chance := refundChancePerOrder * cond.refundRateMult
	if chance > maxRefundChancePerOrder {
		return maxRefundChancePerOrder
	}
	return chance
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
//
// The count is the day's base demand put through two multipliers: the
// weekday shape (dayShape) and the day's own trading condition
// (conditionForDate). Neither touches meanTicketCents or the commission
// rates — a bad day at this restaurant is fewer covers, not cheaper food
// or a renegotiated contract — so the dollar scale of the business is
// exactly what it was.
func simulateDay(p Platform, date time.Time, rateBps int64) []simulatedOrder {
	rng := dayRNG(p, date)
	day := merchantDay(date)
	cond := conditionForDate(date)

	count := shapedOrderCount(baseDemand(p, date), dayShape[day.Weekday()]*cond.deliveryMult)
	refundChance := refundChanceFor(cond)

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
		if rng.Float64() < refundChance {
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
	day := merchantDay(date)
	cond := conditionForDate(date)

	echoed := simulateDay(posIntegratedPlatform, date, platformCommissionBps(posIntegratedPlatform))

	// In-house count is chosen so that echoed tickets are the remaining
	// (100 - posInHouseSharePct)% of the day, keeping the mix honest to
	// cmd/gendata's revenue shares regardless of how busy the platform was.
	//
	// It is sized off the platform's UNCONDITIONED baseline, then shaped by
	// the dining room's OWN multipliers, rather than off len(echoed). That
	// distinction is the whole point of splitting deliveryMult from
	// inHouseMult: deriving the dining room from the already-shaped delivery
	// count would force the two channels to move together by construction,
	// and an aggregator outage — apps down, dining room fine — could not
	// exist.
	inHouseBase := baseDemand(posIntegratedPlatform, date) * posInHouseSharePct / (100 - posInHouseSharePct)
	inHouseCount := shapedOrderCount(inHouseBase, dayShape[day.Weekday()]*cond.inHouseMult)

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
