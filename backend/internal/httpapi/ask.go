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
	Status             string             `json:"status"`
	AnswerText         string             `json:"answer_text,omitempty"`
	ProvenanceRefs     []string           `json:"provenance_refs,omitempty"`
	ClarifyingQuestion string             `json:"clarifying_question,omitempty"`
	AssumptionStated   string             `json:"assumption_stated,omitempty"`
	RefusalReason      string             `json:"refusal_reason,omitempty"`
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

// Deps are HandleAsk's dependencies, constructed once at process start (by
// whatever Integration-phase wiring calls this package — not here).
type Deps struct {
	Gate      *ambiguity.Gate
	Explainer *explain.Explainer
	Logger    *instrumentation.Logger
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
			writeJSON(w, http.StatusOK, AskResponse{
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
			writeJSON(w, http.StatusOK, AskResponse{
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
			writeJSON(w, http.StatusOK, AskResponse{
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
			Interactions:   []CostInteraction{gateInteraction, explainInteraction},
		}
		if decision.Result == instrumentation.GateAmbiguous {
			resp.AssumptionStated = decision.AssumptionStated
		}
		writeJSON(w, http.StatusOK, resp)
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
