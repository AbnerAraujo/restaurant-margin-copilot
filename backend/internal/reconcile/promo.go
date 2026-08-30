package reconcile

import (
	"time"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
)

// PromotionRoiRecord is the deterministic, provenanced output of
// ComputePromotionRoiRecords — one row per promotion/campaign per period
// (data-model.md's PromotionRoiRecord entity). All money fields are integer
// cents (internal/money), never float64, for the same exactness reasons as
// DailyReconciliation.
//
// AttributedIncrementalOrders, AttributedIncrementalRevenueCents, and
// ROICents are nil together, and ONLY together, when incremental revenue
// cannot be attributed from available data (FR-013) — never estimated, not
// even a zero. A caller must check for nil, not assume a numeric default.
type PromotionRoiRecord struct {
	Platform                          string
	CampaignID                        string
	PeriodStart                       time.Time
	PeriodEnd                         time.Time
	SpendCents                        int64
	AttributedIncrementalOrders       *int
	AttributedIncrementalRevenueCents *int64
	// ROICents is the promotion's net dollar return in cents (attributed
	// incremental revenue minus spend) — negative exactly when spend
	// exceeded the incremental revenue it drove (FR-012's "flag any
	// promotion where cost exceeds incremental revenue"). nil when
	// attribution is unavailable (FR-013).
	ROICents *int64
	// FlaggedNegative is ROICents < 0. Always false when ROICents is nil —
	// an unattributable promotion is refused, not flagged as bad
	// (data-model.md: "flagged_negative is only meaningful once roi is
	// known").
	FlaggedNegative bool
	SourceRowRefs   []SourceRowRef

	// Origin, ReplacesCampaignID, and CreatedAt back spec
	// 002-badge-expansion's Campaign-Creation badge (internal/badges) — they
	// do not exist for the pre-002 ingestion path in any meaningful sense
	// beyond their defaults, which is why ComputePromotionRoiRecords below
	// never sets them: every record it produces is, by construction,
	// OriginIngested with no replaces reference.

	// Origin is OriginIngested for every record ComputePromotionRoiRecords
	// produces (the file pipeline) and OriginOwnerCreated for a record
	// created directly through POST /api/promotions (internal/httpapi).
	Origin string
	// ReplacesCampaignID is set only on an OriginOwnerCreated record whose
	// creation was framed as replacing a promotion already flagged
	// negative-ROI (FR-006/FR-007) — nil on every ingested record and on an
	// owner-created record with no replacement claim.
	ReplacesCampaignID *string
	// CreatedAt is when this row was first written to Postgres — the
	// Campaign-Creation badge is dated to this field specifically (not to
	// PeriodStart/PeriodEnd), since what it acknowledges is the act of
	// logging the replacement, not any campaign's own calendar period.
	CreatedAt time.Time

	// PaymentMethod and PointsSpent back the points-payment option
	// (internal/badges' earned Steward points, redeemable at POST
	// /api/promotions instead of paying a campaign's spend in cash).
	// SpendCents above is unaffected either way — it always records the
	// campaign's real dollar cost, so ROI math never depends on how that
	// cost was funded. PaymentMethod is PaymentMethodMoney for every
	// pre-existing and ingested record (the default the migration backfills)
	// and PaymentMethodPoints only for an owner-created record that chose to
	// redeem points; PointsSpent is nil unless PaymentMethod is
	// PaymentMethodPoints, mirroring AttributedIncrementalRevenueCents'
	// nil-means-not-applicable convention elsewhere in this struct.
	PaymentMethod string
	PointsSpent   *int
}

// Origin values for PromotionRoiRecord.Origin (spec 002-badge-expansion).
const (
	OriginIngested     = "ingested"
	OriginOwnerCreated = "owner_created"
)

// PaymentMethod values for PromotionRoiRecord.PaymentMethod.
const (
	PaymentMethodMoney  = "money"
	PaymentMethodPoints = "points"
)

// ComputePromotionRoiRecords turns raw promotion spend records and the
// delivery-platform export into one PromotionRoiRecord per promotion,
// applying FR-012/FR-013's attribution and negative-ROI-flagging rules. As
// with ComputeDailyReconciliations, every number here is produced by this
// function alone — no LLM ever computes or overrides it (Constitution
// Principle I).
//
// Attribution is a tag-join over delivery orders by campaign_id, restricted
// to status=completed and deduplicated first (reusing the exact same
// dedupeDelivery pass ComputeDailyReconciliations uses) — per
// cmd/gendata/opening/README.md: "A campaign's incremental revenue must be computed by
// summing the subtotal of delivery_platform_export.csv rows whose
// campaign_id matches (after deduplication), restricted to status =
// completed." This is a defined, deterministic tagging convention, not
// statistical multi-touch attribution modeling (spec Assumptions) — a
// promotion whose delivery orders were never tagged (or whose only tagged
// day has no delivery data at all, see IFOOD-CAMP-WEEKEND) has zero matches,
// which this function reports as unattributable, never as a computed zero.
func ComputePromotionRoiRecords(promos []ingest.PromotionSpendRecord, delivery []ingest.DeliveryRecord) []PromotionRoiRecord {
	deduped, _ := dedupeDelivery(delivery)

	byCampaign := make(map[string][]ingest.DeliveryRecord)
	for _, d := range deduped {
		if d.CampaignID == "" || d.Status != "completed" {
			continue
		}
		byCampaign[d.CampaignID] = append(byCampaign[d.CampaignID], d)
	}

	out := make([]PromotionRoiRecord, 0, len(promos))
	for _, p := range promos {
		matches := byCampaign[p.CampaignID]

		rec := PromotionRoiRecord{
			Platform:      p.Platform,
			CampaignID:    p.CampaignID,
			PeriodStart:   p.PeriodStart,
			PeriodEnd:     p.PeriodEnd,
			SpendCents:    p.SpendCents,
			SourceRowRefs: []SourceRowRef{p.Ref},
			Origin:        OriginIngested,
			PaymentMethod: PaymentMethodMoney,
		}

		if len(matches) > 0 {
			var revenueCents int64
			for _, m := range matches {
				revenueCents += m.SubtotalCents
				rec.SourceRowRefs = append(rec.SourceRowRefs, m.Ref)
			}
			orders := len(matches)
			roi := revenueCents - p.SpendCents

			rec.AttributedIncrementalOrders = &orders
			rec.AttributedIncrementalRevenueCents = &revenueCents
			rec.ROICents = &roi
			rec.FlaggedNegative = roi < 0
		}
		// len(matches) == 0: AttributedIncrementalOrders,
		// AttributedIncrementalRevenueCents, and ROICents stay nil —
		// FR-013's refusal, enforced here, at the source, not left to the
		// MCP tool layer to paper over.

		out = append(out, rec)
	}
	return out
}
