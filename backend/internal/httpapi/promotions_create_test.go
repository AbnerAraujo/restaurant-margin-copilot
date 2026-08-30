package httpapi

// Live-Postgres integration tests for POST /api/promotions and POST
// /api/usage, following this codebase's established pattern (see
// internal/storage/promotion_test.go): skipped when DATABASE_URL is unset,
// sentinel campaign_ids so cleanup can never touch real dataset rows.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/badges"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

func httpapiConnectOrSkip(t *testing.T) (*pgx.Conn, *storage.Queries) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping live-Postgres integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close(context.Background()) })
	return conn, storage.New(conn)
}

func doCreatePromotion(t *testing.T, q storage.Querier, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/promotions", bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	HandleCreatePromotion(q)(rec, req)
	return rec
}

// TestHandleCreatePromotion_RefusesAReplacesClaimAgainstANonFlaggedCampaign
// is FR-007 exercised through the real HTTP handler against a live database:
// a "replaces" reference naming a campaign that is NOT currently
// flagged_negative=true is refused with a typed error, and — critically —
// the promotion is never inserted at all, not even without the claim
// (spec Acceptance Scenario 3 distinguishes "refuse this specific claim"
// from "refuse the whole submission"; this handler's stricter behavior —
// refusing outright rather than silently dropping the claim — is a
// documented judgment call, see the accompanying report).
func TestHandleCreatePromotion_RefusesAReplacesClaimAgainstANonFlaggedCampaign(t *testing.T) {
	conn, q := httpapiConnectOrSkip(t)

	notFlaggedID := "TEST-HTTPAPI-SENTINEL-NOT-FLAGGED"
	newCampaignID := "TEST-HTTPAPI-SENTINEL-REFUSED-REPLACEMENT"
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "DELETE FROM promotion_roi_record WHERE campaign_id IN ($1, $2)", notFlaggedID, newCampaignID)
	})

	// A real, persisted, POSITIVE-roi (i.e. NOT flagged) campaign — the
	// live fact FR-007 must actually check, not a client-asserted one.
	roiPos := int64(1000)
	_, err := storage.SavePromotionRoiRecord(context.Background(), q, reconcile.PromotionRoiRecord{
		Platform:                          "TestPlatform",
		CampaignID:                        notFlaggedID,
		PeriodStart:                       time.Date(1999, 5, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:                         time.Date(1999, 5, 3, 0, 0, 0, 0, time.UTC),
		SpendCents:                        2000,
		AttributedIncrementalOrders:       intPtrHTTP(1),
		AttributedIncrementalRevenueCents: int64PtrHTTP(3000),
		ROICents:                          &roiPos,
		FlaggedNegative:                   false,
	})
	require.NoError(t, err)

	rec := doCreatePromotion(t, q, map[string]any{
		"platform":    "iFood",
		"campaign_id": newCampaignID,
		"period":      map[string]string{"start": "1999-06-01", "end": "1999-06-07"},
		"spend":       "50.00",
		"replaces":    notFlaggedID,
	})

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "replaces_not_flagged_negative", body["error"])

	var count int
	require.NoError(t, conn.QueryRow(context.Background(), "SELECT count(*) FROM promotion_roi_record WHERE campaign_id = $1", newCampaignID).Scan(&count))
	require.Equal(t, 0, count, "the refused submission must not have been persisted")
}

// TestHandleCreatePromotion_AcceptsAReplacesClaimAgainstARealFlaggedCampaign
// is FR-007/FR-008's success path, using the REAL persisted campaign
// JET-CAMP-LUNCHFIX (backend/cmd/gendata/opening/README.md: -$450.75 ROI, genuinely
// flagged_negative=true once -ingest-promo has run) rather than a sentinel
// flagged row, so this test also proves the handler works against the
// product's actual persisted data, not just a purpose-built sample.
func TestHandleCreatePromotion_AcceptsAReplacesClaimAgainstARealFlaggedCampaign(t *testing.T) {
	conn, q := httpapiConnectOrSkip(t)

	flagged, err := storage.IsCampaignFlaggedNegative(context.Background(), q, "JET-CAMP-LUNCHFIX")
	require.NoError(t, err)
	if !flagged {
		t.Skip("real campaign JET-CAMP-LUNCHFIX is not flagged negative in this database — has -ingest-promo been run?")
	}

	newCampaignID := "TEST-HTTPAPI-SENTINEL-ACCEPTED-REPLACEMENT"
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "DELETE FROM promotion_roi_record WHERE campaign_id = $1", newCampaignID)
	})

	rec := doCreatePromotion(t, q, map[string]any{
		"platform":    "Just Eat Takeaway",
		"campaign_id": newCampaignID,
		"period":      map[string]string{"start": "1999-06-01", "end": "1999-06-07"},
		"spend":       "75.00",
		"replaces":    "JET-CAMP-LUNCHFIX",
	})

	require.Equal(t, http.StatusCreated, rec.Code)

	var body CreatePromotionResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.True(t, body.EarnedCampaignBadge)
	require.Equal(t, "owner_created", body.Promotion.Origin)
	require.NotNil(t, body.Promotion.ReplacesCampaignID)
	require.Equal(t, "JET-CAMP-LUNCHFIX", *body.Promotion.ReplacesCampaignID)
}

// TestHandleCreatePromotion_NoReplacesClaimNeedsNoFlaggedCampaignAtAll is
// spec Acceptance Scenario 2: a promotion logged with no replacement claim
// is accepted unconditionally (FR-007's check never even runs) and earns no
// Campaign-Creation badge.
func TestHandleCreatePromotion_NoReplacesClaimNeedsNoFlaggedCampaignAtAll(t *testing.T) {
	conn, q := httpapiConnectOrSkip(t)

	newCampaignID := "TEST-HTTPAPI-SENTINEL-STANDALONE"
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "DELETE FROM promotion_roi_record WHERE campaign_id = $1", newCampaignID)
	})

	rec := doCreatePromotion(t, q, map[string]any{
		"platform":    "iFood",
		"campaign_id": newCampaignID,
		"period":      map[string]string{"start": "1999-07-01", "end": "1999-07-07"},
		"spend":       "40.00",
	})

	require.Equal(t, http.StatusCreated, rec.Code)

	var body CreatePromotionResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.False(t, body.EarnedCampaignBadge)
	require.Nil(t, body.Promotion.ReplacesCampaignID)
}

// TestHandleCreatePromotion_RejectsMalformedInput exercises the plain
// input-validation refusals (Constitution Principle II: refuse rather than
// guess) that need no database write to prove — malformed money, a missing
// required field, an end before its start.
func TestHandleCreatePromotion_RejectsMalformedInput(t *testing.T) {
	_, q := httpapiConnectOrSkip(t)

	tests := []struct {
		name string
		body map[string]any
	}{
		{
			name: "missing campaign_id",
			body: map[string]any{
				"platform": "iFood",
				"period":   map[string]string{"start": "1999-08-01", "end": "1999-08-07"},
				"spend":    "10.00",
			},
		},
		{
			name: "malformed spend is refused, never coerced to zero",
			body: map[string]any{
				"platform":    "iFood",
				"campaign_id": "TEST-HTTPAPI-SENTINEL-BAD-SPEND",
				"period":      map[string]string{"start": "1999-08-01", "end": "1999-08-07"},
				"spend":       "not-a-number",
			},
		},
		{
			name: "end before start is refused, never silently swapped",
			body: map[string]any{
				"platform":    "iFood",
				"campaign_id": "TEST-HTTPAPI-SENTINEL-BAD-PERIOD",
				"period":      map[string]string{"start": "1999-08-07", "end": "1999-08-01"},
				"spend":       "10.00",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doCreatePromotion(t, q, tt.body)
			require.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

// TestHandleRecordUsage_RespondsAndDoesNotDoubleCount is a thin handler-level
// check that POST /api/usage actually calls through to the real dedup
// mechanism storage/badge_expansion_test.go proves at the storage layer —
// this test only confirms the HTTP plumbing reports it correctly.
func TestHandleRecordUsage_RespondsAndDoesNotDoubleCount(t *testing.T) {
	_, q := httpapiConnectOrSkip(t)

	req1 := httptest.NewRequest(http.MethodPost, "/api/usage", nil)
	rec1 := httptest.NewRecorder()
	HandleRecordUsage(q)(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)

	req2 := httptest.NewRequest(http.MethodPost, "/api/usage", nil)
	rec2 := httptest.NewRecorder()
	HandleRecordUsage(q)(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)

	var body2 RecordUsageResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &body2))
	require.False(t, body2.Recorded, "the second call within the same test (same UTC day) must report it did not record a new day")
}

// TestHandleCreatePromotion_PayWithPointsRefusesWhenBalanceInsufficient
// proves the refuse-rather-than-guess discipline applies to points spend
// exactly like every other typed refusal in this codebase: an absurd spend
// no realistic earned balance could ever cover is rejected with a specific,
// named error (insufficient_points, naming both figures) — and, just as
// important, nothing is persisted for the refused attempt.
func TestHandleCreatePromotion_PayWithPointsRefusesWhenBalanceInsufficient(t *testing.T) {
	conn, q := httpapiConnectOrSkip(t)

	campaignID := "TEST-HTTPAPI-SENTINEL-POINTS-INSUFFICIENT"
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "DELETE FROM promotion_roi_record WHERE campaign_id = $1", campaignID)
	})

	rec := doCreatePromotion(t, q, map[string]any{
		"platform":       "iFood",
		"campaign_id":    campaignID,
		"period":         map[string]string{"start": "1999-06-01", "end": "1999-06-07"},
		"spend":          "999999999.00",
		"payment_method": "points",
	})

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "insufficient_points", body["error"])

	var count int
	require.NoError(t, conn.QueryRow(context.Background(), "SELECT count(*) FROM promotion_roi_record WHERE campaign_id = $1", campaignID).Scan(&count))
	require.Equal(t, 0, count, "a refused points redemption must not have been persisted")
}

// TestHandleCreatePromotion_PayWithPointsSucceedsAndDeductsBalance proves the
// success path end to end against the real, live earned balance: a spend
// small enough that 1 point covers it (badges.CentsPerPoint = 10) succeeds
// against any real balance above zero, persists payment_method/points_spent
// on the row, and reports a real points_balance_after in the response
// rather than requiring a second GET /api/badges round trip.
func TestHandleCreatePromotion_PayWithPointsSucceedsAndDeductsBalance(t *testing.T) {
	conn, q := httpapiConnectOrSkip(t)

	earned, err := storage.LoadDailyReconciliationsInPeriod(context.Background(), q,
		time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2999, 12, 31, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	if len(earned) == 0 {
		t.Skip("no reconciled days in this database — no earned points balance to redeem against")
	}

	campaignID := "TEST-HTTPAPI-SENTINEL-POINTS-SUCCESS"
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "DELETE FROM promotion_roi_record WHERE campaign_id = $1", campaignID)
	})

	rec := doCreatePromotion(t, q, map[string]any{
		"platform":       "iFood",
		"campaign_id":    campaignID,
		"period":         map[string]string{"start": "1999-06-01", "end": "1999-06-07"},
		"spend":          "0.10",
		"payment_method": "points",
	})

	if rec.Code == http.StatusUnprocessableEntity {
		t.Skip("real earned balance in this database is currently 0 points — nothing to redeem")
	}
	require.Equal(t, http.StatusCreated, rec.Code)

	var body CreatePromotionResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "points", body.Promotion.PaymentMethod)
	require.NotNil(t, body.Promotion.PointsSpent)
	require.Equal(t, 1, *body.Promotion.PointsSpent)
	require.NotNil(t, body.PointsBalanceAfter)
	require.Equal(t, "0.10", body.Promotion.Spend, "spend stays the real dollar amount regardless of how it was funded")

	var paymentMethod string
	var pointsSpent int
	require.NoError(t, conn.QueryRow(context.Background(),
		"SELECT payment_method, points_spent FROM promotion_roi_record WHERE campaign_id = $1", campaignID,
	).Scan(&paymentMethod, &pointsSpent))
	require.Equal(t, "points", paymentMethod)
	require.Equal(t, 1, pointsSpent)
}

// TestHandleCreatePromotion_RejectsAnUnknownPaymentMethod is the same typed-
// refusal discipline every other malformed field on this endpoint already
// gets — a payment_method that is neither "money" nor "points" is a 400,
// never silently coerced to a default.
func TestHandleCreatePromotion_RejectsAnUnknownPaymentMethod(t *testing.T) {
	_, q := httpapiConnectOrSkip(t)

	rec := doCreatePromotion(t, q, map[string]any{
		"platform":       "iFood",
		"campaign_id":    "TEST-HTTPAPI-SENTINEL-BAD-PAYMENT-METHOD",
		"period":         map[string]string{"start": "1999-06-01", "end": "1999-06-07"},
		"spend":          "10.00",
		"payment_method": "bitcoin",
	})

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestHandleCreatePromotion_RejectsAnUnrecognizedPlatform is the fix for the
// exact data-integrity bug a QA pass found: this endpoint's platform field
// used to be unconstrained free text, and an owner (or a client bug) typing
// "Ifood" instead of "iFood" silently created a second, distinct platform
// value — duplicate "IFOOD ROI" stat cards, a filter dropdown that dropped
// half the platform's campaigns, under-reported spend. The wrong casing
// itself is the value under test here: a platform that is anything other
// than exactly one of mcptools.KnownPlatformDisplayNames() must now be
// refused with a typed 400, never silently accepted and persisted.
func TestHandleCreatePromotion_RejectsAnUnrecognizedPlatform(t *testing.T) {
	conn, q := httpapiConnectOrSkip(t)

	campaignID := "TEST-HTTPAPI-SENTINEL-BAD-PLATFORM-CASING"
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "DELETE FROM promotion_roi_record WHERE campaign_id = $1", campaignID)
	})

	rec := doCreatePromotion(t, q, map[string]any{
		"platform":    "Ifood",
		"campaign_id": campaignID,
		"period":      map[string]string{"start": "1999-06-01", "end": "1999-06-07"},
		"spend":       "10.00",
	})

	require.Equal(t, http.StatusBadRequest, rec.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "invalid_input", body["error"])

	var count int
	require.NoError(t, conn.QueryRow(context.Background(), "SELECT count(*) FROM promotion_roi_record WHERE campaign_id = $1", campaignID).Scan(&count))
	require.Equal(t, 0, count, "a refused platform value must never be persisted")
}

// TestHandleCreatePromotion_RejectsASecondReplacementOfAnAlreadyReplacedCampaign
// is the fix for the real double-award currency bug a QA pass found: after
// one promotion record was logged replacing a flagged campaign, that SAME
// flagged campaign stayed offered in the frontend's "replaces" dropdown (a
// stale client-side derivation — Promotions/PromotionsPage.tsx's own fix),
// and submitting it again minted a SECOND Campaign Launcher badge (and a
// second real points award, at 10 cents/point) for one real replacement.
// This is the server-side half of the fix: POST /api/promotions must refuse
// a "replaces" claim naming a campaign_id some OTHER already-persisted
// record already names, regardless of what the client's own UI currently
// shows — the same "re-verify against live data" discipline FR-007's
// flagged_negative re-check already applies a few lines above this new
// check in the handler.
func TestHandleCreatePromotion_RejectsASecondReplacementOfAnAlreadyReplacedCampaign(t *testing.T) {
	conn, q := httpapiConnectOrSkip(t)

	flaggedID := "TEST-HTTPAPI-SENTINEL-DOUBLE-AWARD-FLAGGED"
	firstReplacementID := "TEST-HTTPAPI-SENTINEL-DOUBLE-AWARD-FIRST"
	secondReplacementID := "TEST-HTTPAPI-SENTINEL-DOUBLE-AWARD-SECOND"
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(),
			"DELETE FROM promotion_roi_record WHERE campaign_id IN ($1, $2, $3)",
			flaggedID, firstReplacementID, secondReplacementID)
	})

	// A real, persisted, NEGATIVE-roi (i.e. genuinely flagged_negative=true)
	// sentinel campaign — a purpose-built fixture rather than depending on
	// JET-CAMP-LUNCHFIX's real state, since other tests/runs may have
	// already replaced that specific campaign.
	roiNeg := int64(-5000)
	_, err := storage.SavePromotionRoiRecord(context.Background(), q, reconcile.PromotionRoiRecord{
		Platform:                          "TestPlatform",
		CampaignID:                        flaggedID,
		PeriodStart:                       time.Date(1999, 5, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:                         time.Date(1999, 5, 3, 0, 0, 0, 0, time.UTC),
		SpendCents:                        6000,
		AttributedIncrementalOrders:       intPtrHTTP(1),
		AttributedIncrementalRevenueCents: int64PtrHTTP(1000),
		ROICents:                          &roiNeg,
		FlaggedNegative:                   true,
	})
	require.NoError(t, err)

	// First replacement: succeeds and earns the badge, exactly as
	// TestHandleCreatePromotion_AcceptsAReplacesClaimAgainstARealFlaggedCampaign
	// already proves for the success path in isolation.
	first := doCreatePromotion(t, q, map[string]any{
		"platform":    "iFood",
		"campaign_id": firstReplacementID,
		"period":      map[string]string{"start": "1999-06-01", "end": "1999-06-07"},
		"spend":       "50.00",
		"replaces":    flaggedID,
	})
	require.Equal(t, http.StatusCreated, first.Code)
	var firstBody CreatePromotionResponse
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &firstBody))
	require.True(t, firstBody.EarnedCampaignBadge)

	// Second replacement of the SAME already-replaced campaign: this is the
	// exact repro from the QA finding (the dropdown offering it again) —
	// must be refused with a typed, specific error, and must NOT be
	// persisted at all.
	second := doCreatePromotion(t, q, map[string]any{
		"platform":    "iFood",
		"campaign_id": secondReplacementID,
		"period":      map[string]string{"start": "1999-07-01", "end": "1999-07-07"},
		"spend":       "50.00",
		"replaces":    flaggedID,
	})
	require.Equal(t, http.StatusConflict, second.Code)

	var secondBody map[string]string
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &secondBody))
	require.Equal(t, "already_replaced", secondBody["error"])

	var count int
	require.NoError(t, conn.QueryRow(context.Background(),
		"SELECT count(*) FROM promotion_roi_record WHERE campaign_id = $1", secondReplacementID,
	).Scan(&count))
	require.Equal(t, 0, count, "the refused second replacement must not have been persisted")

	// The actual currency guarantee: even setting the write-time refusal
	// aside, the badge/points evaluator itself must never award a second
	// Campaign Launcher badge for one real replacement — re-load every
	// persisted record for these sentinels and confirm exactly one badge
	// exists naming flaggedID as replaced.
	all, err := storage.LoadAllPromotionRoiRecords(context.Background(), q)
	require.NoError(t, err)
	sentinelOnly := make([]reconcile.PromotionRoiRecord, 0, 2)
	for _, p := range all {
		if p.CampaignID == flaggedID || p.CampaignID == firstReplacementID || p.CampaignID == secondReplacementID {
			sentinelOnly = append(sentinelOnly, p)
		}
	}
	campaignBadges := badges.EvaluateCampaignCreationBadges(sentinelOnly)
	require.Len(t, campaignBadges, 1, "exactly one Campaign Launcher badge for one real replacement, never two")
	require.Equal(t, flaggedID, campaignBadges[0].ReplacesCampaignID)
	require.Equal(t, firstReplacementID, campaignBadges[0].CampaignID, "the EARLIEST replacement wins the badge")
}

// TestHandleCreatePromotion_ConcurrentReplacementsOfTheSameCampaignRaceToExactlyOneWinner
// is the genuinely concurrent counterpart to
// TestHandleCreatePromotion_RejectsASecondReplacementOfAnAlreadyReplacedCampaign.
// That test proves the sequential case (IsCampaignAlreadyReplaced's
// check-then-insert correctly refuses a SECOND, later request). It cannot
// prove the concurrent case: two requests racing to replace the same flagged
// campaign can both read "not yet replaced" from IsCampaignAlreadyReplaced
// before either has committed its insert — a real, live-reproducible TOCTOU
// gap the pre-insert application check alone cannot close, since it is two
// separate round trips with nothing serializing them. Only the database sees
// both transactions.
//
// migrations/000012_replaces_campaign_unique.up.sql's partial unique index
// closes it: with N requests fired truly concurrently, exactly one must
// succeed and every other must come back as the SAME typed 409
// "already_replaced" a sequential double-submit already gets — never a raw
// 500 from an unmapped constraint violation, and never two successes.
func TestHandleCreatePromotion_ConcurrentReplacementsOfTheSameCampaignRaceToExactlyOneWinner(t *testing.T) {
	conn, _ := httpapiConnectOrSkip(t)

	// httpapiConnectOrSkip's single *pgx.Conn (shared by every OTHER test in
	// this file, all of which issue one request at a time) is not safe for
	// concurrent use — pgx.Conn, unlike pgxpool.Pool, serializes all
	// traffic onto one physical connection and returns "conn busy" under
	// real concurrency. Production never hits this: cmd/server/main.go
	// hands every request its own connection from a pgxpool.Pool. This test
	// needs that same concurrency model to race real, simultaneous
	// requests the way the bug it's proving actually occurs, so it opens
	// its own pool against the same database rather than reusing conn/q.
	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	pooledQ := storage.New(pool)

	flaggedID := "TEST-HTTPAPI-SENTINEL-CONCURRENT-REPLACE-FLAGGED"
	const contenders = 25
	replacementIDs := make([]string, contenders)
	for i := range replacementIDs {
		replacementIDs[i] = fmt.Sprintf("TEST-HTTPAPI-SENTINEL-CONCURRENT-REPLACE-%d", i)
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(),
			"DELETE FROM promotion_roi_record WHERE campaign_id = ANY($1)",
			append([]string{flaggedID}, replacementIDs...))
	})

	roiNeg := int64(-5000)
	_, err = storage.SavePromotionRoiRecord(context.Background(), pooledQ, reconcile.PromotionRoiRecord{
		Platform:                          "TestPlatform",
		CampaignID:                        flaggedID,
		PeriodStart:                       time.Date(1999, 8, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:                         time.Date(1999, 8, 3, 0, 0, 0, 0, time.UTC),
		SpendCents:                        6000,
		AttributedIncrementalOrders:       intPtrHTTP(1),
		AttributedIncrementalRevenueCents: int64PtrHTTP(1000),
		ROICents:                          &roiNeg,
		FlaggedNegative:                   true,
	})
	require.NoError(t, err)

	var wg sync.WaitGroup
	results := make([]*httptest.ResponseRecorder, contenders)
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = doCreatePromotion(t, pooledQ, map[string]any{
				"platform":    "iFood",
				"campaign_id": replacementIDs[i],
				"period":      map[string]string{"start": fmt.Sprintf("1999-09-%02d", i+1), "end": fmt.Sprintf("1999-09-%02d", i+1)},
				"spend":       "50.00",
				"replaces":    flaggedID,
			})
		}(i)
	}
	wg.Wait()

	created, conflicted := 0, 0
	for _, rec := range results {
		switch rec.Code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflicted++
			var body map[string]string
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			require.Equal(t, "already_replaced", body["error"],
				"a losing request must come back as the same typed 409 a sequential double-submit gets, not an unmapped constraint error: %s", rec.Body.String())
		default:
			t.Fatalf("unexpected status %d for a concurrent replace attempt: %s", rec.Code, rec.Body.String())
		}
	}
	require.Equal(t, 1, created, "exactly one concurrent replacement of the same flagged campaign may succeed")
	require.Equal(t, contenders-1, conflicted, "every other contender must be refused, not silently dropped or double-awarded")

	var count int
	require.NoError(t, conn.QueryRow(context.Background(),
		"SELECT count(*) FROM promotion_roi_record WHERE replaces_campaign_id = $1", flaggedID,
	).Scan(&count))
	require.Equal(t, 1, count, "the database must hold exactly one row claiming this replacement, regardless of how many requests raced for it")
}

func intPtrHTTP(v int) *int       { return &v }
func int64PtrHTTP(v int64) *int64 { return &v }
