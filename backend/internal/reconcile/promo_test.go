package reconcile

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
)

// The paths point at the dataset's checked-in hand-authored opening
// window; the independently-verified golden values below come straight
// from backend/cmd/gendata/opening/README.md's "Promotion ROI" table —
// this test does not reimplement or second-guess that document, it
// asserts this package's output matches it exactly.
const (
	openingPromoDeliveryFile = "../../cmd/gendata/opening/delivery_platform_export.csv"
	openingPromoSpendFile    = "../../cmd/gendata/opening/promotion_ad_spend_export.csv"
)

func loadPromoOpeningWindow(t *testing.T) ([]ingest.PromotionSpendRecord, []ingest.DeliveryRecord) {
	t.Helper()

	promoFile, err := os.Open(openingPromoSpendFile)
	require.NoError(t, err)
	defer promoFile.Close()
	promos, err := ingest.ParsePromotionExport(promoFile, openingPromoSpendFile)
	require.NoError(t, err)

	deliveryFile, err := os.Open(openingPromoDeliveryFile)
	require.NoError(t, err)
	defer deliveryFile.Close()
	delivery, err := ingest.ParseDeliveryExport(deliveryFile, openingPromoDeliveryFile)
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

// TestComputePromotionRoiRecords_MatchesOpeningReferenceTable is the single
// table-driven test (T028) exercising every campaign in the opening
// window's promotion_ad_spend_export.csv against the exact reference
// values in opening/README.md's Promotion ROI table.
func TestComputePromotionRoiRecords_MatchesOpeningReferenceTable(t *testing.T) {
	promos, delivery := loadPromoOpeningWindow(t)
	records := ComputePromotionRoiRecords(promos, delivery)
	require.Len(t, records, 4, "the opening window has exactly 4 promotion campaigns")

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
			wantSpendCents:      38000,
			wantOrders:          9, // 42.00 + 55.25 + 48.50 + 63.50 + 24.00 (dedup'd) + 59.00 + 43.00 + 52.00 + 55.50
			wantRevenueCents:    44275,
			wantROICents:        6275, // net +62.75
			wantFlaggedNegative: false,
		},
		{
			name:                "JET-CAMP-LUNCHFIX negative ROI, must be flagged",
			campaignID:          "JET-CAMP-LUNCHFIX",
			wantPlatform:        "Just Eat Takeaway",
			wantSpendCents:      61000,
			wantOrders:          4, // 42.25 + 36.25 + 34.50 + 46.25
			wantRevenueCents:    15925,
			wantROICents:        -45075, // net -450.75
			wantFlaggedNegative: true,
		},
		{
			name:               "IFOOD-CAMP-WEEKEND zero tagged orders, unattributable per FR-013",
			campaignID:         "IFOOD-CAMP-WEEKEND",
			wantPlatform:       "iFood",
			wantSpendCents:     26000,
			wantUnattributable: true,
		},
		{
			name:                "JET-CAMP-NEWMENU positive ROI, not flagged",
			campaignID:          "JET-CAMP-NEWMENU",
			wantPlatform:        "Just Eat Takeaway",
			wantSpendCents:      12000,
			wantOrders:          3, // 58.00 + 45.75 + 50.00
			wantRevenueCents:    15375,
			wantROICents:        3375, // net +33.75
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
// (opening/README.md irregularity #1: order IFOOD-20240803-0011 appears
// twice, byte-for-byte identical) — attribution must not double-count it
// any more than gross daily revenue does.
func TestComputePromotionRoiRecords_DuplicateOrderNotDoubleCounted(t *testing.T) {
	promos, delivery := loadPromoOpeningWindow(t)
	records := ComputePromotionRoiRecords(promos, delivery)

	rec := findPromoRecord(t, records, "IFOOD-CAMP-BOOST01")
	require.NotNil(t, rec.AttributedIncrementalOrders)
	require.Equal(t, 9, *rec.AttributedIncrementalOrders, "the duplicated order 0011 must be counted once, not twice (would otherwise be 10)")
	require.NotNil(t, rec.AttributedIncrementalRevenueCents)
	require.Equal(t, int64(44275), *rec.AttributedIncrementalRevenueCents, "duplicate counted twice would incorrectly total 466.75, not 442.75")
}
