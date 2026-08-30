package reconcile

import (
	"fmt"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/money"
)

// AnomalyThresholdPct is the percentage deviation from a trailing average
// gross-revenue figure that triggers an anomaly_threshold_exceeded flag
// (FR-003). At the dataset's realistic scale, ordinary weekly seasonality
// (a strong Friday/Saturday, a slow Monday) can legitimately cross 20%
// against a 3-day trailing window — reconcile_test.go's regression test
// pins exactly which opening-window days do, so a threshold change shows
// up as a deliberate, visible decision. Tunable per spec Assumptions, not
// asserted as a universal constant.
const AnomalyThresholdPct = 0.20

// TrailingWindowDays is how many preceding complete days feed the anomaly
// threshold's baseline average. Kept short (3, not a full 7-day week) so
// the very first few days of any dataset get a working baseline quickly.
const TrailingWindowDays = 3

// applyAnomalyFlags flags any day whose total gross revenue (summed across
// every source) deviates from a trailing-window average of preceding
// complete days by more than AnomalyThresholdPct. It mutates days in place.
//
// A day already carrying a missing_delivery_source flag is excluded both
// from being flagged itself and from the trailing window used to evaluate
// other days: an incomplete day's revenue figure isn't a real measurement
// of that day, so it must not poison the baseline for its neighbors, and
// flagging it as an "anomaly" on top of "missing data" would just be noise
// restating something already reported.
func applyAnomalyFlags(days []DailyReconciliation) {
	incomplete := make([]bool, len(days))
	for i, d := range days {
		incomplete[i] = hasFlagType(d.DiscrepancyFlags, FlagMissingDeliverySource)
	}

	for i := range days {
		if incomplete[i] {
			continue
		}

		var windowSum, windowCount int64
		for j := i - 1; j >= 0 && windowCount < TrailingWindowDays; j-- {
			if incomplete[j] {
				continue
			}
			windowSum += grossTotalCentsOf(days[j])
			windowCount++
		}
		if windowCount == 0 {
			continue // start of the series: no baseline to compare against yet
		}

		avg := windowSum / windowCount
		if avg == 0 {
			continue
		}

		current := grossTotalCentsOf(days[i])
		deviation := float64(current-avg) / float64(avg)
		if deviation < 0 {
			deviation = -deviation
		}

		if deviation > AnomalyThresholdPct {
			days[i].DiscrepancyFlags = append(days[i].DiscrepancyFlags, DiscrepancyFlag{
				Type: FlagAnomalyThresholdExceeded,
				Detail: fmt.Sprintf(
					"gross revenue %s deviates %.1f%% from the trailing %d-day average %s (threshold %.0f%%)",
					money.FormatCents(current), deviation*100, windowCount, money.FormatCents(avg), AnomalyThresholdPct*100,
				),
			})
		}
	}
}

func grossTotalCentsOf(d DailyReconciliation) int64 {
	var total int64
	for _, v := range d.GrossSalesBySource {
		total += v
	}
	return total
}

func hasFlagType(flags []DiscrepancyFlag, flagType string) bool {
	for _, f := range flags {
		if f.Type == flagType {
			return true
		}
	}
	return false
}
