package ambiguity

// Pure unit tests over daterange.go's deterministic date-range pre-check —
// the arithmetic Constitution Principle I requires to live in Go, not in a
// model. Zero API calls, zero cost, fully reproducible: the whole point of
// the pre-check is that these verdicts never depend on a model's mood.

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The multi-year window from the real 2026-08-29 incident: 730 synthetic
// days plus the 14-day fixture, spanning three calendar years.
const (
	incidentStart = "2024-08-01"
	incidentEnd   = "2026-08-14"
)

func TestCheckExplicitDateRange_Verdicts(t *testing.T) {
	cases := []struct {
		name          string
		question      string
		wantMentions  int
		wantAllOut    bool
		wantFirstIn   bool
		wantFirstText string
	}{
		{
			// THE incident question: fully in range, previously refused by
			// Haiku doing the comparison itself.
			name: "month-year in range across year boundary", question: "What was our margin for July 2026?",
			wantMentions: 1, wantAllOut: false, wantFirstIn: true, wantFirstText: "July 2026",
		},
		{
			name: "month-year before the window", question: "How did July 2023 go?",
			wantMentions: 1, wantAllOut: true, wantFirstIn: false, wantFirstText: "July 2023",
		},
		{
			name: "month-year after the window", question: "Show me September 2026 please",
			wantMentions: 1, wantAllOut: true, wantFirstIn: false, wantFirstText: "September 2026",
		},
		{
			name: "month partially overlapping the window counts as in range", question: "How was August 2026?",
			wantMentions: 1, wantAllOut: false, wantFirstIn: true, wantFirstText: "August 2026",
		},
		{
			name: "ISO date in range", question: "What was our margin on 2026-08-05?",
			wantMentions: 1, wantAllOut: false, wantFirstIn: true, wantFirstText: "2026-08-05",
		},
		{
			name: "ISO date out of range", question: "What was our margin on 2023-01-15?",
			wantMentions: 1, wantAllOut: true, wantFirstIn: false, wantFirstText: "2023-01-15",
		},
		{
			name: "month day comma year in range", question: "sales on August 9, 2025?",
			wantMentions: 1, wantAllOut: false, wantFirstIn: true, wantFirstText: "August 9, 2025",
		},
		{
			name: "day month year in range", question: "sales on 9 August 2025",
			wantMentions: 1, wantAllOut: false, wantFirstIn: true, wantFirstText: "9 August 2025",
		},
		{
			name: "abbreviated month with ordinal", question: "how about Aug 9th, 2025?",
			wantMentions: 1, wantAllOut: false, wantFirstIn: true, wantFirstText: "Aug 9th, 2025",
		},
		{
			name: "numeric day-first date in range", question: "margin on 14/08/2026?",
			wantMentions: 1, wantAllOut: false, wantFirstIn: true, wantFirstText: "14/08/2026",
		},
		{
			// 12/31 is only calendar-valid month-first, and lands out of
			// range: the single-reading path must still refuse.
			name: "numeric month-first-only date out of range", question: "revenue on 12/31/2023",
			wantMentions: 1, wantAllOut: true, wantFirstIn: false, wantFirstText: "12/31/2023",
		},
		{
			name: "bare year before the window", question: "how was 2023?",
			wantMentions: 1, wantAllOut: true, wantFirstIn: false, wantFirstText: "2023",
		},
		{
			name: "bare year inside the window", question: "how was 2025 overall?",
			wantMentions: 1, wantAllOut: false, wantFirstIn: true, wantFirstText: "2025",
		},
		{
			// Mixed: one in, one out — never a deterministic refusal, both
			// verdicts reported.
			name: "mixed in and out of range", question: "compare July 2026 against July 2023",
			wantMentions: 2, wantAllOut: false, wantFirstIn: true, wantFirstText: "July 2026",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			check := checkExplicitDateRange(tc.question, incidentStart, incidentEnd)
			require.NotNil(t, check)
			require.Len(t, check.Verdicts, tc.wantMentions)
			require.Equal(t, tc.wantAllOut, check.AllOutOfRange)
			require.Equal(t, tc.wantFirstText, check.Verdicts[0].Text)
			require.Equal(t, tc.wantFirstIn, check.Verdicts[0].InRange)
		})
	}
}

// Questions with no explicit, fully-specified date must be left entirely to
// the model — relative and year-less phrasing is a language-understanding
// job, and a false deterministic refusal would be worse than one more model
// classification.
func TestCheckExplicitDateRange_LeavesNonExplicitDatesToTheModel(t *testing.T) {
	for _, question := range []string{
		"How did we do last month?",
		"How was the weekend?",
		"What was our margin on August 3rd?", // year-less: resolution rule, not range arithmetic
		"How are things recently?",
		"What was our best day?",
	} {
		t.Run(question, func(t *testing.T) {
			require.Nil(t, checkExplicitDateRange(question, incidentStart, incidentEnd),
				"no explicit fully-specified date — the pre-check must stand aside")
		})
	}
}

// Four-digit numbers that are not standalone calendar years must never be
// misread as dates.
func TestFindExplicitDateMentions_NoFalsePositives(t *testing.T) {
	for _, question := range []string{
		"what happened with order #2024?",
		"we spent $2025 on ads",
		"is the app on v2.2026 yet?",
		"invoice 20250814",
	} {
		t.Run(question, func(t *testing.T) {
			require.Empty(t, findExplicitDateMentions(question))
		})
	}
}

// A calendar-invalid explicit day must not produce a mention (and must not
// crash) — February 30th does not exist in any year.
func TestFindExplicitDateMentions_RejectsImpossibleDates(t *testing.T) {
	mentions := findExplicitDateMentions("what about 2025-02-30?")
	require.Empty(t, mentions)
}

func TestCheckExplicitDateRange_NilOnMalformedWindow(t *testing.T) {
	require.Nil(t, checkExplicitDateRange("July 2026?", "not-a-date", incidentEnd),
		"a malformed window must degrade to the model path, never refuse on a bad bound")
}

func TestPrecheckRefusalReason_NamesTheFactsExactly(t *testing.T) {
	reason := precheckRefusalReason(
		[]mentionVerdict{{Text: "July 2023", InRange: false}},
		incidentStart, incidentEnd,
	)
	require.Contains(t, reason, incidentStart)
	require.Contains(t, reason, incidentEnd)
	require.Contains(t, reason, `"July 2023"`)
}

func TestPrecheckFactNote_CarriesEveryVerdict(t *testing.T) {
	note := precheckFactNote(
		[]mentionVerdict{
			{Text: "July 2026", InRange: true},
			{Text: "July 2023", InRange: false},
		},
		incidentStart, incidentEnd,
	)
	require.Contains(t, note, `"July 2026": IN RANGE`)
	require.Contains(t, note, `"July 2023": OUT OF RANGE`)
	require.Contains(t, note, "[Deterministic date-range check")
}
