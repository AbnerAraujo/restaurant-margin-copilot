// Package httpapi implements the model-facing HTTP surface — the
// POST /api/ask endpoint contracts/mcp-tools.md and spec FR-004/FR-006/
// FR-007 describe — as a plain, exported handler (HandleAsk), not wired
// into cmd/server/main.go here (tasks.md T023: "not wired into main.go
// directly"; a later Integration phase does that).
//
// HandleAsk is the one place that sequences a whole question-answering
// interaction: the ambiguity gate (internal/ambiguity, Claude Haiku 4.5)
// first, then either a refusal/clarification short-circuit or the
// explanation step (internal/explain, Claude Sonnet 5) against the typed
// MCP tools — and, for every branch, an internal/instrumentation write.
//
// Instrumentation design decision (tasks.md's T022/T026 note: the gate's
// own tokens/cost/latency must be logged "even when the request never
// reaches explain"): when a question reaches explain, this handler writes
// TWO QuestionInteraction rows — one for the gate's classification call,
// one for explain's narration call — rather than one merged row. The
// schema (data-model.md) gives a QuestionInteraction a single model_used
// text field and a single input/output token pair; since the gate and
// explain are two independent Claude API calls with their own token counts,
// forcing them into one row would mean either concatenating two model
// names into one field (unparseable) or silently dropping one call's cost.
// Constitution Principle VI ("every model interaction MUST log...") reads
// as per-call, not per-question, so two rows sharing the same question_text
// is the reading that never drops a real API call's cost — the running
// total (SumEstimatedCostUSD) still sums correctly across both.
//
// The same per-call reading extends to a THIRD possible row: when the gate
// classifies a question ambiguous (with a clarifying question) or
// unanswerable, internal/ambiguity may itself have made a second, real
// Claude Sonnet 5 call to upgrade that message's wording (see its package
// doc, Decision.Writer). logWriterCallIfAny logs that as its own row and its
// own CostInteraction, exactly like the gate/explain pair above — this
// handler never merges it into the gate's row or drops it because the
// request short-circuits before reaching explain.
//
// specs/004-semantic-cache adds one more step between the exact-match cache
// probe and the ambiguity gate: on an exact-match MISS against a non-empty
// cache, deps.ParaphraseMatcher (internal/paraphrase) checks whether the
// question means the same thing as a recently-cached one. See
// serveFromParaphraseMatch for the full flow and its defensive
// re-verification against the live cache before ever serving an entry.
package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ambiguity"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/answercache"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/explain"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/instrumentation"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/llmclient"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/paraphrase"
)

// AskRequest is POST /api/ask's request body.
type AskRequest struct {
	Question string `json:"question"`
	// PendingClarification is set when Question is a REPLY to a clarifying
	// question this endpoint returned a moment ago. It is what makes the
	// clarification path a conversation instead of a dead end.
	//
	// Design note — why the CONTEXT travels rather than a pre-merged string:
	// the obvious alternative was to have the frontend concatenate the
	// original question, the clarifying question and the reply into one
	// sentence before posting. That was rejected because the composed text
	// would then be what lands in question_interaction.question_text — the
	// instrumentation log would record a question the user never typed, as
	// if they had. Sending the pieces keeps question_text exactly what was
	// typed ("yes") while still giving the gate everything it needs, and it
	// fixes the defect for any client rather than for this one frontend.
	// It also mirrors how AssumptionStated already flows from the gate into
	// the explanation step: resolved context travels as its own typed field.
	PendingClarification *PendingClarification `json:"pending_clarification,omitempty"`
}

// PendingClarification is the wire form of ambiguity.PendingClarification.
type PendingClarification struct {
	OriginalQuestion   string `json:"original_question"`
	ClarifyingQuestion string `json:"clarifying_question"`
}

func (p *PendingClarification) toGateContext() *ambiguity.PendingClarification {
	if p == nil || strings.TrimSpace(p.ClarifyingQuestion) == "" {
		return nil
	}
	return &ambiguity.PendingClarification{
		OriginalQuestion:   strings.TrimSpace(p.OriginalQuestion),
		ClarifyingQuestion: strings.TrimSpace(p.ClarifyingQuestion),
	}
}

// AskResponse is POST /api/ask's response body. Status is exactly one of
// "answered", "clarification_needed", or "refused" — the three outcomes
// spec FR-006/FR-007 define. Fields not relevant to the given Status are
// left empty, never populated with a placeholder.
type AskResponse struct {
	Status             string   `json:"status"`
	AnswerText         string   `json:"answer_text,omitempty"`
	ProvenanceRefs     []string `json:"provenance_refs,omitempty"`
	ClarifyingQuestion string   `json:"clarifying_question,omitempty"`
	// ClarifyingOptions are one-tap phrasings of a reply to
	// ClarifyingQuestion. Each is posted back like any typed question and
	// re-classified from scratch — never treated as an established fact.
	ClarifyingOptions []string `json:"clarifying_options,omitempty"`
	AssumptionStated  string   `json:"assumption_stated,omitempty"`
	RefusalReason     string   `json:"refusal_reason,omitempty"`
	// Visualization is an optional structured rendering of the SAME
	// deterministic tool results the answer was narrated from — a table, bar
	// chart, or pie chart. Chosen in plain Go from the tool name and result
	// shape (visualization.go), never by a second model call. Omitted
	// entirely when no tool result has a shape worth drawing, so the client
	// never has to distinguish "no chart" from "an empty chart".
	Visualization *Visualization `json:"visualization,omitempty"`
	// SuggestedFollowUps are 0-3 natural-language next questions, generated
	// deterministically in Go (suggestions.go's deriveFollowUpSuggestions)
	// from the real tool that grounded this answer and its real result —
	// never a second model call. Populated ONLY when Status is "answered";
	// a refusal or clarification gets none in this pass (a separate, later
	// enhancement covers those).
	SuggestedFollowUps []string `json:"suggested_followups,omitempty"`
	// Cache is set only when this response was served from the answer cache
	// without any model call. Its presence is the client's signal that
	// Interactions is empty because NOTHING RAN, not because measurement
	// failed — two states a running cost panel must never confuse.
	Cache *CacheInfo `json:"cache,omitempty"`
	// Interactions carries this request's real, just-measured cost — one
	// entry per model call that actually ran (the gate always; explain only
	// if the gate let the question through) — so the frontend's running
	// cost panel (PRD "a visible provenance citation and running cost panel
	// on every answer") reflects this session's real spend rather than a
	// hard-coded placeholder figure.
	Interactions []CostInteraction `json:"interactions,omitempty"`
}

// CostInteraction is one model call's real, measured cost — mirrors the
// same fields internal/instrumentation.Record logs to Postgres, exposed
// here so the client doesn't need its own copy of the pricing table.
type CostInteraction struct {
	ModelUsed        string  `json:"model_used"`
	InputTokens      int64   `json:"input_tokens"`
	OutputTokens     int64   `json:"output_tokens"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	LatencyMs        int64   `json:"latency_ms"`
}

// CacheInfo describes a cache hit. CostAvoidedUSD is what the original model
// calls cost — money this request did NOT spend. It is deliberately a
// separate field from Interactions rather than a zero-cost entry inside it,
// so no client can accidentally sum an avoided cost into a spend total.
type CacheInfo struct {
	Hit            bool    `json:"hit"`
	CachedAt       string  `json:"cached_at,omitempty"`
	CostAvoidedUSD float64 `json:"cost_avoided_usd"`
	// MatchKind is exactly "exact" or "paraphrase" whenever Hit is true —
	// specs/004-semantic-cache FR-004's requirement that an exact-text hit
	// and a paraphrase-recognized hit stay distinguishable from each other
	// (and both from a fresh answer, signaled by Cache being nil/omitted
	// entirely) rather than collapsing into one generic "cache: hit" state.
	MatchKind string `json:"match_kind,omitempty"`
	// Note is the disclosed limitation, carried on the wire so the UI can
	// state it rather than implying the cache is smarter than it is.
	Note string `json:"note,omitempty"`
}

// CacheMatchNote is the one-line statement of what this cache does and does
// not match, shown wherever an EXACT-text cache hit is surfaced.
const CacheMatchNote = "Exact question match (whitespace and case ignored). A reworded question is a new question and costs full price."

// ParaphraseMatchNote is CacheMatchNote's counterpart for a paraphrase hit
// (specs/004-semantic-cache): unlike an exact hit, this one cost a small,
// real amount to recognize (see Interactions on this same response) —
// stated here so the UI never implies a paraphrase hit was free the way an
// exact-text hit genuinely is.
const ParaphraseMatchNote = "Recognized as the same question, worded differently, by a small verification call (its real cost is included above) — not an exact text match."

// Classifier is the ambiguity gate as this handler needs it.
// *ambiguity.Gate satisfies it directly.
//
// Declared as an interface rather than taking the concrete type so a test can
// COUNT model calls. Proving "an identical question is answered twice but
// billed once" requires observing that the gate and explainer ran exactly
// once between the two requests, which is impossible against a struct that
// only talks to the live Anthropic API.
type Classifier interface {
	Classify(ctx context.Context, question string, pending *ambiguity.PendingClarification) (*ambiguity.Decision, error)
}

// Narrator is the explanation step as this handler needs it.
// *explain.Explainer satisfies it directly.
type Narrator interface {
	Explain(ctx context.Context, question, assumptionStated string) (*explain.Result, error)
}

// ParaphraseClassifier is specs/004-semantic-cache's paraphrase layer as
// this handler needs it. *paraphrase.Matcher satisfies it directly.
//
// Declared as an interface for the same reason Classifier and Narrator are:
// a test needs to COUNT and CONTROL classification calls (verified match vs.
// hallucinated/unverifiable match vs. NONE) without making real Anthropic
// API calls or depending on Postgres.
type ParaphraseClassifier interface {
	Classify(ctx context.Context, newQuestion string, candidates []answercache.Candidate) (*paraphrase.Decision, error)
}

// Deps are HandleAsk's dependencies, constructed once at process start (by
// whatever Integration-phase wiring calls this package — not here).
type Deps struct {
	Gate      Classifier
	Explainer Narrator
	Logger    *instrumentation.Logger
	// Cache, when non-nil, short-circuits an exact repeat question before
	// either model call. Optional: a Deps with no Cache behaves exactly as
	// this handler did before the cache existed, which is what keeps the
	// cache an optimisation rather than a dependency.
	Cache *answercache.Cache
	// ParaphraseMatcher, when non-nil (and Cache is also non-nil), checks an
	// exact-match MISS against a bounded set of recently-cached questions
	// before falling through to the gate — specs/004-semantic-cache.
	// Optional and additive, same as Cache itself: a Deps with no
	// ParaphraseMatcher behaves exactly as this handler did before this
	// feature existed (spec FR-006 — this must never weaken the existing
	// exact-match cache's own behavior).
	ParaphraseMatcher ParaphraseClassifier
	// DataStart/DataEnd are the real, actual [start, end] this product has
	// reconciled data for (internal/storage.LoadDataDateRange, resolved once
	// at process start — cmd/server/main.go's buildAskDeps), in YYYY-MM-DD.
	// Threaded through so deriveFollowUpSuggestions can clamp every
	// generated follow-up's date/period to real, answerable bounds. Left
	// empty in a Deps built without them (most existing tests): follow-up
	// suggestions then come back empty rather than guessing at a range,
	// which never breaks the answer path itself.
	DataStart string
	DataEnd   string
}

// HandleAsk implements POST /api/ask. It is exported and takes its
// dependencies as plain constructor arguments specifically so it is
// trivial to wire into any router (net/http ServeMux, chi, etc.) without
// this package importing cmd/server or vice versa.
func HandleAsk(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req AskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		req.Question = strings.TrimSpace(req.Question)
		if req.Question == "" {
			http.Error(w, "question is required", http.StatusBadRequest)
			return
		}

		ctx := r.Context()

		pending := req.PendingClarification.toGateContext()
		// resolved is the self-contained question this request really asks:
		// identical to req.Question for a normal question, and the original
		// question plus its clarification context for a follow-up reply.
		// It is what the gate classifies, what the explanation step narrates,
		// and — critically — what the answer cache keys on. Keying a
		// follow-up on the bare reply would let "yes" answering one
		// clarification serve the cached answer to a different one.
		resolved := ambiguity.ComposeFollowUp(req.Question, pending)

		// Cache probe BEFORE the ambiguity gate — the gate is itself a real
		// billed Haiku call, so checking after it would still spend money on
		// every repeat. A hit returns the previously-served response verbatim,
		// including its refusal or clarification: a question that was
		// unanswerable an hour ago is still unanswerable now against the same
		// data, and re-deciding it would cost two model calls to reach the
		// same conclusion.
		if deps.Cache != nil {
			if served := deps.serveFromCache(ctx, w, resolved, req.Question); served {
				return
			}
			// Second-tier check, ONLY on an exact-match miss: does this
			// question mean the same thing as one already cached, just
			// worded differently? Unlike the exact-match probe above, this
			// one is a real (small, bounded) billed Haiku call — see
			// serveFromParaphraseMatch for why it still runs before the
			// ambiguity gate rather than after it.
			if deps.ParaphraseMatcher != nil {
				if served := deps.serveFromParaphraseMatch(ctx, w, resolved, req.Question); served {
					return
				}
			}
		}

		decision, err := deps.Gate.Classify(ctx, req.Question, pending)
		if err != nil {
			// ambiguity.Gate.Classify's error contract returns (nil, err) on
			// every failure path, so there is no partial Decision here to
			// pull tokens/cost out of and log — unlike internal/explain's
			// Explain below, which was fixed to carry partial usage back
			// specifically so this handler could still log it. The real
			// error goes to the server log; the client gets a generic,
			// safe message rather than a raw internal error string.
			log.Printf("httpapi: ambiguity gate failed (question=%q): %v", req.Question, err)
			writeJSONError(w, http.StatusBadGateway, "gate_failed", "the ambiguity check failed; please try again")
			return
		}

		if decision.Result == instrumentation.GateUnanswerable {
			deps.logOrWarn(ctx, instrumentation.Record{
				QuestionText:        req.Question,
				AmbiguityGateResult: instrumentation.GateUnanswerable,
				RefusalFired:        true,
				ModelUsed:           llmclient.ModelAmbiguityGate,
				InputTokens:         decision.InputTokens,
				OutputTokens:        decision.OutputTokens,
				EstimatedCostUSD:    decision.EstimatedCostUSD,
				LatencyMs:           decision.LatencyMs,
			})
			interactions := []CostInteraction{
				{ModelUsed: llmclient.ModelAmbiguityGate, InputTokens: decision.InputTokens, OutputTokens: decision.OutputTokens, EstimatedCostUSD: decision.EstimatedCostUSD, LatencyMs: decision.LatencyMs},
			}
			interactions = deps.logWriterCallIfAny(ctx, req.Question, decision, false, true, interactions)
			deps.writeAndCache(ctx, w, resolved, AskResponse{
				Status:        "refused",
				RefusalReason: decision.RefusalReason,
				Interactions:  interactions,
			})
			return
		}

		if decision.Result == instrumentation.GateAmbiguous && decision.ClarifyingQuestion != "" {
			deps.logOrWarn(ctx, instrumentation.Record{
				QuestionText:        req.Question,
				AmbiguityGateResult: instrumentation.GateAmbiguous,
				ClarificationFired:  true,
				ModelUsed:           llmclient.ModelAmbiguityGate,
				InputTokens:         decision.InputTokens,
				OutputTokens:        decision.OutputTokens,
				EstimatedCostUSD:    decision.EstimatedCostUSD,
				LatencyMs:           decision.LatencyMs,
			})
			interactions := []CostInteraction{
				{ModelUsed: llmclient.ModelAmbiguityGate, InputTokens: decision.InputTokens, OutputTokens: decision.OutputTokens, EstimatedCostUSD: decision.EstimatedCostUSD, LatencyMs: decision.LatencyMs},
			}
			interactions = deps.logWriterCallIfAny(ctx, req.Question, decision, true, false, interactions)
			deps.writeAndCache(ctx, w, resolved, AskResponse{
				Status:             "clarification_needed",
				ClarifyingQuestion: decision.ClarifyingQuestion,
				ClarifyingOptions:  decision.ClarifyingOptions,
				Interactions:       interactions,
			})
			return
		}

		// answerable, or ambiguous-with-an-explicitly-stated-assumption:
		// log the gate's own call now (it ran and cost real tokens
		// regardless of what explain does next — see package doc), then
		// proceed to explain.
		deps.logOrWarn(ctx, instrumentation.Record{
			QuestionText:        req.Question,
			AmbiguityGateResult: decision.Result,
			ModelUsed:           llmclient.ModelAmbiguityGate,
			InputTokens:         decision.InputTokens,
			OutputTokens:        decision.OutputTokens,
			EstimatedCostUSD:    decision.EstimatedCostUSD,
			LatencyMs:           decision.LatencyMs,
		})
		gateInteraction := CostInteraction{ModelUsed: llmclient.ModelAmbiguityGate, InputTokens: decision.InputTokens, OutputTokens: decision.OutputTokens, EstimatedCostUSD: decision.EstimatedCostUSD, LatencyMs: decision.LatencyMs}

		// The explanation step gets the RESOLVED question. Handing it the bare
		// reply ("yes") would leave it narrating a fragment even though the
		// gate had already worked out what was being asked.
		result, err := deps.Explainer.Explain(ctx, resolved, decision.AssumptionStated)
		if err != nil {
			// Explain still returns a non-nil *Result on a mid-loop failure,
			// carrying whatever tokens/cost this interaction's earlier turns
			// already accumulated before the failure — those Anthropic
			// calls were genuinely billed regardless of how this request
			// ends, so that spend must still be logged (Constitution
			// Principle VI) rather than silently discarded just because the
			// interaction as a whole failed. The gate's own call was already
			// logged above, before Explain ran, so this only ever adds
			// explain's row.
			if result != nil {
				deps.logOrWarn(ctx, instrumentation.Record{
					QuestionText:        req.Question,
					AmbiguityGateResult: decision.Result,
					RefusalFired:        true,
					ModelUsed:           llmclient.ModelExplanation,
					InputTokens:         result.InputTokens,
					OutputTokens:        result.OutputTokens,
					EstimatedCostUSD:    result.EstimatedCostUSD,
					LatencyMs:           result.LatencyMs,
				})
			}
			log.Printf("httpapi: explanation step failed (question=%q): %v", req.Question, err)
			writeJSONError(w, http.StatusBadGateway, "explanation_failed", "the explanation step failed; please try again")
			return
		}
		explainInteraction := CostInteraction{ModelUsed: llmclient.ModelExplanation, InputTokens: result.InputTokens, OutputTokens: result.OutputTokens, EstimatedCostUSD: result.EstimatedCostUSD, LatencyMs: result.LatencyMs}

		if result.IncompleteReason != "" {
			// Spec Acceptance Scenario US3.3: stop and report inability
			// rather than a partial, unlabeled answer.
			deps.logOrWarn(ctx, instrumentation.Record{
				QuestionText:        req.Question,
				AmbiguityGateResult: decision.Result,
				RefusalFired:        true,
				ModelUsed:           llmclient.ModelExplanation,
				InputTokens:         result.InputTokens,
				OutputTokens:        result.OutputTokens,
				EstimatedCostUSD:    result.EstimatedCostUSD,
				LatencyMs:           result.LatencyMs,
			})
			deps.writeAndCache(ctx, w, resolved, AskResponse{
				Status:        "refused",
				RefusalReason: result.IncompleteReason,
				Interactions:  []CostInteraction{gateInteraction, explainInteraction},
			})
			return
		}

		deps.logOrWarn(ctx, instrumentation.Record{
			QuestionText:        req.Question,
			AmbiguityGateResult: decision.Result,
			AnswerText:          result.AnswerText,
			ProvenanceRefs:      result.ProvenanceRefs,
			ModelUsed:           llmclient.ModelExplanation,
			InputTokens:         result.InputTokens,
			OutputTokens:        result.OutputTokens,
			EstimatedCostUSD:    result.EstimatedCostUSD,
			LatencyMs:           result.LatencyMs,
		})

		resp := AskResponse{
			Status:             "answered",
			AnswerText:         result.AnswerText,
			ProvenanceRefs:     result.ProvenanceRefs,
			Visualization:      deriveVisualization(result.ToolInvocations),
			SuggestedFollowUps: deriveFollowUpSuggestions(result.ToolInvocations, req.Question, deps.DataStart, deps.DataEnd),
			Interactions:       []CostInteraction{gateInteraction, explainInteraction},
		}
		if decision.Result == instrumentation.GateAmbiguous {
			resp.AssumptionStated = decision.AssumptionStated
		}
		deps.writeAndCache(ctx, w, resolved, resp)
	}
}

// logOrWarn writes r via deps.Logger and, on failure, logs loudly to
// stderr rather than silently dropping it or failing the user's request —
// Constitution Principle VI requires every interaction be instrumented,
// so a logging failure here is a real defect worth surfacing clearly, not
// something to swallow just because the user's answer already succeeded.
func (deps Deps) logOrWarn(ctx context.Context, r instrumentation.Record) {
	if err := deps.Logger.Log(ctx, r); err != nil {
		log.Printf("httpapi: FAILED to write instrumentation record (question=%q, model=%s): %v", r.QuestionText, r.ModelUsed, err)
	}
}

// logWriterCallIfAny logs decision.Writer — the ambiguity gate's optional
// second-pass Claude Sonnet 5 call that rewrites a hard-case clarifying
// question or refusal reason (internal/ambiguity's package doc) — as its
// OWN instrumentation.Record and CostInteraction, exactly the same
// discipline this file already applies to the gate/explain pair (package
// doc: "two rows sharing the same question_text ... never drops a real API
// call's cost"). It is a no-op, returning interactions unchanged, whenever
// this decision's gate call did not need the second pass (Answerable, or
// Ambiguous-with-an-AssumptionStated).
func (deps Deps) logWriterCallIfAny(ctx context.Context, questionText string, decision *ambiguity.Decision, clarificationFired, refusalFired bool, interactions []CostInteraction) []CostInteraction {
	if decision.Writer == nil {
		return interactions
	}
	deps.logOrWarn(ctx, instrumentation.Record{
		QuestionText:        questionText,
		AmbiguityGateResult: decision.Result,
		ClarificationFired:  clarificationFired,
		RefusalFired:        refusalFired,
		ModelUsed:           decision.Writer.ModelUsed,
		InputTokens:         decision.Writer.InputTokens,
		OutputTokens:        decision.Writer.OutputTokens,
		EstimatedCostUSD:    decision.Writer.EstimatedCostUSD,
		LatencyMs:           decision.Writer.LatencyMs,
	})
	return append(interactions, CostInteraction{
		ModelUsed:        decision.Writer.ModelUsed,
		InputTokens:      decision.Writer.InputTokens,
		OutputTokens:     decision.Writer.OutputTokens,
		EstimatedCostUSD: decision.Writer.EstimatedCostUSD,
		LatencyMs:        decision.Writer.LatencyMs,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// serveFromCache probes the answer cache and, on a hit, writes the previously
// served response and reports true. On a miss — or on any cache failure — it
// reports false so the caller proceeds with the real model calls: a broken
// cache must degrade into "pay full price", never into "fail the request" or,
// worse, "serve something stale-looking anyway".
//
// The response is rewritten in exactly two ways before it goes out:
// Interactions is emptied, because no model call ran and reporting the
// original call's cost would charge the user twice for one API call; and
// Cache is set, so the client can tell "zero cost because nothing ran" from
// "zero cost because measurement failed".
func (deps Deps) serveFromCache(ctx context.Context, w http.ResponseWriter, cacheSubject, askedText string) bool {
	entry, err := deps.Cache.Lookup(ctx, cacheSubject)
	if err != nil {
		log.Printf("httpapi: answer-cache lookup failed (question=%q): %v — falling through to the model", askedText, err)
		return false
	}
	if entry == nil {
		return false
	}

	var cached AskResponse
	if err := json.Unmarshal(entry.ResponseJSON, &cached); err != nil {
		log.Printf("httpapi: answer-cache entry for %q is unreadable: %v — falling through to the model", askedText, err)
		return false
	}

	cached.Interactions = nil
	cached.Cache = &CacheInfo{
		Hit:            true,
		CachedAt:       entry.CachedAt,
		CostAvoidedUSD: entry.OriginCostUSD,
		MatchKind:      "exact",
		Note:           CacheMatchNote,
	}

	// The hit is instrumented in its own ledger, never as a QuestionInteraction
	// (see internal/answercache's package doc). A failure to record it is
	// logged loudly rather than swallowed — the same treatment logOrWarn gives
	// a failed interaction write — but does not deny the user their answer.
	if err := deps.Cache.RecordHit(ctx, cacheSubject, entry.OriginCostUSD); err != nil {
		log.Printf("httpapi: FAILED to record answer-cache hit (question=%q): %v", askedText, err)
	}

	writeJSON(w, http.StatusOK, cached)
	return true
}

// serveFromParaphraseMatch runs specs/004-semantic-cache's second-tier check
// after an exact-match miss: fetch a bounded candidate set of recently
// cached questions, ask deps.ParaphraseMatcher whether cacheSubject means
// the same thing as one of them, and — only if that claim then verifies
// against the REAL, CURRENT cache (not just the in-memory candidate list the
// classifier saw a moment earlier) — serve that entry.
//
// Every failure mode here (no candidates, a classification error, no match,
// or a match that fails live re-verification) returns false and changes
// nothing: the caller falls through to the normal gate->explain flow exactly
// as if this method did not exist, matching serveFromCache's "a broken
// mechanism degrades to full price, never to a wrong or failed answer"
// discipline (spec FR-002/FR-003 — uncertain or unverifiable means new).
//
// The double verification is deliberate, not redundant: internal/paraphrase
// already refuses to report Matched=true unless the model's claim resolves
// to one of the candidates it was actually shown (see resolveMatch there).
// The second check here — a real Cache.Lookup — additionally covers the
// window between that candidate fetch and this moment, however small: if an
// ingestion cleared the cache in between, or the candidate came from a
// normalized key that (for any reason) no longer resolves, this call fails
// closed into a fresh answer instead of serving a nonexistent match.
func (deps Deps) serveFromParaphraseMatch(ctx context.Context, w http.ResponseWriter, cacheSubject, askedText string) bool {
	candidates, err := deps.Cache.Candidates(ctx, paraphrase.MaxCandidates)
	if err != nil {
		log.Printf("httpapi: paraphrase candidate fetch failed (question=%q): %v — falling through to the model", askedText, err)
		return false
	}
	if len(candidates) == 0 {
		// Nothing cached yet to possibly paraphrase — skip the classification
		// call entirely rather than paying for a comparison against nothing
		// (plan.md: "on a miss, if the cache is non-empty").
		return false
	}

	decision, err := deps.ParaphraseMatcher.Classify(ctx, cacheSubject, candidates)
	if err != nil {
		log.Printf("httpapi: paraphrase classification failed (question=%q): %v — falling through to the model", askedText, err)
		return false
	}
	if !decision.Matched {
		return false
	}

	// Live re-verification (see doc comment above) — never serve an entry
	// because a classifier CLAIMED a match without checking the real cache.
	entry, err := deps.Cache.Lookup(ctx, decision.MatchedCandidate.OriginalQuestion)
	if err != nil {
		log.Printf("httpapi: paraphrase match verification lookup failed (question=%q, claimed match=%q): %v — falling through to the model", askedText, decision.MatchedCandidate.OriginalQuestion, err)
		return false
	}
	if entry == nil {
		log.Printf("httpapi: paraphrase classifier claimed a match (%q) that does not verify against the live cache — treating as a miss (question=%q)", decision.MatchedCandidate.OriginalQuestion, askedText)
		return false
	}

	var cached AskResponse
	if err := json.Unmarshal(entry.ResponseJSON, &cached); err != nil {
		log.Printf("httpapi: paraphrase-matched cache entry for %q is unreadable: %v — falling through to the model", decision.MatchedCandidate.OriginalQuestion, err)
		return false
	}

	// Unlike an exact-text hit, a paraphrase hit DID spend real money — the
	// classification call itself. That real cost is reported in Interactions
	// (the one call that actually ran this request), and the cost it avoided
	// is reported separately in Cache.CostAvoidedUSD — two numbers, never
	// netted into one (spec FR-005).
	classificationInteraction := CostInteraction{
		ModelUsed:        llmclient.ModelAmbiguityGate,
		InputTokens:      decision.InputTokens,
		OutputTokens:     decision.OutputTokens,
		EstimatedCostUSD: decision.EstimatedCostUSD,
		LatencyMs:        decision.LatencyMs,
	}
	cached.Interactions = []CostInteraction{classificationInteraction}
	cached.Cache = &CacheInfo{
		Hit:            true,
		CachedAt:       entry.CachedAt,
		CostAvoidedUSD: entry.OriginCostUSD,
		MatchKind:      "paraphrase",
		Note:           ParaphraseMatchNote,
	}

	if err := deps.Cache.RecordParaphraseMatch(ctx, answercache.ParaphraseMatchParams{
		NewQuestion:                askedText,
		MatchedNormalizedQuestion:  decision.MatchedCandidate.NormalizedQuestion,
		ClassificationInputTokens:  decision.InputTokens,
		ClassificationOutputTokens: decision.OutputTokens,
		ClassificationCostUSD:      decision.EstimatedCostUSD,
		ClassificationLatencyMs:    decision.LatencyMs,
		CostAvoidedUSD:             entry.OriginCostUSD,
	}); err != nil {
		log.Printf("httpapi: FAILED to record paraphrase-match ledger row (question=%q): %v", askedText, err)
	}

	writeJSON(w, http.StatusOK, cached)
	return true
}

// writeAndCache writes resp and, when a cache is configured, stores it for
// the next identical question. Caching happens AFTER the response is written
// and never affects it: a cache write failure is a lost optimisation, not a
// failed answer, so it is logged and otherwise ignored.
//
// Every outcome is cached, refusals and clarifications included. A refusal is
// a deterministic consequence of the data on file — asking the same
// unanswerable question twice should not cost twice — and the cache is
// cleared wholesale the moment new data arrives, which is exactly when a
// refusal might stop being correct.
func (deps Deps) writeAndCache(ctx context.Context, w http.ResponseWriter, question string, resp AskResponse) {
	writeJSON(w, http.StatusOK, resp)

	if deps.Cache == nil {
		return
	}

	var originCostUSD float64
	for _, interaction := range resp.Interactions {
		originCostUSD += interaction.EstimatedCostUSD
	}

	body, err := json.Marshal(resp)
	if err != nil {
		log.Printf("httpapi: could not serialize response for the answer cache (question=%q): %v", question, err)
		return
	}
	if err := deps.Cache.Save(ctx, question, body, originCostUSD); err != nil {
		log.Printf("httpapi: could not write answer-cache entry (question=%q): %v", question, err)
	}
}
