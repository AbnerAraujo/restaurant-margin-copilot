package mcptools_test

// TestGetPromotionRoi_ResolvesRealCampaignByHumanReadableOrShortenedName is
// a genuine integration test against the live PostgreSQL instance
// (DATABASE_URL), reusing the real, already-persisted fixture data
// (backend/fixtures/README.md's promotion_ad_spend_export.csv, ingested by
// an earlier pipeline run) — it makes no writes and no Anthropic API calls,
// so it costs nothing and can never collide with or delete real fixture
// rows. It reproduces the EXACT two failing inputs from the evaluation
// harness report (docs/plan.md mistakes log, docs/product-strategy.md
// "Real evaluation results", C3/A9): asking about JET-CAMP-LUNCHFIX by its
// full human-readable display name, and by its shortened form alone, both
// of which previously produced a hallucinated "not in the data" no_data
// refusal even though the campaign is real and its ROI is computable.
//
// Skipped, not faked, when DATABASE_URL isn't set or the real fixture data
// hasn't been ingested yet.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/mcptools"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

func TestGetPromotionRoi_ResolvesRealCampaignByHumanReadableOrShortenedName(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping live-Postgres integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err, "must be able to connect to the live Postgres instance at DATABASE_URL")
	t.Cleanup(func() { conn.Close(context.Background()) })

	q := storage.New(conn)

	// Sanity check: the real fixture campaign this test targets must
	// already be persisted (an earlier `-ingest-promo` pipeline run) — if
	// not, every assertion below would be meaningless.
	roiExact, toolErr, err := mcptools.GetPromotionRoi(ctx, q, mcptools.GetPromotionRoiArgs{CampaignID: "JET-CAMP-LUNCHFIX"})
	require.NoError(t, err)
	require.Nil(t, toolErr, "real fixture data for JET-CAMP-LUNCHFIX must already be persisted for this test to be meaningful — has -ingest-promo been run?")
	require.NotNil(t, roiExact)
	require.Len(t, roiExact.Promotions, 1)
	require.Equal(t, "JET-CAMP-LUNCHFIX", roiExact.Promotions[0].CampaignID)
	require.NotNil(t, roiExact.Promotions[0].ROI)
	require.Equal(t, "-165.00", *roiExact.Promotions[0].ROI, "golden value per backend/fixtures/README.md")

	t.Run("full human-readable display name", func(t *testing.T) {
		result, toolErr, err := mcptools.GetPromotionRoi(ctx, q, mcptools.GetPromotionRoiArgs{
			CampaignID: "Banner Ad - Lunch Fix Menu (JET-CAMP-LUNCHFIX)",
		})
		require.NoError(t, err)
		require.Nil(t, toolErr, "must resolve via fuzzy match, not return no_data")
		require.NotNil(t, result)
		require.Len(t, result.Promotions, 1)
		require.Equal(t, "JET-CAMP-LUNCHFIX", result.Promotions[0].CampaignID)
		require.NotNil(t, result.Promotions[0].ROI)
		require.Equal(t, "-165.00", *result.Promotions[0].ROI)
	})

	t.Run("shortened form alone", func(t *testing.T) {
		result, toolErr, err := mcptools.GetPromotionRoi(ctx, q, mcptools.GetPromotionRoiArgs{
			CampaignID: "LUNCHFIX",
		})
		require.NoError(t, err)
		require.Nil(t, toolErr, "must resolve via fuzzy match, not return the original bug's \"no campaign named 'LUNCHFIX'\" refusal")
		require.NotNil(t, result)
		require.Len(t, result.Promotions, 1)
		require.Equal(t, "JET-CAMP-LUNCHFIX", result.Promotions[0].CampaignID)
		require.NotNil(t, result.Promotions[0].ROI)
		require.Equal(t, "-165.00", *result.Promotions[0].ROI)
	})

	t.Run("still refuses a genuinely unknown campaign (Principle II must not be weakened)", func(t *testing.T) {
		result, toolErr, err := mcptools.GetPromotionRoi(ctx, q, mcptools.GetPromotionRoiArgs{
			CampaignID: "TOTALLY-UNKNOWN-CAMPAIGN-XYZ",
		})
		require.NoError(t, err)
		require.Nil(t, result)
		require.NotNil(t, toolErr)
		require.Equal(t, "no_data", toolErr.Error)
	})
}
