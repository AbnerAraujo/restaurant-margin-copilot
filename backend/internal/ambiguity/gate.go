// Package ambiguity is the pre-processing gate CLAUDE.md and Constitution
// Principle II require: before any MCP tool call is made, classify the
// incoming question as answerable, ambiguous (needs either a clarifying
// question or an explicitly stated assumption), or unanswerable given the
// data this product actually has. It uses Claude Haiku 4.5 — a cheap
// classification task, not one that needs frontier reasoning
// (constitution v1.1.0, research.md's model-split rationale).
//
// This package has no import path to internal/mcptools or internal/storage
// at all (docs/technical-rfc.md's module architecture diagram): it
// classifies question text only and never touches real data itself. The
// one exception, deliberately kept narrow: New takes the data's actual
// min/max date range as plain strings (resolved once by the caller, via
// internal/storage.LoadDataDateRange, at process start) so the gate's
// system prompt can ground relative date language ("today", "this week", a
// year-less date) against what the data really covers, instead of either a
// hardcoded literal that can drift from the fixtures actually loaded, or
// the host machine's wall-clock date — see systemPrompt's "Date grounding"
// paragraph and docs/plan.md's mistakes log ("date-year grounding
// defect") for the bug this fixes. This package still never queries
// Postgres, never imports internal/storage, and never touches question
// data beyond the string it's asked to classify.
//
// It also never writes a QuestionInteraction row itself —
// internal/httpapi.HandleAsk does that, via internal/instrumentation, for
// every branch (including the refusal/clarification paths that never reach
// internal/explain) per tasks.md's T022/T026 note.
package ambiguity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/instrumentation"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/llmclient"
)

// ErrEmptyQuestion is returned by Classify for blank input — there is
// nothing to classify, and asking the model to classify an empty string
// would be a wasted call, not a real "ambiguous" case.
var ErrEmptyQuestion = errors.New("ambiguity: question is empty")

// MaxOutputTokens bounds the gate's response — it produces a small fixed
// JSON object (see systemPrompt), never free-form prose.
const MaxOutputTokens = 512

// Decision is the gate's classification of one question, plus the
// token/cost/latency figures the caller (internal/httpapi) hands straight
// to internal/instrumentation — this package computes them but never
// persists them itself (see package doc).
type Decision struct {
	Result instrumentation.GateResult

	// ClarifyingQuestion is non-empty when Result is Ambiguous and the gate
	// chose to ask, rather than assume.
	ClarifyingQuestion string
	// AssumptionStated is non-empty when Result is Ambiguous and the gate
	// chose to proceed with an explicitly stated assumption instead of
	// asking (spec FR-006: "or proceed while explicitly stating the
	// assumption taken"). Exactly one of ClarifyingQuestion/AssumptionStated
	// is set when Result is Ambiguous.
	AssumptionStated string
	// RefusalReason is non-empty when Result is Unanswerable.
	RefusalReason string

	InputTokens      int64
	OutputTokens     int64
	EstimatedCostUSD float64
	LatencyMs        int64
}

// Gate wraps an llmclient.Client to run this project's answerable /
// ambiguous / unanswerable classification.
type Gate struct {
	client       *llmclient.Client
	systemPrompt string
}

// New constructs a Gate over client (internal/llmclient, Phase 1).
// dataStart/dataEnd are the actual inclusive min/max date (YYYY-MM-DD) this
// product has reconciled data for — the caller (cmd/server/main.go)
// resolves these once via internal/storage.LoadDataDateRange at process
// start, so the gate's classification prompt reflects the real data
// actually loaded rather than a hardcoded literal baked into this package.
func New(client *llmclient.Client, dataStart, dataEnd string) *Gate {
	return &Gate{client: client, systemPrompt: buildSystemPrompt(dataStart, dataEnd)}
}

// gateResponse is the fixed JSON shape systemPrompt instructs the model to
// reply with — this package parses exactly this shape and refuses (returns
// an error, never a silent default) if the model's reply doesn't match it,
// the same "refuse rather than guess" discipline applied to the gate's own
// output, not just the product's numbers.
type gateResponse struct {
	Classification     string `json:"classification"`
	ClarifyingQuestion string `json:"clarifying_question"`
	AssumptionStated   string `json:"assumption_stated"`
	Reason             string `json:"reason"`
}

// Classify asks Claude Haiku 4.5 to classify question against the data
// this product actually has (see systemPrompt), returning a Decision the
// caller can act on directly.
func (g *Gate) Classify(ctx context.Context, question string) (*Decision, error) {
	if strings.TrimSpace(question) == "" {
		return nil, ErrEmptyQuestion
	}

	resp, err := g.client.CreateMessage(ctx, llmclient.MessageRequest{
		Model:     llmclient.ModelAmbiguityGate,
		System:    g.systemPrompt,
		MaxTokens: MaxOutputTokens,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(question)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("ambiguity: classify: %w", err)
	}

	cost, err := resp.EstimatedCostUSD(llmclient.ModelAmbiguityGate)
	if err != nil {
		return nil, fmt.Errorf("ambiguity: %w", err)
	}

	decision := &Decision{
		InputTokens:      resp.InputTokens,
		OutputTokens:     resp.OutputTokens,
		EstimatedCostUSD: cost,
		LatencyMs:        resp.Latency.Milliseconds(),
	}

	if resp.Refused {
		decision.Result = instrumentation.GateUnanswerable
		decision.RefusalReason = "model declined to classify this question (category: " + resp.RefusalCategory + ")"
		return decision, nil
	}

	parsed, err := parseGateResponse(resp.Text)
	if err != nil {
		return nil, fmt.Errorf("ambiguity: %w", err)
	}

	decision.Result = parsed.Result
	decision.ClarifyingQuestion = parsed.ClarifyingQuestion
	decision.AssumptionStated = parsed.AssumptionStated
	decision.RefusalReason = parsed.RefusalReason
	return decision, nil
}

// parseGateResponse is the pure, model-independent half of this package's
// logic (tasks.md T024's unit-test target): given the raw text a gate call
// produced, extract and validate the fixed JSON shape systemPrompt
// requires, or return a specific error explaining what was wrong with it.
// It never defaults to "answerable" on a parse failure — an
// unparseable/invalid model response is treated the same as any other
// unfulfillable case (Constitution Principle II), surfaced as an error the
// caller must handle rather than narrated past silently.
func parseGateResponse(text string) (*Decision, error) {
	raw := extractJSONObject(text)
	if raw == "" {
		return nil, fmt.Errorf("ambiguity: gate response did not contain a JSON object: %q", text)
	}

	var gr gateResponse
	if err := json.Unmarshal([]byte(raw), &gr); err != nil {
		return nil, fmt.Errorf("ambiguity: gate response is not valid JSON: %w (raw: %q)", err, raw)
	}

	result := instrumentation.GateResult(strings.ToLower(strings.TrimSpace(gr.Classification)))
	if !result.Valid() {
		return nil, fmt.Errorf("ambiguity: gate returned an invalid classification %q (must be answerable, ambiguous, or unanswerable)", gr.Classification)
	}

	d := &Decision{
		Result:             result,
		ClarifyingQuestion: strings.TrimSpace(gr.ClarifyingQuestion),
		AssumptionStated:   strings.TrimSpace(gr.AssumptionStated),
		RefusalReason:      strings.TrimSpace(gr.Reason),
	}

	switch result {
	case instrumentation.GateAmbiguous:
		if d.ClarifyingQuestion == "" && d.AssumptionStated == "" {
			return nil, fmt.Errorf("ambiguity: gate classified the question as ambiguous but gave neither a clarifying_question nor an assumption_stated")
		}
		if d.ClarifyingQuestion != "" && d.AssumptionStated != "" {
			return nil, fmt.Errorf("ambiguity: gate gave both a clarifying_question and an assumption_stated for an ambiguous question — exactly one is expected")
		}
	case instrumentation.GateUnanswerable:
		if d.RefusalReason == "" {
			return nil, fmt.Errorf("ambiguity: gate classified the question as unanswerable but gave no reason")
		}
	}

	return d, nil
}

// extractJSONObject returns the first top-level {...} substring of text, or
// "" if none is found. Claude Haiku reliably follows the "reply with ONLY a
// JSON object" instruction in systemPrompt, but this tolerates the model
// wrapping it in stray prose or a markdown code fence rather than treating
// that as an unrecoverable parse failure.
func extractJSONObject(text string) string {
	start := strings.IndexByte(text, '{')
	end := strings.LastIndexByte(text, '}')
	if start == -1 || end == -1 || end < start {
		return ""
	}
	return text[start : end+1]
}

// systemPromptTemplate encodes CLAUDE.md's "Pre-processing gate before
// execution" and spec FR-006 directly: what data this product has (so the
// model can tell a genuinely unanswerable question from an answerable
// one), and the exact three-way classification with its ambiguous-case
// rule (clarify OR state an assumption, never both, never neither).
//
// %[1]s/%[2]s are the actual data date range (buildSystemPrompt's
// dataStart/dataEnd) substituted in twice each: once in the plain data
// description, and once in the "Date grounding" paragraph, which is the
// direct fix for the year-omission/wrong-year defect docs/plan.md's
// mistakes log records (a question with no year, or relative language
// like "today"/"this week", was sometimes resolved against the real
// wall-clock system date — or worse, an invented year like 2024 — instead
// of the only period this product actually has data for).
const systemPromptTemplate = `You are the ambiguity/answerability gate for a restaurant margin-reconciliation copilot. You do not answer questions and you do not compute numbers — you only classify the question that follows.

Data actually available to this product (nothing else exists — do not assume any other data source, platform, supplier, or time period is present):
- A single independent restaurant/bar, single currency, single time zone.
- Daily reconciled margin data (gross sales by source, commissions, refunds, input costs, computed margin, discrepancy flags) for the fixed period %[1]s through %[2]s inclusive. A handful of days in that window are deliberately messy (a duplicate order, a refund crossing into a later week, one day with zero delivery-platform data) but every day in the window has SOME reconciled data.
- Promotion/ad-spend campaigns on two delivery platforms (iFood, Just Eat Takeaway) within that same period, with computed ROI where the incremental revenue can be attributed, and explicitly "cannot attribute" for at least one campaign.
- Nothing before %[1]s or after %[2]s exists. No other restaurant, location, supplier, platform, or currency exists.

Date grounding — read this before classifying any question that mentions a date:
- This product's only notion of "now"/"today" is %[2]s, the last date it has any data for. Never use the real-world current calendar date for that purpose — you do not know it reliably, and guessing at it (including inventing a plausible-sounding year) is exactly the failure this rule exists to prevent.
- Relative language — "today", "yesterday", "this week", "last week", "the weekend" — MUST be resolved as an offset from %[2]s, not from the real world's current date. E.g. "this week" is a trailing window ending %[2]s; "last week" is the 7 days before that.
- A date given without a year (e.g. "August 3rd", "the 2nd", "Aug 1") MUST be assumed to fall in the one year this data actually spans (between %[1]s and %[2]s) — resolve it to that year directly. Do not ask a clarifying question about the year, and never state or imply a different year (e.g. the real-world current year, or any other guess) anywhere in "reason", "clarifying_question", or "assumption_stated".
- Only classify a dated question as "unanswerable" for being outside the range if, once resolved per the rule above, the date still plainly falls outside %[1]s..%[2]s (e.g. a full date that explicitly names a different year, or a month/day that cannot exist within this window at all).

Classify the question into exactly one of:
- "answerable": it can be answered from the data above with no ambiguity about what's being asked.
- "ambiguous": the question is answerable in principle, but has more than one reasonable interpretation (e.g. a vague date range like "the weekend" without a clear anchor once resolved per the date-grounding rule above, or a pronoun/reference with no clear antecedent). For an ambiguous question, either:
  - ask ONE specific clarifying question that would resolve it (set "clarifying_question"), OR
  - if a reasonable default assumption is obvious and stating it plainly would let you proceed safely (e.g. "week" defaults to a trailing 7-day window ending %[2]s), state that assumption instead (set "assumption_stated") — never both, never neither.
- "unanswerable": the question references data this product does not have at all (a date outside %[1]s..%[2]s once resolved per the date-grounding rule above, a supplier/platform/restaurant not listed above, or a question that isn't about this restaurant's margin/reconciliation/promotions at all). Give a specific "reason" naming what's missing.

Reply with ONLY a single JSON object, no other text, no markdown fence, in exactly this shape:
{"classification": "answerable" | "ambiguous" | "unanswerable", "clarifying_question": "...", "assumption_stated": "...", "reason": "..."}

Leave clarifying_question/assumption_stated/reason as empty strings ("") whenever they don't apply to the classification you chose.`

// buildSystemPrompt substitutes the real data date range into
// systemPromptTemplate. dataStart/dataEnd are expected in YYYY-MM-DD form
// (internal/storage.LoadDataDateRange's format), but this function does not
// itself validate that — an obviously malformed value would simply produce
// a prompt the model can't use sensibly, surfaced immediately by every live
// call failing classification, not a silent wrong answer.
func buildSystemPrompt(dataStart, dataEnd string) string {
	return fmt.Sprintf(systemPromptTemplate, dataStart, dataEnd)
}
