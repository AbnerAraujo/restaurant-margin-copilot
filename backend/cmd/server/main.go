// Command server is the entry point for the Daily Margin & Growth Copilot
// backend. Per tasks.md T017, it is currently a thin CLI wrapper around
// internal/pipeline's ingest -> reconcile -> persist flows, runnable via the
// -ingest and -ingest-promo flags (quickstart.md's "Validate User Story 1"
// and "User Story 4" steps), plus a -serve flag exposing the one
// deterministic-only REST endpoint built so far (GET /api/badges, T032).
// The real pipeline logic lives in internal/pipeline, not here, specifically
// so later phases (the MCP tool layer, the evaluation harness) can import
// and call it directly — a package named main cannot be imported elsewhere.
//
// The chat/MCP endpoints (tasks.md T020, T023) are not wired yet — User
// Story 1's deterministic core is deliberately proven first, with no LLM
// call anywhere in this path (Constitution Principle V). -serve exists only
// for GET /api/badges, which is itself deterministic (internal/badges) and
// carries no model call either.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/badges"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/pipeline"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

func main() {
	ingestDir := flag.String("ingest", "", "run the ingest -> reconcile -> persist pipeline against the delivery/POS/cost-sheet exports in this directory, then exit (see quickstart.md)")
	ingestPromoDir := flag.String("ingest-promo", "", "run User Story 4's promotion ingest -> reconcile -> persist pipeline against the promotion/ad-spend + delivery-platform exports in this directory, then exit (tasks.md T029-T030)")
	serveAddr := flag.String("serve", "", "if set, start an HTTP server on this address (e.g. :8080) exposing GET /api/badges (tasks.md T032) and block until interrupted")
	flag.Parse()

	if *ingestDir == "" && *ingestPromoDir == "" && *serveAddr == "" {
		log.Println("restaurant-margin-copilot backend: no -ingest, -ingest-promo, or -serve flag given; nothing to do.")
		log.Println("Usage: go run ./backend/cmd/server -ingest <fixture-directory>")
		log.Println("       go run ./backend/cmd/server -ingest-promo <fixture-directory>")
		log.Println("       go run ./backend/cmd/server -serve <addr>")
		log.Println("The chat/MCP endpoints are not wired yet — see specs/001-margin-reconciliation-qa/tasks.md T018+.")
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
		log.Printf("serving GET /api/badges on %s (tasks.md T032) — Ctrl+C to stop", *serveAddr)
		if err := http.ListenAndServe(*serveAddr, mux); err != nil {
			log.Fatalf("http server failed: %v", err)
		}
	}
}
