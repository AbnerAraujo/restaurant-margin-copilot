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
)

// AskRequest is POST /api/ask's request body.
type AskRequest struct {
	Question string `json:"question"`
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
	AssumptionStated   string   `json:"assumption_stated,omitempty"`
	RefusalReason      string   `json:"refusal_reason,omitempty"`
	// Visualization is an optional structured rendering of the SAME
	// deterministic tool results the answer was narrated from — a table, bar
	// chart, or pie chart. Chosen in plain Go from the tool name and result
	// shape (visualization.go), never by a second model call. Omitted
	// entirely when no tool result has a shape worth drawing, so the client
	// never has to distinguish "no chart" from "an empty chart".
	Visualization *Visualization `json:"visualization,omitempty"`
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
	// Note is the disclosed limitation, carried on the wire so the UI can
	// state it rather than implying the cache is smarter than it is.
	Note string `json:"note,omitempty"`
}

// CacheMatchNote is the one-line statement of what this cache does and does
// not match, shown wherever a cache hit is surfaced.
const CacheMatchNote = "Exact question match (whitespace and case ignored). A reworded question is a new question and costs full price."

// Classifier is the ambiguity gate as this handler needs it.
// *ambiguity.Gate satisfies it directly.
//
// Declared as an interface rather than taking the concrete type so a test can
// COUNT model calls. Proving "an identical question is answered twice but
// billed once" requires observing that the gate and explainer ran exactly
// once between the two requests, which is impossible against a struct that
// only talks to the live Anthropic API.
type Classifier interface {
	Classify(ctx context.Context, question string) (*ambiguity.Decision, error)
}

// Narrator is the explanation step as this handler needs it.
// *explain.Explainer satisfies it directly.
type Narrator interface {
	Explain(ctx context.Context, question, assumptionStated string) (*explain.Result, error)
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

		// Cache probe BEFORE the ambiguity gate — the gate is itself a real
		// billed Haiku call, so checking after it would still spend money on
		// every repeat. A hit returns the previously-served response verbatim,
		// including its refusal or clarification: a question that was
		// unanswerable an hour ago is still unanswerable now against the same
		// data, and re-deciding it would cost two model calls to reach the
		// same conclusion.
		if deps.Cache != nil {
			if served := deps.serveFromCache(ctx, w, req.Question); served {
				return
			}
		}

		decision, err := deps.Gate.Classify(ctx, req.Question)
		if err != nil {
			http.Error(w, "ambiguity gate failed: "+err.Error(), http.StatusBadGateway)
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
			deps.writeAndCache(ctx, w, req.Question, AskResponse{
				Status:        "refused",
				RefusalReason: decision.RefusalReason,
				Interactions: []CostInteraction{
					{ModelUsed: llmclient.ModelAmbiguityGate, InputTokens: decision.InputTokens, OutputTokens: decision.OutputTokens, EstimatedCostUSD: decision.EstimatedCostUSD, LatencyMs: decision.LatencyMs},
				},
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
			deps.writeAndCache(ctx, w, req.Question, AskResponse{
				Status:             "clarification_needed",
				ClarifyingQuestion: decision.ClarifyingQuestion,
				Interactions: []CostInteraction{
					{ModelUsed: llmclient.ModelAmbiguityGate, InputTokens: decision.InputTokens, OutputTokens: decision.OutputTokens, EstimatedCostUSD: decision.EstimatedCostUSD, LatencyMs: decision.LatencyMs},
				},
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

		result, err := deps.Explainer.Explain(ctx, req.Question, decision.AssumptionStated)
		if err != nil {
			http.Error(w, "explanation step failed: "+err.Error(), http.StatusBadGateway)
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
			deps.writeAndCache(ctx, w, req.Question, AskResponse{
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
			Status:         "answered",
			AnswerText:     result.AnswerText,
			ProvenanceRefs: result.ProvenanceRefs,
			Visualization:  deriveVisualization(result.ToolInvocations),
			Interactions:   []CostInteraction{gateInteraction, explainInteraction},
		}
		if decision.Result == instrumentation.GateAmbiguous {
			resp.AssumptionStated = decision.AssumptionStated
		}
		deps.writeAndCache(ctx, w, req.Question, resp)
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
func (deps Deps) serveFromCache(ctx context.Context, w http.ResponseWriter, question string) bool {
	entry, err := deps.Cache.Lookup(ctx, question)
	if err != nil {
		log.Printf("httpapi: answer-cache lookup failed (question=%q): %v — falling through to the model", question, err)
		return false
	}
	if entry == nil {
		return false
	}

	var cached AskResponse
	if err := json.Unmarshal(entry.ResponseJSON, &cached); err != nil {
		log.Printf("httpapi: answer-cache entry for %q is unreadable: %v — falling through to the model", question, err)
		return false
	}

	cached.Interactions = nil
	cached.Cache = &CacheInfo{
		Hit:            true,
		CachedAt:       entry.CachedAt,
		CostAvoidedUSD: entry.OriginCostUSD,
		Note:           CacheMatchNote,
	}

	// The hit is instrumented in its own ledger, never as a QuestionInteraction
	// (see internal/answercache's package doc). A failure to record it is
	// logged loudly rather than swallowed — the same treatment logOrWarn gives
	// a failed interaction write — but does not deny the user their answer.
	if err := deps.Cache.RecordHit(ctx, question, entry.OriginCostUSD); err != nil {
		log.Printf("httpapi: FAILED to record answer-cache hit (question=%q): %v", question, err)
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
