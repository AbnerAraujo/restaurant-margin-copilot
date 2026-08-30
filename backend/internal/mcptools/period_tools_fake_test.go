package mcptools_test

// Fake-Querier-backed coverage for get_period_totals (period_tools.go),
// following reconciliation_tools_fake_test.go's established pattern
// (Finding 4): this tool's core logic must run in a default
// `go test ./...` with zero Postgres dependency, not only in the
// DATABASE_URL-gated live suite (period_tools_test.go).

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/mcptools"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// TestGetPeriodTotals_SumsAndRanksAThreeDayPeriod_Fake is the happy path:
// three synthetic days with distinct per-source gross, commissions,
// refunds, input costs, and margins, hand-summed in the assertions below
// (not derived from the tool's own output).
func TestGetPeriodTotals_SumsAndRanksAThreeDayPeriod_Fake(t *testing.T) {
	q := newFakeQuerier()
	ctx := context.Background()

	// day 1: gross ifood 50.00, pos 30.00; commission 5.00; refund 0;
	//   input costs 10.00; margin = 50+30-5-0-10 = 65.00
	// day 2: gross ifood 20.00, pos 40.00; commission 4.00; refund 2.00;
	//   input costs 15.00; margin = 20+40-4-2-15 = 39.00
	// day 3: gross ifood 10.00, pos 10.00; commission 1.00; refund 0;
	//   input costs 50.00; margin = 10+10-1-0-50 = -31.00
	// totals: ifood 80.00, pos 80.00 -> delivery total 80.00 (pos excluded)
	//   commissions 10.00, refunds 2.00 (all Just Eat Takeaway's, from day 2),
	//   input costs 75.00
	//   margin total = 65.00+39.00-31.00 = 73.00
	//   avg daily margin = 73.00 / 3 = 24.33 (round-half-up on 24.333...)
	//   best day: 2020-04-01 (65.00); worst day: 2020-04-03 (-31.00)
	days := []struct {
		date            string
		ifoodCents      int64
		posCents        int64
		commissionCents int64
		refundCents     int64
		inputCostCents  int64
		marginCents     int64
	}{
		{"2020-04-01", 5000, 3000, 500, 0, 1000, 6500},
		{"2020-04-02", 2000, 4000, 400, 200, 1500, 3900},
		{"2020-04-03", 1000, 1000, 100, 0, 5000, -3100},
	}
	for i, d := range days {
		date := sentinelDate(t, d.date)
		day := reconcile.DailyReconciliation{
			Date:               date,
			GrossSalesBySource: map[string]int64{"ifood": d.ifoodCents, "pos": d.posCents},
			CommissionsCents:   d.commissionCents,
			RefundsCents:       d.refundCents,
			InputCostsCents:    d.inputCostCents,
			MarginCents:        d.marginCents,
			// A distinct row per day (1, 2, 3), not all the same row —
			// so this test actually exercises collapseSourceRowRefsByFile's
			// min/max collapsing (see period_tools.go's doc comment on
			// that function for the real bug this collapsing fixes)
			// rather than trivially collapsing 3 identical refs into 1.
			SourceRowRefs: []reconcile.SourceRowRef{{File: "test.csv", Row: i + 1}},
		}
		// Attribute day 2's refund to Just Eat Takeaway (A15,
		// docs/product-strategy.md) — a distinct source from the ifood
		// gross above, so this test also proves RefundsBySource isn't
		// silently keyed the same as GrossSalesBySource by accident.
		if d.refundCents != 0 {
			day.RefundsBySource = map[string]int64{"just_eat_takeaway": d.refundCents}
		}
		_, err := storage.SaveDailyReconciliation(ctx, q, day)
		require.NoError(t, err)
	}

	result, toolErr, err := mcptools.GetPeriodTotals(ctx, q, mcptools.Period{Start: "2020-04-01", End: "2020-04-03"})
	require.NoError(t, err)
	require.Nil(t, toolErr)
	require.NotNil(t, result)

	require.Equal(t, "2020-04-01", result.Start)
	require.Equal(t, "2020-04-03", result.End)
	require.Equal(t, 3, result.DaysIncluded)
	require.Equal(t, "80.00", result.GrossSalesBySource["ifood"])
	require.Equal(t, "80.00", result.GrossSalesBySource["pos"])
	require.Equal(t, "80.00", result.TotalDeliveryGrossSales, "must be ifood only — pos is not delivery revenue")
	require.Equal(t, "10.00", result.Commissions)
	require.Equal(t, "2.00", result.Refunds)
	require.Equal(t, "2.00", result.RefundsBySource["just_eat_takeaway"], "period sum of RefundsBySource must attribute the whole 2.00 to the platform it actually came from")
	require.NotContains(t, result.RefundsBySource, "ifood", "ifood had zero refunds across the period")
	require.Equal(t, "75.00", result.InputCosts)
	require.Equal(t, "73.00", result.MarginTotal)
	require.Equal(t, "24.33", result.AvgDailyMargin, "73.00/3 = 24.333..., round-half-up to 24.33")
	require.Equal(t, "2020-04-01", result.BestDay.Date)
	require.Equal(t, "65.00", result.BestDay.Margin)
	require.Equal(t, "2020-04-03", result.WorstDay.Date)
	require.Equal(t, "-31.00", result.WorstDay.Margin)
	// Collapsed to the min and max row seen for "test.csv" (1 and 3, from
	// the three days' distinct rows above) — not one entry per day. See
	// collapseSourceRowRefsByFile's doc comment: this is the fix for a
	// real live failure where an unbounded per-day ref list, multiplied
	// across the full multi-year dataset, pushed a single explain-step
	// prompt past 1,000,000 tokens.
	require.Equal(t, []reconcile.SourceRowRef{
		{File: "test.csv", Row: 1},
		{File: "test.csv", Row: 3},
	}, result.SourceRowRefs)
}

// TestGetPeriodTotals_BestAndWorstDayTieBreakToEarliestDate_Fake proves the
// tie-break rule this tool documents (period_tools.go's PeriodTotalsResult
// doc comment): on an exact margin tie, the chronologically earliest date
// wins both best_day and worst_day.
func TestGetPeriodTotals_BestAndWorstDayTieBreakToEarliestDate_Fake(t *testing.T) {
	q := newFakeQuerier()
	ctx := context.Background()

	for _, ds := range []string{"2020-05-03", "2020-05-01", "2020-05-02"} {
		date := sentinelDate(t, ds)
		_, err := storage.SaveDailyReconciliation(ctx, q, reconcile.DailyReconciliation{
			Date:        date,
			MarginCents: 1000, // every day ties at the same margin
		})
		require.NoError(t, err)
	}

	result, toolErr, err := mcptools.GetPeriodTotals(ctx, q, mcptools.Period{Start: "2020-05-01", End: "2020-05-03"})
	require.NoError(t, err)
	require.Nil(t, toolErr)
	require.NotNil(t, result)

	require.Equal(t, "2020-05-01", result.BestDay.Date, "earliest date must win a tie for best_day")
	require.Equal(t, "2020-05-01", result.WorstDay.Date, "earliest date must win a tie for worst_day")
}

// TestGetPeriodTotals_InsufficientDataWhenPeriodHasMissingDay_Fake is this
// tool's own version of Finding 4's core assertion: periodMargin's
// missing-day refusal is mirrored here as GetPeriodTotals' own
// missing-day check, and must fire identically — never a partial total.
func TestGetPeriodTotals_InsufficientDataWhenPeriodHasMissingDay_Fake(t *testing.T) {
	q := newFakeQuerier()
	ctx := context.Background()

	// 2020-06-10 and 2020-06-12 exist; 2020-06-11 (in between) does not.
	for _, ds := range []string{"2020-06-10", "2020-06-12"} {
		date := sentinelDate(t, ds)
		_, err := storage.SaveDailyReconciliation(ctx, q, reconcile.DailyReconciliation{
			Date:        date,
			MarginCents: 100,
		})
		require.NoError(t, err)
	}

	result, toolErr, err := mcptools.GetPeriodTotals(ctx, q, mcptools.Period{Start: "2020-06-10", End: "2020-06-12"})
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, toolErr)
	require.Equal(t, "insufficient_data", toolErr.Error)
	require.Equal(t, []string{"2020-06-11"}, toolErr.Missing)
}

// TestGetPeriodTotals_InsufficientDataWhenPeriodIsEntirelyMissing_Fake
// covers the boundary the "one gap in an otherwise-populated range" test
// above doesn't: zero persisted days at all.
func TestGetPeriodTotals_InsufficientDataWhenPeriodIsEntirelyMissing_Fake(t *testing.T) {
	q := newFakeQuerier()
	ctx := context.Background()

	result, toolErr, err := mcptools.GetPeriodTotals(ctx, q, mcptools.Period{Start: "2020-07-01", End: "2020-07-02"})
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, toolErr)
	require.Equal(t, "insufficient_data", toolErr.Error)
	require.ElementsMatch(t, []string{"2020-07-01", "2020-07-02"}, toolErr.Missing)
}

// TestGetPeriodTotals_InvalidDateFormat_Fake mirrors the invalid-input
// coverage every other period-taking tool in this package has.
func TestGetPeriodTotals_InvalidDateFormat_Fake(t *testing.T) {
	q := newFakeQuerier()
	ctx := context.Background()

	result, toolErr, err := mcptools.GetPeriodTotals(ctx, q, mcptools.Period{Start: "01/04/2020", End: "2020-04-03"})
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, toolErr)
	require.Equal(t, "invalid_input", toolErr.Error)
}

// TestGetPeriodTotals_SingleDayPeriod_Fake proves a one-day period (start
// == end) works and does not divide-by-zero in AvgDailyMargin.
func TestGetPeriodTotals_SingleDayPeriod_Fake(t *testing.T) {
	q := newFakeQuerier()
	ctx := context.Background()

	date := sentinelDate(t, "2020-08-01")
	_, err := storage.SaveDailyReconciliation(ctx, q, reconcile.DailyReconciliation{
		Date:               date,
		GrossSalesBySource: map[string]int64{"ifood": 1000},
		MarginCents:        700,
		SourceRowRefs:      []reconcile.SourceRowRef{{File: "test.csv", Row: 1}},
	})
	require.NoError(t, err)

	result, toolErr, err := mcptools.GetPeriodTotals(ctx, q, mcptools.Period{Start: "2020-08-01", End: "2020-08-01"})
	require.NoError(t, err)
	require.Nil(t, toolErr)
	require.NotNil(t, result)

	require.Equal(t, 1, result.DaysIncluded)
	require.Equal(t, "7.00", result.MarginTotal)
	require.Equal(t, "7.00", result.AvgDailyMargin)
	require.Equal(t, "2020-08-01", result.BestDay.Date)
	require.Equal(t, "2020-08-01", result.WorstDay.Date)
}
