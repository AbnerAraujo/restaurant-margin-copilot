package httpapi

// POST /api/usage: the real app-open ping backing Engagement badge
// evaluation (spec 002-badge-expansion User Story 2, FR-003). Deliberately
// the thinnest possible handler — the actual "no double-counting within a
// calendar day, no manual dedup required of the caller" guarantee lives at
// the database layer (migrations/000003's unique index on a generated
// column), not here. This handler's only job is to call it and report
// whether today was new, never to compute or decide anything about
// engagement itself (that is internal/badges' job, reading the table this
// endpoint writes to).

import (
	"net/http"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// RecordUsageResponse tells the caller whether this call was the day's
// FIRST recorded usage (recorded=true) or a repeat ping on a day already on
// file (recorded=false) — informational only; the frontend does not need to
// branch on it (see the ping's own doc comment for why it fires once per
// app load regardless).
type RecordUsageResponse struct {
	Recorded bool `json:"recorded"`
}

// HandleRecordUsage implements POST /api/usage.
func HandleRecordUsage(q storage.Querier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}

		recorded, err := storage.RecordUsageEvent(r.Context(), q)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "query_failed", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, RecordUsageResponse{Recorded: recorded})
	}
}
