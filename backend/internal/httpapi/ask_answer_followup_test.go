package httpapi

// Tests for the ANSWER-side follow-up mechanism (PreviousExchange /
// ambiguity.ComposeAnswerFollowUp) — the counterpart to
// ask_clarification_test.go's tests for the CLARIFICATION-side mechanism
// (PendingClarification / ambiguity.ComposeFollowUp). Same shape,
// deliberately: a real follow-up to an ANSWER ("and the day before?", "why?")
// had no equivalent mechanism at all before this, and the highest-risk
// regression it could introduce is identical to the clarification case's —
// a bare repeated reply text wrongly cache-hitting a DIFFERENT prior
// exchange's answer.

import (
	"strings"
	"testing"
)

func previousExchangeBody(question, prevQuestion, prevAnswer string) string {
	return `{
  "question": ` + quote(question) + `,
  "previous_exchange": {
    "question": ` + quote(prevQuestion) + `,
    "answer_text": ` + quote(prevAnswer) + `
  }
}`
}

func TestAnswerFollowUpReachesTheGateWithItsContextAttached(t *testing.T) {
	h := newAskHarness(t)

	h.askWithBody(t, previousExchangeBody(
		"and the day before?",
		"What was our margin on 2026-08-05?",
		"Margin on 2026-08-05 was $612.40.",
	))

	if h.gate.lastPreviousAnswer == nil {
		t.Fatal("the gate was given no previous-exchange context — a bare follow-up like \"and the day before?\" is unclassifiable without it")
	}
	if h.gate.lastPreviousAnswer.Question != "What was our margin on 2026-08-05?" {
		t.Errorf("previous Question = %q", h.gate.lastPreviousAnswer.Question)
	}
	if h.gate.lastPreviousAnswer.AnswerText != "Margin on 2026-08-05 was $612.40." {
		t.Errorf("previous AnswerText = %q", h.gate.lastPreviousAnswer.AnswerText)
	}
	// The gate still receives the literal follow-up as the question text;
	// the context is a separate typed field, not smuggled into it.
	if h.gate.lastQuestion != "and the day before?" {
		t.Errorf("gate question = %q, want the literal follow-up %q", h.gate.lastQuestion, "and the day before?")
	}
	// pending_clarification and previous_exchange are mutually exclusive —
	// this request only set the latter.
	if h.gate.lastPending != nil {
		t.Error("a previous_exchange-only request must reach the gate with nil pending clarification context")
	}
}

func TestExplainNarratesTheAnswerFollowUpsResolvedTextNotTheBareFragment(t *testing.T) {
	h := newAskHarness(t)

	h.askWithBody(t, previousExchangeBody(
		"and the day before?",
		"What was our margin on 2026-08-05?",
		"Margin on 2026-08-05 was $612.40.",
	))

	if h.explainer.lastQuestion == "and the day before?" {
		t.Fatal("the explanation step was handed the bare follow-up — it would be narrating a fragment the gate had already resolved")
	}
	for _, want := range []string{"and the day before?", "What was our margin on 2026-08-05?", "$612.40", "Previous exchange context"} {
		if !strings.Contains(h.explainer.lastQuestion, want) {
			t.Errorf("resolved question is missing %q; got:\n%s", want, h.explainer.lastQuestion)
		}
	}
}

// The regression this design most easily introduces, mirroring the
// clarification-path test of the same shape: keying the cache on the bare
// follow-up text. The same phrase ("and the day before?") following TWO
// DIFFERENT prior answers must never collide in the cache — the second
// must be answered fresh, on its own terms, never served the first's answer.
func TestTheSameBareFollowUpAfterTwoDifferentAnswersDoesNotCollideInTheCache(t *testing.T) {
	h := newAskHarness(t)

	h.explainer.result.AnswerText = "Margin on 2026-08-04 was $580.10."
	first := h.askWithBody(t, previousExchangeBody(
		"and the day before?",
		"What was our margin on 2026-08-05?",
		"Margin on 2026-08-05 was $612.40.",
	))

	h.explainer.result.AnswerText = "Margin on 2026-08-08 was $701.05."
	second := h.askWithBody(t, previousExchangeBody(
		"and the day before?",
		"What was our margin on 2026-08-09?",
		"Margin on 2026-08-09 was $690.00.",
	))

	if second.Cache != nil {
		t.Fatal("the same bare follow-up text after a DIFFERENT prior answer hit the cache — the cache key must include the previous-exchange context, not just the follow-up text")
	}
	if first.AnswerText == second.AnswerText {
		t.Errorf("both follow-ups produced %q; the second must be answered on its own terms", first.AnswerText)
	}
	if h.gate.calls != 2 || h.explainer.calls != 2 {
		t.Errorf("gate=%d explain=%d, want 2 and 2", h.gate.calls, h.explainer.calls)
	}
}

func TestTheSameAnswerFollowUpAskedTwiceStillHitsTheCache(t *testing.T) {
	h := newAskHarness(t)
	body := previousExchangeBody(
		"and the day before?",
		"What was our margin on 2026-08-05?",
		"Margin on 2026-08-05 was $612.40.",
	)

	h.askWithBody(t, body)
	second := h.askWithBody(t, body)

	if second.Cache == nil || !second.Cache.Hit {
		t.Fatal("an identical answer follow-up must still be cacheable — context-aware keying must not disable caching for the whole answer-follow-up path")
	}
	if h.gate.calls != 1 {
		t.Errorf("gate ran %d times, want 1", h.gate.calls)
	}
}

func TestInstrumentationRecordsTheAnswerFollowUpsLiteralTypedText(t *testing.T) {
	h := newAskHarness(t)

	h.askWithBody(t, previousExchangeBody(
		"and the day before?",
		"What was our margin on 2026-08-05?",
		"Margin on 2026-08-05 was $612.40.",
	))

	if len(h.instrumentation.records) == 0 {
		t.Fatal("no interaction was logged")
	}
	for _, record := range h.instrumentation.records {
		if record.QuestionText != "and the day before?" {
			t.Errorf("question_text = %q, want the literal typed follow-up %q", record.QuestionText, "and the day before?")
		}
	}
}

// The two mechanisms must never both fire: a well-formed request carries at
// most one of pending_clarification/previous_exchange (ChatPanel.tsx derives
// them from mutually exclusive prior-message kinds). If a malformed request
// somehow sent both, pending_clarification must win — it is the older,
// higher-value mechanism, and this is a defensive default never expected to
// matter in practice.
func TestPendingClarificationTakesPrecedenceIfBothSomehowArrive(t *testing.T) {
	h := newAskHarness(t)

	h.askWithBody(t, `{
  "question": "yes",
  "pending_clarification": {
    "original_question": "Which days had discrepancies this month?",
    "clarifying_question": "Do you mean August 2026, or a different calendar month?"
  },
  "previous_exchange": {
    "question": "What was our margin on 2026-08-05?",
    "answer_text": "Margin on 2026-08-05 was $612.40."
  }
}`)

	if h.gate.lastPending == nil {
		t.Fatal("pending_clarification must still reach the gate when both fields are present")
	}
	if h.gate.lastPending.OriginalQuestion != "Which days had discrepancies this month?" {
		t.Errorf("OriginalQuestion = %q", h.gate.lastPending.OriginalQuestion)
	}
}

func TestANormalQuestionIsUnaffectedByTheAnswerFollowUpPath(t *testing.T) {
	h := newAskHarness(t)

	h.ask(t, "How did we do on 2026-08-07?")

	if h.gate.lastPreviousAnswer != nil {
		t.Error("a question with no previous_exchange must reach the gate with nil context")
	}
}
