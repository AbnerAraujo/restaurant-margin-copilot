package mcptools

// Finding 8: timeoutAndBudgetMiddleware must not report a canceled parent
// context (e.g. the browser closed, the HTTP request was aborted upstream)
// as if it were a genuine 5s deadline expiry — that would be a false claim
// about what happened, which this codebase's refuse-rather-than-guess
// principle forbids for an internal status report just as much as for a
// margin figure. White-box (package mcptools, not mcptools_test) because
// timeoutAndBudgetMiddleware is unexported and this is the only way to
// drive it directly without a live MCP server or Postgres.

import (
	"context"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

// TestTimeoutAndBudgetMiddleware_DeadlineExceeded_ReportsTimeout is the
// positive case: a tool call that genuinely outlives the configured bound
// must still be reported as ErrToolCallTimeout, with "timeout" in the
// reason text.
func TestTimeoutAndBudgetMiddleware_DeadlineExceeded_ReportsTimeout(t *testing.T) {
	mw := timeoutAndBudgetMiddleware(20 * time.Millisecond)
	next := func(cctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		<-cctx.Done() // block past the deadline, exactly like a slow real query would
		return nil, cctx.Err()
	}
	handler := mw(next)

	result, err := handler(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "slow_tool"}})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.IsError)

	toolErr, ok := result.StructuredContent.(ToolError)
	require.True(t, ok, "expected a mcptools.ToolError in StructuredContent")
	require.Equal(t, ErrToolCallTimeout, toolErr.Error)
	require.Contains(t, toolErr.Reason, "timeout")
}

// TestTimeoutAndBudgetMiddleware_ParentCanceled_DoesNotReportTimeout is
// Finding 8's dedicated regression test: a context canceled by its PARENT
// (never hitting the 5s bound at all — the timeout here is deliberately
// generous, 5s, so a flaky race with the deadline cannot make this test
// pass for the wrong reason) must be reported as ErrToolCallCanceled, never
// ErrToolCallTimeout, and the reason text must never claim a timeout
// occurred.
func TestTimeoutAndBudgetMiddleware_ParentCanceled_DoesNotReportTimeout(t *testing.T) {
	mw := timeoutAndBudgetMiddleware(5 * time.Second)
	parentCtx, cancel := context.WithCancel(context.Background())

	next := func(cctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Simulate the client disconnecting mid-call: the PARENT context is
		// canceled, not the 5s deadline being reached.
		cancel()
		<-cctx.Done()
		return nil, cctx.Err()
	}
	handler := mw(next)

	result, err := handler(parentCtx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "slow_tool"}})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.IsError)

	toolErr, ok := result.StructuredContent.(ToolError)
	require.True(t, ok, "expected a mcptools.ToolError in StructuredContent")
	require.Equal(t, ErrToolCallCanceled, toolErr.Error, "a canceled parent context must be reported distinctly from a timeout")
	require.NotEqual(t, ErrToolCallTimeout, toolErr.Error)
	require.NotContains(t, toolErr.Reason, "timeout", "a genuine cancellation must never be reported using timeout wording")
}
