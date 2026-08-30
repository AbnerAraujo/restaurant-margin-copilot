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
	"strings"
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

// PlatformComparisonResponse is GET /api/platforms' body — the exact
// mcptools.PlatformComparisonResult shape compare_platform_economics
// returns, so the dedicated Platforms page and a chat answer about the same
// period read one rendering of one computation (spec 003-platform-comparator
// SC-003: "the two surfaces never disagree").
type PlatformComparisonResponse = mcptools.PlatformComparisonResult

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

// HandlePlatformComparison implements GET /api/platforms: iFood's and Just
// Eat Takeaway's gross sales, commission, effective rate, and promo-adjusted
// combined cost/rate for one period, via the exact same
// mcptools.ComparePlatformEconomics compare_platform_economics itself calls
// — one computation, read here directly (no model involved) and narrated in
// chat, never two.
//
// Unlike HandleReconciliation/HandlePromotions, an omitted ?start/?end here
// does NOT default to the wide-open 1900..2999 sentinel parseOptionalPeriod
// uses for "everything persisted": ComparePlatformEconomics refuses
// (insufficient_data) on any calendar day in range with no persisted
// reconciliation, and iterating a ~1000-year range to discover that would be
// wasted work for a query this endpoint can answer directly. It defaults
// instead to the real data's own [min, max] date range
// (storage.LoadDataDateRange) — the same "ground against what the data
// actually covers" convention cmd/server/main.go already applies for the
// chat surface.
func HandlePlatformComparison(q storage.Querier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}

		start, end, err := resolvePlatformComparisonPeriod(r, q)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_period", err.Error())
			return
		}

		result, toolErr, err := mcptools.ComparePlatformEconomics(r.Context(), q, mcptools.Period{
			Start: start.Format(dateLayout),
			End:   end.Format(dateLayout),
		})
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "query_failed", err.Error())
			return
		}
		if toolErr != nil {
			writeJSONError(w, http.StatusUnprocessableEntity, toolErr.Error, toolErrorDetail(toolErr))
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}

// resolvePlatformComparisonPeriod reads optional ?start/?end query
// parameters (both, or neither — a lone bound would leave the other at the
// wide-open sentinel, which is exactly the ~1000-year range this handler's
// doc comment explains is the wrong default here), falling back to the real
// persisted data's own date range when neither is given.
func resolvePlatformComparisonPeriod(r *http.Request, q storage.Querier) (start, end time.Time, err error) {
	s, e := r.URL.Query().Get("start"), r.URL.Query().Get("end")
	if s == "" && e == "" {
		return storage.LoadDataDateRange(r.Context(), q)
	}
	if s == "" || e == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("both start and end are required together, or neither")
	}
	start, err = time.Parse(dateLayout, s)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("start %q is not YYYY-MM-DD", s)
	}
	end, err = time.Parse(dateLayout, e)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("end %q is not YYYY-MM-DD", e)
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("end %s is before start %s", e, s)
	}
	return start, end, nil
}

// toolErrorDetail renders a mcptools.ToolError's Reason/Missing into a
// single human-readable string for writeJSONError's flat {error, detail}
// shape — this endpoint's only consumer of a *mcptools.ToolError, so no
// richer shape exists yet for it to preserve.
func toolErrorDetail(e *mcptools.ToolError) string {
	if e.Reason != "" {
		return e.Reason
	}
	if len(e.Missing) > 0 {
		return "missing reconciliation for: " + strings.Join(e.Missing, ", ")
	}
	return e.Error
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
	WriteError(w, status, code, detail)
}

// WriteError is writeJSONError, exported for internal/bff.
//
// The BFF layer (specs/013-bff-layer) refuses two things before a handler
// ever runs — a method the route does not serve, and a panic — and both
// refusals have to arrive in the SAME {error, detail} envelope every
// handler in this package already produces, because the frontend's
// lib/api.ts parses exactly one shape for every verb (its ApiError).
//
// Exported rather than duplicated in internal/bff on purpose: two
// definitions of one wire envelope is the drift this whole spec exists to
// remove, and reintroducing it one package over to avoid an exported
// identifier would be a poor trade. One implementation, two callers.
func WriteError(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "detail": detail})
}
