package bff

// The owner app's API surface, declared once, as data.
//
// On main this lived as seventeen mux.HandleFunc calls in cmd/server's
// main(), interleaved with the prose explaining each one. Nothing could
// enumerate it — not a test, not the CORS layer, not the startup log, not a
// human without reading 120 lines of wiring. cmd/server/main.go's own doc
// comment names the reason that mattered: "a package named main cannot be
// imported elsewhere". Moving the table here makes the surface importable,
// and therefore assertable (see routes_test.go).
//
// Adding route nineteen means adding one entry below. Its preflight, its
// 405 policy, and its startup log line all follow from that entry — which
// is the property specs/013-bff-layer exists to buy, because the last time
// they did NOT follow from one declaration, PUT /api/profile shipped broken
// from the browser and nobody could see it from curl.

import (
	"net/http"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/advisor"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/answercache"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/badges"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/httpapi"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/llmclient"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/platformconnector"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// Deps is everything the route table needs to build its handlers. Assembled
// by cmd/server (which owns the flags, the pool, and the fail-fast checks)
// and handed here, so this package composes without knowing how any of it
// was obtained.
type Deps struct {
	// Store is the concrete *storage.Queries several write paths require
	// (the cost-sheet commit and the connector sync both run
	// internal/pipeline, which needs the concrete type, not the Querier
	// interface).
	Store *storage.Queries

	// Cache is the answer cache. Both ingestion-shaped write paths
	// invalidate it for the same reason the -ingest flag does: new source
	// data can change any previously cached answer.
	Cache *answercache.Cache

	// Connectors is the simulated iFood / Just Eat Takeaway / POS proxy,
	// constructed once by the caller so all connector routes share one
	// registration of the three upstreams.
	Connectors *platformconnector.Proxy

	// Ask carries the wiring for POST /api/ask — the gate, the explainer,
	// the instrumentation logger, the caches, and the resolved data date
	// range. Built by cmd/server because it needs a live Postgres read to
	// resolve that range, and must fail fast at startup if it cannot.
	Ask httpapi.Deps

	// LLM is the one shared Anthropic client for every model call this
	// process makes, so the ask pipeline and the advisor share one timeout
	// policy and one credential source.
	LLM *llmclient.Client
}

// Routes returns the complete API surface.
//
// The method sets below are read off each handler's OWN method guard, not
// chosen here — a route that declared a method its handler rejects would
// advertise a preflight the handler then refuses, which is the same
// mismatch this table exists to prevent, only inverted.
func Routes(deps Deps) []Route {
	get := func(h http.HandlerFunc) map[string]http.HandlerFunc {
		return map[string]http.HandlerFunc{http.MethodGet: h}
	}
	post := func(h http.HandlerFunc) map[string]http.HandlerFunc {
		return map[string]http.HandlerFunc{http.MethodPost: h}
	}

	return []Route{
		{
			Pattern:  "/api/badges",
			Handlers: get(badges.RegisterBadgeHandler(deps.Store)),
			Summary:  "Points balance and badge state. Deterministic; no model.",
		},
		{
			Pattern:  "/api/reconciliation",
			Handlers: get(httpapi.HandleReconciliation(deps.Store)),
			Summary:  "Per-day reconciled margin for the Close and Home pages.",
		},
		{
			// Two methods, one resource. On main this needed methodSplit,
			// which routed POST to the create handler and EVERYTHING ELSE
			// to the listing handler — so a DELETE was answered by whichever
			// handler happened to be the fallback. Declaring the map makes
			// that helper unnecessary and the DELETE answer uniform.
			Pattern: "/api/promotions",
			Handlers: map[string]http.HandlerFunc{
				http.MethodGet:  httpapi.HandlePromotions(deps.Store),
				http.MethodPost: httpapi.HandleCreatePromotion(deps.Store),
			},
			Summary: "Promotion ROI: list, and owner-created campaigns.",
		},
		{
			Pattern:  "/api/platforms",
			Handlers: get(httpapi.HandlePlatformComparison(deps.Store)),
			Summary:  "compare_platform_economics, same computation a chat answer reads.",
		},
		{
			Pattern:  "/api/platforms/trend",
			Handlers: get(httpapi.HandlePlatformsTrend(deps.Store)),
			Summary:  "Trailing-months effective-rate trend (spec 008 FR-007).",
		},
		{
			Pattern:  "/api/usage",
			Handlers: post(httpapi.HandleRecordUsage(deps.Store)),
			Summary:  "App-open ping backing Engagement badges (spec 002).",
		},
		{
			Pattern:  "/api/client-errors",
			Handlers: post(httpapi.HandleRecordClientError(deps.Store)),
			Summary:  "Frontend ErrorBoundary crash reports.",
		},
		{
			// The route this whole package exists because of: its PUT
			// shipped invisible to the browser for want of one method in a
			// hand-maintained header literal.
			Pattern: "/api/profile",
			Handlers: map[string]http.HandlerFunc{
				http.MethodGet: httpapi.HandleProfile(deps.Store),
				http.MethodPut: httpapi.HandleProfile(deps.Store),
			},
			Summary: "The restaurant's own company information and photo.",
		},
		{
			Pattern:  "/api/ingest/cost-sheet/preview",
			Handlers: post(httpapi.HandlePreviewCostSheet),
			Summary:  "Parse an uploaded cost sheet; persist nothing (spec 007).",
		},
		{
			Pattern:  "/api/ingest/cost-sheet/commit",
			Handlers: post(httpapi.HandleCommitCostSheet(deps.Store, deps.Cache)),
			Summary:  "Commit a cost sheet; re-reconciles and clears the answer cache.",
		},
		{
			Pattern:  "/api/ingest/cost-sheet/template",
			Handlers: get(httpapi.HandleCostSheetTemplate),
			Summary:  "Blank cost-sheet CSV template.",
		},
		{
			Pattern:  "/api/connectors/platforms",
			Handlers: get(httpapi.HandleConnectorPlatforms(deps.Connectors)),
			Summary:  "The three SIMULATED connector upstreams and their wire formats.",
		},
		{
			// Exact patterns, so this and /api/connectors/sync below do not
			// shadow each other under http.ServeMux. True on main too; the
			// table just makes the adjacency visible for the first time.
			Pattern:  "/api/connectors/sync/preview",
			Handlers: post(httpapi.HandleConnectorSyncPreview(deps.Connectors)),
			Summary:  "Fetch from the simulated upstreams and summarize; persist nothing.",
		},
		{
			Pattern:  "/api/connectors/sync",
			Handlers: post(httpapi.HandleConnectorSync(deps.Connectors, deps.Store, deps.Cache)),
			Summary:  "Fetch, deduplicate across sources, then commit (specs 010 + 012).",
		},
		{
			// specs/013-bff-layer FR-006: one vocabulary for every source
			// of ingested data, whether it arrives as an uploaded file or
			// from a (simulated) connector pull.
			Pattern:  "/api/sources",
			Handlers: get(HandleSources(deps.Connectors)),
			Summary:  "Every source this product ingests, in one vocabulary (spec 013).",
		},
		{
			Pattern:  "/api/ask",
			Handlers: post(httpapi.HandleAsk(deps.Ask)),
			Summary:  "The chat surface: ambiguity gate, typed MCP tools, narration.",
		},
		{
			Pattern: "/api/business-insight",
			Handlers: post(httpapi.HandleBusinessInsight(httpapi.BusinessInsightDeps{
				Adviser: advisor.New(deps.LLM),
				Store:   deps.Store,
			})),
			Summary: "On-demand advice (spec 009), ledgered separately from questions.",
		},
	}
}
