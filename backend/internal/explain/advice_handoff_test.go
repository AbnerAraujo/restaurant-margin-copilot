package explain

// Offline tests for specs/011-inline-grounded-advice's narration handoff
// (FR-011): when an inline advisor call will follow the narration, the
// narration must present the data without declining the advice part the
// advisor is about to deliver — and the system prompt must carry the
// exception that makes those two instructions coexist.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildSystemPrompt_CarriesTheAdviceHandoffException(t *testing.T) {
	prompt := buildSystemPrompt("2024-08-01", "2026-08-14")

	require.Contains(t, prompt, "EXCEPTION: when the user turn carries an explicit upstream note that a separate advisor step will handle the advice part",
		"the mixed-question rule must carry the handoff exception, or the narration will decline advice the advisor is about to deliver in the same reply")
	// The pre-011 decline behavior must survive for the no-advisor case —
	// the exception is scoped to the note's presence, never a blanket
	// removal of the boundary statement.
	require.Contains(t, prompt, "state directly that recommending staffing, menu, or other operational/business decisions isn't something this tool computes or has data for")
}

func TestAdviceHandoffNote_SaysWhatItMustAndNothingItMustNot(t *testing.T) {
	for _, want := range []string{
		"answer the data-answerable part in full",
		"do NOT state that advice cannot be given",
		"do NOT add recommendations of your own",
	} {
		if !strings.Contains(AdviceHandoffNote, want) {
			t.Errorf("AdviceHandoffNote missing %q", want)
		}
	}
}
