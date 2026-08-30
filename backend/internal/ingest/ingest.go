package ingest

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strings"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/money"
)

// utf8BOM is the 3-byte UTF-8 byte-order-mark Microsoft Excel on Windows
// prepends by default when saving a file as "CSV UTF-8". It is invisible in
// most editors, but left un-stripped it glues onto the first header cell
// (e.g. an "invoice_id" header cell), which then fails every alias match in
// columns.go's headerIndex and surfaces as a "required column not found"
// error that points the user at a column plainly visible in the file.
// Stripped once here, at the one place file bytes become a CSV reader, so
// every parser in this package (and promo.go's) is immune without a special
// case anywhere else.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

func readAllRows(r io.Reader) ([][]string, error) {
	br := bufio.NewReader(r)
	if peek, err := br.Peek(len(utf8BOM)); err == nil && bytes.Equal(peek, utf8BOM) {
		_, _ = br.Discard(len(utf8BOM))
	}

	cr := csv.NewReader(br)
	cr.FieldsPerRecord = -1 // tolerate short rows (a trailing optional column simply omitted)
	cr.TrimLeadingSpace = true
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// requireDataRows refuses a file that parsed successfully but produced zero
// actual data records — a header-only CSV (or one whose only content rows
// are blank), which readAllRows' "is empty" check does NOT catch: that check
// only fires on a truly empty/zero-byte file (len(rows) == 0), while a
// header-only file parses to len(rows) == 1. Distinguished from "is empty"
// deliberately: this is a different, equally serious failure mode (a
// dataset-wiping commit, not a bad upload) and deserves a message that
// names it rather than reusing "is empty" for a file that plainly isn't.
func requireDataRows(n int, sourceFile, noun string) error {
	if n == 0 {
		return fmt.Errorf("ingest: %s: no data rows found — the file has a header row but no %s rows to ingest", sourceFile, noun)
	}
	return nil
}

// ParseDeliveryExport parses a delivery-platform settlement export
// (iFood, Just Eat Takeaway, or any similarly-shaped platform export) into
// DeliveryRecords. Column matching tolerates realistic real-world header
// variance (see columns.go) rather than requiring this dataset's exact
// column names.
func ParseDeliveryExport(r io.Reader, sourceFile string) ([]DeliveryRecord, error) {
	rows, err := readAllRows(r)
	if err != nil {
		return nil, fmt.Errorf("ingest: reading delivery export %s: %w", sourceFile, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("ingest: %s is empty", sourceFile)
	}

	h := newHeaderIndex(rows[0])
	colPlatform, err := h.require("platform", "platform", "delivery_platform", "source_platform")
	if err != nil {
		return nil, fmt.Errorf("ingest: %s: %w", sourceFile, err)
	}
	colOrderID, err := h.require("order_id", "order_id", "order_number", "id")
	if err != nil {
		return nil, fmt.Errorf("ingest: %s: %w", sourceFile, err)
	}
	colOrderDate, err := h.require("order_date", "order_date", "date")
	if err != nil {
		return nil, fmt.Errorf("ingest: %s: %w", sourceFile, err)
	}
	colOrderTime := h.find("order_time", "time")
	colSubtotal, err := h.require("subtotal", "subtotal", "order_subtotal", "gross_amount", "amount")
	if err != nil {
		return nil, fmt.Errorf("ingest: %s: %w", sourceFile, err)
	}
	colRate, err := h.require("commission_rate_pct", "commission_rate_pct", "commission_rate", "commission_pct")
	if err != nil {
		return nil, fmt.Errorf("ingest: %s: %w", sourceFile, err)
	}
	colCommission, err := h.require("commission_amount", "commission_amount", "commission", "commission_fee")
	if err != nil {
		return nil, fmt.Errorf("ingest: %s: %w", sourceFile, err)
	}
	colNetPayout, err := h.require("net_payout", "net_payout", "payout", "net_amount")
	if err != nil {
		return nil, fmt.Errorf("ingest: %s: %w", sourceFile, err)
	}
	colStatus, err := h.require("status", "status", "order_status")
	if err != nil {
		return nil, fmt.Errorf("ingest: %s: %w", sourceFile, err)
	}
	colRefundDate := h.find("refund_date")
	colCampaign := h.find("campaign_id", "campaign", "campaign_code")
	colNotes := h.find("notes", "note", "comment", "comments")

	// One date-format resolver for the whole file (see date.go's doc
	// comment): order_date and refund_date share one convention within a
	// single export, so both columns feed the same per-file detection.
	dateRes := newDateFormatResolver(gatherDateStrings(rows[1:], colOrderDate, colRefundDate))

	var out []DeliveryRecord
	for i, row := range rows[1:] {
		rowNum := i + 2 // header occupies row 1
		if isBlankRow(row) {
			continue
		}

		orderDate, err := dateRes.parse(get(row, colOrderDate))
		if err != nil {
			return nil, fmt.Errorf("ingest: %s row %d: order_date: %w", sourceFile, rowNum, err)
		}
		subtotalCents, err := money.ParseCents(get(row, colSubtotal))
		if err != nil {
			return nil, fmt.Errorf("ingest: %s row %d: subtotal: %w", sourceFile, rowNum, err)
		}
		rateBps, err := money.ParseFixedPoint(get(row, colRate), 2)
		if err != nil {
			return nil, fmt.Errorf("ingest: %s row %d: commission_rate_pct: %w", sourceFile, rowNum, err)
		}
		commissionCents, err := money.ParseCents(get(row, colCommission))
		if err != nil {
			return nil, fmt.Errorf("ingest: %s row %d: commission_amount: %w", sourceFile, rowNum, err)
		}
		netPayoutCents, err := money.ParseCents(get(row, colNetPayout))
		if err != nil {
			return nil, fmt.Errorf("ingest: %s row %d: net_payout: %w", sourceFile, rowNum, err)
		}

		rec := DeliveryRecord{
			Ref:               SourceRowRef{File: sourceFile, Row: rowNum},
			Platform:          get(row, colPlatform),
			OrderID:           get(row, colOrderID),
			OrderDate:         orderDate,
			SubtotalCents:     subtotalCents,
			CommissionRateBps: rateBps,
			CommissionCents:   commissionCents,
			NetPayoutCents:    netPayoutCents,
			Status:            strings.ToLower(get(row, colStatus)),
		}
		if colOrderTime >= 0 {
			rec.OrderTime = get(row, colOrderTime)
		}
		if colRefundDate >= 0 {
			if raw := get(row, colRefundDate); raw != "" {
				refundDate, err := dateRes.parse(raw)
				if err != nil {
					return nil, fmt.Errorf("ingest: %s row %d: refund_date: %w", sourceFile, rowNum, err)
				}
				rec.RefundDate = &refundDate
			}
		}
		if colCampaign >= 0 {
			rec.CampaignID = get(row, colCampaign)
		}
		if colNotes >= 0 {
			rec.Notes = get(row, colNotes)
		}

		out = append(out, rec)
	}
	if err := requireDataRows(len(out), sourceFile, "order"); err != nil {
		return nil, err
	}
	return out, nil
}

// ParsePOSExport parses the in-house POS export into POSRecords.
func ParsePOSExport(r io.Reader, sourceFile string) ([]POSRecord, error) {
	rows, err := readAllRows(r)
	if err != nil {
		return nil, fmt.Errorf("ingest: reading POS export %s: %w", sourceFile, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("ingest: %s is empty", sourceFile)
	}

	h := newHeaderIndex(rows[0])
	colOrderID, err := h.require("order_id", "order_id", "order_number", "id", "receipt_number")
	if err != nil {
		return nil, fmt.Errorf("ingest: %s: %w", sourceFile, err)
	}
	colOrderDate, err := h.require("order_date", "order_date", "date")
	if err != nil {
		return nil, fmt.Errorf("ingest: %s: %w", sourceFile, err)
	}
	colOrderTime := h.find("order_time", "time")
	colChannel := h.find("channel", "order_type", "service_type")
	colGross, err := h.require("gross_amount", "gross_amount", "amount", "total", "subtotal")
	if err != nil {
		return nil, fmt.Errorf("ingest: %s: %w", sourceFile, err)
	}
	colPayment := h.find("payment_method", "payment_type", "tender")
	colStatus := h.find("status", "order_status")

	// See ParseDeliveryExport's comment: one resolver per file, not per row.
	dateRes := newDateFormatResolver(gatherDateStrings(rows[1:], colOrderDate))

	var out []POSRecord
	for i, row := range rows[1:] {
		rowNum := i + 2
		if isBlankRow(row) {
			continue
		}

		orderDate, err := dateRes.parse(get(row, colOrderDate))
		if err != nil {
			return nil, fmt.Errorf("ingest: %s row %d: order_date: %w", sourceFile, rowNum, err)
		}
		grossCents, err := money.ParseCents(get(row, colGross))
		if err != nil {
			return nil, fmt.Errorf("ingest: %s row %d: gross_amount: %w", sourceFile, rowNum, err)
		}

		rec := POSRecord{
			Ref:        SourceRowRef{File: sourceFile, Row: rowNum},
			OrderID:    get(row, colOrderID),
			OrderDate:  orderDate,
			GrossCents: grossCents,
		}
		if colOrderTime >= 0 {
			rec.OrderTime = get(row, colOrderTime)
		}
		if colChannel >= 0 {
			rec.Channel = get(row, colChannel)
		}
		if colPayment >= 0 {
			rec.PaymentMethod = get(row, colPayment)
		}
		if colStatus >= 0 {
			rec.Status = strings.ToLower(get(row, colStatus))
		} else {
			rec.Status = "completed" // POS exports without a status column are assumed settled sales
		}

		out = append(out, rec)
	}
	if err := requireDataRows(len(out), sourceFile, "order"); err != nil {
		return nil, err
	}
	return out, nil
}

// ParseCostSheet parses the supplier cost sheet into CostInvoiceRecords.
func ParseCostSheet(r io.Reader, sourceFile string) ([]CostInvoiceRecord, error) {
	rows, err := readAllRows(r)
	if err != nil {
		return nil, fmt.Errorf("ingest: reading cost sheet %s: %w", sourceFile, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("ingest: %s is empty", sourceFile)
	}

	h := newHeaderIndex(rows[0])
	colInvoiceID, err := h.require("invoice_id", "invoice_id", "invoice_number", "id")
	if err != nil {
		return nil, fmt.Errorf("ingest: %s: %w", sourceFile, err)
	}
	colInvoiceDate, err := h.require("invoice_date", "invoice_date", "date")
	if err != nil {
		return nil, fmt.Errorf("ingest: %s: %w", sourceFile, err)
	}
	colSupplier, err := h.require("supplier", "supplier", "vendor", "supplier_name")
	if err != nil {
		return nil, fmt.Errorf("ingest: %s: %w", sourceFile, err)
	}
	colCategory := h.find("category", "cost_category", "type")
	colAmount, err := h.require("amount", "amount", "total", "invoice_amount")
	if err != nil {
		return nil, fmt.Errorf("ingest: %s: %w", sourceFile, err)
	}
	colNotes := h.find("notes", "note", "comment", "comments")

	// See ParseDeliveryExport's comment: one resolver per file, not per row.
	dateRes := newDateFormatResolver(gatherDateStrings(rows[1:], colInvoiceDate))

	var out []CostInvoiceRecord
	for i, row := range rows[1:] {
		rowNum := i + 2
		if isBlankRow(row) {
			continue
		}

		invoiceDate, err := dateRes.parse(get(row, colInvoiceDate))
		if err != nil {
			return nil, fmt.Errorf("ingest: %s row %d: invoice_date: %w", sourceFile, rowNum, err)
		}
		amountCents, err := money.ParseCents(get(row, colAmount))
		if err != nil {
			return nil, fmt.Errorf("ingest: %s row %d: amount: %w", sourceFile, rowNum, err)
		}

		rec := CostInvoiceRecord{
			Ref:         SourceRowRef{File: sourceFile, Row: rowNum},
			InvoiceID:   get(row, colInvoiceID),
			InvoiceDate: invoiceDate,
			Supplier:    get(row, colSupplier),
			AmountCents: amountCents,
		}
		if colCategory >= 0 {
			rec.Category = get(row, colCategory)
		}
		if colNotes >= 0 {
			rec.Notes = get(row, colNotes)
		}

		out = append(out, rec)
	}
	if err := requireDataRows(len(out), sourceFile, "invoice"); err != nil {
		return nil, err
	}
	return out, nil
}
