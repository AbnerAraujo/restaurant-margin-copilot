package httpapi

// Inline grounded advice (specs/011-inline-grounded-advice): the
// question-initiated second path into internal/advisor, running INSIDE
// the /api/ask flow. Where business_insight.go's teaser path is
// "Go detected a pattern the owner didn't ask about → opt-in tap →
// advice", this path is "the owner explicitly asked for a suggestion →
// the normal narration answers the data part → ONE advisor call runs
// inline, grounded in the tool results that narration just computed".
//
// The deterministic gates stay in Go, mirroring the teaser path's
// discipline exactly: WHETHER this runs is decided by two typed facts —
// the ambiguity gate's Decision.AdviceRequested signal and a successful
// narrated answer with at least one real tool invocation — never by a
// model deciding it would like to advise. The grounding is exclusively
// result.ToolInvocations, the same JSON the answer's provenance already
// covers; nothing is fetched specially for the advice and nothing is
// accepted from the client.
//
// Failure discipline (spec FR-009): a failed advice call degrades to
// serving the computed answer unchanged — the owner already paid for and
// received a real answer, and a broken suggestion must never take it
// away. The failure is logged loudly instead.

import (
	"context"
	"log"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/advisor"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/explain"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/llmclient"
)

// QuestionAdviser is internal/advisor's question-initiated advice call as
// HandleAsk needs it — an interface for the same reason Classifier,
// Narrator, and business_insight.go's Adviser are: tests must COUNT and
// CONTROL advice calls without the live Anthropic API. *advisor.Advisor
// satisfies it directly.
type QuestionAdviser interface {
	AdviseOnQuestion(ctx context.Context, question string, results []advisor.ToolResult) (*advisor.Advice, error)
}

// InlineAdviceView is AskResponse.Advice's wire shape: the suggestion
// text, the SAME standing disclaimer the teaser path carries
// (BusinessInsightDisclaimer — one wording, both paths), and the call's
// real measured cost. The cost also appears as its own entry in
// AskResponse.Interactions — the running cost panel reads Interactions,
// and an advice call must never look free (Constitution Principle VI).
type InlineAdviceView struct {
	Text        string          `json:"text"`
	Disclaimer  string          `json:"disclaimer"`
	Interaction CostInteraction `json:"interaction"`
}

// maybeAttachInlineAdvice runs the inline advisor call when — and only
// when — every deterministic precondition holds: the gate flagged the
// question as advice-requesting, an adviser is configured, and the
// narrated answer carries at least one successful tool invocation to
// ground the advice in (no grounding, no advice — advisor.ErrNoToolResults'
// rule applied before spending anything). On success it attaches
// resp.Advice, appends the call's cost to resp.Interactions, and writes
// the dedicated business_insight_interaction ledger row (kind
// question_advice — migration 000013). On any failure it logs and leaves
// resp exactly as it was.
func (deps Deps) maybeAttachInlineAdvice(ctx context.Context, resp *AskResponse, resolvedQuestion string, invocations []explain.ToolInvocation) {
	if deps.QuestionAdviser == nil || len(invocations) == 0 {
		return
	}

	toolResults := make([]advisor.ToolResult, 0, len(invocations))
	for _, inv := range invocations {
		toolResults = append(toolResults, advisor.ToolResult{Name: inv.Name, ResultJSON: inv.ResultJSON})
	}

	advice, err := deps.QuestionAdviser.AdviseOnQuestion(ctx, resolvedQuestion, toolResults)
	if err != nil {
		// advisor.AdviseOnQuestion's error contract is (nil, err) on every
		// failure path — like the teaser handler, there is no partial
		// Advice to pull tokens out of and log. The computed answer is
		// served unchanged (spec FR-009).
		log.Printf("httpapi: inline advice call failed (question=%q): %v — serving the answer without advice", resolvedQuestion, err)
		return
	}

	interaction := CostInteraction{
		ModelUsed:        llmclient.ModelBusinessInsight,
		InputTokens:      advice.InputTokens,
		OutputTokens:     advice.OutputTokens,
		EstimatedCostUSD: advice.EstimatedCostUSD,
		LatencyMs:        advice.LatencyMs,
	}
	resp.Advice = &InlineAdviceView{
		Text:        advice.Text,
		Disclaimer:  BusinessInsightDisclaimer,
		Interaction: interaction,
	}
	resp.Interactions = append(resp.Interactions, interaction)

	if deps.InsightStore != nil {
		logBusinessInsightOrWarn(ctx, deps.InsightStore, BusinessInsightRequest{
			Kind:      advisor.KindQuestionAdvice,
			ToolCalls: toToolCallViews(invocations),
		}, advice)
	} else {
		// A configured adviser with no store is a wiring defect worth
		// shouting about (Constitution Principle VI: an unlogged model
		// call is a real defect), not a reason to withhold the advice the
		// owner asked for.
		log.Printf("httpapi: inline advice ran but no InsightStore is configured — the call's ledger row was NOT written (question=%q)", resolvedQuestion)
	}
}
