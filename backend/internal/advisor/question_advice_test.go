package advisor

// Offline tests for specs/011-inline-grounded-advice's dynamic prompt
// assembly (question_advice.go) — the same zero-API-call split
// advisor_test.go documents. The central claims under test: prompt
// content is a deterministic function of which tools actually ran; the
// verbatim non-fabrication rules are always present; the five 009 kind
// templates never leak onto this path; and no call is ever attempted
// without a question and real grounding.

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestQuestionAdviceKindIsALedgerKindNotATeaserKind(t *testing.T) {
	// POST /api/business-insight validates with KnownKind — question_advice
	// must stay rejected there (spec 011 FR-010 / User Story 3 scenario 2).
	if KnownKind(KindQuestionAdvice) {
		t.Fatal("KnownKind(question_advice) = true — the closed five-kind teaser set must stay closed")
	}
}

func TestBuildQuestionSystemPrompt_CarriesTheVerbatimNonFabricationRules(t *testing.T) {
	prompt := BuildQuestionSystemPrompt([]ToolResult{{Name: "get_period_totals", ResultJSON: "{}"}})

	// Rules 1 and 2 must match baseSystemPrompt's own wording verbatim
	// (spec FR-004) — asserted against the exact sentences, so a reworded
	// "equivalent" fails the build.
	for _, want := range []string{
		"NEVER state a fact about this specific restaurant that is not literally present in the JSON you were given.",
		"NEVER invent statistics, percentages, dollar amounts, study names, or source names.",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("question prompt missing the verbatim hard rule %q", want)
		}
		if !strings.Contains(baseSystemPrompt, want) {
			t.Errorf("drift: baseSystemPrompt no longer contains %q — the verbatim-rule claim is broken at the source", want)
		}
	}
	// The added rule for ungroundable question parts (spec FR-004's one
	// extension) must be present too.
	if !strings.Contains(prompt, "say so plainly in one sentence") {
		t.Error("question prompt missing the ungroundable-part rule")
	}
}

func TestBuildQuestionSystemPrompt_SelectsExactlyTheSectionsForToolsPresent(t *testing.T) {
	prompt := BuildQuestionSystemPrompt([]ToolResult{
		{Name: "compare_platform_economics", ResultJSON: "{}"},
		{Name: "get_period_totals", ResultJSON: "{}"},
	})

	for _, present := range []string{
		toolGuidance["compare_platform_economics"],
		toolGuidance["get_period_totals"],
	} {
		if !strings.Contains(prompt, present) {
			t.Errorf("prompt missing the guidance section for a tool that ran")
		}
	}
	for name, guidance := range toolGuidance {
		if name == "compare_platform_economics" || name == "get_period_totals" {
			continue
		}
		if strings.Contains(prompt, guidance) {
			t.Errorf("prompt includes guidance for %s, a tool that never ran — selection must be a function of the real grounding set", name)
		}
	}
}

func TestBuildQuestionSystemPrompt_DeduplicatesRepeatedTools(t *testing.T) {
	prompt := BuildQuestionSystemPrompt([]ToolResult{
		{Name: "get_daily_summary", ResultJSON: `{"date":"2026-08-01"}`},
		{Name: "get_daily_summary", ResultJSON: `{"date":"2026-08-02"}`},
	})
	if got := strings.Count(prompt, toolGuidance["get_daily_summary"]); got != 1 {
		t.Fatalf("guidance section appears %d times for a tool that ran twice, want exactly 1", got)
	}
}

func TestBuildQuestionSystemPrompt_IsDeterministicRegardlessOfToolOrder(t *testing.T) {
	a := BuildQuestionSystemPrompt([]ToolResult{
		{Name: "list_discrepancies", ResultJSON: "{}"},
		{Name: "get_margin_delta", ResultJSON: "{}"},
	})
	b := BuildQuestionSystemPrompt([]ToolResult{
		{Name: "get_margin_delta", ResultJSON: "{}"},
		{Name: "list_discrepancies", ResultJSON: "{}"},
	})
	if a != b {
		t.Fatal("prompt assembly depends on tool-invocation order — it must be canonical")
	}
}

func TestBuildQuestionSystemPrompt_NeverConsultsTheFiveKindTemplates(t *testing.T) {
	// Every tool at once — the widest possible prompt this path can build
	// — must still contain none of the 009 teaser-kind templates: this
	// path has no kind, and a kind template sneaking in would be exactly
	// the fixed-lookup design this spec replaces.
	all := make([]ToolResult, 0, len(guidanceOrder))
	for _, name := range guidanceOrder {
		all = append(all, ToolResult{Name: name, ResultJSON: "{}"})
	}
	prompt := BuildQuestionSystemPrompt(all)
	for kind, guidance := range kindGuidance {
		if strings.Contains(prompt, guidance) {
			t.Errorf("question prompt contains kindGuidance[%q] — the 009 templates must never be consulted on this path", kind)
		}
	}
	if strings.Contains(prompt, "Insight kind:") {
		t.Error("question prompt carries the teaser path's 'Insight kind:' framing")
	}
}

func TestGuidanceOrderCoversEveryToolGuidanceEntryAndNothingElse(t *testing.T) {
	if len(guidanceOrder) != len(toolGuidance) {
		t.Fatalf("guidanceOrder has %d entries, toolGuidance has %d — they must stay in lockstep", len(guidanceOrder), len(toolGuidance))
	}
	for _, name := range guidanceOrder {
		g, ok := toolGuidance[name]
		if !ok {
			t.Errorf("guidanceOrder names %q but toolGuidance has no such section", name)
		}
		if strings.TrimSpace(g) == "" {
			t.Errorf("toolGuidance[%q] is empty", name)
		}
	}
}

func TestEveryToolGuidanceAnchorsBackToTheJSON(t *testing.T) {
	// Same structural check advisor_test.go applies to kindGuidance: a
	// section that forgot to point back at the shown data would let the
	// model drift into fully generic content.
	for name, guidance := range toolGuidance {
		if !strings.Contains(guidance, "JSON") {
			t.Errorf("toolGuidance[%q] never anchors the advice back to the JSON it was shown", name)
		}
	}
}

func TestComposeQuestionUserMessageCarriesQuestionAndEveryResultVerbatim(t *testing.T) {
	msg := composeQuestionUserMessage("how can I improve my margin overall?", []ToolResult{
		{Name: "get_period_totals", ResultJSON: `{"margin_total":"1234.56"}`},
		{Name: "compare_platform_economics", ResultJSON: `{"platforms":[]}`},
	})
	for _, want := range []string{
		"how can I improve my margin overall?",
		"get_period_totals",
		`{"margin_total":"1234.56"}`,
		"compare_platform_economics",
		`{"platforms":[]}`,
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("composed user message missing %q:\n%s", want, msg)
		}
	}
}

func TestAdviseOnQuestionRefusesEmptyQuestionWithoutAnyAPICall(t *testing.T) {
	a := New(nil) // a nil client panics if the call is ever attempted
	_, err := a.AdviseOnQuestion(context.Background(), "   ", []ToolResult{{Name: "t", ResultJSON: "{}"}})
	if !errors.Is(err, ErrEmptyQuestion) {
		t.Fatalf("err = %v, want ErrEmptyQuestion", err)
	}
}

func TestAdviseOnQuestionRefusesEmptyGroundingWithoutAnyAPICall(t *testing.T) {
	a := New(nil)
	_, err := a.AdviseOnQuestion(context.Background(), "how can I improve my margin?", nil)
	if !errors.Is(err, ErrNoToolResults) {
		t.Fatalf("err = %v, want ErrNoToolResults", err)
	}
}
