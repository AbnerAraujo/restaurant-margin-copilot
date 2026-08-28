package storage

// Hand-written adapter, mirroring reconciliation.go's pattern exactly: it
// bridges internal/reconcile's domain representation of PromotionRoiRecord
// (integer cents, nil-able pointers, no pgx types at all) with the
// sqlc-generated Queries that talk to the promotion_roi_record table.
// Nothing here computes a number — it only converts a number already
// computed by internal/reconcile into the shapes Postgres understands, and
// back (Constitution Principle I — this package is a storage adapter, not a
// place where arithmetic happens). This file also backs the get_promotion_roi
// / list_negative_roi_promotions MCP tool contracts directly, the same way
// LoadDailyReconciliation backs get_daily_summary.

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
)

// SavePromotionRoiRecord persists a reconcile.PromotionRoiRecord via the
// sqlc-generated UpsertPromotionRoiRecord query. The caller (the promotion
// ingest -> reconcile -> persist pipeline) is the only writer; the model
// layer never calls this (data-model.md's PromotionRoiRecord validation
// rule mirrors DailyReconciliation's).
func SavePromotionRoiRecord(ctx context.Context, q Querier, rec reconcile.PromotionRoiRecord) (PromotionRoiRecord, error) {
	refsJSON, err := marshalOrEmptyArray(rec.SourceRowRefs)
	if err != nil {
		return PromotionRoiRecord{}, fmt.Errorf("storage: marshal source_row_refs: %w", err)
	}

	params := UpsertPromotionRoiRecordParams{
		Platform:        rec.Platform,
		CampaignID:      rec.CampaignID,
		Period:          PromotionPeriodRange(rec.PeriodStart, rec.PeriodEnd),
		Spend:           centsToNumeric(rec.SpendCents),
		FlaggedNegative: rec.FlaggedNegative,
		SourceRowRefs:   refsJSON,
	}
	// AttributedIncrementalOrders/Revenue/Roi are left at their zero
	// (invalid/NULL) value when reconcile reported nil — FR-013's "never
	// estimated" guarantee must survive the trip into Postgres, not just
	// live in the Go struct. The roi_requires_attribution and
	// flagged_negative_requires_roi CHECK constraints in the schema are the
	// second, DB-level gate on the same invariant.
	if rec.AttributedIncrementalOrders != nil {
		params.AttributedIncrementalOrders = pgtype.Int4{Int32: int32(*rec.AttributedIncrementalOrders), Valid: true}
	}
	if rec.AttributedIncrementalRevenueCents != nil {
		params.AttributedIncrementalRevenue = centsToNumeric(*rec.AttributedIncrementalRevenueCents)
	}
	if rec.ROICents != nil {
		params.Roi = centsToNumeric(*rec.ROICents)
	}

	return q.UpsertPromotionRoiRecord(ctx, params)
}

// LoadPromotionRoiRecordsByCampaign backs get_promotion_roi's campaign_id
// input form.
func LoadPromotionRoiRecordsByCampaign(ctx context.Context, q Querier, campaignID string) ([]reconcile.PromotionRoiRecord, error) {
	rows, err := q.GetPromotionRoiByCampaign(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	return promotionRowsToDomain(rows)
}

// LoadPromotionRoiRecordsByPlatformAndPeriod backs get_promotion_roi's
// platform+period input form.
func LoadPromotionRoiRecordsByPlatformAndPeriod(ctx context.Context, q Querier, platform string, start, end time.Time) ([]reconcile.PromotionRoiRecord, error) {
	rows, err := q.GetPromotionRoiByPlatformAndPeriod(ctx, GetPromotionRoiByPlatformAndPeriodParams{
		Platform: platform,
		Column2:  PromotionPeriodRange(start, end),
	})
	if err != nil {
		return nil, err
	}
	return promotionRowsToDomain(rows)
}

// LoadNegativeRoiPromotionsInPeriod backs the list_negative_roi_promotions
// MCP tool contract (spec User Story 4 / SC-006).
func LoadNegativeRoiPromotionsInPeriod(ctx context.Context, q Querier, start, end time.Time) ([]reconcile.PromotionRoiRecord, error) {
	rows, err := q.ListNegativeRoiPromotions(ctx, PromotionPeriodRange(start, end))
	if err != nil {
		return nil, err
	}
	return promotionRowsToDomain(rows)
}

// LoadDistinctCampaignIDs returns every real, persisted campaign_id — the
// bounded, typed set internal/mcptools matches a human-readable or
// shortened campaign reference against (e.g. "LUNCHFIX" or "Banner Ad -
// Lunch Fix Menu (JET-CAMP-LUNCHFIX)" both resolving to the real
// JET-CAMP-LUNCHFIX id) rather than requiring an exact string match or
// letting the model guess an id that doesn't exist (Constitution Principle
// III). See docs/plan.md's mistakes log, "campaign name/entity lookup
// defect", for the bug this backs the fix for.
func LoadDistinctCampaignIDs(ctx context.Context, q Querier) ([]string, error) {
	return q.ListDistinctCampaignIDs(ctx)
}

func promotionRowsToDomain(rows []PromotionRoiRecord) ([]reconcile.PromotionRoiRecord, error) {
	out := make([]reconcile.PromotionRoiRecord, 0, len(rows))
	for _, row := range rows {
		rec, err := PromotionRoiRecordToDomain(row)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

// PromotionRoiRecordToDomain converts one sqlc-scanned row into
// internal/reconcile's domain representation — the exact inverse of
// SavePromotionRoiRecord's write path — so a save-then-load round trip can
// be asserted lossless field-for-field (see promotion_test.go's live
// -Postgres integration test).
func PromotionRoiRecordToDomain(row PromotionRoiRecord) (reconcile.PromotionRoiRecord, error) {
	start, end, err := PeriodFromRange(row.Period)
	if err != nil {
		return reconcile.PromotionRoiRecord{}, fmt.Errorf("storage: period: %w", err)
	}
	spendCents, err := numericToCentsAnyScale(row.Spend)
	if err != nil {
		return reconcile.PromotionRoiRecord{}, fmt.Errorf("storage: spend: %w", err)
	}
	var refs []reconcile.SourceRowRef
	if err := json.Unmarshal(nonNilJSON(row.SourceRowRefs), &refs); err != nil {
		return reconcile.PromotionRoiRecord{}, fmt.Errorf("storage: unmarshal source_row_refs: %w", err)
	}

	rec := reconcile.PromotionRoiRecord{
		Platform:        row.Platform,
		CampaignID:      row.CampaignID,
		PeriodStart:     start,
		PeriodEnd:       end,
		SpendCents:      spendCents,
		FlaggedNegative: row.FlaggedNegative,
		SourceRowRefs:   refs,
		Origin:          row.Origin,
		CreatedAt:       row.CreatedAt.Time,
	}
	if row.ReplacesCampaignID.Valid {
		replaces := row.ReplacesCampaignID.String
		rec.ReplacesCampaignID = &replaces
	}
	if row.AttributedIncrementalOrders.Valid {
		n := int(row.AttributedIncrementalOrders.Int32)
		rec.AttributedIncrementalOrders = &n
	}
	if row.AttributedIncrementalRevenue.Valid {
		revCents, err := numericToCentsAnyScale(row.AttributedIncrementalRevenue)
		if err != nil {
			return reconcile.PromotionRoiRecord{}, fmt.Errorf("storage: attributed_incremental_revenue: %w", err)
		}
		rec.AttributedIncrementalRevenueCents = &revCents
	}
	if row.Roi.Valid {
		roiCents, err := numericToCentsAnyScale(row.Roi)
		if err != nil {
			return reconcile.PromotionRoiRecord{}, fmt.Errorf("storage: roi: %w", err)
		}
		rec.ROICents = &roiCents
	}
	return rec, nil
}

// PromotionPeriodRange builds the inclusive daterange Postgres expects for
// promotion_roi_record.period (and for the period-overlap query parameters
// on GetPromotionRoiByPlatformAndPeriod / ListNegativeRoiPromotions) from a
// calendar period_start/period_end that are both inclusive dates, per
// fixtures/README.md's promotion export ("2026-08-01 to 2026-08-07").
//
// Postgres canonicalizes any DATERANGE bound to the form [start, end) on
// storage, regardless of how it was inserted — a documented Postgres
// behavior for discrete range types, not a bug here. PeriodFromRange below
// undoes that canonicalization symmetrically on read, so PeriodStart/
// PeriodEnd round-trip exactly as the original inclusive calendar dates
// rather than drifting by a day after the first save.
func PromotionPeriodRange(start, end time.Time) pgtype.Range[pgtype.Date] {
	return pgtype.Range[pgtype.Date]{
		Lower:     pgtype.Date{Time: start, Valid: true},
		Upper:     pgtype.Date{Time: end, Valid: true},
		LowerType: pgtype.Inclusive,
		UpperType: pgtype.Inclusive,
		Valid:     true,
	}
}

// PeriodFromRange is PromotionPeriodRange's exact inverse — see its comment
// for why the exclusive-upper-bound handling here is necessary rather than
// paranoid.
func PeriodFromRange(r pgtype.Range[pgtype.Date]) (start, end time.Time, err error) {
	if !r.Valid || r.LowerType == pgtype.Empty {
		return time.Time{}, time.Time{}, fmt.Errorf("period range is not valid")
	}
	if r.LowerType == pgtype.Unbounded || r.UpperType == pgtype.Unbounded {
		return time.Time{}, time.Time{}, fmt.Errorf("unbounded period range")
	}

	start = r.Lower.Time
	if r.LowerType == pgtype.Exclusive {
		start = start.AddDate(0, 0, 1)
	}
	end = r.Upper.Time
	if r.UpperType == pgtype.Exclusive {
		end = end.AddDate(0, 0, -1)
	}
	return start, end, nil
}

// numericToCentsAnyScale converts a pgtype.Numeric back into integer cents
// regardless of the column's declared decimal scale. It generalizes
// reconciliation.go's numericToCents (which assumes NUMERIC(*, 2) and
// refuses anything more precise) because promotion_roi_record.roi is
// NUMERIC(12, 4) — Postgres always returns a numeric at its column's
// declared scale, so roi comes back with Exp=-4, not -2. Every roi value
// this system ever writes is itself derived from cents math (see
// SavePromotionRoiRecord / ComputePromotionRoiRecords), so it must always
// divide back down to a whole number of cents exactly; this function
// refuses (Constitution Principle II) rather than silently truncating real
// sub-cent precision if that ever stops being true.
func numericToCentsAnyScale(n pgtype.Numeric) (int64, error) {
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
	v := new(big.Int).Set(n.Int)
	switch {
	case shift == 0:
		// already exactly cents
	case shift > 0:
		scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(shift)), nil)
		v.Mul(v, scale)
	default:
		scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-shift)), nil)
		q, r := new(big.Int), new(big.Int)
		q.QuoRem(v, scale, r)
		if r.Sign() != 0 {
			return 0, fmt.Errorf("numeric %v has more precision than cents can represent exactly (exp=%d) — refusing to truncate", n.Int, n.Exp)
		}
		v = q
	}
	if !v.IsInt64() {
		return 0, fmt.Errorf("numeric value %s out of int64 cents range", v.String())
	}
	return v.Int64(), nil
}

// IsCampaignFlaggedNegative reports whether ANY persisted promotion_roi_record
// for campaignID currently has flagged_negative = true — the exact fact
// list_negative_roi_promotions itself surfaces, queried directly by
// campaign_id rather than by period. Backs FR-007's server-side
// re-verification of a POST /api/promotions "replaces" claim: the handler
// must not trust a client-supplied flag, and must not assume a period,
// since the owner may reasonably reference a flagged campaign without
// re-typing its exact date range.
func IsCampaignFlaggedNegative(ctx context.Context, q Querier, campaignID string) (bool, error) {
	records, err := q.GetPromotionRoiByCampaign(ctx, campaignID)
	if err != nil {
		return false, err
	}
	for _, r := range records {
		if r.FlaggedNegative {
			return true, nil
		}
	}
	return false, nil
}

// NewOwnerPromotion is the plain-Go input to CreateOwnerPromotion —
// internal/httpapi's POST /api/promotions handler builds this after FR-007's
// validation has already run; this function performs no validation of its
// own beyond what the CreateOwnerPromotion sqlc query's own CHECK
// constraints enforce, per this package's doc comment ("nothing here
// computes a number"). Named distinctly from the sqlc-generated
// CreateOwnerPromotionParams (this file's own inverse-conversion pattern —
// see PromotionRoiRecordToDomain — rather than a name collision with
// generated code).
type NewOwnerPromotion struct {
	Platform           string
	CampaignID         string
	PeriodStart        time.Time
	PeriodEnd          time.Time
	SpendCents         int64
	ReplacesCampaignID *string
}

// CreateOwnerPromotion inserts a new owner_created promotion_roi_record
// (User Story 3 / FR-005-FR-008). Unlike SavePromotionRoiRecord, this is a
// plain insert with no ON CONFLICT upsert: a second submission naming the
// same platform/campaign_id/period is a genuine duplicate attempt from a
// person, not a re-run of the same deterministic pipeline, so it surfaces as
// a real unique-violation error for the handler to render as a 409, not a
// silent overwrite.
func CreateOwnerPromotion(ctx context.Context, q Querier, p NewOwnerPromotion) (reconcile.PromotionRoiRecord, error) {
	refsJSON, err := marshalOrEmptyArray[reconcile.SourceRowRef](nil)
	if err != nil {
		return reconcile.PromotionRoiRecord{}, fmt.Errorf("storage: marshal source_row_refs: %w", err)
	}

	var replaces pgtype.Text
	if p.ReplacesCampaignID != nil {
		replaces = pgtype.Text{String: *p.ReplacesCampaignID, Valid: true}
	}

	row, err := q.CreateOwnerPromotion(ctx, CreateOwnerPromotionParams{
		Platform:           p.Platform,
		CampaignID:         p.CampaignID,
		Period:             PromotionPeriodRange(p.PeriodStart, p.PeriodEnd),
		Spend:              centsToNumeric(p.SpendCents),
		SourceRowRefs:      refsJSON,
		ReplacesCampaignID: replaces,
	})
	if err != nil {
		return reconcile.PromotionRoiRecord{}, err
	}
	return PromotionRoiRecordToDomain(row)
}

// LoadAllPromotionRoiRecords reads every persisted promotion record, in
// period then campaign order. Backs GET /api/promotions — the Promotions
// page shows the whole set rather than a window, since "which campaigns paid
// for themselves" is not a question a default date range can answer without
// silently omitting campaigns.
func LoadAllPromotionRoiRecords(ctx context.Context, q Querier) ([]reconcile.PromotionRoiRecord, error) {
	rows, err := q.ListAllPromotionRoiRecords(ctx)
	if err != nil {
		return nil, err
	}
	return promotionRowsToDomain(rows)
}
