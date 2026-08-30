package mcptools_test

// TestGetPromotionRoi_ResolvesRealCampaignByHumanReadableOrShortenedName is
// a genuine integration test against the live PostgreSQL instance
// (DATABASE_URL), reusing the real, already-persisted dataset
// (the hand-authored opening window's promotion_ad_spend_export.csv, ingested by
// an earlier pipeline run) — it makes no writes and no Anthropic API calls,
// so it costs nothing and can never collide with or delete real
// rows. It reproduces the EXACT two failing inputs from the evaluation
// harness report (docs/plan.md mistakes log, docs/product-strategy.md
// "Real evaluation results", C3/A9): asking about JET-CAMP-LUNCHFIX by its
// full human-readable display name, and by its shortened form alone, both
// of which previously produced a hallucinated "not in the data" no_data
// refusal even though the campaign is real and its ROI is computable.
//
// Skipped, not faked, when DATABASE_URL isn't set or the real dataset
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

	// Sanity check: the real opening-window campaign this test targets must
	// already be persisted (an earlier `-ingest-promo` pipeline run) — if
	// not, every assertion below would be meaningless.
	roiExact, toolErr, err := mcptools.GetPromotionRoi(ctx, q, mcptools.GetPromotionRoiArgs{CampaignID: "JET-CAMP-LUNCHFIX"})
	require.NoError(t, err)
	require.Nil(t, toolErr, "real data for JET-CAMP-LUNCHFIX must already be persisted for this test to be meaningful — has -ingest-promo been run?")
	require.NotNil(t, roiExact)
	require.Len(t, roiExact.Promotions, 1)
	require.Equal(t, "JET-CAMP-LUNCHFIX", roiExact.Promotions[0].CampaignID)
	require.NotNil(t, roiExact.Promotions[0].ROI)
	require.Equal(t, "-450.75", *roiExact.Promotions[0].ROI, "golden value per backend/cmd/gendata/opening/README.md")

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
		require.Equal(t, "-450.75", *result.Promotions[0].ROI)
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
		require.Equal(t, "-450.75", *result.Promotions[0].ROI)
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

// TestListNegativeRoiPromotions_RealDataset_FlagsLunchfixWithProvenance is
// KR3's regression guard: "at least one negative-ROI promotion is flagged
// end-to-end, with provenance". Until this test existed that key result was
// true only BY CONSTRUCTION — the hand-authored opening window happens to
// contain JET-CAMP-LUNCHFIX, and TestListNegativeRoiPromotions_Fake
// (promo_tools_fake_test.go) only proved the tool filters a seeded fake
// correctly. Neither would fail if the real ingested dataset stopped
// producing a flagged row, which is exactly the regression KR3 claims
// cannot happen.
//
// So this asserts the whole persisted chain the KR is stated over: the real
// promotion_ad_spend_export.csv, ingested and reconciled by an earlier
// `-ingest-promo` run, still yields >= 1 flagged_negative record from
// list_negative_roi_promotions over the opening window, that record is the
// one the golden reference table names, its ROI is the independently
// hand-computed -450.75, and it carries non-empty file+row provenance (no
// number without provenance, Constitution Principle IV).
//
// Same shape as the test above: live PostgreSQL via DATABASE_URL,
// read-only, no Anthropic call, no writes — it cannot collide with or
// delete real rows. Skipped, not faked, when DATABASE_URL isn't set.
func TestListNegativeRoiPromotions_RealDataset_FlagsLunchfixWithProvenance(t *testing.T) {
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

	// The hand-authored opening window (backend/cmd/gendata/opening/README.md),
	// which the golden reference table below is computed over.
	result, toolErr, err := mcptools.ListNegativeRoiPromotions(ctx, q, mcptools.ListNegativeRoiPromotionsArgs{
		Period: mcptools.Period{Start: "2024-08-01", End: "2024-08-14"},
	})
	require.NoError(t, err)
	require.Nil(t, toolErr)
	require.NotNil(t, result)
	require.GreaterOrEqual(t, len(result.Promotions), 1,
		"KR3: at least one negative-ROI promotion must be flagged from the real ingested dataset — has -ingest-promo been run?")

	var lunchfix *mcptools.PromotionRoiView
	for i := range result.Promotions {
		require.True(t, result.Promotions[i].FlaggedNegative,
			"list_negative_roi_promotions must only ever return flagged rows")
		if result.Promotions[i].CampaignID == "JET-CAMP-LUNCHFIX" {
			lunchfix = &result.Promotions[i]
		}
	}
	require.NotNil(t, lunchfix, "the opening window's known negative-ROI campaign must be among the flagged rows")

	require.Equal(t, "Just Eat Takeaway", lunchfix.Platform)
	require.Equal(t, "610.00", lunchfix.Spend, "golden value per backend/cmd/gendata/opening/README.md")
	require.NotNil(t, lunchfix.AttributedIncrementalRevenue)
	require.Equal(t, "159.25", *lunchfix.AttributedIncrementalRevenue, "golden value: 42.25 + 36.25 + 34.50 + 46.25")
	require.NotNil(t, lunchfix.ROI)
	require.Equal(t, "-450.75", *lunchfix.ROI, "golden value: 159.25 - 610.00")

	// Provenance, not just a number: every flagged row must name the file
	// and row(s) it was computed from.
	require.NotEmpty(t, lunchfix.SourceRowRefs, "KR3 requires the flag to carry provenance, not a bare figure")
	for _, ref := range lunchfix.SourceRowRefs {
		require.NotEmpty(t, ref.File, "every source row ref must name its file")
		require.Positive(t, ref.Row, "every source row ref must name a real row number")
	}

	// FR-013's counterpart, asserted here rather than assumed: the
	// unattributable campaign is NOT smuggled into the flagged list. "We
	// could not attribute it" must never be reported as "it lost money".
	for _, p := range result.Promotions {
		require.NotEqual(t, "IFOOD-CAMP-WEEKEND", p.CampaignID,
			"a campaign with unattributable incremental revenue (FR-013) must never be listed as negative-ROI")
	}
}
