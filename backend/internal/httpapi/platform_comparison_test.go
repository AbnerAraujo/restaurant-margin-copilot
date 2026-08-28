package httpapi

// Live-Postgres integration test for GET /api/platforms, following this
// package's established pattern (promotions_create_test.go's
// httpapiConnectOrSkip): skipped when DATABASE_URL is unset. This test makes
// no writes — it reads the real, already-persisted fixture data the same
// read-only way internal/mcptools' own
// TestComparePlatformEconomics_MatchesFixtureReferenceValues does — so it
// costs nothing and can never collide with real fixture rows.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/mcptools"
)

func TestHandlePlatformComparison_DefaultsToRealDataRangeAndMatchesFixtures(t *testing.T) {
	_, q := httpapiConnectOrSkip(t)

	req := httptest.NewRequest(http.MethodGet, "/api/platforms", nil)
	rec := httptest.NewRecorder()
	HandlePlatformComparison(q)(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var result mcptools.PlatformComparisonResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.Equal(t, "2026-08-01", result.Period.Start, "must default to the real persisted data's own range, not the wide-open sentinel")
	require.Equal(t, "2026-08-14", result.Period.End)
	require.Len(t, result.Platforms, 2)

	for _, p := range result.Platforms {
		switch p.Source {
		case "ifood":
			require.Equal(t, "838.00", p.GrossSales)
			require.NotNil(t, p.EffectiveRate)
			require.Equal(t, "22.06%", *p.EffectiveRate)
		case "just_eat_takeaway":
			require.Equal(t, "908.00", p.GrossSales)
			require.NotNil(t, p.EffectiveRate)
			require.Equal(t, "20.00%", *p.EffectiveRate)
		default:
			t.Errorf("unexpected platform source %q", p.Source)
		}
	}
}

func TestHandlePlatformComparison_ExplicitPeriodIsHonoured(t *testing.T) {
	_, q := httpapiConnectOrSkip(t)

	req := httptest.NewRequest(http.MethodGet, "/api/platforms?start=2026-08-01&end=2026-08-07", nil)
	rec := httptest.NewRecorder()
	HandlePlatformComparison(q)(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var result mcptools.PlatformComparisonResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.Equal(t, "2026-08-01", result.Period.Start)
	require.Equal(t, "2026-08-07", result.Period.End)
	require.Equal(t, 7, result.DaysIncluded)
}

func TestHandlePlatformComparison_RejectsWrongMethod(t *testing.T) {
	_, q := httpapiConnectOrSkip(t)

	req := httptest.NewRequest(http.MethodPost, "/api/platforms", nil)
	rec := httptest.NewRecorder()
	HandlePlatformComparison(q)(rec, req)

	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestHandlePlatformComparison_RejectsOneSidedPeriod(t *testing.T) {
	_, q := httpapiConnectOrSkip(t)

	req := httptest.NewRequest(http.MethodGet, "/api/platforms?start=2026-08-01", nil)
	rec := httptest.NewRecorder()
	HandlePlatformComparison(q)(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code, "a lone bound would otherwise silently leave the other wide open")
}
