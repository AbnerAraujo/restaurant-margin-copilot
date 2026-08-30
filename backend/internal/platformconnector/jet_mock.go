package platformconnector

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/money"
)

// --- The simulated Just Eat Takeaway partner API ---------------------------
//
// Shaped as a plausible Just Eat Takeaway-style partner API. Nothing here
// is real: no credentials, no network, no Just Eat Takeaway. See the
// package doc.
//
// It disagrees with the iFood mock (ifood_mock.go) on every format
// decision either of them makes:
//
//   - camelCase field names
//   - a bare "data" array with a sibling "cursor" object
//   - CURSOR pagination, with an opaque base64 token
//   - money as INTEGER MINOR UNITS (4200), never a decimal string
//   - NO commission rate reported at all — only the charged amount
//   - timestamps as EPOCH MILLISECONDS in UTC
//   - status vocabulary DELIVERED / REFUNDED
//   - a refunded order reported with ALREADY-NEGATIVE minor units
//
// Two of those force real work rather than a rename:
//
// No rate. ingest.DeliveryRecord.CommissionRateBps is not decoration —
// reconcile.recomputeCommissionCents independently recomputes commission
// from subtotal and rate and raises a commission_mismatch flag when the
// two disagree. A platform that reports no rate leaves that field to be
// DERIVED here, and getting it wrong would raise a mismatch flag on every
// single Just Eat Takeaway order in the dataset: a wall of discrepancies
// caused entirely by the integration, drowning the real ones the product
// exists to surface.
//
// UTC timestamps. internal/reconcile files an order under
// OrderDate.Format("2006-01-02"), so the calendar day is decided at
// normalization time and can never be corrected later. A 21:30 local order
// is 00:30 the NEXT day in UTC; reading the date straight off the epoch
// would move that order — and its revenue and its commission — into
// tomorrow's margin, on both days, with nothing to flag it. This adapter
// converts into merchantZone first. See
// TestJETAdapter_LateEveningOrderKeepsItsLocalDay.

// jetPageSize is deliberately a different page size from iFood's, so a
// day's orders split at different boundaries in the two feeds and no test
// can pass by accident on aligned pagination.
const jetPageSize = 9

// jetOrdersResponse is the mock's wire envelope.
type jetOrdersResponse struct {
	Data   []jetOrderDTO `json:"data"`
	Cursor jetCursor     `json:"cursor"`
}

type jetCursor struct {
	Next    string `json:"next"`
	HasMore bool   `json:"hasMore"`
}

type jetOrderDTO struct {
	OrderReference   string `json:"orderReference"`
	PlacedAtEpochMs  int64  `json:"placedAtEpochMs"`
	Currency         string `json:"currency"`
	GrossAmountMinor int64  `json:"grossAmountMinor"`
	CommissionMinor  int64  `json:"commissionMinor"`
	PayoutMinor      int64  `json:"payoutMinor"`
	FulfilmentState  string `json:"fulfilmentState"`
	// RefundedAtEpochMs is null on a delivered order — a pointer, so
	// "not refunded" and "refunded at the epoch" stay distinguishable.
	RefundedAtEpochMs    *int64 `json:"refundedAtEpochMs"`
	MarketingCampaignRef string `json:"marketingCampaignRef,omitempty"`
}

// jetUpstream is the mock server. Like ifoodUpstream it returns raw JSON
// bytes; see that type's doc for why the round trip is deliberate.
type jetUpstream struct{}

// getOrders is the simulated GET /partner/orders?day=YYYY-MM-DD&cursor=...
// An empty cursor means the first page.
func (u jetUpstream) getOrders(date time.Time, cursor string) ([]byte, error) {
	offset, err := decodeJETCursor(cursor)
	if err != nil {
		return nil, err
	}

	orders := simulateDay(PlatformJustEatTakeaway, date, jetCommissionBps)
	end := offset + jetPageSize
	if offset > len(orders) {
		offset = len(orders)
	}
	if end > len(orders) {
		end = len(orders)
	}

	dtos := make([]jetOrderDTO, 0, end-offset)
	for _, o := range orders[offset:end] {
		dto := jetOrderDTO{
			OrderReference:       fmt.Sprintf("JET-SIM-%s-%04d", date.Format("20060102"), o.Seq),
			PlacedAtEpochMs:      o.PlacedAt.UnixMilli(),
			Currency:             "USD",
			GrossAmountMinor:     o.SubtotalCents,
			CommissionMinor:      o.CommissionCents,
			PayoutMinor:          o.PayoutCents,
			FulfilmentState:      "DELIVERED",
			MarketingCampaignRef: o.CampaignCode,
		}
		if o.Refunded {
			// Already negative on the wire — the opposite of what the
			// iFood mock does with the same business event.
			refundedAt := o.RefundedAt.UnixMilli()
			dto.FulfilmentState = "REFUNDED"
			dto.RefundedAtEpochMs = &refundedAt
			dto.GrossAmountMinor = -o.SubtotalCents
			dto.CommissionMinor = -o.CommissionCents
			dto.PayoutMinor = -o.PayoutCents
		}
		dtos = append(dtos, dto)
	}

	resp := jetOrdersResponse{Data: dtos, Cursor: jetCursor{}}
	if end < len(orders) {
		resp.Cursor = jetCursor{Next: encodeJETCursor(end), HasMore: true}
	}
	return json.Marshal(resp)
}

// encodeJETCursor/decodeJETCursor make the cursor genuinely opaque to the
// adapter — it is a base64 token the client must round-trip, not an offset
// it can compute. That is what cursor pagination actually is, and it is
// what makes this different from iFood's page numbers rather than a
// cosmetic rename of them.
func encodeJETCursor(offset int) string {
	return base64.StdEncoding.EncodeToString([]byte("offset:" + strconv.Itoa(offset)))
}

func decodeJETCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("platformconnector: Just Eat Takeaway cursor %q is not valid base64: %w", cursor, err)
	}
	value, ok := strings.CutPrefix(string(raw), "offset:")
	if !ok {
		return 0, fmt.Errorf("platformconnector: Just Eat Takeaway cursor %q does not decode to a known cursor form", cursor)
	}
	offset, err := strconv.Atoi(value)
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("platformconnector: Just Eat Takeaway cursor %q does not decode to a valid offset", cursor)
	}
	return offset, nil
}

// jetAdapter implements Client over jetUpstream.
type jetAdapter struct {
	upstream jetUpstream
}

// NewJustEatTakeawayClient returns the simulated Just Eat Takeaway connector.
func NewJustEatTakeawayClient() Client { return jetAdapter{} }

func (a jetAdapter) Platform() Platform { return PlatformJustEatTakeaway }

func (a jetAdapter) Describe() Description {
	return Description{
		Platform:          PlatformJustEatTakeaway,
		Name:              PlatformJustEatTakeaway.DisplayName(),
		Simulated:         true,
		WireFormat:        "cursor-paginated JSON, camelCase, amounts as integer minor units, epoch-millisecond timestamps, no commission rate reported (derived here)",
		CommissionRatePct: money.FormatCents(jetCommissionBps),
		Endpoint:          jetEndpoint(),
	}
}

func jetEndpoint() string {
	return "simulated://just-eat-takeaway-partner-api/partner/orders"
}

// FetchDeliveryRevenue walks the simulated cursor and normalizes every
// order into ingest.DeliveryRecord.
func (a jetAdapter) FetchDeliveryRevenue(ctx context.Context, date time.Time) ([]ingest.DeliveryRecord, error) {
	var out []ingest.DeliveryRecord
	cursor := ""

	for page := 1; page <= maxPagesPerDay; page++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("platformconnector: Just Eat Takeaway fetch for %s: %w", date.Format(dateLayout), err)
		}

		raw, err := a.upstream.getOrders(date, cursor)
		if err != nil {
			return nil, fmt.Errorf("platformconnector: Just Eat Takeaway upstream for %s page %d: %w", date.Format(dateLayout), page, err)
		}

		var resp jetOrdersResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, fmt.Errorf("platformconnector: decoding Just Eat Takeaway response for %s page %d: %w", date.Format(dateLayout), page, err)
		}

		src := fmt.Sprintf("%s?day=%s&page=%d", jetEndpoint(), date.Format(dateLayout), page)
		for i, dto := range resp.Data {
			rec, err := a.normalize(dto, ingest.SourceRowRef{File: src, Row: i + 1})
			if err != nil {
				return nil, err
			}
			out = append(out, rec)
		}

		if !resp.Cursor.HasMore {
			return out, nil
		}
		cursor = resp.Cursor.Next
	}

	return nil, fmt.Errorf("platformconnector: Just Eat Takeaway returned more than %d pages for %s — refusing rather than returning a partial day", maxPagesPerDay, date.Format(dateLayout))
}

// normalize converts one Just Eat Takeaway wire order into the shared
// record type. See this file's header comment for the two conversions that
// carry real risk (the derived rate, and the UTC-to-local calendar day).
func (a jetAdapter) normalize(dto jetOrderDTO, ref ingest.SourceRowRef) (ingest.DeliveryRecord, error) {
	fail := func(field, detail string) (ingest.DeliveryRecord, error) {
		return ingest.DeliveryRecord{}, fmt.Errorf("platformconnector: Just Eat Takeaway order %s: %s: %s", dto.OrderReference, field, detail)
	}

	if dto.GrossAmountMinor == 0 {
		// A zero-value order cannot yield a commission rate, and this
		// product has no concept of a free order. Refuse rather than
		// emit a record with a fabricated 0 bps rate, which would then
		// disagree with the reported commission and fire a
		// commission_mismatch flag blaming the data instead of the
		// integration.
		return fail("grossAmountMinor", "is zero — a commission rate cannot be derived from it, and this connector will not invent one")
	}

	// The derived rate. Just Eat Takeaway reports what it charged but
	// never at what rate; ingest.DeliveryRecord carries the rate because
	// reconcile independently recomputes commission from it. Recovering it
	// here, in basis points, with the same round-half-away-from-zero
	// convention reconcile uses, is what keeps those two in agreement.
	// Sign-safe: on a refunded order both operands are negative, so the
	// derived rate stays positive.
	rateBps := money.DivRoundHalfUp(dto.CommissionMinor*10000, dto.GrossAmountMinor)

	// Epoch milliseconds are UTC. .In(merchantZone) is the whole
	// correctness of the calendar day; see this file's header comment.
	placedAt := time.UnixMilli(dto.PlacedAtEpochMs).In(merchantZone)
	y, m, d := placedAt.Date()

	rec := ingest.DeliveryRecord{
		Ref:               ref,
		Platform:          PlatformJustEatTakeaway.DisplayName(),
		OrderID:           dto.OrderReference,
		OrderDate:         time.Date(y, m, d, 0, 0, 0, 0, merchantZone),
		OrderTime:         placedAt.Format("15:04"),
		SubtotalCents:     dto.GrossAmountMinor,
		CommissionRateBps: rateBps,
		CommissionCents:   dto.CommissionMinor,
		NetPayoutCents:    dto.PayoutMinor,
		Status:            "completed",
		CampaignID:        dto.MarketingCampaignRef,
		Notes:             "Simulated Just Eat Takeaway partner-API order — not a real settlement.",
	}

	switch dto.FulfilmentState {
	case "DELIVERED":
		if dto.RefundedAtEpochMs != nil {
			return fail("refundedAtEpochMs", "is set on a DELIVERED order — refusing rather than guessing whether this counts as revenue or a reversal")
		}
	case "REFUNDED":
		if dto.RefundedAtEpochMs == nil {
			return fail("refundedAtEpochMs", "is null on a REFUNDED order — refusing rather than attributing the reversal to a date this connector made up")
		}
		refundedAt := time.UnixMilli(*dto.RefundedAtEpochMs).In(merchantZone)
		ry, rm, rd := refundedAt.Date()
		refundDate := time.Date(ry, rm, rd, 0, 0, 0, 0, merchantZone)

		rec.Status = "refunded"
		rec.RefundDate = &refundDate
		rec.Notes = "Simulated Just Eat Takeaway partner-API refund — not a real settlement."
		// No sign flip: this platform already reports a refund negative,
		// which is this repository's own convention. The iFood adapter
		// has to negate; this one must NOT, and the Proxy's contract
		// check is what proves each of them got its own case right.
	default:
		return fail("fulfilmentState", fmt.Sprintf("unrecognized state %q — refusing rather than guessing whether this order counts as revenue", dto.FulfilmentState))
	}

	return rec, nil
}
