package storage_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

const (
	fixtureDeliveryFile = "../../fixtures/delivery_platform_export.csv"
	fixturePOSFile      = "../../fixtures/pos_export.csv"
	fixtureCostFile     = "../../fixtures/supplier_cost_sheet.csv"
)

func loadFixtureDays(t *testing.T) []reconcile.DailyReconciliation {
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

	return reconcile.ComputeDailyReconciliations(delivery, pos, costs)
}

func findFixtureDay(t *testing.T, days []reconcile.DailyReconciliation, date string) reconcile.DailyReconciliation {
	t.Helper()
	want, err := time.Parse("2006-01-02", date)
	require.NoError(t, err)
	for _, d := range days {
		if d.Date.Equal(want) {
			return d
		}
	}
	t.Fatalf("no DailyReconciliation for %s", date)
	return reconcile.DailyReconciliation{}
}

// TestSaveAndLoadDailyReconciliation_RoundTripsExactly is a genuine
// integration test against a live PostgreSQL instance (DATABASE_URL) — not
// a mock. It computes a real DailyReconciliation from
// backend/fixtures/ via internal/ingest + internal/reconcile (the exact
// pipeline T017 wires into cmd/server), persists it, reads it back, and
// asserts every field — including the jsonb columns — matches exactly.
//
// It uses 2026-08-08 (fixtures/README.md irregularity #3: the missing
// delivery-platform day) deliberately, since that day exercises the most
// jsonb surface: a non-empty discrepancy_flags array and a
// gross_sales_by_source map missing the "ifood"/"just_eat_takeaway" keys
// entirely, not just zeroed.
//
// The test is skipped (not faked) when DATABASE_URL isn't set.
func TestSaveAndLoadDailyReconciliation_RoundTripsExactly(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping live-Postgres integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err, "must be able to connect to the live Postgres instance at DATABASE_URL")
	// Registered before the delete cleanup below so it runs LAST (t.Cleanup
	// is LIFO): closing the connection before the delete cleanup runs would
	// silently no-op the delete and leak the test row.
	t.Cleanup(func() { conn.Close(context.Background()) })

	q := storage.New(conn)

	days := loadFixtureDays(t)
	target := findFixtureDay(t, days, "2026-08-08")
	require.NotEmpty(t, target.DiscrepancyFlags, "sanity check: this day must carry the missing_delivery_source flag to exercise jsonb round-trip")
	require.NotEmpty(t, target.SourceRowRefs)
	require.NotContains(t, target.GrossSalesBySource, "ifood", "sanity check: this day has zero delivery rows, so the gross_sales_by_source map must omit the key entirely")

	// Retarget onto a sentinel date far outside the real fixture period
	// (2026-08-01..14) before touching the live database. This test shares
	// a live Postgres instance with real `cmd/server -ingest` pipeline runs
	// (see quickstart.md) — an earlier version of this test used the real
	// 2026-08-08 as both its subject AND its primary key, so its own
	// cleanup (`DELETE WHERE date = target.Date`) silently deleted the real
	// pipeline's legitimately-computed row for that day when both ran
	// against the same database (caught in independent verification, see
	// docs/plan.md's mistakes log). Keeping every other field from the real
	// 2026-08-08 computation preserves exactly what this test needs to
	// exercise (the missing-delivery jsonb shape); only the primary key
	// changes, so cleanup can never touch real data again.
	target.Date = time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Cleanup(func() {
		_, err := conn.Exec(context.Background(), "DELETE FROM daily_reconciliation WHERE date = $1", target.Date)
		if err != nil {
			t.Logf("cleanup: failed to delete test row for %s: %v", target.Date.Format("2006-01-02"), err)
		}
	})

	saved, err := storage.SaveDailyReconciliation(ctx, q, target)
	require.NoError(t, err)
	require.True(t, saved.Date.Valid)

	loaded, err := storage.LoadDailyReconciliation(ctx, q, target.Date)
	require.NoError(t, err)

	require.True(t, target.Date.Equal(loaded.Date), "date must round-trip exactly")
	require.Equal(t, target.GrossSalesBySource, loaded.GrossSalesBySource, "gross_sales_by_source jsonb must round-trip exactly, including the absence of the ifood/just_eat_takeaway keys")
	require.Equal(t, target.CommissionsCents, loaded.CommissionsCents)
	require.Equal(t, target.CommissionsBySource, loaded.CommissionsBySource, "commissions_by_source jsonb (specs/003-platform-comparator) must round-trip exactly, including the absence of the ifood/just_eat_takeaway keys on this missing-delivery-day fixture")
	require.Equal(t, target.RefundsCents, loaded.RefundsCents)
	require.Equal(t, target.RefundsBySource, loaded.RefundsBySource, "refunds_by_source jsonb (A15, docs/product-strategy.md) must round-trip exactly, including being empty on this refund-free fixture day")
	require.Equal(t, target.InputCostsCents, loaded.InputCostsCents)
	require.Equal(t, target.MarginCents, loaded.MarginCents, "margin must round-trip exactly — a rounding-mode mismatch here would silently corrupt the one number Constitution Principle I says must never be wrong")
	require.ElementsMatch(t, target.DiscrepancyFlags, loaded.DiscrepancyFlags, "discrepancy_flags jsonb must round-trip exactly")
	require.ElementsMatch(t, target.SourceRowRefs, loaded.SourceRowRefs, "source_row_refs jsonb (provenance) must round-trip exactly — Constitution Principle IV")

	// UpsertDailyReconciliation must be idempotent on date: re-running the
	// pipeline for a day already reconciled (e.g. a corrected re-ingest)
	// must update the existing row, not append a duplicate.
	saved2, err := storage.SaveDailyReconciliation(ctx, q, target)
	require.NoError(t, err)
	require.Equal(t, saved.Date, saved2.Date)

	var count int
	row := conn.QueryRow(ctx, "SELECT count(*) FROM daily_reconciliation WHERE date = $1", target.Date)
	require.NoError(t, row.Scan(&count))
	require.Equal(t, 1, count, "UpsertDailyReconciliation must not append a duplicate row on re-save")
}
