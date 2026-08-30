package ambiguity

// Offline tests for specs/011-inline-grounded-advice's trigger signal
// (Decision.AdviceRequested): the prompt carries the rules, the parser
// carries the field with a conservative default, and — structurally —
// the second-pass prose writer cannot set it, the same guarantee the
// classification field already has. Zero Anthropic API calls.

import (
	"context"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/instrumentation"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/llmclient"
)

func TestBuildSystemPrompt_CarriesTheAdviceRequestSignalRules(t *testing.T) {
	prompt := buildSystemPrompt(testDataStart, testDataEnd)

	require.Contains(t, prompt, `"advice_requested"`,
		"the reply shape must include the advice_requested field or the gate can never emit the signal")
	require.Contains(t, prompt, "Advice requests and mixed data-plus-advice questions",
		"the prompt must carry the explicit advice-request section")
	require.Contains(t, prompt, `This flag never changes the classification itself`,
		"the signal must be documented as orthogonal to answerable/ambiguous/unanswerable")
	require.Contains(t, prompt, "This product never guesses at advice it cannot ground in a computed figure",
		"the ungroundable-advice refusal rule (spec 011 FR-006) must survive the widening verbatim in the prompt")
}

func TestParseGateResponse_AdviceRequestedTrue(t *testing.T) {
	d, err := parseGateResponse(`{"classification": "answerable", "clarifying_question": "", "assumption_stated": "", "reason": "", "advice_requested": true}`)
	require.NoError(t, err)
	require.True(t, d.AdviceRequested)
}

func TestParseGateResponse_AdviceRequestedFalse(t *testing.T) {
	d, err := parseGateResponse(`{"classification": "answerable", "clarifying_question": "", "assumption_stated": "", "reason": "", "advice_requested": false}`)
	require.NoError(t, err)
	require.False(t, d.AdviceRequested)
}

// An absent field must parse as false — no signal, no advice call, no
// spend. This is the conservative default the Decision doc comment
// promises, and it also keeps every pre-011 cached/scripted gate reply
// shape valid.
func TestParseGateResponse_AdviceRequestedDefaultsToFalseWhenAbsent(t *testing.T) {
	d, err := parseGateResponse(`{"classification": "answerable", "clarifying_question": "", "assumption_stated": "", "reason": ""}`)
	require.NoError(t, err)
	require.False(t, d.AdviceRequested)
}

func TestParseGateResponse_AdviceRequestedOnUnanswerableParsesButChangesNothing(t *testing.T) {
	// A refusal never runs the advisor regardless of the flag — this test
	// pins that the parser doesn't reject the combination (the model may
	// honestly report "this wanted advice AND it is unanswerable").
	d, err := parseGateResponse(`{"classification": "unanswerable", "clarifying_question": "", "assumption_stated": "", "reason": "no staffing data exists", "advice_requested": true}`)
	require.NoError(t, err)
	require.True(t, d.AdviceRequested)
	require.Equal(t, "no staffing data exists", d.RefusalReason)
}

// The Classify-level end-to-end of the signal, with a scripted fake model
// — this is the test that would have caught the exact defect found during
// live verification: parseGateResponse carried the flag correctly, but
// Classify's field-by-field copy from the parsed Decision onto the
// usage-carrying Decision omitted AdviceRequested, silently dropping the
// signal on every real request while every parser-level test stayed green.
func TestClassify_CarriesAdviceRequestedThroughToTheReturnedDecision(t *testing.T) {
	fake := &countingCreator{response: &llmclient.MessageResult{
		Text:         `{"classification": "answerable", "clarifying_question": "", "clarifying_options": [], "assumption_stated": "", "reason": "", "advice_requested": true}`,
		StopReason:   anthropic.StopReasonEndTurn,
		InputTokens:  400,
		OutputTokens: 40,
	}}
	g := newGate(fake, testDataStart, testDataEnd)

	d, err := g.Classify(context.Background(), "How can I improve my margin overall?", nil, nil)
	require.NoError(t, err)
	require.Equal(t, instrumentation.GateAnswerable, d.Result)
	require.True(t, d.AdviceRequested,
		"the flag the model emitted must survive Classify's copy onto the returned Decision — the wire signal is useless if it is dropped here")
}

// The structural half of "the writer pass cannot set the signal",
// mirroring TestParseWriterResponse_IgnoresAnyClassificationField:
// writerResponse has no advice_requested field, so even a reply that
// smuggles one in is silently ignored by encoding/json, and nothing in
// refineIfNeeded assigns Decision.AdviceRequested.
func TestParseWriterResponse_IgnoresAnyAdviceRequestedField(t *testing.T) {
	wr, err := parseWriterResponse(`{"clarifying_question": "", "clarifying_options": [], "reason": "rewritten", "advice_requested": true, "classification": "answerable"}`)
	require.NoError(t, err)
	require.Equal(t, "rewritten", wr.Reason)
	// No assertion on an AdviceRequested field is possible — writerResponse
	// has none, which is exactly the guarantee.
}
