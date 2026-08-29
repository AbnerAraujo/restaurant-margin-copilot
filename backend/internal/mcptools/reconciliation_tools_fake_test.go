package mcptools_test

// Finding 4: reconciliation_tools_test.go's tests all skip when
// DATABASE_URL isn't set, leaving get_margin_delta's "refuse rather than
// compute over partial data" rule (periodMargin's missing-day check,
// reconciliation_tools.go) completely untested in a default `go test ./...`
// run. These tests exercise the exact same mcptools core functions against
// fakeQuerier (fake_querier_test.go) instead of live Postgres, so the logic
// runs with zero DATABASE_URL dependency. Mirrors reconciliation_tools_test.go's
// scenarios and assertions; the live-gated tests are kept as-is.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/mcptools"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// TestGetDailySummary_ReturnsPersistedDay_Fake proves the fake is correct
// against a known-good case, not just wired up to pass the refusal path:
// same fixture-shaped values and assertions as
// reconciliation_tools_test.go's live TestGetDailySummary_ReturnsPersistedDay.
func TestGetDailySummary_ReturnsPersistedDay_Fake(t *testing.T) {
	q := newFakeQuerier()
	ctx := context.Background()
	date := sentinelDate(t, "2020-01-01")

	day := reconcile.DailyReconciliation{
		Date:               date,
		GrossSalesBySource: map[string]int64{"ifood": 5000, "pos": 3000},
		CommissionsCents:   1150,
		RefundsCents:       200,
		RefundsBySource:    map[string]int64{"ifood": 200},
		InputCostsCents:    1000,
		MarginCents:        5650, // 5000+3000-1150-200-1000
		DiscrepancyFlags: []reconcile.DiscrepancyFlag{
			{Type: reconcile.FlagAnomalyThresholdExceeded, Detail: "test flag"},
		},
		SourceRowRefs: []reconcile.SourceRowRef{{File: "test.csv", Row: 2}},
	}
	_, err := storage.SaveDailyReconciliation(ctx, q, day)
	require.NoError(t, err)

	result, toolErr, err := mcptools.GetDailySummary(ctx, q, "2020-01-01")
	require.NoError(t, err)
	require.Nil(t, toolErr)
	require.NotNil(t, result)

	require.Equal(t, "2020-01-01", result.Date)
	require.Equal(t, "50.00", result.GrossSalesBySource["ifood"])
	require.Equal(t, "30.00", result.GrossSalesBySource["pos"])
	require.Equal(t, "50.00", result.TotalDeliveryGrossSales, "must be ifood only (50.00) — pos (30.00) is dine-in, not delivery revenue")
	require.Equal(t, "11.50", result.Commissions)
	require.Equal(t, "2.00", result.Refunds)
	require.Equal(t, "2.00", result.RefundsBySource["ifood"], "A15: the day's only refund must be attributable to the platform it actually came from")
	require.NotContains(t, result.RefundsBySource, "pos", "POS never contributes to RefundsBySource")
	require.Equal(t, "10.00", result.InputCosts)
	require.Equal(t, "56.50", result.Margin)
	require.Len(t, result.DiscrepancyFlags, 1)
	require.Equal(t, reconcile.FlagAnomalyThresholdExceeded, result.DiscrepancyFlags[0].Type)
	require.Equal(t, []reconcile.SourceRowRef{{File: "test.csv", Row: 2}}, result.SourceRowRefs)
}

func TestGetDailySummary_NoDataForUnknownDate_Fake(t *testing.T) {
	q := newFakeQuerier()
	ctx := context.Background()

	result, toolErr, err := mcptools.GetDailySummary(ctx, q, "2020-03-15")
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, toolErr)
	require.Equal(t, "no_data", toolErr.Error)
	require.NotEmpty(t, toolErr.Missing)
}

func TestGetDailySummary_InvalidDateFormat_Fake(t *testing.T) {
	q := newFakeQuerier()
	ctx := context.Background()

	result, toolErr, err := mcptools.GetDailySummary(ctx, q, "03/01/2020")
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, toolErr)
	require.Equal(t, "invalid_input", toolErr.Error)
}

// TestGetMarginDelta_ComputesDeltaAcrossTwoFullPeriods_Fake is the happy
// path proving the fake behaves correctly on a known-good case (mirrors the
// live test's exact numbers).
func TestGetMarginDelta_ComputesDeltaAcrossTwoFullPeriods_Fake(t *testing.T) {
	q := newFakeQuerier()
	ctx := context.Background()

	// Period A: 2020-02-01..2020-02-02, margins 10.00 + 20.00 = 30.00
	// Period B: 2020-02-05..2020-02-06, margins 25.00 + 45.00 = 70.00
	// Delta (B - A) = 40.00
	days := []struct {
		date        string
		marginCents int64
	}{
		{"2020-02-01", 1000},
		{"2020-02-02", 2000},
		{"2020-02-05", 2500},
		{"2020-02-06", 4500},
	}
	for i, d := range days {
		date := sentinelDate(t, d.date)
		_, err := storage.SaveDailyReconciliation(ctx, q, reconcile.DailyReconciliation{
			Date:               date,
			GrossSalesBySource: map[string]int64{"pos": d.marginCents},
			MarginCents:        d.marginCents,
			// A distinct row per day, not all the same row — exercises
			// collapseSourceRowRefsByFile's real min/max collapsing (see
			// period_tools.go's doc comment) instead of trivially
			// collapsing identical refs.
			SourceRowRefs: []reconcile.SourceRowRef{{File: "test.csv", Row: i + 1}},
		})
		require.NoError(t, err)
	}

	result, toolErr, err := mcptools.GetMarginDelta(ctx, q,
		mcptools.Period{Start: "2020-02-01", End: "2020-02-02"},
		mcptools.Period{Start: "2020-02-05", End: "2020-02-06"},
	)
	require.NoError(t, err)
	require.Nil(t, toolErr)
	require.NotNil(t, result)

	require.Equal(t, "30.00", result.PeriodA.MarginTotal)
	require.Equal(t, 2, result.PeriodA.DaysIncluded)
	require.Equal(t, "70.00", result.PeriodB.MarginTotal)
	require.Equal(t, 2, result.PeriodB.DaysIncluded)
	require.Equal(t, "40.00", result.DeltaMarginTotal)
	// Collapsed to the min/max row per file, not one entry per day (rows
	// 1,2 for period A -> [1,2]; rows 3,4 for period B -> [3,4]).
	require.Equal(t, []reconcile.SourceRowRef{
		{File: "test.csv", Row: 1},
		{File: "test.csv", Row: 2},
	}, result.PeriodA.SourceRowRefs)
	require.Equal(t, []reconcile.SourceRowRef{
		{File: "test.csv", Row: 3},
		{File: "test.csv", Row: 4},
	}, result.PeriodB.SourceRowRefs)
}

// TestGetMarginDelta_InsufficientDataWhenPeriodHasMissingDay_Fake is the
// core Finding 4 test: periodMargin's missing-day refusal
// (reconciliation_tools.go) — the constitution's refuse-rather-than-guess
// rule enforced at the tool boundary — now runs in every default
// `go test ./...`, not only when DATABASE_URL happens to be set.
func TestGetMarginDelta_InsufficientDataWhenPeriodHasMissingDay_Fake(t *testing.T) {
	q := newFakeQuerier()
	ctx := context.Background()

	// 2020-02-10 and 2020-02-12 exist; 2020-02-11 (in between) does not —
	// the period 10..12 must refuse, not silently compute a 2-day delta.
	for _, ds := range []string{"2020-02-10", "2020-02-12"} {
		date := sentinelDate(t, ds)
		_, err := storage.SaveDailyReconciliation(ctx, q, reconcile.DailyReconciliation{
			Date:        date,
			MarginCents: 100,
		})
		require.NoError(t, err)
	}

	result, toolErr, err := mcptools.GetMarginDelta(ctx, q,
		mcptools.Period{Start: "2020-02-10", End: "2020-02-12"},
		mcptools.Period{Start: "2020-02-10", End: "2020-02-10"},
	)
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, toolErr)
	require.Equal(t, "insufficient_data", toolErr.Error)
	require.Contains(t, toolErr.Missing, "2020-02-11")
}

// TestGetMarginDelta_InsufficientDataWhenPeriodIsEntirelyMissing_Fake covers
// the boundary the live suite doesn't: a period with ZERO persisted days at
// all (not just one gap in an otherwise-populated range) must still refuse.
func TestGetMarginDelta_InsufficientDataWhenPeriodIsEntirelyMissing_Fake(t *testing.T) {
	q := newFakeQuerier()
	ctx := context.Background()

	result, toolErr, err := mcptools.GetMarginDelta(ctx, q,
		mcptools.Period{Start: "2020-05-01", End: "2020-05-02"},
		mcptools.Period{Start: "2020-05-01", End: "2020-05-01"},
	)
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, toolErr)
	require.Equal(t, "insufficient_data", toolErr.Error)
	require.ElementsMatch(t, []string{"2020-05-01", "2020-05-02"}, toolErr.Missing)
}

func TestListDiscrepancies_ByDate_ReturnsFlags_Fake(t *testing.T) {
	q := newFakeQuerier()
	ctx := context.Background()

	date := sentinelDate(t, "2020-03-20")
	_, err := storage.SaveDailyReconciliation(ctx, q, reconcile.DailyReconciliation{
		Date: date,
		DiscrepancyFlags: []reconcile.DiscrepancyFlag{
			{Type: reconcile.FlagDuplicateOrderRemoved, Detail: "order X duplicated"},
		},
		SourceRowRefs: []reconcile.SourceRowRef{{File: "d.csv", Row: 5}},
	})
	require.NoError(t, err)

	result, toolErr, err := mcptools.ListDiscrepancies(ctx, q, "2020-03-20", nil)
	require.NoError(t, err)
	require.Nil(t, toolErr)
	require.NotNil(t, result)
	require.Equal(t, 1, result.DaysChecked)
	require.Len(t, result.Days, 1)
	require.Equal(t, "2020-03-20", result.Days[0].Date)
	require.Equal(t, reconcile.FlagDuplicateOrderRemoved, result.Days[0].Flags[0].Type)
}

func TestListDiscrepancies_ByPeriod_OmitsDaysWithNoFlags_Fake(t *testing.T) {
	q := newFakeQuerier()
	ctx := context.Background()

	flaggedDate := sentinelDate(t, "2020-03-25")
	cleanDate := sentinelDate(t, "2020-03-26")

	_, err := storage.SaveDailyReconciliation(ctx, q, reconcile.DailyReconciliation{
		Date: flaggedDate,
		DiscrepancyFlags: []reconcile.DiscrepancyFlag{
			{Type: reconcile.FlagMissingDeliverySource, Detail: "no delivery rows"},
		},
	})
	require.NoError(t, err)
	_, err = storage.SaveDailyReconciliation(ctx, q, reconcile.DailyReconciliation{Date: cleanDate})
	require.NoError(t, err)

	result, toolErr, err := mcptools.ListDiscrepancies(ctx, q, "", &mcptools.Period{Start: "2020-03-25", End: "2020-03-26"})
	require.NoError(t, err)
	require.Nil(t, toolErr)
	require.NotNil(t, result)
	require.Equal(t, 2, result.DaysChecked, "both persisted days should be counted as checked")
	require.Len(t, result.Days, 1, "only the flagged day should be reported")
	require.Equal(t, "2020-03-25", result.Days[0].Date)
}

func TestListDiscrepancies_RejectsBothDateAndPeriod_Fake(t *testing.T) {
	q := newFakeQuerier()
	ctx := context.Background()

	result, toolErr, err := mcptools.ListDiscrepancies(ctx, q, "2020-03-25", &mcptools.Period{Start: "2020-03-25", End: "2020-03-26"})
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, toolErr)
	require.Equal(t, "invalid_input", toolErr.Error)
}

func TestListDiscrepancies_RejectsNeitherDateNorPeriod_Fake(t *testing.T) {
	q := newFakeQuerier()
	ctx := context.Background()

	result, toolErr, err := mcptools.ListDiscrepancies(ctx, q, "", nil)
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, toolErr)
	require.Equal(t, "invalid_input", toolErr.Error)
}
