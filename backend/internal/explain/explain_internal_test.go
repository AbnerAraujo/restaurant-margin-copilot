package explain

// Unit tests over this package's pure, model-independent logic, plus the
// tool-calling loop's own control flow driven against a scripted fake
// llmCaller (see explain.go's llmCaller interface) rather than the real
// Anthropic API. None of these need DATABASE_URL or ANTHROPIC_API_KEY and
// all run in a default `go test ./...` — filling the gap explain_test.go's
// package doc comment flags: that file's only test (TestExplain_LiveSmokeTest)
// skips without both env vars, leaving MaxTurns exhaustion,
// collectProvenance/walkForSourceRowRefs/addRefs, splitContentBlocks, the
// Finding 1 partial-usage-on-error path, and the Finding 13 zero-tool-call
// guard completely untested by default.
//
// This file is `package explain` (white-box), not `package explain_test`,
// specifically so it can exercise the unexported helpers directly and
// construct an *Explainer by hand (bypassing New, which requires a real
// storage-backed MCP server) with a fake llmCaller and a minimal in-process
// MCP test server of its own.

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/llmclient"
)

// --- test doubles --------------------------------------------------------

// fakeLLM is a scripted llmCaller: it returns responses[i]/errs[i] on its
// i-th call, in order. A test wires up exactly the turn sequence it wants to
// exercise rather than depending on real model behavior.
type fakeLLM struct {
	responses []*llmclient.MessageResult
	errs      []error
	calls     int
}

func (f *fakeLLM) CreateMessage(ctx context.Context, req llmclient.MessageRequest) (*llmclient.MessageResult, error) {
	i := f.calls
	f.calls++
	var err error
	if i < len(f.errs) {
		err = f.errs[i]
	}
	if err != nil {
		return nil, err
	}
	if i >= len(f.responses) {
		return nil, fmt.Errorf("fakeLLM: no scripted response for call %d", i)
	}
	return f.responses[i], nil
}

// textBlock/toolUseBlock build real anthropic.ContentBlockUnion values by
// round-tripping through its custom UnmarshalJSON — a bare struct literal
// (ContentBlockUnion{Type: "text", Text: "..."}) does NOT work here, because
// AsAny()/AsText()/AsToolUse() re-parse the block's internally-cached raw
// JSON bytes rather than reading the struct fields directly, and only
// UnmarshalJSON populates that cache.
func textBlock(t *testing.T, text string) anthropic.ContentBlockUnion {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"type": "text", "text": text})
	require.NoError(t, err)
	var block anthropic.ContentBlockUnion
	require.NoError(t, json.Unmarshal(raw, &block))
	return block
}

func toolUseBlock(t *testing.T, id, name string, input map[string]any) anthropic.ContentBlockUnion {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"type": "tool_use", "id": id, "name": name, "input": input})
	require.NoError(t, err)
	var block anthropic.ContentBlockUnion
	require.NoError(t, json.Unmarshal(raw, &block))
	return block
}

// newFakeMCPClient builds a tiny in-process MCP server exposing one tool,
// "test_tool", that always succeeds and returns a canned source_row_refs
// entry — enough for Explain's tool-calling loop to round-trip a real
// tool_use block without needing internal/mcptools' storage-backed tools or
// a live Postgres connection. It deliberately does NOT install
// internal/mcptools' timeout+call-cap middleware (that middleware is
// unexported to this package) — these tests only need the round trip to
// succeed, not to prove the cap itself, which internal/mcptools already
// tests against the real thing. One consequence: Result.ToolCallsMade
// (budget.Used()) reads 0 in every test using this client, since nothing
// ever calls CallBudget.take() — tests assert on len(Result.ToolInvocations)
// instead, which explain.go populates directly and independently of that
// middleware.
func newFakeMCPClient(t *testing.T) *client.Client {
	t.Helper()

	s := server.NewMCPServer("explain-test-server", "0.0.1", server.WithToolCapabilities(false))
	s.AddTool(
		mcp.NewTool("test_tool", mcp.WithDescription("test tool returning canned provenance")),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultJSON(map[string]any{
				"value":           "42.00",
				"source_row_refs": []map[string]any{{"file": "sample.csv", "row": 1}},
			})
		},
	)

	c, err := client.NewInProcessClient(s)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = c.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo:      mcp.Implementation{Name: "explain-test-client", Version: "0.0.1"},
		},
	})
	require.NoError(t, err)
	return c
}

func newTestExplainer(llm llmCaller, mcpClient *client.Client) *Explainer {
	return &Explainer{
		client:       llm,
		mcpClient:    mcpClient,
		systemPrompt: "test system prompt",
	}
}

// --- splitContentBlocks ---------------------------------------------------

func TestSplitContentBlocks_SeparatesTextAndToolUse(t *testing.T) {
	blocks := []anthropic.ContentBlockUnion{
		textBlock(t, "here is my answer"),
		toolUseBlock(t, "toolu_1", "test_tool", map[string]any{"x": "y"}),
	}

	params, toolUses := splitContentBlocks(blocks)

	require.Len(t, params, 2, "both the text and the tool_use block replay into the next assistant message")
	require.Len(t, toolUses, 1)
	require.Equal(t, "toolu_1", toolUses[0].ID)
	require.Equal(t, "test_tool", toolUses[0].Name)
}

func TestSplitContentBlocks_DropsEmptyTextBlock(t *testing.T) {
	blocks := []anthropic.ContentBlockUnion{textBlock(t, "")}

	params, toolUses := splitContentBlocks(blocks)

	require.Empty(t, params, "an empty text block carries nothing worth replaying")
	require.Empty(t, toolUses)
}

func TestSplitContentBlocks_NoToolUse(t *testing.T) {
	blocks := []anthropic.ContentBlockUnion{textBlock(t, "just text")}

	params, toolUses := splitContentBlocks(blocks)

	require.Len(t, params, 1)
	require.Empty(t, toolUses)
}

// --- collectProvenance / walkForSourceRowRefs / addRefs -------------------

func TestCollectProvenance_FlatSourceRowRefs(t *testing.T) {
	seen := map[string]struct{}{}
	var ordered []string

	collectProvenance(`{"margin": "12.34", "source_row_refs": [{"file": "pos.csv", "row": 3}, {"file": "pos.csv", "row": 4}]}`, seen, &ordered)

	require.Equal(t, []string{"pos.csv:3", "pos.csv:4"}, ordered)
}

func TestCollectProvenance_NestedAndDeduplicated(t *testing.T) {
	seen := map[string]struct{}{}
	var ordered []string

	// period_a and period_b each carry their own source_row_refs, and one
	// ref (ifood.csv:1) is deliberately repeated across both — a real shape
	// get_margin_delta's PeriodMarginResult produces.
	collectProvenance(`{
		"period_a": {"source_row_refs": [{"file": "ifood.csv", "row": 1}]},
		"period_b": {"source_row_refs": [{"file": "ifood.csv", "row": 1}, {"file": "ifood.csv", "row": 2}]}
	}`, seen, &ordered)

	require.Equal(t, []string{"ifood.csv:1", "ifood.csv:2"}, ordered, "the repeated ref must appear once, in first-seen order")
}

func TestCollectProvenance_ArrayOfDaysEachWithRefs(t *testing.T) {
	seen := map[string]struct{}{}
	var ordered []string

	// list_discrepancies' DiscrepanciesResult shape: an array of per-day
	// objects, each with its own source_row_refs.
	collectProvenance(`{"days": [
		{"date": "2026-08-03", "source_row_refs": [{"file": "jet.csv", "row": 10}]},
		{"date": "2026-08-04", "source_row_refs": [{"file": "jet.csv", "row": 11}]}
	]}`, seen, &ordered)

	require.Equal(t, []string{"jet.csv:10", "jet.csv:11"}, ordered)
}

func TestCollectProvenance_MalformedJSONIsSkippedNotFatal(t *testing.T) {
	seen := map[string]struct{}{}
	var ordered []string

	require.NotPanics(t, func() {
		collectProvenance(`{not valid json`, seen, &ordered)
	})
	require.Empty(t, ordered)
}

func TestCollectProvenance_NoSourceRowRefsKey(t *testing.T) {
	seen := map[string]struct{}{}
	var ordered []string

	collectProvenance(`{"error": "no_data", "reason": "no reconciliation for that date"}`, seen, &ordered)

	require.Empty(t, ordered)
}

func TestAddRefs_SkipsEntryWithNoFile(t *testing.T) {
	seen := map[string]struct{}{}
	var ordered []string

	addRefs([]any{
		map[string]any{"row": float64(1)},                 // no "file" — skipped
		map[string]any{"file": "a.csv", "row": float64(2)}, // kept
	}, seen, &ordered)

	require.Equal(t, []string{"a.csv:2"}, ordered)
}

func TestAddRefs_NonArrayValueIsIgnored(t *testing.T) {
	seen := map[string]struct{}{}
	var ordered []string

	addRefs("not an array", seen, &ordered)

	require.Empty(t, ordered)
}

// --- looksLikeCurrencyAmount (Finding 13's structural guard) -------------

func TestLooksLikeCurrencyAmount(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"Your margin was $1,234.56 that day.", true},
		{"Your margin was 1234.56 that day.", true},
		{"That date, 2026-09-01, is outside the range we have data for (2026-08-01 through 2026-08-14).", false},
		{"I'm not sure I can answer that from the data available.", false},
		{"The reconciled margin was R$50.00.", true},
	}
	for _, c := range cases {
		require.Equal(t, c.want, looksLikeCurrencyAmount(c.text), "text: %q", c.text)
	}
}

// TestBuildSystemPrompt_MonthHasADeterministicAnchor is the narration
// step's half of QA round 6's fix — internal/ambiguity/gate_test.go's test
// of the same name covers the gate's copy of this rule. Before this fix,
// this package's "Data grounding" paragraph named "today"/"this week"/
// "last week" explicitly but said nothing about "this month"/"last month"
// at all, leaving the model free to narrate "this month" as, say, a
// trailing 30-day window while internal/httpapi's period-comparison and
// platform-trend endpoints use a real calendar-month convention for the
// same phrase — a real risk of a chat answer disagreeing with what those
// pages show for identical underlying data.
func TestBuildSystemPrompt_MonthHasADeterministicAnchor(t *testing.T) {
	prompt := buildSystemPrompt("2026-08-01", "2026-08-14")

	require.Contains(t, prompt, `"This month" is the calendar month`)
	require.Contains(t, prompt, `"last month" is the FULL prior calendar month`)
}

// TestBuildSystemPrompt_AnswersDataCoreBeforeDecliningAdvice is the
// narration step's half of a real live defect (internal/ambiguity/gate_test.go's
// TestBuildSystemPrompt_MixedDataAdviceQuestionsAreNotFlatlyUnanswerable
// covers the gate's copy): a question like "what should I change about
// staffing/menu/promotions to replicate the margin from Aug 22?" reliably
// got refused outright by the gate before this fix, and even the rare cases
// that reached this package had no instruction telling the model to answer
// the data-answerable core (what actually happened that day) before
// declining the part it genuinely can't help with (a staffing or menu
// recommendation). This is a plain offline string check that the fix — a
// new rule near the top of "Rules, no exceptions" — is present in the
// generated prompt, no live model call involved.
func TestBuildSystemPrompt_AnswersDataCoreBeforeDecliningAdvice(t *testing.T) {
	prompt := buildSystemPrompt("2026-08-01", "2026-08-14")

	require.Contains(t, prompt, "ALWAYS call the relevant tool(s) and answer the data-answerable part in full first",
		"the narration step must answer whatever the tools can show before declining an advice-shaped request this product's data can't support")
	require.Contains(t, prompt, "isn't something this tool computes or has data for")
}

// --- Explain's tool-calling loop, against the fake llmCaller --------------

func costFor(t *testing.T, inputTokens, outputTokens int64) float64 {
	t.Helper()
	cost, err := llmclient.EstimateCostUSD(llmclient.ModelExplanation, inputTokens, outputTokens)
	require.NoError(t, err)
	return cost
}

// TestExplain_ZeroToolCallCurrencyAnswerIsRefused is Finding 13's core
// case: the model answers on the very first turn, states a currency-shaped
// figure, and never called a tool. That cannot have come from the
// deterministic layer, so Explain must treat it as incomplete/refused
// rather than handing it back as AnswerText.
func TestExplain_ZeroToolCallCurrencyAnswerIsRefused(t *testing.T) {
	llm := &fakeLLM{
		responses: []*llmclient.MessageResult{
			{
				Text:          "Your margin on 2026-08-03 was $412.50.",
				ContentBlocks: []anthropic.ContentBlockUnion{textBlock(t, "Your margin on 2026-08-03 was $412.50.")},
				StopReason:    anthropic.StopReasonEndTurn,
				InputTokens:   100,
				OutputTokens:  20,
			},
		},
	}
	e := newTestExplainer(llm, nil)

	result, err := e.Explain(context.Background(), "What was our margin on 2026-08-03?", "")

	require.NoError(t, err)
	require.NotEmpty(t, result.IncompleteReason, "a currency figure with zero tool calls must be refused, not answered")
	require.Empty(t, result.AnswerText)
	require.Zero(t, result.ToolCallsMade)
	// The turn's real usage must still be reported even though the answer
	// itself is refused — this was still a real, billed API call.
	require.Equal(t, int64(100), result.InputTokens)
	require.Equal(t, int64(20), result.OutputTokens)
}

// TestExplain_ZeroToolCallNonCurrencyAnswerStillAllowed proves the guard
// above doesn't overreach: the system prompt explicitly permits stating,
// with no tool call, that a date falls outside the covered range (an
// established fact per systemPromptTemplate's "Date grounding" paragraph),
// and that must still come back as a normal answer, matching
// explain_test.go's live-gated "question about data outside the dataset
// period" case.
func TestExplain_ZeroToolCallNonCurrencyAnswerStillAllowed(t *testing.T) {
	answer := "2026-09-01 is outside the only period we have data for, which is 2026-08-01 through 2026-08-14."
	llm := &fakeLLM{
		responses: []*llmclient.MessageResult{
			{
				Text:          answer,
				ContentBlocks: []anthropic.ContentBlockUnion{textBlock(t, answer)},
				StopReason:    anthropic.StopReasonEndTurn,
				InputTokens:   90,
				OutputTokens:  15,
			},
		},
	}
	e := newTestExplainer(llm, nil)

	result, err := e.Explain(context.Background(), "What was our margin on 2026-09-01?", "")

	require.NoError(t, err)
	require.Empty(t, result.IncompleteReason)
	require.Equal(t, answer, result.AnswerText)
}

// TestExplain_MaxTokensTruncationIsRefusedEvenWithoutCurrencyText reproduces
// a real interaction caught live in question_interaction: "show me the day
// with the most profit and why" was served as an answer consisting only of
// "I'll need to check each day's reconciliation to find which one had the
// highest margin. Let me pull all 14 days." — a planning sentence the model
// never finished, cut off by hitting MaxAnswerTokens before it made a
// single tool call. TestExplain_ZeroToolCallCurrencyAnswerIsRefused's guard
// didn't catch it because the truncated text has no dollar amount in it.
// StopReasonMaxTokens must be refused unconditionally, independent of the
// currency heuristic.
func TestExplain_MaxTokensTruncationIsRefusedEvenWithoutCurrencyText(t *testing.T) {
	truncated := "I'll need to check each day's reconciliation to find which one had the highest margin. Let me pull all 14 days."
	llm := &fakeLLM{
		responses: []*llmclient.MessageResult{
			{
				Text:          truncated,
				ContentBlocks: []anthropic.ContentBlockUnion{textBlock(t, truncated)},
				StopReason:    anthropic.StopReasonMaxTokens,
				InputTokens:   500,
				OutputTokens:  1024,
			},
		},
	}
	e := newTestExplainer(llm, nil)

	result, err := e.Explain(context.Background(), "show me the day with the most profit and why", "")

	require.NoError(t, err)
	require.NotEmpty(t, result.IncompleteReason, "a max_tokens-truncated response must be refused, not served as an answer")
	require.Empty(t, result.AnswerText)
	require.Equal(t, int64(500), result.InputTokens)
	require.Equal(t, int64(1024), result.OutputTokens)
}

// TestExplain_MidLoopFailurePreservesPartialUsage is Finding 1: turn 0
// succeeds and makes one real (billed) tool call; turn 1's CreateMessage
// call fails. Explain must still hand back turn 0's accumulated tokens/cost
// alongside the error, rather than discarding real billed spend.
func TestExplain_MidLoopFailurePreservesPartialUsage(t *testing.T) {
	turn0 := &llmclient.MessageResult{
		ContentBlocks: []anthropic.ContentBlockUnion{toolUseBlock(t, "toolu_1", "test_tool", map[string]any{})},
		StopReason:    anthropic.StopReasonToolUse,
		InputTokens:   500,
		OutputTokens:  50,
	}
	boom := fmt.Errorf("simulated transport failure on turn 1")
	llm := &fakeLLM{
		responses: []*llmclient.MessageResult{turn0, nil},
		errs:      []error{nil, boom},
	}
	e := newTestExplainer(llm, newFakeMCPClient(t))

	result, err := e.Explain(context.Background(), "What was our margin on 2026-08-03?", "")

	require.Error(t, err, "the interaction as a whole must still fail")
	require.NotNil(t, result, "turn 0's real, billed usage must not be discarded just because turn 1 failed")
	require.Equal(t, int64(500), result.InputTokens)
	require.Equal(t, int64(50), result.OutputTokens)
	require.Equal(t, costFor(t, 500, 50), result.EstimatedCostUSD)
	require.Len(t, result.ToolInvocations, 1, "turn 0's real tool call must still be counted")
	require.NotEmpty(t, result.IncompleteReason)
}

// TestExplain_MaxTurnsExhaustion drives the loop past MaxTurns with a model
// that always calls a tool and never produces a final answer, proving the
// exhaustion path fires (rather than looping forever) and still reports the
// usage accumulated across every turn actually taken.
func TestExplain_MaxTurnsExhaustion(t *testing.T) {
	var responses []*llmclient.MessageResult
	for i := 0; i < MaxTurns+1; i++ {
		responses = append(responses, &llmclient.MessageResult{
			ContentBlocks: []anthropic.ContentBlockUnion{toolUseBlock(t, fmt.Sprintf("toolu_%d", i), "test_tool", map[string]any{})},
			StopReason:    anthropic.StopReasonToolUse,
			InputTokens:   10,
			OutputTokens:  5,
		})
	}
	llm := &fakeLLM{responses: responses}
	e := newTestExplainer(llm, newFakeMCPClient(t))

	result, err := e.Explain(context.Background(), "What was our margin on 2026-08-03?", "")

	require.NoError(t, err)
	require.Empty(t, result.AnswerText)
	require.Contains(t, result.IncompleteReason, fmt.Sprintf("%d model turns", MaxTurns))
	require.Equal(t, int64(MaxTurns*10), result.InputTokens)
	require.Equal(t, int64(MaxTurns*5), result.OutputTokens)
	require.Len(t, result.ToolInvocations, MaxTurns, "every one of the MaxTurns turns actually called the tool")
}

// TestExplain_ModelRefusal proves the existing model-safety-refusal branch
// (unrelated to Findings 1/13, but otherwise also untested by default)
// still reports accumulated usage from that same turn.
func TestExplain_ModelRefusal(t *testing.T) {
	llm := &fakeLLM{
		responses: []*llmclient.MessageResult{
			{
				Refused:         true,
				RefusalCategory: "some_category",
				InputTokens:     42,
				OutputTokens:    1,
			},
		},
	}
	e := newTestExplainer(llm, nil)

	result, err := e.Explain(context.Background(), "anything", "")

	require.NoError(t, err)
	require.Contains(t, result.IncompleteReason, "some_category")
	require.Equal(t, int64(42), result.InputTokens)
}
