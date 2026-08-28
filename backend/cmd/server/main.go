// Command server is the entry point for the Daily Margin & Growth Copilot
// backend. Per tasks.md T017, it is currently a thin CLI wrapper around
// internal/pipeline's ingest -> reconcile -> persist flow, runnable via the
// -ingest flag (quickstart.md's "Validate User Story 1" step). The real
// pipeline logic lives in internal/pipeline, not here, specifically so
// later phases (the MCP tool layer, the evaluation harness) can import and
// call it directly — a package named main cannot be imported elsewhere.
//
// The HTTP API and in-process MCP tool server (tasks.md T020, T023) are not
// wired yet — User Story 1's deterministic core is deliberately proven
// first, with no LLM call anywhere in this path (Constitution Principle V).
package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/jackc/pgx/v5"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/pipeline"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

func main() {
	ingestDir := flag.String("ingest", "", "run the ingest -> reconcile -> persist pipeline against the delivery/POS/cost-sheet exports in this directory, then exit (see quickstart.md)")
	flag.Parse()

	if *ingestDir == "" {
		log.Println("restaurant-margin-copilot backend: no -ingest flag given; nothing to do.")
		log.Println("Usage: go run ./backend/cmd/server -ingest <fixture-directory>")
		log.Println("The HTTP API and MCP server are not wired yet — see specs/001-margin-reconciliation-qa/tasks.md T018+.")
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

	if err := pipeline.RunIngestionPipeline(*ingestDir, store); err != nil {
		log.Fatalf("ingestion pipeline failed: %v", err)
	}
}
