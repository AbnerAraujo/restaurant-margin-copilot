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
	"os"

	"github.com/jackc/pgx/v5"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ambiguity"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/badges"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/explain"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/httpapi"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/instrumentation"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/llmclient"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/mcptools"
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
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		log.Fatalf("connecting to Postgres: %v", err)
	}
	defer func() {
		if err := conn.Close(ctx); err != nil {
			log.Printf("closing Postgres connection: %v", err)
		}
	}()

	store := storage.New(conn)

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

		askDeps, err := buildAskDeps(ctx, store)
		if err != nil {
			log.Fatalf("wiring POST /api/ask: %v", err)
		}
		mux.HandleFunc("/api/ask", httpapi.HandleAsk(askDeps))

		log.Printf("serving GET /api/badges (T032) and POST /api/ask (T020/T023) on %s — Ctrl+C to stop", *serveAddr)
		if err := http.ListenAndServe(*serveAddr, withDevCORS(mux)); err != nil {
			log.Fatalf("http server failed: %v", err)
		}
	}
}

// withDevCORS allows the Vite dev server (a different origin/port from this
// API) to call it directly from the browser. This is a local-prototype
// convenience, not a production CORS policy — it allows exactly the one
// known frontend dev origin rather than "*", but a real deployment would
// derive this from configuration instead of a hard-coded localhost port.
func withDevCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:5173")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
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
func buildAskDeps(ctx context.Context, store *storage.Queries) (httpapi.Deps, error) {
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
	}, nil
}

// dateLayout matches internal/mcptools' own YYYY-MM-DD convention for every
// date string this product hands to the model layer.
const dateLayout = "2006-01-02"
