package storage_test

// Live-Postgres integration tests for spec 002-badge-expansion's storage
// layer, following promotion_test.go's own established pattern exactly:
// skipped (not faked) when DATABASE_URL is unset, sentinel campaign_ids and
// an out-of-dataset-range period so cleanup can never touch a real,
// permanently-persisted row, and defer-then-cleanup ordering that
// matches docs/plan.md's mistakes log entry on t.Cleanup being LIFO.

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

func connectOrSkip(t *testing.T) (*pgx.Conn, *storage.Queries, context.Context) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping live-Postgres integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err, "must be able to connect to the live Postgres instance at DATABASE_URL")
	t.Cleanup(func() { conn.Close(context.Background()) })

	assertCanonicalDatasetFingerprint(t, ctx, conn)

	return conn, storage.New(conn), ctx
}

// assertCanonicalDatasetFingerprint is a cheap tripwire, not a general
// drift-detection framework: one query against the hand-authored sentinel
// day 2024-08-01, whose margin (701.90) is independently verified in
// backend/cmd/gendata/opening/README.md and cross-checked against
// internal/reconcile's TestComputeDailyReconciliations_CleanDayMatchesHandComputation.
// It exists because backend/data/live/ is gitignored and has, in practice,
// been silently clobbered by this app's own cost-sheet-upload feature
// writing its example template over the real supplier_cost_sheet.csv —
// producing wrong input_costs (and therefore wrong margin) with no signal
// until a DB-backed test failed for a reason that looked like a code bug.
// This fires before any test in this package runs a real query, so a
// drifted dataset fails loudly and actionably instead of as a confusing
// numeric mismatch several layers downstream.
func assertCanonicalDatasetFingerprint(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	const sentinelDate = "2024-08-01"
	const canonicalMargin = "701.90"

	var gotMargin string
	err := conn.QueryRow(ctx, "SELECT margin::text FROM daily_reconciliation WHERE date = $1", sentinelDate).Scan(&gotMargin)
	if err != nil {
		t.Fatalf("dataset fingerprint check failed: could not read sentinel day %s from daily_reconciliation (%v) — data/live may not be ingested, or database state has drifted from the canonical dataset; regenerate with `go run ./cmd/gendata` and re-ingest via `cmd/server -ingest data/live -ingest-promo data/live` before running DB-backed tests", sentinelDate, err)
	}
	if gotMargin != canonicalMargin {
		t.Fatalf("database state has drifted from the canonical dataset (sentinel day %s: got margin %s, want %s per backend/cmd/gendata/opening/README.md) — data/live may have been overwritten (e.g. by the cost-sheet upload feature's own example template); regenerate with `go run ./cmd/gendata` and re-ingest via `cmd/server -ingest data/live -ingest-promo data/live` before running DB-backed tests", sentinelDate, gotMargin, canonicalMargin)
	}
}

// TestCreateOwnerPromotion_RoundTripsOriginAndReplacesReference exercises
// spec 002 User Story 3's write path end to end: an owner-created record
// persists origin="owner_created" and its replaces_campaign_id, and reads
// back through the exact same PromotionRoiRecordToDomain conversion every
// ingested record uses.
func TestCreateOwnerPromotion_RoundTripsOriginAndReplacesReference(t *testing.T) {
	conn, q, ctx := connectOrSkip(t)

	sentinelStart := time.Date(1999, 2, 1, 0, 0, 0, 0, time.UTC)
	sentinelEnd := time.Date(1999, 2, 3, 0, 0, 0, 0, time.UTC)

	t.Run("with a replaces reference", func(t *testing.T) {
		campaignID := "TEST-SENTINEL-OWNER-REPLACEMENT"
		replaces := "TEST-SENTINEL-REPLACED-CAMPAIGN"
		t.Cleanup(func() {
			_, err := conn.Exec(context.Background(), "DELETE FROM promotion_roi_record WHERE campaign_id = $1", campaignID)
			if err != nil {
				t.Logf("cleanup: failed to delete test row for %s: %v", campaignID, err)
			}
		})

		created, err := storage.CreateOwnerPromotion(ctx, q, storage.NewOwnerPromotion{
			Platform:           "TestPlatform",
			CampaignID:         campaignID,
			PeriodStart:        sentinelStart,
			PeriodEnd:          sentinelEnd,
			SpendCents:         5000,
			ReplacesCampaignID: &replaces,
		})
		require.NoError(t, err)
		require.Equal(t, reconcile.OriginOwnerCreated, created.Origin)
		require.NotNil(t, created.ReplacesCampaignID)
		require.Equal(t, replaces, *created.ReplacesCampaignID)
		require.Nil(t, created.ROICents, "an owner-created record has no computed ROI yet — never a fabricated zero")
		require.False(t, created.FlaggedNegative)
		require.False(t, created.CreatedAt.IsZero())

		loaded, err := storage.LoadPromotionRoiRecordsByCampaign(ctx, q, campaignID)
		require.NoError(t, err)
		require.Len(t, loaded, 1)
		require.Equal(t, reconcile.OriginOwnerCreated, loaded[0].Origin)
		require.Equal(t, replaces, *loaded[0].ReplacesCampaignID)
	})

	t.Run("with no replaces reference", func(t *testing.T) {
		campaignID := "TEST-SENTINEL-OWNER-STANDALONE"
		t.Cleanup(func() {
			_, err := conn.Exec(context.Background(), "DELETE FROM promotion_roi_record WHERE campaign_id = $1", campaignID)
			if err != nil {
				t.Logf("cleanup: failed to delete test row for %s: %v", campaignID, err)
			}
		})

		created, err := storage.CreateOwnerPromotion(ctx, q, storage.NewOwnerPromotion{
			Platform:    "TestPlatform",
			CampaignID:  campaignID,
			PeriodStart: sentinelStart,
			PeriodEnd:   sentinelEnd,
			SpendCents:  5000,
		})
		require.NoError(t, err)
		require.Equal(t, reconcile.OriginOwnerCreated, created.Origin)
		require.Nil(t, created.ReplacesCampaignID)
	})
}

// TestCreateOwnerPromotion_DuplicateSubmissionIsAUniqueViolation confirms
// this insert path is a genuine INSERT, not UpsertPromotionRoiRecord's
// ON CONFLICT upsert (see the sqlc query's own doc comment on why that
// distinction matters for a human resubmission versus a pipeline re-run).
func TestCreateOwnerPromotion_DuplicateSubmissionIsAUniqueViolation(t *testing.T) {
	conn, q, ctx := connectOrSkip(t)

	campaignID := "TEST-SENTINEL-OWNER-DUPLICATE"
	start := time.Date(1999, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(1999, 3, 3, 0, 0, 0, 0, time.UTC)
	t.Cleanup(func() {
		_, err := conn.Exec(context.Background(), "DELETE FROM promotion_roi_record WHERE campaign_id = $1", campaignID)
		if err != nil {
			t.Logf("cleanup: failed to delete test row for %s: %v", campaignID, err)
		}
	})

	params := storage.NewOwnerPromotion{
		Platform:    "TestPlatform",
		CampaignID:  campaignID,
		PeriodStart: start,
		PeriodEnd:   end,
		SpendCents:  1000,
	}
	_, err := storage.CreateOwnerPromotion(ctx, q, params)
	require.NoError(t, err)

	_, err = storage.CreateOwnerPromotion(ctx, q, params)
	require.Error(t, err, "a second identical submission must fail loudly, never silently overwrite")
}

// TestIsCampaignFlaggedNegative confirms FR-007's actual re-verification
// mechanism reads the REAL, live flagged_negative fact for a campaign_id —
// true for a persisted campaign with flagged_negative=true, false both for
// one with flagged_negative=false and for a campaign_id that does not exist
// at all (an unknown campaign is not a verified negative-ROI campaign
// either).
func TestIsCampaignFlaggedNegative(t *testing.T) {
	conn, q, ctx := connectOrSkip(t)

	sentinelStart := time.Date(1999, 4, 1, 0, 0, 0, 0, time.UTC)
	sentinelEnd := time.Date(1999, 4, 3, 0, 0, 0, 0, time.UTC)

	flaggedID := "TEST-SENTINEL-FLAGGED-NEGATIVE"
	notFlaggedID := "TEST-SENTINEL-NOT-FLAGGED"
	t.Cleanup(func() {
		_, err := conn.Exec(context.Background(), "DELETE FROM promotion_roi_record WHERE campaign_id IN ($1, $2)", flaggedID, notFlaggedID)
		if err != nil {
			t.Logf("cleanup: failed to delete test rows: %v", err)
		}
	})

	roiNeg := int64(-5000)
	_, err := storage.SavePromotionRoiRecord(ctx, q, reconcile.PromotionRoiRecord{
		Platform:                          "TestPlatform",
		CampaignID:                        flaggedID,
		PeriodStart:                       sentinelStart,
		PeriodEnd:                         sentinelEnd,
		SpendCents:                        10000,
		AttributedIncrementalOrders:       intPtr(1),
		AttributedIncrementalRevenueCents: int64Ptr(5000),
		ROICents:                          &roiNeg,
		FlaggedNegative:                   true,
	})
	require.NoError(t, err)

	roiPos := int64(5000)
	_, err = storage.SavePromotionRoiRecord(ctx, q, reconcile.PromotionRoiRecord{
		Platform:                          "TestPlatform",
		CampaignID:                        notFlaggedID,
		PeriodStart:                       sentinelStart,
		PeriodEnd:                         sentinelEnd,
		SpendCents:                        5000,
		AttributedIncrementalOrders:       intPtr(1),
		AttributedIncrementalRevenueCents: int64Ptr(10000),
		ROICents:                          &roiPos,
		FlaggedNegative:                   false,
	})
	require.NoError(t, err)

	flagged, err := storage.IsCampaignFlaggedNegative(ctx, q, flaggedID)
	require.NoError(t, err)
	require.True(t, flagged)

	notFlagged, err := storage.IsCampaignFlaggedNegative(ctx, q, notFlaggedID)
	require.NoError(t, err)
	require.False(t, notFlagged)

	unknown, err := storage.IsCampaignFlaggedNegative(ctx, q, "TEST-SENTINEL-DOES-NOT-EXIST")
	require.NoError(t, err)
	require.False(t, unknown, "a campaign_id with no record at all must never read as flagged")
}

// TestIsCampaignAlreadyReplaced backs the fix for a QA-found double-award
// currency bug: a flagged campaign stayed offered in the frontend's
// "replaces" dropdown after it had already been replaced once, and
// submitting it again minted a second Campaign Launcher badge/points award
// for one real replacement. POST /api/promotions now re-verifies this
// exact live fact before accepting a "replaces" claim (mirroring
// TestIsCampaignFlaggedNegative's own re-verify-at-submission discipline
// for the sibling check right above it) — true once ANY other persisted
// record names campaignID as replaced, false before that, and false for a
// campaign_id with no record naming it at all.
func TestIsCampaignAlreadyReplaced(t *testing.T) {
	conn, q, ctx := connectOrSkip(t)

	sentinelStart := time.Date(1999, 4, 10, 0, 0, 0, 0, time.UTC)
	sentinelEnd := time.Date(1999, 4, 12, 0, 0, 0, 0, time.UTC)

	targetID := "TEST-SENTINEL-ALREADY-REPLACED-TARGET"
	replacementID := "TEST-SENTINEL-ALREADY-REPLACED-REPLACEMENT"
	neverReplacedID := "TEST-SENTINEL-NEVER-REPLACED-TARGET"
	t.Cleanup(func() {
		_, err := conn.Exec(context.Background(),
			"DELETE FROM promotion_roi_record WHERE campaign_id IN ($1, $2, $3)",
			targetID, replacementID, neverReplacedID)
		if err != nil {
			t.Logf("cleanup: failed to delete test rows: %v", err)
		}
	})

	roiNeg := int64(-5000)
	_, err := storage.SavePromotionRoiRecord(ctx, q, reconcile.PromotionRoiRecord{
		Platform:                          "TestPlatform",
		CampaignID:                        targetID,
		PeriodStart:                       sentinelStart,
		PeriodEnd:                         sentinelEnd,
		SpendCents:                        10000,
		AttributedIncrementalOrders:       intPtr(1),
		AttributedIncrementalRevenueCents: int64Ptr(5000),
		ROICents:                          &roiNeg,
		FlaggedNegative:                   true,
	})
	require.NoError(t, err)

	_, err = storage.SavePromotionRoiRecord(ctx, q, reconcile.PromotionRoiRecord{
		Platform:                          "TestPlatform",
		CampaignID:                        neverReplacedID,
		PeriodStart:                       sentinelStart,
		PeriodEnd:                         sentinelEnd,
		SpendCents:                        10000,
		AttributedIncrementalOrders:       intPtr(1),
		AttributedIncrementalRevenueCents: int64Ptr(5000),
		ROICents:                          &roiNeg,
		FlaggedNegative:                   true,
	})
	require.NoError(t, err)

	notYetReplaced, err := storage.IsCampaignAlreadyReplaced(ctx, q, targetID)
	require.NoError(t, err)
	require.False(t, notYetReplaced, "a flagged campaign with no record naming it must not read as already replaced")

	_, err = storage.CreateOwnerPromotion(ctx, q, storage.NewOwnerPromotion{
		Platform:           "TestPlatform",
		CampaignID:         replacementID,
		PeriodStart:        sentinelStart,
		PeriodEnd:          sentinelEnd,
		SpendCents:         5000,
		ReplacesCampaignID: &targetID,
	})
	require.NoError(t, err)

	nowReplaced, err := storage.IsCampaignAlreadyReplaced(ctx, q, targetID)
	require.NoError(t, err)
	require.True(t, nowReplaced, "once ANY record names this campaign_id as replaced, it must read as already replaced")

	stillNotReplaced, err := storage.IsCampaignAlreadyReplaced(ctx, q, neverReplacedID)
	require.NoError(t, err)
	require.False(t, stillNotReplaced, "a different flagged campaign that nothing replaces must stay false")

	unknown, err := storage.IsCampaignAlreadyReplaced(ctx, q, "TEST-SENTINEL-DOES-NOT-EXIST")
	require.NoError(t, err)
	require.False(t, unknown)
}

// TestRecordUsageEvent_DedupesWithinTheSameUTCDay is spec 002 User Story 2
// Acceptance Scenario 3's real mechanism under test: two pings on the same
// UTC calendar day must collapse to exactly one usage_event row, enforced
// by the database (migrations/000003's unique index on the generated
// occurred_on column), not by any dedup logic this test could route around.
//
// This test never fabricates or deletes real usage history it did not
// itself create: it only removes the row it added, and only if today's UTC
// date was not already on file before this test ran.
func TestRecordUsageEvent_DedupesWithinTheSameUTCDay(t *testing.T) {
	_, q, ctx := connectOrSkip(t)

	todayUTC := time.Now().UTC().Format("2006-01-02")

	before, err := storage.LoadDistinctUsageDays(ctx, q)
	require.NoError(t, err)
	alreadyRecordedToday := false
	for _, d := range before {
		if d.Format("2006-01-02") == todayUTC {
			alreadyRecordedToday = true
		}
	}

	firstRecorded, err := storage.RecordUsageEvent(ctx, q)
	require.NoError(t, err)
	if !alreadyRecordedToday {
		require.True(t, firstRecorded, "the first ping of a genuinely new UTC day must record a new row")
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn, err := pgx.Connect(cleanupCtx, os.Getenv("DATABASE_URL"))
			if err != nil {
				t.Logf("cleanup: could not reconnect: %v", err)
				return
			}
			defer conn.Close(cleanupCtx)
			if _, err := conn.Exec(cleanupCtx, "DELETE FROM usage_event WHERE occurred_on = (now() AT TIME ZONE 'UTC')::date"); err != nil {
				t.Logf("cleanup: failed to delete today's test usage_event row: %v", err)
			}
		})
	} else {
		require.False(t, firstRecorded, "today was already recorded before this test ran — a real prior ping, not something to duplicate")
	}

	secondRecorded, err := storage.RecordUsageEvent(ctx, q)
	require.NoError(t, err)
	require.False(t, secondRecorded, "a second ping on the same UTC day must never insert a second row")

	after, err := storage.LoadDistinctUsageDays(ctx, q)
	require.NoError(t, err)
	count := 0
	for _, d := range after {
		if d.Format("2006-01-02") == todayUTC {
			count++
		}
	}
	require.Equal(t, 1, count, "exactly one row for today's UTC date, no matter how many times it was pinged")
}
