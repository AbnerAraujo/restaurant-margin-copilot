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
	openingDeliveryFile = "../../cmd/gendata/opening/delivery_platform_export.csv"
	openingPOSFile      = "../../cmd/gendata/opening/pos_export.csv"
	openingCostFile     = "../../cmd/gendata/opening/supplier_cost_sheet.csv"
)

func loadOpeningDays(t *testing.T) []reconcile.DailyReconciliation {
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

	return reconcile.ComputeDailyReconciliations(delivery, pos, costs)
}

func findOpeningDay(t *testing.T, days []reconcile.DailyReconciliation, date string) reconcile.DailyReconciliation {
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
// a mock. It computes a real DailyReconciliation from the dataset's
// hand-authored opening window via internal/ingest + internal/reconcile
// (the exact pipeline T017 wires into cmd/server), persists it, reads it
// back, and asserts every field — including the jsonb columns — matches
// exactly.
//
// It uses 2024-08-10 (opening/README.md irregularity #3: the missing
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

	days := loadOpeningDays(t)
	target := findOpeningDay(t, days, "2024-08-10")
	require.NotEmpty(t, target.DiscrepancyFlags, "sanity check: this day must carry the missing_delivery_source flag to exercise jsonb round-trip")
	require.NotEmpty(t, target.SourceRowRefs)
	require.NotContains(t, target.GrossSalesBySource, "ifood", "sanity check: this day has zero delivery rows, so the gross_sales_by_source map must omit the key entirely")

	// Retarget onto a sentinel date far outside the real dataset period
	// before touching the live database. This test shares a live Postgres
	// instance with real `cmd/server -ingest` pipeline runs (see
	// quickstart.md) — an earlier version of this test used the real
	// missing-delivery day as both its subject AND its primary key, so its own
	// cleanup (`DELETE WHERE date = target.Date`) silently deleted the real
	// pipeline's legitimately-computed row for that day when both ran
	// against the same database (caught in independent verification, see
	// docs/plan.md's mistakes log). Keeping every other field from the real
	// 2024-08-10 computation preserves exactly what this test needs to
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
	require.Equal(t, target.CommissionsBySource, loaded.CommissionsBySource, "commissions_by_source jsonb (specs/003-platform-comparator) must round-trip exactly, including the absence of the ifood/just_eat_takeaway keys on this missing-delivery day")
	require.Equal(t, target.RefundsCents, loaded.RefundsCents)
	require.Equal(t, target.RefundsBySource, loaded.RefundsBySource, "refunds_by_source jsonb (A15, docs/product-strategy.md) must round-trip exactly, including being empty on this refund-free day")
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

// TestOpeningWindow_PersistedWithZeroSilentDataLoss is KR2's regression
// guard: "zero silent data loss on the messy test data".
//
// internal/ingest and internal/reconcile each already assert every
// deliberate irregularity IN MEMORY, straight off the checked-in CSVs
// (ingest_test.go, reconcile_test.go). What no test asserted before this
// one is the step KR2 is actually stated over — that those results survive
// PERSISTENCE. That gap is not hypothetical: the single worst defect of
// this build (docs/plan.md's mistakes log; the deck's mistakes slide) was
// exactly a persisted-state loss — 13 of 14 opening days in Postgres,
// short by $152.50, with every in-memory test still green, caught only by
// a hand-run psql query. This test is that psql query, made permanent.
//
// It reads the real rows the `-ingest` pipeline wrote and asserts, against
// the independently hand-computed values in
// backend/cmd/gendata/opening/README.md (never against the engine's own
// output):
//
//   - no day silently dropped: all 14 dates present, exactly once each;
//   - irregularity #1, duplicate order: 2024-08-03 persisted with the
//     duplicate counted ONCE, and the flag saying so;
//   - irregularity #2, refund: 2024-08-02 persisted with the refund netted
//     at the ORIGINAL order date, gross and refund both still legible;
//   - irregularity #3, missing day: 2024-08-10 persisted with the delivery
//     keys ABSENT and flagged — never silently zeroed;
//   - irregularity #4, inconsistent date format: the POS export's
//     DD/MM/YYYY rows landed on the right days, proven where a
//     day/month swap would be unambiguous.
//
// Strictly read-only: no writes, no sentinel row, no cleanup, so it can
// never repeat the deletion bug above. Skipped, not faked, when
// DATABASE_URL isn't set or the pipeline hasn't been run.
func TestOpeningWindow_PersistedWithZeroSilentDataLoss(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping live-Postgres integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err, "must be able to connect to the live Postgres instance at DATABASE_URL")
	t.Cleanup(func() { conn.Close(context.Background()) })

	q := storage.New(conn)

	windowStart, err := time.Parse("2006-01-02", "2024-08-01")
	require.NoError(t, err)
	windowEnd, err := time.Parse("2006-01-02", "2024-08-14")
	require.NoError(t, err)

	persisted, err := storage.LoadDailyReconciliationsInPeriod(ctx, q, windowStart, windowEnd)
	require.NoError(t, err)
	require.Len(t, persisted, 14,
		"KR2: all 14 hand-authored opening days must be persisted — a short count is exactly the silent loss this key result denies (has -ingest been run?)")

	byDate := make(map[string]reconcile.DailyReconciliation, len(persisted))
	for _, day := range persisted {
		key := day.Date.Format("2006-01-02")
		_, seen := byDate[key]
		require.False(t, seen, "date %s persisted more than once", key)
		byDate[key] = day
	}
	for day := windowStart; !day.After(windowEnd); day = day.AddDate(0, 0, 1) {
		require.Contains(t, byDate, day.Format("2006-01-02"), "no persisted reconciliation for %s", day.Format("2006-01-02"))
	}

	flagTypes := func(day reconcile.DailyReconciliation) []string {
		out := make([]string, 0, len(day.DiscrepancyFlags))
		for _, f := range day.DiscrepancyFlags {
			out = append(out, f.Type)
		}
		return out
	}

	// Irregularity #1 — the duplicate of IFOOD-20240803-0011 must be counted
	// once in the PERSISTED row, and the day must say out loud that it was
	// removed. Golden: iFood 262.00, JET 239.50 (opening/README.md).
	t.Run("irregularity 1: duplicate order counted once, and disclosed", func(t *testing.T) {
		day := byDate["2024-08-03"]
		require.Equal(t, int64(26200), day.GrossSalesBySource["ifood"], "golden value per opening/README.md — a double-counted duplicate would read 28600")
		require.Equal(t, int64(23950), day.GrossSalesBySource["just_eat_takeaway"], "golden value per opening/README.md")
		require.Contains(t, flagTypes(day), reconcile.FlagDuplicateOrderRemoved,
			"silently deduplicating is still data loss: the persisted day must disclose that a duplicate was removed")
	})

	// Irregularity #2 — the refund is dated 2024-08-09 but belongs to the
	// 2024-08-02 order. It must net at the ORIGINAL order date, and both
	// halves must survive persistence so a reader can still see gross and
	// refund separately rather than an unexplained smaller number.
	t.Run("irregularity 2: refund netted at the original order date", func(t *testing.T) {
		day := byDate["2024-08-02"]
		require.Equal(t, int64(23175), day.GrossSalesBySource["ifood"], "golden value per opening/README.md (pre-netting gross)")
		require.Equal(t, int64(21450), day.GrossSalesBySource["just_eat_takeaway"], "golden value per opening/README.md")
		require.Equal(t, int64(6225), day.RefundsCents, "the 2024-08-02 order's 62.25 refund must land on 2024-08-02, not on its 2024-08-09 refund_date")
		require.Equal(t, int64(6225), day.RefundsBySource["ifood"], "the refund must stay attributed to the platform it happened on")

		gross := day.GrossSalesBySource["ifood"] + day.GrossSalesBySource["just_eat_takeaway"]
		require.Equal(t, int64(44625), gross, "golden value per opening/README.md")
		require.Equal(t, int64(38400), gross-day.RefundsCents, "golden value: 446.25 - 62.25 = 384.00 net delivery revenue")

		// The refund must not be double-counted onto its settlement date.
		require.Zero(t, byDate["2024-08-09"].RefundsCents, "2024-08-09 is the refund_date, not the accrual date — netting it here too would double-count it")
	})

	// Irregularity #3 — a missing source is a fact to report, not a zero.
	// The persisted row must OMIT the delivery keys (absent, not 0) and
	// carry the flag; POS and supplier cost for the day must still be there.
	t.Run("irregularity 3: missing delivery day flagged, never silently zeroed", func(t *testing.T) {
		day := byDate["2024-08-10"]
		require.NotContains(t, day.GrossSalesBySource, "ifood", "an absent source must be absent, not zero — a 0.00 would read as 'we sold nothing'")
		require.NotContains(t, day.GrossSalesBySource, "just_eat_takeaway", "an absent source must be absent, not zero")
		require.NotContains(t, day.CommissionsBySource, "ifood", "no delivery rows means no commission entry either, not a zeroed one")
		require.Contains(t, flagTypes(day), reconcile.FlagMissingDeliverySource,
			"the day must state plainly that the delivery-platform source is missing")
		require.Equal(t, int64(120450), day.GrossSalesBySource["pos"],
			"the sources that DID arrive must survive intact: golden POS value for 2024-08-10 per opening/README.md")
		require.Positive(t, day.InputCostsCents, "invoice INV-3009 lands on this day — the missing delivery source must not take the cost sheet down with it")
	})

	// Irregularity #4 — the POS export is DD/MM/YYYY throughout while every
	// other file is ISO. 2024-08-10 is the assertion that proves the parse:
	// read as MM/DD it would be October 8th, outside the window entirely, so
	// the window's single largest POS day would simply vanish from these 14
	// rows. Its presence, at its golden value, is the proof.
	t.Run("irregularity 4: DD/MM/YYYY POS rows landed on the right days", func(t *testing.T) {
		require.Equal(t, int64(120450), byDate["2024-08-10"].GrossSalesBySource["pos"],
			"10/08/2024 must resolve to 2024-08-10, not 2024-10-08 — a day/month swap would move this whole day out of the window")
		for date, day := range byDate {
			require.Contains(t, day.GrossSalesBySource, "pos",
				"every opening day has POS rows; a missing 'pos' key on %s would mean a date-format parse dropped them", date)
		}
	})
}
