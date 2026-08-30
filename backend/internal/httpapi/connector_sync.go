package httpapi

// specs/010-platform-connector-proxy: pulling delivery-platform revenue
// from iFood and Just Eat Takeaway directly, instead of waiting for
// somebody to export a CSV from each merchant portal.
//
// Both upstreams are SIMULATED. This project has no partner-API
// credentials for either platform, so internal/platformconnector stands in
// two mock APIs with deliberately different wire formats and normalizes
// them into the same ingest.DeliveryRecord the CSV path already produces.
// Every response body in this file carries a top-level "simulated": true,
// so a client that ignores every UI affordance still cannot render these
// numbers without having been told what they are.
//
// Zero model involvement. The fetch is seeded pseudorandom Go, the
// normalization is Go, and the margin recomputation is
// internal/reconcile — unchanged, and unaware that anything about this
// request differs from a cost-sheet upload.
//
// Three endpoints, mirroring ingest_cost_sheet.go's preview/commit shape
// because it is the same job (stage something, look at it, then let it
// change the numbers):
//   - GET  /api/connectors/platforms     — what is connected, and how it is shaped
//   - POST /api/connectors/sync/preview  — fetch and summarize, persist nothing
//   - POST /api/connectors/sync          — fetch again from scratch, then commit

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/answercache"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/livedata"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/money"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/pipeline"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/platformconnector"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// syncSimulationNotice is the one sentence every response in this file
// repeats. It is duplicated into the payload rather than left to the UI
// because the UI is not the only consumer: a curl, a screenshot of a JSON
// body, a future integration reading this API. Disclosure that lives only
// in the presentation layer is disclosure that can be cropped out.
const syncSimulationNotice = "Emulated connection. No real iFood or Just Eat Takeaway account is connected — these orders are generated locally for demonstration."

// ConnectorPlatformView is one connector, as GET
// /api/connectors/platforms renders it.
type ConnectorPlatformView struct {
	Platform          string `json:"platform"`
	Name              string `json:"name"`
	Simulated         bool   `json:"simulated"`
	WireFormat        string `json:"wire_format"`
	CommissionRatePct string `json:"commission_rate_pct"`
	Endpoint          string `json:"endpoint"`
}

// ConnectorPlatformsResponse is GET /api/connectors/platforms' body.
type ConnectorPlatformsResponse struct {
	Simulated bool                    `json:"simulated"`
	Notice    string                  `json:"notice"`
	Platforms []ConnectorPlatformView `json:"platforms"`
}

// ConnectorSyncRequest is the body both POST endpoints accept.
type ConnectorSyncRequest struct {
	From      string   `json:"from"`
	To        string   `json:"to"`
	Platforms []string `json:"platforms"`
}

// ConnectorDayTotalsView is one platform's reported activity for one day.
// Money is a decimal string via money.FormatCents, matching every other
// money field this API returns — never a float.
type ConnectorDayTotalsView struct {
	Platform     string `json:"platform"`
	PlatformName string `json:"platform_name"`
	Date         string `json:"date"`
	OrderCount   int    `json:"order_count"`
	RefundCount  int    `json:"refund_count"`
	GrossSales   string `json:"gross_sales"`
	Refunds      string `json:"refunds"`
	Commissions  string `json:"commissions"`
}

// ConnectorSyncPreviewResponse is POST /api/connectors/sync/preview's
// body. Nothing has been persisted when this is returned.
type ConnectorSyncPreviewResponse struct {
	Simulated   bool                     `json:"simulated"`
	Notice      string                   `json:"notice"`
	From        string                   `json:"from"`
	To          string                   `json:"to"`
	OrderCount  int                      `json:"order_count"`
	GrossSales  string                   `json:"gross_sales"`
	Refunds     string                   `json:"refunds"`
	Commissions string                   `json:"commissions"`
	Days        []ConnectorDayTotalsView `json:"days"`
}

// ConnectorSyncResponse is POST /api/connectors/sync's body. Before/After
// reuse ingest_cost_sheet.go's MarginSnapshotView so the two write paths
// report their effect in exactly the same shape.
type ConnectorSyncResponse struct {
	Simulated     bool               `json:"simulated"`
	Notice        string             `json:"notice"`
	From          string             `json:"from"`
	To            string             `json:"to"`
	DaysAffected  int                `json:"days_affected"`
	OrdersSynced  int                `json:"orders_synced"`
	RefundsSynced int                `json:"refunds_synced"`
	Before        MarginSnapshotView `json:"before"`
	After         MarginSnapshotView `json:"after"`
}

// HandleConnectorPlatforms implements GET /api/connectors/platforms.
// Static: no database, no fetch, no side effect.
func HandleConnectorPlatforms(proxy *platformconnector.Proxy) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}

		descriptions := proxy.Describe()
		views := make([]ConnectorPlatformView, 0, len(descriptions))
		for _, d := range descriptions {
			views = append(views, ConnectorPlatformView{
				Platform:          string(d.Platform),
				Name:              d.Name,
				Simulated:         d.Simulated,
				WireFormat:        d.WireFormat,
				CommissionRatePct: d.CommissionRatePct,
				Endpoint:          d.Endpoint,
			})
		}
		writeJSON(w, http.StatusOK, ConnectorPlatformsResponse{
			Simulated: true,
			Notice:    syncSimulationNotice,
			Platforms: views,
		})
	}
}

// HandleConnectorSyncPreview implements POST /api/connectors/sync/preview.
// It fetches through the proxy and summarizes what came back. It writes
// nothing, touches internal/livedata not at all, and runs no pipeline —
// the same read-only contract POST /api/ingest/cost-sheet/preview has.
func HandleConnectorSyncPreview(proxy *platformconnector.Proxy) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}

		result, ok := fetchForRequest(w, r, proxy)
		if !ok {
			return
		}

		days := make([]ConnectorDayTotalsView, 0, len(result.Totals))
		var orderCount int
		var grossCents, refundCents, commissionCents int64
		for _, t := range result.Totals {
			orderCount += t.OrderCount
			grossCents += t.GrossCents
			refundCents += t.RefundsCents
			commissionCents += t.CommissionCents
			days = append(days, ConnectorDayTotalsView{
				Platform:     string(t.Platform),
				PlatformName: t.PlatformName,
				Date:         t.Date.Format(dateLayout),
				OrderCount:   t.OrderCount,
				RefundCount:  t.RefundCount,
				GrossSales:   money.FormatCents(t.GrossCents),
				Refunds:      money.FormatCents(t.RefundsCents),
				Commissions:  money.FormatCents(t.CommissionCents),
			})
		}

		writeJSON(w, http.StatusOK, ConnectorSyncPreviewResponse{
			Simulated:   true,
			Notice:      syncSimulationNotice,
			From:        result.From.Format(dateLayout),
			To:          result.To.Format(dateLayout),
			OrderCount:  orderCount,
			GrossSales:  money.FormatCents(grossCents),
			Refunds:     money.FormatCents(refundCents),
			Commissions: money.FormatCents(commissionCents),
			Days:        days,
		})
	}
}

// HandleConnectorSync implements POST /api/connectors/sync.
//
// It re-fetches from scratch rather than reusing anything a preview
// returned — the same discipline HandleCommitCostSheet applies to an
// uploaded file (spec 007 FR-007): a client claiming "it looked fine a
// moment ago" buys nothing. Because the upstreams are deterministic per
// (platform, date), the re-fetch is guaranteed to produce exactly what the
// preview showed, which is what makes "confirm what you previewed" a true
// statement here rather than a hopeful one.
//
// The write section takes ingestMu — see its doc comment in
// ingest_cost_sheet.go. This handler and the cost-sheet commit both write
// the same live dataset and both re-run the same pipeline against it.
func HandleConnectorSync(proxy *platformconnector.Proxy, store *storage.Queries, cache *answercache.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}

		result, ok := fetchForRequest(w, r, proxy)
		if !ok {
			return
		}

		ingestMu.Lock()
		defer ingestMu.Unlock()

		ctx := r.Context()

		before, err := loadMarginSnapshot(ctx, store)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "query_failed", fmt.Sprintf("reading pre-sync margin totals: %v", err))
			return
		}

		if err := livedata.EnsureReady(); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "live_data_not_ready", err.Error())
			return
		}

		// Cleared BEFORE the pipeline runs, not after — main.go's own
		// rationale for the -ingest flag: new source data can invalidate
		// any cached answer, and a pipeline that fails partway through
		// has still already changed some days.
		if cache != nil {
			if err := cache.Clear(ctx); err != nil {
				writeJSONError(w, http.StatusInternalServerError, "cache_clear_failed", err.Error())
				return
			}
		}

		overlay := &pipeline.DeliveryOverlay{
			From:    result.From,
			To:      result.To,
			Records: result.Records,
		}
		if err := pipeline.RunIngestionPipelineWithDeliveryOverlay(livedata.Dir, store, overlay); err != nil {
			// The fetch itself already succeeded and passed the
			// connector contract check, so a failure here is an
			// operational failure of the pipeline run, not a rejection of
			// the request — a 500, not a 422, exactly as the cost-sheet
			// commit treats the same distinction.
			writeJSONError(w, http.StatusInternalServerError, "pipeline_failed", err.Error())
			return
		}

		after, err := loadMarginSnapshot(ctx, store)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "query_failed", fmt.Sprintf("reading post-sync margin totals: %v", err))
			return
		}

		var refunds int
		for _, rec := range result.Records {
			if rec.Status == "refunded" {
				refunds++
			}
		}

		writeJSON(w, http.StatusOK, ConnectorSyncResponse{
			Simulated:     true,
			Notice:        syncSimulationNotice,
			From:          result.From.Format(dateLayout),
			To:            result.To.Format(dateLayout),
			DaysAffected:  int(result.To.Sub(result.From).Hours()/24) + 1,
			OrdersSynced:  len(result.Records),
			RefundsSynced: refunds,
			Before:        toMarginSnapshotView(before),
			After:         toMarginSnapshotView(after),
		})
	}
}

// fetchForRequest decodes and validates the shared request body, then
// fetches through the proxy. It writes the error response itself and
// reports ok=false, so both handlers share one set of refusal messages
// rather than drifting apart.
func fetchForRequest(w http.ResponseWriter, r *http.Request, proxy *platformconnector.Proxy) (*platformconnector.FetchResult, bool) {
	var req ConnectorSyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("body must be JSON with \"from\", \"to\" and \"platforms\": %v", err))
		return nil, false
	}

	from, err := time.Parse(dateLayout, req.From)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("\"from\" must be a YYYY-MM-DD date, got %q", req.From))
		return nil, false
	}
	to, err := time.Parse(dateLayout, req.To)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("\"to\" must be a YYYY-MM-DD date, got %q", req.To))
		return nil, false
	}

	// An omitted platform list means every connected platform, NOT an
	// empty fetch. The proxy refuses an empty list explicitly, so the
	// only way to get "no platforms" is to ask for it.
	keys := req.Platforms
	if len(keys) == 0 {
		for _, p := range proxy.Platforms() {
			keys = append(keys, string(p))
		}
	}
	platforms := make([]platformconnector.Platform, 0, len(keys))
	for _, key := range keys {
		p, err := platformconnector.ParsePlatform(key)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return nil, false
		}
		platforms = append(platforms, p)
	}

	result, err := proxy.FetchRange(r.Context(), from, to, platforms)
	if err != nil {
		// Every error the proxy produces is a refusal with a specific,
		// actionable message (an inverted range, an over-cap range, an
		// upstream that failed, a record that violated the connector
		// contract). Surfacing it verbatim is the same treatment
		// ingest.ParseCostSheet's errors already get.
		writeJSONError(w, http.StatusUnprocessableEntity, "connector_fetch_failed", err.Error())
		return nil, false
	}
	return result, true
}
