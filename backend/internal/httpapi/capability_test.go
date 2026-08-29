package httpapi

import (
	"strings"
	"testing"
)

func TestIsCapabilityQuestionMatchesRealPhrasings(t *testing.T) {
	for _, q := range []string{
		"what do you do?",
		"What can you do",
		"how can you help me?",
		"How do you help",
		"what can I ask?",
		"What should I ask you",
		"what kind of questions can I ask",
		"help me think about what to ask",
		"what's this app for?",
		"hi",
		"Hello!",
	} {
		if !isCapabilityQuestion(q) {
			t.Errorf("isCapabilityQuestion(%q) = false, want true", q)
		}
	}
}

func TestIsCapabilityQuestionDoesNotMatchRealDataQuestions(t *testing.T) {
	for _, q := range []string{
		"How did we do on 2026-08-07?",
		"can you help me understand the discrepancy on 2026-08-05?",
		"Which promotions lost money between 2026-08-01 and 2026-08-14?",
		"what was our margin last week",
		"",
		"   ",
	} {
		if isCapabilityQuestion(q) {
			t.Errorf("isCapabilityQuestion(%q) = true, want false — this is a real data question, not a meta-question about the product", q)
		}
	}
}

func TestCapabilityAnswerTextUsesTheRealDataWindowWhenGiven(t *testing.T) {
	text := capabilityAnswerText("2026-08-01", "2026-08-14")
	if !strings.Contains(text, "2026-08-01") || !strings.Contains(text, "2026-08-14") {
		t.Errorf("capability answer does not state the real data window: %s", text)
	}
}

func TestCapabilityAnswerTextFallsBackGracefullyWithNoDataWindow(t *testing.T) {
	text := capabilityAnswerText("", "")
	if strings.Contains(text, " through , ") {
		t.Errorf("capability answer rendered an empty date placeholder: %s", text)
	}
}

// TestCapabilityQuestionNeverReachesGateOrExplainer proves the whole point
// of this feature: a meta-question about the product costs nothing and
// never touches either model call, unlike every other question path.
func TestCapabilityQuestionNeverReachesGateOrExplainer(t *testing.T) {
	h := newAskHarness(t)

	resp := h.ask(t, "How can you help me?")

	if resp.Status != "answered" {
		t.Fatalf("status = %q, want %q", resp.Status, "answered")
	}
	if resp.AnswerText == "" {
		t.Fatal("expected a non-empty capability answer")
	}
	if len(resp.Interactions) != 0 {
		t.Errorf("Interactions = %v, want none — a capability question must never bill a model call", resp.Interactions)
	}
	if h.gate.calls != 0 {
		t.Errorf("gate.calls = %d, want 0", h.gate.calls)
	}
	if h.explainer.calls != 0 {
		t.Errorf("explainer.calls = %d, want 0", h.explainer.calls)
	}
	if len(h.instrumentation.records) != 0 {
		t.Errorf("instrumentation records = %d, want 0 — no real model call happened to log", len(h.instrumentation.records))
	}
}

// TestCapabilityQuestionSkippedForAClarificationReply proves a bare reply
// to an active clarifying question is never mistaken for a fresh
// capability question, even one that happens to match a pattern.
func TestCapabilityQuestionSkippedForAClarificationReply(t *testing.T) {
	h := newAskHarness(t)

	h.askWithBody(t, `{
	  "question": "hi",
	  "pending_clarification": {
	    "original_question": "Which days had discrepancies this month?",
	    "clarifying_question": "Do you mean August 2026, or a different month?"
	  }
	}`)

	if h.gate.calls != 1 {
		t.Errorf("gate.calls = %d, want 1 — a clarification reply must still reach the gate even if its text matches a capability pattern", h.gate.calls)
	}
}
