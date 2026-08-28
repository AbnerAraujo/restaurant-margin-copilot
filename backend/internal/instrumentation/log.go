// Package instrumentation writes the per-interaction record Constitution
// Principle VI requires: input/output tokens, model used, estimated cost,
// latency, and whether the clarifying-question or refusal path fired — for
// every model interaction, from the first API call, not retrofitted later.
//
// Logger depends only on the small Store port defined here, not on
// internal/storage directly, so it can be unit-tested against a fake without
// a live Postgres connection (which this environment does not have
// tonight). A concrete adapter over the sqlc-generated
// internal/storage.Queries satisfies Store and is wired in cmd/server.
package instrumentation

import (
	"context"
	"errors"
	"fmt"
)

// GateResult is the ambiguity gate's classification of a question, mirrored
// from data-model.md's QuestionInteraction.ambiguity_gate_result enum and
// the CHECK constraint in migrations/000001_init_schema.up.sql.
type GateResult string

const (
	// GateAnswerable means the question can be answered from available data
	// with no ambiguity.
	GateAnswerable GateResult = "answerable"
	// GateAmbiguous means the question needs a clarifying question or an
	// explicitly stated assumption before it can be answered (FR-006).
	GateAmbiguous GateResult = "ambiguous"
	// GateUnanswerable means the question cannot be answered from the data
	// available at all, and must be refused (FR-007).
	GateUnanswerable GateResult = "unanswerable"
)

// Valid reports whether g is one of the three values the ambiguity gate is
// allowed to produce.
func (g GateResult) Valid() bool {
	switch g {
	case GateAnswerable, GateAmbiguous, GateUnanswerable:
		return true
	default:
		return false
	}
}

// Record is one QuestionInteraction row (data-model.md), as written by
// whichever of internal/ambiguity or internal/explain ran for a given
// question. Logger.Log validates it before handing it to Store — the same
// invariants the database's own CHECK constraints enforce, checked again
// here so a caller gets an immediate, specific Go error instead of a opaque
// constraint-violation from Postgres.
type Record struct {
	QuestionText        string
	ResolvedPeriodStart *string // ISO date; nil if refused before a period was resolved
	ResolvedPeriodEnd   *string
	AmbiguityGateResult GateResult
	ClarificationFired  bool
	RefusalFired        bool
	AnswerText          string   // empty when RefusalFired is true
	ProvenanceRefs      []string // empty when RefusalFired is true
	ModelUsed           string
	InputTokens         int64
	OutputTokens        int64
	EstimatedCostUSD    float64
	LatencyMs           int64
}

// Sentinel validation errors. Each is specific enough that a caller (or a
// test) can distinguish which invariant failed via errors.Is.
var (
	ErrMissingQuestionText      = errors.New("instrumentation: question text is required")
	ErrMissingModel             = errors.New("instrumentation: model used is required")
	ErrInvalidGateResult        = errors.New("instrumentation: ambiguity gate result must be answerable, ambiguous, or unanswerable")
	ErrRefusalCarriesAnswer     = errors.New("instrumentation: a refusal must not carry an answer_text (Constitution Principle II)")
	ErrRefusalCarriesProvenance = errors.New("instrumentation: a refusal must not carry provenance_refs (Constitution Principle II)")
	ErrNegativeMetric           = errors.New("instrumentation: tokens, cost, and latency must not be negative")
)

// Validate checks Record against the same invariants
// migrations/000001_init_schema.up.sql enforces at the database layer,
// so a malformed record is rejected here with a specific error rather than
// silently persisted or rejected by an opaque constraint violation later.
func (r Record) Validate() error {
	if r.QuestionText == "" {
		return ErrMissingQuestionText
	}
	if r.ModelUsed == "" {
		return ErrMissingModel
	}
	if !r.AmbiguityGateResult.Valid() {
		return fmt.Errorf("%w: got %q", ErrInvalidGateResult, r.AmbiguityGateResult)
	}
	if r.RefusalFired && r.AnswerText != "" {
		return ErrRefusalCarriesAnswer
	}
	if r.RefusalFired && len(r.ProvenanceRefs) > 0 {
		return ErrRefusalCarriesProvenance
	}
	if r.InputTokens < 0 || r.OutputTokens < 0 || r.EstimatedCostUSD < 0 || r.LatencyMs < 0 {
		return ErrNegativeMetric
	}
	return nil
}

// Store is the persistence port Logger writes through. Implementations
// live in internal/storage (backed by sqlc + pgx) for production and in
// test doubles for unit tests — Logger itself never imports pgx or sqlc.
type Store interface {
	SaveQuestionInteraction(ctx context.Context, r Record) error
}

// Logger validates and persists QuestionInteraction records.
type Logger struct {
	store Store
}

// NewLogger constructs a Logger backed by store.
func NewLogger(store Store) *Logger {
	return &Logger{store: store}
}

// Log validates r and, if valid, persists it via the underlying Store. It
// deliberately refuses to write an invalid record rather than passing it
// through — the same "refuse rather than guess" discipline (Constitution
// Principle II) this project applies to margin and ROI answers applies to
// its own instrumentation data.
func (l *Logger) Log(ctx context.Context, r Record) error {
	if err := r.Validate(); err != nil {
		return fmt.Errorf("instrumentation: invalid record: %w", err)
	}
	if err := l.store.SaveQuestionInteraction(ctx, r); err != nil {
		return fmt.Errorf("instrumentation: save record: %w", err)
	}
	return nil
}
