package httpapi

// GET /api/platforms/trend backs spec 008 FR-007's Platforms effective-rate
// trend chart: a bounded, fully deterministic (no model call) sequence of
// mcptools.ComparePlatformEconomics results across the trailing calendar
// months the real data actually covers.
//
// This is a genuinely new small backend aggregation, not an assemble-from-
// existing-endpoints frontend loop, per plan.md's own stated rule: fetching
// up to trailingMonths calendar months one at a time from the frontend
// would be a chattier request pattern than "2-3 sequential calls" and the
// existing GET /api/platforms already computes exactly this per-period
// result — this endpoint just calls it in a loop server-side instead.

import (
	"net/http"
	"time"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/mcptools"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// trailingMonths bounds how far back the trend looks — a deliberate,
// documented constant (not unbounded), matching this project's other
// "bounded, not unbounded" choices (e.g. paraphrase.MaxCandidates).
const trailingMonths = 6

// PlatformsTrendResponse is GET /api/platforms/trend's body.
type PlatformsTrendResponse struct {
	// Periods holds one entry per trailing calendar month that
	// ComparePlatformEconomics did NOT refuse with insufficient_data —
	// skipped months are simply absent, never zero-padded or fabricated
	// (spec 008 FR-013).
	Periods []PlatformsTrendPeriod `json:"periods"`
}

// PlatformsTrendPeriod is one real, non-refused month's result, tagged with
// the calendar month it covers so the frontend can label the x-axis without
// re-deriving it from Start/End.
type PlatformsTrendPeriod struct {
	Month  string                             `json:"month"` // YYYY-MM
	Result *mcptools.PlatformComparisonResult `json:"result"`
}

// HandlePlatformsTrend computes the trailing trailingMonths calendar months
// ending on the real data's own max date (storage.LoadDataDateRange) — the
// same "ground against what the data actually covers" convention
// HandlePlatformComparison already uses — and returns one
// PlatformsTrendPeriod per month ComparePlatformEconomics could actually
// answer.
func HandlePlatformsTrend(q storage.Querier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}

		_, dataEnd, err := storage.LoadDataDateRange(r.Context(), q)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "query_failed", err.Error())
			return
		}

		periods := make([]PlatformsTrendPeriod, 0, trailingMonths)
		// Walk backwards from the calendar month containing dataEnd, oldest
		// last in the loop but appended so the response is chronological
		// (oldest first) — a trend chart reads left-to-right as time
		// passing, and building the slice in that order once here is
		// simpler than asking every caller to reverse it.
		monthStarts := make([]time.Time, 0, trailingMonths)
		cursor := time.Date(dataEnd.Year(), dataEnd.Month(), 1, 0, 0, 0, 0, time.UTC)
		for i := 0; i < trailingMonths; i++ {
			monthStarts = append(monthStarts, cursor)
			cursor = cursor.AddDate(0, -1, 0)
		}

		for i := len(monthStarts) - 1; i >= 0; i-- {
			monthStart := monthStarts[i]
			monthEnd := monthStart.AddDate(0, 1, 0).AddDate(0, 0, -1)
			if monthEnd.After(dataEnd) {
				monthEnd = dataEnd
			}

			result, toolErr, err := mcptools.ComparePlatformEconomics(r.Context(), q, mcptools.Period{
				Start: monthStart.Format(dateLayout),
				End:   monthEnd.Format(dateLayout),
			})
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "query_failed", err.Error())
				return
			}
			if toolErr != nil {
				// insufficient_data for this one month — skipped, never
				// zero-padded or fabricated (FR-013). Any other tool error
				// class is unexpected here (period bounds are always valid
				// by construction) but is treated the same way: skip this
				// month rather than fail the whole trend.
				continue
			}

			periods = append(periods, PlatformsTrendPeriod{
				Month:  monthStart.Format("2006-01"),
				Result: result,
			})
		}

		writeJSON(w, http.StatusOK, PlatformsTrendResponse{Periods: periods})
	}
}
