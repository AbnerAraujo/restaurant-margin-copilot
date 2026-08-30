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
	"log"
	"regexp"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/answerverify"
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

// MaxAnswerTokens bounds the model's final narration turn. Raised from the
// original 1024 after a real, recurring live truncation, confirmed from
// question_interaction: "The single highest-expense calendar date
// overall"/"...in 2026" hit exactly 1024 output tokens on three separate
// live attempts (two before get_expense_pattern_by_day_of_month existed,
// one after) — a question whose honest answer needs real room, either to
// explain in detail why "highest expense on one specific calendar date"
// isn't something get_period_totals ranks (it ranks by margin, not
// expense) while offering real alternatives, or, now that
// get_expense_pattern_by_day_of_month exists, to narrate that tool's
// fuller result (up to 31 day-of-month rows plus highest/lowest) well.
// 2048 gives real headroom for either case — still a hard, deliberate
// cap on ONE narration turn, not a loosening into unbounded prose — and
// a response that still hits it is caught explicitly by the
// stop_reason==MaxTokens check just below, never silently trusted.
const MaxAnswerTokens = 2048

// ToolInvocation is one successful MCP tool call this interaction made: the
// tool's name and the raw JSON result it returned. Recorded so a caller can
// derive presentation from what the DETERMINISTIC layer actually computed —
// internal/httpapi uses it to pick a chart type (table/bar/pie) in plain Go,
// from the tool name and the shape of its result, with no second model call
// involved (Constitution Principle I: the model narrates, it does not decide
// how the product renders a number).
//
// Errored tool calls are deliberately excluded: a typed no_data /
// insufficient_data result is a refusal to produce a figure, and there is by
// definition nothing to chart from one.
type ToolInvocation struct {
	Name       string
	ResultJSON string
}

// Result is what Explain returns: the narrated answer (empty if the model
// could not produce one within MaxTurns — Constitution Principle II: never
// a partial, unlabeled answer), the provenance every tool call in this
// interaction actually returned, the deterministic tool results behind it,
// and the aggregate token/cost/latency across every model turn this one
// interaction took, ready for internal/instrumentation.
type Result struct {
	AnswerText      string
	ProvenanceRefs  []string
	ToolCallsMade   int
	ToolInvocations []ToolInvocation

	// IncompleteReason is set (and AnswerText left empty) when the loop
	// exhausted MaxTurns, the model refused, the model narrated an
	// answer without ever making a grounded tool call (see Explain's
	// zero-tool-call guard), or the narration stated a money/percentage
	// figure that does not appear in what the tools returned (Explain's
	// internal/answerverify check) — the caller (internal/httpapi) treats
	// this as a refusal, not a partial answer.
	IncompleteReason string

	InputTokens      int64
	OutputTokens     int64
	EstimatedCostUSD float64
	LatencyMs        int64
}

// AdviceHandoffNote is appended (by internal/httpapi, deterministically,
// in Go) to the narration input for exactly the requests where an inline
// advisor call WILL follow the narration (specs/011-inline-grounded-advice
// FR-011): the ambiguity gate flagged the question as advice-requesting
// AND an advisor is actually configured. It exists to resolve an
// instruction conflict this feature would otherwise create: the system
// prompt's mixed-question rule tells the model to plainly decline the
// advice-shaped part of a question — correct when nothing downstream will
// ever address it, and wrong when the advisor step is about to address it
// in the same reply. Like the gate's precheckFactNote, this is a typed,
// constant, per-request fact handed to the model, never a model decision.
const AdviceHandoffNote = `(Note: an upstream check identified that this question also asks for advice or a recommendation. A separate advisor step will generate a clearly-labeled, general-practice suggestion grounded in your tool results, appended after your answer in this same reply. So: answer the data-answerable part in full exactly as usual, but do NOT state that advice cannot be given, and do NOT add recommendations of your own — the advisor step owns that part.)`

// llmCaller is the subset of *llmclient.Client this package's tool-calling
// loop needs. Declared as an interface — rather than depending on the
// concrete *llmclient.Client directly — solely so tests can drive Explain's
// multi-turn loop (a mid-loop API failure, a first-turn answer with no
// tool call) with scripted responses, without making real, billed
// Anthropic API calls. *llmclient.Client satisfies this today with no
// changes; New (below) still takes one exactly as before.
type llmCaller interface {
	CreateMessage(ctx context.Context, req llmclient.MessageRequest) (*llmclient.MessageResult, error)
}

// Explainer runs the tool-calling loop over one internal/mcptools MCP
// server.
type Explainer struct {
	client       llmCaller
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
//
// On a mid-loop failure (a turn's CreateMessage call, or its cost
// estimation, errors), Explain still returns a non-nil *Result alongside
// the error, carrying whatever InputTokens/OutputTokens/EstimatedCostUSD/
// LatencyMs/ToolCallsMade this interaction had already accumulated from
// earlier turns before the failure. Those tokens were really billed by
// Anthropic regardless of how this call ends, so the caller
// (internal/httpapi) MUST still log that partial spend via
// internal/instrumentation before turning the error into an HTTP response
// — never discard it just because the interaction as a whole failed. This
// mirrors the same "a real API call may have run and been billed even when
// err is non-nil" discipline internal/ambiguity.Gate.writeBetterText
// already applies to its own second-pass call.
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
		invocations       []ToolInvocation
		// verifySources is every tool result text this interaction saw,
		// INCLUDING the errored ones invocations deliberately drops. A typed
		// no_data/invalid_input payload is still deterministic tool output the
		// narration may legitimately quote a figure from, and feeding it to
		// answerverify can only widen the allowed set, never narrow it — so
		// including it removes a false-refusal risk rather than adding one.
		verifySources []string
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
			// The call itself failed (transport/timeout/API error) —
			// nothing new was billed on THIS turn, but turns 0..turn-1 may
			// already have accumulated real, billed usage. Return it rather
			// than discarding it (see this method's doc comment).
			return &Result{
				IncompleteReason: fmt.Sprintf("model call failed on turn %d: %v", turn, err),
				ToolCallsMade:    budget.Used(),
				ToolInvocations:  invocations,
				InputTokens:      totalIn, OutputTokens: totalOut,
				EstimatedCostUSD: totalCostUSD, LatencyMs: totalLatencyMs,
			}, fmt.Errorf("explain: turn %d: %w", turn, err)
		}

		totalIn += resp.InputTokens
		totalOut += resp.OutputTokens
		totalLatencyMs += resp.Latency.Milliseconds()
		cost, err := resp.EstimatedCostUSD(llmclient.ModelExplanation)
		if err != nil {
			// Unlike the branch above, THIS turn's tokens (already folded
			// into totalIn/totalOut/totalLatencyMs just above) were
			// genuinely billed — only the cost estimate itself failed (an
			// unpriced model string). Carry that real usage back too.
			return &Result{
				IncompleteReason: fmt.Sprintf("cost estimation failed on turn %d: %v", turn, err),
				ToolCallsMade:    budget.Used(),
				ToolInvocations:  invocations,
				InputTokens:      totalIn, OutputTokens: totalOut,
				EstimatedCostUSD: totalCostUSD, LatencyMs: totalLatencyMs,
			}, fmt.Errorf("explain: turn %d: %w", turn, err)
		}
		totalCostUSD += cost

		if resp.Refused {
			return &Result{
				IncompleteReason: "model declined to answer (category: " + resp.RefusalCategory + ")",
				ToolCallsMade:    budget.Used(),
				ToolInvocations:  invocations,
				InputTokens:      totalIn, OutputTokens: totalOut,
				EstimatedCostUSD: totalCostUSD, LatencyMs: totalLatencyMs,
			}, nil
		}

		assistantBlocks, toolUses := splitContentBlocks(resp.ContentBlocks)
		if len(assistantBlocks) > 0 {
			messages = append(messages, anthropic.NewAssistantMessage(assistantBlocks...))
		}

		if resp.StopReason != anthropic.StopReasonToolUse || len(toolUses) == 0 {
			// A response cut off by the output-token cap is never a
			// trustworthy final answer, whatever it says and whether or not
			// a tool ran earlier in the loop: the model was stopped
			// mid-thought, and there is no way to know whether the missing
			// remainder would have corrected, qualified, or completed
			// whatever text already exists. Caught live: "show me the day
			// with the most profit and why" returned "I'll need to check
			// each day's reconciliation... Let me pull all 14 days." as a
			// served answer — a truncated planning sentence with zero tool
			// calls, but not currency-shaped, so the check below never saw
			// it. Same philosophy as the MaxTurns-exhaustion path further
			// down: report inability plainly rather than return a partial
			// narration, checked first and unconditionally rather than
			// folded into the currency-specific heuristic.
			if resp.StopReason == anthropic.StopReasonMaxTokens {
				return &Result{
					IncompleteReason: "the model's response was cut off by the output-token limit before it finished — refusing rather than returning a truncated narration",
					ToolCallsMade:    budget.Used(),
					ToolInvocations:  invocations,
					InputTokens:      totalIn, OutputTokens: totalOut,
					EstimatedCostUSD: totalCostUSD, LatencyMs: totalLatencyMs,
				}, nil
			}

			// A final narrated answer: Principle I is enforced upstream by
			// what tools exist (no free-form computation tool) and by
			// systemPrompt's instruction to never state a number that
			// didn't come from a tool result — this package cannot itself
			// verify that a given sentence's numbers trace to a tool call,
			// but it never computes one either.
			//
			// One structural check IS available and cheap, though: this
			// interaction never made a single tool call (budget.Used()==0)
			// and collected zero provenance (orderedRefs empty), yet the
			// text states a currency-shaped figure. By this system's own
			// definition every number MUST come from a tool result
			// (Principle I) — a dollar amount with no tool call behind it
			// cannot be that, whatever the model claims. Refuse exactly
			// like the MaxTurns-exhaustion path below, rather than
			// returning it as an answer.
			//
			// Deliberately scoped to a CURRENCY-shaped answer rather than
			// "any zero-tool-call answer": systemPromptTemplate's own "Date
			// grounding" paragraph explicitly permits the model to state,
			// with no tool call, that a date falls outside the covered
			// range (an established fact, not something to verify
			// per-question) — see explain_test.go's
			// "question about data outside the dataset period" case, which
			// legitimately makes zero tool calls and must keep answering
			// rather than being refused here.
			//
			// IncompleteReason here is not a debug log — internal/httpapi
			// (HandleAsk) forwards it verbatim as AskResponse.RefusalReason,
			// and the frontend (AskPage.tsx) renders it verbatim as the chat
			// bubble's text. A live report caught the ORIGINAL wording here
			// doing exactly that: a restaurant owner asking a follow-up like
			// "how can I replicate it on other days?" saw "model stated a
			// currency-shaped figure without making any MCP tool call or
			// collecting any provenance — refusing rather than trusting a
			// number that cannot trace to the deterministic layer" verbatim
			// in their chat — internal component names ("MCP", "provenance",
			// "the deterministic layer") with no place in owner-facing copy.
			// This message must stay in that plain, owner-facing voice —
			// what happened, why, what to do next — the same discipline
			// internal/ambiguity's precheckRefusalReason and writerSystemPrompt
			// already hold refusal/clarification copy to; see
			// TestExplain_ZeroToolCallCurrencyAnswerIsRefused for the guard
			// against a regression back to internal vocabulary.
			if budget.Used() == 0 && len(orderedRefs) == 0 && looksLikeCurrencyAmount(resp.Text) {
				return &Result{
					IncompleteReason: "I can't back that up with real numbers right now, so I won't guess at a figure — a wrong number is worse than no answer. Ask again, or point me at one specific day or period, and I'll pull the exact reconciled numbers before answering.",
					ToolCallsMade:    budget.Used(),
					ToolInvocations:  invocations,
					InputTokens:      totalIn, OutputTokens: totalOut,
					EstimatedCostUSD: totalCostUSD, LatencyMs: totalLatencyMs,
				}, nil
			}
			// The zero-tool-call guard above is a STRUCTURAL check: did any
			// grounded computation happen at all. This one is the arithmetic
			// check it never was — does every money figure this sentence
			// STATES actually appear in what the tools RETURNED
			// (internal/answerverify). A model that calls get_daily_summary
			// correctly, is handed $374.75, and then narrates $999.99 — or
			// $374.57 — passes every other check in this file. It is the exact
			// shape of "a confidently wrong margin figure" CLAUDE.md calls
			// worse than a refusal, and until this call nothing caught it.
			//
			// Runs only when at least one tool result exists to check against:
			// with an empty allowed set every figure would "mismatch", which
			// is not a finding, it is an absence of evidence — and the
			// zero-tool-call case already has its own guard, with its own
			// wording, immediately above.
			if len(verifySources) > 0 {
				report := answerverify.Verify(resp.Text, verifySources)
				if len(report.Dates) > 0 {
					// Advisory only, deliberately: legitimate date phrasing
					// varies far more than money phrasing, so a refusal here
					// would cost real answers to catch a class of error
					// provenance already surfaces. Logged so the variance is
					// measurable if it ever becomes worth acting on.
					log.Printf("explain: date_validation_mismatch (advisory, not refused) dates=%d", len(report.Dates))
				}
				if report.Blocking() {
					log.Printf("explain: numeric_validation_failed — narrated figures not found in tool results: %s (tool calls made=%d, figures checked: currency=%d percent=%d)",
						report.Summary(), budget.Used(), report.CheckedCurrency, report.CheckedPercent)
					return &Result{
						IncompleteReason: answerverify.RefusalReason,
						ToolCallsMade:    budget.Used(),
						ToolInvocations:  invocations,
						InputTokens:      totalIn, OutputTokens: totalOut,
						EstimatedCostUSD: totalCostUSD, LatencyMs: totalLatencyMs,
					}, nil
				}
			}

			return &Result{
				AnswerText:      resp.Text,
				ProvenanceRefs:  orderedRefs,
				ToolCallsMade:   budget.Used(),
				ToolInvocations: invocations,
				InputTokens:     totalIn, OutputTokens: totalOut,
				EstimatedCostUSD: totalCostUSD, LatencyMs: totalLatencyMs,
			}, nil
		}

		toolResultBlocks := make([]anthropic.ContentBlockParamUnion, 0, len(toolUses))
		for _, tu := range toolUses {
			text, isError := e.callTool(ctx, tu, seenRefs, &orderedRefs)
			verifySources = append(verifySources, text)
			if !isError {
				invocations = append(invocations, ToolInvocation{Name: tu.Name, ResultJSON: text})
			}
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
		ToolInvocations:  invocations,
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
// currencyShapedPattern matches text that states an amount of money: a
// currency symbol immediately followed by a digit (e.g. "$1,234", "R$50"),
// or a bare decimal-cents number (e.g. "1234.56"). It deliberately does
// NOT match a plain calendar date like "2026-08-14" (dashes, not a decimal
// point) — see the zero-tool-call guard in Explain that relies on this
// distinction to keep allowing the model's documented no-tool-call
// "that date is outside our range" answers through unrefused.
var currencyShapedPattern = regexp.MustCompile(`[$\p{Sc}]\s?\d|\d[\d,]*\.\d{2}\b`)

// looksLikeCurrencyAmount reports whether text appears to state a money
// figure — see Explain's zero-tool-call guard (Finding 13: a narrated
// answer with zero tool calls and zero provenance stating a currency-shaped
// number cannot, by this system's own definition, have come from the
// deterministic layer).
func looksLikeCurrencyAmount(text string) bool {
	return currencyShapedPattern.MatchString(text)
}

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

Data grounding: the only data this product has spans %[1]s through %[2]s inclusive, and %[2]s is this product's own notion of "today" — never the real-world current date. Resolve "today", "this week", "last week", and any date given without a year against %[2]s and the %[1]s..%[2]s range, exactly as an upstream ambiguity check already would have. "This month" is the calendar month containing %[2]s, truncated at %[2]s (the 1st of that month through %[2]s, not a trailing 30-day window); "last month" is the FULL prior calendar month — the same calendar-month convention this product's period-comparison and platform-trend pages already use, so a chat answer about "this/last month" never disagrees with what those pages show. Never state, in your answer or in a tool call argument, a year outside what %[1]s..%[2]s spans.

Tool routing for subjective-sounding language: this product's tool set already defines several evaluative words deterministically — call the matching tool directly rather than asking the user what the word means or what threshold to use:
- "underperforming", "losing money", "worst promotions" → list_negative_roi_promotions (a computed-negative-ROI list, not something to define ad hoc).
- "best/worst day", "period totals", "averages" → get_period_totals (it ranks every day by margin and totals the period in one call — never assemble this yourself from repeated get_daily_summary calls; see the next rule).
An upstream ambiguity check already lets exactly these questions through as answerable for this same reason — never second-guess that by asking the user to define the term yourself here.

Rules, no exceptions:
- A question can mix a data-answerable core (what happened on a day or period, which figures explain a result, what changed) with a request for operational advice this product's tools were never built to give (staffing, menu, pricing, marketing strategy, or any other business decision). When that happens: ALWAYS call the relevant tool(s) and answer the data-answerable part in full first — an advice-shaped wrapper around an answerable question ("how do I replicate/improve/fix this margin?") is never a reason to withhold the data you do have. Then, in one or two plain sentences, state directly that recommending staffing, menu, or other operational/business decisions isn't something this tool computes or has data for — state that boundary plainly, without hedging it into a maybe, and without letting it stop you from giving the data-grounded answer first. EXCEPTION: when the user turn carries an explicit upstream note that a separate advisor step will handle the advice part of this reply, present the computed data only — neither add recommendations of your own nor state that advice cannot be given; that note means the advice part is being handled, in this same reply, by a step grounded in your own tool results.
- This applies just as much on a follow-up question as on a first question. If the conversation history handed to you already contains a figure from an earlier turn (e.g. an earlier answer stated a day's margin, and the new question asks how to "replicate" or "repeat" or "hit that again"), that earlier text is background, not something you may restate as this turn's answer from memory. Call the relevant tool again in THIS turn and answer the data-answerable part from that fresh result — never narrate a number you only know because you saw it earlier in the conversation, even if you are confident it is the same number a tool would return now.
- Every number you state MUST come directly from a tool call result. Never perform arithmetic on tool results yourself (no adding, subtracting, or averaging numbers across multiple tool calls) — if you need a combined or comparative figure, call the tool that computes it (e.g. get_margin_delta for a comparison between two periods, or compare_platform_economics for any question comparing iFood's and Just Eat Takeaway's costs/rates — e.g. "which platform costs me more in commission?"), rather than computing it yourself from two get_daily_summary or get_promotion_roi calls. This rule is about arithmetic ACROSS separate tool calls, not about numbers already handed to you within a single result: get_daily_summary's total_delivery_gross_sales field is already the deterministic sum of that day's delivery-platform sales (gross_sales_by_source minus "pos") — when a question asks for delivery revenue, state that field directly rather than adding gross_sales_by_source entries yourself or omitting the combined figure. If no single tool computes the combined figure you need, say plainly that this isn't something the product can compute yet — do NOT call the same tool repeatedly per day (or per period) to assemble an aggregate yourself; that burns the turn/tool-call budget trying to simulate a tool that doesn't exist, and the result would be exactly the arithmetic-across-tool-calls this rule forbids.
- If a tool returns a typed error (e.g. "no_data", "insufficient_data", "invalid_input"), tell the user plainly what is missing or why the request could not be fulfilled. Never estimate, extrapolate, or state a plausible-sounding number in place of a refused tool call.
- Never state that a specific named entity — a campaign, promotion, or supplier — is missing or "not in the data" without first calling the relevant tool (e.g. get_promotion_roi) and confirming via its actual no_data error. A claim that a named entity doesn't exist must always be grounded in a real tool response, never asserted from assumption or recollection — asserting absence without checking is exactly as much a fabrication as inventing a number. (This does not apply to the date range and scope already stated in "Data grounding" above — you may state directly, without a tool call, that a date is outside %[1]s..%[2]s, since that boundary is already an established fact, not something to verify per-question.)
- get_promotion_roi's campaign_id lookup tolerates a shortened form (e.g. "LUNCHFIX") or a full human-readable campaign name (e.g. "Banner Ad - Lunch Fix Menu (JET-CAMP-LUNCHFIX)") as well as the exact id — pass whatever reference the user gave directly as campaign_id rather than declining to call the tool because the text isn't an exact id, and rather than inventing your own guess at the id. Only report a campaign as unavailable after the tool itself returns no_data for it.
- Cite where each number came from (which date, which period, which file/rows if asked) using the tool result's own fields — never invent a citation.
- If you are not sure a question is fully answerable from what the tools return, say so rather than guessing.
- Keep answers concise and in plain language suitable for a busy restaurant owner, not a data analyst.
- Write like a steward who's on the owner's side, not a report generator: a brief, warm opening acknowledgment is welcome (e.g. "Nice — Tuesday was a strong day.") before the figures, and a plain-language read of what a number means for the business is welcome after them. Warmth is about phrasing and framing only — it never changes, hedges, or founds itself on a number the tools didn't return, and it never adds a sentence longer than the number itself warrants.`

// buildSystemPrompt substitutes the real data date range into
// systemPromptTemplate (see internal/ambiguity.buildSystemPrompt, which
// this mirrors).
func buildSystemPrompt(dataStart, dataEnd string) string {
	return fmt.Sprintf(systemPromptTemplate, dataStart, dataEnd)
}
