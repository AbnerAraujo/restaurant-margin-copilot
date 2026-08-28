package mcptools_test

// Finding 4: platform_comparison_tools_test.go's tests all skip when
// DATABASE_URL isn't set. These mirror its scenarios against fakeQuerier
// (fake_querier_test.go) instead, so ComparePlatformEconomics' own
// insufficient_data refusal and its FR-003 zero-safety rule both run in a
// default `go test ./...`. The live-gated tests are kept as-is.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/mcptools"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// TestComparePlatformEconomics_HappyPath_Fake proves the fake is correct
// against a known-good, hand-computed case — not just the refusal path.
func TestComparePlatformEconomics_HappyPath_Fake(t *testing.T) {
	q := newFakeQuerier()
	ctx := context.Background()

	date := sentinelDate(t, "2020-04-05")
	_, err := storage.SaveDailyReconciliation(ctx, q, reconcile.DailyReconciliation{
		Date: date,
		GrossSalesBySource: map[string]int64{
			"ifood":             10000, // 100.00
			"just_eat_takeaway": 20000, // 200.00
			"pos":               5000,
		},
		CommissionsBySource: map[string]int64{
			"ifood":             2300, // 23.00 -> 23.00%
			"just_eat_takeaway": 4000, // 40.00 -> 20.00%
		},
		CommissionsCents: 6300,
		MarginCents:      28700,
		SourceRowRefs:    []reconcile.SourceRowRef{{File: "test.csv", Row: 1}},
	})
	require.NoError(t, err)

	_, err = storage.SavePromotionRoiRecord(ctx, q, reconcile.PromotionRoiRecord{
		Platform:      "iFood",
		CampaignID:    "IFOOD-CAMP-TEST",
		PeriodStart:   sentinelDate(t, "2020-04-01"),
		PeriodEnd:     sentinelDate(t, "2020-04-07"),
		SpendCents:    1500, // 15.00
		SourceRowRefs: []reconcile.SourceRowRef{{File: "promo.csv", Row: 1}},
	})
	require.NoError(t, err)

	result, toolErr, err := mcptools.ComparePlatformEconomics(ctx, q, mcptools.Period{Start: "2020-04-05", End: "2020-04-05"})
	require.NoError(t, err)
	require.Nil(t, toolErr)
	require.NotNil(t, result)
	require.Equal(t, 1, result.DaysIncluded)
	require.Len(t, result.Platforms, 2)

	byName := make(map[string]mcptools.PlatformEconomicsView, 2)
	for _, p := range result.Platforms {
		byName[p.Source] = p
	}

	ifood := byName["ifood"]
	require.Equal(t, "iFood", ifood.DisplayName)
	require.Equal(t, "100.00", ifood.GrossSales)
	require.Equal(t, "23.00", ifood.CommissionPaid)
	require.NotNil(t, ifood.EffectiveRate)
	require.Equal(t, "23.00%", *ifood.EffectiveRate)
	require.Equal(t, "15.00", ifood.PromoSpend, "IFOOD-CAMP-TEST overlaps 2020-04-05")
	require.Equal(t, "38.00", ifood.CombinedCost)
	require.NotNil(t, ifood.CombinedEffectiveRate)
	require.Equal(t, "38.00%", *ifood.CombinedEffectiveRate)

	jet := byName["just_eat_takeaway"]
	require.Equal(t, "Just Eat Takeaway", jet.DisplayName)
	require.Equal(t, "200.00", jet.GrossSales)
	require.Equal(t, "40.00", jet.CommissionPaid)
	require.NotNil(t, jet.EffectiveRate)
	require.Equal(t, "20.00%", *jet.EffectiveRate)
	require.Equal(t, "0.00", jet.PromoSpend, "no promo record was seeded for Just Eat Takeaway")
}

// TestComparePlatformEconomics_EffectiveRateNilWhenGrossSalesZero_Fake
// mirrors FR-003's dedicated zero-safety test from the live suite.
func TestComparePlatformEconomics_EffectiveRateNilWhenGrossSalesZero_Fake(t *testing.T) {
	q := newFakeQuerier()
	ctx := context.Background()

	date := sentinelDate(t, "2020-04-10")
	_, err := storage.SaveDailyReconciliation(ctx, q, reconcile.DailyReconciliation{
		Date:                date,
		GrossSalesBySource:  map[string]int64{"just_eat_takeaway": 10000, "pos": 5000},
		CommissionsCents:    2000,
		CommissionsBySource: map[string]int64{"just_eat_takeaway": 2000},
		MarginCents:         13000,
		SourceRowRefs:       []reconcile.SourceRowRef{{File: "test.csv", Row: 1}},
	})
	require.NoError(t, err)

	result, toolErr, err := mcptools.ComparePlatformEconomics(ctx, q, mcptools.Period{Start: "2020-04-10", End: "2020-04-10"})
	require.NoError(t, err)
	require.Nil(t, toolErr)
	require.NotNil(t, result)

	byName := make(map[string]mcptools.PlatformEconomicsView, 2)
	for _, p := range result.Platforms {
		byName[p.Source] = p
	}

	ifood := byName["ifood"]
	require.Equal(t, "0.00", ifood.GrossSales, "a real zero — iFood must still appear, per FR-003, never be silently omitted")
	require.Equal(t, "0.00", ifood.CommissionPaid)
	require.Nil(t, ifood.EffectiveRate, "a rate over zero sales is undefined — must be null, never a fabricated 0.00%% or a divide-by-zero")
	require.Nil(t, ifood.CombinedEffectiveRate)

	jet := byName["just_eat_takeaway"]
	require.Equal(t, "100.00", jet.GrossSales)
	require.NotNil(t, jet.EffectiveRate)
	require.Equal(t, "20.00%", *jet.EffectiveRate)
}

// TestComparePlatformEconomics_InsufficientDataWhenPeriodHasMissingDay_Fake
// is Finding 4's core case for this file: a calendar day in the requested
// period with no persisted reconciliation at all must refuse the whole
// comparison, now proven without DATABASE_URL.
func TestComparePlatformEconomics_InsufficientDataWhenPeriodHasMissingDay_Fake(t *testing.T) {
	q := newFakeQuerier()
	ctx := context.Background()

	for _, ds := range []string{"2020-04-20", "2020-04-22"} {
		date := sentinelDate(t, ds)
		_, err := storage.SaveDailyReconciliation(ctx, q, reconcile.DailyReconciliation{Date: date})
		require.NoError(t, err)
	}

	result, toolErr, err := mcptools.ComparePlatformEconomics(ctx, q, mcptools.Period{Start: "2020-04-20", End: "2020-04-22"})
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, toolErr)
	require.Equal(t, "insufficient_data", toolErr.Error)
	require.Contains(t, toolErr.Missing, "2020-04-21")
}

func TestComparePlatformEconomics_InvalidInputForMalformedPeriod_Fake(t *testing.T) {
	q := newFakeQuerier()
	ctx := context.Background()

	result, toolErr, err := mcptools.ComparePlatformEconomics(ctx, q, mcptools.Period{Start: "not-a-date", End: "2020-04-01"})
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, toolErr)
	require.Equal(t, "invalid_input", toolErr.Error)
}
