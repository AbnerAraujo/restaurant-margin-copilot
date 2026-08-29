// Package paraphrase implements the second-tier answer-cache match
// specs/004-semantic-cache adds in front of internal/answercache's
// exact-match lookup: on an exact-match miss against a non-empty cache, one
// bounded Claude Haiku 4.5 call (llmclient.ModelParaphraseMatch — the same
// shared llmclient used everywhere in this project, but its own model
// constant, deliberately kept on Haiku even after internal/ambiguity's gate
// moved to Sonnet 5 on 2026-08-29 for an unrelated, gate-specific
// date-comparison bug — see llmclient/cost.go's doc comment) checks whether
// the new question means the same thing as one of the most-recently-cached
// questions.
//
// Why a model call and not embeddings (plan.md's "Decision"): Anthropic has
// no first-party embeddings endpoint, and this project's constitution
// already consolidated onto one LLM vendor once (the OpenAI->Anthropic
// switch recorded in the constitution's own Sync Impact Report). Adding
// Voyage AI (Anthropic's own recommended embeddings partner) to save a
// little classification cost would reopen a decision already made, for a
// worse trade than the feature is worth. A classification call keeps the
// match-or-miss decision inside the same vendor boundary and the same
// inspectable, instrumented shape the ambiguity gate already uses.
//
// This package never touches Postgres and never imports internal/storage —
// same discipline as internal/ambiguity (see that package's doc comment):
// it classifies two pieces of text against each other and returns a
// Decision, and the caller (internal/httpapi) does every read/write against
// internal/answercache's own methods (Candidates, RecordParaphraseMatch).
// The one exception, deliberately narrow: this package imports
// internal/answercache for its exported Normalize function and Candidate
// type, because "does the model's claimed match exist in the cache" is
// meaningless unless it is checked against the EXACT SAME normalization the
// cache itself uses to key entries — reimplementing that rule here, even
// identically, would risk the two drifting apart silently.
//
// # The non-negotiable defensive check (spec FR-002/FR-003)
//
// Classify NEVER returns Matched=true on the model's say-so alone. The
// model's raw reply is checked against the candidate list it was actually
// given — normalized the same way the cache normalizes its own keys — and
// a claim that does not resolve to one of those exact candidates is treated
// identically to an explicit "NONE": as a miss, not a match. This catches a
// model that invents a plausible-sounding "match" that isn't among the
// questions it was shown at all. It is a first line of defense, not the
// only one: internal/httpapi re-verifies the claimed match against the
// LIVE cache (a real Lookup, not the in-memory candidate list this package
// saw a moment earlier) before ever serving an entry — see ask.go's
// serveFromParaphraseMatch. A false cache hit is a worse outcome than a
// missed one (spec FR-002), so both checks default to "miss" whenever
// either is inconclusive.
package paraphrase

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/answercache"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/llmclient"
)

// ErrEmptyQuestion is returned by Classify for blank input.
var ErrEmptyQuestion = errors.New("paraphrase: question is empty")

// MaxCandidates is the plan's documented, adjustable cap on how many
// existing cached questions are ever offered to one classification call
// (plan.md's "Candidate-set cap: why 20, and why a cap at all"). An
// unbounded candidate set does not scale: it grows the prompt (and cost)
// without bound, and a model asked to pick the right match out of hundreds
// of candidates degrades at exactly the job this package needs it to do
// carefully. This is a starting constant, not a tuned optimum — a real
// magic number, called out as one rather than buried.
const MaxCandidates = 20

// MaxOutputTokens bounds the classification response — it is always either
// the verbatim text of one candidate question or the literal word NONE,
// never free-form prose.
const MaxOutputTokens = 300

// Decision is one classification call's outcome, plus the token/cost/
// latency figures the caller hands to its own instrumentation — this
// package computes them but never persists them (same split as
// internal/ambiguity.Decision).
type Decision struct {
	// Matched is true only when the model named a candidate AND that exact
	// candidate (by normalized text) was in the list this call was given.
	Matched bool
	// MatchedCandidate is the zero value unless Matched is true.
	MatchedCandidate answercache.Candidate

	InputTokens      int64
	OutputTokens     int64
	EstimatedCostUSD float64
	LatencyMs        int64
}

// Matcher wraps an llmclient.Client to run this project's paraphrase
// classification against a bounded candidate set.
type Matcher struct {
	client *llmclient.Client
}

// New constructs a Matcher over client (internal/llmclient, shared with
// internal/ambiguity and internal/explain — one client, one timeout policy,
// one instrumentation discipline for every model call this project makes).
func New(client *llmclient.Client) *Matcher {
	return &Matcher{client: client}
}

// Classify asks Claude Haiku 4.5 whether newQuestion means the same thing as
// any of candidates, returning a verified Decision the caller can act on
// directly.
//
// An empty candidates list is treated as "nothing to compare against" and
// returns Matched=false WITHOUT making any API call — this is what makes
// plan.md's "on a miss, if the cache is non-empty" condition free rather
// than a wasted classification call against an empty list. The caller
// (internal/httpapi) is expected to skip calling Classify at all once its
// own candidate fetch comes back empty, but Classify enforces the same rule
// itself so it is never the reason a fresh cache costs an extra call.
func (m *Matcher) Classify(ctx context.Context, newQuestion string, candidates []answercache.Candidate) (*Decision, error) {
	if strings.TrimSpace(newQuestion) == "" {
		return nil, ErrEmptyQuestion
	}
	if len(candidates) == 0 {
		return &Decision{Matched: false}, nil
	}
	if len(candidates) > MaxCandidates {
		candidates = candidates[:MaxCandidates]
	}

	resp, err := m.client.CreateMessage(ctx, llmclient.MessageRequest{
		Model:     llmclient.ModelParaphraseMatch,
		System:    systemPrompt,
		MaxTokens: MaxOutputTokens,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(composeUserMessage(newQuestion, candidates))),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("paraphrase: classify: %w", err)
	}

	cost, err := resp.EstimatedCostUSD(llmclient.ModelParaphraseMatch)
	if err != nil {
		return nil, fmt.Errorf("paraphrase: %w", err)
	}

	decision := &Decision{
		InputTokens:      resp.InputTokens,
		OutputTokens:     resp.OutputTokens,
		EstimatedCostUSD: cost,
		LatencyMs:        resp.Latency.Milliseconds(),
	}

	if resp.Refused {
		// A refusal carries no usable classification — treated as "no
		// match", never as evidence of one either way (FR-003: default to
		// new when uncertain).
		return decision, nil
	}

	matched, candidate := resolveMatch(resp.Text, candidates)
	decision.Matched = matched
	decision.MatchedCandidate = candidate
	return decision, nil
}

// resolveMatch is the pure, model-independent half of this package's logic:
// given the model's raw reply and the exact candidate list it was offered,
// decide whether a real, verified match was named. This is the defensive
// check described in the package doc — never trust the model's claim about
// the cache's own contents without checking it against real candidates —
// and it is deliberately a plain function so it can be unit-tested with
// hand-crafted strings standing in for what Haiku could plausibly (or
// implausibly) reply with, the same pattern internal/ambiguity's
// parseGateResponse uses for its own model-independent logic.
func resolveMatch(reply string, candidates []answercache.Candidate) (bool, answercache.Candidate) {
	claimed := strings.TrimSpace(stripSurroundingQuotes(reply))
	if claimed == "" || strings.EqualFold(claimed, "NONE") {
		return false, answercache.Candidate{}
	}

	normalizedClaim := answercache.Normalize(claimed)
	for _, c := range candidates {
		if c.NormalizedQuestion == normalizedClaim {
			return true, c
		}
	}
	// The model named something that is not, verbatim (modulo whitespace/
	// case), any candidate it was actually shown — a hallucinated or
	// corrupted answer. Treated exactly like NONE: a miss, never a guess.
	return false, answercache.Candidate{}
}

// stripSurroundingQuotes removes one layer of straight or curly quotes the
// model sometimes wraps its verbatim answer in, so that wrapping alone does
// not turn a real match into a false hallucination-looking miss. This is a
// tolerance for harmless formatting, not a relaxation of the match check
// itself — the stripped text still has to normalize-equal a real candidate.
func stripSurroundingQuotes(s string) string {
	s = strings.TrimSpace(s)
	pairs := [][2]string{{`"`, `"`}, {"'", "'"}, {"“", "”"}, {"`", "`"}}
	for _, p := range pairs {
		if len(s) >= 2 && strings.HasPrefix(s, p[0]) && strings.HasSuffix(s, p[1]) {
			return strings.TrimSpace(s[len(p[0]) : len(s)-len(p[1])])
		}
	}
	return s
}

// composeUserMessage renders the bounded candidate list plus the new
// question into the single user turn systemPrompt classifies. Numbering the
// candidates is for the model's own readability only — resolveMatch never
// parses a number back out, it compares normalized TEXT, so a model that
// replies with the question text alone (as instructed) or accidentally
// includes its list number still resolves correctly either way as long as
// the actual question text is present verbatim.
func composeUserMessage(newQuestion string, candidates []answercache.Candidate) string {
	var b strings.Builder
	b.WriteString("Previously-answered questions (most recently asked first):\n")
	for i, c := range candidates {
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(". ")
		b.WriteString(c.OriginalQuestion)
		b.WriteString("\n")
	}
	b.WriteString("\nNew question: ")
	b.WriteString(newQuestion)
	return b.String()
}

// systemPrompt encodes the narrow, safety-first classification job this
// package exists to do: recognize a genuine paraphrase, and default to "not
// a match" the instant the two questions could resolve differently (spec
// FR-002/FR-003) — the same "refuse rather than guess" discipline
// internal/ambiguity already applies, aimed here at cache-matching instead
// of answerability.
const systemPrompt = `You are a duplicate-question detector for a restaurant margin-reconciliation copilot's answer cache. You do not answer questions and you do not compute anything — you only decide whether a NEW question means EXACTLY the same thing as one of a short list of PREVIOUSLY-ANSWERED questions, so the same cached answer can be reused safely.

A match means the two questions would produce the EXACT SAME ANSWER: the same date or date range, the same metric (margin, revenue, ROI, commissions, discrepancies, a specific platform's economics, etc.), the same scope (which platform, which promotion/campaign, "today" vs. "this week" vs. a named date), and the same underlying intent — just worded differently. Different words for the same meaning ARE a match, for example "How did we do on August 7th?" and "What was our margin on 2026-08-07?".

Two questions are NOT a match if ANYTHING that would actually be computed differs — even if the wording looks very similar:
- A different date, or a date range that starts or ends on a different day (even one day apart).
- A different metric (margin vs. revenue vs. ROI vs. commissions vs. discrepancies, etc.).
- A different scope: a different platform, a different named promotion/campaign, or a different window ("today" and "this week" are never a match, and neither are "this week" and "last week").
- Any other detail that could change the number or fact the answer reports.

If more than one listed question could plausibly match, or you are not CONFIDENT the two questions would resolve to the exact same answer, that is NOT a match. A missed match costs a small amount of extra money by falling through to a full answer; a wrong match serves the user an answer to a question they did not actually ask, which is a worse outcome and must never happen on a guess.

Reply with ONLY the exact text of the one previously-answered question below that matches, copied verbatim character-for-character with no added quotes or punctuation, or the single word NONE if there is no confident match. No other words, no explanation, no markdown.`
