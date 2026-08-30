// question_advice.go implements the question-initiated half of
// specs/011-inline-grounded-advice: the SAME bounded, instrumented advice
// call Advise makes for the five closed-set teaser kinds, reached instead
// by the owner's own explicit advice-shaped question ("how can I improve
// my margin overall?", "should I push delivery or dine-in?"), and
// grounded in whatever tool results the answering interaction actually
// computed rather than in one pre-classified pattern.
//
// The deterministic/probabilistic split holds exactly as advisor.go's
// package doc describes it — this file decides NOTHING: whether the
// question wants advice is internal/ambiguity's typed signal
// (Decision.AdviceRequested), whether a grounded answer exists is
// internal/explain's tool loop, and WHICH researched-practice guidance
// enters the prompt is a plain Go map lookup over the NAMES of the tools
// that actually ran (BuildQuestionSystemPrompt). The model selects no
// guidance, computes no figure, and sees no data the deterministic layer
// didn't compute for this very answer.
//
// # What the guidance below is grounded in (Sourced vs. Judgment)
//
// Full source list and tagging in specs/011-inline-grounded-advice/
// plan.md; the five 009 kinds' already-researched material (commission
// tiers and negotiation, payout-reconciliation mechanics, par-level and
// ordering-cadence practice) is reused where a tool overlaps its topic.
// New for this spec: prime cost as the standard margin-decomposition lens
// (Restaurant365's prime-cost guidance: ~60% of sales for a sustainable
// operation, full-service typically 60-65%, quick-service 55-60%, tracked
// weekly); menu engineering as the standard sales-mix practice (Kasavana
// & Smith, Menu Engineering, Michigan State University, 1982 — the
// popularity-by-contribution-margin matrix and its standard actions); and
// direct-channel steering for delivery-mix questions (Toast's and
// ChowNow's online-ordering guidance: a restaurant's own ordering channel
// carries no marketplace commission, and the documented steering tactics
// are visibility plus repeat/loyalty incentives). Vendor marketing
// figures that could not be independently checked ("3x more profit",
// "56% more direct sales") are deliberately absent, the same discipline
// that keeps 009's unverified "~52% take-home" figure out of every
// prompt. Which guidance section maps to which tool name is this
// project's own labeled editorial judgment — no source prescribes it.
package advisor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/llmclient"
)

// KindQuestionAdvice is the business_insight_interaction ledger kind for
// an inline, question-initiated advice call (migration 000013). It is
// deliberately NOT in kindGuidance, so KnownKind reports false for it:
// POST /api/business-insight's closed five-kind teaser validation must
// keep rejecting it — this kind is a ledger label, never a tappable or
// client-postable insight kind.
const KindQuestionAdvice = "question_advice"

// ErrEmptyQuestion is returned by AdviseOnQuestion for blank input —
// mirroring ambiguity.ErrEmptyQuestion: nothing to advise on, and the
// call would be a wasted spend.
var ErrEmptyQuestion = errors.New("advisor: question is empty")

// AdviseOnQuestion runs one bounded Claude Sonnet 5 call answering the
// advice-shaped part of the owner's own question, grounded exclusively in
// results — the tool invocations the answering interaction actually made.
//
// Error contract: (nil, err) on EVERY failure path, identical to Advise
// (see its doc comment) — an empty question, empty grounding, a
// transport/API error, a model refusal, or an empty reply. The caller
// (internal/httpapi) degrades a failure to serving the computed answer
// without advice, never to failing the request (spec FR-009).
func (a *Advisor) AdviseOnQuestion(ctx context.Context, question string, results []ToolResult) (*Advice, error) {
	if strings.TrimSpace(question) == "" {
		return nil, ErrEmptyQuestion
	}
	if len(results) == 0 {
		return nil, ErrNoToolResults
	}

	resp, err := a.client.CreateMessage(ctx, llmclient.MessageRequest{
		Model:     llmclient.ModelBusinessInsight,
		System:    BuildQuestionSystemPrompt(results),
		MaxTokens: MaxOutputTokens,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(composeQuestionUserMessage(question, results))),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("advisor: advise on question: %w", err)
	}
	if resp.Refused {
		return nil, fmt.Errorf("advisor: model refused (category %q)", resp.RefusalCategory)
	}

	text := strings.TrimSpace(resp.Text)
	if text == "" {
		return nil, errors.New("advisor: model returned empty advice text")
	}

	cost, err := resp.EstimatedCostUSD(llmclient.ModelBusinessInsight)
	if err != nil {
		return nil, fmt.Errorf("advisor: %w", err)
	}

	return &Advice{
		Text:             text,
		InputTokens:      resp.InputTokens,
		OutputTokens:     resp.OutputTokens,
		EstimatedCostUSD: cost,
		LatencyMs:        resp.Latency.Milliseconds(),
	}, nil
}

// BuildQuestionSystemPrompt assembles this path's system prompt from the
// grounding that actually exists: the fixed safety base (the same
// non-fabrication rules Advise's baseSystemPrompt enforces, restated for
// a question-shaped grounding) plus one researched-practice section per
// DISTINCT tool name present in results, in guidanceOrder's fixed
// canonical order. Selection is a deterministic function of which typed
// tools the interaction ran — the model neither chooses nor sees any
// guidance for data it was not shown. kindGuidance (the five 009
// templates) is never consulted here: those are teaser-kind prompts, and
// this path has no kind.
//
// Exported (unlike composeUserMessage) because its output IS this spec's
// central grounding claim — tests assert its exact assembly behavior.
func BuildQuestionSystemPrompt(results []ToolResult) string {
	var b strings.Builder
	b.WriteString(questionBaseSystemPrompt)

	seen := map[string]bool{}
	for _, r := range results {
		seen[r.Name] = true
	}
	for _, name := range guidanceOrder {
		if !seen[name] {
			continue
		}
		b.WriteString("\n\n")
		b.WriteString(toolGuidance[name])
	}
	return b.String()
}

// composeQuestionUserMessage renders the owner's question plus every
// grounding tool result into the single user turn — the same
// travels-verbatim discipline composeUserMessage applies for the teaser
// path: the JSON is never summarized or re-computed here, so the only
// restaurant-specific facts the model can truthfully restate are the ones
// the deterministic layer actually computed for this answer.
func composeQuestionUserMessage(question string, results []ToolResult) string {
	var b strings.Builder
	b.WriteString("The owner's question (already answered with the computed data below; you handle only its advice-shaped part):\n")
	b.WriteString(question)
	b.WriteString("\n\nComputed tool results this answer was grounded in:\n")
	for _, r := range results {
		b.WriteString("\n--- ")
		b.WriteString(r.Name)
		b.WriteString(" ---\n")
		b.WriteString(r.ResultJSON)
		b.WriteString("\n")
	}
	return b.String()
}

// questionBaseSystemPrompt is this path's fixed safety base. Hard rules 1
// and 2 are baseSystemPrompt's rules 1 and 2 VERBATIM (spec FR-004) — the
// two lines that make fabrication structurally out of bounds — with the
// framing generalized from "the ONE computed pattern you are shown" to
// "the owner's own question plus the data gathered to answer it", and one
// added rule for the part of a question the shown data cannot ground.
const questionBaseSystemPrompt = `You are the business-advisor skill of a restaurant margin-reconciliation copilot used by one independent restaurant owner. The owner asked a question that includes a request for advice, and the JSON you are shown was computed deterministically from the restaurant's own reconciled sales, commission, refund, cost, and promotion data to answer it. Your job is to connect what that computed data actually shows to general, widely-documented restaurant-industry practice, and suggest what an owner in this situation typically considers doing next — addressed to what the owner actually asked.

Hard rules, in priority order:
1. NEVER state a fact about this specific restaurant that is not literally present in the JSON you were given. You do not know its menu, location, contracts, staffing, platforms beyond those named in the data, or anything about its history beyond these figures.
2. NEVER invent statistics, percentages, dollar amounts, study names, or source names. You may restate a figure that appears in the JSON, exactly as it appears there, to anchor a point. You may state a published industry practice or benchmark ONLY as it is given to you in the guidance sections below — never from memory.
3. If part of the question asks about something the data you were shown cannot ground (staffing, hiring, wages, menu contents, suppliers or platforms not named in the data), say so plainly in one sentence — this product only advises from its own computed data — and advise only on the grounded part. Never improvise around missing data.
4. Frame everything as general practice ("restaurants in this situation typically...", "a common next step is..."), connected explicitly to the computed figures — never as a diagnosis or a guarantee about this business.
5. Be honest about uncertainty: these are options to weigh, and the owner knows their business better than this data does.
6. Keep it short and immediately usable: 120-180 words, 3-5 concrete actions, plain language, no markdown headers or emphasis, no preamble — start directly with the first point.`

// guidanceOrder fixes the canonical order guidance sections are appended
// in — deterministic assembly regardless of the order tools happened to
// run in. It lists every tool internal/mcptools registers; a tool name
// outside this list (impossible today) simply contributes no section,
// never an improvised one.
var guidanceOrder = []string{
	"get_daily_summary",
	"get_period_totals",
	"get_margin_delta",
	"compare_platform_economics",
	"get_promotion_roi",
	"list_negative_roi_promotions",
	"list_discrepancies",
	"get_expense_pattern_by_day_of_month",
}

// toolGuidance is the per-tool researched-practice half of the prompt —
// selected by BuildQuestionSystemPrompt from the tools that actually ran,
// never by the model. Sources per section in
// specs/011-inline-grounded-advice/plan.md (new research) and
// specs/009-business-insight-advisor/plan.md (reused research); the
// mapping of section to tool name is this project's own labeled judgment.
var toolGuidance = map[string]string{
	"get_daily_summary": `Guidance for daily-summary data (only if relevant to the question): a single day's reconciled figures are most useful compared against the owner's own typical day rather than judged alone. Documented practice: day-level review is exactly what daily reconciliation exists for — checking whether commissions, refunds, and input costs on a given day moved out of line with its sales, and whether any discrepancy flags explain an odd figure. Suggest comparisons this product can actually compute (another day, a period average) rather than external benchmarks, and anchor every point to the day's own figures in the JSON.`,

	"get_period_totals": `Guidance for period-totals and margin-level questions: the standard first lens owners apply to "how do I improve my margin" is prime cost — food and beverage cost plus labor as a share of sales. Published guidance (Restaurant365's prime-cost material) puts a sustainable operation's prime cost around 60% of sales, with full-service restaurants typically 60-65% and quick-service 55-60%, and recommends tracking it weekly rather than at month-end. This product computes the input-cost and commission side of that picture, not labor. The standard sales-mix practice is menu engineering (Kasavana & Smith, Michigan State University, 1982): rank items by popularity and contribution margin, protect the popular-and-profitable, re-price or re-cost the popular-but-thin, promote the profitable-but-slow, retire the neither. This product's data ranks days and channels, not menu items — suggest the owner apply that lens with their own item data. Anchor every point to the period's own computed totals: which cost lines (commissions, refunds, input costs) are largest relative to sales in the JSON.`,

	"get_margin_delta": `Guidance for margin-comparison questions: a margin move is only actionable once decomposed — the documented usual suspects are commission and promo costs growing faster than sales, refund spikes, input-cost increases, and sales mix shifting toward lower-margin channels. Common next steps owners weigh: comparing each cost line between the two periods to find the biggest mover before acting on any of them; checking whether flagged discrepancies or refunds explain the gap; and reviewing promotion spend in the weaker period. This product's own tools compute each of those comparisons deterministically — suggest asking those follow-up questions rather than guessing at the cause. Anchor to the two periods' real computed figures in the JSON.`,

	"compare_platform_economics": `Guidance for channel-mix and platform-cost questions: major delivery marketplaces publish tiered commission plans spanning roughly 15-30% (entry tiers around 15% with less visibility, premium tiers at 25-30% with more), and rates above the entry tier usually reflect a chosen plan level or added services; reviewing the plan tier and negotiating — with order volume as the owner's main leverage — is a real, documented practice. On mix: orders on a restaurant's own ordering channel carry no marketplace commission (Toast's and ChowNow's online-ordering guidance), so the documented practice for "delivery vs. direct" is not abandoning marketplaces (they bring discovery and new customers) but steering REPEAT customers toward the direct channel — visibility on packaging and signage, and loyalty or repeat-order incentives. Use the per-channel sales, commission, and effective-rate figures in the JSON to anchor which channel currently costs what.`,

	"get_promotion_roi": `Guidance for promotion questions: documented practice is to judge a campaign on attributed incremental revenue against its full cost, not on gross sales during the promo. Promotion losses usually come from paying for orders that would have happened anyway (weak incrementality), discount depth exceeding the margin on the incremental orders, and promo fees stacking on the platform's regular commission. Common next steps owners weigh: pausing a campaign the data flags as negative; restructuring with a shallower discount or a minimum order value; and targeting offers at new customers rather than everyone. Anchor to the campaign's own computed spend, attributed revenue, and ROI in the JSON — and if the JSON says attribution is not possible for a campaign, say that plainly rather than treating its ROI as known.`,

	"list_negative_roi_promotions": `Guidance for money-losing promotions: each listed campaign is one the deterministic layer computed as negative-ROI — its attributed incremental revenue did not cover its spend. Documented practice for the decision that follows: pause or end the flagged campaign; restructure with a shallower discount or a minimum order value; target new customers only; and judge any replacement on attributed incremental orders and repeat rate, not gross sales during the promo. Anchor each point to the flagged campaigns' own spend and revenue figures in the JSON.`,

	"list_discrepancies": `Guidance for discrepancy questions: the documented most common causes of POS-vs-payout discrepancies are platform-issued refunds and chargebacks silently deducted from a later payout, marketing/promotion charges deducted the same way, and erroneous or leftover fees; platform dispute windows are commonly short (often 14-30 days), which is why the standard remediation is reconciling daily rather than at month-end, cross-referencing refunds and cancellations against the platform's own reports, disputing invalid deductions promptly, and keeping supporting records. Connect suggestions to the specific flag types actually present in the JSON.`,

	"get_expense_pattern_by_day_of_month": `Guidance for expense-pattern questions: recurring same-day-of-month cost concentration usually traces to fixed billing and delivery schedules — supplier deliveries, subscriptions, bulk stock-up orders — and the documented tools for smoothing it are par levels (stock sized to reach the next delivery plus a small buffer), a consistent counting-and-ordering cadence, reorder points tuned per ingredient class, and renegotiating delivery timing with suppliers so big cost days do not pile onto weak sales days. Suggest identifying WHICH recurring charges land on the pattern's peak day in the JSON before changing anything — the data shows the pattern, not its cause.`,
}
