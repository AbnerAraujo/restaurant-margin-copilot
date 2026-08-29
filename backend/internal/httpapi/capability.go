package httpapi

import (
	"fmt"
	"regexp"
	"strings"
)

// Deterministic handling of a meta/capability question — "what do you do?",
// "how can you help me?", "what can I ask?" — the one class of question this
// product answers about ITSELF rather than about the restaurant's data.
//
// This is matched and answered without spending a single model token,
// exactly the same discipline exampleQuestions.ts's doc comment states for
// the frontend's own capability list: asking a model to describe its own
// capabilities is a fresh hallucination surface (it would happily invent
// "I can compare your food costs to industry benchmarks"), and offering a
// question the product cannot actually answer is the same class of lie as
// inventing a number. So this text is hand-written from the real, fixed
// MCP tool set (specs/001-margin-reconciliation-qa/contracts/mcp-tools.md)
// and never touches internal/ambiguity or internal/explain at all.
//
// Before this existed, a real capability question like "how can you help
// me?" was classified "unanswerable" by the ambiguity gate and refused —
// technically defensible (it isn't a question about the restaurant's
// margin data) but a genuinely bad first experience for an owner who just
// wants to know what to ask. Ownership: an owner meta-question deserves a
// warm, concrete answer, not a refusal that reads as the product not
// understanding its own job.
var capabilityQuestionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bwhat (do|can) you do\b`),
	regexp.MustCompile(`\bwhat (are|is) you (capable of|able to do|for)\b`),
	regexp.MustCompile(`\bhow (can|do) you help\b`),
	regexp.MustCompile(`\bwhat can (i|you) ask\b`),
	regexp.MustCompile(`\bwhat should i ask\b`),
	regexp.MustCompile(`\bwhat kind(s)? of questions\b`),
	regexp.MustCompile(`\bwhat questions (can|do) (i|you)\b`),
	regexp.MustCompile(`\bhelp me think\b`),
	regexp.MustCompile(`\bwhat('s| is) this (app|product|tool|thing) (do|for)\b`),
	regexp.MustCompile(`^(hi|hello|hey)[,.! ]*$`),
}

// isCapabilityQuestion reports whether question reads as a meta-question
// about the product itself rather than a question about restaurant data.
// Deliberately conservative (whole-phrase patterns, not single keywords
// like "help" or "ask" that appear constantly in ordinary data questions,
// e.g. "can you help me understand the discrepancy on 2026-08-05?") —
// a false positive here would silently swap a real data question for a
// canned capability blurb, which is worse than the refusal it replaces.
func isCapabilityQuestion(question string) bool {
	q := strings.ToLower(strings.TrimSpace(question))
	if q == "" {
		return false
	}
	for _, pattern := range capabilityQuestionPatterns {
		if pattern.MatchString(q) {
			return true
		}
	}
	return false
}

// capabilityAnswerText renders the hand-written capability description,
// grounded in the seven real MCP tools (mcp-tools.md) and the actual data
// window this instance was seeded with — never a placeholder date range.
// Mirrors frontend/src/components/Chat/exampleQuestions.ts's
// CAPABILITY_SUMMARY in substance (one written by hand for Go, one for
// TypeScript, since the two runtimes don't share a source file) — if the
// tool set changes, both need updating together.
func capabilityAnswerText(dataStart, dataEnd string) string {
	coverage := "for the period this data covers"
	if dataStart != "" && dataEnd != "" {
		coverage = fmt.Sprintf("for %s through %s", dataStart, dataEnd)
	}
	return fmt.Sprintf(
		`I'm your margin steward for this restaurant — I don't guess, I only tell you what the reconciled numbers actually show, %s. Here's what I can dig into:

- **A single day's numbers** — sales by platform, commissions, refunds, input costs, and the margin that's left. ("How did we do on 2026-08-07?")
- **A period's totals and its best/worst day** — the whole window summed and ranked in one go. ("What was our total margin for the two weeks, and which day was strongest?")
- **Week-over-week or period-over-period comparisons** — margin delta between any two ranges you name. ("Compare last week's margin to the week before.")
- **Discrepancies caught during reconciliation** — duplicate orders, refunds, anomaly flags, with the source rows behind each one.
- **Promotion ROI** — a specific campaign's return, or every campaign that's losing money in a period.
- **How the delivery platforms compare** — commission rates and promo spend, iFood against Just Eat Takeaway, side by side.

If a question needs data this product doesn't have — a date outside the window, a platform or supplier that was never in the fixture set — I'll say so plainly instead of estimating. A confidently wrong number is worse than an honest "I don't have that."`,
		coverage,
	)
}
