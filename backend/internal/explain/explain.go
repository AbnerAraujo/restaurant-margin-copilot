// Package explain is the explanation step Constitution Principle I
// requires: Claude Sonnet 5 narrates an already-computed result reached
// exclusively through internal/mcptools' fixed, typed tool set — it never
// computes a margin, delta, or ROI figure itself. This package owns the
// tool-calling loop: direct Anthropic API calls with the MCP tool
// definitions attached, no agent framework (CLAUDE.md, constitution's
// Technology & Scope Constraints).
//
// Like internal/ambiguity, this package never writes a QuestionInteraction
// row itself (data-model.md: "No entity here is ever written by
// internal/ambiguity or internal/explain — those packages only read and
// narrate"). internal/httpapi.HandleAsk calls Explain, gets back a Result
// carrying the tokens/cost/latency this one interaction's Sonnet 5 calls
// actually used, and logs it via internal/instrumentation itself.
package explain

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/llmclient"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/mcptools"
)

// MaxTurns bounds how many request/response round trips one Explain call
// will make with the model, independent of mcptools.DefaultMaxToolCallsPerInteraction
// (the actual per-interaction tool-call cap, enforced inside every tool
// call itself — see mcptools/limits.go). MaxTurns is a coarser outer guard
// against a model that keeps requesting tool calls after the budget starts
// returning tool_call_cap_exceeded instead of producing a final answer;
// it is set a little above the tool-call cap so the model has room to see
// the cap-exceeded error and still respond in text once.
const MaxTurns = mcptools.DefaultMaxToolCallsPerInteraction + 3

// MaxAnswerTokens bounds the model's final narration turn.
const MaxAnswerTokens = 1024

// Result is what Explain returns: the narrated answer (empty if the model
// could not produce one within MaxTurns — Constitution Principle II: never
// a partial, unlabeled answer), the provenance every tool call in this
// interaction actually returned, and the aggregate token/cost/latency
// across every model turn this one interaction took, ready for
// internal/instrumentation.
type Result struct {
	AnswerText     string
	ProvenanceRefs []string
	ToolCallsMade  int

	// IncompleteReason is set (and AnswerText left empty) when the loop
	// exhausted MaxTurns, or the model refused, without ever producing a
	// final narrated answer — the caller (internal/httpapi) treats this as
	// a refusal, not a partial answer.
	IncompleteReason string

	InputTokens      int64
	OutputTokens     int64
	EstimatedCostUSD float64
	LatencyMs        int64
}

// Explainer runs the tool-calling loop over one internal/mcptools MCP
// server.
type Explainer struct {
	client       *llmclient.Client
	mcpClient    *client.Client
	tools        []anthropic.ToolUnionParam
	systemPrompt string
}

// New builds an Explainer. mcpServer is normally the result of
// mcptools.RegisterMCPServer — Explainer talks to it via mcp-go's
// in-process client (client.NewInProcessClient), which is what actually
// routes every tool call through the timeout+call-cap middleware
// RegisterMCPServer installs (mcptools/limits.go): calling a *ServerTool's
// Handler directly, bypassing the client, would skip that middleware
// entirely, defeating Constitution Principle III's enforcement.
//
// dataStart/dataEnd are the actual inclusive min/max date (YYYY-MM-DD) this
// product has reconciled data for — resolved once by the caller
// (cmd/server/main.go, via internal/storage.LoadDataDateRange) and baked
// into this Explainer's system prompt, the same date-grounding fix
// internal/ambiguity.New applies (docs/plan.md mistakes log: "date-year
// grounding defect"). This matters here too, not just in the gate: the
// model still has to resolve relative/year-less dates into concrete tool
// arguments (e.g. a get_margin_delta period) when narrating an answerable
// or assumption-carrying question.
func New(ctx context.Context, llm *llmclient.Client, mcpServer *server.MCPServer, dataStart, dataEnd string) (*Explainer, error) {
	mcpClient, err := client.NewInProcessClient(mcpServer)
	if err != nil {
		return nil, fmt.Errorf("explain: creating in-process MCP client: %w", err)
	}
	if _, err := mcpClient.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo:      mcp.Implementation{Name: "restaurant-margin-copilot-explain", Version: "0.1.0"},
		},
	}); err != nil {
		return nil, fmt.Errorf("explain: initializing MCP client: %w", err)
	}

	listed, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("explain: listing MCP tools: %w", err)
	}

	return &Explainer{
		client:       llm,
		mcpClient:    mcpClient,
		tools:        anthropicTools(listed.Tools),
		systemPrompt: buildSystemPrompt(dataStart, dataEnd),
	}, nil
}

// Explain narrates an answer to question, calling MCP tools as needed.
// assumptionStated, when non-empty, is an assumption internal/ambiguity's
// gate already decided to proceed under (spec FR-006) — it is handed to
// the model as a fact already established, not something for the model to
// re-derive or second-guess.
func (e *Explainer) Explain(ctx context.Context, question, assumptionStated string) (*Result, error) {
	budget := mcptools.NewCallBudget(mcptools.DefaultMaxToolCallsPerInteraction)
	ctx = mcptools.WithCallBudget(ctx, budget)

	userText := question
	if assumptionStated != "" {
		userText = fmt.Sprintf("%s\n\n(Note: an upstream ambiguity check already resolved this question under the following stated assumption — proceed under it rather than asking again: %s)", question, assumptionStated)
	}

	messages := []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(userText))}

	var (
		totalIn, totalOut int64
		totalCostUSD      float64
		totalLatencyMs    int64
		seenRefs          = map[string]struct{}{}
		orderedRefs       []string
	)

	for turn := 0; turn < MaxTurns; turn++ {
		resp, err := e.client.CreateMessage(ctx, llmclient.MessageRequest{
			Model:     llmclient.ModelExplanation,
			System:    e.systemPrompt,
			MaxTokens: MaxAnswerTokens,
			Messages:  messages,
			Tools:     e.tools,
		})
		if err != nil {
			return nil, fmt.Errorf("explain: turn %d: %w", turn, err)
		}

		totalIn += resp.InputTokens
		totalOut += resp.OutputTokens
		totalLatencyMs += resp.Latency.Milliseconds()
		cost, err := resp.EstimatedCostUSD(llmclient.ModelExplanation)
		if err != nil {
			return nil, fmt.Errorf("explain: turn %d: %w", turn, err)
		}
		totalCostUSD += cost

		if resp.Refused {
			return &Result{
				IncompleteReason: "model declined to answer (category: " + resp.RefusalCategory + ")",
				ToolCallsMade:    budget.Used(),
				InputTokens:      totalIn, OutputTokens: totalOut,
				EstimatedCostUSD: totalCostUSD, LatencyMs: totalLatencyMs,
			}, nil
		}

		assistantBlocks, toolUses := splitContentBlocks(resp.ContentBlocks)
		if len(assistantBlocks) > 0 {
			messages = append(messages, anthropic.NewAssistantMessage(assistantBlocks...))
		}

		if resp.StopReason != anthropic.StopReasonToolUse || len(toolUses) == 0 {
			// A final narrated answer: Principle I is enforced upstream by
			// what tools exist (no free-form computation tool) and by
			// systemPrompt's instruction to never state a number that
			// didn't come from a tool result — this package cannot itself
			// verify that a given sentence's numbers trace to a tool call,
			// but it never computes one either.
			return &Result{
				AnswerText:     resp.Text,
				ProvenanceRefs: orderedRefs,
				ToolCallsMade:  budget.Used(),
				InputTokens:    totalIn, OutputTokens: totalOut,
				EstimatedCostUSD: totalCostUSD, LatencyMs: totalLatencyMs,
			}, nil
		}

		toolResultBlocks := make([]anthropic.ContentBlockParamUnion, 0, len(toolUses))
		for _, tu := range toolUses {
			text, isError := e.callTool(ctx, tu, seenRefs, &orderedRefs)
			toolResultBlocks = append(toolResultBlocks, anthropic.NewToolResultBlock(tu.ID, text, isError))
		}
		messages = append(messages, anthropic.NewUserMessage(toolResultBlocks...))
	}

	// Exhausted MaxTurns without a final answer — Constitution Principle
	// II: report inability plainly (spec Acceptance Scenario US3.3) rather
	// than returning whatever partial narration exists.
	return &Result{
		IncompleteReason: fmt.Sprintf("could not produce an answer within %d model turns (tool-call cap or loop guard reached)", MaxTurns),
		ToolCallsMade:    budget.Used(),
		InputTokens:      totalIn, OutputTokens: totalOut,
		EstimatedCostUSD: totalCostUSD, LatencyMs: totalLatencyMs,
	}, nil
}

// callTool invokes one tool_use block against the in-process MCP client
// (so the timeout+call-cap middleware applies — see New's doc comment),
// returning the text to hand back to the model as a tool_result block and
// whether it represents an error, and — on success — folding any
// source_row_refs the tool returned into seen/ordered.
func (e *Explainer) callTool(ctx context.Context, tu anthropic.ToolUseBlock, seen map[string]struct{}, ordered *[]string) (text string, isError bool) {
	result, err := e.mcpClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      tu.Name,
			Arguments: tu.Input,
		},
	})
	if err != nil {
		return fmt.Sprintf("tool invocation failed: %v", err), true
	}

	text = toolResultText(result)
	if !result.IsError {
		collectProvenance(text, seen, ordered)
	}
	return text, result.IsError
}

func toolResultText(r *mcp.CallToolResult) string {
	if r == nil {
		return ""
	}
	var sb strings.Builder
	for _, c := range r.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// collectProvenance walks a tool result's JSON looking for
// "source_row_refs" arrays (the shape every internal/mcptools result uses,
// per contracts/mcp-tools.md's cross-cutting provenance rule) and folds
// each {"file", "row"} entry into a deduplicated, order-preserving list of
// "file:row" strings. This is deliberately generic — it does not hardcode
// per-tool response shapes — so it keeps working unchanged as later
// phases add more tools (get_promotion_roi, list_negative_roi_promotions)
// that carry the same provenance shape. Malformed/unparseable tool output
// is skipped rather than treated as fatal: provenance collection is a
// best-effort enrichment on top of an answer the model already has, not a
// gate on whether the interaction can proceed.
func collectProvenance(rawJSON string, seen map[string]struct{}, ordered *[]string) {
	var v any
	if err := json.Unmarshal([]byte(rawJSON), &v); err != nil {
		return
	}
	walkForSourceRowRefs(v, seen, ordered)
}

func walkForSourceRowRefs(v any, seen map[string]struct{}, ordered *[]string) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if k == "source_row_refs" {
				addRefs(val, seen, ordered)
				continue
			}
			walkForSourceRowRefs(val, seen, ordered)
		}
	case []any:
		for _, item := range t {
			walkForSourceRowRefs(item, seen, ordered)
		}
	}
}

func addRefs(v any, seen map[string]struct{}, ordered *[]string) {
	arr, ok := v.([]any)
	if !ok {
		return
	}
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		file, _ := m["file"].(string)
		row, _ := m["row"].(float64)
		if file == "" {
			continue
		}
		ref := fmt.Sprintf("%s:%d", file, int(row))
		if _, dup := seen[ref]; dup {
			continue
		}
		seen[ref] = struct{}{}
		*ordered = append(*ordered, ref)
	}
}

// splitContentBlocks separates one model turn's response content into the
// anthropic.ContentBlockParamUnion list needed to replay it back as the
// next assistant message, and the anthropic.ToolUseBlock values (if any)
// that need executing.
func splitContentBlocks(blocks []anthropic.ContentBlockUnion) ([]anthropic.ContentBlockParamUnion, []anthropic.ToolUseBlock) {
	var params []anthropic.ContentBlockParamUnion
	var toolUses []anthropic.ToolUseBlock
	for _, block := range blocks {
		switch v := block.AsAny().(type) {
		case anthropic.TextBlock:
			if v.Text != "" {
				params = append(params, anthropic.NewTextBlock(v.Text))
			}
		case anthropic.ToolUseBlock:
			toolUses = append(toolUses, v)
			params = append(params, anthropic.NewToolUseBlock(v.ID, v.Input, v.Name))
		}
	}
	return params, toolUses
}

// anthropicTools converts internal/mcptools' MCP tool definitions into the
// Anthropic SDK's tool-param shape, so the same schema
// mcptools.RegisterMCPServer declared (contracts/mcp-tools.md) is exactly
// what the model sees — this package never hand-duplicates a tool's shape.
func anthropicTools(tools []mcp.Tool) []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		tp := anthropic.ToolParam{
			Name:        t.Name,
			Description: param.NewOpt(t.Description),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: t.InputSchema.Properties,
				Required:   t.InputSchema.Required,
			},
		}
		out = append(out, anthropic.ToolUnionParam{OfTool: &tp})
	}
	return out
}

// systemPromptTemplate is the direct enforcement, at the narration layer, of
// Constitution Principle I ("the model MUST be restricted to interpreting
// the user's question and narrating an already-computed result") and
// Principle IV (provenance on every number) — the tools themselves are the
// hard boundary (no free-form computation tool exists at all), but the
// prompt is what keeps the model from, say, adding two tool results
// together itself instead of calling get_margin_delta.
//
// %[1]s/%[2]s are the actual data date range (buildSystemPrompt's
// dataStart/dataEnd), the same date-grounding fix applied in
// internal/ambiguity's system prompt — see that package's
// systemPromptTemplate doc comment for the full bug this addresses
// (docs/plan.md mistakes log: "date-year grounding defect").
const systemPromptTemplate = `You are the explanation step of a restaurant margin-reconciliation copilot. You narrate already-computed results in plain language for a restaurant owner — you never compute, estimate, or independently derive a number yourself.

Data grounding: the only data this product has spans %[1]s through %[2]s inclusive, and %[2]s is this product's own notion of "today" — never the real-world current date. Resolve "today", "this week", "last week", and any date given without a year against %[2]s and the %[1]s..%[2]s range, exactly as an upstream ambiguity check already would have. Never state, in your answer or in a tool call argument, a year outside what %[1]s..%[2]s spans.

Rules, no exceptions:
- Every number you state MUST come directly from a tool call result. Never perform arithmetic on tool results yourself (no adding, subtracting, or averaging numbers across multiple tool calls) — if you need a combined or comparative figure, call the tool that computes it (e.g. get_margin_delta for a comparison between two periods), rather than computing it yourself from two get_daily_summary calls.
- If a tool returns a typed error (e.g. "no_data", "insufficient_data", "invalid_input"), tell the user plainly what is missing or why the request could not be fulfilled. Never estimate, extrapolate, or state a plausible-sounding number in place of a refused tool call.
- Never state that a specific named entity — a campaign, promotion, or supplier — is missing or "not in the data" without first calling the relevant tool (e.g. get_promotion_roi) and confirming via its actual no_data error. A claim that a named entity doesn't exist must always be grounded in a real tool response, never asserted from assumption or recollection — asserting absence without checking is exactly as much a fabrication as inventing a number. (This does not apply to the date range and scope already stated in "Data grounding" above — you may state directly, without a tool call, that a date is outside %[1]s..%[2]s, since that boundary is already an established fact, not something to verify per-question.)
- get_promotion_roi's campaign_id lookup tolerates a shortened form (e.g. "LUNCHFIX") or a full human-readable campaign name (e.g. "Banner Ad - Lunch Fix Menu (JET-CAMP-LUNCHFIX)") as well as the exact id — pass whatever reference the user gave directly as campaign_id rather than declining to call the tool because the text isn't an exact id, and rather than inventing your own guess at the id. Only report a campaign as unavailable after the tool itself returns no_data for it.
- Cite where each number came from (which date, which period, which file/rows if asked) using the tool result's own fields — never invent a citation.
- If you are not sure a question is fully answerable from what the tools return, say so rather than guessing.
- Keep answers concise and in plain language suitable for a busy restaurant owner, not a data analyst.`

// buildSystemPrompt substitutes the real data date range into
// systemPromptTemplate (see internal/ambiguity.buildSystemPrompt, which
// this mirrors).
func buildSystemPrompt(dataStart, dataEnd string) string {
	return fmt.Sprintf(systemPromptTemplate, dataStart, dataEnd)
}
