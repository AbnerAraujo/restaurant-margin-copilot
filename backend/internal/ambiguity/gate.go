// Package ambiguity is the pre-processing gate CLAUDE.md and Constitution
// Principle II require: before any MCP tool call is made, classify the
// incoming question as answerable, ambiguous (needs either a clarifying
// question or an explicitly stated assumption), or unanswerable given the
// data this product actually has.
//
// It used Claude Haiku 4.5 originally — a cheap classification task, not
// one that needs frontier reasoning at the scale of the 14-day take-home
// dataset (constitution v1.1.0, research.md's model-split rationale). It
// now uses Claude Sonnet 5 (llmclient.ModelAmbiguityGate), moved there
// 2026-08-29 after Haiku was caught, live and reproducibly, misclassifying
// a fully in-range dated question as unanswerable once the real dataset
// grew to a multi-year span — see llmclient/cost.go's doc comment for the
// full account, including the honest correction to it: that failure was a
// date-range COMPARISON on an explicitly dated question, i.e. arithmetic
// that Constitution Principle I says should never have been any model's
// job. The real fix is daterange.go's deterministic pre-check, which now
// refuses clearly-out-of-range explicit dates in Go before any model call
// runs, and hands the model precomputed range verdicts for explicit dates
// that are in range. The Sonnet swap remains in place for what is left to
// the model — genuinely linguistic date resolution and the classification
// judgment itself — but it no longer carries the range arithmetic.
//
// Two passes, not one model doing both jobs, even though both now happen
// to run on the same underlying model: classification is ALWAYS one
// classification-pass call, exactly as before — an answerable question still costs
// exactly what it cost before this file changed. But when that
// classification is "ambiguous" (and the gate chose to ask rather than
// assume) or "unanswerable", the message the user actually reads was, until
// now, whatever Haiku happened to word in the same breath as its
// classification — a cheap model doing a job (writing a specific, helpful
// sentence to a restaurant owner) it was never chosen for. Classify's
// second pass (refineIfNeeded/writeBetterText) sends that draft to Claude
// Sonnet 5 to rewrite ONLY the prose, given Haiku's classification and
// draft as settled fact. Sonnet cannot re-decide answerable/ambiguous/
// unanswerable here even if it tried: writerResponse, the fixed JSON shape
// this second pass replies in, has no classification field at all, and
// refineIfNeeded never assigns decision.Result from it. This is a real,
// disclosed cost increase for exactly the harder cases — see Decision.Writer.
//
// This package has no import path to internal/mcptools or internal/storage
// at all (docs/technical-rfc.md's module architecture diagram): it
// classifies question text only and never touches real data itself. The
// one exception, deliberately kept narrow: New takes the data's actual
// min/max date range as plain strings (resolved once by the caller, via
// internal/storage.LoadDataDateRange, at process start) so that (a) the
// gate's system prompt can ground relative date language ("today", "this
// week", a year-less date) against what the data really covers, instead of
// either a hardcoded literal that can drift from the data actually
// loaded, or the host machine's wall-clock date — see systemPrompt's "Date
// grounding" paragraph and docs/plan.md's mistakes log ("date-year
// grounding defect") for the bug this fixes — and (b) daterange.go's
// deterministic pre-check can compare explicit, parseable dates against
// that window in Go before any model call happens at all. This package
// still never queries Postgres, never imports internal/storage, and never
// touches question data beyond the string it's asked to classify.
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
	"log"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/instrumentation"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/llmclient"
)

// ErrEmptyQuestion is returned by Classify for blank input — there is
// nothing to classify, and asking the model to classify an empty string
// would be a wasted call, not a real "ambiguous" case.
var ErrEmptyQuestion = errors.New("ambiguity: question is empty")

// MaxOutputTokens bounds the gate's response — it produces a small fixed
// JSON object (see systemPrompt), never free-form prose. Raised from the
// original 512 after a real live truncation on a genuinely complex,
// multi-part question ("what day of the month has the highest expenses,
// and what revenue would keep that day profitable?") — the classification
// pass needs room to reason about compound questions just as much as the
// writer pass already got more room for reasoning about a wide,
// multi-year date range (see MaxWriterOutputTokens's own doc comment,
// which fixed the identical symptom in the OTHER call this package makes
// but was never applied here too — this was that same fix's missing half).
//
// Raised again, from 768 to 1536, after a SECOND real live truncation on
// a different question ("the single highest-expense calendar date
// overall") — this one legitimately unanswerable (no tool ranks days by
// total expenses, only by margin), and gateResponse's own RefusalReason
// is composed inside this SAME call, not a separate one: for a genuinely
// nuanced refusal, Claude has to both classify AND write a full,
// specific explanation of what's missing and what it CAN offer instead,
// inside one token budget — a heavier combined burden than the writer
// pass's job of rewriting one already-decided sentence. 1536 gives real
// headroom for that combined job, still a hard, deliberate limit, not a
// loosening into open-ended prose — a response that still hits it is
// caught explicitly by the stop_reason check in Classify, never silently
// trusted.
const MaxOutputTokens = 1536

// Decision is the gate's classification of one question, plus the
// token/cost/latency figures the caller (internal/httpapi) hands straight
// to internal/instrumentation — this package computes them but never
// persists them itself (see package doc).
type Decision struct {
	Result instrumentation.GateResult

	// ClarifyingQuestion is non-empty when Result is Ambiguous and the gate
	// chose to ask, rather than assume.
	ClarifyingQuestion string
	// ClarifyingOptions are one-tap answers to ClarifyingQuestion.
	//
	// These are phrasings of a REPLY, never facts: whichever one the user
	// picks is posted back and re-classified by this same gate from scratch,
	// exactly as if they had typed it. So the model authoring them adds no
	// new trust surface — it already authors the clarifying question itself —
	// and nothing downstream treats an option as established.
	ClarifyingOptions []string
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

	// DeterministicPrecheck marks a Decision produced entirely by
	// daterange.go's explicit-date range pre-check: every explicit date the
	// question named falls wholly outside the known data window, so the
	// refusal was decided and worded in Go and NO model call of any kind was
	// made — every token/cost/latency field above is genuinely zero, and
	// Writer is nil by construction. The caller (internal/httpapi) uses this
	// to record the interaction honestly as a no-model refusal
	// (PrecheckModelLabel) rather than attributing a zero-token call to the
	// gate's model.
	DeterministicPrecheck bool

	// Writer is set when — and only when — this Decision's ambiguous/
	// unanswerable message was upgraded by the second-pass Claude Sonnet 5
	// writing call described in this package's doc comment: exactly the
	// Ambiguous-with-a-ClarifyingQuestion and Unanswerable cases. It stays
	// nil for Answerable and for Ambiguous-with-an-AssumptionStated (that
	// path proceeds straight into internal/explain — itself a Sonnet 5
	// call — so a second Sonnet call here would just double-pay for the
	// same quality upgrade without the user ever seeing this gate's prose).
	//
	// The caller (internal/httpapi) MUST log this as its own
	// instrumentation.Record and its own CostInteraction, exactly as it
	// already does for the classification call above — Constitution
	// Principle VI reads as per-call, not per-question (see ask.go's
	// package doc), and this is a second real, billed Anthropic API call.
	Writer *WriterCall
}

// WriterCall is the token/cost/latency figures for this Decision's optional
// second-pass Claude Sonnet 5 writing call — see Decision.Writer. It never
// carries a classification: see writerResponse and refineIfNeeded, which is
// the structural guarantee that this call can upgrade prose but never
// overrule Haiku's answerable/ambiguous/unanswerable decision.
type WriterCall struct {
	ModelUsed        string
	InputTokens      int64
	OutputTokens     int64
	EstimatedCostUSD float64
	LatencyMs        int64
}

// PendingClarification is the conversational context a follow-up reply needs
// in order to be classifiable at all.
//
// Without it this gate sees only the bare reply. A user answering the
// clarifying question "Do you mean August 2026, or a different month?" with
// "yes" was classified UNANSWERABLE with the reason "No question was asked"
// — correct in isolation, and completely wrong as product behaviour. Every
// clarification round trip in this product failed that way, which made the
// clarification path (the one Constitution Principle II exists to enable)
// a dead end rather than a conversation.
//
// This is deliberately a typed pair, not a free-form message history: the
// gate needs exactly the question that was ambiguous and the question it
// asked about it. Passing a whole transcript would hand the model far more
// latitude than the classification job needs, and this package's whole
// design is to keep the model's input narrow and its output a fixed shape.
type PendingClarification struct {
	// OriginalQuestion is the question that was classified ambiguous.
	OriginalQuestion string
	// ClarifyingQuestion is what this gate asked about it.
	ClarifyingQuestion string
}

// PreviousExchange is the immediately preceding question and its real
// ANSWER — the counterpart to PendingClarification for the gap that
// mechanism never covered: a follow-up to an actual answer ("and the day
// before?", "why?", "what about the week after that?"), not a reply to a
// clarifying question.
//
// Before this type existed, every such follow-up was classified against
// the gate in total isolation — "and the day before?" on its own is
// unclassifiable (no antecedent for "the day before" relative to anything),
// so the gate would almost certainly misfire: ambiguous when it shouldn't
// be, or unanswerable, or occasionally answerable against a wrong guessed
// date. This is what made the product feel like a stateless search box
// rather than a conversation for exactly this, very common, phrasing.
//
// Deliberately typed the same narrow way as PendingClarification, for the
// same reason (see that type's doc comment): exactly the immediately
// preceding question and the answer text the user was actually given,
// never a growing transcript. This is the direct discipline arXiv
// 2602.07338 ("Intent Mismatch Causes LLMs to Get Lost in Multi-Turn
// Conversation") argues for — a model given loose, unbounded conversational
// history drifts from what the user actually meant; one typed hop bounds
// that risk exactly the way it already does for the clarification case.
//
// Unlike PendingClarification, receiving a PreviousExchange does NOT mean
// the new text is definitely a follow-up — the user may equally be asking a
// brand new, unrelated question right after an answer. Deciding which is
// exactly this gate's classification job (see systemPromptTemplate's
// "Follow-ups to a previous answer" section), not something ComposeAnswerFollowUp
// or its caller should presume.
type PreviousExchange struct {
	// Question is the immediately preceding question that was actually
	// answered (not a clarifying question, refusal, or error).
	Question string
	// AnswerText is the answer text the user was actually given for Question.
	AnswerText string
}

// PrecheckModelLabel is what a deterministic pre-check refusal records as
// its "model used" — an honest sentinel, not a model ID: no model ran, no
// tokens were spent, and instrumentation must say so rather than logging a
// zero-token call against a real model name.
const PrecheckModelLabel = "none (deterministic date-range pre-check)"

// messageCreator is the one llmclient capability this package uses,
// declared as an interface at the point of use so tests can substitute a
// counting fake and assert — structurally, not by trust — that the
// deterministic pre-check path makes zero model calls. *llmclient.Client
// satisfies it directly.
type messageCreator interface {
	CreateMessage(ctx context.Context, req llmclient.MessageRequest) (*llmclient.MessageResult, error)
}

// Gate wraps an llmclient.Client to run this project's answerable /
// ambiguous / unanswerable classification.
type Gate struct {
	client       messageCreator
	systemPrompt string
	// dataStart/dataEnd (YYYY-MM-DD, inclusive) are kept beyond prompt
	// construction because daterange.go's deterministic pre-check compares
	// explicit dates against them in Go on every Classify call.
	dataStart string
	dataEnd   string
}

// New constructs a Gate over client (internal/llmclient, Phase 1).
// dataStart/dataEnd are the actual inclusive min/max date (YYYY-MM-DD) this
// product has reconciled data for — the caller (cmd/server/main.go)
// resolves these once via internal/storage.LoadDataDateRange at process
// start, so the gate's classification prompt and its deterministic
// date-range pre-check both reflect the real data actually loaded rather
// than a hardcoded literal baked into this package.
func New(client *llmclient.Client, dataStart, dataEnd string) *Gate {
	return newGate(client, dataStart, dataEnd)
}

// newGate is New minus the concrete client type, for tests that inject a
// counting fake messageCreator.
func newGate(client messageCreator, dataStart, dataEnd string) *Gate {
	return &Gate{
		client:       client,
		systemPrompt: buildSystemPrompt(dataStart, dataEnd),
		dataStart:    dataStart,
		dataEnd:      dataEnd,
	}
}

// gateResponse is the fixed JSON shape systemPrompt instructs the model to
// reply with — this package parses exactly this shape and refuses (returns
// an error, never a silent default) if the model's reply doesn't match it,
// the same "refuse rather than guess" discipline applied to the gate's own
// output, not just the product's numbers.
type gateResponse struct {
	Classification     string   `json:"classification"`
	ClarifyingQuestion string   `json:"clarifying_question"`
	ClarifyingOptions  []string `json:"clarifying_options"`
	AssumptionStated   string   `json:"assumption_stated"`
	Reason             string   `json:"reason"`
}

// Classify asks Claude Haiku 4.5 to classify question against the data
// this product actually has (see systemPrompt), returning a Decision the
// caller can act on directly.
//
// pending, when non-nil, says that question is a REPLY to a clarifying
// question this gate asked earlier. The pair is classified together as one
// resolved question — see PendingClarification for the defect this fixes.
//
// previousAnswer, when non-nil, says the immediately preceding assistant
// message was a real ANSWER (not a clarification) that question might be a
// follow-up to — see PreviousExchange. A single request never carries both:
// pending takes precedence if somehow both are set (a defensive default,
// not an expected input — the caller composes at most one of them from the
// visible conversation).
func (g *Gate) Classify(ctx context.Context, question string, pending *PendingClarification, previousAnswer *PreviousExchange) (*Decision, error) {
	if strings.TrimSpace(question) == "" {
		return nil, ErrEmptyQuestion
	}

	// Deterministic date-range pre-check (daterange.go), BEFORE any model
	// call: range inclusion for an explicit, parseable date is arithmetic
	// (Constitution Principle I), so it is decided here in Go, never by the
	// model. It runs on the user's own new text only — never on composed
	// follow-up context, whose quoted previous answers legitimately contain
	// in-range dates that are not what the user is asking about now.
	check := checkExplicitDateRange(question, g.dataStart, g.dataEnd)
	if check != nil && check.AllOutOfRange {
		// Refused with zero model calls: no classification call, no writer
		// pass. The refusal text is composed from the same facts a model
		// would have been handed, so there is nothing for a writer pass to
		// improve — and spending tokens polishing a deterministic refusal
		// would defeat the point of having decided it deterministically.
		return &Decision{
			Result:                instrumentation.GateUnanswerable,
			RefusalReason:         precheckRefusalReason(check.Verdicts, g.dataStart, g.dataEnd),
			DeterministicPrecheck: true,
		}, nil
	}

	resolved := ComposeFollowUp(question, pending)
	if pending == nil || strings.TrimSpace(pending.ClarifyingQuestion) == "" {
		resolved = ComposeAnswerFollowUp(question, previousAnswer)
	}
	if check != nil {
		// At least one explicit date IS in range (or the mentions are
		// mixed): the model still classifies the question, but every range
		// verdict Go could compute travels with it as settled fact, so the
		// model never re-derives date-range inclusion for a parseable date
		// — the exact arithmetic-in-the-model failure the 2026-08-29
		// incident exposed.
		resolved += "\n\n" + precheckFactNote(check.Verdicts, g.dataStart, g.dataEnd)
	}

	resp, err := g.client.CreateMessage(ctx, llmclient.MessageRequest{
		Model:     llmclient.ModelAmbiguityGate,
		System:    g.systemPrompt,
		MaxTokens: MaxOutputTokens,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(resolved)),
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
		g.refineIfNeeded(ctx, resolved, decision)
		return decision, nil
	}

	// Same discipline as writeBetterText: a max_tokens stop is a hard
	// failure regardless of whether the truncated text happens to parse as
	// valid JSON — the API's own stop_reason is the only reliable
	// truncation signal, and this path has no partial-Decision fallback to
	// offer (see internal/httpapi's own comment on this: a Classify error
	// always returns (nil, err), never a partial result).
	if resp.StopReason == anthropic.StopReasonMaxTokens {
		return nil, fmt.Errorf("ambiguity: classify: response was truncated at the %d-token cap before finishing (stop_reason=max_tokens)", MaxOutputTokens)
	}

	parsed, err := parseGateResponse(resp.Text)
	if err != nil {
		return nil, fmt.Errorf("ambiguity: %w", err)
	}

	decision.Result = parsed.Result
	decision.ClarifyingQuestion = parsed.ClarifyingQuestion
	decision.ClarifyingOptions = parsed.ClarifyingOptions
	decision.AssumptionStated = parsed.AssumptionStated
	decision.RefusalReason = parsed.RefusalReason

	g.refineIfNeeded(ctx, resolved, decision)
	return decision, nil
}

// refineIfNeeded runs this gate's second pass (see package doc) for exactly
// the two cases where a hard-to-word message actually reaches the user:
// Ambiguous with a ClarifyingQuestion, and Unanswerable. It is a deliberate
// no-op for Answerable (nothing to write) and for Ambiguous-with-an-
// AssumptionStated (see Decision.Writer for why).
//
// This method can only overwrite decision's PROSE fields
// (ClarifyingQuestion/ClarifyingOptions/RefusalReason) — it never touches
// decision.Result. A transport/parse failure in the writing pass is logged
// and swallowed, never surfaced as a Classify error: Haiku's own draft
// message is always a valid fallback, so a broken quality upgrade must
// degrade into "the question still gets answered/refused with the earlier
// wording", never into "the whole request fails" (the same degrade-not-fail
// discipline internal/httpapi's answer cache already applies).
func (g *Gate) refineIfNeeded(ctx context.Context, resolvedQuestion string, decision *Decision) {
	var draftText string
	switch {
	case decision.Result == instrumentation.GateAmbiguous && decision.ClarifyingQuestion != "":
		draftText = decision.ClarifyingQuestion
	case decision.Result == instrumentation.GateUnanswerable:
		draftText = decision.RefusalReason
	default:
		return
	}

	writer, call, err := g.writeBetterText(ctx, resolvedQuestion, decision.Result, draftText, decision.ClarifyingOptions)
	// A real API call may have actually run (and been billed) even when err
	// is non-nil below — e.g. the response came back but wasn't parseable
	// JSON. That cost is still real and must still be logged/reported, so
	// call is attached to decision independently of err.
	if call != nil {
		decision.Writer = call
	}
	if err != nil {
		log.Printf("ambiguity: Sonnet writing pass failed, falling back to Haiku's draft text (result=%s): %v", decision.Result, err)
		return
	}
	if writer == nil {
		// Sonnet declined to write (a model safety refusal) — Haiku's draft
		// still stands. The attempt is still a real, billed interaction
		// (decision.Writer was set above from call), it just produced no
		// usable replacement text.
		return
	}

	switch decision.Result {
	case instrumentation.GateAmbiguous:
		if writer.ClarifyingQuestion != "" {
			decision.ClarifyingQuestion = writer.ClarifyingQuestion
		}
		if len(writer.ClarifyingOptions) > 0 {
			decision.ClarifyingOptions = writer.ClarifyingOptions
		}
	case instrumentation.GateUnanswerable:
		if writer.Reason != "" {
			decision.RefusalReason = writer.Reason
		}
	}
}

// writerResponse is the fixed JSON shape the Sonnet writing pass must reply
// with. It deliberately has NO "classification" field — unlike
// gateResponse, whose whole job is to report one. Even if a writing-pass
// reply somehow smuggled in an extra "classification" key, encoding/json
// silently ignores unknown fields on Unmarshal, and nothing in
// refineIfNeeded ever reads decision.Result from a writerResponse. This is
// the structural half of "Sonnet cannot override Haiku's classification";
// writerSystemPrompt's instructions are the other half.
type writerResponse struct {
	ClarifyingQuestion string   `json:"clarifying_question"`
	ClarifyingOptions  []string `json:"clarifying_options"`
	Reason             string   `json:"reason"`
}

// MaxWriterOutputTokens bounds the writing pass's reply — one rewritten
// clarifying question (plus a short options list) or one rewritten refusal
// reason, never open-ended prose. Raised from the original 512 after a
// real truncation was observed live once the data window grew from 14
// days to over 700 (a multi-year date-range refusal needed more reasoning
// room than a short-window one did) — 768 is still a hard, deliberate cap
// for a single rewritten sentence, not a loosening into open-ended prose.
// A response that still hits this cap is now caught explicitly by the
// stop_reason check in writeBetterText, never silently trusted.
const MaxWriterOutputTokens = 768

// writeBetterText asks Claude Sonnet 5 to rewrite ONE already-decided
// clarifying-question or refusal-reason into a more specific, more helpful
// version, given the original question and Haiku's own draft.
//
// Return shape is deliberately three-way, not (result, error):
//   - (writer, call, nil): the call succeeded and produced usable text.
//   - (nil, call, nil): the call succeeded but Sonnet issued a safety
//     refusal — a real, billed interaction that produced no usable text.
//   - (nil, nil, err): a transport failure, or a parse failure severe
//     enough that no usage was ever attached (see below) — the caller
//     falls back to Haiku's draft without billing anything for this call.
//   - (nil, call, err): the API call itself succeeded (and billed real
//     tokens, captured in call) but the reply wasn't parseable JSON — the
//     caller must still log call's real cost even though there is no
//     writer text to apply.
func (g *Gate) writeBetterText(ctx context.Context, resolvedQuestion string, result instrumentation.GateResult, draftText string, draftOptions []string) (*writerResponse, *WriterCall, error) {
	prompt := buildWriterPrompt(resolvedQuestion, result, draftText, draftOptions)

	resp, err := g.client.CreateMessage(ctx, llmclient.MessageRequest{
		Model:     llmclient.ModelExplanation,
		System:    writerSystemPrompt,
		MaxTokens: MaxWriterOutputTokens,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("writing pass: %w", err)
	}

	cost, err := resp.EstimatedCostUSD(llmclient.ModelExplanation)
	if err != nil {
		return nil, nil, fmt.Errorf("writing pass: %w", err)
	}
	call := &WriterCall{
		ModelUsed:        llmclient.ModelExplanation,
		InputTokens:      resp.InputTokens,
		OutputTokens:     resp.OutputTokens,
		EstimatedCostUSD: cost,
		LatencyMs:        resp.Latency.Milliseconds(),
	}

	if resp.Refused {
		return nil, call, nil
	}

	// A max_tokens stop is treated as a hard failure, never handed to
	// parseWriterResponse at all — even if extractJSONObject's naive
	// first-'{'-to-last-'}' scan happens to find something that unmarshals
	// cleanly, a response Anthropic itself cut off mid-generation cannot be
	// trusted: the model may have been mid-way through unrelated reasoning
	// text before ever reaching its real answer, and a stray brace pair in
	// that text can extract as syntactically valid but semantically
	// garbled JSON (observed live: a truncated response's leftover
	// reasoning — "which is before 2024-08-01 is impossible; re-reading:
	// ..." — landed directly in a refusal_reason shown to the user). This
	// is the one failure mode extractJSONObject's own scan cannot detect
	// from the text alone, since the API's own stop_reason is the only
	// reliable signal that the model never finished.
	if resp.StopReason == anthropic.StopReasonMaxTokens {
		return nil, call, fmt.Errorf("writing pass: response was truncated at the %d-token cap before finishing (stop_reason=max_tokens) — never trusting a cut-off reply, however it happens to parse", MaxWriterOutputTokens)
	}

	writer, err := parseWriterResponse(resp.Text)
	if err != nil {
		return nil, call, fmt.Errorf("writing pass: %w", err)
	}
	return writer, call, nil
}

// parseWriterResponse is the pure, model-independent half of writeBetterText
// (mirrors parseGateResponse) — given the raw text a writing-pass call
// produced, extract and validate the fixed JSON shape writerSystemPrompt
// requires.
func parseWriterResponse(text string) (*writerResponse, error) {
	raw := extractJSONObject(text)
	if raw == "" {
		return nil, fmt.Errorf("writing pass response did not contain a JSON object: %q", text)
	}

	var wr writerResponse
	if err := json.Unmarshal([]byte(raw), &wr); err != nil {
		return nil, fmt.Errorf("writing pass response is not valid JSON: %w (raw: %q)", err, raw)
	}

	wr.ClarifyingQuestion = strings.TrimSpace(wr.ClarifyingQuestion)
	wr.Reason = strings.TrimSpace(wr.Reason)
	wr.ClarifyingOptions = cleanOptions(wr.ClarifyingOptions)
	return &wr, nil
}

// buildWriterPrompt renders the original question plus Haiku's own
// classification and draft into the single user message the writing pass
// sees. Deliberately narrow, the same discipline ComposeFollowUp documents:
// this pass gets exactly the facts it needs to rewrite one message, not a
// transcript or the full classification system prompt.
func buildWriterPrompt(resolvedQuestion string, result instrumentation.GateResult, draftText string, draftOptions []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "User's question: %q\n\n", resolvedQuestion)

	switch result {
	case instrumentation.GateAmbiguous:
		fmt.Fprintf(&b, "Classification (already decided — do not change it, and there is nowhere in your reply to state one): ambiguous, needs a clarifying question.\nDraft clarifying question: %q\n", draftText)
		if len(draftOptions) > 0 {
			b.WriteString("Draft reply options:\n")
			for _, opt := range draftOptions {
				fmt.Fprintf(&b, "- %q\n", opt)
			}
		} else {
			b.WriteString("Draft reply options: (none given)\n")
		}
	case instrumentation.GateUnanswerable:
		fmt.Fprintf(&b, "Classification (already decided — do not change it, and there is nowhere in your reply to state one): unanswerable, must be refused.\nDraft refusal reason: %q\n", draftText)
	}

	b.WriteString("\nRewrite the draft above into the best possible version for this outcome, per your system instructions.")
	return b.String()
}

// writerSystemPrompt is the second pass's system prompt. Unlike
// systemPromptTemplate, it never lists what data this product has and never
// asks the model to classify anything — see writerResponse and package doc
// for why that's a structural guarantee, not just a phrasing choice.
const writerSystemPrompt = `You are the writing pass for a restaurant margin-reconciliation copilot's question-answering gate. A separate, already-completed step has classified a user's question as either "ambiguous" (it needs a clarifying question) or "unanswerable" (it must be refused), and drafted a first-pass message for that outcome. Your ONLY job is to rewrite that message to be clearer, more specific, and more genuinely helpful to the restaurant owner reading it — using only the facts already given to you below. You do not classify anything, and you cannot change the outcome that was already decided; there is nowhere in your reply to even state one.

Rules:
- Never contradict, second-guess, or hedge about the classification given to you — treat it as settled fact.
- Never invent a new fact (a date, a platform, a number) that was not already present in the question or the draft you were given.
- For a clarifying question: keep it to ONE specific question. If draft reply options were given, write 2-3 short, concrete reply phrasings (each a complete answer the user could send as-is) — improve their wording if they were vague, but keep the same number of distinct choices they represent.
- For a refusal reason: state plainly and specifically what is missing or out of range, in one or two sentences — no hedging, no apology padding, no suggestion the answer might exist somewhere else.
- Keep the tone direct and plain-language, the way a competent colleague would explain it — not corporate, not verbose. Sound like a steward who's on the owner's side and wants to actually help them get to an answer, not like a compliance notice — warmth is in word choice and framing only, never in softening what is missing or turning a refusal into a maybe.

Reply with ONLY a single JSON object, no other text, no markdown fence, in exactly this shape:
{"clarifying_question": "...", "clarifying_options": ["...", "..."], "reason": "..."}

Leave whichever fields don't apply to the case you were given as empty strings ("")/[] — do not fill in the field for the outcome you were NOT given.`

// ComposeFollowUp renders a question plus its pending-clarification context
// into the single self-contained prompt the gate classifies.
//
// This is plain deterministic string assembly in Go, never a model call: the
// composition is a mechanical restatement of three strings the system already
// has, and asking a model to "merge" them would put a probabilistic step in
// front of the very gate that exists to decide whether a question is
// well-formed.
//
// Exported because internal/httpapi needs the identical composition for two
// other purposes — the text handed to the explanation step, and the answer
// cache's key. A bare reply like "yes" MUST NOT be the cache key: two
// different clarifications answered "yes" would otherwise collide and the
// second would be served the first one's answer.
func ComposeFollowUp(question string, pending *PendingClarification) string {
	if pending == nil || strings.TrimSpace(pending.ClarifyingQuestion) == "" {
		return question
	}
	return fmt.Sprintf(
		"%s\n\n[Follow-up context] The user originally asked: %q\nA clarifying question was put to them: %q\nThe text above this block is their reply to that clarifying question. Treat the original question, as resolved by that reply, as the question to classify.",
		question,
		strings.TrimSpace(pending.OriginalQuestion),
		strings.TrimSpace(pending.ClarifyingQuestion),
	)
}

// ComposeAnswerFollowUp renders a question plus the immediately preceding
// answered exchange into the single self-contained prompt the gate
// classifies — the ANSWER-side counterpart to ComposeFollowUp's
// CLARIFICATION-side composition. See PreviousExchange's doc comment for
// the gap this closes (a follow-up to a real answer had no equivalent
// mechanism at all before this) and for why it is deliberately exactly one
// hop, never a growing transcript.
//
// Like ComposeFollowUp, this is plain deterministic string assembly in Go,
// never a model call, and is exported for the same reason: internal/httpapi
// needs the identical composition for the explanation step's input and for
// the answer cache's key. A bare repeat like "and the day before?" MUST NOT
// be the cache key on its own — two different prior answers followed by the
// identical bare phrase would otherwise collide, and the second would be
// served the first one's (now wrong) answer.
//
// Unlike ComposeFollowUp, the composed text does NOT assert that the new
// text IS a follow-up — previous may be entirely irrelevant to a brand new
// question the user happens to ask right after an answer. Deciding that is
// this gate's classification job (systemPromptTemplate's "Follow-ups to a
// previous answer" section), not something this function should presume.
func ComposeAnswerFollowUp(question string, previous *PreviousExchange) string {
	if previous == nil || strings.TrimSpace(previous.AnswerText) == "" {
		return question
	}
	return fmt.Sprintf(
		"%s\n\n[Previous exchange context] The user previously asked: %q and was told: %q\nThe text above this block is what they have now said. This may be a follow-up to that previous exchange, or it may be a brand new, unrelated question — decide which from its content. If it is a follow-up, classify its resolved meaning (the new text interpreted in light of the previous question and answer) as the question to classify.",
		question,
		strings.TrimSpace(previous.Question),
		strings.TrimSpace(previous.AnswerText),
	)
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
		ClarifyingOptions:  cleanOptions(gr.ClarifyingOptions),
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

// cleanOptions trims and drops blank options, and caps the list. A cap
// matters because these render as tappable chips: a model returning eight
// options would produce a wall of buttons where the point is a fast choice.
func cleanOptions(options []string) []string {
	const maxOptions = 4
	out := make([]string, 0, len(options))
	for _, option := range options {
		trimmed := strings.TrimSpace(option)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
		if len(out) == maxOptions {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
// dataStart/dataEnd) substituted in throughout: in the plain data
// description, and in the "Date grounding" paragraph, which is the
// direct fix for the year-omission/wrong-year defect docs/plan.md's
// mistakes log records (a question with no year, or relative language
// like "today"/"this week", was sometimes resolved against the real
// wall-clock system date — or worse, an invented year like 2024 — instead
// of the only period this product actually has data for).
const systemPromptTemplate = `You are the ambiguity/answerability gate for a restaurant margin-reconciliation copilot. You do not answer questions and you do not compute numbers — you only classify the question that follows.

Data actually available to this product (nothing else exists — do not assume any other data source, platform, supplier, or time period is present):
- A single independent restaurant/bar, single currency, single time zone.
- Daily reconciled margin data (gross sales by source, commissions, refunds, input costs, computed margin, discrepancy flags) for the fixed period %[1]s through %[2]s inclusive, spanning calendar year(s) %[3]s — this window may span anywhere from a couple of weeks to multiple years; do not assume it is short, and do not silently substitute a different, shorter-sounding range from memory. Some days in that window carry discrepancy flags (a duplicate order, a refund crossing into a later week, an anomaly threshold breach) but every day in the window has SOME reconciled data.
- Promotion/ad-spend campaigns on two delivery platforms (iFood, Just Eat Takeaway) within that same period, with computed ROI where the incremental revenue can be attributed, and explicitly "cannot attribute" for at least one campaign.
- Nothing before %[1]s or after %[2]s exists. No other restaurant, location, supplier, platform, or currency exists.

Date grounding — read this before classifying any question that mentions a date:
- This product's only notion of "now"/"today" is %[2]s, the last date it has any data for. Never use the real-world current calendar date for that purpose — you do not know it reliably, and guessing at it (including inventing a plausible-sounding year) is exactly the failure this rule exists to prevent.
- Relative language — "today", "yesterday", "this week", "last week", "the weekend" — MUST be resolved as an offset from %[2]s, not from the real world's current date. E.g. "this week" is a trailing window ending %[2]s; "last week" is the 7 days before that.
- A date given without a year (e.g. "August 3rd", "the 2nd", "Aug 1") MUST be resolved against %[2]s (this product's "today") — assume the most recent occurrence of that month/day at or before %[2]s, since that is what "today" and recent relative language ("this week", "last week") already anchor to elsewhere in this prompt. Do not ask a clarifying question about the year, and never state or imply the real-world current year.
- Range comparison for explicitly dated references is NOT your job. A deterministic pre-check in Go parses common explicit date forms (an ISO date, "July 2026", "August 9, 2025", a bare year) and compares them against %[1]s..%[2]s before you are ever called: a question whose explicit dates all fall outside the range is refused before this prompt runs, and any explicit reference the pre-check did recognize reaches you inside a "[Deterministic date-range check]" block with its verdict already computed. Treat those verdicts as settled fact — never re-derive, second-guess, or contradict them, and never classify a question as unanswerable on date-range grounds when its reference is marked IN RANGE.
- Only when a dated reference carries NO precomputed verdict (a phrasing the pre-check does not parse, e.g. "the seventh month of 2026") does the comparison fall to you as a fallback: do it explicitly and carefully — a later calendar year is always a later date regardless of month (e.g. any month in 2026 is after any month in 2024) — and classify "unanswerable" for range reasons only if the resolved date is truly earlier than %[1]s or truly later than %[2]s once compared this way, never assumed from a superficial reading.

Campaign/promotion references — read this before classifying any question that names a specific campaign or promotion:
- This product's promotion data is identified by campaign_id codes (e.g. IFOOD-CAMP-BOOST01, JET-CAMP-LUNCHFIX). A user may reasonably refer to a real campaign by a shortened fragment of its id (e.g. "LUNCHFIX", "BOOST01"), or by a full human-readable display name that embeds the id (e.g. "Banner Ad - Lunch Fix Menu (JET-CAMP-LUNCHFIX)"), rather than typing the exact id.
- You do not have the list of real campaign ids in front of you, and you must NOT classify a question as "unanswerable" just because a named campaign doesn't look familiar or doesn't exactly match an id you can recall. Resolving a shortened or human-readable campaign reference against the real, bounded set of campaigns is a downstream typed lookup's job, not yours — classify a question that names what is plausibly a campaign/promotion (any short code, abbreviation, or descriptive campaign-sounding name) as "answerable", and let the downstream tool return its own no_data result if that specific campaign genuinely doesn't exist.
- This does not apply to platforms, suppliers, or restaurants — those ARE fully enumerated above, so a question naming one not listed there ("Instagram ads", a named supplier not in this data) is still correctly "unanswerable".

Evaluative language — read this before classifying a question that uses a subjective-sounding word ("underperforming", "losing money", "bad", "worst", "best", "worth it"):
- This product's tool set already defines several of these words deterministically — "underperforming" or "losing money" promotions means a computed negative ROI, and a typed tool exists specifically to return that list. A question is not ambiguous merely because it uses an evaluative word that this product's tool set already resolves to a fixed computation.
- Classify a question like this as "answerable" and let the downstream explanation step call the matching tool to resolve it. Do not ask the user to define a threshold, cutoff, or ranking method that a typed tool already defines for them — that is exactly the false-clarify failure this rule exists to prevent.

Follow-up replies — read this before classifying any input containing a "[Follow-up context]" block:
- That block means the user is REPLYING to a clarifying question this gate asked a moment ago. Their reply may be a bare fragment ("yes", "Saturday-Sunday only", "the second one", "August"), which is meaningless on its own and must NEVER be classified on its own.
- Classify the ORIGINAL question as resolved by that reply. A short reply is not an empty question, and "no question was asked" is never a correct classification for one.
- If the reply resolves the ambiguity, classify "answerable" and set "assumption_stated" to a one-line statement of the now-resolved reading (e.g. "Interpreting 'this month' as %[1]s through %[2]s, per your reply."), so the downstream step answers the resolved question rather than the fragment.
- If the reply genuinely does not resolve it — for example a bare "yes" to an either/or question where either branch remains possible — classify "ambiguous" and ask ONE more specific clarifying question that names the remaining options explicitly. Do not refuse.
- Only classify "unanswerable" here if the RESOLVED question is itself outside the data described above (e.g. the reply pins the question to a date outside %[1]s..%[2]s).
- Classify the resolved question fresh, on its own terms — never silently carry forward an interpretation, assumption, or unstated context from any earlier exchange beyond the exact follow-up pair you are given here. This pair is the entire conversational history you get, deliberately (see this package's doc comment on PendingClarification); treat it as such rather than reasoning as if a longer transcript existed behind it.

Follow-ups to a previous answer — read this before classifying any input containing a "[Previous exchange context]" block:
- That block means the immediately preceding assistant message was a real ANSWER (not a clarifying question), and the text above the block MIGHT be a follow-up to it — "and the day before?", "why?", "what about last month instead?", a bare "and Sunday?". Unlike the "[Follow-up context]" case above, this is NOT guaranteed to be a follow-up at all: the user may just as easily be asking a brand new, self-contained question right after getting an answer.
- Decide which, from the content of the new text alone. If it plausibly continues or references the previous question/answer (a pronoun, an implicit comparison, an elliptical date/period reference, a bare "why?"), resolve it against that previous exchange and classify the RESOLVED meaning — exactly as you would classify any other question, following every rule above (date grounding, evaluative language, etc.) against that resolved meaning.
- If the new text reads as a complete, unrelated question on its own (a different topic, a fully-specified new date or period, a different tool-shaped request), classify it on its own terms and ignore the previous exchange entirely — do not force a follow-up reading onto a question that does not need one.
- Same anti-drift discipline as the clarification case above: this one prior exchange is the entire conversational history you get, deliberately. Never reason as if a longer transcript existed behind it, and never treat this resolved question as itself carrying forward into some further, later exchange beyond it.

Classify the question into exactly one of:
- "answerable": it can be answered from the data above with no ambiguity about what's being asked.
- "ambiguous": the question is answerable in principle, but has more than one reasonable interpretation (e.g. a vague date range like "the weekend" without a clear anchor once resolved per the date-grounding rule above, or a pronoun/reference with no clear antecedent). For an ambiguous question, either:
  - ask ONE specific clarifying question that would resolve it (set "clarifying_question", and set "clarifying_options" to 2-3 short phrasings of the possible answers, each one a complete reply the user could send as-is, e.g. ["Friday to Sunday", "Saturday and Sunday only"]), OR
  - if a reasonable default assumption is obvious and stating it plainly would let you proceed safely (e.g. "week" defaults to a trailing 7-day window ending %[2]s), state that assumption instead (set "assumption_stated") — never both, never neither.
- "unanswerable": the question references data this product does not have at all (a date outside %[1]s..%[2]s once resolved per the date-grounding rule above, a supplier/platform/restaurant not listed above, or a question that isn't about this restaurant's margin/reconciliation/promotions at all). Give a specific "reason" naming what's missing.

Reply with ONLY a single JSON object, no other text, no markdown fence, in exactly this shape:
{"classification": "answerable" | "ambiguous" | "unanswerable", "clarifying_question": "...", "clarifying_options": ["...", "..."], "assumption_stated": "...", "reason": "..."}

Leave clarifying_question/assumption_stated/reason as empty strings ("") and clarifying_options as [] whenever they don't apply to the classification you chose.`

// buildSystemPrompt substitutes the real data date range into
// systemPromptTemplate. dataStart/dataEnd are expected in YYYY-MM-DD form
// (internal/storage.LoadDataDateRange's format), but this function does not
// itself validate that — an obviously malformed value would simply produce
// a prompt the model can't use sensibly, surfaced immediately by every live
// call failing classification, not a silent wrong answer.
func buildSystemPrompt(dataStart, dataEnd string) string {
	return fmt.Sprintf(systemPromptTemplate, dataStart, dataEnd, spannedYears(dataStart, dataEnd))
}

// spannedYears renders the real calendar year(s) dataStart..dataEnd covers
// as an explicit, already-computed fact ("2024, 2025, and 2026", or just
// "2026" for a single-year range) — a real bug this fixes directly: once
// the data window grew from 14 days (all in one calendar year) to over 700
// days spanning three, Haiku's own classification drafts were observed
// live mis-stating the range entirely (defaulting back to a short,
// single-year window it wasn't actually given), i.e. inferring "how many
// years does this span" was itself an unreliable step for a cheap
// classification model to take silently. Stating the span as a fact here
// removes that inference step rather than asking the prompt to word it
// more persuasively. dataStart/dataEnd are already validated YYYY-MM-DD by
// the caller (internal/storage.LoadDataDateRange); a parse failure here
// degrades to omitting the fact rather than panicking, since this is a
// clarity aid, not the correctness boundary itself (Rules elsewhere in
// this prompt still require the model to reason from %[1]s/%[2]s directly).
func spannedYears(dataStart, dataEnd string) string {
	start, err1 := time.Parse("2006-01-02", dataStart)
	end, err2 := time.Parse("2006-01-02", dataEnd)
	if err1 != nil || err2 != nil || end.Year() < start.Year() {
		return "the years covered by that range"
	}
	if start.Year() == end.Year() {
		return fmt.Sprintf("%d", start.Year())
	}
	years := make([]string, 0, end.Year()-start.Year()+1)
	for y := start.Year(); y <= end.Year(); y++ {
		years = append(years, fmt.Sprintf("%d", y))
	}
	if len(years) == 2 {
		return years[0] + " and " + years[1]
	}
	return strings.Join(years[:len(years)-1], ", ") + ", and " + years[len(years)-1]
}
