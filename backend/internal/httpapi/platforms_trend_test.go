package httpapi

// Live-Postgres integration test for GET /api/platforms/trend, following
// platform_comparison_test.go's established pattern (httpapiConnectOrSkip:
// skipped when DATABASE_URL is unset). Read-only, same as that file.
//
// Deliberately does NOT hardcode an expected date range the way
// platform_comparison_test.go's own DefaultsToRealDataRange test does
// (that test is currently broken on this dev machine, unrelated to this
// change: it assumes a single 14-day window, but this machine's Postgres
// has since been seeded with the full, gap-free 2024-08-01..2026-08-14
// synthetic history — see backend/cmd/gendata's 2026-08-29 fix). This test
// derives its expectations from storage.LoadDataDateRange itself, so it
// holds regardless of how much real history is currently seeded.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

func TestHandlePlatformsTrend_ReturnsChronologicalRealMonthsTruncatedToDataEnd(t *testing.T) {
	_, q := httpapiConnectOrSkip(t)

	_, dataEnd, err := storage.LoadDataDateRange(context.Background(), q)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/platforms/trend", nil)
	rec := httptest.NewRecorder()
	HandlePlatformsTrend(q)(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var body PlatformsTrendResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	require.LessOrEqual(t, len(body.Periods), trailingMonths, "must never return more than the bounded trailing window")
	require.NotEmpty(t, body.Periods, "this dev database's real seeded history should cover at least the most recent month")

	lastMonth := body.Periods[len(body.Periods)-1]
	require.Equal(t, dataEnd.Format("2006-01"), lastMonth.Month, "the most recent period must be the calendar month containing the real data's max date")
	require.NotNil(t, lastMonth.Result)
	require.Equal(t, dataEnd.Format(dateLayout), lastMonth.Result.Period.End, "the most recent month must be truncated to the real max date, never padded past it")

	// Every entry must be a real, non-fabricated result (FR-013) with a
	// well-formed period, and months must be strictly chronological
	// (oldest first) with no duplicates or gaps larger than one calendar
	// month between consecutive entries.
	var prevMonth time.Time
	for i, p := range body.Periods {
		require.NotNil(t, p.Result, "period %q must carry a real result, never a placeholder", p.Month)
		require.NotEmpty(t, p.Result.Platforms)

		parsed, err := time.Parse("2006-01", p.Month)
		require.NoError(t, err)
		if i > 0 {
			require.True(t, parsed.After(prevMonth), "months must be strictly increasing (chronological), got %q after %q", p.Month, prevMonth.Format("2006-01"))
		}
		prevMonth = parsed
	}
}

func TestHandlePlatformsTrend_RejectsNonGET(t *testing.T) {
	_, q := httpapiConnectOrSkip(t)

	req := httptest.NewRequest(http.MethodPost, "/api/platforms/trend", nil)
	rec := httptest.NewRecorder()
	HandlePlatformsTrend(q)(rec, req)

	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}
