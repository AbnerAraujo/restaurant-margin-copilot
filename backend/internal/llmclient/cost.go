package llmclient

import (
	"errors"
	"fmt"
)

// Model IDs this project uses (constitution v1.1.0, CLAUDE.md, research.md).
//
// ModelAmbiguityGate was Claude Haiku 4.5 for the original 14-day take-home
// fixture — a cheap classification task that didn't need frontier
// reasoning at that scale. It was moved to Claude Sonnet 5 on 2026-08-29
// after a real, reproducible bug: once the live dataset grew to a
// multi-year range (backend/cmd/gendata's 730-day synthetic history, on
// top of the fixture), Haiku's classification calls repeatedly
// misclassified a fully-in-range, explicitly-dated question ("July 2026",
// inside a 2024-08-01..2026-08-14 window) as unanswerable — a real date-
// comparison failure across a year boundary, not a prompt-wording issue
// (three separate prompt fixes were tried and verified NOT to resolve it;
// swapping the same call to Sonnet resolved it on the first try).
//
// The honest correction to that account, recorded here because every doc
// points at this comment: the model swap treated a symptom. Comparing an
// explicit, parseable date against a known min/max window is date
// ARITHMETIC — exactly what Constitution Principle I says no model, cheap
// or expensive, may be responsible for — and for a while the gate's prompt
// delegated that comparison to the model anyway; the three failed prompt
// fixes were attempts to make a model better at arithmetic rather than
// taking the arithmetic away from it. The real fix is
// internal/ambiguity/daterange.go's deterministic pre-check: clearly
// out-of-range explicit dates are refused in Go before any model call
// (zero tokens), and in-range explicit dates reach the model with their
// range verdicts precomputed as fact. Sonnet stays as the gate's model for
// what is genuinely a language job — resolving relative/vague date
// phrasing, date forms the conservative Go parser deliberately doesn't
// attempt, and the answerable/ambiguous/unanswerable judgment itself —
// which is "the cheapest model that clears the bar" applied to the job as
// now correctly scoped, at roughly double Haiku's cost per classification
// call.
//
// ModelExplanation narrates an already-computed result — reusing the same
// constant here (both now "claude-sonnet-5") rather than adding a second
// one priced identically, since pricePerMTok below is keyed by model
// string and a second constant equal to the same string would be a
// duplicate map key.
//
// ModelParaphraseMatch (internal/paraphrase) stays on Claude Haiku 4.5,
// deliberately NOT swept along with the ambiguity gate above: it was a
// separate constant sharing ModelAmbiguityGate's value only incidentally
// before this change, and its task (recognizing a same-meaning reworded
// question against a short, bounded candidate list) has no evidence of the
// date-comparison failure that moved the gate — moving it too would be an
// unvalidated cost increase for a problem never actually observed there.
const (
	ModelAmbiguityGate   = "claude-sonnet-5"
	ModelExplanation     = "claude-sonnet-5"
	ModelParaphraseMatch = "claude-haiku-4-5"
)

// ErrUnknownModel is returned by EstimateCostUSD for any model this project
// has not priced. Refusing to guess a cost matches this project's broader
// "refuse rather than guess" discipline (Constitution Principle II) — an
// unpriced model should surface as a loud error, not a silent zero.
var ErrUnknownModel = errors.New("llmclient: unknown model for cost estimation")

// pricePerMTok holds the per-model USD price per million tokens this
// project priced against (Anthropic first-party API rates, documented in
// research.md at the time the model choices were made). ModelAmbiguityGate
// and ModelExplanation share one entry since both are now "claude-sonnet-5"
// — see the doc comment above for why the gate moved off Haiku 4.5's
// $1/$5-per-MTok pricing. ModelParaphraseMatch keeps that original Haiku
// pricing, since it stayed on Haiku.
var pricePerMTok = map[string]struct {
	InputUSDPerMTok  float64
	OutputUSDPerMTok float64
}{
	ModelExplanation:     {InputUSDPerMTok: 2.00, OutputUSDPerMTok: 10.00},
	ModelParaphraseMatch: {InputUSDPerMTok: 1.00, OutputUSDPerMTok: 5.00},
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
