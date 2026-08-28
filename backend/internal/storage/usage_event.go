package storage

// Hand-written adapter over the sqlc-generated usage_event queries, mirroring
// promotion.go's pattern: bridge plain Go (time.Time) with the pgtype shapes
// Postgres understands. See migrations/000003_badge_expansion.up.sql for why
// "distinct day" is enforced by a unique index on a generated column rather
// than in application code — this file has nothing to compute, only to call.

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// RecordUsageEvent records one real usage event (FR-003) for "right now",
// server-side — the caller never supplies a timestamp, so a client cannot
// backdate or fabricate a usage day. Returns whether this call actually
// inserted a NEW day (false when today was already recorded, including by
// an earlier call in the same day) — internal/httpapi uses this only to pick
// a response, never to decide correctness: correctness is the unique index
// on usage_event.occurred_on itself (spec 002 User Story 2 Acceptance
// Scenario 3), not this boolean.
func RecordUsageEvent(ctx context.Context, q Querier) (recorded bool, err error) {
	_, err = q.RecordUsageEvent(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// ON CONFLICT DO NOTHING with no RETURNING row means today's
			// usage day already existed — a real, expected outcome (the
			// double-submit case), never an error.
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// LoadDistinctUsageDays returns every distinct UTC calendar day the app has
// genuinely been opened on, oldest first — backs Engagement badge
// evaluation (FR-004). An empty, non-nil slice on a fresh instance is the
// correct, honest zero (SC-002), not an error.
func LoadDistinctUsageDays(ctx context.Context, q Querier) ([]time.Time, error) {
	rows, err := q.ListDistinctUsageDays(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]time.Time, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Time)
	}
	return out, nil
}
