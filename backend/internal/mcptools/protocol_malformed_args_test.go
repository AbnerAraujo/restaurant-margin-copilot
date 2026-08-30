package mcptools_test

// This QA pass exercises the MCP tool-calling contract directly — the SAME
// path internal/explain.New actually uses (client.NewInProcessClient over
// mcptools.RegisterMCPServer, per that package's own doc comment) — rather
// than only through internal/mcptools' Go-level core-function tests
// (reconciliation_tools_test.go et al.) or through the chat UI end-to-end.
// The goal per the QA brief: for each of this package's 8 tools, feed
// malformed arguments (wrong JSON types, missing required fields,
// out-of-range/garbage dates, negative numbers, wrong-shaped objects) over
// the REAL wire protocol and confirm the server always answers with a
// graceful CallToolResult (IsError: true, a JSON ToolError payload) or a
// clean transport-level error — never a panic that would take the whole
// in-process server down for every other in-flight request sharing it
// (server.WithRecovery() in RegisterMCPServer suggests this was already a
// known risk class; this test proves the fixed set of 8 tools actually
// holds that line under real malformed input, not just well-typed Go
// arguments).
//
// Every subtest calls a raw map[string]any as CallToolRequest.Params.
// Arguments — never mcptools' own typed args structs — specifically to
// simulate a hostile or buggy MCP client sending JSON that doesn't match
// the expected shape at all (arrays where an object is expected, numbers
// where a string is expected, etc.), which a purely Go-level unit test
// calling GetDailySummary(ctx, q, "not-a-date") directly could never
// exercise: that call is Go-type-safe by construction, so it can never
// prove what happens when the JSON on the wire itself is the wrong shape.

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/mcptools"
)

// newTestMCPClient builds the exact client/server pair internal/explain.New
// builds (in-process transport, initialized, tools listed), over a fresh
// fakeQuerier — no live Postgres required.
func newTestMCPClient(t *testing.T) *client.Client {
	t.Helper()
	q := newFakeQuerier()
	srv := mcptools.RegisterMCPServer(q)

	c, err := client.NewInProcessClient(srv)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	ctx := context.Background()
	_, err = c.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo:      mcp.Implementation{Name: "qa-malformed-args-test", Version: "0.1.0"},
		},
	})
	require.NoError(t, err)
	return c
}

// callRaw issues a CallTool with exactly the arguments given, bypassing
// every one of mcptools' own typed Go structs — the point of this file.
func callRaw(t *testing.T, c *client.Client, tool string, args map[string]any) (*mcp.CallToolResult, error) {
	t.Helper()
	return c.CallTool(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      tool,
			Arguments: args,
		},
	})
}

// requireGraceful asserts the call did not panic (implicit: a panic inside
// a tool handler would either surface as a transport error here, thanks to
// server.WithRecovery(), or fail the test process outright) and, when it
// returned normally, that the result is a well-formed error result rather
// than a 200-shaped success carrying garbage data.
func requireGraceful(t *testing.T, result *mcp.CallToolResult, err error) {
	t.Helper()
	if err != nil {
		// A transport-level error (e.g. the recovered-panic path, or a
		// genuine argument-binding failure surfaced as an error rather than
		// a result) is still "graceful" in the sense this test cares about:
		// the server did not crash and did not fabricate a plausible-looking
		// success payload from garbage input. Logged, not failed, so a human
		// reviewing test output can see which shape each malformed case took.
		t.Logf("tool call returned transport error (acceptable): %v", err)
		return
	}
	require.NotNil(t, result)
	require.True(t, result.IsError, "malformed input must produce an error result, never a fabricated success")
}

func TestMCPTools_MalformedArguments_NeverPanicOrFabricateSuccess(t *testing.T) {
	cases := []struct {
		name string
		tool string
		args map[string]any
	}{
		// --- get_daily_summary ---
		{"get_daily_summary/date is a number", "get_daily_summary", map[string]any{"date": 20260101}},
		{"get_daily_summary/date is an array", "get_daily_summary", map[string]any{"date": []string{"2026-01-01"}}},
		{"get_daily_summary/date is null", "get_daily_summary", map[string]any{"date": nil}},
		{"get_daily_summary/date missing entirely", "get_daily_summary", map[string]any{}},
		{"get_daily_summary/date is garbage text", "get_daily_summary", map[string]any{"date": "not-a-date-at-all"}},
		{"get_daily_summary/date month 13", "get_daily_summary", map[string]any{"date": "2026-13-01"}},
		{"get_daily_summary/date day 45", "get_daily_summary", map[string]any{"date": "2026-01-45"}},
		{"get_daily_summary/date is a bool", "get_daily_summary", map[string]any{"date": true}},

		// --- get_margin_delta ---
		{"get_margin_delta/period_a is a string not object", "get_margin_delta", map[string]any{
			"period_a": "2026-01-01",
			"period_b": map[string]any{"start": "2026-01-08", "end": "2026-01-14"},
		}},
		{"get_margin_delta/period_a missing", "get_margin_delta", map[string]any{
			"period_b": map[string]any{"start": "2026-01-08", "end": "2026-01-14"},
		}},
		{"get_margin_delta/start is a number", "get_margin_delta", map[string]any{
			"period_a": map[string]any{"start": 20260101, "end": "2026-01-07"},
			"period_b": map[string]any{"start": "2026-01-08", "end": "2026-01-14"},
		}},
		{"get_margin_delta/end before start", "get_margin_delta", map[string]any{
			"period_a": map[string]any{"start": "2026-01-14", "end": "2026-01-01"},
			"period_b": map[string]any{"start": "2026-01-08", "end": "2026-01-14"},
		}},
		{"get_margin_delta/both periods entirely absent", "get_margin_delta", map[string]any{}},

		// --- list_discrepancies ---
		{"list_discrepancies/neither date nor period", "list_discrepancies", map[string]any{}},
		{"list_discrepancies/both date and period", "list_discrepancies", map[string]any{
			"date":   "2026-01-01",
			"period": map[string]any{"start": "2026-01-01", "end": "2026-01-07"},
		}},
		{"list_discrepancies/period is an array", "list_discrepancies", map[string]any{"period": []int{1, 2, 3}}},

		// --- get_promotion_roi ---
		{"get_promotion_roi/campaign_id is a number", "get_promotion_roi", map[string]any{"campaign_id": 12345}},
		{"get_promotion_roi/nothing given", "get_promotion_roi", map[string]any{}},
		{"get_promotion_roi/period without platform", "get_promotion_roi", map[string]any{
			"period": map[string]any{"start": "2026-01-01", "end": "2026-01-07"},
		}},
		{"get_promotion_roi/campaign_id is an object", "get_promotion_roi", map[string]any{"campaign_id": map[string]any{"nested": true}}},

		// --- list_negative_roi_promotions ---
		{"list_negative_roi_promotions/period missing", "list_negative_roi_promotions", map[string]any{}},
		{"list_negative_roi_promotions/period is a string", "list_negative_roi_promotions", map[string]any{"period": "2026"}},
		{"list_negative_roi_promotions/negative-looking dates", "list_negative_roi_promotions", map[string]any{
			"period": map[string]any{"start": "-2026-01-01", "end": "2026-01-07"},
		}},

		// --- compare_platform_economics ---
		{"compare_platform_economics/period missing", "compare_platform_economics", map[string]any{}},
		{"compare_platform_economics/period.start is boolean", "compare_platform_economics", map[string]any{
			"period": map[string]any{"start": false, "end": "2026-01-07"},
		}},

		// --- get_period_totals ---
		{"get_period_totals/period missing", "get_period_totals", map[string]any{}},
		{"get_period_totals/period is null", "get_period_totals", map[string]any{"period": nil}},
		{"get_period_totals/year zero", "get_period_totals", map[string]any{
			"period": map[string]any{"start": "0000-01-01", "end": "0000-01-02"},
		}},

		// --- get_expense_pattern_by_day_of_month ---
		{"get_expense_pattern_by_day_of_month/period missing", "get_expense_pattern_by_day_of_month", map[string]any{}},
		{"get_expense_pattern_by_day_of_month/period.end is an array", "get_expense_pattern_by_day_of_month", map[string]any{
			"period": map[string]any{"start": "2026-01-01", "end": []string{"2026-01-31"}},
		}},
		{"get_expense_pattern_by_day_of_month/wildly out of order dates", "get_expense_pattern_by_day_of_month", map[string]any{
			"period": map[string]any{"start": "9999-12-31", "end": "0001-01-01"},
		}},
	}

	c := newTestMCPClient(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := callRaw(t, c, tc.tool, tc.args)
			requireGraceful(t, result, err)
		})
	}
}

// TestMCPTools_UnknownToolName_IsAGracefulProtocolError proves that calling
// a tool name outside the fixed 8-tool set (Constitution Principle III: "no
// open... free-form computation tool — defined, typed operations only")
// fails as a normal protocol error rather than panicking or silently
// no-op'ing.
func TestMCPTools_UnknownToolName_IsAGracefulProtocolError(t *testing.T) {
	c := newTestMCPClient(t)
	result, err := callRaw(t, c, "run_arbitrary_sql", map[string]any{"query": "DROP TABLE daily_reconciliation"})
	requireGraceful(t, result, err)
}
