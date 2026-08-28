package httpapi

// POST /api/client-errors: the "retro feed" — a real, timestamped record of
// a frontend crash the React ErrorBoundary caught about itself. Built after
// a real bug (a stale, pre-schema-change chat message shape breaking the
// renderer) was diagnosed by reading git history and reproducing manually,
// because there was no record of the crash actually happening — only a
// person's report of it hours later. This endpoint exists so the next crash
// leaves a queryable trace instead of depending on someone noticing.
//
// Deliberately unauthenticated and un-rate-limited, matching every other
// endpoint in this single-owner prototype (see docs/rfc-multi-tenant.md's
// honesty about what a real deployment would need that this one doesn't).
// No PII is accepted: message/component/stack/url/user_agent only, never
// arbitrary request/state payloads that could carry a customer's own data
// into an error log nobody scoped for that.

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// ClientErrorReportRequest is POST /api/client-errors' request body. Message
// is the only required field — a report with no message is not a report.
type ClientErrorReportRequest struct {
	Message   string `json:"message"`
	Component string `json:"component,omitempty"`
	Stack     string `json:"stack,omitempty"`
	URL       string `json:"url,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
}

// HandleRecordClientError implements POST /api/client-errors.
func HandleRecordClientError(q storage.Querier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}

		var req ClientErrorReportRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_input", "invalid JSON body")
			return
		}
		if req.Message == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_input", "message is required")
			return
		}

		_, err := q.RecordClientErrorReport(r.Context(), storage.RecordClientErrorReportParams{
			Message:   req.Message,
			Component: optionalText(req.Component),
			Stack:     optionalText(req.Stack),
			Url:       optionalText(req.URL),
			UserAgent: optionalText(req.UserAgent),
		})
		if err != nil {
			// A failed error-report write must not itself become a second
			// error the caller has to handle — the frontend already gave up
			// on its own render tree; report success either way (the write
			// is best-effort telemetry, not a guarantee this endpoint makes).
			writeJSON(w, http.StatusAccepted, map[string]bool{"logged": false})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]bool{"logged": true})
	}
}

func optionalText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}
