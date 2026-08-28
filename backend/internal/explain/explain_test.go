package explain_test

// TestExplain_LiveSmokeTest makes a small, deliberately bounded number of
// real Claude Sonnet 5 calls against the real in-process MCP server (backed
// by the real, already-persisted 2026-08-01..14 fixture data) to prove the
// tool-calling loop's request shape — tool definitions built from the live
// MCP server, tool_use round-tripping, provenance collection — actually
// works end to end, not just against hand-crafted types. It only reads
// already-persisted rows (no writes, no deletes), so it cannot touch the
// fixture data's integrity. Skipped, not faked, when either DATABASE_URL or
// ANTHROPIC_API_KEY isn't set. Full evaluation-harness verification happens
// in a later phase; tasks.md's "under 10 calls" guidance for this phase is
// deliberately respected here (two scenarios, a handful of model turns).

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/explain"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/llmclient"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/mcptools"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

func TestExplain_LiveSmokeTest(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping live-Postgres-backed smoke test")
	}
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set; skipping live Claude Sonnet 5 smoke test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close(context.Background()) })

	mcpServer := mcptools.RegisterMCPServer(storage.New(conn))
	explainer, err := explain.New(ctx, llmclient.New(), mcpServer)
	require.NoError(t, err)

	t.Run("answerable question about real fixture data", func(t *testing.T) {
		result, err := explainer.Explain(ctx, "What was our reconciled margin on 2026-08-03, and were there any discrepancies that day?", "")
		require.NoError(t, err)
		require.Empty(t, result.IncompleteReason, "should produce a final answer, not stop mid-loop")
		require.NotEmpty(t, result.AnswerText)
		require.GreaterOrEqual(t, result.ToolCallsMade, 1, "must have called at least one MCP tool rather than guessing")
		require.NotEmpty(t, result.ProvenanceRefs, "an answer about real data must carry provenance")
		require.Greater(t, result.EstimatedCostUSD, 0.0)
		t.Logf("answer: %s", result.AnswerText)
		t.Logf("provenance: %v", result.ProvenanceRefs)
		t.Logf("tool calls=%d, tokens=%d in / %d out, cost=$%.6f", result.ToolCallsMade, result.InputTokens, result.OutputTokens, result.EstimatedCostUSD)
	})

	t.Run("question about data outside the fixture period narrates the tool's no_data error rather than guessing", func(t *testing.T) {
		result, err := explainer.Explain(ctx, "What was our margin on 2026-09-01?", "")
		require.NoError(t, err)
		require.Empty(t, result.IncompleteReason)
		require.NotEmpty(t, result.AnswerText)
		require.GreaterOrEqual(t, result.ToolCallsMade, 1)
		t.Logf("answer: %s", result.AnswerText)
		t.Logf("tool calls=%d, tokens=%d in / %d out, cost=$%.6f", result.ToolCallsMade, result.InputTokens, result.OutputTokens, result.EstimatedCostUSD)
	})
}
