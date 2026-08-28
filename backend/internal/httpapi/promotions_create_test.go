package httpapi

// Live-Postgres integration tests for POST /api/promotions and POST
// /api/usage, following this codebase's established pattern (see
// internal/storage/promotion_test.go): skipped when DATABASE_URL is unset,
// sentinel campaign_ids so cleanup can never touch real fixture data.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

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
		"platform":    "TestPlatform",
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
// is FR-007/FR-008's success path, using the REAL fixture campaign
// JET-CAMP-LUNCHFIX (backend/fixtures/README.md: -$165.00 ROI, genuinely
// flagged_negative=true once -ingest-promo has run) rather than a sentinel
// flagged row, so this test also proves the handler works against the
// product's actual persisted data, not just a purpose-built fixture.
func TestHandleCreatePromotion_AcceptsAReplacesClaimAgainstARealFlaggedCampaign(t *testing.T) {
	conn, q := httpapiConnectOrSkip(t)

	flagged, err := storage.IsCampaignFlaggedNegative(context.Background(), q, "JET-CAMP-LUNCHFIX")
	require.NoError(t, err)
	if !flagged {
		t.Skip("real fixture campaign JET-CAMP-LUNCHFIX is not flagged negative in this database — has -ingest-promo been run?")
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
		"platform":    "TestPlatform",
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
				"platform": "TestPlatform",
				"period":   map[string]string{"start": "1999-08-01", "end": "1999-08-07"},
				"spend":    "10.00",
			},
		},
		{
			name: "malformed spend is refused, never coerced to zero",
			body: map[string]any{
				"platform":    "TestPlatform",
				"campaign_id": "TEST-HTTPAPI-SENTINEL-BAD-SPEND",
				"period":      map[string]string{"start": "1999-08-01", "end": "1999-08-07"},
				"spend":       "not-a-number",
			},
		},
		{
			name: "end before start is refused, never silently swapped",
			body: map[string]any{
				"platform":    "TestPlatform",
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

func intPtrHTTP(v int) *int       { return &v }
func int64PtrHTTP(v int64) *int64 { return &v }
