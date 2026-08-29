package storage_test

// Live-Postgres integration tests for specs/009-business-insight-advisor's
// storage layer — the business_insight_interaction ledger written by
// POST /api/business-insight. Follows badge_expansion_test.go's own
// established pattern exactly (connectOrSkip: skipped, not faked, when
// DATABASE_URL is unset), with sentinel advice text so cleanup can never
// touch a real ledger row.

import (
	"encoding/json"
	"fmt"

	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

const businessInsightSentinelText = "TEST-SENTINEL business-insight advice row — safe to delete"

func TestCreateBusinessInsightInteraction_RoundTripsAgainstTheRealTable(t *testing.T) {
	conn, q, ctx := connectOrSkip(t)

	// LIFO-safe cleanup (docs/plan.md's mistakes-log entry on t.Cleanup
	// ordering): registered before the insert, keyed on sentinel text, so
	// a failed assertion never strands the row.
	t.Cleanup(func() {
		_, err := conn.Exec(ctx, "DELETE FROM business_insight_interaction WHERE advice_text = $1", businessInsightSentinelText)
		require.NoError(t, err)
	})

	grounding, err := json.Marshal([]map[string]any{{
		"name":        "get_daily_summary",
		"result_json": map[string]any{"date": "2026-08-03", "discrepancy_flags": []map[string]string{{"type": "duplicate_order_removed", "detail": "dup"}}},
	}})
	require.NoError(t, err)

	var cost pgtype.Numeric
	require.NoError(t, cost.Scan(fmt.Sprintf("%.6f", 0.004740)))

	row, err := q.CreateBusinessInsightInteraction(ctx, storage.CreateBusinessInsightInteractionParams{
		Kind:               "discrepancy_pattern",
		GroundingToolCalls: grounding,
		AdviceText:         businessInsightSentinelText,
		ModelUsed:          "claude-sonnet-5",
		InputTokens:        1420,
		OutputTokens:       190,
		EstimatedCostUsd:   cost,
		LatencyMs:          2100,
	})
	require.NoError(t, err)

	require.Equal(t, "discrepancy_pattern", row.Kind)
	require.Equal(t, businessInsightSentinelText, row.AdviceText)
	require.Equal(t, "claude-sonnet-5", row.ModelUsed)
	require.EqualValues(t, 1420, row.InputTokens)
	require.EqualValues(t, 190, row.OutputTokens)
	require.EqualValues(t, 2100, row.LatencyMs)
	require.True(t, row.CreatedAt.Valid, "created_at must be set by the database default")
	require.JSONEq(t, string(grounding), string(row.GroundingToolCalls))

	roundTripped, err := row.EstimatedCostUsd.Float64Value()
	require.NoError(t, err)
	require.InDelta(t, 0.004740, roundTripped.Float64, 1e-9)

	// The aggregate queries see the row too — the Sum/Count pair the cost
	// ledger reporting relies on.
	count, err := q.CountBusinessInsightInteractions(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, count, int64(1))

	total, err := q.SumBusinessInsightCost(ctx)
	require.NoError(t, err)
	totalFloat, err := total.Float64Value()
	require.NoError(t, err)
	require.GreaterOrEqual(t, totalFloat.Float64, 0.004740)
}

func TestCreateBusinessInsightInteraction_RejectsAnUnknownKind(t *testing.T) {
	// The CHECK constraint is a real, load-bearing guard (a closed set of
	// five kinds, per migration 000010's own comment) — prove the database
	// actually enforces it rather than trusting the comment.
	conn, q, ctx := connectOrSkip(t)
	_ = conn

	var cost pgtype.Numeric
	require.NoError(t, cost.Scan("0.001000"))

	_, err := q.CreateBusinessInsightInteraction(ctx, storage.CreateBusinessInsightInteractionParams{
		Kind:               "not_a_real_kind",
		GroundingToolCalls: json.RawMessage(`[]`),
		AdviceText:         businessInsightSentinelText,
		ModelUsed:          "claude-sonnet-5",
		InputTokens:        1,
		OutputTokens:       1,
		EstimatedCostUsd:   cost,
		LatencyMs:          1,
	})
	require.Error(t, err, "an out-of-set kind must violate the CHECK constraint")
	require.Contains(t, err.Error(), "check", "want a constraint violation, got: %v", err)
}
