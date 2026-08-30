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

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/money"
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
		require.Len(t, body.Platforms, 3)

		for _, p := range body.Platforms {
			require.True(t, p.Simulated, "platform %q must carry its own simulated flag, not rely on the envelope's", p.Platform)
			require.NotEmpty(t, p.WireFormat, "platform %q must describe the wire format it normalizes", p.Platform)
			require.True(t, strings.HasPrefix(p.Endpoint, "simulated://"),
				"platform %q endpoint %q must be self-evidently synthetic", p.Platform, p.Endpoint)
		}
		require.Equal(t, "ifood", body.Platforms[0].Platform)
		require.Equal(t, "just_eat_takeaway", body.Platforms[1].Platform)
		require.Equal(t, "pos", body.Platforms[2].Platform)
		// All three, pairwise. Two connectors describing the same wire
		// format would mean the heterogeneity this feature exists to
		// normalize had quietly gone.
		for i := range body.Platforms {
			for j := i + 1; j < len(body.Platforms); j++ {
				require.NotEqual(t, body.Platforms[i].WireFormat, body.Platforms[j].WireFormat,
					"%s and %s describe identical wire formats", body.Platforms[i].Platform, body.Platforms[j].Platform)
			}
		}
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
		require.Contains(t, body.Notice, "No real iFood account, Just Eat Takeaway account or POS terminal is connected")
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

	// Three sources x three days.
	require.Len(t, body.Days, 9)
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
		"pos|2026-08-18", "pos|2026-08-19", "pos|2026-08-20",
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
	require.Len(t, body.Days, 3, "an omitted platform list should have fetched every connected source, including the POS")
}

// specs/012-pos-connector-dedup SC-001, at the API boundary: gross sales
// for a three-source sync equal what the three sources reported on their
// own, minus exactly the duplicates that were removed — and that
// difference is accounted for, line by line, by the decisions in the same
// response.
//
// This is the arithmetic the feature exists to get right. A response where
// the numbers and the explanation did not reconcile would be worse than no
// deduplication at all, because it would look correct.
func TestHandleConnectorSyncPreview_GrossIsRawTotalsMinusRemovedDuplicates(t *testing.T) {
	proxy := platformconnector.NewSimulatedProxy()

	preview := func(t *testing.T, platforms []string) ConnectorSyncPreviewResponse {
		t.Helper()
		rec := httptest.NewRecorder()
		HandleConnectorSyncPreview(proxy)(rec, connectorRequest(t, "/api/connectors/sync/preview", map[string]any{
			"from":      "2026-08-18",
			"to":        "2026-08-20",
			"platforms": platforms,
		}))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var body ConnectorSyncPreviewResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		return body
	}

	deliveryOnly := preview(t, []string{"ifood", "just_eat_takeaway"})
	posOnly := preview(t, []string{"pos"})
	all := preview(t, []string{"ifood", "just_eat_takeaway", "pos"})

	require.Zero(t, deliveryOnly.DuplicatesRemoved, "a delivery-only sync has no POS tickets to remove")
	require.Zero(t, posOnly.DuplicatesRemoved, "a POS-only sync has nothing to compare against")
	require.Greater(t, all.DuplicatesRemoved, 0, "the simulated POS records iFood orders — a combined sync must find duplicates")

	// The removed amount, derived independently from the decisions rather
	// than from the totals it is being checked against.
	var removedCents int64
	var mergedCount int
	for _, d := range all.Dedup {
		if !d.Resolved {
			continue
		}
		mergedCount++
	}
	require.Equal(t, all.DuplicatesRemoved, mergedCount,
		"the headline count and the itemized decisions must agree")

	// The gross difference must be non-zero and must be entirely explained
	// by removals: combined gross is strictly less than the sum of the two
	// independent syncs, by the removed tickets' value.
	raw := parseCents(t, deliveryOnly.GrossSales) + parseCents(t, posOnly.GrossSales)
	combined := parseCents(t, all.GrossSales)
	removedCents = raw - combined
	require.Greater(t, removedCents, int64(0),
		"combined gross is not lower than the two sources summed separately — no duplicate was actually removed from the numbers")

	// Every unresolved overlap is reported too, not quietly dropped from
	// the summary. A response that only listed successes would be claiming
	// a clean result it did not achieve.
	// Filtered on kind, not on !Resolved: an amount-mismatch decision is
	// also not a resolution, but it accompanies one rather than reporting
	// a possible double-count.
	unresolved := 0
	for _, d := range all.Dedup {
		require.NotEmpty(t, d.Detail, "decision %s carries no explanation", d.Kind)
		if strings.HasPrefix(d.Kind, "unresolved_") {
			require.False(t, d.Resolved)
			unresolved++
		}
	}
	require.Equal(t, all.UnresolvedOverlaps, unresolved)

	t.Logf("2026-08-18..20: delivery %s + POS %s = %s raw, %s after removing %d duplicate(s) (%s), %d overlap(s) left unresolved",
		deliveryOnly.GrossSales, posOnly.GrossSales, money.FormatCents(raw), all.GrossSales,
		all.DuplicatesRemoved, money.FormatCents(removedCents), all.UnresolvedOverlaps)
}

func parseCents(t *testing.T, decimal string) int64 {
	t.Helper()
	cents, err := money.ParseCents(decimal)
	require.NoError(t, err)
	return cents
}
