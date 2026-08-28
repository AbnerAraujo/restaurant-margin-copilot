package storage

// This file is hand-written (unlike the sqlc-generated *.sql.go files in
// this package) and bridges internal/reconcile's domain representation of
// DailyReconciliation (integer cents, no pgx types — reconcile has no
// Postgres dependency at all) with the sqlc-generated Queries that talk to
// the daily_reconciliation table. Nothing here computes a number: it only
// converts a number already computed by internal/reconcile into the shapes
// Postgres understands, and back (Constitution Principle I — this package
// is a storage adapter, not a place where arithmetic happens).

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/money"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
)

// SaveDailyReconciliation persists a reconcile.DailyReconciliation via the
// sqlc-generated UpsertDailyReconciliation query. The caller (the ingest ->
// reconcile -> persist pipeline in cmd/server) is the only writer; the
// model layer never calls this (data-model.md's DailyReconciliation
// validation rule).
func SaveDailyReconciliation(ctx context.Context, q Querier, day reconcile.DailyReconciliation) (DailyReconciliation, error) {
	grossJSON, err := marshalGrossSales(day.GrossSalesBySource)
	if err != nil {
		return DailyReconciliation{}, fmt.Errorf("storage: marshal gross_sales_by_source: %w", err)
	}
	flagsJSON, err := marshalOrEmptyArray(day.DiscrepancyFlags)
	if err != nil {
		return DailyReconciliation{}, fmt.Errorf("storage: marshal discrepancy_flags: %w", err)
	}
	refsJSON, err := marshalOrEmptyArray(day.SourceRowRefs)
	if err != nil {
		return DailyReconciliation{}, fmt.Errorf("storage: marshal source_row_refs: %w", err)
	}

	params := UpsertDailyReconciliationParams{
		Date:               pgtype.Date{Time: day.Date, Valid: true},
		GrossSalesBySource: grossJSON,
		Commissions:        centsToNumeric(day.CommissionsCents),
		Refunds:            centsToNumeric(day.RefundsCents),
		InputCosts:         centsToNumeric(day.InputCostsCents),
		Margin:             centsToNumeric(day.MarginCents),
		DiscrepancyFlags:   flagsJSON,
		SourceRowRefs:      refsJSON,
	}

	return q.UpsertDailyReconciliation(ctx, params)
}

// LoadDailyReconciliation reads a persisted row back and converts it into
// internal/reconcile's domain representation — the exact inverse of
// SaveDailyReconciliation — so a save-then-load round trip can be asserted
// lossless field-for-field, including the jsonb columns (see
// reconciliation_test.go's live-Postgres integration test).
func LoadDailyReconciliation(ctx context.Context, q Querier, date time.Time) (reconcile.DailyReconciliation, error) {
	row, err := q.GetDailyReconciliationByDate(ctx, pgtype.Date{Time: date, Valid: true})
	if err != nil {
		return reconcile.DailyReconciliation{}, err
	}
	return rowToDomain(row)
}

func rowToDomain(row DailyReconciliation) (reconcile.DailyReconciliation, error) {
	gross, err := unmarshalGrossSales(row.GrossSalesBySource)
	if err != nil {
		return reconcile.DailyReconciliation{}, fmt.Errorf("storage: unmarshal gross_sales_by_source: %w", err)
	}
	var flags []reconcile.DiscrepancyFlag
	if err := json.Unmarshal(nonNilJSON(row.DiscrepancyFlags), &flags); err != nil {
		return reconcile.DailyReconciliation{}, fmt.Errorf("storage: unmarshal discrepancy_flags: %w", err)
	}
	var refs []reconcile.SourceRowRef
	if err := json.Unmarshal(nonNilJSON(row.SourceRowRefs), &refs); err != nil {
		return reconcile.DailyReconciliation{}, fmt.Errorf("storage: unmarshal source_row_refs: %w", err)
	}

	commissions, err := numericToCents(row.Commissions)
	if err != nil {
		return reconcile.DailyReconciliation{}, fmt.Errorf("storage: commissions: %w", err)
	}
	refunds, err := numericToCents(row.Refunds)
	if err != nil {
		return reconcile.DailyReconciliation{}, fmt.Errorf("storage: refunds: %w", err)
	}
	inputCosts, err := numericToCents(row.InputCosts)
	if err != nil {
		return reconcile.DailyReconciliation{}, fmt.Errorf("storage: input_costs: %w", err)
	}
	margin, err := numericToCents(row.Margin)
	if err != nil {
		return reconcile.DailyReconciliation{}, fmt.Errorf("storage: margin: %w", err)
	}

	return reconcile.DailyReconciliation{
		Date:               row.Date.Time,
		GrossSalesBySource: gross,
		CommissionsCents:   commissions,
		RefundsCents:       refunds,
		InputCostsCents:    inputCosts,
		MarginCents:        margin,
		DiscrepancyFlags:   flags,
		SourceRowRefs:      refs,
	}, nil
}

// marshalGrossSales renders cents as "12.34"-style decimal strings (via
// internal/money), matching the decimal convention every other money field
// in this schema uses — jsonb has no native fixed-point numeric type, and
// storing raw integer cents there would silently disagree with how
// commissions/refunds/input_costs/margin are represented in the same row.
func marshalGrossSales(gross map[string]int64) (json.RawMessage, error) {
	decimals := make(map[string]string, len(gross))
	for source, cents := range gross {
		decimals[source] = money.FormatCents(cents)
	}
	if len(decimals) == 0 {
		return json.RawMessage(`{}`), nil
	}
	b, err := json.Marshal(decimals)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

func unmarshalGrossSales(raw json.RawMessage) (map[string]int64, error) {
	var decimals map[string]string
	if err := json.Unmarshal(nonNilJSON(raw), &decimals); err != nil {
		return nil, err
	}
	gross := make(map[string]int64, len(decimals))
	for source, s := range decimals {
		cents, err := money.ParseCents(s)
		if err != nil {
			return nil, fmt.Errorf("gross_sales_by_source[%q]=%q: %w", source, s, err)
		}
		gross[source] = cents
	}
	return gross, nil
}

// marshalOrEmptyArray marshals a possibly-nil slice, rendering nil as `[]`
// rather than JSON `null` — the schema's columns default to '[]'::jsonb and
// data-model.md's refusal-provenance invariant checks provenance_refs
// against '[]'::jsonb specifically, so nil must never round-trip as null.
func marshalOrEmptyArray[T any](items []T) (json.RawMessage, error) {
	if items == nil {
		return json.RawMessage(`[]`), nil
	}
	b, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

func nonNilJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`null`)
	}
	return raw
}

// centsToNumeric converts integer cents into the pgtype.Numeric Postgres
// expects for a NUMERIC(12,2) column: value = Int * 10^Exp, so cents=1234
// becomes 1234 * 10^-2 = 12.34.
func centsToNumeric(cents int64) pgtype.Numeric {
	return pgtype.Numeric{Int: big.NewInt(cents), Exp: -2, Valid: true}
}

// numericToCents converts a pgtype.Numeric back into integer cents. It
// tolerates a numeric with fewer decimal places than 2 (e.g. a driver that
// returns Exp=0 for a whole-dollar value) by scaling up, but refuses rather
// than silently truncating if the value carries more precision than cents
// can represent (Constitution Principle II) — which should never happen
// against this schema's NUMERIC(12,2) columns, so surfacing it loudly if it
// ever does is the safer choice over adding untested rounding logic for a
// case that isn't expected to occur.
func numericToCents(n pgtype.Numeric) (int64, error) {
	if !n.Valid {
		return 0, fmt.Errorf("unexpected null numeric")
	}
	if n.NaN {
		return 0, fmt.Errorf("unexpected NaN numeric")
	}
	if n.Int == nil {
		return 0, fmt.Errorf("numeric has no underlying value")
	}

	shift := n.Exp + 2
	if shift < 0 {
		return 0, fmt.Errorf("numeric %v has more precision than cents (exp=%d) — refusing to truncate", n.Int, n.Exp)
	}

	v := new(big.Int).Set(n.Int)
	if shift > 0 {
		scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(shift)), nil)
		v.Mul(v, scale)
	}
	if !v.IsInt64() {
		return 0, fmt.Errorf("numeric value %s out of int64 cents range", v.String())
	}
	return v.Int64(), nil
}
