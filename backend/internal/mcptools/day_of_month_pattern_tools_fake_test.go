package mcptools_test

// Fake-Querier-backed coverage for get_expense_pattern_by_day_of_month
// (day_of_month_pattern_tools.go), following period_tools_fake_test.go's
// established pattern (Finding 4): this tool's core logic must run in a
// default `go test ./...` with zero Postgres dependency, not only in the
// DATABASE_URL-gated live suite.
//
// Like every other period-taking tool in this package, this one refuses
// (insufficient_data) if ANY calendar day in the requested range has no
// persisted reconciliation — so every test period below has EVERY day
// populated, via saveDayOfMonthFixtureRange filling a baseline value
// first and individual tests overriding only the specific days they care
// about, rather than each test hand-listing dozens of filler days.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/mcptools"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// saveDayOfMonthFixtureRange saves one DailyReconciliation for every day
// in [start, end] (inclusive), using a baseline 10.00 gross / 1.00
// commission / 0 refund / 0 input-cost day (expense = 1.00) for any date
// not present in overrides, and the override's own values otherwise.
// Guarantees the tool's own "no missing day" refusal never fires for a
// reason unrelated to what a given test is actually checking.
func saveDayOfMonthFixtureRange(t *testing.T, q storage.Querier, start, end time.Time, overrides map[string]struct {
	commissionCents int64
	refundCents     int64
	inputCostCents  int64
}) {
	t.Helper()
	ctx := context.Background()
	for cur := start; !cur.After(end); cur = cur.AddDate(0, 0, 1) {
		key := cur.Format("2006-01-02")
		commission, refund, inputCost := int64(100), int64(0), int64(0) // baseline expense 1.00
		if o, ok := overrides[key]; ok {
			commission, refund, inputCost = o.commissionCents, o.refundCents, o.inputCostCents
		}
		_, err := storage.SaveDailyReconciliation(ctx, q, reconcile.DailyReconciliation{
			Date:               cur,
			GrossSalesBySource: map[string]int64{"pos": 10000},
			CommissionsCents:   commission,
			RefundsCents:       refund,
			InputCostsCents:    inputCost,
			MarginCents:        10000 - commission - refund - inputCost,
			SourceRowRefs:      []reconcile.SourceRowRef{{File: "test.csv", Row: 1}},
		})
		require.NoError(t, err)
	}
}

// patternFor finds one day-of-month's entry in a result's Pattern slice,
// failing the test immediately if that day-of-month never occurred — a
// clearer failure than a nil-pointer panic on a missing lookup.
func patternFor(t *testing.T, result *mcptools.ExpensePatternByDayOfMonthResult, dayOfMonth int) mcptools.DayOfMonthExpense {
	t.Helper()
	for _, p := range result.Pattern {
		if p.DayOfMonth == dayOfMonth {
			return p
		}
	}
	t.Fatalf("day-of-month %d not present in pattern", dayOfMonth)
	return mcptools.DayOfMonthExpense{}
}

// TestGetExpensePatternByDayOfMonth_GroupsAndRanksAcrossMonths_Fake is the
// happy path: three consecutive months, each with its own day-1 and
// day-15 overridden to a distinct expense, hand-computed below (not
// derived from the tool's own output) — day-1 deliberately more expensive
// across the board so it should rank highest.
func TestGetExpensePatternByDayOfMonth_GroupsAndRanksAcrossMonths_Fake(t *testing.T) {
	q := newFakeQuerier()

	// day-of-month 1: expenses (commission+refund+input) = 30, 40, 50 ->
	//   avg = 40.00, occurrences 3
	// day-of-month 15: expenses = 5, 5, 5 -> avg = 5.00, occurrences 3
	overrides := map[string]struct {
		commissionCents int64
		refundCents     int64
		inputCostCents  int64
	}{
		"2020-01-01": {1000, 0, 2000},   // expense 3000 (30.00)
		"2020-01-15": {200, 100, 200},   // expense 500 (5.00)
		"2020-02-01": {1500, 500, 2000}, // expense 4000 (40.00)
		"2020-02-15": {200, 100, 200},   // expense 500 (5.00)
		"2020-03-01": {2000, 1000, 2000}, // expense 5000 (50.00)
		"2020-03-15": {200, 100, 200},    // expense 500 (5.00)
	}
	saveDayOfMonthFixtureRange(t, q, sentinelDate(t, "2020-01-01"), sentinelDate(t, "2020-03-15"), overrides)

	result, toolErr, err := mcptools.GetExpensePatternByDayOfMonth(context.Background(), q, mcptools.Period{Start: "2020-01-01", End: "2020-03-15"})
	require.NoError(t, err)
	require.Nil(t, toolErr)
	require.NotNil(t, result)

	day1 := patternFor(t, result, 1)
	require.Equal(t, "40.00", day1.AvgExpense, "(30+40+50)/3 = 40.00")
	require.Equal(t, 3, day1.Occurrences)

	day15 := patternFor(t, result, 15)
	require.Equal(t, "5.00", day15.AvgExpense)
	require.Equal(t, 3, day15.Occurrences)

	require.Equal(t, 1, result.HighestExpenseDay.DayOfMonth)
	require.Equal(t, "40.00", result.HighestExpenseDay.AvgExpense)
}

// TestGetExpensePatternByDayOfMonth_DisclosesLowOccurrenceCount_Fake proves
// a day-of-month that only exists in ONE of the months in range (the
// 31st, absent from February) still reports its real occurrence count
// rather than silently averaging as if it had the same sample size as
// every other day-of-month.
func TestGetExpensePatternByDayOfMonth_DisclosesLowOccurrenceCount_Fake(t *testing.T) {
	q := newFakeQuerier()
	saveDayOfMonthFixtureRange(t, q, sentinelDate(t, "2020-01-01"), sentinelDate(t, "2020-02-01"), nil)

	result, toolErr, err := mcptools.GetExpensePatternByDayOfMonth(context.Background(), q, mcptools.Period{Start: "2020-01-01", End: "2020-02-01"})
	require.NoError(t, err)
	require.Nil(t, toolErr)
	require.NotNil(t, result)

	require.Equal(t, 2, patternFor(t, result, 1).Occurrences, "the 1st occurs in both January and February")
	require.Equal(t, 1, patternFor(t, result, 31).Occurrences, "the 31st only exists in January within this range")
}

// TestGetExpensePatternByDayOfMonth_TieBreaksToTheSmallerDayOfMonth_Fake
// proves the documented tie-break: on an exact average-expense tie, the
// SMALLER day-of-month number wins both the highest and lowest slots
// (every day in this fixture shares the same baseline expense, so
// EVERY day-of-month ties).
func TestGetExpensePatternByDayOfMonth_TieBreaksToTheSmallerDayOfMonth_Fake(t *testing.T) {
	q := newFakeQuerier()
	saveDayOfMonthFixtureRange(t, q, sentinelDate(t, "2020-01-05"), sentinelDate(t, "2020-01-20"), nil)

	result, toolErr, err := mcptools.GetExpensePatternByDayOfMonth(context.Background(), q, mcptools.Period{Start: "2020-01-05", End: "2020-01-20"})
	require.NoError(t, err)
	require.Nil(t, toolErr)
	require.NotNil(t, result)

	require.Equal(t, 5, result.HighestExpenseDay.DayOfMonth, "exact tie across every day: the smallest day-of-month (5) wins")
	require.Equal(t, 5, result.LowestExpenseDay.DayOfMonth)
}

// TestGetExpensePatternByDayOfMonth_InsufficientDataWhenAnyDayMissing_Fake
// mirrors get_period_totals'/get_margin_delta's own refusal policy: any
// missing calendar day in the requested range refuses the whole
// computation, never a pattern averaged against partial data.
func TestGetExpensePatternByDayOfMonth_InsufficientDataWhenAnyDayMissing_Fake(t *testing.T) {
	q := newFakeQuerier()
	ctx := context.Background()

	d := sentinelDate(t, "2020-01-01")
	_, err := storage.SaveDailyReconciliation(ctx, q, reconcile.DailyReconciliation{
		Date:               d,
		GrossSalesBySource: map[string]int64{"pos": 1000},
		MarginCents:        1000,
		SourceRowRefs:      []reconcile.SourceRowRef{{File: "test.csv", Row: 1}},
	})
	require.NoError(t, err)
	// 2020-01-02 deliberately never saved.

	result, toolErr, err := mcptools.GetExpensePatternByDayOfMonth(ctx, q, mcptools.Period{Start: "2020-01-01", End: "2020-01-02"})
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, toolErr)
	require.Equal(t, "insufficient_data", toolErr.Error)
	require.Equal(t, []string{"2020-01-02"}, toolErr.Missing)
}

// TestGetExpensePatternByDayOfMonth_InvalidDateFormat_Fake mirrors the
// invalid-input coverage every other period-taking tool in this package
// has.
func TestGetExpensePatternByDayOfMonth_InvalidDateFormat_Fake(t *testing.T) {
	q := newFakeQuerier()
	ctx := context.Background()

	result, toolErr, err := mcptools.GetExpensePatternByDayOfMonth(ctx, q, mcptools.Period{Start: "01/01/2020", End: "2020-01-02"})
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, toolErr)
	require.Equal(t, "invalid_input", toolErr.Error)
}
