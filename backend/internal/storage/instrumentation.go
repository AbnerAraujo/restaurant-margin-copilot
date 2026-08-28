// Package storage (this file): the concrete adapter internal/instrumentation's
// own package doc promises but does not itself provide — "A concrete adapter
// over the sqlc-generated internal/storage.Queries satisfies Store and is
// wired in cmd/server." It lives here, not in cmd/server, because it is
// pure translation between two already-defined shapes (instrumentation.Record
// and CreateQuestionInteractionParams) with no process-lifecycle concerns of
// its own — the same shape as this package's other hand-written adapters
// (promotion.go, reconciliation.go) sitting alongside the sqlc-generated
// files. instrumentation.Logger itself never imports this package (or pgx),
// preserving the dependency direction its doc comment describes.
package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/instrumentation"
)

// InstrumentationAdapter implements instrumentation.Store over a Querier,
// so cmd/server can hand instrumentation.NewLogger a real Postgres-backed
// store without instrumentation itself ever depending on storage or pgx.
type InstrumentationAdapter struct {
	q Querier
}

var _ instrumentation.Store = (*InstrumentationAdapter)(nil)

// NewInstrumentationAdapter builds an InstrumentationAdapter over q.
func NewInstrumentationAdapter(q Querier) *InstrumentationAdapter {
	return &InstrumentationAdapter{q: q}
}

// SaveQuestionInteraction implements instrumentation.Store by translating r
// into CreateQuestionInteractionParams and persisting it.
func (a *InstrumentationAdapter) SaveQuestionInteraction(ctx context.Context, r instrumentation.Record) error {
	period, err := resolvedPeriodRange(r.ResolvedPeriodStart, r.ResolvedPeriodEnd)
	if err != nil {
		return fmt.Errorf("storage: resolved period: %w", err)
	}

	// migrations/000001_init_schema.up.sql: provenance_refs is NOT NULL
	// DEFAULT '[]'::jsonb, and the refusal_has_no_answer_or_provenance CHECK
	// compares it against '[]'::jsonb specifically — so a nil slice must
	// still marshal to "[]", never JSON null.
	refs := r.ProvenanceRefs
	if refs == nil {
		refs = []string{}
	}
	provenanceJSON, err := json.Marshal(refs)
	if err != nil {
		return fmt.Errorf("storage: marshaling provenance_refs: %w", err)
	}

	var costNumeric pgtype.Numeric
	if err := costNumeric.Scan(fmt.Sprintf("%.6f", r.EstimatedCostUSD)); err != nil {
		return fmt.Errorf("storage: encoding estimated_cost_usd: %w", err)
	}

	// answer_text is nullable, and the same CHECK requires it be NULL (not
	// empty-string) on a refusal — instrumentation.Record.Validate already
	// guarantees AnswerText == "" whenever RefusalFired, so leaving it
	// pgtype.Text{}'s zero value (Valid: false) on empty covers both that
	// case and the gate-only row (no answer yet) with the same branch.
	answerText := pgtype.Text{}
	if r.AnswerText != "" {
		answerText = pgtype.Text{String: r.AnswerText, Valid: true}
	}

	if _, err := a.q.CreateQuestionInteraction(ctx, CreateQuestionInteractionParams{
		QuestionText:        r.QuestionText,
		ResolvedPeriod:      period,
		AmbiguityGateResult: string(r.AmbiguityGateResult),
		ClarificationFired:  r.ClarificationFired,
		RefusalFired:        r.RefusalFired,
		AnswerText:          answerText,
		ProvenanceRefs:      provenanceJSON,
		ModelUsed:           r.ModelUsed,
		InputTokens:         int32(r.InputTokens),
		OutputTokens:        int32(r.OutputTokens),
		EstimatedCostUsd:    costNumeric,
		LatencyMs:           int32(r.LatencyMs),
	}); err != nil {
		return fmt.Errorf("storage: creating question_interaction: %w", err)
	}
	return nil
}

// resolvedPeriodRange builds the nullable inclusive daterange
// question_interaction.resolved_period expects from a Record's optional
// ISO-8601 (YYYY-MM-DD) date strings. Both nil is the common case (a
// refusal, or the gate's own row logged before any period was resolved)
// and yields an explicit NULL — a pgtype.Range left at its zero value
// (Valid: false) — never an empty range, which is a distinct, wrong value
// for "no period resolved".
func resolvedPeriodRange(start, end *string) (pgtype.Range[pgtype.Date], error) {
	if start == nil && end == nil {
		return pgtype.Range[pgtype.Date]{}, nil
	}
	if start == nil || end == nil {
		return pgtype.Range[pgtype.Date]{}, fmt.Errorf("resolved period start and end must both be set or both be nil, got start=%v end=%v", start, end)
	}
	startTime, err := time.Parse("2006-01-02", *start)
	if err != nil {
		return pgtype.Range[pgtype.Date]{}, fmt.Errorf("parsing resolved period start %q: %w", *start, err)
	}
	endTime, err := time.Parse("2006-01-02", *end)
	if err != nil {
		return pgtype.Range[pgtype.Date]{}, fmt.Errorf("parsing resolved period end %q: %w", *end, err)
	}
	return PromotionPeriodRange(startTime, endTime), nil
}
