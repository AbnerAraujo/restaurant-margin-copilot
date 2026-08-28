package mcptools_test

// TestComparePlatformEconomics_MatchesFixtureReferenceValues is a genuine
// integration test against the live PostgreSQL instance (DATABASE_URL),
// reusing the real, already-persisted fixture data (an earlier `-ingest`/
// `-ingest-promo` pipeline run), the same read-only pattern
// promo_tools_test.go's TestGetPromotionRoi_ResolvesRealCampaignByHumanReadableOrShortenedName
// already established: it makes no writes, so it costs nothing and can
// never collide with real fixture rows.
//
// Every expected figure below was computed INDEPENDENTLY of this
// repository's Go code — a standalone Python script parsed
// backend/fixtures/delivery_platform_export.csv and
// promotion_ad_spend_export.csv directly (dedup'ing the byte-for-byte
// duplicate order 0011, summing completed-only subtotals for gross sales,
// summing completed+refunded recomputed commission per row, and summing
// promo spend for campaigns whose period overlaps 2026-08-01..14), matching
// this project's own established double-verification discipline
// (fixtures/README.md: "computed independently... by hand, then
// cross-checked with a throwaway script").
//
// iFood's effective rate is 22.06%, not the nominal flat 23% rate
// (backend/fixtures/README.md) — a real consequence of the
// IFOOD-20260802-0007 refund: gross sales still counts that order's
// completed-row subtotal (34.50), but its net commission contribution is
// zero (the refund row's -7.94 cancels the completed row's +7.94), so total
// commission ends up short of a flat 23% of true completed gross. This is
// exactly why FR-001 forbids deriving effective_rate from a hardcoded rate
// table: the real per-order data diverges from the nominal rate, and only
// summing real per-order commission recovers that.
import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/mcptools"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

func TestComparePlatformEconomics_MatchesFixtureReferenceValues(t *testing.T) {
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

	result, toolErr, err := mcptools.ComparePlatformEconomics(ctx, q, mcptools.Period{Start: "2026-08-01", End: "2026-08-14"})
	require.NoError(t, err)
	require.Nil(t, toolErr, "the real 14-day fixture period must already be fully persisted for this test to be meaningful — have -ingest and -ingest-promo been run?")
	require.NotNil(t, result)
	require.Equal(t, 14, result.DaysIncluded)
	require.Len(t, result.Platforms, 2)

	byName := make(map[string]mcptools.PlatformEconomicsView, 2)
	for _, p := range result.Platforms {
		byName[p.Source] = p
	}

	ifood, ok := byName["ifood"]
	require.True(t, ok, "iFood must always appear, even though it happens to have real activity in this period")
	require.Equal(t, "iFood", ifood.DisplayName)
	require.Equal(t, "838.00", ifood.GrossSales)
	require.Equal(t, "184.85", ifood.CommissionPaid)
	require.NotNil(t, ifood.EffectiveRate)
	require.Equal(t, "22.06%", *ifood.EffectiveRate, "184.85 / 838.00 — diverges from the nominal flat 23% rate because of the 2026-08-02 refund, see this file's doc comment")
	require.Equal(t, "275.00", ifood.PromoSpend, "IFOOD-CAMP-BOOST01 (180.00) + IFOOD-CAMP-WEEKEND (95.00)")
	require.Equal(t, "459.85", ifood.CombinedCost)
	require.NotNil(t, ifood.CombinedEffectiveRate)
	require.Equal(t, "54.87%", *ifood.CombinedEffectiveRate)
	require.NotEmpty(t, ifood.SourceRowRefs)

	jet, ok := byName["just_eat_takeaway"]
	require.True(t, ok)
	require.Equal(t, "Just Eat Takeaway", jet.DisplayName)
	require.Equal(t, "908.00", jet.GrossSales)
	require.Equal(t, "181.60", jet.CommissionPaid)
	require.NotNil(t, jet.EffectiveRate)
	require.Equal(t, "20.00%", *jet.EffectiveRate, "exactly the nominal flat 20% rate — JET has no refund irregularity in this fixture period")
	require.Equal(t, "280.00", jet.PromoSpend, "JET-CAMP-LUNCHFIX (220.00) + JET-CAMP-NEWMENU (60.00)")
	require.Equal(t, "461.60", jet.CombinedCost)
	require.NotNil(t, jet.CombinedEffectiveRate)
	require.Equal(t, "50.84%", *jet.CombinedEffectiveRate)
	require.NotEmpty(t, jet.SourceRowRefs)
}

// TestComparePlatformEconomics_EffectiveRateNilWhenGrossSalesZero is FR-003's
// dedicated zero-safety test: a platform with zero gross sales in the period
// must report effective_rate/combined_effective_rate as null, never a
// fabricated "0.00%" and never a divide-by-zero panic — the exact edge case
// the task's non-negotiables call out as easy to get wrong.
func TestComparePlatformEconomics_EffectiveRateNilWhenGrossSalesZero(t *testing.T) {
	conn, q := connectAndQueries(t)
	ctx := context.Background()

	date := sentinelDate(t, "1999-04-01")
	deleteDay(t, conn, date)

	// Just Eat Takeaway has real activity; iFood has NONE at all this day —
	// its key is entirely absent from both maps, exactly as reconcile.go
	// leaves it when a source has zero completed/refunded rows (never a
	// zero-valued key either).
	_, err := storage.SaveDailyReconciliation(ctx, q, reconcile.DailyReconciliation{
		Date:                date,
		GrossSalesBySource:  map[string]int64{"just_eat_takeaway": 10000, "pos": 5000},
		CommissionsCents:    2000,
		CommissionsBySource: map[string]int64{"just_eat_takeaway": 2000},
		MarginCents:         13000,
		SourceRowRefs:       []reconcile.SourceRowRef{{File: "test.csv", Row: 1}},
	})
	require.NoError(t, err)

	result, toolErr, err := mcptools.ComparePlatformEconomics(ctx, q, mcptools.Period{Start: "1999-04-01", End: "1999-04-01"})
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
	require.Equal(t, "0.00", ifood.PromoSpend)
	require.Equal(t, "0.00", ifood.CombinedCost)
	require.Nil(t, ifood.CombinedEffectiveRate)

	jet := byName["just_eat_takeaway"]
	require.Equal(t, "100.00", jet.GrossSales)
	require.Equal(t, "20.00", jet.CommissionPaid)
	require.NotNil(t, jet.EffectiveRate)
	require.Equal(t, "20.00%", *jet.EffectiveRate, "20.00 / 100.00")
}

// TestComparePlatformEconomics_InsufficientDataWhenPeriodHasMissingDay
// mirrors get_margin_delta's own policy exactly (reconciliation_tools_test.go's
// TestGetMarginDelta_InsufficientDataWhenPeriodHasMissingDay): a calendar day
// in the requested period with no persisted reconciliation AT ALL must
// refuse the whole comparison, not silently compute it over partial
// coverage.
func TestComparePlatformEconomics_InsufficientDataWhenPeriodHasMissingDay(t *testing.T) {
	conn, q := connectAndQueries(t)
	ctx := context.Background()

	for _, ds := range []string{"1999-04-10", "1999-04-12"} {
		date := sentinelDate(t, ds)
		deleteDay(t, conn, date)
		_, err := storage.SaveDailyReconciliation(ctx, q, reconcile.DailyReconciliation{Date: date})
		require.NoError(t, err)
	}

	result, toolErr, err := mcptools.ComparePlatformEconomics(ctx, q, mcptools.Period{Start: "1999-04-10", End: "1999-04-12"})
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, toolErr)
	require.Equal(t, "insufficient_data", toolErr.Error)
	require.Contains(t, toolErr.Missing, "1999-04-11")
}

func TestComparePlatformEconomics_InvalidInputForMalformedPeriod(t *testing.T) {
	_, q := connectAndQueries(t)
	ctx := context.Background()

	result, toolErr, err := mcptools.ComparePlatformEconomics(ctx, q, mcptools.Period{Start: "not-a-date", End: "2026-08-01"})
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, toolErr)
	require.Equal(t, "invalid_input", toolErr.Error)
}
