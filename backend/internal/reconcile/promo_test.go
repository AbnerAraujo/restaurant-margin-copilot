package reconcile

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
)

// Fixture paths and the independently-verified golden values below come
// straight from backend/fixtures/README.md's "Promotion ROI" table — this
// test does not reimplement or second-guess that document, it asserts this
// package's output matches it exactly.
const (
	fixturePromoDeliveryFile = "../../fixtures/delivery_platform_export.csv"
	fixturePromoSpendFile    = "../../fixtures/promotion_ad_spend_export.csv"
)

func loadPromoFixtures(t *testing.T) ([]ingest.PromotionSpendRecord, []ingest.DeliveryRecord) {
	t.Helper()

	promoFile, err := os.Open(fixturePromoSpendFile)
	require.NoError(t, err)
	defer promoFile.Close()
	promos, err := ingest.ParsePromotionExport(promoFile, fixturePromoSpendFile)
	require.NoError(t, err)

	deliveryFile, err := os.Open(fixturePromoDeliveryFile)
	require.NoError(t, err)
	defer deliveryFile.Close()
	delivery, err := ingest.ParseDeliveryExport(deliveryFile, fixturePromoDeliveryFile)
	require.NoError(t, err)

	return promos, delivery
}

func findPromoRecord(t *testing.T, records []PromotionRoiRecord, campaignID string) PromotionRoiRecord {
	t.Helper()
	for _, r := range records {
		if r.CampaignID == campaignID {
			return r
		}
	}
	t.Fatalf("no PromotionRoiRecord for campaign_id %s", campaignID)
	return PromotionRoiRecord{}
}

// TestComputePromotionRoiRecords_MatchesFixtureReferenceTable is the single
// table-driven test (T028) exercising every campaign in
// backend/fixtures/promotion_ad_spend_export.csv against the exact
// reference values in fixtures/README.md's Promotion ROI table.
func TestComputePromotionRoiRecords_MatchesFixtureReferenceTable(t *testing.T) {
	promos, delivery := loadPromoFixtures(t)
	records := ComputePromotionRoiRecords(promos, delivery)
	require.Len(t, records, 4, "fixture set has exactly 4 promotion campaigns")

	tests := []struct {
		name                string
		campaignID          string
		wantPlatform        string
		wantSpendCents      int64
		wantOrders          int // ignored when wantUnattributable
		wantRevenueCents    int64
		wantROICents        int64
		wantFlaggedNegative bool
		wantUnattributable  bool
	}{
		{
			name:                "IFOOD-CAMP-BOOST01 positive ROI, not flagged",
			campaignID:          "IFOOD-CAMP-BOOST01",
			wantPlatform:        "iFood",
			wantSpendCents:      18000,
			wantOrders:          6, // 42.00 + 38.00 + 24.00 (dedup'd) + 29.00 + 36.00 + 45.00
			wantRevenueCents:    21400,
			wantROICents:        3400, // net +34.00
			wantFlaggedNegative: false,
		},
		{
			name:                "JET-CAMP-LUNCHFIX negative ROI, must be flagged",
			campaignID:          "JET-CAMP-LUNCHFIX",
			wantPlatform:        "Just Eat Takeaway",
			wantSpendCents:      22000,
			wantOrders:          2, // 22.00 + 33.00
			wantRevenueCents:    5500,
			wantROICents:        -16500, // net -165.00
			wantFlaggedNegative: true,
		},
		{
			name:               "IFOOD-CAMP-WEEKEND zero tagged orders, unattributable per FR-013",
			campaignID:         "IFOOD-CAMP-WEEKEND",
			wantPlatform:       "iFood",
			wantSpendCents:     9500,
			wantUnattributable: true,
		},
		{
			name:                "JET-CAMP-NEWMENU positive ROI, not flagged",
			campaignID:          "JET-CAMP-NEWMENU",
			wantPlatform:        "Just Eat Takeaway",
			wantSpendCents:      6000,
			wantOrders:          3, // 26.00 + 24.50 + 29.00
			wantRevenueCents:    7950,
			wantROICents:        1950, // net +19.50
			wantFlaggedNegative: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := findPromoRecord(t, records, tt.campaignID)
			require.Equal(t, tt.wantPlatform, rec.Platform)
			require.Equal(t, tt.wantSpendCents, rec.SpendCents)
			require.NotEmpty(t, rec.SourceRowRefs, "every record, attributable or not, must carry provenance to its own spend row")

			if tt.wantUnattributable {
				require.Nil(t, rec.AttributedIncrementalOrders, "FR-013: unattributable orders count must be nil, never zero")
				require.Nil(t, rec.AttributedIncrementalRevenueCents, "FR-013: unattributable revenue must be nil, never zero")
				require.Nil(t, rec.ROICents, "FR-013: roi MUST be null when incremental revenue cannot be attributed — never estimated")
				require.False(t, rec.FlaggedNegative, "an unattributable promotion must not be flagged negative — that would assert a figure FR-013 says must not exist")
				return
			}

			require.NotNil(t, rec.AttributedIncrementalOrders)
			require.Equal(t, tt.wantOrders, *rec.AttributedIncrementalOrders)
			require.NotNil(t, rec.AttributedIncrementalRevenueCents)
			require.Equal(t, tt.wantRevenueCents, *rec.AttributedIncrementalRevenueCents)
			require.NotNil(t, rec.ROICents)
			require.Equal(t, tt.wantROICents, *rec.ROICents)
			require.Equal(t, tt.wantFlaggedNegative, rec.FlaggedNegative)
		})
	}
}

// TestComputePromotionRoiRecords_DuplicateOrderNotDoubleCounted pins down,
// in isolation, that IFOOD-CAMP-BOOST01's attribution reuses the same
// duplicate-collapse dedupeDelivery already proves in reconcile_test.go
// (fixtures/README.md irregularity #1: order IFOOD-20260803-0011 appears
// twice, byte-for-byte identical) — attribution must not double-count it
// any more than gross daily revenue does.
func TestComputePromotionRoiRecords_DuplicateOrderNotDoubleCounted(t *testing.T) {
	promos, delivery := loadPromoFixtures(t)
	records := ComputePromotionRoiRecords(promos, delivery)

	rec := findPromoRecord(t, records, "IFOOD-CAMP-BOOST01")
	require.NotNil(t, rec.AttributedIncrementalOrders)
	require.Equal(t, 6, *rec.AttributedIncrementalOrders, "the duplicated order 0011 must be counted once, not twice (would otherwise be 7)")
	require.NotNil(t, rec.AttributedIncrementalRevenueCents)
	require.Equal(t, int64(21400), *rec.AttributedIncrementalRevenueCents, "duplicate counted twice would incorrectly total 238.00, not 214.00")
}
