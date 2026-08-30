package badges

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
)

func int64Ptr(v int64) *int64 { return &v }
func strPtr(v string) *string { return &v }

func promoPeriod(t *testing.T, start, end string) (time.Time, time.Time) {
	t.Helper()
	s, err := time.Parse(dateLayout, start)
	require.NoError(t, err)
	e, err := time.Parse(dateLayout, end)
	require.NoError(t, err)
	return s, e
}

// TestEvaluateGrowthBadges_TableDriven exercises User Story 1 against the
// REAL, independently-computed reference values in
// backend/cmd/gendata/opening/README.md (not invented numbers):
// IFOOD-CAMP-BOOST01 (+$62.75) and JET-CAMP-NEWMENU (+$33.75) are the
// opening window's two positive-ROI campaigns, JET-CAMP-LUNCHFIX
// (-$450.75) is its one negative one, and IFOOD-CAMP-WEEKEND is its one
// unattributable one (FR-013). Spec 002 SC-001 requires exactly this
// campaign set to produce exactly the two Growth badges.
func TestEvaluateGrowthBadges_TableDriven(t *testing.T) {
	boostStart, boostEnd := promoPeriod(t, "2024-08-01", "2024-08-07")
	lunchStart, lunchEnd := promoPeriod(t, "2024-08-04", "2024-08-10")
	weekendStart, weekendEnd := promoPeriod(t, "2024-08-09", "2024-08-10")
	newmenuStart, newmenuEnd := promoPeriod(t, "2024-08-11", "2024-08-14")

	tests := []struct {
		name      string
		promo     reconcile.PromotionRoiRecord
		wantBadge bool
	}{
		{
			name: "IFOOD-CAMP-BOOST01: real +$62.75 ROI earns Growth",
			promo: reconcile.PromotionRoiRecord{
				Platform:    "iFood",
				CampaignID:  "IFOOD-CAMP-BOOST01",
				PeriodStart: boostStart,
				PeriodEnd:   boostEnd,
				SpendCents:  38000,
				ROICents:    int64Ptr(6275),
			},
			wantBadge: true,
		},
		{
			name: "JET-CAMP-NEWMENU: real +$33.75 ROI earns Growth",
			promo: reconcile.PromotionRoiRecord{
				Platform:    "Just Eat Takeaway",
				CampaignID:  "JET-CAMP-NEWMENU",
				PeriodStart: newmenuStart,
				PeriodEnd:   newmenuEnd,
				SpendCents:  12000,
				ROICents:    int64Ptr(3375),
			},
			wantBadge: true,
		},
		{
			name: "JET-CAMP-LUNCHFIX: real -$450.75 ROI earns nothing",
			promo: reconcile.PromotionRoiRecord{
				Platform:        "Just Eat Takeaway",
				CampaignID:      "JET-CAMP-LUNCHFIX",
				PeriodStart:     lunchStart,
				PeriodEnd:       lunchEnd,
				SpendCents:      61000,
				ROICents:        int64Ptr(-45075),
				FlaggedNegative: true,
			},
			wantBadge: false,
		},
		{
			name: "IFOOD-CAMP-WEEKEND: real unattributable ROI (nil) earns nothing",
			promo: reconcile.PromotionRoiRecord{
				Platform:    "iFood",
				CampaignID:  "IFOOD-CAMP-WEEKEND",
				PeriodStart: weekendStart,
				PeriodEnd:   weekendEnd,
				SpendCents:  26000,
				ROICents:    nil,
			},
			wantBadge: false,
		},
		{
			name: "a promotion that exactly broke even (zero ROI) earns nothing",
			promo: reconcile.PromotionRoiRecord{
				Platform:    "iFood",
				CampaignID:  "SENTINEL-BREAKEVEN",
				PeriodStart: boostStart,
				PeriodEnd:   boostEnd,
				SpendCents:  10000,
				ROICents:    int64Ptr(0),
			},
			wantBadge: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			badges := EvaluateGrowthBadges([]reconcile.PromotionRoiRecord{tt.promo})
			if !tt.wantBadge {
				require.Empty(t, badges)
				return
			}
			require.Len(t, badges, 1)
			got := badges[0]
			require.Equal(t, CodeGrowth, got.Code)
			require.Equal(t, "growth", got.Category)
			require.Equal(t, tt.promo.CampaignID, got.CampaignID)
			require.Equal(t, tt.promo.PeriodEnd.Format(dateLayout), got.Date)
		})
	}
}

// TestEvaluateGrowthBadges_TwoPositivePromotionsEachBadgeOnce is spec 002
// User Story 1 Acceptance Scenario 3: two positive-ROI promotions in the
// same period are each acknowledged once — no double-counting, no combined
// "bonus" badge inventing a value neither promotion earned alone. Uses the
// opening window's real two positive campaigns together.
func TestEvaluateGrowthBadges_TwoPositivePromotionsEachBadgeOnce(t *testing.T) {
	boostStart, boostEnd := promoPeriod(t, "2024-08-01", "2024-08-07")
	newmenuStart, newmenuEnd := promoPeriod(t, "2024-08-11", "2024-08-14")

	badges := EvaluateGrowthBadges([]reconcile.PromotionRoiRecord{
		{CampaignID: "IFOOD-CAMP-BOOST01", PeriodStart: boostStart, PeriodEnd: boostEnd, ROICents: int64Ptr(6275)},
		{CampaignID: "JET-CAMP-NEWMENU", PeriodStart: newmenuStart, PeriodEnd: newmenuEnd, ROICents: int64Ptr(3375)},
	})

	require.Len(t, badges, 2)
	campaignIDs := []string{badges[0].CampaignID, badges[1].CampaignID}
	require.ElementsMatch(t, []string{"IFOOD-CAMP-BOOST01", "JET-CAMP-NEWMENU"}, campaignIDs)
}

// TestEvaluateGrowthBadges_EmptyInput mirrors
// TestEvaluateReconciliationBadges_EmptyInput's boundary case for the new
// category: no promotions in, no badges out.
func TestEvaluateGrowthBadges_EmptyInput(t *testing.T) {
	require.Empty(t, EvaluateGrowthBadges(nil))
}

func mustDay(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse(dateLayout, s)
	require.NoError(t, err)
	return d
}

// TestEvaluateEngagementBadges_TableDriven is spec 002 User Story 2's whole
// contract in one table: a fresh/near-empty install fabricates nothing
// (SC-002), 7 distinct days earns "Week One" dated to the 7th real day, and
// a same-day double-submission is collapsed before the threshold is
// checked, never inflating the count (Acceptance Scenario 3).
func TestEvaluateEngagementBadges_TableDriven(t *testing.T) {
	tests := []struct {
		name          string
		usageDays     []time.Time
		wantBadge     bool
		wantDate      string
		wantUsageDays int
	}{
		{
			name:      "zero usage events earns nothing, no fabricated streak",
			usageDays: nil,
			wantBadge: false,
		},
		{
			name:      "one real usage day earns nothing yet",
			usageDays: []time.Time{mustDay(t, "2026-08-01")},
			wantBadge: false,
		},
		{
			name: "six distinct days earns nothing — the threshold is 7, not close-enough",
			usageDays: []time.Time{
				mustDay(t, "2026-08-01"), mustDay(t, "2026-08-02"), mustDay(t, "2026-08-03"),
				mustDay(t, "2026-08-04"), mustDay(t, "2026-08-05"), mustDay(t, "2026-08-06"),
			},
			wantBadge: false,
		},
		{
			name: "seven distinct non-consecutive days earns Week One, dated to the 7th",
			usageDays: []time.Time{
				mustDay(t, "2026-08-01"), mustDay(t, "2026-08-02"), mustDay(t, "2026-08-04"),
				mustDay(t, "2026-08-06"), mustDay(t, "2026-08-09"), mustDay(t, "2026-08-11"),
				mustDay(t, "2026-08-14"),
			},
			wantBadge:     true,
			wantDate:      "2026-08-14",
			wantUsageDays: 7,
		},
		{
			name: "a same-day double-submission counts once, not twice — still under threshold",
			usageDays: []time.Time{
				mustDay(t, "2026-08-01"), mustDay(t, "2026-08-01"), mustDay(t, "2026-08-02"),
				mustDay(t, "2026-08-03"), mustDay(t, "2026-08-04"), mustDay(t, "2026-08-05"),
			},
			wantBadge: false, // 5 distinct days, not 6 raw events worth
		},
		{
			name: "unordered input is sorted before the 7th day is picked",
			usageDays: []time.Time{
				mustDay(t, "2026-08-14"), mustDay(t, "2026-08-01"), mustDay(t, "2026-08-11"),
				mustDay(t, "2026-08-02"), mustDay(t, "2026-08-09"), mustDay(t, "2026-08-04"),
				mustDay(t, "2026-08-06"),
			},
			wantBadge:     true,
			wantDate:      "2026-08-14",
			wantUsageDays: 7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			badges := EvaluateEngagementBadges(tt.usageDays)
			if !tt.wantBadge {
				require.Empty(t, badges)
				return
			}
			require.Len(t, badges, 1)
			got := badges[0]
			require.Equal(t, CodeEngagement, got.Code)
			require.Equal(t, "engagement", got.Category)
			require.Equal(t, "Week One", got.Name)
			require.Equal(t, tt.wantDate, got.Date)
			require.Equal(t, tt.wantUsageDays, got.UsageDays)
		})
	}
}

// TestEvaluateEngagementBadges_FreshInstallShowsNoFabricatedStreak asserts
// spec 002 User Story 2 Acceptance Scenario 1 directly, per this task's own
// non-negotiable ("write a test that asserts this directly, not just 'hope
// it's true'"): nil input (a genuinely fresh instance's
// storage.LoadDistinctUsageDays return) produces zero badges and, critically,
// EvaluatePoints derives zero Engagement points from that zero — there is no
// code path that could report a non-zero default anywhere downstream of this
// function.
func TestEvaluateEngagementBadges_FreshInstallShowsNoFabricatedStreak(t *testing.T) {
	badges := EvaluateEngagementBadges(nil)
	require.Empty(t, badges, "a fresh install with zero usage_event rows must earn zero Engagement badges")

	points := EvaluatePoints(badges)
	require.Equal(t, 0, points.Total)
	require.Empty(t, points.Breakdown)
}

// TestEvaluateCampaignCreationBadges_TableDriven is spec 002 User Story 3's
// contract: an owner_created record with a replaces reference earns the
// badge (Acceptance Scenario 1); an owner_created record with no reference
// earns nothing (Acceptance Scenario 2 — logging a promotion is not itself
// rewarded); an ingested (pipeline) record is never eligible even if it
// somehow carried a replaces value, since origin gates the whole category.
func TestEvaluateCampaignCreationBadges_TableDriven(t *testing.T) {
	created := time.Date(2026, 8, 12, 14, 30, 0, 0, time.UTC)

	tests := []struct {
		name      string
		promo     reconcile.PromotionRoiRecord
		wantBadge bool
	}{
		{
			name: "owner_created with a replaces reference earns Campaign Launcher",
			promo: reconcile.PromotionRoiRecord{
				CampaignID:         "OWNER-CAMP-REPLACEMENT",
				Origin:             reconcile.OriginOwnerCreated,
				ReplacesCampaignID: strPtr("JET-CAMP-LUNCHFIX"),
				CreatedAt:          created,
			},
			wantBadge: true,
		},
		{
			name: "owner_created with NO replaces reference earns nothing (logging alone is not rewarded)",
			promo: reconcile.PromotionRoiRecord{
				CampaignID:         "OWNER-CAMP-STANDALONE",
				Origin:             reconcile.OriginOwnerCreated,
				ReplacesCampaignID: nil,
				CreatedAt:          created,
			},
			wantBadge: false,
		},
		{
			name: "owner_created with an EMPTY-STRING replaces reference earns nothing",
			promo: reconcile.PromotionRoiRecord{
				CampaignID:         "OWNER-CAMP-EMPTY-REPLACES",
				Origin:             reconcile.OriginOwnerCreated,
				ReplacesCampaignID: strPtr(""),
				CreatedAt:          created,
			},
			wantBadge: false,
		},
		{
			name: "an ingested record is never eligible, even with a replaces value set",
			promo: reconcile.PromotionRoiRecord{
				CampaignID:         "IFOOD-CAMP-BOOST01",
				Origin:             reconcile.OriginIngested,
				ReplacesCampaignID: strPtr("JET-CAMP-LUNCHFIX"),
				CreatedAt:          created,
			},
			wantBadge: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			badges := EvaluateCampaignCreationBadges([]reconcile.PromotionRoiRecord{tt.promo})
			if !tt.wantBadge {
				require.Empty(t, badges)
				return
			}
			require.Len(t, badges, 1)
			got := badges[0]
			require.Equal(t, CodeCampaignCreation, got.Code)
			require.Equal(t, "campaign_creation", got.Category)
			require.Equal(t, tt.promo.CampaignID, got.CampaignID)
			require.Equal(t, *tt.promo.ReplacesCampaignID, got.ReplacesCampaignID)
			require.Equal(t, tt.promo.CreatedAt.Format(dateLayout), got.Date)
		})
	}
}

// TestEvaluateCampaignCreationBadges_SameTargetReplacedTwiceEarnsOnlyOneBadge
// is the source-of-truth fix for a QA-found double-award currency bug: a
// flagged campaign stayed offered in the frontend's "replaces" dropdown
// after it had already been replaced once, and submitting it again minted a
// SECOND Campaign Launcher badge (and points award) for one real
// replacement. Two owner_created records naming the SAME
// ReplacesCampaignID must earn exactly one badge — the earliest one by
// CreatedAt — regardless of what order this slice arrives in, since
// storage.LoadAllPromotionRoiRecords orders by period/campaign_id, not by
// creation time.
func TestEvaluateCampaignCreationBadges_SameTargetReplacedTwiceEarnsOnlyOneBadge(t *testing.T) {
	earlier := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	later := time.Date(2026, 8, 12, 14, 30, 0, 0, time.UTC)

	first := reconcile.PromotionRoiRecord{
		CampaignID:         "OWNER-CAMP-FIRST-REPLACEMENT",
		Origin:             reconcile.OriginOwnerCreated,
		ReplacesCampaignID: strPtr("JET-CAMP-LUNCHFIX"),
		CreatedAt:          earlier,
	}
	second := reconcile.PromotionRoiRecord{
		CampaignID:         "OWNER-CAMP-SECOND-REPLACEMENT",
		Origin:             reconcile.OriginOwnerCreated,
		ReplacesCampaignID: strPtr("JET-CAMP-LUNCHFIX"),
		CreatedAt:          later,
	}

	// Reversed input order (as if the second-in-time record happened to sort
	// first) — the dedup must still pick the EARLIEST by CreatedAt, not the
	// first one encountered in the slice.
	got := EvaluateCampaignCreationBadges([]reconcile.PromotionRoiRecord{second, first})

	require.Len(t, got, 1, "the same flagged campaign can only genuinely be replaced once")
	require.Equal(t, "OWNER-CAMP-FIRST-REPLACEMENT", got[0].CampaignID, "the earliest replacement wins the badge")
	require.Equal(t, "JET-CAMP-LUNCHFIX", got[0].ReplacesCampaignID)
	require.Equal(t, earlier.Format(dateLayout), got[0].Date)

	points := EvaluatePoints(got)
	require.Equal(t, PointsCampaignCreation, points.Total, "one real replacement must be worth exactly one Campaign Launcher's points, never two")
}

// TestEvaluateCampaignCreationBadges_DifferentTargetsEachEarnTheirOwnBadge is
// the boundary the dedup fix must not break: two owner_created records
// replacing TWO DIFFERENT flagged campaigns are two genuinely separate
// replacements and must each earn their own badge.
func TestEvaluateCampaignCreationBadges_DifferentTargetsEachEarnTheirOwnBadge(t *testing.T) {
	created := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)

	got := EvaluateCampaignCreationBadges([]reconcile.PromotionRoiRecord{
		{
			CampaignID:         "OWNER-CAMP-A",
			Origin:             reconcile.OriginOwnerCreated,
			ReplacesCampaignID: strPtr("JET-CAMP-LUNCHFIX"),
			CreatedAt:          created,
		},
		{
			CampaignID:         "OWNER-CAMP-B",
			Origin:             reconcile.OriginOwnerCreated,
			ReplacesCampaignID: strPtr("IFOOD-CAMP-WEEKEND"),
			CreatedAt:          created,
		},
	})

	require.Len(t, got, 2)
	replaces := []string{got[0].ReplacesCampaignID, got[1].ReplacesCampaignID}
	require.ElementsMatch(t, []string{"JET-CAMP-LUNCHFIX", "IFOOD-CAMP-WEEKEND"}, replaces)
}

// TestEvaluatePoints_IncludesNewCategories confirms the three new codes are
// wired into the SAME points derivation the Reconciliation badges use — a
// mixed set of all five categories still reconciles to an auditable total,
// matching TestEvaluatePoints' own "breakdown sums to Total" invariant.
func TestEvaluatePoints_IncludesNewCategories(t *testing.T) {
	all := []Badge{
		{Code: CodeCleanClose, Name: names[CodeCleanClose]},
		{Code: CodeDiscrepancyCatcher, Name: names[CodeDiscrepancyCatcher]},
		{Code: CodeGrowth, Name: names[CodeGrowth]},
		{Code: CodeGrowth, Name: names[CodeGrowth]},
		{Code: CodeEngagement, Name: names[CodeEngagement]},
		{Code: CodeCampaignCreation, Name: names[CodeCampaignCreation]},
	}

	got := EvaluatePoints(all)

	wantTotal := PointsCleanClose + PointsDiscrepancyCatcher + 2*PointsGrowth + PointsEngagement + PointsCampaignCreation
	require.Equal(t, wantTotal, got.Total)
	require.Len(t, got.Breakdown, 5, "one breakdown line per distinct code present")

	sum := 0
	for _, line := range got.Breakdown {
		require.Equal(t, line.Count*line.PointsEach, line.Points)
		sum += line.Points
	}
	require.Equal(t, got.Total, sum)

	// Fixed emission order (pointsBreakdownOrder), not map iteration order —
	// Growth, Engagement, Campaign-Creation must appear after the two
	// original codes, in that order, every run.
	wantOrder := []Code{CodeCleanClose, CodeDiscrepancyCatcher, CodeGrowth, CodeEngagement, CodeCampaignCreation}
	gotOrder := make([]Code, 0, len(got.Breakdown))
	for _, line := range got.Breakdown {
		gotOrder = append(gotOrder, line.Code)
	}
	require.Equal(t, wantOrder, gotOrder)
}

// TestBuildResponse_CombinesAllFiveCategoriesWithDistinguishableCategory is
// FR-009's exact requirement exercised end to end through BuildResponse: all
// five codes can appear in one Badges slice, and each one's Category field
// says which of the four badge CATEGORIES (not codes) it belongs to, so a
// client never has to infer that clean_close/discrepancy_catcher share a
// category while growth/engagement/campaign_creation are each their own.
func TestBuildResponse_CombinesAllFiveCategoriesWithDistinguishableCategory(t *testing.T) {
	boostStart, boostEnd := promoPeriod(t, "2026-08-01", "2026-08-07")
	created := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)

	resp := BuildResponse(
		[]reconcile.DailyReconciliation{day("2026-08-01"), day("2026-08-03", reconcile.FlagDuplicateOrderRemoved)},
		[]reconcile.PromotionRoiRecord{
			{CampaignID: "IFOOD-CAMP-BOOST01", PeriodStart: boostStart, PeriodEnd: boostEnd, ROICents: int64Ptr(3400)},
			{
				CampaignID:         "OWNER-CAMP-REPLACEMENT",
				Origin:             reconcile.OriginOwnerCreated,
				ReplacesCampaignID: strPtr("JET-CAMP-LUNCHFIX"),
				CreatedAt:          created,
			},
		},
		[]time.Time{
			mustDay(t, "2026-08-01"), mustDay(t, "2026-08-02"), mustDay(t, "2026-08-03"),
			mustDay(t, "2026-08-04"), mustDay(t, "2026-08-05"), mustDay(t, "2026-08-06"),
			mustDay(t, "2026-08-07"),
		},
	)

	require.Len(t, resp.Badges, 5, "2 reconciliation + 1 growth + 1 engagement + 1 campaign_creation, all distinct")

	gotCategories := map[string]bool{}
	for _, b := range resp.Badges {
		gotCategories[b.Category] = true
		require.NotEmpty(t, b.Category, "every badge must carry an explicit, non-empty category (FR-009)")
	}
	require.Equal(t, map[string]bool{
		"reconciliation":    true,
		"growth":            true,
		"engagement":        true,
		"campaign_creation": true,
	}, gotCategories)

	require.Equal(t, PointsCleanClose+PointsDiscrepancyCatcher+PointsGrowth+PointsEngagement+PointsCampaignCreation, resp.Points.Total)
}
