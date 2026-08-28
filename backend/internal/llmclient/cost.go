package llmclient

import (
	"errors"
	"fmt"
)

// Model IDs this project has decided on (constitution v1.1.0, CLAUDE.md,
// research.md): Claude Haiku 4.5 for the cheap ambiguity-classification
// gate, Claude Sonnet 5 for narrating an already-computed result. Neither
// step needs frontier reasoning, so Opus/Fable are deliberately not used
// here — see research.md's "LLM vendor and model split" for the rationale.
const (
	ModelAmbiguityGate = "claude-haiku-4-5"
	ModelExplanation   = "claude-sonnet-5"
)

// ErrUnknownModel is returned by EstimateCostUSD for any model this project
// has not priced. Refusing to guess a cost matches this project's broader
// "refuse rather than guess" discipline (Constitution Principle II) — an
// unpriced model should surface as a loud error, not a silent zero.
var ErrUnknownModel = errors.New("llmclient: unknown model for cost estimation")

// pricePerMTok holds the per-model USD price per million tokens this
// project priced against (Anthropic first-party API rates, documented in
// research.md at the time the model choices were made).
var pricePerMTok = map[string]struct {
	InputUSDPerMTok  float64
	OutputUSDPerMTok float64
}{
	ModelAmbiguityGate: {InputUSDPerMTok: 1.00, OutputUSDPerMTok: 5.00},
	ModelExplanation:   {InputUSDPerMTok: 2.00, OutputUSDPerMTok: 10.00},
}

// EstimateCostUSD computes the deterministic USD cost of one model call from
// its token usage. This is arithmetic, not model output (Constitution
// Principle I extends here: instrumentation's cost figure must be
// reproducible from the same inputs every time, never reported by the model
// itself).
func EstimateCostUSD(model string, inputTokens, outputTokens int64) (float64, error) {
	price, ok := pricePerMTok[model]
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrUnknownModel, model)
	}

	const tokensPerMillion = 1_000_000.0
	cost := (float64(inputTokens)/tokensPerMillion)*price.InputUSDPerMTok +
		(float64(outputTokens)/tokensPerMillion)*price.OutputUSDPerMTok
	return cost, nil
}
