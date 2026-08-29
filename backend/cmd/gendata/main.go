// Command gendata generates a realistic 2-year (730-day) synthetic dataset
// for My Business Steward's "live" exploration database
// (backend/data/live/), replacing its current contents — which today are
// just a copy of the small, deliberately-messy 14-day evaluation fixture
// (backend/fixtures/), untouched by this tool and unaffected by it.
//
// The growth story: gross revenue grows on an S-curve (faster in the first
// year, decelerating in the second — a real small-restaurant ramp, not
// flat compound growth) from a modest single-location starting point to a
// scale that produces roughly $20,000/month in this product's own margin
// metric (gross sales minus commissions minus refunds minus input costs —
// NOT full net profit after labor/rent/overhead, which this product never
// computes at all). At the cost ratios used here (roughly 62% of gross
// revenue survives as margin), that endpoint is reached at approximately
// $32,000-34,000/month gross revenue — a modest, realistic figure for one
// location, not the $250k+/month a literal bottom-line-profit reading of
// "$20k/month profit" would imply for a single restaurant.
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
	"time"
)

// --- Tunable parameters, all in one place for auditability -----------------

const (
	numDays = 730

	// Gross revenue growth curve (logistic S-curve), in dollars/month.
	startMonthlyGross = 14000.0
	endMonthlyGross   = 33500.0
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

	promoEveryDays = 30 // roughly one campaign a month
	promoPositiveP = 0.65
	randSeed       = 20260815 // deterministic — same seed, same dataset, every regen
)

var startDate = time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)

func main() {
	outDir := flag.String("out", "data/live", "output directory for the generated CSVs (relative to backend/)")
	flag.Parse()

	rng := rand.New(rand.NewSource(randSeed))

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "gendata:", err)
		os.Exit(1)
	}

	days := make([]dayPlan, numDays)
	for i := range days {
		date := startDate.AddDate(0, 0, i)
		monthIdx := float64(i) / 30.44
		targetMonthly := logisticGross(monthIdx)
		days[i] = planDay(date, targetMonthly, rng)
	}

	promos := buildPromotions(days, rng)

	if err := writeDeliveryCSV(filepath.Join(*outDir, "delivery_platform_export.csv"), days, promos, rng); err != nil {
		fail(err)
	}
	if err := writePOSCSV(filepath.Join(*outDir, "pos_export.csv"), days, rng); err != nil {
		fail(err)
	}
	if err := writeCostSheetCSV(filepath.Join(*outDir, "supplier_cost_sheet.csv"), days); err != nil {
		fail(err)
	}
	if err := writePromoCSV(filepath.Join(*outDir, "promotion_ad_spend_export.csv"), promos); err != nil {
		fail(err)
	}

	first, last := days[0], days[len(days)-1]
	fmt.Printf("generated %d days: %s ($%.0f/mo gross) -> %s ($%.0f/mo gross)\n",
		numDays, first.date.Format("2006-01-02"), first.targetMonthly,
		last.date.Format("2006-01-02"), last.targetMonthly)
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
	date          time.Time
	targetMonthly float64
	grossTotal    float64
	posGross      float64
	ifoodGross    float64
	jetGross      float64
	anomaly       bool
}

func planDay(date time.Time, targetMonthly float64, rng *rand.Rand) dayPlan {
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

	gross := baseDaily * mult * noise

	return dayPlan{
		date:          date,
		targetMonthly: targetMonthly,
		grossTotal:    gross,
		posGross:      gross * posShare,
		ifoodGross:    gross * ifoodShare,
		jetGross:      gross * jetShare,
		anomaly:       anomaly,
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

				// A real refund, sparingly.
				if rng.Float64() < refundRatePerOrder {
					refundDate := d.date.AddDate(0, 0, 1+rng.Intn(6))
					if err := w.Write([]string{
						source.name, orderID, d.date.Format("2006-01-02"), randomTime(rng),
						money(-subtotal), fmt.Sprintf("%.0f", source.rate), money(-commission), money(-net),
						"refunded", refundDate.Format("2006-01-02"), "",
						"Customer dispute; refund settled after the original order date.",
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

func writeCostSheetCSV(path string, days []dayPlan) error {
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
