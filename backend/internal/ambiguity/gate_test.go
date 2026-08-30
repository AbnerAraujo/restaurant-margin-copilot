package ambiguity

// TestParseGateResponse_* are pure unit tests over this package's
// model-independent classification logic (tasks.md T024) — hand-crafted
// strings standing in for what Claude Haiku 4.5 could plausibly reply with,
// covering all three classifications plus the malformed-response cases.
// They make zero Anthropic API calls and cost nothing.
//
// TestGate_Classify_LiveSmokeTest is the separate, deliberately small
// (3 calls) live-API test proving the real end-to-end path works
// end-to-end against Claude Haiku 4.5 (tasks.md T025's "a handful of calls
// is plenty" allowance) — skipped when ANTHROPIC_API_KEY isn't set, never
// faked.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/instrumentation"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/llmclient"
)

func TestParseGateResponse_Answerable(t *testing.T) {
	d, err := parseGateResponse(`{"classification": "answerable", "clarifying_question": "", "assumption_stated": "", "reason": ""}`)
	require.NoError(t, err)
	require.Equal(t, instrumentation.GateAnswerable, d.Result)
	require.Empty(t, d.ClarifyingQuestion)
	require.Empty(t, d.AssumptionStated)
	require.Empty(t, d.RefusalReason)
}

func TestParseGateResponse_AmbiguousWithClarifyingQuestion(t *testing.T) {
	d, err := parseGateResponse(`{"classification": "ambiguous", "clarifying_question": "Does \"the weekend\" include Friday?", "assumption_stated": "", "reason": ""}`)
	require.NoError(t, err)
	require.Equal(t, instrumentation.GateAmbiguous, d.Result)
	require.Equal(t, `Does "the weekend" include Friday?`, d.ClarifyingQuestion)
	require.Empty(t, d.AssumptionStated)
}

func TestParseGateResponse_AmbiguousWithStatedAssumption(t *testing.T) {
	d, err := parseGateResponse(`{"classification": "ambiguous", "clarifying_question": "", "assumption_stated": "Assuming \"this week\" means the trailing 7 days ending 2026-08-14.", "reason": ""}`)
	require.NoError(t, err)
	require.Equal(t, instrumentation.GateAmbiguous, d.Result)
	require.Empty(t, d.ClarifyingQuestion)
	require.Contains(t, d.AssumptionStated, "trailing 7 days")
}

func TestParseGateResponse_Unanswerable(t *testing.T) {
	d, err := parseGateResponse(`{"classification": "unanswerable", "clarifying_question": "", "assumption_stated": "", "reason": "No data exists for September 2026; this product only covers 2026-08-01 through 2026-08-14."}`)
	require.NoError(t, err)
	require.Equal(t, instrumentation.GateUnanswerable, d.Result)
	require.Contains(t, d.RefusalReason, "2026-08-01")
}

func TestParseGateResponse_TolerantOfSurroundingProseAndMarkdownFence(t *testing.T) {
	d, err := parseGateResponse("Sure, here's my classification:\n```json\n{\"classification\": \"answerable\", \"clarifying_question\": \"\", \"assumption_stated\": \"\", \"reason\": \"\"}\n```")
	require.NoError(t, err)
	require.Equal(t, instrumentation.GateAnswerable, d.Result)
}

func TestParseGateResponse_RejectsInvalidClassification(t *testing.T) {
	_, err := parseGateResponse(`{"classification": "maybe", "clarifying_question": "", "assumption_stated": "", "reason": ""}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid classification")
}

func TestParseGateResponse_RejectsAmbiguousWithNeitherClarificationNorAssumption(t *testing.T) {
	_, err := parseGateResponse(`{"classification": "ambiguous", "clarifying_question": "", "assumption_stated": "", "reason": ""}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "neither")
}

func TestParseGateResponse_RejectsAmbiguousWithBothClarificationAndAssumption(t *testing.T) {
	_, err := parseGateResponse(`{"classification": "ambiguous", "clarifying_question": "Which week?", "assumption_stated": "Assuming last week.", "reason": ""}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "both")
}

func TestParseGateResponse_RejectsUnanswerableWithNoReason(t *testing.T) {
	_, err := parseGateResponse(`{"classification": "unanswerable", "clarifying_question": "", "assumption_stated": "", "reason": ""}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no reason")
}

func TestParseGateResponse_RejectsNonJSON(t *testing.T) {
	_, err := parseGateResponse("I think this question is answerable.")
	require.Error(t, err)
	require.Contains(t, err.Error(), "did not contain a JSON object")
}

func TestParseGateResponse_RejectsMalformedJSON(t *testing.T) {
	_, err := parseGateResponse(`{"classification": "answerable", }`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not valid JSON")
}

// TestParseWriterResponse_* are pure unit tests over the second-pass
// writing call's model-independent parsing (parseWriterResponse), mirroring
// TestParseGateResponse_* above. writerResponse has no "classification"
// field at all — these tests confirm that an extra "classification" key in
// the raw JSON is simply ignored by encoding/json (Go's default behavior
// for unknown fields) rather than somehow smuggled through, which is the
// structural half of "Sonnet cannot override Haiku's classification" this
// package's doc comment describes.

func TestParseWriterResponse_AmbiguousRewrite(t *testing.T) {
	wr, err := parseWriterResponse(`{"clarifying_question": "Does \"the weekend\" mean Friday through Sunday, or just Saturday and Sunday?", "clarifying_options": ["Friday through Sunday", "Saturday and Sunday only"], "reason": ""}`)
	require.NoError(t, err)
	require.Equal(t, `Does "the weekend" mean Friday through Sunday, or just Saturday and Sunday?`, wr.ClarifyingQuestion)
	require.Equal(t, []string{"Friday through Sunday", "Saturday and Sunday only"}, wr.ClarifyingOptions)
	require.Empty(t, wr.Reason)
}

func TestParseWriterResponse_UnanswerableRewrite(t *testing.T) {
	wr, err := parseWriterResponse(`{"clarifying_question": "", "clarifying_options": [], "reason": "This product only has reconciled data from 2026-08-01 through 2026-08-14 — September 2026 isn't in range yet."}`)
	require.NoError(t, err)
	require.Empty(t, wr.ClarifyingQuestion)
	require.Contains(t, wr.Reason, "2026-08-01")
}

// A malicious or confused model response including an extra
// "classification" key must not break parsing, and — critically —
// writerResponse has nowhere for that value to land: this test proves the
// parsed struct carries only prose fields, never a classification.
func TestParseWriterResponse_IgnoresAnyClassificationField(t *testing.T) {
	wr, err := parseWriterResponse(`{"classification": "answerable", "clarifying_question": "Which week — this one or last?", "clarifying_options": [], "reason": ""}`)
	require.NoError(t, err)
	require.Equal(t, "Which week — this one or last?", wr.ClarifyingQuestion)
	// writerResponse (see its type definition) has exactly three fields:
	// ClarifyingQuestion, ClarifyingOptions, Reason. There is no field a
	// "classification" key could have populated, by construction.
}

func TestParseWriterResponse_RejectsNonJSON(t *testing.T) {
	_, err := parseWriterResponse("Sure, here's a better clarifying question.")
	require.Error(t, err)
	require.Contains(t, err.Error(), "did not contain a JSON object")
}

func TestParseWriterResponse_RejectsMalformedJSON(t *testing.T) {
	_, err := parseWriterResponse(`{"reason": "missing data", }`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not valid JSON")
}

// TestSpannedYears_* is a pure unit test over the fix for a real bug found
// live: once the data window grew from 14 days (all in 2026) to a
// multi-year range, Haiku's own classification drafts were observed
// mis-stating the range entirely — mid-generation collapsing back to a
// short, single-year window it wasn't actually given. Stating the exact
// years spanned as a computed fact in the prompt (rather than requiring
// the model to infer "how many years does this cover" itself) is the fix;
// this test is the one part of that fix pure Go can verify without an API
// call — the wording, not the model's resulting behavior.
func TestSpannedYears_SingleYear(t *testing.T) {
	require.Equal(t, "2026", spannedYears("2026-08-01", "2026-08-14"))
}

func TestSpannedYears_TwoYears(t *testing.T) {
	require.Equal(t, "2025 and 2026", spannedYears("2025-08-15", "2026-08-14"))
}

func TestSpannedYears_ThreeYears(t *testing.T) {
	require.Equal(t, "2024, 2025, and 2026", spannedYears("2024-08-01", "2026-08-14"))
}

func TestSpannedYears_DegradesGracefullyOnUnparsableInput(t *testing.T) {
	require.Equal(t, "the years covered by that range", spannedYears("not-a-date", "2026-08-14"))
}

// TestComposeAnswerFollowUp_* are pure unit tests over the ANSWER-side
// follow-up composition (the counterpart to ask_clarification_test.go's
// TestComposeFollowUpIsDeterministicAndInertodWithoutContext, which covers
// ComposeFollowUp), asserting the EXACT composed text rather than just
// checking it's non-empty — this string is what the gate classifies, what
// explain narrates, and what the answer cache keys on, so its exact shape
// is load-bearing, not incidental.

func TestComposeAnswerFollowUp_NilPreviousReturnsQuestionUnchanged(t *testing.T) {
	require.Equal(t, "and the day before?", ComposeAnswerFollowUp("and the day before?", nil))
}

func TestComposeAnswerFollowUp_EmptyAnswerTextTreatedAsNoContext(t *testing.T) {
	previous := &PreviousExchange{Question: "What was our margin on 2026-08-05?", AnswerText: "   "}
	require.Equal(t, "and the day before?", ComposeAnswerFollowUp("and the day before?", previous))
}

func TestComposeAnswerFollowUp_ComposesTheExactExpectedText(t *testing.T) {
	previous := &PreviousExchange{
		Question:   "What was our margin on 2026-08-05?",
		AnswerText: "Margin on 2026-08-05 was $612.40 on $2,180.00 in gross sales.",
	}
	got := ComposeAnswerFollowUp("and the day before?", previous)
	want := "and the day before?" +
		"\n\n[Previous exchange context] The user previously asked: \"What was our margin on 2026-08-05?\" and was told: \"Margin on 2026-08-05 was $612.40 on $2,180.00 in gross sales.\"" +
		"\nThe text above this block is what they have now said. This may be a follow-up to that previous exchange, or it may be a brand new, unrelated question — decide which from its content. If it is a follow-up, classify its resolved meaning (the new text interpreted in light of the previous question and answer) as the question to classify."
	require.Equal(t, want, got)
}

func TestComposeAnswerFollowUp_TrimsWhitespaceInPreviousFields(t *testing.T) {
	previous := &PreviousExchange{Question: "  orig question  ", AnswerText: "  the answer  "}
	got := ComposeAnswerFollowUp("why?", previous)
	require.Contains(t, got, `"orig question"`)
	require.Contains(t, got, `"the answer"`)
	require.NotContains(t, got, "  orig question  ")
}

func TestComposeAnswerFollowUp_IsDeterministic(t *testing.T) {
	previous := &PreviousExchange{Question: "orig", AnswerText: "answer"}
	require.Equal(t, ComposeAnswerFollowUp("reply", previous), ComposeAnswerFollowUp("reply", previous),
		"composition must be deterministic — it is the cache key")
}

// A follow-up to a real ANSWER must never be confused, at the composition
// layer, with a reply to a CLARIFYING question — the two produce visibly
// different marker text ("[Previous exchange context]" vs. "[Follow-up
// context]") so the gate's system prompt sections can never be misapplied
// to the wrong case.
func TestComposeAnswerFollowUp_UsesADistinctMarkerFromClarificationFollowUp(t *testing.T) {
	previous := &PreviousExchange{Question: "orig", AnswerText: "answer"}
	got := ComposeAnswerFollowUp("why?", previous)
	require.Contains(t, got, "[Previous exchange context]")
	require.NotContains(t, got, "[Follow-up context]")
}

// testDataStart/testDataEnd are an arbitrary 14-day grounding window the
// assertions below stay consistent with — tests that don't care
// about the exact date-grounding text still need a well-formed range for
// New to build a usable system prompt.
const (
	testDataStart = "2026-08-01"
	testDataEnd   = "2026-08-14"
)

func TestClassify_RejectsEmptyQuestion(t *testing.T) {
	g := New(llmclient.New(), testDataStart, testDataEnd)
	_, err := g.Classify(context.Background(), "   ", nil, nil)
	require.ErrorIs(t, err, ErrEmptyQuestion)
}

// countingCreator is a fake messageCreator in this codebase's counting-fake
// convention (see internal/httpapi's countingGate): it records every
// would-be model call so a test can assert, structurally, that a code path
// made exactly N of them — most importantly N == 0 for the deterministic
// date-range pre-check.
type countingCreator struct {
	calls    int
	lastReq  llmclient.MessageRequest
	response *llmclient.MessageResult
}

func (c *countingCreator) CreateMessage(_ context.Context, req llmclient.MessageRequest) (*llmclient.MessageResult, error) {
	c.calls++
	c.lastReq = req
	if c.response == nil {
		return nil, fmt.Errorf("countingCreator: no canned response configured")
	}
	resp := *c.response
	return &resp, nil
}

// The core guarantee of the deterministic pre-check: a question whose
// explicit date is clearly outside the known data window is refused by Go
// alone — zero model invocations, zero tokens, zero cost, and a refusal
// reason naming the real facts.
func TestClassify_OutOfRangeExplicitDateRefusesWithZeroModelCalls(t *testing.T) {
	fake := &countingCreator{}
	g := newGate(fake, testDataStart, testDataEnd)

	d, err := g.Classify(context.Background(), "What was our margin in July 2023?", nil, nil)
	require.NoError(t, err)

	require.Equal(t, 0, fake.calls, "an out-of-range explicit date must never reach any model — range inclusion is Go's job (Constitution Principle I)")
	require.Equal(t, instrumentation.GateUnanswerable, d.Result)
	require.True(t, d.DeterministicPrecheck)
	require.Nil(t, d.Writer, "no writer pass either — the refusal is worded deterministically")
	require.Zero(t, d.InputTokens)
	require.Zero(t, d.OutputTokens)
	require.Zero(t, d.EstimatedCostUSD)
	require.Contains(t, d.RefusalReason, `"July 2023"`)
	require.Contains(t, d.RefusalReason, testDataStart)
	require.Contains(t, d.RefusalReason, testDataEnd)
}

// The other half of the pre-check: an explicit IN-RANGE date still goes to
// the model for classification (ambiguity judgment is legitimately its
// job), but the range verdict travels with it as an already-computed fact —
// the model is never asked to re-derive the comparison that Haiku got wrong
// live on 2026-08-29.
func TestClassify_InRangeExplicitDateHandsTheModelAPrecomputedVerdict(t *testing.T) {
	fake := &countingCreator{response: &llmclient.MessageResult{
		Text:         `{"classification": "answerable", "clarifying_question": "", "clarifying_options": [], "assumption_stated": "", "reason": ""}`,
		StopReason:   anthropic.StopReasonEndTurn,
		InputTokens:  400,
		OutputTokens: 20,
	}}
	g := newGate(fake, testDataStart, testDataEnd)

	d, err := g.Classify(context.Background(), "What was our margin on 2026-08-05?", nil, nil)
	require.NoError(t, err)
	require.Equal(t, instrumentation.GateAnswerable, d.Result)
	require.False(t, d.DeterministicPrecheck)
	require.Equal(t, 1, fake.calls, "in-range dates still get a real classification call")

	require.Len(t, fake.lastReq.Messages, 1)
	sent := fake.lastReq.Messages[0].Content[0].GetText()
	require.NotNil(t, sent)
	require.Contains(t, *sent, "[Deterministic date-range check")
	require.Contains(t, *sent, `"2026-08-05": IN RANGE`)
}

// A question with no explicit fully-specified date must reach the model
// exactly as before — no verdict block, no behavior change for the
// genuinely linguistic cases the model exists for.
func TestClassify_NonExplicitDateQuestionIsUntouchedByThePrecheck(t *testing.T) {
	fake := &countingCreator{response: &llmclient.MessageResult{
		Text:       `{"classification": "answerable", "clarifying_question": "", "clarifying_options": [], "assumption_stated": "", "reason": ""}`,
		StopReason: anthropic.StopReasonEndTurn,
	}}
	g := newGate(fake, testDataStart, testDataEnd)

	_, err := g.Classify(context.Background(), "How did we do this week?", nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, fake.calls)
	sent := fake.lastReq.Messages[0].Content[0].GetText()
	require.NotNil(t, sent)
	require.NotContains(t, *sent, "[Deterministic date-range check")
}

// TestGate_Classify_LiveSmokeTest makes exactly 3 real Claude Haiku 4.5
// calls — one per classification — to prove the request shape (system
// prompt, JSON response parsing, cost estimation) actually works against
// the live API, not just against hand-crafted strings above. Skipped, not
// faked, when ANTHROPIC_API_KEY isn't set. Full evaluation-harness
// verification of the gate happens in a later phase; this is deliberately
// minimal per tasks.md's "under 10 calls" guidance for this phase.
func TestGate_Classify_LiveSmokeTest(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set; skipping live Claude Haiku 4.5 smoke test")
	}

	g := New(llmclient.New(), testDataStart, testDataEnd)
	ctx := context.Background()

	t.Run("answerable", func(t *testing.T) {
		d, err := g.Classify(ctx, "What was our reconciled margin on 2026-08-03?", nil, nil)
		require.NoError(t, err)
		require.Equal(t, instrumentation.GateAnswerable, d.Result)
		require.Greater(t, d.InputTokens, int64(0))
		require.GreaterOrEqual(t, d.EstimatedCostUSD, 0.0)
		t.Logf("answerable smoke test: %d in / %d out tokens, $%.6f", d.InputTokens, d.OutputTokens, d.EstimatedCostUSD)
	})

	t.Run("unanswerable", func(t *testing.T) {
		d, err := g.Classify(ctx, "How much did we spend with our cheese supplier in September 2026?", nil, nil)
		require.NoError(t, err)
		require.Equal(t, instrumentation.GateUnanswerable, d.Result)
		require.NotEmpty(t, d.RefusalReason)
		// The non-negotiable this feature adds: the second-pass Sonnet call
		// may rewrite d.RefusalReason's PROSE, but it can never change
		// d.Result away from what Haiku classified — asserted directly here
		// against a real live response, not just by code inspection.
		require.Equal(t, instrumentation.GateUnanswerable, d.Result, "Sonnet's writing pass must never change the gate's classification")
		if d.Writer != nil {
			require.Equal(t, llmclient.ModelExplanation, d.Writer.ModelUsed)
			require.Greater(t, d.Writer.InputTokens, int64(0))
			t.Logf("unanswerable writer pass: %d in / %d out tokens, $%.6f", d.Writer.InputTokens, d.Writer.OutputTokens, d.Writer.EstimatedCostUSD)
		}
		t.Logf("unanswerable smoke test: %d in / %d out tokens, $%.6f, reason=%q", d.InputTokens, d.OutputTokens, d.EstimatedCostUSD, d.RefusalReason)
	})

	t.Run("ambiguous", func(t *testing.T) {
		d, err := g.Classify(ctx, "How did we do over the weekend?", nil, nil)
		require.NoError(t, err)
		require.Equal(t, instrumentation.GateAmbiguous, d.Result)
		require.True(t, d.ClarifyingQuestion != "" || d.AssumptionStated != "")
		// Same non-negotiable as the unanswerable case above: a real writer
		// pass (only fires when a clarifying question, not an assumption,
		// was chosen) must never move d.Result off "ambiguous".
		require.Equal(t, instrumentation.GateAmbiguous, d.Result, "Sonnet's writing pass must never change the gate's classification")
		if d.ClarifyingQuestion != "" && d.Writer != nil {
			require.Equal(t, llmclient.ModelExplanation, d.Writer.ModelUsed)
			require.Greater(t, d.Writer.InputTokens, int64(0))
			t.Logf("ambiguous writer pass: %d in / %d out tokens, $%.6f", d.Writer.InputTokens, d.Writer.OutputTokens, d.Writer.EstimatedCostUSD)
		}
		t.Logf("ambiguous smoke test: %d in / %d out tokens, $%.6f, clarify=%q assume=%q", d.InputTokens, d.OutputTokens, d.EstimatedCostUSD, d.ClarifyingQuestion, d.AssumptionStated)
	})

	t.Run("answerable question costs exactly the gate's single Haiku call", func(t *testing.T) {
		d, err := g.Classify(ctx, "What was our reconciled margin on 2026-08-05?", nil, nil)
		require.NoError(t, err)
		require.Equal(t, instrumentation.GateAnswerable, d.Result)
		require.Nil(t, d.Writer, "an answerable question must never trigger the second Sonnet writing pass — no cost change for the common case")
	})
}

// TestGate_Classify_DateGroundingRegression reproduces, live against Claude
// Haiku 4.5, the exact "date-year grounding defect" recorded in
// docs/plan.md's mistakes log and docs/product-strategy.md's "Real
// evaluation results" section: a bare, no-year, relative-date question
// ("this week") whose classification varied unprompted across repeated
// calls with IDENTICAL input — sometimes answering correctly, sometimes
// asking a clarifying question about the year, and in the worst observed
// case confidently inventing a wrong year ("no data for ..., 2024") and
// refusing against that fabricated premise.
//
// The fix (buildSystemPrompt's "Date grounding" paragraph) tells the gate
// explicitly that its only "today" is dataEnd (2026-08-14), not the real
// wall-clock date, so this question should now resolve unambiguously and
// IDENTICALLY every time — this test calls Classify 3 times with the exact
// same input specifically to catch a regression to the old
// sometimes-right-sometimes-wrong behavior, not just check a single lucky
// pass. Skipped, not faked, when ANTHROPIC_API_KEY isn't set.
func TestGate_Classify_DateGroundingRegression(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set; skipping live Claude Haiku 4.5 regression test")
	}

	g := New(llmclient.New(), testDataStart, testDataEnd)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		t.Run(fmt.Sprintf("run_%d", i), func(t *testing.T) {
			d, err := g.Classify(ctx, "How did we do this week?", nil, nil)
			require.NoError(t, err)

			// The specific worst-case failure: inventing a year the data
			// doesn't cover, anywhere in the gate's own output.
			for _, s := range []string{d.ClarifyingQuestion, d.AssumptionStated, d.RefusalReason} {
				require.NotContains(t, s, "2024")
				require.NotContains(t, s, "2025")
				require.NotContains(t, s, "2027")
			}

			// There IS reconciled data for the trailing week ending
			// 2026-08-14 — a bare "this week" must never be refused as
			// unanswerable.
			require.NotEqual(t, instrumentation.GateUnanswerable, d.Result,
				"must not refuse a bare 'this week' question — the trailing week ending %s has real data", testDataEnd)

			// Asking about the boundary of "week" is a legitimate ambiguous
			// case (and is exactly what the systemPrompt's own example
			// covers by stating a default assumption instead) — but asking
			// WHICH YEAR is the specific bug this test guards against.
			require.NotContains(t, strings.ToLower(d.ClarifyingQuestion), "year",
				"must not ask which year — dataEnd grounds this unambiguously to 2026")

			t.Logf("run %d: classification=%s clarify=%q assume=%q reason=%q", i, d.Result, d.ClarifyingQuestion, d.AssumptionStated, d.RefusalReason)
		})
	}
}

// TestBuildSystemPrompt_MonthHasADeterministicAnchor is QA round 6's fix for
// a gap the existing "this week" grounding (and its dedicated
// TestGate_Classify_DateGroundingRegression above) never covered: the
// "Date grounding" paragraph gave "this week"/"last week" an explicit,
// unambiguous anchor (a trailing window ending dataEnd) after the real
// live defect recorded above, but had no equivalent rule for "this
// month"/"last month" at all — leaving the gate free to interpret "this
// month" as, say, a trailing 30-day window, while
// internal/httpapi/comparison_period.go and platforms_trend.go both use a
// real CALENDAR-month convention for the exact same phrase elsewhere in
// this product. A chat answer and the period-comparison/platform-trend
// pages could then legitimately disagree about which days "this month"
// covers for the same underlying data. This is a plain offline string
// check (no live model call, no API key needed) on the generated prompt
// text itself — the same discipline is mirrored in
// internal/explain/explain_internal_test.go for the narration step's own
// copy of this rule.
func TestBuildSystemPrompt_MonthHasADeterministicAnchor(t *testing.T) {
	prompt := buildSystemPrompt(testDataStart, testDataEnd)

	require.Contains(t, prompt, `"This month" is the calendar month`,
		`the Date grounding section must give "this month" the same explicit calendar-month anchor comparison_period.go/platforms_trend.go already use, not leave it to model judgment`)
	require.Contains(t, prompt, `"Last month" is the FULL prior calendar month`)
}

// TestBuildSystemPrompt_MixedDataAdviceQuestionsAreNotFlatlyUnanswerable is
// the offline half of a real live defect: a question phrased as "how do I
// replicate/improve X" (e.g. "how can I replicate the margin from Aug 22 on
// other days?", or the same question naming staffing/menu/promotions
// explicitly) was observed, live against a fresh -eval-no-answer-cache
// instance, being classified "unanswerable" for the WHOLE question — even
// though it always has a genuinely data-answerable core (what the tools can
// show about the named day). Worse, the gate's own Sonnet writing pass then
// produced a polished refusal that explicitly PROMISED the data-answerable
// part ("I can show you what drove Aug 22's margin...") without the system
// ever actually delivering it — the user had to ask a second, separate
// question to get data the first response already said it could give.
//
// This is a plain offline string check (no live model call) that the fix —
// a new "Mixed data-plus-advice questions" section — is actually present in
// the generated prompt, mirroring TestBuildSystemPrompt_MonthHasADeterministicAnchor's
// pattern above. internal/explain/explain_internal_test.go carries the
// narration step's own copy of this same test.
func TestBuildSystemPrompt_MixedDataAdviceQuestionsAreNotFlatlyUnanswerable(t *testing.T) {
	prompt := buildSystemPrompt(testDataStart, testDataEnd)

	// Section retitled by specs/011-inline-grounded-advice (it now also
	// carries the advice_requested signal rules) — the mixed-question
	// protection it asserts is unchanged.
	require.Contains(t, prompt, "mixed data-plus-advice questions",
		"the gate must have an explicit section telling it not to refuse a whole question just because part of it asks for advice this product can't give")
	require.Contains(t, prompt, `Do NOT classify the whole question "unanswerable"`,
		"the rule must explicitly forbid refusing the entire interaction when a data-answerable core exists underneath an advice-shaped wrapper")
}
