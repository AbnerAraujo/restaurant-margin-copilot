package mcptools_test

// These are genuine integration tests against a live PostgreSQL instance
// (DATABASE_URL) — not a mock or a fake Querier — following the same
// pattern internal/storage/reconciliation_test.go already established for
// User Story 1. A lightweight fake would have been simpler to wire up, but
// this package's whole job is to be the boundary between the model and the
// real reconciliation.Queries SQL (Constitution Principle III), so proving
// it against the real thing is worth the extra setup.
//
// Every row this file writes uses a sentinel date far outside the real
// fixture period (2026-08-01..14) — 1999-02-xx / 1999-03-xx — per the
// lesson recorded in docs/plan.md's mistakes log: an earlier integration
// test in this project once used an in-range fixture date as its own
// primary key and its cleanup silently deleted real pipeline output that
// happened to share it. Dates here are also kept distinct from
// reconciliation_test.go's own sentinel (1999-01-01) so the two test files
// can never collide even if a future change makes them share a table.
//
// Tests are skipped (not faked) when DATABASE_URL isn't set.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/mcptools"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

func sentinelDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	require.NoError(t, err)
	return d
}

// deleteDay is registered directly with t.Cleanup by each test (rather than
// inside saveSentinelDay) so it can share the same live connection the test
// itself is using, keeping the LIFO close-after-delete ordering explicit at
// the call site.
func deleteDay(t *testing.T, conn *pgx.Conn, date time.Time) {
	t.Helper()
	t.Cleanup(func() {
		_, err := conn.Exec(context.Background(), "DELETE FROM daily_reconciliation WHERE date = $1", date)
		if err != nil {
			t.Logf("cleanup: failed to delete sentinel row for %s: %v", date.Format("2006-01-02"), err)
		}
	})
}

// connectAndQueries connects once and returns both the raw *pgx.Conn (for
// this file's own cleanup deletes) and the *storage.Queries built over it,
// with the connection's Close registered first so it always runs last.
func connectAndQueries(t *testing.T) (*pgx.Conn, *storage.Queries) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping live-Postgres integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err, "must be able to connect to the live Postgres instance at DATABASE_URL")
	t.Cleanup(func() { conn.Close(context.Background()) })

	return conn, storage.New(conn)
}

func TestGetDailySummary_ReturnsPersistedDay(t *testing.T) {
	conn, q := connectAndQueries(t)
	date := sentinelDate(t, "1999-03-01")
	deleteDay(t, conn, date)

	day := reconcile.DailyReconciliation{
		Date:               date,
		GrossSalesBySource: map[string]int64{"ifood": 5000, "pos": 3000},
		CommissionsCents:   1150,
		RefundsCents:       200,
		InputCostsCents:    1000,
		MarginCents:        5650, // 5000+3000-1150-200-1000
		DiscrepancyFlags: []reconcile.DiscrepancyFlag{
			{Type: reconcile.FlagAnomalyThresholdExceeded, Detail: "test flag"},
		},
		SourceRowRefs: []reconcile.SourceRowRef{{File: "test.csv", Row: 2}},
	}
	ctx := context.Background()
	_, err := storage.SaveDailyReconciliation(ctx, q, day)
	require.NoError(t, err)

	result, toolErr, err := mcptools.GetDailySummary(ctx, q, "1999-03-01")
	require.NoError(t, err)
	require.Nil(t, toolErr)
	require.NotNil(t, result)

	require.Equal(t, "1999-03-01", result.Date)
	require.Equal(t, "50.00", result.GrossSalesBySource["ifood"])
	require.Equal(t, "30.00", result.GrossSalesBySource["pos"])
	require.Equal(t, "11.50", result.Commissions)
	require.Equal(t, "2.00", result.Refunds)
	require.Equal(t, "10.00", result.InputCosts)
	require.Equal(t, "56.50", result.Margin)
	require.Len(t, result.DiscrepancyFlags, 1)
	require.Equal(t, reconcile.FlagAnomalyThresholdExceeded, result.DiscrepancyFlags[0].Type)
	require.Equal(t, []reconcile.SourceRowRef{{File: "test.csv", Row: 2}}, result.SourceRowRefs)
}

func TestGetDailySummary_NoDataForUnknownDate(t *testing.T) {
	_, q := connectAndQueries(t)
	ctx := context.Background()

	// 1999-03-15 is never written by any test in this file.
	result, toolErr, err := mcptools.GetDailySummary(ctx, q, "1999-03-15")
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, toolErr)
	require.Equal(t, "no_data", toolErr.Error)
	require.NotEmpty(t, toolErr.Missing)
}

func TestGetDailySummary_InvalidDateFormat(t *testing.T) {
	_, q := connectAndQueries(t)
	ctx := context.Background()

	result, toolErr, err := mcptools.GetDailySummary(ctx, q, "03/01/1999")
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, toolErr)
	require.Equal(t, "invalid_input", toolErr.Error)
}

func TestGetMarginDelta_ComputesDeltaAcrossTwoFullPeriods(t *testing.T) {
	conn, q := connectAndQueries(t)
	ctx := context.Background()

	// Period A: 1999-02-01..1999-02-02, margins 10.00 + 20.00 = 30.00
	// Period B: 1999-02-05..1999-02-06, margins 25.00 + 45.00 = 70.00
	// Delta (B - A) = 40.00
	days := []struct {
		date        string
		marginCents int64
	}{
		{"1999-02-01", 1000},
		{"1999-02-02", 2000},
		{"1999-02-05", 2500},
		{"1999-02-06", 4500},
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

	result, toolErr, err := mcptools.GetMarginDelta(ctx, q,
		mcptools.Period{Start: "1999-02-01", End: "1999-02-02"},
		mcptools.Period{Start: "1999-02-05", End: "1999-02-06"},
	)
	require.NoError(t, err)
	require.Nil(t, toolErr)
	require.NotNil(t, result)

	require.Equal(t, "30.00", result.PeriodA.MarginTotal)
	require.Equal(t, 2, result.PeriodA.DaysIncluded)
	require.Equal(t, "70.00", result.PeriodB.MarginTotal)
	require.Equal(t, 2, result.PeriodB.DaysIncluded)
	require.Equal(t, "40.00", result.DeltaMarginTotal)
	require.Len(t, result.PeriodA.SourceRowRefs, 2)
	require.Len(t, result.PeriodB.SourceRowRefs, 2)
}

func TestGetMarginDelta_InsufficientDataWhenPeriodHasMissingDay(t *testing.T) {
	conn, q := connectAndQueries(t)
	ctx := context.Background()

	// 1999-02-10 and 1999-02-12 exist; 1999-02-11 (in between) does not —
	// the period 10..12 must refuse, not silently compute a 2-day delta.
	for _, ds := range []string{"1999-02-10", "1999-02-12"} {
		date := sentinelDate(t, ds)
		deleteDay(t, conn, date)
		_, err := storage.SaveDailyReconciliation(ctx, q, reconcile.DailyReconciliation{
			Date:        date,
			MarginCents: 100,
		})
		require.NoError(t, err)
	}

	result, toolErr, err := mcptools.GetMarginDelta(ctx, q,
		mcptools.Period{Start: "1999-02-10", End: "1999-02-12"},
		mcptools.Period{Start: "1999-02-10", End: "1999-02-10"},
	)
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, toolErr)
	require.Equal(t, "insufficient_data", toolErr.Error)
	require.Contains(t, toolErr.Missing, "1999-02-11")
}

func TestListDiscrepancies_ByDate_ReturnsFlags(t *testing.T) {
	conn, q := connectAndQueries(t)
	ctx := context.Background()

	date := sentinelDate(t, "1999-03-20")
	deleteDay(t, conn, date)
	_, err := storage.SaveDailyReconciliation(ctx, q, reconcile.DailyReconciliation{
		Date: date,
		DiscrepancyFlags: []reconcile.DiscrepancyFlag{
			{Type: reconcile.FlagDuplicateOrderRemoved, Detail: "order X duplicated"},
		},
		SourceRowRefs: []reconcile.SourceRowRef{{File: "d.csv", Row: 5}},
	})
	require.NoError(t, err)

	result, toolErr, err := mcptools.ListDiscrepancies(ctx, q, "1999-03-20", nil)
	require.NoError(t, err)
	require.Nil(t, toolErr)
	require.NotNil(t, result)
	require.Equal(t, 1, result.DaysChecked)
	require.Len(t, result.Days, 1)
	require.Equal(t, "1999-03-20", result.Days[0].Date)
	require.Equal(t, reconcile.FlagDuplicateOrderRemoved, result.Days[0].Flags[0].Type)
}

func TestListDiscrepancies_ByPeriod_OmitsDaysWithNoFlags(t *testing.T) {
	conn, q := connectAndQueries(t)
	ctx := context.Background()

	flaggedDate := sentinelDate(t, "1999-03-25")
	cleanDate := sentinelDate(t, "1999-03-26")
	deleteDay(t, conn, flaggedDate)
	deleteDay(t, conn, cleanDate)

	_, err := storage.SaveDailyReconciliation(ctx, q, reconcile.DailyReconciliation{
		Date: flaggedDate,
		DiscrepancyFlags: []reconcile.DiscrepancyFlag{
			{Type: reconcile.FlagMissingDeliverySource, Detail: "no delivery rows"},
		},
	})
	require.NoError(t, err)
	_, err = storage.SaveDailyReconciliation(ctx, q, reconcile.DailyReconciliation{Date: cleanDate})
	require.NoError(t, err)

	result, toolErr, err := mcptools.ListDiscrepancies(ctx, q, "", &mcptools.Period{Start: "1999-03-25", End: "1999-03-26"})
	require.NoError(t, err)
	require.Nil(t, toolErr)
	require.NotNil(t, result)
	require.Equal(t, 2, result.DaysChecked, "both persisted days should be counted as checked")
	require.Len(t, result.Days, 1, "only the flagged day should be reported")
	require.Equal(t, "1999-03-25", result.Days[0].Date)
}

func TestListDiscrepancies_RejectsBothDateAndPeriod(t *testing.T) {
	_, q := connectAndQueries(t)
	ctx := context.Background()

	result, toolErr, err := mcptools.ListDiscrepancies(ctx, q, "1999-03-25", &mcptools.Period{Start: "1999-03-25", End: "1999-03-26"})
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, toolErr)
	require.Equal(t, "invalid_input", toolErr.Error)
}

func TestListDiscrepancies_RejectsNeitherDateNorPeriod(t *testing.T) {
	_, q := connectAndQueries(t)
	ctx := context.Background()

	result, toolErr, err := mcptools.ListDiscrepancies(ctx, q, "", nil)
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, toolErr)
	require.Equal(t, "invalid_input", toolErr.Error)
}
