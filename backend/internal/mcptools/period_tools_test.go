package mcptools_test

// Live-Postgres integration tests for get_period_totals (period_tools.go),
// following reconciliation_tools_test.go's established pattern: genuine
// integration tests against DATABASE_URL, skipped (not faked) when it
// isn't set. period_tools_fake_test.go covers the identical logic against
// fakeQuerier with zero Postgres dependency, so this tool's core logic
// still runs in a default `go test ./...`.
//
// TestGetPeriodTotals_TotalsARealMultiDayPeriodFromFixtureData below is the
// one test in this file that reads the REAL, already-ingested fixture
// pipeline output (2026-08-01..14) rather than writing sentinel rows of its
// own — its expected numbers are NOT computed by this test or trusted from
// this tool's own output; they are copied from
// internal/reconcile/reconcile_test.go's own independently-verified golden
// values for 2026-08-01 and 2026-08-02 (themselves hand-verified twice per
// docs/plan.md's mistakes log: once via Python's Decimal with explicit
// ROUND_HALF_UP, once against the real pipeline's persisted output), so no
// fresh arithmetic error can sneak into this test's own expectations. This
// test writes nothing and cleans up nothing — a design deliberately chosen
// per the docs/plan.md "sentinel key collision" lesson recorded in
// reconciliation_tools_test.go's own doc comment (an earlier integration
// test used the real, in-range fixture date 2026-08-08 as its own test
// key, and its cleanup silently deleted real pipeline output sharing that
// key). All other tests in this file use sentinel dates far outside the
// real fixture period (2020-xx-xx), exactly like reconciliation_tools_test.go.
//
// Note on 2026-08-08: fixtures/README.md's irregularity #3 documents that
// date as having ZERO delivery-platform rows, but the real pipeline still
// computes and persists a DailyReconciliation row for it (flagged
// missing_delivery_source, not absent) — verified directly against the
// live database for this task. get_margin_delta/get_period_totals'
// insufficient_data refusal fires on a calendar day with NO persisted row
// at all, which 2026-08-08 is not; the missing-day refusal tests below
// therefore use sentinel dates with a genuine gap, the same technique
// reconciliation_tools_test.go's own TestGetMarginDelta_InsufficientDataWhenPeriodHasMissingDay
// already established, rather than 2026-08-08.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/mcptools"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// TestGetPeriodTotals_TotalsARealMultiDayPeriodFromFixtureData totals the
// real 2026-08-01..2026-08-02 window against the real, already-ingested
// fixture pipeline output. Expected figures are reconcile_test.go's own
// independently-verified golden values for those two days:
//
//	2026-08-01: ifood 69.50, just_eat_takeaway 76.25, pos 248.75,
//	            commissions 31.24, refunds 0, input_costs 320.00,
//	            margin 43.26 (TestComputeDailyReconciliations_CleanDayMatchesHandComputation)
//	2026-08-02: ifood 72.50, just_eat_takeaway 81.75, pos 223.75,
//	            commissions 25.09, refunds 34.50, input_costs 545.50,
//	            margin -227.09 (TestComputeDailyReconciliations_RefundNetsCorrectly)
//
// Summed by hand here (not by this tool, not by reconcile_test.go):
//
//	ifood:        69.50 + 72.50   = 142.00
//	just_eat:     76.25 + 81.75   = 158.00
//	pos:         248.75 + 223.75  = 472.50
//	delivery total (ifood+just_eat, pos excluded): 142.00 + 158.00 = 300.00
//	commissions:  31.24 + 25.09   =  56.33
//	refunds:       0.00 + 34.50   =  34.50
//	input_costs: 320.00 + 545.50  = 865.50
//	margin_total: 43.26 + (-227.09) = -183.83
//	avg_daily_margin: -183.83 / 2 = -91.915 -> round-half-up -> -91.92
//	best_day: 2026-08-01 (43.26); worst_day: 2026-08-02 (-227.09)
func TestGetPeriodTotals_TotalsARealMultiDayPeriodFromFixtureData(t *testing.T) {
	_, q := connectAndQueries(t)
	ctx := context.Background()

	result, toolErr, err := mcptools.GetPeriodTotals(ctx, q, mcptools.Period{Start: "2026-08-01", End: "2026-08-02"})
	require.NoError(t, err)
	require.Nil(t, toolErr, "the real fixture pipeline must already have both days persisted")
	require.NotNil(t, result)

	require.Equal(t, 2, result.DaysIncluded)
	require.Equal(t, "142.00", result.GrossSalesBySource["ifood"])
	require.Equal(t, "158.00", result.GrossSalesBySource["just_eat_takeaway"])
	require.Equal(t, "472.50", result.GrossSalesBySource["pos"])
	require.Equal(t, "300.00", result.TotalDeliveryGrossSales, "142.00 (ifood) + 158.00 (just_eat_takeaway); pos excluded")
	require.Equal(t, "56.33", result.Commissions)
	require.Equal(t, "34.50", result.Refunds)
	require.Equal(t, "34.50", result.RefundsBySource["ifood"], "A15: fixtures/README.md's only refund in this window (order 0007) is iFood's")
	require.NotContains(t, result.RefundsBySource, "just_eat_takeaway", "no Just Eat Takeaway refund exists in this window")
	require.Equal(t, "865.50", result.InputCosts)
	require.Equal(t, "-183.83", result.MarginTotal, "43.26 + (-227.09)")
	require.Equal(t, "-91.92", result.AvgDailyMargin, "-183.83 / 2 = -91.915, round-half-up to -91.92")
	require.Equal(t, "2026-08-01", result.BestDay.Date)
	require.Equal(t, "43.26", result.BestDay.Margin)
	require.Equal(t, "2026-08-02", result.WorstDay.Date)
	require.Equal(t, "-227.09", result.WorstDay.Margin)
	require.NotEmpty(t, result.SourceRowRefs)
}

// TestGetPeriodTotals_InsufficientDataWhenPeriodHasMissingDay mirrors
// TestGetMarginDelta_InsufficientDataWhenPeriodHasMissingDay's exact
// technique: a sentinel period with one real gap in the middle must
// refuse, not silently total the two days that do exist.
func TestGetPeriodTotals_InsufficientDataWhenPeriodHasMissingDay(t *testing.T) {
	conn, q := connectAndQueries(t)
	ctx := context.Background()

	// 1999-04-10 and 1999-04-12 exist; 1999-04-11 (in between) does not.
	for _, ds := range []string{"1999-04-10", "1999-04-12"} {
		date := sentinelDate(t, ds)
		deleteDay(t, conn, date)
		_, err := storage.SaveDailyReconciliation(ctx, q, reconcile.DailyReconciliation{
			Date:        date,
			MarginCents: 100,
		})
		require.NoError(t, err)
	}

	result, toolErr, err := mcptools.GetPeriodTotals(ctx, q, mcptools.Period{Start: "1999-04-10", End: "1999-04-12"})
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, toolErr)
	require.Equal(t, "insufficient_data", toolErr.Error)
	require.Contains(t, toolErr.Missing, "1999-04-11")
}

// TestGetPeriodTotals_BestAndWorstDayPickTheRealCorrectDates writes three
// sentinel days with distinct margins and asserts best_day/worst_day name
// the actual correct dates, not just the actual correct amounts.
func TestGetPeriodTotals_BestAndWorstDayPickTheRealCorrectDates(t *testing.T) {
	conn, q := connectAndQueries(t)
	ctx := context.Background()

	days := []struct {
		date        string
		marginCents int64
	}{
		{"1999-04-20", 1500}, // best
		{"1999-04-21", -500}, // worst
		{"1999-04-22", 700},
	}
	for _, d := range days {
		date := sentinelDate(t, d.date)
		deleteDay(t, conn, date)
		_, err := storage.SaveDailyReconciliation(ctx, q, reconcile.DailyReconciliation{
			Date:               date,
			GrossSalesBySource: map[string]int64{"pos": d.marginCents},
			MarginCents:        d.marginCents,
			SourceRowRefs:      []reconcile.SourceRowRef{{File: "test.csv", Row: 1}},
		})
		require.NoError(t, err)
	}

	result, toolErr, err := mcptools.GetPeriodTotals(ctx, q, mcptools.Period{Start: "1999-04-20", End: "1999-04-22"})
	require.NoError(t, err)
	require.Nil(t, toolErr)
	require.NotNil(t, result)

	require.Equal(t, 3, result.DaysIncluded)
	require.Equal(t, "1999-04-20", result.BestDay.Date)
	require.Equal(t, "15.00", result.BestDay.Margin)
	require.Equal(t, "1999-04-21", result.WorstDay.Date)
	require.Equal(t, "-5.00", result.WorstDay.Margin)
	require.Equal(t, "17.00", result.MarginTotal, "15.00 + (-5.00) + 7.00")
}

// TestGetPeriodTotals_InvalidDateFormat mirrors every other period-taking
// tool's invalid-input coverage.
func TestGetPeriodTotals_InvalidDateFormat(t *testing.T) {
	_, q := connectAndQueries(t)
	ctx := context.Background()

	result, toolErr, err := mcptools.GetPeriodTotals(ctx, q, mcptools.Period{Start: "04/20/1999", End: "1999-04-22"})
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, toolErr)
	require.Equal(t, "invalid_input", toolErr.Error)
}
