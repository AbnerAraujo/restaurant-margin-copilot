package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ambiguity"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/instrumentation"
)

// askWithBody posts a raw JSON body so the wire contract itself is exercised.
func (h *askHarness) askWithBody(t *testing.T, body string) AskResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	h.handler(recorder, httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(body)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", recorder.Code, recorder.Body.String())
	}
	var response AskResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	return response
}

const followUpBody = `{
  "question": "yes",
  "pending_clarification": {
    "original_question": "Which days had discrepancies this month?",
    "clarifying_question": "Do you mean the month of August 2026 (2026-08-01 through 2026-08-14), or a different calendar month?"
  }
}`

func TestFollowUpReplyReachesTheGateWithItsContextAttached(t *testing.T) {
	h := newAskHarness(t)

	h.askWithBody(t, followUpBody)

	if h.gate.lastPending == nil {
		t.Fatal("the gate was given no clarification context — a bare reply like \"yes\" is unclassifiable without it, which is exactly the defect that produced \"No question was asked\" refusals")
	}
	if h.gate.lastPending.OriginalQuestion != "Which days had discrepancies this month?" {
		t.Errorf("OriginalQuestion = %q", h.gate.lastPending.OriginalQuestion)
	}
	if !strings.Contains(h.gate.lastPending.ClarifyingQuestion, "August 2026") {
		t.Errorf("ClarifyingQuestion = %q", h.gate.lastPending.ClarifyingQuestion)
	}
	// The gate still receives the literal reply as the question text; the
	// context is a separate typed field, not smuggled into it.
	if h.gate.lastQuestion != "yes" {
		t.Errorf("gate question = %q, want the literal reply %q", h.gate.lastQuestion, "yes")
	}
}

func TestExplainNarratesTheResolvedQuestionNotTheBareFragment(t *testing.T) {
	h := newAskHarness(t)

	h.askWithBody(t, followUpBody)

	if h.explainer.lastQuestion == "yes" {
		t.Fatal("the explanation step was handed the bare reply — it would be narrating a fragment the gate had already resolved")
	}
	for _, want := range []string{"yes", "Which days had discrepancies this month?", "Follow-up context"} {
		if !strings.Contains(h.explainer.lastQuestion, want) {
			t.Errorf("resolved question is missing %q; got:\n%s", want, h.explainer.lastQuestion)
		}
	}
}

// The regression this design most easily introduces: keying the cache on the
// bare reply. Two different clarifications answered "yes" would collide and
// the second would be served the first one's answer — a wrong answer served
// confidently, the one failure mode this product exists to avoid.
func TestTwoDifferentClarificationsAnsweredYesDoNotCollideInTheCache(t *testing.T) {
	h := newAskHarness(t)

	h.explainer.result.AnswerText = "Discrepancies answer."
	first := h.askWithBody(t, followUpBody)

	h.explainer.result.AnswerText = "Promotions answer."
	second := h.askWithBody(t, `{
	  "question": "yes",
	  "pending_clarification": {
	    "original_question": "Which promotions lost money?",
	    "clarifying_question": "Do you mean across the whole period, or only the last week?"
	  }
	}`)

	if second.Cache != nil {
		t.Fatal("a \"yes\" answering a DIFFERENT clarifying question hit the cache — the cache key must include the clarification context, not just the reply text")
	}
	if first.AnswerText == second.AnswerText {
		t.Errorf("both follow-ups produced %q; the second must be answered on its own terms", first.AnswerText)
	}
	if h.gate.calls != 2 || h.explainer.calls != 2 {
		t.Errorf("gate=%d explain=%d, want 2 and 2", h.gate.calls, h.explainer.calls)
	}
}

func TestTheSameFollowUpAskedTwiceStillHitsTheCache(t *testing.T) {
	h := newAskHarness(t)

	h.askWithBody(t, followUpBody)
	second := h.askWithBody(t, followUpBody)

	if second.Cache == nil || !second.Cache.Hit {
		t.Fatal("an identical follow-up must still be cacheable — context-aware keying must not disable caching for the whole clarification path")
	}
	if h.gate.calls != 1 {
		t.Errorf("gate ran %d times, want 1", h.gate.calls)
	}
}

func TestInstrumentationRecordsWhatTheUserActuallyTyped(t *testing.T) {
	h := newAskHarness(t)

	h.askWithBody(t, followUpBody)

	if len(h.instrumentation.records) == 0 {
		t.Fatal("no interaction was logged")
	}
	for _, record := range h.instrumentation.records {
		// Constitution Principle VI is about honest records: the log must say
		// what was typed, not a composed sentence the user never wrote.
		if record.QuestionText != "yes" {
			t.Errorf("question_text = %q, want the literal typed reply %q", record.QuestionText, "yes")
		}
	}
}

func TestANormalQuestionIsUnaffectedByTheFollowUpPath(t *testing.T) {
	h := newAskHarness(t)

	h.ask(t, "How did we do on 2026-08-07?")

	if h.gate.lastPending != nil {
		t.Error("a question with no pending clarification must reach the gate with nil context")
	}
	if h.explainer.lastQuestion != "How did we do on 2026-08-07?" {
		t.Errorf("explain question = %q, want it passed through untouched", h.explainer.lastQuestion)
	}
}

func TestComposeFollowUpIsDeterministicAndInertodWithoutContext(t *testing.T) {
	if got := ambiguity.ComposeFollowUp("plain question", nil); got != "plain question" {
		t.Errorf("with no context ComposeFollowUp must return the question unchanged, got %q", got)
	}
	empty := &ambiguity.PendingClarification{OriginalQuestion: "x", ClarifyingQuestion: "   "}
	if got := ambiguity.ComposeFollowUp("plain question", empty); got != "plain question" {
		t.Errorf("an empty clarifying question must be treated as no context, got %q", got)
	}
	ctxA := &ambiguity.PendingClarification{OriginalQuestion: "orig", ClarifyingQuestion: "clar"}
	if ambiguity.ComposeFollowUp("reply", ctxA) != ambiguity.ComposeFollowUp("reply", ctxA) {
		t.Error("composition must be deterministic — it is the cache key")
	}
}

func TestClarificationResponseStillCarriesTheGatesQuestion(t *testing.T) {
	h := newAskHarness(t)
	h.gate.decision = ambiguity.Decision{
		Result:             instrumentation.GateAmbiguous,
		ClarifyingQuestion: "Do you mean August 2026, or a different month?",
	}

	response := h.ask(t, "Which days had discrepancies this month?")

	if response.Status != "clarification_needed" {
		t.Fatalf("status = %q, want clarification_needed", response.Status)
	}
	if response.ClarifyingQuestion == "" {
		t.Error("the clarifying question must reach the client — it is what the client echoes back as pending_clarification")
	}
}
