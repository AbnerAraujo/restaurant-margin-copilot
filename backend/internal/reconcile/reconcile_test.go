package reconcile

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
)

// Fixture paths, and the independently-verified golden values below, come
// straight from backend/fixtures/README.md — this test does not
// reimplement or second-guess that document, it asserts this package's
// output matches it exactly.
const (
	fixtureDeliveryFile = "../../fixtures/delivery_platform_export.csv"
	fixturePOSFile      = "../../fixtures/pos_export.csv"
	fixtureCostFile     = "../../fixtures/supplier_cost_sheet.csv"
)

func loadFixtures(t *testing.T) ([]ingest.DeliveryRecord, []ingest.POSRecord, []ingest.CostInvoiceRecord) {
	t.Helper()

	deliveryFile, err := os.Open(fixtureDeliveryFile)
	require.NoError(t, err)
	defer deliveryFile.Close()
	delivery, err := ingest.ParseDeliveryExport(deliveryFile, fixtureDeliveryFile)
	require.NoError(t, err)

	posFile, err := os.Open(fixturePOSFile)
	require.NoError(t, err)
	defer posFile.Close()
	pos, err := ingest.ParsePOSExport(posFile, fixturePOSFile)
	require.NoError(t, err)

	costFile, err := os.Open(fixtureCostFile)
	require.NoError(t, err)
	defer costFile.Close()
	costs, err := ingest.ParseCostSheet(costFile, fixtureCostFile)
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
	delivery, pos, costs := loadFixtures(t)
	days := ComputeDailyReconciliations(delivery, pos, costs)

	day := findDay(t, days, "2026-08-03")
	require.Equal(t, int64(5525), day.GrossSalesBySource["ifood"], "duplicate row for IFOOD-20260803-0011 must be counted once (55.25), not twice (79.25)")
	require.True(t, hasFlagType(day.DiscrepancyFlags, FlagDuplicateOrderRemoved), "duplicate collapse must be visible as a discrepancy flag, not silent")
}

// --- Irregularity #2: refund nets to zero on the original order date ---
func TestComputeDailyReconciliations_RefundNetsCorrectly(t *testing.T) {
	delivery, pos, costs := loadFixtures(t)
	days := ComputeDailyReconciliations(delivery, pos, costs)

	day := findDay(t, days, "2026-08-02")
	require.Equal(t, int64(7250), day.GrossSalesBySource["ifood"], "gross (completed-only) must still show the original 72.50 iFood total")
	require.Equal(t, int64(8175), day.GrossSalesBySource["just_eat_takeaway"])
	require.Equal(t, int64(3450), day.RefundsCents, "refund magnitude must be 34.50, netted against the original order_date per fixtures/README.md's documented convention")
	require.Equal(t, int64(2509), day.CommissionsCents, "commission must net to 25.09: the refunded order's 7.94 commission is reversed by its own refund row's -7.94")

	// CommissionsBySource (specs/003-platform-comparator): the refund's
	// reversal is keyed by "ifood" (same source as its original order), so
	// it nets WITHIN that source's own entry — iFood's 8.74 (order 0006) +
	// 7.94 (order 0007 completed) - 7.94 (order 0007 refunded) = 8.74, not
	// 16.68. JET is untouched by the refund: 5.95 + 10.40 = 16.35. The two
	// sources still sum to the day's existing 25.09 total.
	require.Equal(t, int64(874), day.CommissionsBySource["ifood"], "iFood commission for 2026-08-02, net of order 0007's refund reversal")
	require.Equal(t, int64(1635), day.CommissionsBySource["just_eat_takeaway"])
	require.Equal(t, day.CommissionsCents, day.CommissionsBySource["ifood"]+day.CommissionsBySource["just_eat_takeaway"], "per-source commission must sum back to the day's total")

	require.Equal(t, int64(22375), day.GrossSalesBySource["pos"])
	require.Equal(t, int64(54550), day.InputCostsCents)
	require.Equal(t, int64(-22709), day.MarginCents, "2026-08-02 margin: (72.50+81.75+223.75) - 25.09 - 34.50 - 545.50 = -227.09")
}

// --- Irregularity #3: missing delivery day is flagged, not silently omitted ---
func TestComputeDailyReconciliations_MissingDeliveryDayFlaggedNotOmitted(t *testing.T) {
	delivery, pos, costs := loadFixtures(t)
	days := ComputeDailyReconciliations(delivery, pos, costs)

	day := findDay(t, days, "2026-08-08")
	require.True(t, hasFlagType(day.DiscrepancyFlags, FlagMissingDeliverySource), "a day with POS/cost data but zero delivery rows must be explicitly flagged")
	require.Zero(t, day.GrossSalesBySource["ifood"])
	require.Zero(t, day.GrossSalesBySource["just_eat_takeaway"])

	// POS and cost-sheet data for the day must still be reconciled — the
	// missing source must not blank out the whole day (spec Acceptance
	// Scenario US1.3).
	require.Equal(t, int64(48750), day.GrossSalesBySource["pos"], "POS gross for 2026-08-08 (145.00+132.50+168.00+42.00)")
	require.Equal(t, int64(33500), day.InputCostsCents, "INV-3007 (335.00)")
	require.Equal(t, int64(15250), day.MarginCents, "487.50 - 335.00, since delivery contributes nothing this day")
}

// A clean day (no anomalies) must match a hand computation exactly (spec
// Acceptance Scenario US1.1).
func TestComputeDailyReconciliations_CleanDayMatchesHandComputation(t *testing.T) {
	delivery, pos, costs := loadFixtures(t)
	days := ComputeDailyReconciliations(delivery, pos, costs)

	day := findDay(t, days, "2026-08-01")
	require.Equal(t, int64(6950), day.GrossSalesBySource["ifood"])
	require.Equal(t, int64(7625), day.GrossSalesBySource["just_eat_takeaway"])
	require.Equal(t, int64(24875), day.GrossSalesBySource["pos"])
	require.Equal(t, int64(3124), day.CommissionsCents)
	require.Equal(t, int64(1599), day.CommissionsBySource["ifood"], "9.66 (order 0001) + 6.33 (order 0002)")
	require.Equal(t, int64(1525), day.CommissionsBySource["just_eat_takeaway"], "6.20 + 9.05")
	require.NotContains(t, day.CommissionsBySource, "pos", "POS orders carry no commission at all")
	require.Zero(t, day.RefundsCents)
	require.Equal(t, int64(32000), day.InputCostsCents)
	require.Equal(t, int64(4326), day.MarginCents, "(69.50+76.25+248.75) - 31.24 - 0 - 320.00 = 43.26")
	require.False(t, hasFlagType(day.DiscrepancyFlags, FlagMissingDeliverySource))
	require.False(t, hasFlagType(day.DiscrepancyFlags, FlagDuplicateOrderRemoved))
}

// Regression proof that recomputing commission from subtotal * rate with
// round-half-up (internal/money) matches the fixture's pre-computed
// commission_amount column exactly, with zero false-positive mismatches
// across all 54 delivery rows. A naive float64 or round-half-to-even
// computation would flag several rows here purely from rounding-mode
// artifacts (e.g. 34.50 * 23% = 7.935), not real data problems.
func TestComputeDailyReconciliations_NoFalsePositiveCommissionMismatches(t *testing.T) {
	delivery, pos, costs := loadFixtures(t)
	days := ComputeDailyReconciliations(delivery, pos, costs)

	for _, day := range days {
		for _, f := range day.DiscrepancyFlags {
			require.NotEqual(t, FlagCommissionMismatch, f.Type, "unexpected commission mismatch on %s: %s", day.Date.Format("2006-01-02"), f.Detail)
		}
	}
}

// Full-period totals cross-checked against fixtures/README.md's
// independently-verified sums.
func TestComputeDailyReconciliations_PeriodTotalsMatchIndependentReference(t *testing.T) {
	delivery, pos, costs := loadFixtures(t)
	days := ComputeDailyReconciliations(delivery, pos, costs)

	var totalPOS, totalCosts int64
	for _, day := range days {
		totalPOS += day.GrossSalesBySource["pos"]
		totalCosts += day.InputCostsCents
	}

	require.Equal(t, int64(347275), totalPOS, "fixtures/README.md: POS gross total across all 14 days is 3472.75")
	require.Equal(t, int64(433575), totalCosts, "fixtures/README.md: supplier cost sheet total is 4335.75")
	require.Len(t, days, 14, "the fixture set spans 14 calendar days, 2026-08-01 through 2026-08-14")
}

// Documents current behavior at the current threshold/window: none of this
// fixture set's real day-to-day variance (max ~18% at the current 3-day
// trailing window) crosses the 20% anomaly threshold. A future change to
// AnomalyThresholdPct or TrailingWindowDays that breaks this is a
// deliberate, visible decision, not a silent regression.
func TestComputeDailyReconciliations_NoAnomalyFlaggedAtCurrentThreshold(t *testing.T) {
	delivery, pos, costs := loadFixtures(t)
	days := ComputeDailyReconciliations(delivery, pos, costs)

	for _, day := range days {
		require.False(t, hasFlagType(day.DiscrepancyFlags, FlagAnomalyThresholdExceeded),
			"%s unexpectedly flagged as an anomaly at the current %.0f%% / %d-day threshold", day.Date.Format("2006-01-02"), AnomalyThresholdPct*100, TrailingWindowDays)
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
