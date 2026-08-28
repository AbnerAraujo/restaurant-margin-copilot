// Command server is the entry point for the Daily Margin & Growth Copilot
// backend. Per tasks.md T017, it is a thin CLI wrapper around
// internal/pipeline's ingest -> reconcile -> persist flows, runnable via the
// -ingest and -ingest-promo flags (quickstart.md's "Validate User Story 1"
// and "User Story 4" steps), plus a -serve flag that starts the HTTP server:
// the deterministic GET /api/badges endpoint (T032) and, per the
// Integration phase, POST /api/ask (T020/T023) — the chat surface wiring
// internal/ambiguity's gate, internal/explain's tool-calling loop over
// internal/mcptools' typed MCP server, and internal/instrumentation
// together. The real pipeline and handler logic all live in their own
// internal/ packages, not here, specifically so the evaluation harness can
// import and call them directly — a package named main cannot be imported
// elsewhere.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ambiguity"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/answercache"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/badges"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/explain"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/httpapi"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/instrumentation"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/llmclient"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/mcptools"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/paraphrase"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/pipeline"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

func main() {
	ingestDir := flag.String("ingest", "", "run the ingest -> reconcile -> persist pipeline against the delivery/POS/cost-sheet exports in this directory, then exit (see quickstart.md)")
	ingestPromoDir := flag.String("ingest-promo", "", "run User Story 4's promotion ingest -> reconcile -> persist pipeline against the promotion/ad-spend + delivery-platform exports in this directory, then exit (tasks.md T029-T030)")
	serveAddr := flag.String("serve", "", "if set, start an HTTP server on this address (e.g. :8080) exposing GET /api/badges (T032) and POST /api/ask (T020/T023), then block until interrupted")
	flag.Parse()

	if *ingestDir == "" && *ingestPromoDir == "" && *serveAddr == "" {
		log.Println("restaurant-margin-copilot backend: no -ingest, -ingest-promo, or -serve flag given; nothing to do.")
		log.Println("Usage: go run ./backend/cmd/server -ingest <fixture-directory>")
		log.Println("       go run ./backend/cmd/server -ingest-promo <fixture-directory>")
		log.Println("       go run ./backend/cmd/server -serve <addr>")
		return
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL must be set (see specs/001-margin-reconciliation-qa/quickstart.md)")
	}

	ctx := context.Background()
	// A POOL, not a single pgx.Conn. A lone connection is not safe for
	// concurrent use, and -serve now exposes four endpoints that a single
	// page load fires in parallel (GET /api/badges alongside
	// GET /api/reconciliation, for instance) — which surfaced as a real
	// "conn busy: failed to deallocate cached statement(s)" 500 on the Close
	// and Promotions pages the moment they went live against Postgres.
	// pgxpool hands each in-flight request its own connection and satisfies
	// storage.DBTX unchanged.
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatalf("connecting to Postgres: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("pinging Postgres: %v", err)
	}

	store := storage.New(pool)
	cache := answercache.New(store)

	// Cache invalidation, at the START of any ingestion run rather than the
	// end: new source data can change any cached answer, and if the pipeline
	// fails partway through, having already dropped the cache costs a few
	// re-asked questions, whereas clearing only on success could leave
	// answers cached against data that has already been partly rewritten.
	// Correctness after new data wins over cache retention, every time.
	if *ingestDir != "" || *ingestPromoDir != "" {
		if err := cache.Clear(ctx); err != nil {
			log.Fatalf("clearing the answer cache before ingestion: %v", err)
		}
		log.Println("answer cache cleared — new data invalidates every previously cached answer")
	}

	if *ingestDir != "" {
		if err := pipeline.RunIngestionPipeline(*ingestDir, store); err != nil {
			log.Fatalf("ingestion pipeline failed: %v", err)
		}
	}

	if *ingestPromoDir != "" {
		if err := pipeline.RunPromotionIngestionPipeline(*ingestPromoDir, store); err != nil {
			log.Fatalf("promotion ingestion pipeline failed: %v", err)
		}
	}

	if *serveAddr != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/badges", badges.RegisterBadgeHandler(store))
		// Plain read-only data endpoints backing the Close and Promotions
		// pages. No model is involved in either request path — they read the
		// same persisted deterministic output the MCP tools read, through the
		// same rendering (see internal/httpapi/data.go).
		mux.HandleFunc("/api/reconciliation", httpapi.HandleReconciliation(store))
		mux.HandleFunc("/api/promotions", methodSplit(httpapi.HandlePromotions(store), httpapi.HandleCreatePromotion(store)))
		// GET /api/platforms: specs/003-platform-comparator's dedicated
		// Platforms page, reading the exact same compare_platform_economics
		// computation a chat answer about the same period would (see
		// httpapi.HandlePlatformComparison's doc comment).
		mux.HandleFunc("/api/platforms", httpapi.HandlePlatformComparison(store))
		// POST /api/usage: the real app-open ping backing Engagement badges
		// (spec 002-badge-expansion). No model involved, same as every other
		// endpoint registered directly here rather than through
		// internal/mcptools.
		mux.HandleFunc("/api/usage", httpapi.HandleRecordUsage(store))
		// POST /api/client-errors: the frontend ErrorBoundary's "retro feed"
		// — a real crash report, logged so the next one leaves a queryable
		// trace instead of depending on someone noticing and describing it.
		mux.HandleFunc("/api/client-errors", httpapi.HandleRecordClientError(store))
		// specs/007-cost-sheet-upload: letting the owner upload/replace the
		// supplier cost sheet through the web UI instead of requiring a
		// developer to run -ingest on their behalf. Preview and template need
		// no dependencies (pure parsing / a static file); commit needs the
		// concrete *storage.Queries RunIngestionPipeline requires plus the
		// same answer cache the -ingest flag above invalidates, for the same
		// reason (new cost data can change any previously-cached answer).
		mux.HandleFunc("/api/ingest/cost-sheet/preview", httpapi.HandlePreviewCostSheet)
		mux.HandleFunc("/api/ingest/cost-sheet/commit", httpapi.HandleCommitCostSheet(store, cache))
		mux.HandleFunc("/api/ingest/cost-sheet/template", httpapi.HandleCostSheetTemplate)

		askDeps, err := buildAskDeps(ctx, store, cache)
		if err != nil {
			log.Fatalf("wiring POST /api/ask: %v", err)
		}
		mux.HandleFunc("/api/ask", httpapi.HandleAsk(askDeps))

		log.Printf("serving GET /api/badges, GET /api/reconciliation, GET/POST /api/promotions, GET /api/platforms, POST /api/usage, POST /api/client-errors, POST /api/ingest/cost-sheet/{preview,commit}, GET /api/ingest/cost-sheet/template, and POST /api/ask on %s — Ctrl+C to stop", *serveAddr)
		if err := http.ListenAndServe(*serveAddr, withDevCORS(mux)); err != nil {
			log.Fatalf("http server failed: %v", err)
		}
	}
}

// withDevCORS allows the frontend (a different origin/port from this API) to
// call it directly from the browser. This is a local-prototype convenience,
// not a production CORS policy — it reflects back any localhost origin
// rather than "*", but a real deployment would derive this from
// configuration instead of a hard-coded allowlist rule.
//
// A single hard-coded port (originally just the Vite dev server's 5173) is
// exactly wrong once the frontend can run from more than one: the installed
// PWA build (`vite preview`, port 4173) failed every fetch with a real CORS
// error in this browser's console the moment it was installed, because the
// server only ever answered with the one port baked in. Reflecting any
// http(s)://localhost:<port> origin fixes that without opening this up to
// non-local origins the way "*" would.
func withDevCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); isLocalhostOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isLocalhostOrigin reports whether origin is an http(s) origin on
// localhost or 127.0.0.1, at any port — deliberately not a bare substring
// check (which "http://localhost:5173.evil.example" would slip past).
func isLocalhostOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	return u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"
}

// buildAskDeps wires httpapi.HandleAsk's dependencies: internal/llmclient's
// shared Anthropic client (Claude Haiku 4.5 for the gate, Claude Sonnet 5
// for explain — both model choices live inside internal/ambiguity and
// internal/explain themselves, not here), internal/mcptools' in-process MCP
// server (the fixed, typed tool set backing every explain call), and
// internal/instrumentation's Logger backed by the storage.InstrumentationAdapter
// (storage/instrumentation.go) — the concrete adapter internal/instrumentation's
// own package doc promises.
//
// This requires ANTHROPIC_API_KEY to be resolvable from the environment
// (internal/llmclient.New's documented default); -serve fails fast here
// rather than at the first POST /api/ask if that's missing, matching how
// DATABASE_URL is already checked fast above.
func buildAskDeps(ctx context.Context, store *storage.Queries, cache *answercache.Cache) (httpapi.Deps, error) {
	llm := llmclient.New()

	// Resolve the real data date range ONCE, from Postgres, rather than
	// hardcoding a literal in internal/ambiguity or internal/explain — the
	// fix for the "date-year grounding defect" (docs/plan.md mistakes log):
	// relative date language ("today", "this week", a year-less date) must
	// ground against what the data actually covers, not the host machine's
	// wall-clock date. This requires the ingestion pipeline (-ingest) to
	// have already run at least once; a -serve with no data yet fails fast
	// here with a clear error rather than starting with a gate/explain that
	// has no sensible "today" to reason from.
	dataStart, dataEnd, err := storage.LoadDataDateRange(ctx, store)
	if err != nil {
		return httpapi.Deps{}, fmt.Errorf("resolving data date range (has -ingest been run yet?): %w", err)
	}
	dataStartStr := dataStart.Format(dateLayout)
	dataEndStr := dataEnd.Format(dateLayout)
	log.Printf("resolved data date range for date-grounding: %s..%s", dataStartStr, dataEndStr)

	mcpServer := mcptools.RegisterMCPServer(store)

	explainer, err := explain.New(ctx, llm, mcpServer, dataStartStr, dataEndStr)
	if err != nil {
		return httpapi.Deps{}, err
	}

	return httpapi.Deps{
		Gate:      ambiguity.New(llm, dataStartStr, dataEndStr),
		Explainer: explainer,
		Logger:    instrumentation.NewLogger(storage.NewInstrumentationAdapter(store)),
		Cache:     cache,
		// specs/004-semantic-cache: checked only on an exact-match cache
		// MISS (see httpapi.HandleAsk) — shares the same Haiku model and
		// llmclient.Client the ambiguity gate uses, one more vendor-internal
		// use of a call this project already pays for and rate-limits.
		ParaphraseMatcher: paraphrase.New(llm),
		// Same real data date range the gate/explainer above were just
		// grounded against, threaded through so a successful answer's
		// follow-up suggestions (httpapi/suggestions.go) never point at a
		// date this product has no reconciled data for.
		DataStart: dataStartStr,
		DataEnd:   dataEndStr,
	}, nil
}

// dateLayout matches internal/mcptools' own YYYY-MM-DD convention for every
// date string this product hands to the model layer.
const dateLayout = "2006-01-02"

// methodSplit lets one route (/api/promotions) dispatch to two different
// handlers by HTTP method — GET for the existing read-only listing, POST
// for spec 002's new owner-created-promotion write path — rather than
// registering a second URL for what is conceptually the same resource.
// Each handler already refuses its own wrong-method case (405), so an
// unrecognised method here just falls through to whichever handler was
// picked and lets its own check report it.
func methodSplit(get, post http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			post(w, r)
			return
		}
		get(w, r)
	}
}
