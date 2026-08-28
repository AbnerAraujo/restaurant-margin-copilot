package storage

// This file is hand-written, like reconciliation.go, and adds one more
// read path over the same sqlc-generated Queries: loading every persisted
// DailyReconciliation in a date range and converting each into
// internal/reconcile's domain representation. It exists for
// internal/mcptools (User Story 2/3), which needs period-range reads to
// implement get_margin_delta and list_discrepancies per
// contracts/mcp-tools.md — ListDailyReconciliationsInPeriod itself was
// already generated in Phase 1 anticipating exactly this need (see its doc
// comment in querier.go). Nothing here changes any User Story 1 read/write
// path; it only reuses rowToDomain, the same conversion
// SaveDailyReconciliation/LoadDailyReconciliation already rely on, so a
// period read and a single-date read can never disagree about how a row is
// interpreted.

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
)

// LoadDailyReconciliationsInPeriod reads every persisted DailyReconciliation
// row whose date falls within [start, end] inclusive, ordered by date, and
// converts each into internal/reconcile's domain representation. A date in
// the range with no persisted row simply produces no entry in the result —
// callers that need to distinguish "no data at all" from "a shorter period
// than requested" must compare the returned dates against the requested
// range themselves (internal/mcptools does this for get_margin_delta's
// insufficient_data case).
func LoadDailyReconciliationsInPeriod(ctx context.Context, q Querier, start, end time.Time) ([]reconcile.DailyReconciliation, error) {
	rows, err := q.ListDailyReconciliationsInPeriod(ctx, ListDailyReconciliationsInPeriodParams{
		Date:   pgtype.Date{Time: start, Valid: true},
		Date_2: pgtype.Date{Time: end, Valid: true},
	})
	if err != nil {
		return nil, err
	}

	out := make([]reconcile.DailyReconciliation, 0, len(rows))
	for _, row := range rows {
		d, err := rowToDomain(row)
		if err != nil {
			return nil, fmt.Errorf("storage: converting row for %s: %w", row.Date.Time.Format("2006-01-02"), err)
		}
		out = append(out, d)
	}
	return out, nil
}
