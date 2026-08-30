package httpapi

// Live-Postgres integration test for GET /api/platforms, following this
// package's established pattern (promotions_create_test.go's
// httpapiConnectOrSkip): skipped when DATABASE_URL is unset. This test makes
// no writes — it reads the real, already-persisted fixture data the same
// read-only way internal/mcptools' own
// TestComparePlatformEconomics_MatchesFixtureReferenceValues does — so it
// costs nothing and can never collide with real fixture rows.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/mcptools"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// TestHandlePlatformComparison_DefaultsToRealDataRange asserts the
// "no query params" default resolves to whatever this Postgres instance's
// real, currently-persisted data range actually is — computed live via
// storage.LoadDataDateRange rather than a hardcoded literal, since this
// dev database's real range legitimately changes as datasets are
// regenerated (it originally held only the 14-day fixture; it now also
// holds the full 2024-08-01..today dataset from
// backend/cmd/gendata). A hardcoded expectation here would silently
// re-encode "whatever the range happened to be on the day this test was
// written" as if it were a permanent fact about the product.
func TestHandlePlatformComparison_DefaultsToRealDataRange(t *testing.T) {
	_, q := httpapiConnectOrSkip(t)

	wantStart, wantEnd, err := storage.LoadDataDateRange(context.Background(), q)
	require.NoError(t, err, "the live database must have at least one persisted day for this default-range test to be meaningful")

	req := httptest.NewRequest(http.MethodGet, "/api/platforms", nil)
	rec := httptest.NewRecorder()
	HandlePlatformComparison(q)(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var result mcptools.PlatformComparisonResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.Equal(t, wantStart.Format("2006-01-02"), result.Period.Start, "must default to the real persisted data's own range, not a hardcoded or wide-open sentinel")
	require.Equal(t, wantEnd.Format("2006-01-02"), result.Period.End)
	require.Len(t, result.Platforms, 2)
}

// TestHandlePlatformComparison_MatchesOpeningReferenceValuesForKnownPeriod
// verifies the same independently-computed opening-window figures
// internal/mcptools.TestComparePlatformEconomics_MatchesOpeningReferenceValues
// already proves at the tool layer, but through the HTTP handler — scoped
// to an EXPLICIT period rather than relying on "no params" to coincidentally
// equal the opening window, since this Postgres instance holds the full
// multi-year dataset.
func TestHandlePlatformComparison_MatchesOpeningReferenceValuesForKnownPeriod(t *testing.T) {
	_, q := httpapiConnectOrSkip(t)

	req := httptest.NewRequest(http.MethodGet, "/api/platforms?start=2024-08-01&end=2024-08-14", nil)
	rec := httptest.NewRecorder()
	HandlePlatformComparison(q)(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var result mcptools.PlatformComparisonResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.Equal(t, "2024-08-01", result.Period.Start)
	require.Equal(t, "2024-08-14", result.Period.End)
	require.Len(t, result.Platforms, 2)

	for _, p := range result.Platforms {
		switch p.Source {
		case "ifood":
			require.Equal(t, "2556.25", p.GrossSales)
			require.NotNil(t, p.EffectiveRate)
			require.Equal(t, "22.44%", *p.EffectiveRate)
		case "just_eat_takeaway":
			require.Equal(t, "2403.50", p.GrossSales)
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
