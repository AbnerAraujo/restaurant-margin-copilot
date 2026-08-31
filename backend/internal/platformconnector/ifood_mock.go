package platformconnector

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/money"
)

// --- The simulated iFood partner API ---------------------------------------
//
// Shaped as a plausible iFood-style merchant orders API. Nothing here is
// real: no credentials, no network, no iFood. See the package doc.
//
// Its defining format choices, every one of them different from the Just
// Eat Takeaway mock in jet_mock.go:
//
//   - snake_case field names
//   - an "orders" array inside an envelope that also carries merchant and
//     page metadata
//   - page-NUMBER pagination ({"number": 1, "size": 12, "total_pages": 3})
//   - money as DECIMAL STRINGS inside a nested {currency, amount} object
//   - commission as a nested object that DOES report an explicit
//     rate_percent
//   - timestamps as RFC 3339 with an explicit offset ("...T19:35:00-03:00")
//   - status vocabulary CONCLUDED / CANCELLED
//   - a cancelled order reported with POSITIVE amounts plus a separate
//     cancellation block
//
// That last one is the trap. This repository's canonical delivery-export
// convention is a NEGATIVE subtotal on a refunded row (see
// cmd/gendata/opening/delivery_platform_export.csv's refund row, and
// reconcile.computeOneDay, which does abs64 on it and adds it to
// RefundsCents while NOT adding it to gross). iFood here does the
// opposite. An adapter that passed the wire value straight through would
// hand reconcile a positive-subtotal row whose status happened to say
// "refunded", and the refund would land in RefundsCents as a positive
// number while the commission reversal never happened — margin moving for
// a reason nothing in the product could explain. ifoodAdapter negates; the
// Proxy checks that it did.

// ifoodPageSize is the mock's page size, chosen small enough that a normal
// day (16-29 orders) genuinely spans two or three pages, so the adapter's
// pagination loop is exercised on every single fetch rather than only in a
// contrived test.
const ifoodPageSize = 12

const ifoodMerchantID = "SIMULATED-MERCHANT-0417"

// ifoodOrdersResponse is the mock's wire envelope.
type ifoodOrdersResponse struct {
	MerchantID string          `json:"merchant_id"`
	Page       ifoodPageMeta   `json:"page"`
	Orders     []ifoodOrderDTO `json:"orders"`
}

type ifoodPageMeta struct {
	Number     int `json:"number"`
	Size       int `json:"size"`
	TotalPages int `json:"total_pages"`
}

type ifoodOrderDTO struct {
	ID         string             `json:"id"`
	CreatedAt  string             `json:"created_at"` // RFC 3339 with offset
	Total      ifoodAmount        `json:"total"`
	Commission ifoodCommission    `json:"commission"`
	NetPayout  ifoodAmount        `json:"net_payout"`
	Status     string             `json:"status"` // CONCLUDED | CANCELLED
	Cancel     *ifoodCancellation `json:"cancellation,omitempty"`
	Campaign   *ifoodCampaign     `json:"campaign,omitempty"`
}

type ifoodAmount struct {
	Currency string `json:"currency"`
	Amount   string `json:"amount"` // decimal string, e.g. "42.00"
}

type ifoodCommission struct {
	RatePercent string      `json:"rate_percent"` // decimal string, e.g. "23.00"
	Charged     ifoodAmount `json:"charged"`
}

type ifoodCancellation struct {
	CancelledAt string `json:"cancelled_at"` // RFC 3339 with offset
	Reason      string `json:"reason"`
}

type ifoodCampaign struct {
	Code string `json:"code"`
}

// ifoodUpstream is the mock server. It returns raw JSON BYTES, not Go
// structs, and its adapter unmarshals them back.
//
// The round trip is not free and skipping it would be simpler. It is here
// because skipping it would also make this whole feature vacuous: if the
// mock handed back records the adapter merely copied, the "proxy that
// solves two different APIs" would be a struct copy with extra steps. The
// heterogeneity has to exist at the wire level for the normalization to be
// real work. This is the one place this feature spends complexity on
// purpose. There is still no HTTP server and no network — the upstream is
// a function that returns JSON.
type ifoodUpstream struct{}

// getOrders is the simulated GET
// /v2/merchants/{id}/orders?date=YYYY-MM-DD&page=N.
func (u ifoodUpstream) getOrders(date time.Time, page int) ([]byte, error) {
	orders := simulateDay(PlatformIFood, date, ifoodCommissionBps)
	totalPages := (len(orders) + ifoodPageSize - 1) / ifoodPageSize
	if totalPages == 0 {
		totalPages = 1
	}

	start := (page - 1) * ifoodPageSize
	end := start + ifoodPageSize
	if start > len(orders) {
		start = len(orders)
	}
	if end > len(orders) {
		end = len(orders)
	}

	dtos := make([]ifoodOrderDTO, 0, end-start)
	for _, o := range orders[start:end] {
		dto := ifoodOrderDTO{
			// Built by the shared deliveryOrderID rather than formatted
			// here, so the POS mock's cross-reference can name exactly
			// this id and the two can never drift apart (seed.go).
			ID:        deliveryOrderID(PlatformIFood, date, o.Seq),
			CreatedAt: o.PlacedAt.Format(time.RFC3339),
			Total:     ifoodAmount{Currency: "USD", Amount: money.FormatCents(o.SubtotalCents)},
			Commission: ifoodCommission{
				// iFood reports the rate it applied. Trailing ".00" on
				// purpose: a real API returns a formatted decimal, and the
				// adapter must not depend on it being an integer.
				RatePercent: money.FormatCents(ifoodCommissionBps),
				Charged:     ifoodAmount{Currency: "USD", Amount: money.FormatCents(o.CommissionCents)},
			},
			NetPayout: ifoodAmount{Currency: "USD", Amount: money.FormatCents(o.PayoutCents)},
			Status:    "CONCLUDED",
		}
		if o.Refunded {
			// Positive amounts, status CANCELLED, refund date in a
			// separate block — see this file's header comment.
			dto.Status = "CANCELLED"
			dto.Cancel = &ifoodCancellation{
				CancelledAt: o.RefundedAt.Format(time.RFC3339),
				Reason:      "CUSTOMER_DISPUTE",
			}
		}
		if o.CampaignCode != "" {
			dto.Campaign = &ifoodCampaign{Code: o.CampaignCode}
		}
		dtos = append(dtos, dto)
	}

	return json.Marshal(ifoodOrdersResponse{
		MerchantID: ifoodMerchantID,
		Page:       ifoodPageMeta{Number: page, Size: ifoodPageSize, TotalPages: totalPages},
		Orders:     dtos,
	})
}

// ifoodAdapter implements Client over ifoodUpstream.
type ifoodAdapter struct {
	upstream ifoodUpstream
}

// NewIFoodClient returns the simulated iFood connector.
func NewIFoodClient() Client { return ifoodAdapter{} }

func (a ifoodAdapter) Platform() Platform { return PlatformIFood }

func (a ifoodAdapter) Describe() Description {
	return Description{
		Platform:          PlatformIFood,
		Name:              PlatformIFood.DisplayName(),
		Simulated:         true,
		WireFormat:        "page-numbered JSON, snake_case, amounts as decimal strings, RFC 3339 timestamps, refunds reported positive with a cancellation block",
		CommissionRatePct: money.FormatCents(ifoodCommissionBps),
		Endpoint:          ifoodEndpoint(),
	}
}

func ifoodEndpoint() string {
	return "simulated://ifood-partner-api/v2/merchants/" + ifoodMerchantID + "/orders"
}

// FetchDeliveryRevenue pages through the simulated upstream and normalizes
// every order into ingest.DeliveryRecord.
func (a ifoodAdapter) FetchDeliveryRevenue(ctx context.Context, date time.Time) ([]ingest.DeliveryRecord, error) {
	var out []ingest.DeliveryRecord

	for page := 1; page <= maxPagesPerDay; page++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("platformconnector: iFood fetch for %s: %w", date.Format(dateLayout), err)
		}

		raw, err := a.upstream.getOrders(date, page)
		if err != nil {
			return nil, fmt.Errorf("platformconnector: iFood upstream for %s page %d: %w", date.Format(dateLayout), page, err)
		}

		var resp ifoodOrdersResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, fmt.Errorf("platformconnector: decoding iFood response for %s page %d: %w", date.Format(dateLayout), page, err)
		}

		src := fmt.Sprintf("%s?date=%s&page=%d", ifoodEndpoint(), date.Format(dateLayout), page)
		for i, dto := range resp.Orders {
			recs, err := a.normalize(dto, ingest.SourceRowRef{File: src, Row: i + 1})
			if err != nil {
				return nil, err
			}
			out = append(out, recs...)
		}

		if page >= resp.Page.TotalPages {
			return out, nil
		}
	}

	// Reached only if the upstream claims more pages than the cap allows.
	// Constitution: an explicit cap on loop iterations, and a refusal
	// rather than a silently truncated day (which would understate
	// delivery revenue and quietly inflate margin).
	return nil, fmt.Errorf("platformconnector: iFood reported more than %d pages for %s — refusing rather than returning a partial day", maxPagesPerDay, date.Format(dateLayout))
}

// normalize converts one iFood wire order into the shared record type.
//
// Four real conversions happen here, none of them a rename:
//   - decimal strings -> integer cents (money.ParseCents)
//   - "23.00" percent -> 2300 basis points (money.ParseFixedPoint, 2dp)
//   - RFC 3339 timestamp -> a calendar date plus an "HH:MM" string, with
//     the date read in the offset the timestamp carries
//   - CANCELLED + positive amounts -> "refunded" + NEGATIVE amounts, this
//     repository's convention (see this file's header comment)
// normalize returns one record for a concluded order, or TWO for a
// cancelled one — see the CANCELLED case below for why.
func (a ifoodAdapter) normalize(dto ifoodOrderDTO, ref ingest.SourceRowRef) ([]ingest.DeliveryRecord, error) {
	fail := func(field string, err error) ([]ingest.DeliveryRecord, error) {
		return nil, fmt.Errorf("platformconnector: iFood order %s: %s: %w", dto.ID, field, err)
	}

	placedAt, err := time.Parse(time.RFC3339, dto.CreatedAt)
	if err != nil {
		return fail("created_at", err)
	}
	subtotal, err := money.ParseCents(dto.Total.Amount)
	if err != nil {
		return fail("total.amount", err)
	}
	rateBps, err := money.ParseFixedPoint(dto.Commission.RatePercent, 2)
	if err != nil {
		return fail("commission.rate_percent", err)
	}
	commission, err := money.ParseCents(dto.Commission.Charged.Amount)
	if err != nil {
		return fail("commission.charged.amount", err)
	}
	payout, err := money.ParseCents(dto.NetPayout.Amount)
	if err != nil {
		return fail("net_payout.amount", err)
	}

	// The date is taken in the offset the timestamp itself carries, which
	// is the merchant's own wall clock — so a 21:30 order stays on the day
	// the restaurant served it. (jet_mock.go has to work harder for the
	// same guarantee; see merchantZone.)
	y, m, d := placedAt.Date()

	rec := ingest.DeliveryRecord{
		Ref:               ref,
		Platform:          PlatformIFood.DisplayName(),
		OrderID:           dto.ID,
		OrderDate:         time.Date(y, m, d, 0, 0, 0, 0, merchantZone),
		OrderTime:         placedAt.Format("15:04"),
		SubtotalCents:     subtotal,
		CommissionRateBps: rateBps,
		CommissionCents:   commission,
		NetPayoutCents:    payout,
		Status:            "completed",
		Notes:             "Simulated iFood partner-API order — not a real settlement.",
	}
	if dto.Campaign != nil {
		rec.CampaignID = dto.Campaign.Code
	}

	switch dto.Status {
	case "CONCLUDED":
		return []ingest.DeliveryRecord{rec}, nil
	case "CANCELLED":
		if dto.Cancel == nil {
			return fail("cancellation", fmt.Errorf("status is CANCELLED but no cancellation block was returned"))
		}
		cancelledAt, err := time.Parse(time.RFC3339, dto.Cancel.CancelledAt)
		if err != nil {
			return fail("cancellation.cancelled_at", err)
		}
		cy, cm, cd := cancelledAt.Date()
		refundDate := time.Date(cy, cm, cd, 0, 0, 0, 0, merchantZone)

		// TWO records, matching the exact convention the CSV path already
		// uses (backend/cmd/gendata/opening/README.md: "reversed by a
		// second row with the same order_id, negative amounts") and
		// internal/reconcile.computeOneDay already implements: the
		// original "completed" charge, PLUS a separate "refunded" reversal
		// with negated amounts. reconcile only ever adds a "completed"
		// row's subtotal to gross — mutating the single record in place
		// (the previous behavior here) meant the order's gross was never
		// added in the first place, so subtracting the refund from it
		// double-penalized margin by the full order amount on every
		// cancellation. Found live: $1,138.24 understated over a 31-day
		// sample, 23 of 31 days affected, zero discrepancy flags raised.
		refunded := rec
		refunded.Status = "refunded"
		refunded.RefundDate = &refundDate
		refunded.SubtotalCents = -subtotal
		refunded.CommissionCents = -commission
		refunded.NetPayoutCents = -payout
		refunded.Notes = "Simulated iFood partner-API refund (" + dto.Cancel.Reason + ") — not a real settlement."

		return []ingest.DeliveryRecord{rec, refunded}, nil
	default:
		return fail("status", fmt.Errorf("unrecognized status %q — refusing rather than guessing whether this order counts as revenue", dto.Status))
	}
}
