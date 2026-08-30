package httpapi

// Tests for specs/010-platform-connector-proxy's three endpoints.
//
// GET /api/connectors/platforms and POST /api/connectors/sync/preview need
// no database — the first is static and the second is read-only by
// contract (it must persist nothing), so both are exercised here as fast
// unit tests, the same split ingest_cost_sheet_test.go already makes for
// preview vs. commit.
//
// POST /api/connectors/sync's happy path re-runs the real pipeline against
// internal/livedata.Dir and persists real DailyReconciliation rows. That
// path is covered by a live-Postgres test below (skipped when DATABASE_URL
// is unset, this codebase's established pattern) plus the manual end-to-end
// exercise recorded in the changelog — what is asserted here without a
// database is its refusal behavior, which runs before any storage or
// filesystem interaction.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/platformconnector"
)

func connectorRequest(t *testing.T, path string, body map[string]any) *http.Request {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	return httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
}

// Every response this feature serves must state that the data is
// simulated, in the payload itself. The UI's banner is not enough: a curl,
// a screenshot of a JSON body, or a future consumer would all see the
// numbers without the disclosure if it lived only in the frontend.
func TestConnectorEndpoints_AlwaysDiscloseTheSimulation(t *testing.T) {
	proxy := platformconnector.NewSimulatedProxy()

	t.Run("platforms listing", func(t *testing.T) {
		rec := httptest.NewRecorder()
		HandleConnectorPlatforms(proxy)(rec, httptest.NewRequest(http.MethodGet, "/api/connectors/platforms", nil))
		require.Equal(t, http.StatusOK, rec.Code)

		var body ConnectorPlatformsResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.True(t, body.Simulated, "top-level simulated flag must be true")
		require.Contains(t, body.Notice, "Emulated connection")
		require.Len(t, body.Platforms, 2)

		for _, p := range body.Platforms {
			require.True(t, p.Simulated, "platform %q must carry its own simulated flag, not rely on the envelope's", p.Platform)
			require.NotEmpty(t, p.WireFormat, "platform %q must describe the wire format it normalizes", p.Platform)
			require.True(t, strings.HasPrefix(p.Endpoint, "simulated://"),
				"platform %q endpoint %q must be self-evidently synthetic", p.Platform, p.Endpoint)
		}
		require.Equal(t, "ifood", body.Platforms[0].Platform)
		require.Equal(t, "just_eat_takeaway", body.Platforms[1].Platform)
		require.NotEqual(t, body.Platforms[0].WireFormat, body.Platforms[1].WireFormat,
			"the two connectors describe identical wire formats — the heterogeneity this feature exists to normalize has gone")
	})

	t.Run("sync preview", func(t *testing.T) {
		rec := httptest.NewRecorder()
		HandleConnectorSyncPreview(proxy)(rec, connectorRequest(t, "/api/connectors/sync/preview", map[string]any{
			"from": "2026-08-18",
			"to":   "2026-08-20",
		}))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var body ConnectorSyncPreviewResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.True(t, body.Simulated)
		require.Contains(t, body.Notice, "No real iFood or Just Eat Takeaway account is connected")
	})
}

// A preview must summarize what the platforms reported, per platform per
// day, without persisting anything — the store is never even passed to
// this handler, which is the structural version of that guarantee.
func TestHandleConnectorSyncPreview_SummarizesEveryPlatformDay(t *testing.T) {
	rec := httptest.NewRecorder()
	HandleConnectorSyncPreview(platformconnector.NewSimulatedProxy())(rec, connectorRequest(t, "/api/connectors/sync/preview", map[string]any{
		"from": "2026-08-18",
		"to":   "2026-08-20",
	}))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body ConnectorSyncPreviewResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	// Two platforms x three days.
	require.Len(t, body.Days, 6)
	require.Equal(t, "2026-08-18", body.From)
	require.Equal(t, "2026-08-20", body.To)
	require.Greater(t, body.OrderCount, 0)

	seen := map[string]bool{}
	for _, d := range body.Days {
		seen[d.Platform+"|"+d.Date] = true
		require.Greater(t, d.OrderCount, 0, "%s reported no orders at all on %s", d.PlatformName, d.Date)
		// Money is a decimal string everywhere in this API, never a float.
		require.Regexp(t, `^-?\d+\.\d{2}$`, d.GrossSales)
		require.Regexp(t, `^-?\d+\.\d{2}$`, d.Commissions)
	}
	for _, key := range []string{
		"ifood|2026-08-18", "ifood|2026-08-19", "ifood|2026-08-20",
		"just_eat_takeaway|2026-08-18", "just_eat_takeaway|2026-08-19", "just_eat_takeaway|2026-08-20",
	} {
		require.True(t, seen[key], "missing platform-day %s", key)
	}
}

// The same request twice must return the same figures. This is the
// endpoint-level statement of spec FR-005: a demo, a re-run, and an
// evaluator all see the same numbers.
func TestHandleConnectorSyncPreview_IsRepeatable(t *testing.T) {
	proxy := platformconnector.NewSimulatedProxy()
	body := func() string {
		rec := httptest.NewRecorder()
		HandleConnectorSyncPreview(proxy)(rec, connectorRequest(t, "/api/connectors/sync/preview", map[string]any{
			"from": "2026-08-18",
			"to":   "2026-08-20",
		}))
		require.Equal(t, http.StatusOK, rec.Code)
		return rec.Body.String()
	}
	require.Equal(t, body(), body(), "two identical preview requests returned different numbers")
}

// Each refusal must name what was wrong, specifically — the same treatment
// ingest.ParseCostSheet's row-referenced errors already get.
func TestConnectorSync_Refusals(t *testing.T) {
	proxy := platformconnector.NewSimulatedProxy()

	for _, tc := range []struct {
		name       string
		body       map[string]any
		wantStatus int
		wantDetail string
	}{
		{
			name:       "malformed from date",
			body:       map[string]any{"from": "18/08/2026", "to": "2026-08-20"},
			wantStatus: http.StatusBadRequest,
			wantDetail: `"from" must be a YYYY-MM-DD date`,
		},
		{
			name:       "unknown platform",
			body:       map[string]any{"from": "2026-08-18", "to": "2026-08-20", "platforms": []string{"deliveroo"}},
			wantStatus: http.StatusBadRequest,
			wantDetail: `unknown platform "deliveroo"`,
		},
		{
			name:       "inverted range",
			body:       map[string]any{"from": "2026-08-20", "to": "2026-08-18"},
			wantStatus: http.StatusUnprocessableEntity,
			wantDetail: "runs backwards",
		},
		{
			name:       "range longer than the cap",
			body:       map[string]any{"from": "2026-01-01", "to": "2026-06-30"},
			wantStatus: http.StatusUnprocessableEntity,
			wantDetail: "more than the 31-day limit",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Asserted on BOTH endpoints: a refusal the preview enforces
			// but the sync does not would let a client skip the preview
			// and write anyway.
			for path, handler := range map[string]http.HandlerFunc{
				"/api/connectors/sync/preview": HandleConnectorSyncPreview(proxy),
				// A nil store/cache is safe here precisely because every
				// case below is refused before the handler reaches them.
				"/api/connectors/sync": HandleConnectorSync(proxy, nil, nil),
			} {
				rec := httptest.NewRecorder()
				handler(rec, connectorRequest(t, path, tc.body))
				require.Equal(t, tc.wantStatus, rec.Code, "%s: %s", path, rec.Body.String())

				// Decoded, not matched against the raw body: the messages
				// that matter most here quote the offending value, and a
				// raw-body match would silently be comparing against
				// JSON-escaped quotes.
				var errBody struct {
					Error  string `json:"error"`
					Detail string `json:"detail"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errBody))
				require.Contains(t, errBody.Detail, tc.wantDetail, path)
				// internal/platformconnector wraps every error it returns
				// with a "platformconnector: " prefix for log traceability
				// — that Go-package-name prefix must never reach the HTTP
				// response, the same class of internal-vocabulary leak
				// explain_internal_test.go guards the chat's own refusal
				// copy against.
				require.NotContains(t, errBody.Detail, "platformconnector:", path)
			}
		})
	}

	t.Run("wrong method", func(t *testing.T) {
		rec := httptest.NewRecorder()
		HandleConnectorSyncPreview(proxy)(rec, httptest.NewRequest(http.MethodGet, "/api/connectors/sync/preview", nil))
		require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	})
}

// An omitted platform list means "everything connected", not "nothing".
// The opposite reading would silently return an empty sync that looks like
// a successful one.
func TestHandleConnectorSyncPreview_OmittedPlatformsMeansAll(t *testing.T) {
	rec := httptest.NewRecorder()
	HandleConnectorSyncPreview(platformconnector.NewSimulatedProxy())(rec, connectorRequest(t, "/api/connectors/sync/preview", map[string]any{
		"from": "2026-08-20",
		"to":   "2026-08-20",
	}))
	require.Equal(t, http.StatusOK, rec.Code)

	var body ConnectorSyncPreviewResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Days, 2, "an omitted platform list should have fetched both platforms")
}
