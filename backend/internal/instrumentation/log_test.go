package instrumentation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/instrumentation"
)

// fakeStore is a minimal in-memory Store used to prove Logger.Log's
// validation and delegation logic without a real Postgres connection —
// exactly the kind of pure-Go interface test we can run tonight without the
// DB.
type fakeStore struct {
	saved     []instrumentation.Record
	saveErr   error
	callCount int
}

func (f *fakeStore) SaveQuestionInteraction(_ context.Context, r instrumentation.Record) error {
	f.callCount++
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, r)
	return nil
}

func validAnsweredRecord() instrumentation.Record {
	return instrumentation.Record{
		QuestionText:        "how did we do today?",
		AmbiguityGateResult: instrumentation.GateAnswerable,
		ClarificationFired:  false,
		RefusalFired:        false,
		AnswerText:          "Margin for 2026-08-07 was $139.75.",
		ProvenanceRefs:      []string{"daily_reconciliation:2026-08-07"},
		ModelUsed:           "claude-sonnet-5",
		InputTokens:         512,
		OutputTokens:        128,
		EstimatedCostUSD:    0.0023,
		LatencyMs:           842,
	}
}

func validRefusedRecord() instrumentation.Record {
	return instrumentation.Record{
		QuestionText:        "how much did we spend with Acme Foods?",
		AmbiguityGateResult: instrumentation.GateUnanswerable,
		ClarificationFired:  false,
		RefusalFired:        true,
		AnswerText:          "",
		ProvenanceRefs:      nil,
		ModelUsed:           "claude-haiku-4-5",
		InputTokens:         210,
		OutputTokens:        40,
		EstimatedCostUSD:    0.00041,
		LatencyMs:           305,
	}
}

func TestLogger_Log_ValidRecords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		record instrumentation.Record
	}{
		{"answered interaction", validAnsweredRecord()},
		{"refused interaction carries no answer or provenance", validRefusedRecord()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := &fakeStore{}
			logger := instrumentation.NewLogger(store)

			err := logger.Log(context.Background(), tt.record)

			require.NoError(t, err)
			require.Len(t, store.saved, 1)
			assert.Equal(t, tt.record, store.saved[0])
		})
	}
}

func TestLogger_Log_RejectsInvalidRecords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(r instrumentation.Record) instrumentation.Record
		wantErr error
	}{
		{
			name: "refusal fired but answer text is present",
			mutate: func(r instrumentation.Record) instrumentation.Record {
				r.RefusalFired = true
				r.AnswerText = "here's a guess"
				return r
			},
			wantErr: instrumentation.ErrRefusalCarriesAnswer,
		},
		{
			name: "refusal fired but provenance refs are present",
			mutate: func(r instrumentation.Record) instrumentation.Record {
				r.RefusalFired = true
				r.AnswerText = ""
				r.ProvenanceRefs = []string{"daily_reconciliation:2026-08-07"}
				return r
			},
			wantErr: instrumentation.ErrRefusalCarriesProvenance,
		},
		{
			name: "unrecognized ambiguity gate result",
			mutate: func(r instrumentation.Record) instrumentation.Record {
				r.AmbiguityGateResult = "maybe"
				return r
			},
			wantErr: instrumentation.ErrInvalidGateResult,
		},
		{
			name: "empty model used",
			mutate: func(r instrumentation.Record) instrumentation.Record {
				r.ModelUsed = ""
				return r
			},
			wantErr: instrumentation.ErrMissingModel,
		},
		{
			name: "empty question text",
			mutate: func(r instrumentation.Record) instrumentation.Record {
				r.QuestionText = ""
				return r
			},
			wantErr: instrumentation.ErrMissingQuestionText,
		},
		{
			name: "negative input tokens",
			mutate: func(r instrumentation.Record) instrumentation.Record {
				r.InputTokens = -1
				return r
			},
			wantErr: instrumentation.ErrNegativeMetric,
		},
		{
			name: "negative output tokens",
			mutate: func(r instrumentation.Record) instrumentation.Record {
				r.OutputTokens = -1
				return r
			},
			wantErr: instrumentation.ErrNegativeMetric,
		},
		{
			name: "negative estimated cost",
			mutate: func(r instrumentation.Record) instrumentation.Record {
				r.EstimatedCostUSD = -0.01
				return r
			},
			wantErr: instrumentation.ErrNegativeMetric,
		},
		{
			name: "negative latency",
			mutate: func(r instrumentation.Record) instrumentation.Record {
				r.LatencyMs = -1
				return r
			},
			wantErr: instrumentation.ErrNegativeMetric,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := &fakeStore{}
			logger := instrumentation.NewLogger(store)

			err := logger.Log(context.Background(), tt.mutate(validAnsweredRecord()))

			require.Error(t, err)
			assert.True(t, errors.Is(err, tt.wantErr), "got %v, want %v", err, tt.wantErr)
			assert.Zero(t, store.callCount, "store must not be called for an invalid record")
		})
	}
}

func TestLogger_Log_PropagatesStoreError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("connection refused")
	store := &fakeStore{saveErr: wantErr}
	logger := instrumentation.NewLogger(store)

	err := logger.Log(context.Background(), validAnsweredRecord())

	require.Error(t, err)
	assert.True(t, errors.Is(err, wantErr))
}
