// Package advisor implements the on-demand half of
// specs/009-business-insight-advisor: ONE bounded Claude Sonnet 5 call
// (llmclient.ModelBusinessInsight — see cost.go's doc comment for the
// model-choice rationale) that turns a deterministically-derived insight
// kind plus the exact tool-result JSON that triggered it into a short
// piece of general, best-practice advice.
//
// The deterministic/probabilistic split (Constitution Principle I) runs
// through the MIDDLE of the advisor feature, and this package is entirely
// on the probabilistic side of that line: it decides NOTHING. Whether an
// insight exists, what kind it is, and whether the posted data actually
// supports it are all decided in plain Go by internal/httpapi
// (deriveBusinessInsightTeaser) before this package is ever called — and
// re-verified there against the posted payload, so no client can talk
// this package into advising on a pattern the data does not show.
//
// This package never touches Postgres and never imports internal/storage
// — the same discipline internal/ambiguity and internal/paraphrase
// document: it carries a prompt to the model and returns text plus the
// token/cost/latency figures the caller hands to its own instrumentation
// (the dedicated business_insight_interaction ledger, written by
// internal/httpapi, never here).
//
// # What the prompts are grounded in (Sourced vs. Judgment)
//
// Each kind's guidance below embeds real, published industry practice
// researched for this spec — full source list in
// specs/009-business-insight-advisor/plan.md: delivery-marketplace
// commission tiers and ranges (OPA! Link 2025: DoorDash Basic/Plus/
// Premier at 15/25/30%, Uber Eats 15-30%, Grubhub 15-25% plus marketing;
// CloudKitchens 2025: 15-30% typical) and the documented existence of
// commission negotiation (Orders.co); payout-reconciliation failure modes
// and dispute windows (Voosh.ai's multi-unit reconciliation guide:
// platform-issued refunds/chargebacks deducted from later payouts,
// marketing deductions, phantom fees, dispute windows often 14-30 days);
// and par-level/ordering-cadence cost practice (Restaurant365, Toast).
// Nothing in these prompts asserts a statistic the research did not
// actually surface — one widely-quoted figure (a "~52% take-home on
// discounted orders" claim) was found but could not be verified against
// its original source, so it is deliberately absent.
//
// # The non-negotiable grounding rules (spec FR-011)
//
// The system prompt hard-forbids the two failure modes that would break
// this product's trust architecture: stating a "fact" about this specific
// restaurant that is not literally present in the JSON it was shown, and
// fabricating statistics or named sources. The advice is explicitly
// framed as general practice connected to the computed pattern — the UI
// then labels it as an AI suggestion, never as a computed figure.
package advisor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/llmclient"
)

// The five insight kinds this skill can advise on — the same closed set
// internal/httpapi derives deterministically and migration 000010's CHECK
// constraint enforces at the database. Defined here (the advice skill's
// own vocabulary) and referenced by internal/httpapi, so the trigger
// layer, this package, and the schema can never drift apart silently.
const (
	KindDiscrepancyPattern     = "discrepancy_pattern"
	KindNegativePromoROI       = "negative_promo_roi"
	KindHighCommission         = "high_commission"
	KindDayOfMonthExpenseSpike = "day_of_month_expense_spike"
	KindMarginDecline          = "margin_decline"
)

// KnownKind reports whether kind is one of the five insight kinds.
func KnownKind(kind string) bool {
	_, ok := kindGuidance[kind]
	return ok
}

// MaxOutputTokens bounds the advice response. The prompt asks for 120-180
// words (roughly 160-240 tokens); 400 leaves honest headroom without
// letting a runaway response bill unbounded output.
const MaxOutputTokens = 400

// ErrUnknownKind is returned by Advise for a kind this package has no
// guidance for — refusing rather than improvising a prompt (Constitution
// Principle II applied to prompt selection).
var ErrUnknownKind = errors.New("advisor: unknown insight kind")

// ErrNoToolResults is returned by Advise when there is nothing to ground
// the advice in — advice with no computed pattern behind it is exactly
// the ungrounded content this feature exists to rule out.
var ErrNoToolResults = errors.New("advisor: no tool results to ground the advice in")

// ToolResult is one already-computed MCP tool invocation, as the client
// posted it back — the same {name, raw JSON} pair explain.ToolInvocation
// carries, re-declared here so this package does not import
// internal/explain for two string fields.
type ToolResult struct {
	Name       string
	ResultJSON string
}

// Advice is one advice call's outcome plus the token/cost/latency figures
// the caller hands to its own instrumentation — this package computes
// them but never persists them (the same split as ambiguity.Decision and
// paraphrase.Decision).
type Advice struct {
	Text string

	InputTokens      int64
	OutputTokens     int64
	EstimatedCostUSD float64
	LatencyMs        int64
}

// Advisor wraps the shared llmclient.Client to run this project's
// business-insight advice call.
type Advisor struct {
	client *llmclient.Client
}

// New constructs an Advisor over client (internal/llmclient, shared with
// internal/ambiguity, internal/explain, and internal/paraphrase — one
// client, one timeout policy, one instrumentation discipline for every
// model call this project makes).
func New(client *llmclient.Client) *Advisor {
	return &Advisor{client: client}
}

// Advise runs one Claude Sonnet 5 call for the given kind, grounded in
// results, and returns the advice text plus its real measured usage.
//
// Error contract: (nil, err) on EVERY failure path — an unknown kind,
// empty grounding, a transport/API error, a model refusal, or an empty
// reply — mirroring ambiguity.Gate.Classify's documented contract, which
// internal/httpapi's ask handler already describes as "no partial
// Decision to pull tokens/cost out of and log". A refusal or empty reply
// here is a theoretical edge (this prompt asks for general business
// guidance over the owner's own numbers), and the trade-off of not
// logging its token cost is accepted the same way it already is for the
// gate, rather than inventing a half-populated ledger row with no advice
// text in it.
func (a *Advisor) Advise(ctx context.Context, kind string, results []ToolResult) (*Advice, error) {
	guidance, ok := kindGuidance[kind]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKind, kind)
	}
	if len(results) == 0 {
		return nil, ErrNoToolResults
	}

	resp, err := a.client.CreateMessage(ctx, llmclient.MessageRequest{
		Model:     llmclient.ModelBusinessInsight,
		System:    baseSystemPrompt + "\n\n" + guidance,
		MaxTokens: MaxOutputTokens,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(composeUserMessage(kind, results))),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("advisor: advise: %w", err)
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

// composeUserMessage renders the insight kind plus every grounding tool
// result into the single user turn the system prompt advises over. The
// JSON travels verbatim — never summarized or re-computed here — so the
// only restaurant-specific facts the model can truthfully restate are the
// ones the deterministic layer actually computed. Exported logic kept as
// a plain function so it can be unit-tested with hand-crafted inputs, the
// same pattern paraphrase.composeUserMessage uses.
func composeUserMessage(kind string, results []ToolResult) string {
	var b strings.Builder
	b.WriteString("Insight kind: ")
	b.WriteString(kind)
	b.WriteString("\n\nComputed tool results this insight was triggered by:\n")
	for _, r := range results {
		b.WriteString("\n--- ")
		b.WriteString(r.Name)
		b.WriteString(" ---\n")
		b.WriteString(r.ResultJSON)
		b.WriteString("\n")
	}
	return b.String()
}

// baseSystemPrompt is the shared safety-and-scope base every kind's
// guidance is appended to. The hard rules here ARE the feature (spec
// FR-011): the one thing that must never happen is this call inventing a
// restaurant-specific "fact" or a statistic and having it land on screen
// styled as anything other than a general suggestion.
const baseSystemPrompt = `You are the business-advisor skill of a restaurant margin-reconciliation copilot used by one independent restaurant owner. The JSON you are shown was computed deterministically from the restaurant's own reconciled sales, commission, refund, cost, and promotion data. Your job is to connect the ONE specific computed pattern you are shown to general, widely-documented restaurant-industry practice, and suggest what an owner in this situation typically considers doing next.

Hard rules, in priority order:
1. NEVER state a fact about this specific restaurant that is not literally present in the JSON you were given. You do not know its menu, location, contracts, staffing, platforms beyond those named in the data, or anything about its history beyond these figures.
2. NEVER invent statistics, percentages, dollar amounts, study names, or source names. You may restate a figure that appears in the JSON, exactly as it appears there, to anchor a point.
3. Frame everything as general practice ("restaurants in this situation typically...", "a common next step is..."), connected explicitly to the computed pattern — never as a diagnosis or a guarantee about this business.
4. Be honest about uncertainty: these are options to weigh, and the owner knows their business better than this data does.
5. Keep it short and immediately usable: 120-180 words, 3-5 concrete actions, plain language, no markdown headers or emphasis, no preamble — start directly with the first point.`

// kindGuidance is the per-kind half of the system prompt. Each entry
// embeds the researched practice plan.md sources — and nothing beyond it.
var kindGuidance = map[string]string{
	KindDiscrepancyPattern: `This owner's reconciliation flagged at least one real discrepancy (a duplicate order, a missing source, a commission mismatch, or an anomaly). Relevant documented practice: the most common causes of POS-vs-payout discrepancies are platform-issued refunds and chargebacks silently deducted from a later payout, marketing/promotion charges deducted the same way, and erroneous or leftover fees; platform dispute windows are commonly short (often 14-30 days), which is why the standard remediation is reconciling daily rather than at month-end, cross-referencing refunds and cancellations against the platform's own reports, disputing invalid deductions promptly, and keeping the supporting records (timestamps, receipts). Connect your suggestions to the specific flag types actually present in the JSON.`,

	KindNegativePromoROI: `At least one of this owner's promotions is flagged negative-ROI: its attributed incremental revenue did not cover its spend. Relevant documented practice: promotion losses usually come from paying for orders that would have happened anyway (weak incrementality), discount depth that exceeds the margin on the incremental orders, and promo fees stacking on top of the platform's regular commission. Common next steps owners weigh: pausing or ending the flagged campaign; restructuring it with a shallower discount or a minimum order value; targeting offers at new customers only rather than everyone; and judging any replacement on attributed incremental orders and repeat rate, not gross sales during the promo. Anchor your points to the flagged campaign's own spend/revenue figures in the JSON.`,

	KindHighCommission: `One or more of this owner's delivery platforms shows an effective commission rate at the high end of what marketplaces typically charge. Relevant documented practice: major delivery marketplaces publish tiered commission plans spanning roughly 15-30% (entry tiers around 15% with less visibility, premium tiers at 25-30% with more), and rates above the entry tier usually mean a chosen plan level or added services rather than an immovable cost; negotiating rates and reviewing plan tier is a real, documented practice, with order volume as the owner's main leverage. Common next steps owners weigh: confirming which plan tier and add-ons the current rate reflects; asking whether the visibility a premium tier buys is actually producing measurable extra orders; and steering repeat customers toward the lower-cost channels already visible in their own data. Use the per-platform effective rates in the JSON to anchor this.`,

	KindDayOfMonthExpenseSpike: `This owner's expenses cluster on a particular day of the month, notably above the typical day. Relevant documented practice: recurring same-day-of-month cost concentration usually traces to fixed billing and delivery schedules — supplier deliveries, subscription and service charges, bulk stock-up orders — and the standard tools for smoothing it are par levels (stock sized to reach the next delivery plus a small buffer, so ordering becomes subtraction rather than judgment), a consistent counting-and-ordering cadence, reorder points tuned per ingredient class, and renegotiating delivery timing with suppliers so big cost days do not pile onto weak sales days. Suggest the owner identify WHICH recurring charges land on the spike day shown in the JSON before changing anything — the data shows the pattern, not its cause.`,

	KindMarginDecline: `This owner's margin declined against the comparison period (or the period ran at a real loss). Relevant documented practice: a margin move is only actionable once decomposed — the usual suspects are commission and promo cost growth outpacing sales, refund spikes, input-cost increases, and sales mix shifting toward lower-margin channels. Common next steps owners weigh: comparing each cost line between the two periods to find the biggest mover before acting on any of them; checking whether flagged discrepancies or refunds explain the gap; and reviewing promotion spend in the declining period. This product's own tools can compute each of those comparisons deterministically — suggest asking those follow-up questions rather than guessing at the cause. Anchor to the two periods' real margin figures in the JSON.`,
}
