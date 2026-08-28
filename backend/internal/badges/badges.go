// Package badges evaluates deterministic, typed "badge" facts about
// already-computed DailyReconciliation rows (Constitution Principle I: the
// model may narrate a badge in conversation, never decide one — see
// docs/product-strategy.md's "Badge system" section). Per that document,
// only the Reconciliation category is built now: "Clean Close" and
// "Discrepancy Catcher", both firing directly off
// DailyReconciliation.discrepancy_flags, "no new computation needed beyond
// what KR2 already produces." Growth, Engagement, and Campaign-Creation
// categories are named roadmap directions there, explicitly not built here.
//
// Badges are computed at read time, not persisted: there is no badge table
// in migrations/, and nothing in this package writes to Postgres. A
// Postgres enum was considered and rejected for exactly the reason a badge
// table was — product-strategy.md frames badges as "a typed, extensible
// category", and a small closed Go type serves that just as well while
// keeping the whole feature append-only Go code, not a schema migration,
// for something that isn't stored state in the first place.
package badges

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// Code identifies a badge type.
type Code string

const (
	// CodeCleanClose fires for a day whose discrepancy_flags is empty — a
	// day reconciled with zero discrepancies.
	CodeCleanClose Code = "clean_close"
	// CodeDiscrepancyCatcher fires for a day whose discrepancy_flags is
	// non-empty — the system caught and flagged something (a duplicate
	// order, a missing source, a commission mismatch, or an anomaly
	// threshold breach; see reconcile.DiscrepancyFlag's Flag* constants).
	CodeDiscrepancyCatcher Code = "discrepancy_catcher"
)

// names gives each Code its display name, so the JSON response never makes
// a caller re-derive human text from a machine code.
var names = map[Code]string{
	CodeCleanClose:         "Clean Close",
	CodeDiscrepancyCatcher: "Discrepancy Catcher",
}

// Badge is one badge instance earned for one calendar day. Every
// DailyReconciliation day earns exactly one of the two Reconciliation-
// category badges — CleanClose and DiscrepancyCatcher are complementary by
// construction (a day's discrepancy_flags is either empty or it isn't), not
// independently-evaluated conditions that happen not to overlap.
type Badge struct {
	Date                 string   `json:"date"`
	Code                 Code     `json:"code"`
	Name                 string   `json:"name"`
	DiscrepancyFlagTypes []string `json:"discrepancy_flag_types,omitempty"`
}

const dateLayout = "2006-01-02"

// --- Points ------------------------------------------------------------
//
// Points are a pure function of the badges already earned — same design
// principle as badges themselves: computed at read time, never persisted.
// There is no points table, no points column, and nothing in this package
// writes to Postgres. A points balance held as mutable state would be a
// second source of truth for something already fully determined by
// DailyReconciliation.discrepancy_flags, and the two could drift the moment
// a day is re-ingested and re-reconciled. Deriving instead means a
// re-ingestion silently corrects the balance, which is the only behaviour
// that stays honest.
//
// The weights are a product judgement, stated here rather than buried:
//
//	Clean Close          10 pts — the day closed with zero discrepancies.
//	                              Routine by design; the habit of closing
//	                              daily is what earns it, not the outcome.
//	Discrepancy Catcher  25 pts — the system caught and flagged something
//	                              real (a duplicate order, a missing source,
//	                              a commission mismatch, an anomaly breach).
//
// Weighting the catch HIGHER than the clean day is deliberate and is the
// same reading docs/product-strategy.md already takes of the badge itself:
// a caught anomaly is "a quiet 'system worked' acknowledgment, not a
// failure." The money this product saves is found on the days something was
// wrong, so those days are worth more — scoring them lower would quietly
// tell an owner that a day with a problem was a worse day to have used the
// product, which is the opposite of true.
const (
	PointsCleanClose         = 10
	PointsDiscrepancyCatcher = 25
)

// pointsByCode is the single lookup both the total and the breakdown read
// from, so a new badge code can never be worth points in one and not the
// other.
var pointsByCode = map[Code]int{
	CodeCleanClose:         PointsCleanClose,
	CodeDiscrepancyCatcher: PointsDiscrepancyCatcher,
}

// PointsLine is one badge code's contribution to the total — shown so the
// balance is auditable rather than a bare number the owner has to trust.
type PointsLine struct {
	Code       Code   `json:"code"`
	Name       string `json:"name"`
	Count      int    `json:"count"`
	PointsEach int    `json:"points_each"`
	Points     int    `json:"points"`
}

// Points is the full earned balance plus the arithmetic behind it.
type Points struct {
	Total     int          `json:"total"`
	Breakdown []PointsLine `json:"breakdown"`
}

// EvaluatePoints derives the points balance from already-evaluated badges.
// It takes badges rather than reconciliation rows on purpose: points must be
// a restatement of what the badge layer already decided, never an
// independent second reading of discrepancy_flags that could disagree with
// the badges shown beside it.
//
// The breakdown is emitted in a fixed code order (not map iteration order)
// so the same data always renders the same way.
func EvaluatePoints(earned []Badge) Points {
	counts := map[Code]int{}
	for _, badge := range earned {
		counts[badge.Code]++
	}

	points := Points{Breakdown: make([]PointsLine, 0, len(pointsByCode))}
	for _, code := range []Code{CodeCleanClose, CodeDiscrepancyCatcher} {
		count := counts[code]
		if count == 0 {
			continue
		}
		each := pointsByCode[code]
		subtotal := count * each
		points.Total += subtotal
		points.Breakdown = append(points.Breakdown, PointsLine{
			Code:       code,
			Name:       names[code],
			Count:      count,
			PointsEach: each,
			Points:     subtotal,
		})
	}
	return points
}

// EvaluateReconciliationBadges evaluates the two built-now Reconciliation
// -category badges against already-computed DailyReconciliation rows.
// Nothing here recomputes or reinterprets margin, discrepancies, or
// anomalies — it only reads discrepancy_flags, a field internal/reconcile
// already produced (Constitution Principle I: this package narrates a
// fact, it never decides one from raw data).
func EvaluateReconciliationBadges(days []reconcile.DailyReconciliation) []Badge {
	out := make([]Badge, 0, len(days))
	for _, d := range days {
		dateStr := d.Date.Format(dateLayout)

		if len(d.DiscrepancyFlags) == 0 {
			out = append(out, Badge{
				Date: dateStr,
				Code: CodeCleanClose,
				Name: names[CodeCleanClose],
			})
			continue
		}

		types := make([]string, 0, len(d.DiscrepancyFlags))
		for _, f := range d.DiscrepancyFlags {
			types = append(types, f.Type)
		}
		out = append(out, Badge{
			Date:                 dateStr,
			Code:                 CodeDiscrepancyCatcher,
			Name:                 names[CodeDiscrepancyCatcher],
			DiscrepancyFlagTypes: types,
		})
	}
	return out
}

// Response is GET /api/badges' body. Points ride on this response rather
// than a separate endpoint: they are a derivation of exactly the badges in
// the same payload, and splitting them across two requests would let a
// client render a balance that disagrees with the badges shown beside it if
// the two responses ever straddled an ingestion.
type Response struct {
	Badges []Badge `json:"badges"`
	Points Points  `json:"points"`
}

// BuildResponse assembles GET /api/badges' body from already-loaded
// reconciliation rows. Split out from the handler so the wire contract the
// frontend reads can be asserted directly, without standing up a fake
// implementation of the whole storage.Querier interface for a function that
// touches none of it.
func BuildResponse(days []reconcile.DailyReconciliation) Response {
	earned := EvaluateReconciliationBadges(days)
	return Response{Badges: earned, Points: EvaluatePoints(earned)}
}

// RegisterBadgeHandler returns a plain REST handler for GET /api/badges —
// deliberately NOT an MCP tool (tasks.md T032): no functional requirement
// asks the model to narrate badges, and this is deterministic UI state, not
// something to route through the Principle III tool boundary that exists
// for the model layer specifically.
//
// Optional ?start=YYYY-MM-DD&end=YYYY-MM-DD query parameters scope the
// evaluated period; when omitted, every persisted DailyReconciliation is
// evaluated.
func RegisterBadgeHandler(q storage.Querier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}

		start, end, err := parsePeriodQuery(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_period", err.Error())
			return
		}

		days, err := storage.LoadDailyReconciliationsInPeriod(r.Context(), q, start, end)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "query_failed", err.Error())
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(BuildResponse(days)); err != nil {
			// Headers are already sent at this point (Content-Type, status
			// 200 via the default); there is nothing left to do but log —
			// see cmd/server for how this handler is wired.
			return
		}
	}
}

// parsePeriodQuery reads optional start/end query parameters, defaulting to
// a wide-open range so an omitted period means "everything persisted", not
// "nothing".
func parsePeriodQuery(r *http.Request) (start, end time.Time, err error) {
	start = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
	end = time.Date(2999, 12, 31, 0, 0, 0, 0, time.UTC)

	if s := r.URL.Query().Get("start"); s != "" {
		start, err = time.Parse(dateLayout, s)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if e := r.URL.Query().Get("end"); e != "" {
		end, err = time.Parse(dateLayout, e)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	return start, end, nil
}

func writeJSONError(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "detail": detail})
}
