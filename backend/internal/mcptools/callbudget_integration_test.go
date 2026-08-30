package mcptools_test

// QA round 6, fresh bug class 3 ("the chat's tool-call retry/timeout/loop-cap
// behavior... verify these are actually implemented and have real tests, not
// just documented as intentions"): CLAUDE.md's hard limit "Explicit cap on
// loop iterations" is enforced by mcptools.CallBudget + the unexported
// timeoutAndBudgetMiddleware (limits.go), wired into every registered tool
// via RegisterMCPServer's s.Use(...). That mechanism turned out to have
// ZERO test coverage anywhere in this codebase before this file: grepping
// the whole backend tree for NewCallBudget/WithCallBudget/
// ErrToolCallCapExceeded before this change found only limits.go's own
// definition and explain.go's single production call site — the
// budget-exceeded branch inside timeoutAndBudgetMiddleware was reachable
// only from real chat traffic, never from a test. limits_test.go tests that
// same middleware's timeout/cancellation branches directly but was never
// extended to the budget check sitting right above them in the same
// function. A regression here (e.g. an off-by-one in take()'s `>=`, or the
// budget silently failing to thread through the in-process MCP transport's
// context) would only surface in production as a runaway or over-billed
// chat interaction — exactly the failure mode CLAUDE.md's "Explicit cap on
// loop iterations" hard limit exists to prevent.
//
// This file exercises the cap over the REAL wire protocol this project
// actually uses in production — client.NewInProcessClient over
// mcptools.RegisterMCPServer, the same path internal/explain.New builds
// (see protocol_malformed_args_test.go's newTestMCPClient, reused here) —
// rather than only unit-testing the middleware function in isolation, so it
// also proves the budget installed via mcptools.WithCallBudget actually
// reaches the real dispatch path's middleware unchanged.

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/mcptools"
)

// resultText concatenates a CallToolResult's text content blocks — the same
// shape internal/explain.toolResultText extracts and hands back to the
// model as a tool_result, reproduced here since that helper is unexported
// to this external test package.
func resultText(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	require.NotNil(t, r)
	var sb strings.Builder
	for _, c := range r.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// callWithBudget issues one get_daily_summary call against c under ctx — a
// deliberately cheap, always-safe-to-call tool (an unseeded fakeQuerier just
// answers "no_data", never a panic) so the test can fire it many times and
// care only about the budget's own accounting, not this tool's business
// logic.
func callWithBudget(ctx context.Context, c interface {
	CallTool(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
}, date string) (*mcp.CallToolResult, error) {
	return c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "get_daily_summary",
			Arguments: map[string]any{"date": date},
		},
	})
}

// TestCallBudget_CapExceeded_IsGracefulOverRealWireProtocol proves the three
// things CLAUDE.md's "Explicit cap on loop iterations" hard limit actually
// requires, against the real server (not a hand-rolled middleware call):
// the Nth call within budget goes through untouched, the (N+1)th is refused
// with the typed ErrToolCallCapExceeded (never a panic, never a hang, never
// a fabricated success), and the refusal itself never consumes budget (so a
// model stuck in a loop past the cap cannot somehow "recover" capacity by
// keep asking).
func TestCallBudget_CapExceeded_IsGracefulOverRealWireProtocol(t *testing.T) {
	c := newTestMCPClient(t)

	const max = 3
	budget := mcptools.NewCallBudget(max)
	ctx := mcptools.WithCallBudget(context.Background(), budget)

	for i := 0; i < max; i++ {
		result, err := callWithBudget(ctx, c, "2026-01-01")
		require.NoError(t, err, "call %d must not fail at the transport level", i)
		require.NotContains(t, resultText(t, result), mcptools.ErrToolCallCapExceeded,
			"call %d is within budget and must reach the real tool, not the cap", i)
	}
	require.Equal(t, max, budget.Used())

	// One call past the cap: refused gracefully, typed, and distinguishable
	// from an ordinary business-logic error (no_data) a caller must not
	// confuse with "the interaction hit its loop cap".
	result, err := callWithBudget(ctx, c, "2026-01-01")
	require.NoError(t, err, "a capped call is a normal CallToolResult, never a transport error")
	require.NotNil(t, result)
	require.True(t, result.IsError, "a capped call must be reported as an error result")
	require.Contains(t, resultText(t, result), mcptools.ErrToolCallCapExceeded)
	require.Equal(t, max, budget.Used(), "a refused call must never itself consume budget")

	// Repeated hammering past the cap must stay stable — never panic, never
	// let Used() drift past Max(), matching how internal/explain's loop
	// keeps calling tools for up to MaxTurns-DefaultMaxToolCallsPerInteraction
	// more turns after the budget is already exhausted.
	for i := 0; i < 10; i++ {
		result, err := callWithBudget(ctx, c, "2026-01-01")
		require.NoError(t, err)
		require.True(t, result.IsError)
		require.Contains(t, resultText(t, result), mcptools.ErrToolCallCapExceeded)
	}
	require.Equal(t, max, budget.Used())
	require.Equal(t, max, budget.Max())
}

// TestCallBudget_SharedAcrossDifferentTools proves the cap is genuinely
// per-INTERACTION, not per-tool: internal/explain installs exactly one
// CallBudget per question (Explain's doc comment), so a model alternating
// between two different tools within one interaction must still be capped
// by their combined total, not get a fresh allowance per tool name.
func TestCallBudget_SharedAcrossDifferentTools(t *testing.T) {
	c := newTestMCPClient(t)

	budget := mcptools.NewCallBudget(2)
	ctx := mcptools.WithCallBudget(context.Background(), budget)

	result, err := callWithBudget(ctx, c, "2026-01-01")
	require.NoError(t, err)
	require.NotContains(t, resultText(t, result), mcptools.ErrToolCallCapExceeded)

	result, err = c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "list_discrepancies",
			Arguments: map[string]any{"date": "2026-01-01"},
		},
	})
	require.NoError(t, err)
	require.NotContains(t, resultText(t, result), mcptools.ErrToolCallCapExceeded)
	require.Equal(t, 2, budget.Used(), "two calls to two different tools must consume the same shared budget")

	result, err = callWithBudget(ctx, c, "2026-01-02")
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Contains(t, resultText(t, result), mcptools.ErrToolCallCapExceeded,
		"a third call, regardless of which tool, must be capped once the shared budget is spent")
}

// TestCallBudget_ConcurrentCallsNeverExceedMax is CallBudget's own doc
// comment ("Safe for concurrent use") held to account: this codebase's
// production loop only ever calls tools sequentially (Explain's doc
// comment says so explicitly), so this is deliberately a stronger guarantee
// than production needs — but CallBudget.Used()/take() share one mutex
// specifically to make that guarantee true, and nothing before this file
// ever ran it under -race to prove it. Fired with go test -race, this would
// catch a torn read/write on the counter; run normally, it still catches
// the accounting bug a non-atomic check-then-increment would produce (Used()
// ending up above Max()).
func TestCallBudget_ConcurrentCallsNeverExceedMax(t *testing.T) {
	c := newTestMCPClient(t)

	const max = 5
	const attempts = 30
	budget := mcptools.NewCallBudget(max)
	ctx := mcptools.WithCallBudget(context.Background(), budget)

	var wg sync.WaitGroup
	var mu sync.Mutex
	granted := 0
	capped := 0

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := callWithBudget(ctx, c, "2026-01-01")
			require.NoError(t, err)
			mu.Lock()
			defer mu.Unlock()
			if strings.Contains(resultText(t, result), mcptools.ErrToolCallCapExceeded) {
				capped++
			} else {
				granted++
			}
		}()
	}
	wg.Wait()

	require.Equal(t, max, granted, "exactly Max() concurrent calls must be let through")
	require.Equal(t, attempts-max, capped)
	require.Equal(t, max, budget.Used())
}
