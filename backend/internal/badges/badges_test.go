package badges

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
)

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	require.NoError(t, err)
	return d
}

// TestEvaluateReconciliationBadges_TableDriven is pure Go logic over
// synthetic DailyReconciliation values — no database, no fixture files —
// exercising the complementary CleanClose/DiscrepancyCatcher pair the way
// docs/product-strategy.md describes them: "both fire directly off
// DailyReconciliation.discrepancy_flags".
func TestEvaluateReconciliationBadges_TableDriven(t *testing.T) {
	tests := []struct {
		name          string
		day           reconcile.DailyReconciliation
		want          Code
		wantFlagTypes []string
	}{
		{
			name: "zero discrepancy flags earns Clean Close",
			day: reconcile.DailyReconciliation{
				Date:             mustDate(t, "2026-08-01"),
				DiscrepancyFlags: nil,
			},
			want: CodeCleanClose,
		},
		{
			name: "explicitly empty (non-nil) discrepancy flags also earns Clean Close",
			day: reconcile.DailyReconciliation{
				Date:             mustDate(t, "2026-08-04"),
				DiscrepancyFlags: []reconcile.DiscrepancyFlag{},
			},
			want: CodeCleanClose,
		},
		{
			name: "a duplicate-order flag earns Discrepancy Catcher",
			day: reconcile.DailyReconciliation{
				Date: mustDate(t, "2026-08-03"),
				DiscrepancyFlags: []reconcile.DiscrepancyFlag{
					{Type: reconcile.FlagDuplicateOrderRemoved, Detail: "order X duplicated"},
				},
			},
			want:          CodeDiscrepancyCatcher,
			wantFlagTypes: []string{reconcile.FlagDuplicateOrderRemoved},
		},
		{
			name: "an anomaly-threshold flag earns Discrepancy Catcher",
			day: reconcile.DailyReconciliation{
				Date: mustDate(t, "2026-08-06"),
				DiscrepancyFlags: []reconcile.DiscrepancyFlag{
					{Type: reconcile.FlagAnomalyThresholdExceeded, Detail: "gross revenue deviates 25% from baseline"},
				},
			},
			want:          CodeDiscrepancyCatcher,
			wantFlagTypes: []string{reconcile.FlagAnomalyThresholdExceeded},
		},
		{
			name: "a missing-delivery-source flag also earns Discrepancy Catcher",
			day: reconcile.DailyReconciliation{
				Date: mustDate(t, "2026-08-08"),
				DiscrepancyFlags: []reconcile.DiscrepancyFlag{
					{Type: reconcile.FlagMissingDeliverySource, Detail: "no delivery-platform export rows for 2026-08-08"},
				},
			},
			want:          CodeDiscrepancyCatcher,
			wantFlagTypes: []string{reconcile.FlagMissingDeliverySource},
		},
		{
			name: "multiple flags on the same day all surface, still one Discrepancy Catcher badge",
			day: reconcile.DailyReconciliation{
				Date: mustDate(t, "2026-08-09"),
				DiscrepancyFlags: []reconcile.DiscrepancyFlag{
					{Type: reconcile.FlagDuplicateOrderRemoved, Detail: "..."},
					{Type: reconcile.FlagCommissionMismatch, Detail: "..."},
				},
			},
			want:          CodeDiscrepancyCatcher,
			wantFlagTypes: []string{reconcile.FlagDuplicateOrderRemoved, reconcile.FlagCommissionMismatch},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			badges := EvaluateReconciliationBadges([]reconcile.DailyReconciliation{tt.day})
			require.Len(t, badges, 1)

			got := badges[0]
			require.Equal(t, tt.day.Date.Format(dateLayout), got.Date)
			require.Equal(t, tt.want, got.Code)
			require.Equal(t, names[tt.want], got.Name)
			require.Equal(t, tt.wantFlagTypes, got.DiscrepancyFlagTypes)
		})
	}
}

// TestEvaluateReconciliationBadges_PartitionsEveryDay confirms every input
// day earns exactly one badge — the two badges partition the input set,
// they are never both true, both absent, or double-counted for one day.
func TestEvaluateReconciliationBadges_PartitionsEveryDay(t *testing.T) {
	days := []reconcile.DailyReconciliation{
		{Date: mustDate(t, "2026-08-01")}, // clean
		{Date: mustDate(t, "2026-08-02"), DiscrepancyFlags: []reconcile.DiscrepancyFlag{{Type: reconcile.FlagDuplicateOrderRemoved}}},
		{Date: mustDate(t, "2026-08-03")}, // clean
	}

	badges := EvaluateReconciliationBadges(days)
	require.Len(t, badges, len(days), "every day must earn exactly one badge")

	require.Equal(t, CodeCleanClose, badges[0].Code)
	require.Equal(t, CodeDiscrepancyCatcher, badges[1].Code)
	require.Equal(t, CodeCleanClose, badges[2].Code)
}

// TestEvaluateReconciliationBadges_EmptyInput confirms the boundary case:
// no days in, no badges out — not a nil-pointer panic, not a fabricated
// badge for a day that was never reconciled.
func TestEvaluateReconciliationBadges_EmptyInput(t *testing.T) {
	badges := EvaluateReconciliationBadges(nil)
	require.Empty(t, badges)
}
