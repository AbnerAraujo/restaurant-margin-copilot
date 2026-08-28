// Command server is the entry point for the Daily Margin & Growth Copilot
// backend: the HTTP API and the in-process MCP tool server, per plan.md's
// Project Structure.
//
// This is a Setup/Foundational-phase placeholder (tasks.md T001-T010). The
// real ingest -> reconcile -> persist pipeline, MCP tool registration, and
// /api/ask endpoint are wired in T017, T020, and T023 respectively, once
// User Story 1's deterministic core exists and is proven with tests
// (Constitution Principle V) — deliberately not started here.
package main

import (
	"log"
)

func main() {
	log.Println("restaurant-margin-copilot backend: setup/foundational skeleton only.")
	log.Println("See specs/001-margin-reconciliation-qa/tasks.md T011+ for the ingest/reconcile/MCP/API wiring.")
}
