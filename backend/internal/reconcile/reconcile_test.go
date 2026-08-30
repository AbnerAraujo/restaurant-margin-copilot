package reconcile

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
)

// The paths point at the dataset's checked-in hand-authored opening window
// (backend/cmd/gendata/opening/); the independently-verified golden values
// below come straight from opening/README.md — this test does not
// reimplement or second-guess that document, it asserts this package's
// output matches it exactly.
const (
	openingDeliveryFile = "../../cmd/gendata/opening/delivery_platform_export.csv"
	openingPOSFile      = "../../cmd/gendata/opening/pos_export.csv"
	openingCostFile     = "../../cmd/gendata/opening/supplier_cost_sheet.csv"
)

func loadOpeningWindow(t *testing.T) ([]ingest.DeliveryRecord, []ingest.POSRecord, []ingest.CostInvoiceRecord) {
	t.Helper()

	deliveryFile, err := os.Open(openingDeliveryFile)
	require.NoError(t, err)
	defer deliveryFile.Close()
	delivery, err := ingest.ParseDeliveryExport(deliveryFile, openingDeliveryFile)
	require.NoError(t, err)

	posFile, err := os.Open(openingPOSFile)
	require.NoError(t, err)
	defer posFile.Close()
	pos, err := ingest.ParsePOSExport(posFile, openingPOSFile)
	require.NoError(t, err)

	costFile, err := os.Open(openingCostFile)
	require.NoError(t, err)
	defer costFile.Close()
	costs, err := ingest.ParseCostSheet(costFile, openingCostFile)
	require.NoError(t, err)

	return delivery, pos, costs
}

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	require.NoError(t, err)
	return d
}

func findDay(t *testing.T, days []DailyReconciliation, date string) DailyReconciliation {
	t.Helper()
	want := mustDate(t, date)
	for _, d := range days {
		if d.Date.Equal(want) {
			return d
		}
	}
	t.Fatalf("no DailyReconciliation for %s", date)
	return DailyReconciliation{}
}

// --- Irregularity #1: duplicate order counted once ---
func TestComputeDailyReconciliations_DuplicateOrderCountedOnce(t *testing.T) {
	delivery, pos, costs := loadOpeningWindow(t)
	days := ComputeDailyReconciliations(delivery, pos, costs)

	day := findDay(t, days, "2024-08-03")
	require.Equal(t, int64(26200), day.GrossSalesBySource["ifood"], "duplicate row for IFOOD-20240803-0011 must be counted once (262.00), not twice (286.00)")
	require.True(t, hasFlagType(day.DiscrepancyFlags, FlagDuplicateOrderRemoved), "duplicate collapse must be visible as a discrepancy flag, not silent")
}

// --- Irregularity #2: refund nets to zero on the original order date ---
func TestComputeDailyReconciliations_RefundNetsCorrectly(t *testing.T) {
	delivery, pos, costs := loadOpeningWindow(t)
	days := ComputeDailyReconciliations(delivery, pos, costs)

	day := findDay(t, days, "2024-08-02")
	require.Equal(t, int64(23175), day.GrossSalesBySource["ifood"], "gross (completed-only) must still show the original 231.75 iFood total")
	require.Equal(t, int64(21450), day.GrossSalesBySource["just_eat_takeaway"])
	require.Equal(t, int64(6225), day.RefundsCents, "refund magnitude must be 62.25, netted against the original order_date per opening/README.md's documented convention")
	require.Equal(t, int64(8190), day.CommissionsCents, "commission must net to 81.90: the refunded order's 14.32 commission is reversed by its own refund row's -14.32")

	// CommissionsBySource (specs/003-platform-comparator): the refund's
	// reversal is keyed by "ifood" (same source as its original order), so
	// it nets WITHIN that source's own entry — iFood's 11.16 (order 0005)
	// + 14.32 (order 0006 completed) + 13.23 (order 0007) + 14.61 (order
	// 0008) - 14.32 (order 0006 refunded) = 39.00. JET is untouched by the
	// refund: 7.95 + 11.65 + 12.20 + 11.10 = 42.90. The two sources still
	// sum to the day's existing 81.90 total.
	require.Equal(t, int64(3900), day.CommissionsBySource["ifood"], "iFood commission for 2024-08-02, net of order 0006's refund reversal")
	require.Equal(t, int64(4290), day.CommissionsBySource["just_eat_takeaway"])
	require.Equal(t, day.CommissionsCents, day.CommissionsBySource["ifood"]+day.CommissionsBySource["just_eat_takeaway"], "per-source commission must sum back to the day's total")

	// RefundsBySource (A15, docs/product-strategy.md): order 0006's
	// -62.25 refund row carries platform "iFood", so the day's entire
	// refund total attributes to iFood alone — Just Eat Takeaway had no
	// refund this day and must not appear in the map at all
	// (opening/README.md's only refund in the whole window is this one).
	require.Equal(t, int64(6225), day.RefundsBySource["ifood"], "the day's only refund (order 0006) is iFood's")
	require.NotContains(t, day.RefundsBySource, "just_eat_takeaway", "no refund happened on Just Eat Takeaway this day")
	require.Equal(t, day.RefundsCents, day.RefundsBySource["ifood"], "per-source refund must sum back to the day's total")

	require.Equal(t, int64(80225), day.GrossSalesBySource["pos"])
	require.Equal(t, int64(61275), day.InputCostsCents)
	require.Equal(t, int64(49160), day.MarginCents, "2024-08-02 margin: (231.75+214.50+802.25) - 81.90 - 62.25 - 612.75 = 491.60")
}

// --- Irregularity #3: missing delivery day is flagged, not silently omitted ---
func TestComputeDailyReconciliations_MissingDeliveryDayFlaggedNotOmitted(t *testing.T) {
	delivery, pos, costs := loadOpeningWindow(t)
	days := ComputeDailyReconciliations(delivery, pos, costs)

	day := findDay(t, days, "2024-08-10")
	require.True(t, hasFlagType(day.DiscrepancyFlags, FlagMissingDeliverySource), "a day with POS/cost data but zero delivery rows must be explicitly flagged")
	require.Zero(t, day.GrossSalesBySource["ifood"])
	require.Zero(t, day.GrossSalesBySource["just_eat_takeaway"])

	// POS and cost-sheet data for the day must still be reconciled — the
	// missing source must not blank out the whole day (spec Acceptance
	// Scenario US1.3).
	require.Equal(t, int64(120450), day.GrossSalesBySource["pos"], "POS gross for 2024-08-10 (232.50+318.25+124.75+156.00+198.50+174.50)")
	require.Equal(t, int64(33600), day.InputCostsCents, "INV-3009 (336.00)")
	require.Equal(t, int64(86850), day.MarginCents, "1,204.50 - 336.00, since delivery contributes nothing this day")
}

// A clean day (no anomalies) must match a hand computation exactly (spec
// Acceptance Scenario US1.1).
func TestComputeDailyReconciliations_CleanDayMatchesHandComputation(t *testing.T) {
	delivery, pos, costs := loadOpeningWindow(t)
	days := ComputeDailyReconciliations(delivery, pos, costs)

	day := findDay(t, days, "2024-08-01")
	require.Equal(t, int64(19650), day.GrossSalesBySource["ifood"])
	require.Equal(t, int64(17825), day.GrossSalesBySource["just_eat_takeaway"])
	require.Equal(t, int64(73650), day.GrossSalesBySource["pos"])
	require.Equal(t, int64(8085), day.CommissionsCents)
	require.Equal(t, int64(4520), day.CommissionsBySource["ifood"], "9.66 + 12.71 + 8.91 + 13.92 (orders 0001-0004)")
	require.Equal(t, int64(3565), day.CommissionsBySource["just_eat_takeaway"], "6.30 + 10.55 + 8.80 + 10.00")
	require.NotContains(t, day.CommissionsBySource, "pos", "POS orders carry no commission at all")
	require.Zero(t, day.RefundsCents)
	require.Empty(t, day.RefundsBySource, "no refund happened on 2024-08-01, so the map must be empty, not zero-valued entries")
	require.Equal(t, int64(32850), day.InputCostsCents)
	require.Equal(t, int64(70190), day.MarginCents, "(196.50+178.25+736.50) - 80.85 - 0 - 328.50 = 701.90")
	require.False(t, hasFlagType(day.DiscrepancyFlags, FlagMissingDeliverySource))
	require.False(t, hasFlagType(day.DiscrepancyFlags, FlagDuplicateOrderRemoved))
}

// Regression proof that recomputing commission from subtotal * rate with
// round-half-up (internal/money) matches the opening window's pre-computed
// commission_amount column exactly, with zero false-positive mismatches
// across all 107 delivery rows. A naive float64 or round-half-to-even
// computation would flag several rows here purely from rounding-mode
// artifacts (e.g. 55.25 * 23% = 12.7075), not real data problems.
func TestComputeDailyReconciliations_NoFalsePositiveCommissionMismatches(t *testing.T) {
	delivery, pos, costs := loadOpeningWindow(t)
	days := ComputeDailyReconciliations(delivery, pos, costs)

	for _, day := range days {
		for _, f := range day.DiscrepancyFlags {
			require.NotEqual(t, FlagCommissionMismatch, f.Type, "unexpected commission mismatch on %s: %s", day.Date.Format("2006-01-02"), f.Detail)
		}
	}
}

// Full-period totals cross-checked against opening/README.md's
// independently-verified sums.
func TestComputeDailyReconciliations_PeriodTotalsMatchIndependentReference(t *testing.T) {
	delivery, pos, costs := loadOpeningWindow(t)
	days := ComputeDailyReconciliations(delivery, pos, costs)

	var totalPOS, totalCosts int64
	for _, day := range days {
		totalPOS += day.GrossSalesBySource["pos"]
		totalCosts += day.InputCostsCents
	}

	require.Equal(t, int64(1066725), totalPOS, "opening/README.md: POS gross total across all 14 days is 10,667.25")
	require.Equal(t, int64(500275), totalCosts, "opening/README.md: supplier cost sheet total is 5,002.75")
	require.Len(t, days, 14, "the opening window spans 14 calendar days, 2024-08-01 through 2024-08-14")
}

// Documents current behavior at the current threshold/window: at the
// dataset's realistic scale, ordinary weekly seasonality legitimately
// crosses the 20% / 3-day-trailing threshold on exactly three opening days
// — 2024-08-05 (Monday dip after the weekend), 2024-08-09 (Friday spike),
// and 2024-08-12 (Monday dip; 2024-08-10, the missing-delivery Saturday,
// is correctly excluded from both flagging and the baseline) — and on no
// others. A future change to AnomalyThresholdPct or TrailingWindowDays
// that shifts this set is a deliberate, visible decision, not a silent
// regression.
func TestComputeDailyReconciliations_AnomalyFlagsPinnedAtCurrentThreshold(t *testing.T) {
	delivery, pos, costs := loadOpeningWindow(t)
	days := ComputeDailyReconciliations(delivery, pos, costs)

	wantFlagged := map[string]bool{
		"2024-08-05": true,
		"2024-08-09": true,
		"2024-08-12": true,
	}
	for _, day := range days {
		key := day.Date.Format("2006-01-02")
		got := hasFlagType(day.DiscrepancyFlags, FlagAnomalyThresholdExceeded)
		require.Equalf(t, wantFlagged[key], got,
			"%s: anomaly flag presence at the current %.0f%% / %d-day threshold", key, AnomalyThresholdPct*100, TrailingWindowDays)
	}
}

// --- Anomaly threshold logic, isolated from ingest/parsing entirely ---
func TestApplyAnomalyFlags_TableDriven(t *testing.T) {
	day := func(dateStr string, grossCents int64, flags ...DiscrepancyFlag) DailyReconciliation {
		return DailyReconciliation{
			Date:               mustDate(t, dateStr),
			GrossSalesBySource: map[string]int64{"pos": grossCents},
			DiscrepancyFlags:   flags,
		}
	}

	tests := []struct {
		name          string
		days          []DailyReconciliation
		wantAnomalyAt map[string]bool
	}{
		{
			name: "steady values, no anomaly",
			days: []DailyReconciliation{
				day("2026-01-01", 10000),
				day("2026-01-02", 10200),
				day("2026-01-03", 9800),
				day("2026-01-04", 10100),
			},
			wantAnomalyAt: map[string]bool{
				"2026-01-01": false, "2026-01-02": false, "2026-01-03": false, "2026-01-04": false,
			},
		},
		{
			name: "first day never flagged (no baseline yet)",
			days: []DailyReconciliation{
				day("2026-01-01", 1000000), // would be a huge spike relative to nothing
			},
			wantAnomalyAt: map[string]bool{"2026-01-01": false},
		},
		{
			name: "spike above threshold is flagged",
			days: []DailyReconciliation{
				day("2026-01-01", 10000),
				day("2026-01-02", 10000),
				day("2026-01-03", 10000),
				day("2026-01-04", 20000), // +100% vs trailing average
			},
			wantAnomalyAt: map[string]bool{
				"2026-01-01": false, "2026-01-02": false, "2026-01-03": false, "2026-01-04": true,
			},
		},
		{
			name: "drop below threshold is flagged",
			days: []DailyReconciliation{
				day("2026-01-01", 10000),
				day("2026-01-02", 10000),
				day("2026-01-03", 10000),
				day("2026-01-04", 2000), // -80% vs trailing average
			},
			wantAnomalyAt: map[string]bool{
				"2026-01-01": false, "2026-01-02": false, "2026-01-03": false, "2026-01-04": true,
			},
		},
		{
			name: "a day already flagged missing_delivery_source is excluded from being flagged and from the baseline",
			days: []DailyReconciliation{
				day("2026-01-01", 10000),
				day("2026-01-02", 10000),
				day("2026-01-03", 500000, DiscrepancyFlag{Type: FlagMissingDeliverySource}), // huge, but incomplete data — must not poison the baseline or be flagged itself
				day("2026-01-04", 10000),
			},
			wantAnomalyAt: map[string]bool{
				"2026-01-01": false, "2026-01-02": false, "2026-01-03": false, "2026-01-04": false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			days := make([]DailyReconciliation, len(tt.days))
			copy(days, tt.days)
			applyAnomalyFlags(days)

			for _, d := range days {
				key := d.Date.Format("2006-01-02")
				want, ok := tt.wantAnomalyAt[key]
				require.True(t, ok, "unexpected date %s in test case", key)
				got := hasFlagType(d.DiscrepancyFlags, FlagAnomalyThresholdExceeded)
				require.Equal(t, want, got, "%s: anomaly flag presence mismatch", key)
			}
		})
	}
}
