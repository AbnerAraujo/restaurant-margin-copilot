package httpapi

// Plain read-only REST over the deterministic reconciliation output, for the
// pages that are NOT a conversation: GET /api/reconciliation (the Close page)
// and GET /api/promotions (the Promotions page).
//
// Deliberately NOT MCP tools, for the same reason GET /api/badges isn't
// (internal/badges' handler doc): the Principle III tool boundary exists to
// constrain what the MODEL can reach. These endpoints serve a chart that a
// human is looking at directly, with no model anywhere in the request path,
// so routing them through the tool layer would add a boundary that guards
// nothing while implying the model is involved when it isn't.
//
// What they DO share with the tool layer is the rendering: both handlers
// return internal/mcptools' own result shapes, via its exported
// NewDailySummaryResult / NewPromotionRoiResult. One conversion means the
// number on the Close page and the number the model narrates for the same
// day are the same number, formatted the same way, by the same code — they
// cannot drift apart, because there is only one of them.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/mcptools"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

const dateLayout = "2006-01-02"

// ReconciliationResponse is GET /api/reconciliation's body.
type ReconciliationResponse struct {
	// Start/End are the period actually served — echoed back so a client that
	// omitted them knows what range it got, rather than assuming one.
	Start string `json:"start"`
	End   string `json:"end"`
	// Days holds one entry per persisted DailyReconciliation in range. A
	// calendar day with no reconciliation is simply ABSENT, never present
	// with zeroed figures: a missing day and a zero day are different facts,
	// and the chart draws them differently (Constitution Principle II).
	Days []*mcptools.DailySummaryResult `json:"days"`
}

// PromotionsResponse is GET /api/promotions' body.
type PromotionsResponse struct {
	Promotions []mcptools.PromotionRoiView `json:"promotions"`
}

// HandleReconciliation implements GET /api/reconciliation. Optional
// ?start=YYYY-MM-DD&end=YYYY-MM-DD scope the period; omitting either serves
// everything persisted, matching GET /api/badges' behaviour so the two
// endpoints a page calls together default to the same window.
func HandleReconciliation(q storage.Querier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}

		start, end, err := parseOptionalPeriod(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_period", err.Error())
			return
		}

		days, err := storage.LoadDailyReconciliationsInPeriod(r.Context(), q, start, end)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "query_failed", err.Error())
			return
		}

		out := make([]*mcptools.DailySummaryResult, 0, len(days))
		for _, day := range days {
			out = append(out, mcptools.NewDailySummaryResult(day))
		}

		writeJSON(w, http.StatusOK, ReconciliationResponse{
			Start: servedBound(start, days, true),
			End:   servedBound(end, days, false),
			Days:  out,
		})
	}
}

// HandlePromotions implements GET /api/promotions: every persisted
// PromotionRoiRecord, in the same shape get_promotion_roi returns — roi null
// with reason "attribution_unavailable" where FR-013 applies, never a
// computed-looking placeholder.
func HandlePromotions(q storage.Querier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}

		records, err := storage.LoadAllPromotionRoiRecords(r.Context(), q)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "query_failed", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, PromotionsResponse{
			Promotions: mcptools.NewPromotionRoiResult(records).Promotions,
		})
	}
}

// parseOptionalPeriod reads optional start/end query parameters, defaulting
// to a wide-open range so an omitted period means "everything persisted",
// not "nothing" (the same convention internal/badges uses).
func parseOptionalPeriod(r *http.Request) (start, end time.Time, err error) {
	start = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
	end = time.Date(2999, 12, 31, 0, 0, 0, 0, time.UTC)

	if s := r.URL.Query().Get("start"); s != "" {
		start, err = time.Parse(dateLayout, s)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("start %q is not YYYY-MM-DD", s)
		}
	}
	if e := r.URL.Query().Get("end"); e != "" {
		end, err = time.Parse(dateLayout, e)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("end %q is not YYYY-MM-DD", e)
		}
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("end %s is before start %s", end.Format(dateLayout), start.Format(dateLayout))
	}
	return start, end, nil
}

// servedBound reports the period actually covered. When the caller supplied
// no bound, the sentinel year 1900/2999 would be a misleading thing to echo
// back, so the real data's own first/last date is reported instead — and an
// empty result reports an empty string rather than inventing a range for a
// period that returned nothing.
func servedBound(requested time.Time, days []reconcile.DailyReconciliation, isStart bool) string {
	sentinelYear := 2999
	if isStart {
		sentinelYear = 1900
	}
	if requested.Year() != sentinelYear {
		return requested.Format(dateLayout)
	}
	if len(days) == 0 {
		return ""
	}
	if isStart {
		return days[0].Date.Format(dateLayout)
	}
	return days[len(days)-1].Date.Format(dateLayout)
}

func writeJSONError(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "detail": detail})
}
