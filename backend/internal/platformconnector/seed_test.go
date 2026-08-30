package platformconnector

// Tests for seed.go's trading-day condition model — the mechanism that
// makes a simulated week contain real losing days instead of a flat
// healthy line (CHANGELOG 2026-08-30).
//
// Everything here is a PROPERTY of the model, never a golden number for a
// specific date. That is deliberate: connectorSeedSalt's own doc comment
// says changing the generation model changes every simulated number by
// design, so a test pinned to "2026-08-18 produces 22 orders" would have to
// be rewritten every time the model is tuned, and a test that gets
// rewritten to match new output is a test that proves nothing. What must
// hold across any tuning is what is asserted below: the draw is
// reproducible, it is the same for every platform on a given date, the
// weights are a complete partition, the business stays the same size, and
// the days it produces are not all profitable.

import (
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// costSheetCycleCents is the cost side a simulated day is scored against
// in the profitability test below: a five-day repeating pattern standing in
// for a real supplier cost sheet.
//
// Measured, not invented. Reading the live dataset's own
// supplier_cost_sheet.csv (cmd/gendata output, August 2026) gives a mean of
// $1,494.78/day arriving in exactly this shape — invoices land on roughly
// three days in five, nothing at all on the others, and the days they land
// on range from a few hundred dollars to a $6,245 restock. The cycle below
// averages $1,620/day and reproduces that lumpiness deterministically.
//
// The lumpiness is the point, not an incidental detail. Costs are FIXED
// before the day trades — the produce was ordered, delivered and invoiced
// on a schedule that knows nothing about how many covers walk in — so a bad
// day landing on a delivery day is how a real restaurant actually loses
// money. That asymmetry does not exist in cmd/gendata's own regime
// mechanism, where every modelled cost scales with revenue, and it is why
// this model can turn a day negative on demand alone where gendata cannot.
// See seed.go's trading-day condition model.
var costSheetCycleCents = [5]int64{0, 250000, 0, 150000, 410000}

// simulatedDayMarginCents reproduces internal/reconcile's margin formula
// (gross - commissions - refunds - input costs) over the simulated records
// for one date across all three sources.
//
// It re-implements the formula rather than calling internal/reconcile so
// this package's test suite does not depend on the reconciliation engine
// (an import cycle waiting to happen, and a test that would fail for two
// unrelated reasons). The formula is four terms and is stated once, here.
func simulatedDayMarginCents(t *testing.T, date time.Time, dayIndex int) int64 {
	t.Helper()

	gross, commissions, refunds := simulatedDayRevenueCents(date)
	return gross - commissions - refunds - costSheetCycleCents[dayIndex%len(costSheetCycleCents)]
}

// simulatedDayRevenueCents sums what the three simulated sources report for
// one date: gross (excluding refunded orders and voided tickets, exactly as
// internal/reconcile.computeOneDay does), commission, and refunds.
func simulatedDayRevenueCents(date time.Time) (gross, commissions, refunds int64) {
	for _, p := range []Platform{PlatformIFood, PlatformJustEatTakeaway} {
		for _, o := range simulateDay(p, date, platformCommissionBps(p)) {
			commissions += o.CommissionCents
			if o.Refunded {
				refunds += o.SubtotalCents
				continue
			}
			gross += o.SubtotalCents
		}
	}
	for _, tk := range simulatePOSDay(date) {
		if !tk.Voided {
			gross += tk.GrossCents
		}
	}
	return gross, commissions, refunds
}

func aYearOfDates() []time.Time {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, merchantZone)
	out := make([]time.Time, 0, 365)
	for i := 0; i < 365; i++ {
		out = append(out, start.AddDate(0, 0, i))
	}
	return out
}

// TestDayConditions_WeightsSumToTotal proves the table is a complete
// partition of the draw. A table summing to less than the total would make
// conditionForDate fall through its loop; a table summing to more would
// make the last entries unreachable, silently deleting the severe-weather
// day this whole model exists to produce.
func TestDayConditions_WeightsSumToTotal(t *testing.T) {
	total := 0
	for _, c := range dayConditions() {
		require.Positive(t, c.weight, "condition %d carries no weight and can never be drawn", c.kind)
		total += c.weight
	}
	require.Equal(t, conditionWeightTotal, total)
}

// TestDayConditions_LabelDisciplineHolds: every condition except the
// ordinary day states a cause, and the ordinary day states none. A bad day
// with no statable reason is exactly the "sometimes multiply by 0.5 for no
// reason" this model was built to avoid; a label on the ordinary day would
// print a non-finding on five days out of seven.
func TestDayConditions_LabelDisciplineHolds(t *testing.T) {
	for _, c := range dayConditions() {
		if c.kind == conditionOrdinary {
			require.Empty(t, c.Label, "the ordinary day must not carry a note")
			continue
		}
		require.NotEmpty(t, c.Label, "condition %d changes the day's numbers without saying why", c.kind)
		require.Positive(t, c.deliveryMult)
		require.Positive(t, c.inHouseMult)
		require.GreaterOrEqual(t, c.refundRateMult, 1.0)
	}
}

// TestConditionForDate_IsDeterministic is the property connectorSeedSalt's
// doc comment exists to protect, applied to the new draw: the same date
// resolves to the same condition every time, in any order, in any process.
func TestConditionForDate_IsDeterministic(t *testing.T) {
	for _, date := range aYearOfDates() {
		first := conditionForDate(date)
		second := conditionForDate(date)
		require.Equal(t, first.kind, second.kind, "%s changed condition between two calls", date.Format(dateLayout))
		require.Equal(t, first.Label, second.Label)
	}
}

// TestConditionForDate_IsPlatformIndependent is the reason the condition
// has its own seed namespace rather than riding on dayRNG. Weather and a
// broken fryer are properties of the restaurant's day: iFood, Just Eat
// Takeaway and the POS must agree about them, or a sync produces a day
// where one platform was snowed in and the other was not.
func TestConditionForDate_IsPlatformIndependent(t *testing.T) {
	for _, date := range aYearOfDates() {
		note := TradingNoteForDate(date)
		// Every source's day totals read the note through this one
		// function, so agreement is structural. Assert it anyway: the
		// cheapest possible guard against someone "fixing" it later by
		// threading a platform through.
		require.Equal(t, note, TradingNoteForDate(date))
		require.Equal(t, conditionForDate(date).Label, note)
	}
}

// TestConditionForDate_RespectsWeekdayEligibility: a midweek lull never
// lands on a Friday, and a neighbourhood event never lands on a Monday.
// Both would be the model contradicting its own stated cause.
func TestConditionForDate_RespectsWeekdayEligibility(t *testing.T) {
	for _, date := range aYearOfDates() {
		cond := conditionForDate(date)
		wd := merchantDay(date).Weekday()
		switch cond.kind {
		case conditionQuietWeekdayLull:
			require.True(t, midweekOnly(wd), "%s is a %s and cannot carry a midweek lull", date.Format(dateLayout), wd)
		case conditionNeighbourhoodEvent:
			require.True(t, lateWeekOnly(wd), "%s is a %s and cannot carry a late-week event", date.Format(dateLayout), wd)
		}
	}
}

// TestSimulatedDays_ProduceBothProfitableAndLosingDays is the test the
// product owner's report becomes: reconciled against a plausible, FIXED
// cost sheet, a year of simulated days must contain real losses and real
// healthy days, in a proportion that reads as a business rather than as a
// coin flip or a flat line.
//
// The band is wide (5%-35%) on purpose. The exact rate is a tuning
// artifact of seven multipliers against one assumed cost level, and pinning
// it tightly would make this a golden-number test wearing a property test's
// clothes. What is actually being asserted is that neither degenerate case
// — never a loss, or a business that loses money most days — can ship.
func TestSimulatedDays_ProduceBothProfitableAndLosingDays(t *testing.T) {
	dates := aYearOfDates()
	var losing, healthy int
	worst, best := int64(0), int64(0)

	for i, date := range dates {
		margin := simulatedDayMarginCents(t, date, i)
		switch {
		case margin < 0:
			losing++
		case margin > 50000: // a genuinely good day, not a rounding-error profit
			healthy++
		}
		if margin < worst {
			worst = margin
		}
		if margin > best {
			best = margin
		}
	}

	require.Positive(t, losing, "not one day in a simulated year lost money — the flat-positive pattern this model was built to fix")
	require.Positive(t, healthy, "no comfortably profitable days — this restaurant is supposed to be a going concern")

	rate := float64(losing) / float64(len(dates))
	require.Greater(t, rate, 0.05, "losing days are too rare to reach a demo of any realistic length (%d of %d)", losing, len(dates))
	require.Less(t, rate, 0.35, "losing days are so common the restaurant is not a going concern (%d of %d)", losing, len(dates))

	t.Logf("year of simulated days: %d losing, %d healthy, worst %d cents, best %d cents", losing, healthy, worst, best)
}

// TestSimulatedDays_KeepTheBusinessTheSameSize guards the other half of the
// product owner's request: add variance to the OUTCOME, not to the scale.
// The mean daily gross across a simulated year must stay in the same
// neighbourhood the pre-variance model produced (measured at ~$3,900/day),
// or the connector's days stop being comparable with cmd/gendata's
// CSV-ingested ones on the same Close page.
func TestSimulatedDays_KeepTheBusinessTheSameSize(t *testing.T) {
	dates := aYearOfDates()
	daily := make([]int64, 0, len(dates))
	var total int64
	for _, date := range dates {
		gross, _, _ := simulatedDayRevenueCents(date)
		daily = append(daily, gross)
		total += gross
	}
	meanDaily := total / int64(len(dates))

	// Measured against the live dataset (cmd/gendata, 2025-09-01 + 363
	// days): the historical CSV path averages $4,424/day of gross across the
	// same three sources. The connector model averages ~$5,310/day, which is
	// 20% high — but the model it replaced averaged $6,038/day, 36% high, so
	// this change moved the connector's scale TOWARD the dataset's, as a
	// side effect of dayShape's lower mean. The band below is deliberately
	// generous around the measured value: the assertion that matters is
	// "same order of magnitude as the CSV days it sits beside on the Close
	// page", not a golden mean that would need editing on every tune.
	require.InDelta(t, 530000, meanDaily, 90000,
		"mean daily gross is %d cents — the variance model is supposed to change the shape of a week, not the size of the business", meanDaily)
	t.Logf("mean daily gross across a simulated year: %d cents", meanDaily)

	// And the spread has to be real. This is the assertion that would have
	// FAILED before this change: the pre-variance model's tightest and
	// widest days sat at 0.66x and 1.51x of its mean, a band narrow enough
	// that no plausible cost sheet could ever push a day negative. The
	// historical dataset's own days run 0.49x to 2.03x over the same window,
	// which is the shape being matched here.
	sort.Slice(daily, func(i, j int) bool { return daily[i] < daily[j] })
	quietest := float64(daily[0]) / float64(meanDaily)
	busiest := float64(daily[len(daily)-1]) / float64(meanDaily)
	require.Less(t, quietest, 0.50, "the quietest day of a simulated year is %.2fx the mean — no day is bad enough to cost money", quietest)
	require.Greater(t, busiest, 1.70, "the busiest day of a simulated year is only %.2fx the mean — the upside half of the distribution is missing", busiest)
	t.Logf("daily gross spread: quietest %.2fx mean, busiest %.2fx mean", quietest, busiest)
}

// TestShapedOrderCount_NeverProducesAnEmptyDay: a simulated bad day must be
// a small day, never an absent one. Zero delivery records for a date is how
// internal/reconcile says "I have no data" (missing_delivery_source), and a
// storm must not be able to impersonate a gap in the data.
func TestShapedOrderCount_NeverProducesAnEmptyDay(t *testing.T) {
	require.Equal(t, 1, shapedOrderCount(1, 0.01))
	require.Equal(t, 1, shapedOrderCount(0, 1))
	for _, date := range aYearOfDates() {
		require.NotEmpty(t, simulateDay(PlatformIFood, date, ifoodCommissionBps), "%s produced no iFood orders at all", date.Format(dateLayout))
		require.NotEmpty(t, simulatePOSDay(date), "%s produced no POS tickets at all", date.Format(dateLayout))
	}
}

// TestRefundChanceFor_IsCapped proves the cap is reachable and enforced —
// a condition multiplier is a modelling knob, and a knob that can push a
// probability past 1.0 is a knob that can silently refund an entire day.
func TestRefundChanceFor_IsCapped(t *testing.T) {
	require.InDelta(t, refundChancePerOrder, refundChanceFor(ordinaryDay()), 1e-9)
	require.InDelta(t, maxRefundChancePerOrder, refundChanceFor(dayCondition{refundRateMult: 1000}), 1e-9)
	for _, c := range dayConditions() {
		require.LessOrEqual(t, refundChanceFor(c), maxRefundChancePerOrder)
	}
}
