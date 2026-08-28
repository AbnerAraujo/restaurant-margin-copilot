package mcptools_test

// Finding 4: promo_tools_test.go's only test skips when DATABASE_URL isn't
// set (and additionally needs real ingested fixture data). These exercise
// GetPromotionRoi and ListNegativeRoiPromotions against fakeQuerier
// (fake_querier_test.go) instead, covering both tools' happy paths and
// refusal paths with zero Postgres dependency.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/mcptools"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

func seedPromotion(t *testing.T, q *fakeQuerier, rec reconcile.PromotionRoiRecord) {
	t.Helper()
	_, err := storage.SavePromotionRoiRecord(context.Background(), q, rec)
	require.NoError(t, err)
}

// TestGetPromotionRoi_ByCampaignID_Fake is the happy path proving the fake
// is correct against a known-good case, not just the refusal path.
func TestGetPromotionRoi_ByCampaignID_Fake(t *testing.T) {
	q := newFakeQuerier()
	orders := 12
	revenueCents := int64(2000) // 20.00
	roiCents := int64(500)      // 5.00 (positive: revenue exceeded spend)
	seedPromotion(t, q, reconcile.PromotionRoiRecord{
		Platform:                          "iFood",
		CampaignID:                        "IFOOD-CAMP-BOOST01",
		PeriodStart:                       sentinelDate(t, "2020-06-01"),
		PeriodEnd:                         sentinelDate(t, "2020-06-07"),
		SpendCents:                        1500, // 15.00
		AttributedIncrementalOrders:       &orders,
		AttributedIncrementalRevenueCents: &revenueCents,
		ROICents:                          &roiCents,
		FlaggedNegative:                   false,
		SourceRowRefs:                     []reconcile.SourceRowRef{{File: "promo.csv", Row: 3}},
	})

	result, toolErr, err := mcptools.GetPromotionRoi(context.Background(), q, mcptools.GetPromotionRoiArgs{CampaignID: "IFOOD-CAMP-BOOST01"})
	require.NoError(t, err)
	require.Nil(t, toolErr)
	require.NotNil(t, result)
	require.Len(t, result.Promotions, 1)

	p := result.Promotions[0]
	require.Equal(t, "iFood", p.Platform)
	require.Equal(t, "IFOOD-CAMP-BOOST01", p.CampaignID)
	require.Equal(t, "15.00", p.Spend)
	require.NotNil(t, p.AttributedIncrementalOrders)
	require.Equal(t, 12, *p.AttributedIncrementalOrders)
	require.NotNil(t, p.AttributedIncrementalRevenue)
	require.Equal(t, "20.00", *p.AttributedIncrementalRevenue)
	require.NotNil(t, p.ROI)
	require.Equal(t, "5.00", *p.ROI)
	require.False(t, p.FlaggedNegative)
	require.Equal(t, "ingested", p.Origin)
}

// TestGetPromotionRoi_ByPlatformAndPeriod_Fake covers the platform+period
// input form (get_promotion_roi's other, mutually exclusive shape).
func TestGetPromotionRoi_ByPlatformAndPeriod_Fake(t *testing.T) {
	q := newFakeQuerier()
	seedPromotion(t, q, reconcile.PromotionRoiRecord{
		Platform:      "Just Eat Takeaway",
		CampaignID:    "JET-CAMP-TEST",
		PeriodStart:   sentinelDate(t, "2020-06-01"),
		PeriodEnd:     sentinelDate(t, "2020-06-07"),
		SpendCents:    1000,
		SourceRowRefs: []reconcile.SourceRowRef{{File: "promo.csv", Row: 4}},
	})

	result, toolErr, err := mcptools.GetPromotionRoi(context.Background(), q, mcptools.GetPromotionRoiArgs{
		Platform: "Just Eat Takeaway",
		Period:   &mcptools.Period{Start: "2020-06-01", End: "2020-06-07"},
	})
	require.NoError(t, err)
	require.Nil(t, toolErr)
	require.NotNil(t, result)
	require.Len(t, result.Promotions, 1)
	require.Equal(t, "JET-CAMP-TEST", result.Promotions[0].CampaignID)
	require.Nil(t, result.Promotions[0].ROI, "no attribution was seeded — must be null, not a fabricated value")
	require.Equal(t, "attribution_unavailable", result.Promotions[0].Reason)
}

func TestGetPromotionRoi_NoDataForUnknownCampaign_Fake(t *testing.T) {
	q := newFakeQuerier()

	result, toolErr, err := mcptools.GetPromotionRoi(context.Background(), q, mcptools.GetPromotionRoiArgs{CampaignID: "TOTALLY-UNKNOWN-XYZ"})
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, toolErr)
	require.Equal(t, "no_data", toolErr.Error)
}

func TestGetPromotionRoi_InvalidInput_Fake(t *testing.T) {
	q := newFakeQuerier()

	result, toolErr, err := mcptools.GetPromotionRoi(context.Background(), q, mcptools.GetPromotionRoiArgs{})
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, toolErr)
	require.Equal(t, "invalid_input", toolErr.Error)
}

// TestListNegativeRoiPromotions_Fake is the happy path for
// list_negative_roi_promotions: only the genuinely negative-ROI campaign in
// the period is returned, never one with unattributable (nil) ROI.
func TestListNegativeRoiPromotions_Fake(t *testing.T) {
	q := newFakeQuerier()
	negativeRoi := int64(-3000) // -30.00
	seedPromotion(t, q, reconcile.PromotionRoiRecord{
		Platform:        "iFood",
		CampaignID:      "IFOOD-CAMP-LOSER",
		PeriodStart:     sentinelDate(t, "2020-07-01"),
		PeriodEnd:       sentinelDate(t, "2020-07-07"),
		SpendCents:      5000,
		ROICents:        &negativeRoi,
		FlaggedNegative: true,
		SourceRowRefs:   []reconcile.SourceRowRef{{File: "promo.csv", Row: 5}},
	})
	positiveRoi := int64(1000)
	seedPromotion(t, q, reconcile.PromotionRoiRecord{
		Platform:        "iFood",
		CampaignID:      "IFOOD-CAMP-WINNER",
		PeriodStart:     sentinelDate(t, "2020-07-01"),
		PeriodEnd:       sentinelDate(t, "2020-07-07"),
		SpendCents:      1000,
		ROICents:        &positiveRoi,
		FlaggedNegative: false,
		SourceRowRefs:   []reconcile.SourceRowRef{{File: "promo.csv", Row: 6}},
	})
	seedPromotion(t, q, reconcile.PromotionRoiRecord{
		Platform:        "Just Eat Takeaway",
		CampaignID:      "JET-CAMP-UNATTRIBUTED",
		PeriodStart:     sentinelDate(t, "2020-07-01"),
		PeriodEnd:       sentinelDate(t, "2020-07-07"),
		SpendCents:      2000,
		FlaggedNegative: false, // nil ROI, so never flagged (FR-013)
		SourceRowRefs:   []reconcile.SourceRowRef{{File: "promo.csv", Row: 7}},
	})

	result, toolErr, err := mcptools.ListNegativeRoiPromotions(context.Background(), q, mcptools.ListNegativeRoiPromotionsArgs{
		Period: mcptools.Period{Start: "2020-07-01", End: "2020-07-07"},
	})
	require.NoError(t, err)
	require.Nil(t, toolErr)
	require.NotNil(t, result)
	require.Len(t, result.Promotions, 1)
	require.Equal(t, "IFOOD-CAMP-LOSER", result.Promotions[0].CampaignID)
	require.True(t, result.Promotions[0].FlaggedNegative)
	require.NotNil(t, result.Promotions[0].ROI)
	require.Equal(t, "-30.00", *result.Promotions[0].ROI)
}

func TestListNegativeRoiPromotions_InvalidInput_Fake(t *testing.T) {
	q := newFakeQuerier()

	result, toolErr, err := mcptools.ListNegativeRoiPromotions(context.Background(), q, mcptools.ListNegativeRoiPromotionsArgs{
		Period: mcptools.Period{Start: "not-a-date", End: "2020-07-07"},
	})
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, toolErr)
	require.Equal(t, "invalid_input", toolErr.Error)
}
