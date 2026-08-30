package httpapi

// POST /api/promotions: the owner logging a new promotion record directly in
// the app (spec 002-badge-expansion User Story 3). A genuine write path, not
// an MCP tool — the model never creates a promotion, so this stays outside
// the Principle III tool boundary the same way GET /api/promotions and
// GET /api/badges already do (see data.go's package doc), just on the write
// side instead of the read side.
//
// FR-007 is enforced HERE, server-side, against the live database, not
// against whatever the client's own UI currently shows: a "replaces" claim
// is re-verified by looking up the referenced campaign_id's OWN
// flagged_negative fact at submission time, exactly the fact
// list_negative_roi_promotions itself would report. A client cannot forge
// this by lying about what it displayed.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/badges"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/mcptools"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/money"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// CreatePromotionRequest is POST /api/promotions' request body (FR-005/
// FR-006): the same shape ingested promotions already carry (campaign
// identifier, platform, date range, spend) plus an optional "replaces"
// reference — deliberately not a full promotion-management payload (no
// attribution fields: an owner-created record has not been through
// attribution, per CreateOwnerPromotion's own doc comment).
type CreatePromotionRequest struct {
	Platform   string `json:"platform"`
	CampaignID string `json:"campaign_id"`
	Period     Period `json:"period"`
	// Spend is a decimal string ("120.00"), matching every other money field
	// this API accepts/returns (internal/money.FormatCents' convention) —
	// never a float, which could silently misrepresent a cents value.
	Spend string `json:"spend"`
	// Replaces is the campaign_id of an existing promotion this new record
	// is framed as replacing. Omitted or empty means no replacement claim —
	// FR-008: no claim, no Campaign-Creation badge, and no FR-007 check
	// runs at all.
	Replaces string `json:"replaces,omitempty"`
	// PaymentMethod is "money" (default when omitted, matching every
	// pre-existing record) or "points" — pay this campaign's spend from the
	// owner's earned Steward points balance instead of cash. Verified
	// server-side against the real, current earned-minus-spent balance
	// (badges.EvaluatePoints minus storage.SumPointsSpentOnPromotions), the
	// same "never trust the client's own math" discipline FR-007's
	// replaces-claim re-check already applies a few lines below.
	PaymentMethod string `json:"payment_method,omitempty"`
}

// Period is the {start, end} shape this request body uses — a local type
// rather than reusing mcptools.Period, since that type's date-parsing method
// is unexported to its own package (deliberately: internal/mcptools' three-
// way ToolError return is a model-facing convention this plain REST endpoint
// doesn't share).
type Period struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// CreatePromotionResponse is POST /api/promotions' success body: the created
// record, rendered through the exact same mcptools.PromotionRoiView shape
// GET /api/promotions and get_promotion_roi use (FR-005's "renders through
// the same surfaces"), plus whether logging it earned a Campaign-Creation
// badge — so the frontend form can say so immediately without a second
// GET /api/badges round trip.
type CreatePromotionResponse struct {
	Promotion           mcptools.PromotionRoiView `json:"promotion"`
	EarnedCampaignBadge bool                      `json:"earned_campaign_creation_badge"`
	// PointsBalanceAfter is the owner's real, current available points
	// balance immediately after this request — populated only when
	// PaymentMethod was "points", so the frontend can update its balance
	// display without a second GET /api/badges round trip, the same
	// convenience EarnedCampaignBadge already provides for the badge side.
	PointsBalanceAfter *int `json:"points_balance_after,omitempty"`
}

// HandleCreatePromotion implements POST /api/promotions.
//
// createMu (below) serializes every request from just past cheap,
// no-database input validation through the final insert. Found live in the
// same QA pass as commitMu (internal/httpapi/ingest_cost_sheet.go): the
// points-balance path recomputes `available := earned - alreadySpent` from
// live data and only checks it against the request's own cost afterward —
// a plain read-then-write with no lock or transaction between them. Two
// requests funding two different campaigns with points, submitted close
// together, could each read the SAME "available" balance before either had
// committed its spend, and both pass the check — overdrawing the points
// ledger negative for an economy this product otherwise guards carefully
// (badges.PointsNeededForSpend's whole point). A single in-process mutex is
// sufficient for the same reason commitMu's doc comment gives: this is a
// single-process server (cmd/server/main.go), a low-traffic owner-facing
// write path (nobody is meant to be submitting promotions in a hot loop),
// and this endpoint has no separate connection-pool-based transaction seam
// to hook into today. This also gives the "replaces" check-then-insert a
// second, application-level guard alongside migrations/
// 000012_replaces_campaign_unique.up.sql's unique index — belt and
// suspenders on the exact same bug class, not a replacement for it.
func HandleCreatePromotion(q storage.Querier) http.HandlerFunc {
	var createMu sync.Mutex
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}

		var req CreatePromotionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_body", fmt.Sprintf("could not parse request body as JSON: %v", err))
			return
		}

		platform := strings.TrimSpace(req.Platform)
		campaignID := strings.TrimSpace(req.CampaignID)
		replaces := strings.TrimSpace(req.Replaces)

		var missing []string
		if platform == "" {
			missing = append(missing, "platform")
		}
		if campaignID == "" {
			missing = append(missing, "campaign_id")
		}
		if req.Period.Start == "" || req.Period.End == "" {
			missing = append(missing, "period.start/period.end")
		}
		if req.Spend == "" {
			missing = append(missing, "spend")
		}
		if len(missing) > 0 {
			writeJSONError(w, http.StatusBadRequest, "invalid_input", fmt.Sprintf("missing required field(s): %s", strings.Join(missing, ", ")))
			return
		}

		// Defense in depth against exactly the bug a QA pass found: an
		// unconstrained free-text platform field let "iFood" and "Ifood"
		// diverge into two distinct database values (duplicate stat cards,
		// a filter that silently drops half a platform's campaigns,
		// under-reported spend). The frontend now offers a fixed <select>
		// over mcptools.KnownPlatformDisplayNames(), but this endpoint is a
		// genuine write path other callers could hit directly — refuse
		// anything that isn't exactly one of those known values rather than
		// silently accepting free text the client happened to send.
		if !mcptools.IsKnownPlatformDisplayName(platform) {
			writeJSONError(w, http.StatusBadRequest, "invalid_input",
				fmt.Sprintf("platform %q is not recognized — must be exactly one of: %s", platform, strings.Join(mcptools.KnownPlatformDisplayNames(), ", ")))
			return
		}

		start, end, err := parseStrictPeriod(req.Period.Start, req.Period.End)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_input", err.Error())
			return
		}

		spendCents, err := money.ParseCents(req.Spend)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_input", fmt.Sprintf("spend %q is not a valid decimal amount: %v", req.Spend, err))
			return
		}
		if spendCents < 0 {
			writeJSONError(w, http.StatusBadRequest, "invalid_input", "spend must not be negative")
			return
		}

		// From here on, this request reads and then acts on shared,
		// live-computed state (the points balance, the "already replaced"
		// fact) — see createMu's doc comment above for the exact race this
		// closes. Everything before this line is cheap, request-local
		// validation that touches no shared state and needs no lock.
		createMu.Lock()
		defer createMu.Unlock()

		paymentMethod := strings.TrimSpace(req.PaymentMethod)
		if paymentMethod == "" {
			paymentMethod = reconcile.PaymentMethodMoney
		}
		if paymentMethod != reconcile.PaymentMethodMoney && paymentMethod != reconcile.PaymentMethodPoints {
			writeJSONError(w, http.StatusBadRequest, "invalid_input",
				fmt.Sprintf("payment_method %q must be %q or %q", paymentMethod, reconcile.PaymentMethodMoney, reconcile.PaymentMethodPoints))
			return
		}

		var pointsSpentPtr *int
		var pointsBalanceAfter *int
		if paymentMethod == reconcile.PaymentMethodPoints {
			// Never trust a client-supplied balance — recompute the real,
			// current earned-minus-spent figure the exact same way
			// GET /api/badges does, from live data, at the moment this
			// request is being decided (the same discipline the FR-007
			// replaces-claim re-check just below already applies).
			pointsNeeded := badges.PointsNeededForSpend(spendCents)

			// The same wide-open "everything persisted" range GET
			// /api/badges defaults to (badges.parsePeriodQuery) when no
			// start/end query parameter is given — Growth/Engagement/
			// Campaign-Creation badges (and so the points they're worth)
			// are always evaluated over ALL data regardless of any window,
			// and a balance check here must see the exact same earned
			// total that endpoint would report right now.
			allTimeStart := time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
			allTimeEnd := time.Date(2999, 12, 31, 0, 0, 0, 0, time.UTC)

			allDays, err := storage.LoadDailyReconciliationsInPeriod(r.Context(), q, allTimeStart, allTimeEnd)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "query_failed", err.Error())
				return
			}
			allPromotions, err := storage.LoadAllPromotionRoiRecords(r.Context(), q)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "query_failed", err.Error())
				return
			}
			usageDays, err := storage.LoadDistinctUsageDays(r.Context(), q)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "query_failed", err.Error())
				return
			}
			alreadySpent, err := storage.SumPointsSpentOnPromotions(r.Context(), q)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "query_failed", err.Error())
				return
			}

			earned := badges.BuildResponse(allDays, allPromotions, usageDays).Points.Total
			available := earned - alreadySpent
			if pointsNeeded > available {
				writeJSONError(w, http.StatusUnprocessableEntity, "insufficient_points",
					fmt.Sprintf("this campaign needs %d points (at %d cents/point) to cover %s, but only %d points are available (%d earned, %d already redeemed) — refusing rather than partially funding it. Log it with payment_method \"money\" instead, or reduce spend.",
						pointsNeeded, badges.CentsPerPoint, req.Spend, available, earned, alreadySpent))
				return
			}

			pointsSpentPtr = &pointsNeeded
			after := available - pointsNeeded
			pointsBalanceAfter = &after
		}

		var replacesPtr *string
		if replaces != "" {
			// FR-007: refuse, rather than trust, an unverified "replaces"
			// claim — re-check the REAL, current flagged_negative fact for
			// this exact campaign_id, not whatever the submitting client's
			// own UI happened to be showing when the form was filled in.
			flagged, err := storage.IsCampaignFlaggedNegative(r.Context(), q, replaces)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "query_failed", err.Error())
				return
			}
			if !flagged {
				writeJSONError(w, http.StatusUnprocessableEntity, "replaces_not_flagged_negative",
					fmt.Sprintf("campaign_id %q is not currently flagged negative-ROI by list_negative_roi_promotions — refusing the replaces claim. The promotion can still be logged without a replaces reference.", replaces))
				return
			}

			// Defense against exactly the double-award bug a QA pass found:
			// a flagged campaign stayed offered in the frontend's "replaces"
			// dropdown after it had already been replaced once (a stale
			// client-side derivation), and submitting it again minted a
			// SECOND Campaign Launcher badge and a second points award for
			// the same real replacement. Re-verified here against live data
			// — a campaign can only genuinely be replaced once — rather
			// than trusting the client to have refreshed its own dropdown.
			alreadyReplaced, err := storage.IsCampaignAlreadyReplaced(r.Context(), q, replaces)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "query_failed", err.Error())
				return
			}
			if alreadyReplaced {
				writeJSONError(w, http.StatusConflict, "already_replaced",
					fmt.Sprintf("campaign_id %q has already been replaced by another promotion record — a flagged campaign can only be replaced once. Log this promotion without a replaces reference instead.", replaces))
				return
			}
			replacesPtr = &replaces
		}

		created, err := storage.CreateOwnerPromotion(r.Context(), q, storage.NewOwnerPromotion{
			Platform:           platform,
			CampaignID:         campaignID,
			PeriodStart:        start,
			PeriodEnd:          end,
			SpendCents:         spendCents,
			ReplacesCampaignID: replacesPtr,
			PaymentMethod:      paymentMethod,
			PointsSpent:        pointsSpentPtr,
		})
		if err != nil {
			if constraint, ok := uniqueViolationConstraint(err); ok {
				// Two distinct unique constraints can fire here, and they
				// mean two different things to the owner — see each
				// constraint's own migration for why it exists.
				if constraint == replacesCampaignUniqueConstraint {
					// The TOCTOU counterpart to the IsCampaignAlreadyReplaced
					// check above: that check and this insert are two
					// separate round trips, so two requests racing to
					// replace the SAME flagged campaign can both pass the
					// check before either commits. The database is the only
					// thing that sees both transactions, so it — not the
					// application check — is what actually closes this.
					// Same 409 the pre-insert check above already returns,
					// since to the owner this is the identical outcome:
					// "someone already replaced that campaign."
					writeJSONError(w, http.StatusConflict, "already_replaced",
						fmt.Sprintf("campaign_id %q has already been replaced by another promotion record — a flagged campaign can only be replaced once. Log this promotion without a replaces reference instead.", replaces))
					return
				}
				writeJSONError(w, http.StatusConflict, "already_exists",
					fmt.Sprintf("a promotion for platform %q, campaign_id %q, and this exact period already exists", platform, campaignID))
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "query_failed", err.Error())
			return
		}

		view := mcptools.NewPromotionRoiResult([]reconcile.PromotionRoiRecord{created}).Promotions[0]
		writeJSON(w, http.StatusCreated, CreatePromotionResponse{
			Promotion:           view,
			EarnedCampaignBadge: replacesPtr != nil,
			PointsBalanceAfter:  pointsBalanceAfter,
		})
	}
}

// parseStrictPeriod parses a required {start, end} pair, refusing (rather
// than defaulting) a malformed date or an end before its start — this
// endpoint has no "omitted means everything" convention the way the
// read-only GET endpoints do, since a promotion record MUST have a real
// period.
func parseStrictPeriod(startStr, endStr string) (start, end time.Time, err error) {
	start, err = time.Parse(dateLayout, startStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("period.start %q is not YYYY-MM-DD", startStr)
	}
	end, err = time.Parse(dateLayout, endStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("period.end %q is not YYYY-MM-DD", endStr)
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("period.end %s is before period.start %s", endStr, startStr)
	}
	return start, end, nil
}

// replacesCampaignUniqueConstraint is the partial unique index added by
// migrations/000012_replaces_campaign_unique.up.sql — see its own comment
// for the race it closes.
const replacesCampaignUniqueConstraint = "promotion_roi_record_replaces_campaign_id_idx"

// uniqueViolationConstraint reports whether err is Postgres'
// unique_violation (SQLSTATE 23505) and, if so, which constraint fired.
// Two constraints can fire from this handler's one insert —
// promotion_roi_record_platform_campaign_period_idx (a genuine duplicate
// submission) and replacesCampaignUniqueConstraint (the double-replace race)
// — and they are not the same failure to the owner, so the caller needs to
// tell them apart rather than collapsing both into one generic message.
func uniqueViolationConstraint(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return "", false
	}
	return pgErr.ConstraintName, true
}
