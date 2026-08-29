package llmclient_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/llmclient"
)

// TestEstimateCostUSD locks in the pricing this project has documented and
// decided on (CLAUDE.md / research.md, llmclient/cost.go's own doc
// comment): Claude Sonnet 5 at $2/$10 per MTok (input/output) for both the
// ambiguity gate and the explanation step (the gate moved off Haiku 4.5 on
// 2026-08-29 after a real multi-year date-comparison bug — see cost.go),
// and Claude Haiku 4.5 at $1/$5 per MTok, kept for the paraphrase-match
// classifier specifically, which never showed that bug. This is
// deterministic arithmetic — Constitution Principle I applies here too:
// cost estimation is never something a model call gets to report on its
// own, it is computed the same way every time.
func TestEstimateCostUSD(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		model        string
		inputTokens  int64
		outputTokens int64
		wantUSD      float64
	}{
		{
			name:         "sonnet ambiguity gate, small request",
			model:        llmclient.ModelAmbiguityGate,
			inputTokens:  1000,
			outputTokens: 500,
			// (1000/1e6)*2.00 + (500/1e6)*10.00 = 0.002 + 0.005 = 0.007
			wantUSD: 0.007,
		},
		{
			name:         "sonnet explanation step, larger request",
			model:        llmclient.ModelExplanation,
			inputTokens:  2_000_000,
			outputTokens: 100_000,
			// (2_000_000/1e6)*2.00 + (100_000/1e6)*10.00 = 4.00 + 1.00 = 5.00
			wantUSD: 5.00,
		},
		{
			name:         "haiku paraphrase match, small request",
			model:        llmclient.ModelParaphraseMatch,
			inputTokens:  1000,
			outputTokens: 500,
			// (1000/1e6)*1.00 + (500/1e6)*5.00 = 0.001 + 0.0025 = 0.0035
			wantUSD: 0.0035,
		},
		{
			name:         "zero tokens costs zero",
			model:        llmclient.ModelAmbiguityGate,
			inputTokens:  0,
			outputTokens: 0,
			wantUSD:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := llmclient.EstimateCostUSD(tt.model, tt.inputTokens, tt.outputTokens)
			require.NoError(t, err)
			assert.InDelta(t, tt.wantUSD, got, 1e-9)
		})
	}
}

func TestEstimateCostUSD_UnknownModel(t *testing.T) {
	t.Parallel()

	_, err := llmclient.EstimateCostUSD("claude-opus-5", 100, 100)
	require.Error(t, err)
	assert.True(t, errors.Is(err, llmclient.ErrUnknownModel))
}
