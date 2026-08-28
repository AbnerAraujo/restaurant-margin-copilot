package mcptools

import (
	"github.com/mark3labs/mcp-go/server"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// serverName/serverVersion identify this MCP server to whatever client
// connects to it (internal/explain's in-process client, or — later — an
// external MCP client, since mark3labs/mcp-go's transports are not
// specific to the in-process case).
const (
	serverName    = "restaurant-margin-copilot"
	serverVersion = "0.1.0"
)

// RegisterMCPServer builds the in-process MCP server (mark3labs/mcp-go)
// exposing this project's full fixed, typed tool set over q, per
// contracts/mcp-tools.md: get_daily_summary, get_margin_delta,
// list_discrepancies (this package's own reconciliation_tools.go, User
// Story 2/3), plus get_promotion_roi and list_negative_roi_promotions
// (promo_tools.go, User Story 4 — already implemented and exporting
// RegisterPromoTools specifically for this function to call, per that
// file's own doc comment).
//
// This function is exported and deliberately NOT called from
// cmd/server/main.go here (tasks.md T020) — a later Integration phase
// wires it in. It is what internal/explain's tool-calling loop imports and
// calls directly today: the server runs in-process, with no network
// transport, matching research.md's decision against Anthropic's URL-based
// native MCP connector for this local prototype (docs/technical-rfc.md,
// "Anthropic's native MCP connector" alternative).
func RegisterMCPServer(q *storage.Queries) *server.MCPServer {
	s := server.NewMCPServer(
		serverName,
		serverVersion,
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)

	// Applied to every AddTool'd handler via mcp-go's own dispatch path
	// (see limits.go's doc comment) — this is what actually enforces
	// Constitution Principle III's timeout and call-cap requirements, not
	// something individual tool handlers have to remember to do themselves.
	s.Use(timeoutAndBudgetMiddleware(DefaultToolTimeout))

	registerReconciliationTools(s, q)
	RegisterPromoTools(s, q)

	return s
}
