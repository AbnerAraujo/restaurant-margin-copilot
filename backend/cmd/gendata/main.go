// Command gendata generates a realistic 2-year (730-day) synthetic dataset
// for My Business Steward's "live" exploration database
// (backend/data/live/), replacing its current contents — which today are
// just a copy of the small, deliberately-messy 14-day evaluation fixture
// (backend/fixtures/), untouched by this tool and unaffected by it.
//
// The growth story: gross revenue grows on an S-curve (faster in the first
// year, decelerating in the second — a real small-restaurant ramp, not
// flat compound growth) from a modest single-location starting point to a
// scale that averages roughly $40,000/month, across the full 24 months, in
// this product's own margin metric (gross sales minus commissions minus
// refunds minus input costs — NOT full net profit after labor/rent/
// overhead, which this product never computes at all, and NOT a literal
// reading of "3-9% net margin on revenue" either: at that ratio, $40k/month
// would imply $450k-$1.3M/month of revenue, absurd for one location. The
// $40,000/month figure is the user's own stated target for THIS metric,
// not derived from restaurant-industry margin research — that research
// (see the ledger above buildMonthRegimes, below) informs the SHAPE of the
// dataset's volatility (how often, and why, a month misses), not this
// dollar figure.
//
// 2026-08-29: revised so a believable minority of the 24 months land net-
// negative overall, driven by real, sustained causes (a January seasonal
// slump, a supplier-shortage cost-of-goods spike, a refund/discrepancy
// cluster) rather than the previous model's only mechanism — an
// independent per-day cost-shock chance (lossyDayChance) — which reliably
// produced net-loss DAYS but let almost every MONTH average out positive,
// since ~1-2 bad days a month get absorbed by ~28 good ones. See
// buildMonthRegimes for the new month-level mechanism and its
// Sourced/Assumption ledger, and docs/product-strategy.md's 2026-08-29
// entry for the fuller research writeup.
//
// Output: the same four CSVs internal/ingest already parses
// (delivery_platform_export.csv, pos_export.csv, supplier_cost_sheet.csv,
// promotion_ad_spend_export.csv), in the exact schema backend/fixtures'
// versions use — this tool was written by reading those files and
// internal/ingest's parser directly, not guessed from column names.
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// --- Tunable parameters, all in one place for auditability -----------------

const (
	numDays = 730

	// Gross revenue growth curve (logistic S-curve), in dollars/month.
	// Raised from the previous $14,000 -> $33,500 range (which targeted
	// $20k/month margin) to reach the new $40k/month margin target once
	// the monthRegime deficits below are averaged in — see
	// printMonthlyVerification's output for the actual realized 24-month
	// average ($40,016/month at these two constants, across 6 regime
	// months), which is what this was empirically tuned against, not
	// solved for algebraically. Re-tuned 2026-08-29 when a 6th regime
	// month (2025-08, see monthlyRegimes) was added.
	startMonthlyGross = 34400.0
	endMonthlyGross   = 124700.0
	// midpointMonth/steepness shape the S-curve: faster growth in year 1,
	// decelerating in year 2, per real small-restaurant ramp patterns
	// (not sustained flat compound growth, which would be implausible).
	midpointMonth = 8.0
	steepness     = 0.55

	// Revenue split across sources — matches this project's existing
	// fixture ratio (POS dominant, delivery a meaningful but smaller
	// channel), not an invented split.
	posShare   = 0.66
	ifoodShare = 0.17
	jetShare   = 0.17

	ifoodCommissionPct = 23.0 // matches existing fixture convention
	jetCommissionPct   = 20.0

	refundRatePerOrder = 0.02 // ~2% of delivery orders refunded (research: 1-3%)

	// COGS as a share of total gross revenue (research: independent
	// full-service restaurants run 28-35%; mid-range chosen).
	cogsShareOfGross = 0.30
	// How the COGS total splits across supplier categories.
	produceShare   = 0.35
	proteinShare   = 0.40
	beverageShare  = 0.15
	packagingShare = 0.10

	// Anomaly/mess injection, spread thin across 2 years (unlike the dense
	// 14-day evaluation fixture) so discrepancy flags stay meaningful
	// rather than constant.
	anomalyDayChance   = 0.035 // ~26 days over 2 years get a genuine spike/dip
	duplicateOrderRate = 0.004 // ~a handful of duplicate rows total

	// Independent cost-side shocks — the mechanism that actually produces a
	// real net-loss day. Before this, COGS was purely `windowGross *
	// cogsShareOfGross`: input costs always scaled WITH that window's own
	// revenue, so margin as a fraction of gross stayed ~constant (~62%,
	// see the package doc) no matter how revenue itself moved. A real
	// restaurant's costs don't just track revenue — a walk-in cooler dies,
	// a produce delivery spoils and has to be re-bought at rush pricing, a
	// supplier hikes prices during a regional shortage — and those land on
	// ONE specific day, independent of that day's own sales. lossyDayChance
	// = 6% is deliberately a real, noticeable rate (~44 days over 2 years,
	// roughly 1-2 a month) rather than a token one-off: common enough that
	// an owner would recognize it as "yeah, that happens sometimes," rare
	// enough that it never threatens the overall growth story being told.
	lossyDayChance = 0.06
	// Sized relative to THAT DAY's own gross (not the multi-day COGS
	// window), so it reliably exceeds a typical day's ~62%-of-gross margin
	// cushion regardless of where in the growth curve the day falls.
	lossyDayShockMin = 0.75
	lossyDayShockMax = 1.40

	promoEveryDays = 30 // roughly one campaign a month
	promoPositiveP = 0.65
	randSeed       = 20260815 // deterministic — same seed, same dataset, every regen
)

// startDate is chosen so the 730-day run ends the day BEFORE
// backend/fixtures' own window begins (2026-08-01) — the synthetic
// history is the two years leading UP TO the evaluation fixture, never
// dates after it. The first generation of this tool got this backwards
// (started the day AFTER the fixture, running through 2028) and produced
// a dataset whose "today" was over a year in the real-world future —
// confusing in any chat answer that narrates a date. Everything here must
// stay in the past relative to both the fixture and the real calendar.
var startDate = time.Date(2024, 8, 1, 0, 0, 0, 0, time.UTC)

// --- monthly regimes -----------------------------------------------------
//
// Research ledger for this block (tagged per this project's established
// Sourced/Assumption/Hypothesis convention — see docs/product-strategy.md
// line 5 and its 2026-08-29 dated entry for the full writeup):
//
//   - [Sourced] Independent-restaurant net margins commonly run 3-9% of
//     revenue, skewing toward the low end for full-service independents
//     (VantaInsights' 2026 restaurant-benchmarks synthesis, drawing on
//     Census CBP/BLS/Fed/SEC-informed industry data). Toast/VantaInsights'
//     2026 figures put only ~42% of U.S. restaurants profitable in 2024;
//     Restaurant365's 2026 State of the Restaurant Industry survey puts 45%
//     of operators UNPROFITABLE in 2025. A large minority of the real
//     industry runs at an annual loss — this fictional restaurant is
//     modeled as one of the healthier operators overall (it averages a
//     positive $40k/month), but "almost never a bad month" was never a
//     realistic reading of that backdrop.
//   - [Sourced] The NPD Group's Seasonality Index for Total Restaurant
//     Traffic: January traffic runs ~6% below the average month and ~11%
//     below the June peak, 2013-2019. Individual, single-location
//     restaurants are reported to see steeper slow-period drops - "as much
//     as 30%" in trade coverage (a softer, less rigorous source than NPD's
//     index, cited here only to bound the plausible range, not as a hard
//     number).
//   - [Sourced] JPMorgan Chase Institute's small-business cash-flow
//     research finds that for restaurants specifically, EXPENSES are more
//     volatile than revenues, unlike most other small-business sectors —
//     a cost-side shock, not a demand dip, is the more common driver of a
//     genuinely bad stretch. That finding directly shapes the mechanism
//     below: because every cost this product's margin metric counts
//     (COGS/commission/refunds - no rent or labor, see the package doc)
//     scales proportionally with revenue, a demand dip ALONE cannot
//     mathematically flip a month's margin negative here — it takes a
//     cost-side shock landing on top of it, exactly as JPMorgan's finding
//     would predict. That's why seasonalSlump below pairs a demand dip
//     with an elevated run of cost-shock days rather than relying on the
//     revenue dip by itself.
//   - [Sourced] Wholesale egg prices ran roughly 70% above year-earlier
//     levels in January 2023 (CPI data via FoodNavigator-USA), driven by
//     avian flu — a real, large, sustained single-category input-cost
//     spike, used here only as a scale/shape reference for how severe a
//     genuine "supplier shortage" event can get, not a literal
//     re-creation of the egg market.
//   - [Sourced, trade-press tier] Multiple commercial-refrigeration/HVAC
//     service sources (Culinary Depot, B&J Refrigeration, RepairPros,
//     rentcoolcubes.com — service-industry trade coverage, not a formal
//     research report, hence the lower confidence tag) converge on the
//     same mechanism: summer heat waves push walk-in cooler compressors to
//     run continuously instead of cycling, and dirty condenser coils plus
//     AC-driven voltage sag on the hottest afternoons make mid-summer the
//     highest-failure-rate season for commercial refrigeration. [Sourced,
//     authoritative] The FDA's food-safety "danger zone" rule: perishable
//     food held between 40-140F becomes unsafe within 2 hours generally,
//     within 1 hour if the ambient temperature is above 90F — the reason a
//     summer compressor failure turns into discarded inventory so much
//     faster than the same failure in a cooler month. Together these
//     support a heat-wave equipment-failure regime for a summer month as a
//     real, findable cause, not an invented one — see 2025-08 below.
//   - [Assumption] Everything about WHICH months carry a regime, how many
//     of a regime month's days get forced into an oversized cost shock,
//     and the exact demand-dip/refund-rate multipliers, is this project's
//     own reasoned judgment, not an independently sourced number — no
//     source available here quantifies "restaurant-months net-loss
//     frequency" or "August cost-shock-day frequency" precisely enough to
//     cite. It was tuned so that: (a) a real, named cause (never an
//     unexplained random dip) drives every regime month, (b) the healthy
//     majority of months still carry the ~$40k/month growth story, and
//     (c) the specific regime months actually land net-negative once
//     generated — checked empirically via printMonthlyVerification, not
//     just assumed from the inputs.
//
// Six of the 24 months carry a regime — a "believable minority" per the
// task brief, not a routine occurrence:
//
//   - 2025-01 and 2026-01 ("seasonalSlump"): the recurring January slump,
//     paired here with a plausible concurrent cost pressure — winter cold
//     snaps are widely reported to stress refrigeration/heating equipment
//     (reusing this file's existing "equipment" costShockCauses entries)
//     — rather than trying to make a pure demand dip carry the whole
//     story, which the proportional-cost math above rules out.
//   - 2025-04 and 2026-04 ("supplierShortage"): a sustained regional
//     protein/produce shortage forcing weeks of emergency same-day
//     re-sourcing at rush pricing (the egg/avian-flu event above is this
//     scenario's real-world shape reference) — a pure cost-side event,
//     demand unaffected, per the JPMorgan finding.
//   - 2025-08 ("heatWave"): added 2026-08-29, after a live-usage report
//     that August specifically — the one full historical August in the
//     live dataset, and so the exact comparator any "this August vs. last
//     August" chat question would use — had never gotten a regime. August
//     sits in the middle of this dataset's summer upswing (a strong month
//     on either side), so a demand-side "slow season" story doesn't fit;
//     a heat-wave-driven walk-in cooler failure does, per the trade-press
//     sources above, and — like supplierShortage — is a pure cost-side
//     event with demand left unaffected, consistent with the JPMorgan
//     expense-volatility finding.
//   - 2025-10 ("refundCluster"): a food-safety complaint wave driving a
//     spike in the refund rate on top of a smaller run of cost-shock days
//     — modeling "a cluster of refunds/discrepancies" as its own distinct
//     cause, per the task brief, rather than folding it into a cost-shock
//     month's story.
type regimeKind int

const (
	regimeSeasonalSlump regimeKind = iota
	regimeSupplierShortage
	regimeRefundCluster
	regimeHeatWave
)

type monthRegime struct {
	kind       regimeKind
	label      string // short human-readable cause, echoed into cost-sheet invoice notes
	demandMult float64
	// shockDayCount days within this calendar month are deterministically
	// forced into an oversized cost-shock (same lossyDayShockMin/Max
	// magnitude range as the ordinary daily mechanic, just applied to a
	// specific, tuned COUNT of days rather than left to a per-day dice
	// roll) — chosen so the month's aggregate margin reliably lands
	// negative regardless of which specific days a plain probability
	// would have picked.
	shockDayCount int
	// causeIdx pins every forced-shock invoice in this regime month to the
	// SAME costShockCauses entry, so the month tells one coherent story
	// ("the cooler kept failing all January") instead of reading as
	// unrelated random one-offs.
	causeIdx int
	// refundRateMult multiplies refundRatePerOrder for delivery orders
	// placed in this month (1.0 = unchanged).
	refundRateMult float64
	// refundNote overrides the generic "Customer dispute" refund note for
	// this month, when non-empty.
	refundNote string
}

func monthlyRegimes() map[string]monthRegime {
	return map[string]monthRegime{
		"2025-01": {kind: regimeSeasonalSlump, label: "January seasonal slump + cold-snap equipment strain", demandMult: 0.80, shockDayCount: 21, causeIdx: 0, refundRateMult: 1.0},
		"2025-04": {kind: regimeSupplierShortage, label: "Regional protein shortage — emergency re-sourcing", demandMult: 1.0, shockDayCount: 19, causeIdx: 2, refundRateMult: 1.0},
		"2025-08": {kind: regimeHeatWave, label: "Summer heat wave — walk-in cooler compressor failure", demandMult: 1.0, shockDayCount: 21, causeIdx: 4, refundRateMult: 1.0},
		"2025-10": {kind: regimeRefundCluster, label: "Food-safety complaint wave", demandMult: 1.0, shockDayCount: 17, causeIdx: 1, refundRateMult: 18.0, refundNote: "Batch of undercooked orders reported; refunds issued after a food-safety review."},
		"2026-01": {kind: regimeSeasonalSlump, label: "January seasonal slump + cold-snap equipment strain", demandMult: 0.80, shockDayCount: 21, causeIdx: 3, refundRateMult: 1.0},
		"2026-04": {kind: regimeSupplierShortage, label: "Regional produce shortage — emergency re-sourcing", demandMult: 1.0, shockDayCount: 19, causeIdx: 1, refundRateMult: 1.0},
	}
}

// forcedShockDays deterministically picks, for each regime month, exactly
// shockDayCount distinct dates within that calendar month to force into an
// oversized cost shock (see monthRegime.shockDayCount) — a random SUBSET
// of days, via rng.Perm, so the CSV doesn't read as a suspiciously uniform
// pattern, but a fixed COUNT, so the regime's aggregate effect on the
// month doesn't depend on how a per-day probability happened to land.
func forcedShockDays(regimes map[string]monthRegime, rng *rand.Rand) (dates map[string]int, err error) {
	dates = make(map[string]int)
	// Iterate in sorted key order, not Go's randomized map iteration order:
	// each month's rng.Perm call consumes a different slice of the SHARED
	// rng stream depending on iteration order, so an unsorted range here
	// would silently break the "same seed, same dataset, every regen"
	// guarantee this file has documented since its first version — a real
	// bug caught by literally re-running gendata twice and diffing the
	// monthly verification output.
	monthKeys := make([]string, 0, len(regimes))
	for k := range regimes {
		monthKeys = append(monthKeys, k)
	}
	slices.Sort(monthKeys)
	for _, monthKey := range monthKeys {
		r := regimes[monthKey]
		monthStart, parseErr := time.Parse("2006-01", monthKey)
		if parseErr != nil {
			return nil, fmt.Errorf("gendata: invalid regime month key %q: %w", monthKey, parseErr)
		}
		daysInMonth := monthStart.AddDate(0, 1, 0).Add(-24 * time.Hour).Day()
		if r.shockDayCount > daysInMonth {
			return nil, fmt.Errorf("gendata: regime %q wants %d shock days but %s only has %d", monthKey, r.shockDayCount, monthKey, daysInMonth)
		}
		perm := rng.Perm(daysInMonth)
		for _, dayIdx := range perm[:r.shockDayCount] {
			d := monthStart.AddDate(0, 0, dayIdx)
			dates[d.Format("2006-01-02")] = r.causeIdx
		}
	}
	return dates, nil
}

func main() {
	outDir := flag.String("out", "data/live", "output directory for the generated CSVs (relative to backend/)")
	flag.Parse()

	rng := rand.New(rand.NewSource(randSeed))

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "gendata:", err)
		os.Exit(1)
	}

	regimes := monthlyRegimes()
	forcedShock, err := forcedShockDays(regimes, rng)
	if err != nil {
		fail(err)
	}

	days := make([]dayPlan, numDays)
	for i := range days {
		date := startDate.AddDate(0, 0, i)
		monthIdx := float64(i) / 30.44
		targetMonthly := logisticGross(monthIdx)
		days[i] = planDay(date, targetMonthly, regimes, forcedShock, rng)
	}

	promos := buildPromotions(days, rng)

	if err := writeDeliveryCSV(filepath.Join(*outDir, "delivery_platform_export.csv"), days, promos, rng); err != nil {
		fail(err)
	}
	if err := writePOSCSV(filepath.Join(*outDir, "pos_export.csv"), days, rng); err != nil {
		fail(err)
	}
	if err := writeCostSheetCSV(filepath.Join(*outDir, "supplier_cost_sheet.csv"), days, rng); err != nil {
		fail(err)
	}
	if err := writePromoCSV(filepath.Join(*outDir, "promotion_ad_spend_export.csv"), promos); err != nil {
		fail(err)
	}

	first, last := days[0], days[len(days)-1]
	fmt.Printf("generated %d days: %s ($%.0f/mo gross) -> %s ($%.0f/mo gross)\n",
		numDays, first.date.Format("2006-01-02"), first.targetMonthly,
		last.date.Format("2006-01-02"), last.targetMonthly)

	printMonthlyVerification(days, regimes)
}

// printMonthlyVerification recomputes each calendar month's approximate
// margin directly from the same dayPlan values the CSV writers used (gross
// minus commission minus refund minus COGS minus cost shocks, at the same
// ratios writeCostSheetCSV and writeDeliveryCSV apply) and prints a
// month-by-month table plus a summary line — so a monthly net-loss claim
// for this dataset is something this tool itself reports, not something
// asserted only in a doc comment. This is an approximation (it doesn't
// replay the exact per-order rounding the CSV writers do), good enough for
// a sanity check; the authoritative numbers come from the app's own
// reconciliation engine against the actually-written CSVs.
func printMonthlyVerification(days []dayPlan, regimes map[string]monthRegime) {
	type monthTotal struct {
		gross, margin float64
		regimeLabel   string
	}
	totals := map[string]*monthTotal{}
	var order []string
	blendedCommissionRate := ifoodShare*ifoodCommissionPct/100 + jetShare*jetCommissionPct/100
	deliveryRefundRate := (ifoodShare + jetShare) * refundRatePerOrder

	for _, d := range days {
		monthKey := d.date.Format("2006-01")
		mt, ok := totals[monthKey]
		if !ok {
			mt = &monthTotal{}
			if r, hasRegime := regimes[monthKey]; hasRegime {
				mt.regimeLabel = r.label
			}
			totals[monthKey] = mt
			order = append(order, monthKey)
		}
		refundMult := 1.0
		if r, hasRegime := regimes[monthKey]; hasRegime {
			refundMult = r.refundRateMult
		}
		commission := d.grossTotal * blendedCommissionRate
		refund := d.grossTotal * deliveryRefundRate * refundMult
		cogs := d.grossTotal * cogsShareOfGross
		margin := d.grossTotal - commission - refund - cogs - d.costShock
		mt.gross += d.grossTotal
		mt.margin += margin
	}

	fmt.Println("\nmonth       gross        margin      regime")
	var sumMargin float64
	lossMonths := 0
	for _, k := range order {
		mt := totals[k]
		sumMargin += mt.margin
		mark := ""
		if mt.margin < 0 {
			lossMonths++
			mark = "  <-- NET LOSS"
		}
		fmt.Printf("%s  $%9.0f  $%9.0f  %s%s\n", k, mt.gross, mt.margin, mt.regimeLabel, mark)
	}
	fmt.Printf("\n%d of %d months net-negative; 24-month average margin $%.0f/month\n",
		lossMonths, len(order), sumMargin/float64(len(order)))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gendata:", err)
	os.Exit(1)
}

// logisticGross returns the target monthly gross revenue at month index m
// (0-based, fractional) on a logistic S-curve from startMonthlyGross to
// endMonthlyGross.
func logisticGross(m float64) float64 {
	span := endMonthlyGross - startMonthlyGross
	frac := 1.0 / (1.0 + math.Exp(-steepness*(m-midpointMonth)))
	// Normalize so m=0 starts at (near) 0 and the far tail approaches 1,
	// rather than the raw sigmoid's own asymptotic offset.
	frac0 := 1.0 / (1.0 + math.Exp(steepness*midpointMonth))
	fracEnd := 1.0 / (1.0 + math.Exp(-steepness*(24-midpointMonth)))
	norm := (frac - frac0) / (fracEnd - frac0)
	return startMonthlyGross + span*norm
}

// weekdayMultiplier gives Fri/Sat/Sun a real but modest lift over
// weekdays — high enough to feel like a restaurant, low enough to mostly
// stay under the 20% trailing-3-day anomaly threshold on its own.
func weekdayMultiplier(d time.Weekday) float64 {
	switch d {
	case time.Friday, time.Saturday:
		return 1.32
	case time.Sunday:
		return 1.18
	case time.Monday:
		return 0.82
	default:
		return 1.0
	}
}

type dayPlan struct {
	date           time.Time
	targetMonthly  float64
	grossTotal     float64
	posGross       float64
	ifoodGross     float64
	jetGross       float64
	anomaly        bool
	costShock      float64 // >0 on a lossyDayChance day OR a forced regime shock day; see writeCostSheetCSV
	regimeCauseIdx int     // >=0 when costShock came from a forced regime day (monthRegime.causeIdx); -1 otherwise, meaning "pick a random cause" (the pre-existing daily mechanic)
	refundRateMult float64 // multiplies refundRatePerOrder for this day's delivery orders; 1.0 outside a refundCluster month
	refundNote     string  // overrides the generic refund note when set (refundCluster months)
}

func planDay(date time.Time, targetMonthly float64, regimes map[string]monthRegime, forcedShock map[string]int, rng *rand.Rand) dayPlan {
	daysInMonth := 30.44
	baseDaily := targetMonthly / daysInMonth
	mult := weekdayMultiplier(date.Weekday())
	noise := 1.0 + (rng.Float64()-0.5)*0.16 // +/-8% day-to-day noise

	anomaly := rng.Float64() < anomalyDayChance
	if anomaly {
		if rng.Float64() < 0.5 {
			noise *= 1.55 // a genuinely busy day (private event, viral post)
		} else {
			noise *= 0.55 // a genuinely slow day (bad weather, local event conflict)
		}
	}

	monthKey := date.Format("2006-01")
	demandMult, refundRateMult, refundNote := 1.0, 1.0, ""
	if r, ok := regimes[monthKey]; ok {
		demandMult = r.demandMult
		refundRateMult = r.refundRateMult
		refundNote = r.refundNote
	}

	gross := baseDaily * mult * noise * demandMult

	var costShock float64
	regimeCauseIdx := -1
	dateKey := date.Format("2006-01-02")
	if causeIdx, forced := forcedShock[dateKey]; forced {
		costShock = gross * (lossyDayShockMin + rng.Float64()*(lossyDayShockMax-lossyDayShockMin))
		regimeCauseIdx = causeIdx
	} else if rng.Float64() < lossyDayChance {
		costShock = gross * (lossyDayShockMin + rng.Float64()*(lossyDayShockMax-lossyDayShockMin))
	}

	return dayPlan{
		date:           date,
		targetMonthly:  targetMonthly,
		grossTotal:     gross,
		posGross:       gross * posShare,
		ifoodGross:     gross * ifoodShare,
		jetGross:       gross * jetShare,
		anomaly:        anomaly,
		costShock:      costShock,
		regimeCauseIdx: regimeCauseIdx,
		refundRateMult: refundRateMult,
		refundNote:     refundNote,
	}
}

// --- order generation --------------------------------------------------

// splitIntoOrders breaks a day's gross for one source into individual
// orders around a realistic mean ticket size, so the CSV reads as real
// transactions rather than one lump sum per day.
func splitIntoOrders(gross float64, meanTicket float64, rng *rand.Rand) []float64 {
	if gross <= 0 {
		return nil
	}
	var amounts []float64
	remaining := gross
	for remaining > meanTicket*0.4 {
		size := meanTicket * (0.55 + rng.Float64()*0.9)
		if size > remaining {
			size = remaining
		}
		amounts = append(amounts, math.Round(size*100)/100)
		remaining -= size
	}
	if remaining > 1.0 {
		amounts = append(amounts, math.Round(remaining*100)/100)
	}
	return amounts
}

func randomTime(rng *rand.Rand) string {
	// Lunch (11:30-14:00) and dinner (18:30-22:00) service windows.
	if rng.Float64() < 0.42 {
		mins := 11*60 + 30 + rng.Intn(150)
		return fmt.Sprintf("%02d:%02d", mins/60, mins%60)
	}
	mins := 18*60 + 30 + rng.Intn(210)
	return fmt.Sprintf("%02d:%02d", mins/60, mins%60)
}

// --- CSV writers ---------------------------------------------------------

func newWriter(path string) (*os.File, *csv.Writer, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}
	return f, csv.NewWriter(f), nil
}

func writeDeliveryCSV(path string, days []dayPlan, promos []promo, rng *rand.Rand) error {
	f, w, err := newWriter(path)
	if err != nil {
		return err
	}
	defer f.Close()
	defer w.Flush()

	if err := w.Write([]string{"platform", "order_id", "order_date", "order_time", "subtotal", "commission_rate_pct", "commission_amount", "net_payout", "status", "refund_date", "campaign_id", "notes"}); err != nil {
		return err
	}

	// Target attributed revenue per campaign, computed once up front from
	// each campaign's own spend and its intended positive/negative outcome
	// (get_promotion_roi's real computation is just sum(subtotal of
	// matching-campaign_id orders) - spend, see
	// reconcile.ComputePromotionRoiRecords — so hitting a real, specific
	// ROI outcome means controlling how much order revenue gets tagged
	// with the campaign, not a flat per-order tagging probability that
	// would let ROI fall out however the random ticket sizes happened to
	// land).
	campaignTarget := make(map[string]float64, len(promos))
	campaignTagged := make(map[string]float64, len(promos))
	for _, p := range promos {
		if p.positive {
			campaignTarget[p.campaignID] = p.spend * (1.3 + rng.Float64()*1.2) // 1.3x-2.5x spend
		} else {
			campaignTarget[p.campaignID] = p.spend * (0.25 + rng.Float64()*0.45) // 0.25x-0.7x spend
		}
	}

	ifoodSeq, jetSeq := 1, 1
	for _, d := range days {
		activeCampaign := campaignActiveOn(promos, d.date)

		for _, source := range []struct {
			name string
			rate float64
			seq  *int
			amt  float64
		}{
			{"iFood", ifoodCommissionPct, &ifoodSeq, d.ifoodGross},
			{"Just Eat Takeaway", jetCommissionPct, &jetSeq, d.jetGross},
		} {
			meanTicket := 32.0
			amounts := splitIntoOrders(source.amt, meanTicket, rng)
			for _, subtotal := range amounts {
				var orderID string
				if source.name == "iFood" {
					orderID = fmt.Sprintf("IFOOD-%s-%04d", d.date.Format("20060102"), *source.seq)
				} else {
					orderID = fmt.Sprintf("JET-%07d", 2000000+*source.seq)
				}
				*source.seq++

				campaign := ""
				if activeCampaign != nil && activeCampaign.platform == source.name {
					id := activeCampaign.campaignID
					if campaignTagged[id] < campaignTarget[id] {
						campaign = id
						campaignTagged[id] += subtotal
					}
				}

				commission := math.Round(subtotal*source.rate) / 100
				net := math.Round((subtotal-commission)*100) / 100

				if err := w.Write([]string{
					source.name, orderID, d.date.Format("2006-01-02"), randomTime(rng),
					money(subtotal), fmt.Sprintf("%.0f", source.rate), money(commission), money(net),
					"completed", "", campaign, "",
				}); err != nil {
					return err
				}

				// A duplicate byte-identical row, sparingly — exercises
				// dedupeDelivery's duplicate_order_removed flag.
				if rng.Float64() < duplicateOrderRate {
					if err := w.Write([]string{
						source.name, orderID, d.date.Format("2006-01-02"), randomTime(rng),
						money(subtotal), fmt.Sprintf("%.0f", source.rate), money(commission), money(net),
						"completed", "", campaign, "",
					}); err != nil {
						return err
					}
				}

				// A real refund, sparingly — except during a refundCluster
				// month (d.refundRateMult > 1.0), where a food-safety
				// complaint wave drives the rate up sharply and every
				// refund in that window carries d.refundNote instead of
				// the generic dispute note, so the cluster reads as one
				// coherent incident rather than unrelated one-offs.
				note := "Customer dispute; refund settled after the original order date."
				if d.refundNote != "" {
					note = d.refundNote
				}
				if rng.Float64() < refundRatePerOrder*d.refundRateMult {
					refundDate := d.date.AddDate(0, 0, 1+rng.Intn(6))
					if err := w.Write([]string{
						source.name, orderID, d.date.Format("2006-01-02"), randomTime(rng),
						money(-subtotal), fmt.Sprintf("%.0f", source.rate), money(-commission), money(-net),
						"refunded", refundDate.Format("2006-01-02"), "",
						note,
					}); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func writePOSCSV(path string, days []dayPlan, rng *rand.Rand) error {
	f, w, err := newWriter(path)
	if err != nil {
		return err
	}
	defer f.Close()
	defer w.Flush()

	if err := w.Write([]string{"order_id", "order_date", "order_time", "channel", "gross_amount", "payment_method", "status"}); err != nil {
		return err
	}

	channels := []string{"dine_in", "dine_in", "dine_in", "takeaway", "phone"}
	payments := []string{"card", "card", "cash"}

	seq := 1
	for _, d := range days {
		amounts := splitIntoOrders(d.posGross, 42.0, rng)
		// POS export uses DD/MM/YYYY throughout, matching the existing
		// fixture's own documented convention (internal/ingest/date.go).
		dateStr := d.date.Format("02/01/2006")
		for _, amt := range amounts {
			orderID := fmt.Sprintf("POS-%d", 1000+seq)
			seq++
			channel := channels[rng.Intn(len(channels))]
			payment := payments[rng.Intn(len(payments))]
			if err := w.Write([]string{orderID, dateStr, randomTime(rng), channel, money(amt), payment, "completed"}); err != nil {
				return err
			}
		}
	}
	return nil
}

// costShockCauses are the one-off, revenue-independent events real
// restaurants actually incur — varied across shock days so the CSV doesn't
// read as one repeated fabricated line item. Each is a real cost category
// this dataset already uses (produce/protein) plus a new "equipment"
// category for the two that aren't food-cost overruns at all.
var costShockCauses = []struct {
	supplier string
	category string
	note     string
}{
	{"Frostbyte Refrigeration Repair", "equipment", "Walk-in cooler compressor failure — emergency repair before spoilage"},
	{"Fresh Fields Produce Co.", "produce", "Spoiled delivery discarded; emergency same-day reorder at rush pricing"},
	{"Coastal Meat & Poultry", "protein", "Regional shortage price spike; emergency restock to cover service"},
	{"CityWide Plumbing & Repair", "equipment", "Grease trap backup — emergency plumbing repair"},
	{"Frostbyte Refrigeration Repair", "equipment", "Heat-wave compressor overload — walk-in ran warm for hours; spoiled stock discarded and re-bought same-day at rush pricing"},
}

// generalCostShockCauses is the pool an ORDINARY lossyDayChance day picks
// its cause from at random. It deliberately excludes costShockCauses[4]
// (the heat-wave entry, added for the 2025-08 regime): that cause only
// makes narrative sense in summer, and a plain random pick over the WHOLE
// costShockCauses slice would let it land on an unrelated December or
// April day too — a real bug this file had briefly, caught by reading the
// generated supplier_cost_sheet.csv and noticing a "heat wave" invoice
// dated in December. Season-pinned causes stay reachable only through a
// forced regime day's own causeIdx (see writeCostSheetCSV), never through
// this random pool.
var generalCostShockCauses = costShockCauses[:4]

func writeCostSheetCSV(path string, days []dayPlan, rng *rand.Rand) error {
	f, w, err := newWriter(path)
	if err != nil {
		return err
	}
	defer f.Close()
	defer w.Flush()

	if err := w.Write([]string{"invoice_id", "invoice_date", "supplier", "category", "amount", "notes"}); err != nil {
		return err
	}

	// Each category invoices on its own short cadence, sized from the REAL
	// gross revenue of the exact days it covers (clamped to len(days), so a
	// partial final window can never invoice a date past the last real
	// day — the bug the original week-bucketed version had, where a fixed
	// weekly offset could land 1-2 days after the dataset's actual last
	// day and produce a cost-sheet-only "day" with no matching sales data
	// at all). Cadences are deliberately short (2-4 days, not the
	// once-a-week rhythm a flat "protein weekly" cadence would use) so no
	// single invoice is large enough to swing daily margin by an amount
	// that reads as noise rather than the intended smooth growth curve —
	// a real tension the first version of this generator got wrong.
	type category struct {
		name     string
		supplier string
		cadence  int
		share    float64
	}
	categories := []category{
		{"produce", "Fresh Fields Produce Co.", 2, produceShare},
		{"protein", "Coastal Meat & Poultry", 3, proteinShare},
		{"beverage", "Blue Wave Beverage Distributors", 4, beverageShare},
		{"packaging", "PackRight Disposables", 4, packagingShare},
	}

	invoiceID := 3001
	for _, cat := range categories {
		for i := 0; i < len(days); i += cat.cadence {
			end := i + cat.cadence
			if end > len(days) {
				end = len(days)
			}
			var windowGross float64
			for _, d := range days[i:end] {
				windowGross += d.grossTotal
			}
			amount := windowGross * cogsShareOfGross * cat.share
			if amount <= 0 {
				continue
			}
			if err := w.Write([]string{
				fmt.Sprintf("INV-%d", invoiceID),
				days[i].date.Format("2006-01-02"),
				cat.supplier,
				cat.name,
				money(amount),
				"",
			}); err != nil {
				return err
			}
			invoiceID++
		}
	}

	// Cost shocks: a standalone invoice dated on the exact day it hit,
	// separate from the regular category cadence above, so it reads as
	// the one-off event it is rather than an inflated regular delivery.
	// Ordinary lossyDayChance days (regimeCauseIdx == -1) pick a random
	// cause, same as before; a forced regime shock day (regimeCauseIdx
	// >= 0, see monthRegime) is pinned to that regime's own cause so the
	// whole month's invoices tell one coherent story.
	for _, d := range days {
		if d.costShock <= 0 {
			continue
		}
		cause := generalCostShockCauses[rng.Intn(len(generalCostShockCauses))]
		if d.regimeCauseIdx >= 0 {
			cause = costShockCauses[d.regimeCauseIdx]
		}
		if err := w.Write([]string{
			fmt.Sprintf("INV-%d", invoiceID),
			d.date.Format("2006-01-02"),
			cause.supplier,
			cause.category,
			money(d.costShock),
			cause.note,
		}); err != nil {
			return err
		}
		invoiceID++
	}
	return nil
}

// --- promotions ------------------------------------------------------------

type promo struct {
	platform   string
	campaignID string
	name       string
	start, end time.Time
	spend      float64
	positive   bool
}

func campaignActiveOn(promos []promo, d time.Time) *promo {
	for i := range promos {
		p := &promos[i]
		if !d.Before(p.start) && !d.After(p.end) {
			return p
		}
	}
	return nil
}

func buildPromotions(days []dayPlan, rng *rand.Rand) []promo {
	var promos []promo
	names := []string{"Weekday Lunch Boost", "Weekend Featured Placement", "New Menu Launch", "Loyalty Push", "Late-Night Delivery Ad", "Seasonal Menu Spotlight"}
	n := 0
	for i := 0; i < len(days); i += promoEveryDays {
		platform := "iFood"
		prefix := "IFOOD"
		if n%2 == 1 {
			platform = "Just Eat Takeaway"
			prefix = "JET"
		}
		start := days[i].date
		end := start.AddDate(0, 0, 6)
		if idx := i + 6; idx < len(days) {
			end = days[idx].date
		}
		spend := 60.0 + rng.Float64()*220.0
		promos = append(promos, promo{
			platform:   platform,
			campaignID: fmt.Sprintf("%s-CAMP-%03d", prefix, n+1),
			name:       names[n%len(names)],
			start:      start,
			end:        end,
			spend:      math.Round(spend*100) / 100,
			positive:   rng.Float64() < promoPositiveP,
		})
		n++
	}
	return promos
}

func writePromoCSV(path string, promos []promo) error {
	f, w, err := newWriter(path)
	if err != nil {
		return err
	}
	defer f.Close()
	defer w.Flush()

	if err := w.Write([]string{"platform", "campaign_id", "campaign_name", "period_start", "period_end", "spend_amount", "placement_type", "notes"}); err != nil {
		return err
	}
	placements := []string{"in_app_boost", "banner_ad", "featured_placement", "sponsored_listing"}
	for i, p := range promos {
		placement := placements[i%len(placements)]
		if err := w.Write([]string{
			p.platform, p.campaignID, p.name,
			p.start.Format("2006-01-02"), p.end.Format("2006-01-02"),
			money(p.spend), placement, "",
		}); err != nil {
			return err
		}
	}
	return nil
}

func money(v float64) string {
	return fmt.Sprintf("%.2f", v)
}
