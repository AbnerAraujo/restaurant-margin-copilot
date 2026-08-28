package storage_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// intPtr/int64Ptr mirror the require-a-pointer shape of
// reconcile.PromotionRoiRecord's nil-able attribution fields.
func intPtr(v int) *int       { return &v }
func int64Ptr(v int64) *int64 { return &v }

// TestSaveAndLoadPromotionRoiRecord_RoundTripsExactly is a genuine
// integration test against a live PostgreSQL instance (DATABASE_URL) — not
// a mock. It exercises both the attributable (positive ROI) and
// unattributable (FR-013 refusal) shapes, since the storage adapter has to
// get both nil-handling and the daterange canonicalization right.
//
// Per docs/plan.md's mistakes log ("never use a real in-range fixture date
// as a test's own database primary key"), this test uses a sentinel
// campaign_id and a period entirely outside the real fixture period
// (2026-08-01..14) — never a real promotion_ad_spend_export.csv campaign —
// so its cleanup can never collide with (and delete) the real, permanently
// -persisted fixture campaigns a separate pipeline run writes.
//
// The test is skipped (not faked) when DATABASE_URL isn't set.
func TestSaveAndLoadPromotionRoiRecord_RoundTripsExactly(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping live-Postgres integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err, "must be able to connect to the live Postgres instance at DATABASE_URL")
	// Registered before the delete cleanup below so it runs LAST (t.Cleanup
	// is LIFO) — see docs/plan.md's mistakes log entry on defer/t.Cleanup
	// ordering for why this order matters.
	t.Cleanup(func() { conn.Close(context.Background()) })

	q := storage.New(conn)

	sentinelStart := time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
	sentinelEnd := time.Date(1999, 1, 3, 0, 0, 0, 0, time.UTC)

	t.Run("attributable, positive ROI", func(t *testing.T) {
		campaignID := "TEST-SENTINEL-POSITIVE-ROI"
		target := reconcile.PromotionRoiRecord{
			Platform:                          "TestPlatform",
			CampaignID:                        campaignID,
			PeriodStart:                       sentinelStart,
			PeriodEnd:                         sentinelEnd,
			SpendCents:                        18000,
			AttributedIncrementalOrders:       intPtr(6),
			AttributedIncrementalRevenueCents: int64Ptr(21400),
			ROICents:                          int64Ptr(3400),
			FlaggedNegative:                   false,
			SourceRowRefs: []reconcile.SourceRowRef{
				{File: "sentinel_test.csv", Row: 2},
			},
		}
		t.Cleanup(func() {
			_, err := conn.Exec(context.Background(),
				"DELETE FROM promotion_roi_record WHERE campaign_id = $1", campaignID)
			if err != nil {
				t.Logf("cleanup: failed to delete test row for %s: %v", campaignID, err)
			}
		})

		saved, err := storage.SavePromotionRoiRecord(ctx, q, target)
		require.NoError(t, err)
		require.Equal(t, campaignID, saved.CampaignID)

		loaded, err := storage.LoadPromotionRoiRecordsByCampaign(ctx, q, campaignID)
		require.NoError(t, err)
		require.Len(t, loaded, 1)

		got := loaded[0]
		require.Equal(t, target.Platform, got.Platform)
		require.True(t, target.PeriodStart.Equal(got.PeriodStart), "period_start must round-trip exactly despite Postgres's daterange canonicalization")
		require.True(t, target.PeriodEnd.Equal(got.PeriodEnd), "period_end must round-trip exactly despite Postgres's daterange canonicalization")
		require.Equal(t, target.SpendCents, got.SpendCents)
		require.NotNil(t, got.AttributedIncrementalOrders)
		require.Equal(t, *target.AttributedIncrementalOrders, *got.AttributedIncrementalOrders)
		require.NotNil(t, got.AttributedIncrementalRevenueCents)
		require.Equal(t, *target.AttributedIncrementalRevenueCents, *got.AttributedIncrementalRevenueCents)
		require.NotNil(t, got.ROICents)
		require.Equal(t, *target.ROICents, *got.ROICents, "roi must round-trip exactly through the NUMERIC(12,4) column despite being cents math internally")
		require.Equal(t, target.FlaggedNegative, got.FlaggedNegative)
		require.ElementsMatch(t, target.SourceRowRefs, got.SourceRowRefs)

		// UpsertPromotionRoiRecord must be idempotent on (platform,
		// campaign_id, period): re-saving must update, not duplicate.
		_, err = storage.SavePromotionRoiRecord(ctx, q, target)
		require.NoError(t, err)
		var count int
		row := conn.QueryRow(ctx, "SELECT count(*) FROM promotion_roi_record WHERE campaign_id = $1", campaignID)
		require.NoError(t, row.Scan(&count))
		require.Equal(t, 1, count, "UpsertPromotionRoiRecord must not append a duplicate row on re-save")
	})

	t.Run("unattributable, FR-013 nil roi survives the round trip as NULL, not zero", func(t *testing.T) {
		campaignID := "TEST-SENTINEL-UNATTRIBUTABLE"
		target := reconcile.PromotionRoiRecord{
			Platform:        "TestPlatform",
			CampaignID:      campaignID,
			PeriodStart:     sentinelStart,
			PeriodEnd:       sentinelEnd,
			SpendCents:      9500,
			FlaggedNegative: false,
			SourceRowRefs: []reconcile.SourceRowRef{
				{File: "sentinel_test.csv", Row: 3},
			},
		}
		t.Cleanup(func() {
			_, err := conn.Exec(context.Background(),
				"DELETE FROM promotion_roi_record WHERE campaign_id = $1", campaignID)
			if err != nil {
				t.Logf("cleanup: failed to delete test row for %s: %v", campaignID, err)
			}
		})

		_, err := storage.SavePromotionRoiRecord(ctx, q, target)
		require.NoError(t, err)

		loaded, err := storage.LoadPromotionRoiRecordsByCampaign(ctx, q, campaignID)
		require.NoError(t, err)
		require.Len(t, loaded, 1)

		got := loaded[0]
		require.Nil(t, got.AttributedIncrementalOrders, "must round-trip as NULL, not 0")
		require.Nil(t, got.AttributedIncrementalRevenueCents, "must round-trip as NULL, not 0")
		require.Nil(t, got.ROICents, "FR-013: roi must round-trip as NULL, never an estimated or zeroed value")
		require.False(t, got.FlaggedNegative)

		// The flagged_negative_requires_roi CHECK constraint is the DB-level
		// second gate on this same invariant — verify it actually rejects a
		// flagged_negative=true / roi=NULL row, rather than trusting the
		// migration file was applied as written. No cleanup needed: the
		// insert must fail, so no row is ever created.
		_, err = conn.Exec(ctx,
			`INSERT INTO promotion_roi_record (platform, campaign_id, period, spend, flagged_negative)
			 VALUES ($1, $2, $3, 10.00, true)`,
			"TestPlatform", "TEST-SENTINEL-CONSTRAINT-CHECK", storage.PromotionPeriodRange(sentinelStart, sentinelEnd))
		require.Error(t, err, "flagged_negative_requires_roi CHECK must reject flagged_negative=true with roi=NULL")
	})
}
