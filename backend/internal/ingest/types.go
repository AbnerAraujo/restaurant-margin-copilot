// Package ingest parses the delivery-platform, POS, and supplier
// cost-sheet CSV exports into typed Go records. Parsing here is
// deliberately dumb: it reads and validates individual rows and does not
// interpret them (no deduplication, no refund netting, no margin math) —
// that business logic belongs to internal/reconcile, per plan.md's package
// boundaries. Every record carries a SourceRowRef back to its exact file
// and row so provenance survives from the raw CSV all the way to a number
// shown to the user (Constitution Principle IV).
package ingest

import "time"

// SourceRowRef identifies exactly which file and row a parsed record came
// from. Row is 1-based and counts the header as row 1, matching what a
// human opening the CSV in a spreadsheet would call it.
type SourceRowRef struct {
	File string `json:"file"`
	Row  int    `json:"row"`
}

// DeliveryRecord is one settlement line from a delivery-platform export
// (iFood, Just Eat Takeaway, ...). Money fields are integer cents
// (internal/money) so downstream reconciliation never touches float64.
// CommissionCents and NetPayoutCents are the values as given in the file —
// carried through uninterpreted so internal/reconcile can independently
// recompute and cross-check them against SubtotalCents and
// CommissionRateBps, rather than trusting a pre-aggregated source column.
type DeliveryRecord struct {
	Ref               SourceRowRef
	Platform          string
	OrderID           string
	OrderDate         time.Time
	OrderTime         string
	SubtotalCents     int64
	CommissionRateBps int64 // commission_rate_pct expressed as hundredths-of-a-percent (23% -> 2300)
	CommissionCents   int64
	NetPayoutCents    int64
	Status            string // normalized lowercase: "completed", "refunded", ...
	RefundDate        *time.Time
	CampaignID        string
	Notes             string
}

// POSRecord is one order line from the in-house POS export (dine-in,
// takeaway, phone — not delivery-platform orders, which have no commission
// or platform payout to reconcile).
type POSRecord struct {
	Ref           SourceRowRef
	OrderID       string
	OrderDate     time.Time
	OrderTime     string
	Channel       string
	GrossCents    int64
	PaymentMethod string
	Status        string
}

// CostInvoiceRecord is one supplier invoice line from the cost sheet.
// Supplier billing is not daily (fixtures/README.md: "produce ~every 3
// days, protein weekly, ...") — internal/reconcile, not this package,
// decides how invoices allocate to days without one of their own.
type CostInvoiceRecord struct {
	Ref         SourceRowRef
	InvoiceID   string
	InvoiceDate time.Time
	Supplier    string
	Category    string
	AmountCents int64
	Notes       string
}
