package mcptools

// Pure, model-independent unit tests over matchCampaignID/normalizeCampaignRef
// — no DB, no API calls, zero cost. They reproduce the exact failing inputs
// from the evaluation harness report (docs/plan.md mistakes log,
// docs/product-strategy.md "Real evaluation results", C3/A9) plus the
// boundary cases the bounded-match design (campaign_match.go) exists to
// get right: it must resolve a real shortened/display-name reference, but
// must never guess when the match is ambiguous or the campaign is
// genuinely unknown.

import "testing"

// knownCampaignIDs mirrors the real, persisted campaign_id set this
// product's dataset actually has (backend/cmd/gendata/opening/README.md).
var knownCampaignIDs = []string{
	"IFOOD-CAMP-BOOST01",
	"JET-CAMP-LUNCHFIX",
	"IFOOD-CAMP-WEEKEND",
	"JET-CAMP-NEWMENU",
}

func TestMatchCampaignID_ExactID(t *testing.T) {
	got := matchCampaignID("JET-CAMP-LUNCHFIX", knownCampaignIDs)
	if got != "JET-CAMP-LUNCHFIX" {
		t.Fatalf("exact id match: got %q, want JET-CAMP-LUNCHFIX", got)
	}
}

func TestMatchCampaignID_ExactID_CaseInsensitive(t *testing.T) {
	got := matchCampaignID("jet-camp-lunchfix", knownCampaignIDs)
	if got != "JET-CAMP-LUNCHFIX" {
		t.Fatalf("case-insensitive exact match: got %q, want JET-CAMP-LUNCHFIX", got)
	}
}

// TestMatchCampaignID_FullDisplayName reproduces the exact failing input
// from the evaluation report: asking about a real promotion by its full
// human-readable name, which previously triggered a hallucinated "not in
// the data" refusal even though JET-CAMP-LUNCHFIX is real and computable.
func TestMatchCampaignID_FullDisplayName(t *testing.T) {
	got := matchCampaignID("Banner Ad - Lunch Fix Menu (JET-CAMP-LUNCHFIX)", knownCampaignIDs)
	if got != "JET-CAMP-LUNCHFIX" {
		t.Fatalf("full display name match: got %q, want JET-CAMP-LUNCHFIX", got)
	}
}

// TestMatchCampaignID_ShortenedForm reproduces the second exact failing
// input from the report: the shortened name "LUNCHFIX" alone triggered a
// refusal ("no campaign named 'LUNCHFIX'") instead of resolving to the real
// JET-CAMP-LUNCHFIX campaign_id.
func TestMatchCampaignID_ShortenedForm(t *testing.T) {
	got := matchCampaignID("LUNCHFIX", knownCampaignIDs)
	if got != "JET-CAMP-LUNCHFIX" {
		t.Fatalf("shortened-form match: got %q, want JET-CAMP-LUNCHFIX", got)
	}
}

func TestMatchCampaignID_ShortenedForm_LowercaseWithSpaces(t *testing.T) {
	got := matchCampaignID("lunch fix", knownCampaignIDs)
	if got != "JET-CAMP-LUNCHFIX" {
		t.Fatalf("shortened-form match with spacing/case variance: got %q, want JET-CAMP-LUNCHFIX", got)
	}
}

func TestMatchCampaignID_OtherRealCampaigns(t *testing.T) {
	cases := map[string]string{
		"BOOST01": "IFOOD-CAMP-BOOST01",
		"weekend": "IFOOD-CAMP-WEEKEND",
		"NEWMENU": "JET-CAMP-NEWMENU",
		"Featured Placement (IFOOD-CAMP-WEEKEND)": "IFOOD-CAMP-WEEKEND",
	}
	for input, want := range cases {
		t.Run(input, func(t *testing.T) {
			got := matchCampaignID(input, knownCampaignIDs)
			if got != want {
				t.Fatalf("matchCampaignID(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

// TestMatchCampaignID_AmbiguousFragmentRefusesRatherThanGuesses is the
// Principle-III-compliance case: "CAMP" is a substring of every known id in
// this dataset, so a confident single match does not exist. This must
// refuse ("") rather than arbitrarily pick one — the same "refuse rather
// than guess" discipline (Constitution Principle II) applied to this
// package's own internal resolution step.
func TestMatchCampaignID_AmbiguousFragmentRefusesRatherThanGuesses(t *testing.T) {
	got := matchCampaignID("CAMP", knownCampaignIDs)
	if got != "" {
		t.Fatalf("ambiguous fragment must not resolve to a guess: got %q, want \"\"", got)
	}
}

// TestMatchCampaignID_UnknownCampaignRefuses covers the genuinely
// unanswerable case this fix must NOT weaken (task requirement: do not
// weaken Principle II) — a campaign that really isn't in the known set
// must still fail to resolve, not be forced into a false positive.
func TestMatchCampaignID_UnknownCampaignRefuses(t *testing.T) {
	got := matchCampaignID("SUMMER-SALE-2026", knownCampaignIDs)
	if got != "" {
		t.Fatalf("genuinely unknown campaign must not resolve: got %q, want \"\"", got)
	}
}

func TestMatchCampaignID_EmptyInputRefuses(t *testing.T) {
	got := matchCampaignID("", knownCampaignIDs)
	if got != "" {
		t.Fatalf("empty input must not resolve: got %q, want \"\"", got)
	}
}

func TestMatchCampaignID_NeverReturnsValueOutsideKnownSet(t *testing.T) {
	inputs := []string{
		"JET-CAMP-LUNCHFIX", "LUNCHFIX", "Banner Ad - Lunch Fix Menu (JET-CAMP-LUNCHFIX)",
		"CAMP", "SUMMER-SALE-2026", "", "xyz", "BOOST01", "WEEKEND",
	}
	knownSet := make(map[string]bool, len(knownCampaignIDs))
	for _, id := range knownCampaignIDs {
		knownSet[id] = true
	}
	for _, in := range inputs {
		got := matchCampaignID(in, knownCampaignIDs)
		if got != "" && !knownSet[got] {
			t.Fatalf("matchCampaignID(%q) returned %q, which is not in the known campaign_id set — this must never happen", in, got)
		}
	}
}
