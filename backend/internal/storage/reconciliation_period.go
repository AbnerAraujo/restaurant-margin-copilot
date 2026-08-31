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
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
)

// ErrNoReconciliationDataYet is LoadDataDateRange's sentinel for its one
// EXPECTED failure mode — the table is genuinely empty, not yet ingested —
// as distinct from a real query failure (a connection drop, a malformed
// query). Found live: a caller (httpapi.loadMarginSnapshot) collapsed both
// into the same "no prior data" response, so a transient DB hiccup on a
// cost-sheet commit's pre-write read was served as a confidently wrong
// statement about the owner's history (HasData:false) on the one endpoint
// that changes financial numbers, instead of the 500 a real failure
// deserves. errors.Is against this value is how a caller tells them apart.
var ErrNoReconciliationDataYet = errors.New("storage: no daily_reconciliation rows exist yet — cannot resolve a data date range")

// LoadDataDateRange returns the actual inclusive [start, end] this product
// has ANY reconciled data for — the real min/max of daily_reconciliation's
// date column, not a hardcoded literal. Callers (cmd/server/main.go) use
// this once at process start to hand internal/ambiguity's gate and
// internal/explain's system prompt the data's own reference frame, so
// relative date language ("today", "this week", a year-less date) grounds
// against what the data actually covers instead of the host machine's
// wall-clock date — the fix for the "date-year grounding defect" recorded
// in docs/plan.md's mistakes log. Returns an error (never a zero-value
// range silently treated as valid) if the table is empty — there is no
// sensible "today" to ground against before any reconciliation has run.
func LoadDataDateRange(ctx context.Context, q Querier) (start, end time.Time, err error) {
	row, err := q.GetDataDateRange(ctx)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("storage: get data date range: %w", err)
	}
	if !row.MinDate.Valid || !row.MaxDate.Valid {
		return time.Time{}, time.Time{}, ErrNoReconciliationDataYet
	}
	return row.MinDate.Time, row.MaxDate.Time, nil
}

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
