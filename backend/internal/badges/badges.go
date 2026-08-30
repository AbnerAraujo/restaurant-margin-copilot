// Package badges evaluates deterministic, typed "badge" facts about
// already-computed data (Constitution Principle I: the model may narrate a
// badge in conversation, never decide one — see docs/product-strategy.md's
// "Badge system" section). The original build covered only the
// Reconciliation category: "Clean Close" and "Discrepancy Catcher", both
// firing directly off DailyReconciliation.discrepancy_flags. Spec
// 002-badge-expansion adds three more, each reading a different
// already-computed source rather than deciding anything new here:
//
//   - Growth reads promotion_roi_record's existing ROI computation
//     (positive, attributable ROI -> badge).
//   - Engagement reads the new usage_event table (real, persisted app-opens
//     -> distinct-days-used count -> milestone badge).
//   - Campaign-Creation reads promotion_roi_record's origin/
//     replaces_campaign_id columns (an owner-created record naming a
//     flagged campaign it replaces -> badge).
//
// Badges are computed at read time, not persisted: there is no badge table
// in migrations/, and nothing in this package writes to Postgres. A
// Postgres enum was considered and rejected for exactly the reason a badge
// table was — product-strategy.md frames badges as "a typed, extensible
// category", and a small closed Go type serves that just as well while
// keeping the whole feature append-only Go code, not a schema migration,
// for something that isn't stored state in the first place. This holds for
// all five badge codes, including the two new ones with new tables/columns
// behind them: the TABLES are new persisted facts, the BADGES derived from
// them are still never persisted themselves.
package badges

import (
	"encoding/json"
	"net/http"
	"sort"
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
	// CodeGrowth fires for a promotion whose ROI (the existing
	// get_promotion_roi / list_negative_roi_promotions computation) is
	// positive and attributable — the positive-outcome acknowledgment a
	// negative-ROI campaign already got via the flagged_negative flag, but a
	// profitable one never did (spec 002 User Story 1).
	CodeGrowth Code = "growth"
	// CodeEngagement fires on a real, persisted app-usage milestone
	// (usage_event) — never a simulated, default, or pre-seeded streak
	// (spec 002 User Story 2). Only the "Week One" milestone (7 distinct
	// real usage days) is implemented; see EvaluateEngagementBadges.
	CodeEngagement Code = "engagement"
	// CodeCampaignCreation fires when the owner logs a new promotion record
	// in this product AND marks it as replacing a promotion
	// list_negative_roi_promotions currently flags negative — the
	// insight-to-action loop docs/product-strategy.md names as this badge's
	// purpose (spec 002 User Story 3). Logging a promotion with no
	// replacement claim earns nothing: this badge rewards responding to a
	// flagged problem specifically, not promotion-logging in general
	// (spec Acceptance Scenario 2 — a deliberate, stated modeling choice).
	CodeCampaignCreation Code = "campaign_creation"
)

// names gives each Code its display name, so the JSON response never makes
// a caller re-derive human text from a machine code. CodeEngagement's name
// is the one implemented milestone's name ("Week One") rather than the
// category's own name, since — today — the two are the same badge; adding a
// second milestone later means this map stops being a 1:1 Code->Name lookup
// and gains a milestone dimension, not a redesign of Badge itself.
var names = map[Code]string{
	CodeCleanClose:         "Clean Close",
	CodeDiscrepancyCatcher: "Discrepancy Catcher",
	CodeGrowth:             "Growth",
	CodeEngagement:         "Week One",
	CodeCampaignCreation:   "Campaign Launcher",
}

// categories groups every Code under the badge CATEGORY it belongs to
// (Reconciliation, Growth, Engagement, Campaign-Creation) — FR-009's
// requirement that a badge's category be explicitly distinguishable in the
// GET /api/badges response, not something a client has to infer by knowing
// which codes belong to which category itself.
var categories = map[Code]string{
	CodeCleanClose:         "reconciliation",
	CodeDiscrepancyCatcher: "reconciliation",
	CodeGrowth:             "growth",
	CodeEngagement:         "engagement",
	CodeCampaignCreation:   "campaign_creation",
}

// Badge is one badge instance earned for one calendar day (Reconciliation,
// Growth) or one milestone (Engagement, Campaign-Creation). Every
// DailyReconciliation day earns exactly one of the two Reconciliation-
// category badges — CleanClose and DiscrepancyCatcher are complementary by
// construction (a day's discrepancy_flags is either empty or it isn't), not
// independently-evaluated conditions that happen not to overlap. The three
// new categories are each independently evaluated against their own data
// (a promotion, a set of usage days, an owner-created record) and are not
// mutually exclusive with each other or with Reconciliation.
type Badge struct {
	Date                 string   `json:"date"`
	Code                 Code     `json:"code"`
	Name                 string   `json:"name"`
	Category             string   `json:"category"`
	DiscrepancyFlagTypes []string `json:"discrepancy_flag_types,omitempty"`
	// CampaignID identifies which promotion earned a Growth or
	// Campaign-Creation badge. Empty for Reconciliation/Engagement badges,
	// which are not about any one campaign.
	CampaignID string `json:"campaign_id,omitempty"`
	// ReplacesCampaignID is set only on a Campaign-Creation badge: the
	// flagged campaign the new promotion was logged to replace, so the
	// badge is auditable against the exact claim that earned it.
	ReplacesCampaignID string `json:"replaces_campaign_id,omitempty"`
	// UsageDays is set only on an Engagement badge: how many distinct real
	// usage days it fired at, so the badge is auditable against usage_event
	// rather than a bare, unexplained milestone name.
	UsageDays int `json:"usage_days,omitempty"`
}

const dateLayout = "2006-01-02"

// WeekOneThresholdDays is the number of distinct real usage days required
// for the "Week One" Engagement milestone (spec 002 Assumptions: "not
// necessarily consecutive").
const WeekOneThresholdDays = 7

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
//
// Spec 002-badge-expansion's three additions, per plan.md's proposed
// starting values (confirmed here, not adjusted — the reasoning holds):
//
//	Growth               15 pts — a real positive outcome, worth more than
//	                              the routine Clean Close but less than a
//	                              Discrepancy Catcher: this badge system's
//	                              stated priority is surfacing and acting on
//	                              PROBLEMS (docs/product-strategy.md), so a
//	                              good outcome is worth less than catching a
//	                              bad one, even though both are worth
//	                              acknowledging.
//	Engagement            5 pts — deliberately the smallest weight of the
//	                              five. It rewards showing up, not a specific
//	                              save or outcome, and — with only the "Week
//	                              One" milestone implemented (Assumptions) —
//	                              fires at most once ever per install rather
//	                              than accumulating daily, so a small
//	                              per-milestone value keeps it proportionate
//	                              to what it actually took to earn.
//	Campaign Launcher    30 pts — the highest of any badge. It is the one
//	                              category that required the owner to act on
//	                              an insight the product surfaced
//	                              (list_negative_roi_promotions) rather than
//	                              the product observing an outcome on its
//	                              own — the exact insight-to-action loop
//	                              docs/product-strategy.md names as this
//	                              whole badge system's purpose, so it is
//	                              weighted to match that stated priority.
const (
	PointsCleanClose         = 10
	PointsDiscrepancyCatcher = 25
	PointsGrowth             = 15
	PointsEngagement         = 5
	PointsCampaignCreation   = 30
)

// CentsPerPoint is the fixed, disclosed rate a Steward point redeems for
// when an owner pays for a promotion's spend with points instead of money
// (POST /api/promotions, internal/httpapi) — 1 point = $0.10. Never
// adjusted per-owner or per-promotion — one rate, everywhere, so "how many
// points is this" is always the same arithmetic.
//
// This comment used to add "at this build's real 200-point balance, that's
// $20.00 of campaign spend fundable from points alone". That was true of a
// database holding 14 days. Against the 759 days it holds now the real
// balance is 12,345 points (GET /api/badges, 2026-08-30), and the rate
// stops reading as modest at that scale. Deliberately not re-tuned here:
// the rate is a disclosed product decision, the balance is a consequence of
// how much history is loaded, and a comment that quotes a live number ages
// into a false one — which is exactly what happened. Any real
// discussion of the redemption economics belongs in
// docs/product-strategy.md, not in a constant's doc comment.
const CentsPerPoint = 10

// PointsNeededForSpend converts a campaign's spend into the whole number of
// points required to cover it, rounding UP (never down) so a redemption
// always covers the full spend — the owner never ends up short a few cents
// because a fractional point got silently dropped.
func PointsNeededForSpend(spendCents int64) int {
	if spendCents <= 0 {
		return 0
	}
	return int((spendCents + CentsPerPoint - 1) / CentsPerPoint)
}

// pointsByCode is the single lookup both the total and the breakdown read
// from, so a new badge code can never be worth points in one and not the
// other.
var pointsByCode = map[Code]int{
	CodeCleanClose:         PointsCleanClose,
	CodeDiscrepancyCatcher: PointsDiscrepancyCatcher,
	CodeGrowth:             PointsGrowth,
	CodeEngagement:         PointsEngagement,
	CodeCampaignCreation:   PointsCampaignCreation,
}

// pointsBreakdownOrder is EvaluatePoints' fixed emission order — every code,
// old and new, in one place, so the breakdown's stable ordering guarantee
// (TestEvaluatePointsBreakdownOrderIsStable) extends to the three new codes
// without a second constant to keep in sync.
var pointsBreakdownOrder = []Code{
	CodeCleanClose,
	CodeDiscrepancyCatcher,
	CodeGrowth,
	CodeEngagement,
	CodeCampaignCreation,
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
//
// Total is what EvaluatePoints computes below: a pure function of earned
// badges, no mutable counter, exactly as this package's doc comment
// promises. Spent and Available are NOT computed here — EvaluatePoints has
// no storage access and never will (this package's whole point is staying a
// pure function of already-evaluated badges) — they are set by the caller
// (RegisterBadgeHandler) from storage.SumPointsSpentOnPromotions, the other
// real, persisted fact a spendable balance needs. Available is just as
// deterministic as Total, only derived from two real sources instead of
// one: Total minus Spent, never a value written or adjusted directly.
type Points struct {
	Total     int          `json:"total"`
	Breakdown []PointsLine `json:"breakdown"`
	// Spent is every point already redeemed against a promotion's spend,
	// across all time (storage.SumPointsSpentOnPromotions). Zero on a
	// fresh instance, never omitted.
	Spent int `json:"spent"`
	// Available is Total-Spent — what's actually left to redeem right now.
	// Never negative: a redemption that would take it below zero is refused
	// server-side before it can ever be persisted (POST /api/promotions).
	Available int `json:"available"`
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
	for _, code := range pointsBreakdownOrder {
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
				Date:     dateStr,
				Code:     CodeCleanClose,
				Name:     names[CodeCleanClose],
				Category: categories[CodeCleanClose],
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
			Category:             categories[CodeDiscrepancyCatcher],
			DiscrepancyFlagTypes: types,
		})
	}
	return out
}

// EvaluateGrowthBadges evaluates the Growth category (spec 002 User Story 1)
// against already-computed PromotionRoiRecord rows — the same records
// get_promotion_roi and list_negative_roi_promotions read. A promotion earns
// a Growth badge exactly when its ROI is both attributable and strictly
// positive; FR-002 excludes negative, zero, and unattributable (nil) ROI
// alike — a zero ROI broke even, which is not growth, and a nil ROI is an
// unknown outcome, not a growth outcome (spec Edge Cases).
//
// Dated to PeriodEnd, not PeriodStart: what a Growth badge acknowledges is
// the promotion's RESULT, which is only knowable once its period has
// closed — the same reason list_negative_roi_promotions itself is a
// period-overlap query rather than a start-date one.
func EvaluateGrowthBadges(promotions []reconcile.PromotionRoiRecord) []Badge {
	out := make([]Badge, 0, len(promotions))
	for _, p := range promotions {
		if p.ROICents == nil || *p.ROICents <= 0 {
			continue
		}
		out = append(out, Badge{
			Date:       p.PeriodEnd.Format(dateLayout),
			Code:       CodeGrowth,
			Name:       names[CodeGrowth],
			Category:   categories[CodeGrowth],
			CampaignID: p.CampaignID,
		})
	}
	return out
}

// EvaluateEngagementBadges evaluates the Engagement category (spec 002 User
// Story 2) from real, persisted usage_event days — never from a placeholder
// or pre-seeded count (FR-004, SC-002). usageDays need not be pre-sorted or
// pre-deduplicated by the caller: this function does both defensively, even
// though storage.LoadDistinctUsageDays already guarantees distinct, ordered
// days at the database layer (usage_event_occurred_on_idx's unique index on
// the generated occurred_on column) — a second reading of the same
// guarantee costs nothing here and means this function's own correctness
// never silently depends on a caller upholding a contract it could enforce
// itself.
//
// Only the "Week One" milestone is implemented (spec Assumptions: a stricter
// consecutive-streak variant, or further milestones like "Month One", are
// explicitly out of scope for this spec) — a fresh instance with fewer than
// WeekOneThresholdDays distinct days earns nothing, never a fabricated or
// partial badge.
func EvaluateEngagementBadges(usageDays []time.Time) []Badge {
	days := uniqueSortedDays(usageDays)
	if len(days) < WeekOneThresholdDays {
		return nil
	}
	return []Badge{{
		Date:      days[WeekOneThresholdDays-1].Format(dateLayout),
		Code:      CodeEngagement,
		Name:      names[CodeEngagement],
		Category:  categories[CodeEngagement],
		UsageDays: len(days),
	}}
}

// uniqueSortedDays collapses usageDays to one entry per distinct calendar
// day (dateLayout precision — occurred_on is already a DATE, but this stays
// defensive rather than assuming its caller passed exactly that), sorted
// oldest first, so "the 7th distinct day" is well-defined regardless of the
// order rows arrived in.
func uniqueSortedDays(usageDays []time.Time) []time.Time {
	seen := make(map[string]struct{}, len(usageDays))
	out := make([]time.Time, 0, len(usageDays))
	for _, d := range usageDays {
		key := d.Format(dateLayout)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

// EvaluateCampaignCreationBadges evaluates the Campaign-Creation category
// (spec 002 User Story 3): an owner_created promotion_roi_record that names
// a promotion it replaces earns one badge. A record with no replaces
// reference — or any ingested (non-owner_created) record — earns nothing;
// per FR-008 and spec Acceptance Scenario 2, this badge rewards responding
// to a flagged problem specifically, not promotion-logging in general, a
// deliberate modeling choice stated here rather than left implicit.
//
// A flagged campaign can only genuinely be "replaced" once: this is the
// source-of-truth fix for a QA-found double-award bug where the SAME
// already-replaced campaign_id was offered again in the replaces dropdown
// (a stale frontend derivation) and, on a second submission, earned a
// SECOND Campaign Launcher badge and a second points award for one real
// replacement. POST /api/promotions (internal/httpapi/promotions_create.go)
// now refuses to CREATE a second record naming an already-replaced
// campaign_id, but this evaluator stays independently defensive — a race
// between two concurrent requests, a direct API call bypassing that check,
// or any future write path into this table must never be able to make the
// badge/points system itself double-count. Only the EARLIEST
// owner_created record (by CreatedAt) naming a given ReplacesCampaignID
// earns the badge; any later record naming that same target is treated as
// the duplicate it is and earns nothing.
//
// Dated to CreatedAt — the act of logging the replacement is what this badge
// acknowledges, not the replaced campaign's own dates or the new
// promotion's own period (spec Edge Cases: a badge earned for creating a
// record is honestly gone if that record is later deleted, which CreatedAt
// naturally reflects since it can only exist while the row does).
func EvaluateCampaignCreationBadges(promotions []reconcile.PromotionRoiRecord) []Badge {
	candidates := make([]reconcile.PromotionRoiRecord, 0, len(promotions))
	for _, p := range promotions {
		if p.Origin != reconcile.OriginOwnerCreated {
			continue
		}
		if p.ReplacesCampaignID == nil || *p.ReplacesCampaignID == "" {
			continue
		}
		candidates = append(candidates, p)
	}

	// Sort a copy by CreatedAt ascending — the input order here is whatever
	// storage.LoadAllPromotionRoiRecords returns (period, then campaign_id),
	// not creation order, so "earliest replacement wins" must sort
	// explicitly rather than trust caller order.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
	})

	out := make([]Badge, 0, len(candidates))
	alreadyReplaced := make(map[string]struct{}, len(candidates))
	for _, p := range candidates {
		replaces := *p.ReplacesCampaignID
		if _, dup := alreadyReplaced[replaces]; dup {
			continue
		}
		alreadyReplaced[replaces] = struct{}{}
		out = append(out, Badge{
			Date:               p.CreatedAt.Format(dateLayout),
			Code:               CodeCampaignCreation,
			Name:               names[CodeCampaignCreation],
			Category:           categories[CodeCampaignCreation],
			CampaignID:         p.CampaignID,
			ReplacesCampaignID: replaces,
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
// reconciliation rows, promotion records, and usage days. Split out from the
// handler so the wire contract the frontend reads can be asserted directly,
// without standing up a fake implementation of the whole storage.Querier
// interface for a function that touches none of it.
//
// All five badge codes are combined into one Badges slice per FR-009 — but
// FR-009 requires each one's category to stay DISTINGUISHABLE, which is what
// Badge.Category (not slice position) guarantees: a client can always filter
// or group by category without knowing the Code->category mapping itself.
func BuildResponse(days []reconcile.DailyReconciliation, promotions []reconcile.PromotionRoiRecord, usageDays []time.Time) Response {
	earned := EvaluateReconciliationBadges(days)
	earned = append(earned, EvaluateGrowthBadges(promotions)...)
	earned = append(earned, EvaluateCampaignCreationBadges(promotions)...)
	earned = append(earned, EvaluateEngagementBadges(usageDays)...)
	return Response{Badges: earned, Points: EvaluatePoints(earned)}
}

// RegisterBadgeHandler returns a plain REST handler for GET /api/badges —
// deliberately NOT an MCP tool (tasks.md T032): no functional requirement
// asks the model to narrate badges, and this is deterministic UI state, not
// something to route through the Principle III tool boundary that exists
// for the model layer specifically.
//
// Optional ?start=YYYY-MM-DD&end=YYYY-MM-DD query parameters scope the
// evaluated period FOR RECONCILIATION BADGES ONLY (Clean Close /
// Discrepancy Catcher) — matching this handler's pre-existing behavior.
// Growth, Engagement, and Campaign-Creation are deliberately evaluated over
// ALL persisted data regardless of the query, the same way GET /api/promotions
// is a full listing rather than a period query (see internal/httpapi/data.go's
// own comment on that choice): "which campaigns paid for themselves" and
// "how many distinct days has this app been used" are not questions a
// default date window can answer without silently omitting real badges.
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

		promotions, err := storage.LoadAllPromotionRoiRecords(r.Context(), q)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "query_failed", err.Error())
			return
		}

		usageDays, err := storage.LoadDistinctUsageDays(r.Context(), q)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "query_failed", err.Error())
			return
		}

		spent, err := storage.SumPointsSpentOnPromotions(r.Context(), q)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "query_failed", err.Error())
			return
		}

		resp := BuildResponse(days, promotions, usageDays)
		resp.Points.Spent = spent
		resp.Points.Available = resp.Points.Total - spent

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
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
