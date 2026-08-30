package ingest

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Paths are relative to this package, pointing at the dataset's checked-in
// hand-authored opening window (backend/cmd/gendata/opening/).
// opening/README.md documents every deliberate irregularity below with
// exact row IDs and independently hand-verified sums — that file is the
// ground truth these tests assert against, not a reimplementation of it.
const (
	openingDeliveryFile = "../../cmd/gendata/opening/delivery_platform_export.csv"
	openingPOSFile      = "../../cmd/gendata/opening/pos_export.csv"
	openingCostFile     = "../../cmd/gendata/opening/supplier_cost_sheet.csv"
)

func openDataFile(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err, "opening-window file must exist: %s", path)
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	require.NoError(t, err)
	return d
}

// --- Irregularity #1: duplicate order (opening/README.md) ---
// order_id IFOOD-20240803-0011 appears twice, byte-for-byte identical.
// Ingestion's job is only to parse rows faithfully — deduplication is
// internal/reconcile's business-logic concern (T012/T014) — so this
// asserts parsing preserves both rows rather than silently collapsing or
// losing one.
func TestParseDeliveryExport_DuplicateOrderBothRowsPreserved(t *testing.T) {
	records, err := ParseDeliveryExport(openDataFile(t, openingDeliveryFile), openingDeliveryFile)
	require.NoError(t, err)

	var matches []DeliveryRecord
	for _, r := range records {
		if r.OrderID == "IFOOD-20240803-0011" {
			matches = append(matches, r)
		}
	}

	require.Lenf(t, matches, 2, "expected both duplicate rows for IFOOD-20240803-0011 to be parsed, not deduplicated at ingest time")
	for _, r := range matches {
		require.Equal(t, "iFood", r.Platform)
		require.Equal(t, int64(2400), r.SubtotalCents)
		require.Equal(t, "completed", r.Status)
		require.Equal(t, mustDate(t, "2024-08-03"), r.OrderDate)
		require.Equal(t, "IFOOD-CAMP-BOOST01", r.CampaignID)
	}
	require.NotEqual(t, matches[0].Ref.Row, matches[1].Ref.Row, "duplicate rows must carry distinct row provenance")
}

// --- Irregularity #2: refund from a prior period (opening/README.md) ---
// order_id IFOOD-20240802-0006 was placed 2024-08-02 and reversed by a
// second row with the same order_id, refund_date=2024-08-09 (one week
// later). Ingestion must preserve order_date as the original order's date
// (not the refund date) and parse refund_date separately.
func TestParseDeliveryExport_RefundRowParsedWithOriginalOrderDate(t *testing.T) {
	records, err := ParseDeliveryExport(openDataFile(t, openingDeliveryFile), openingDeliveryFile)
	require.NoError(t, err)

	var original, refund *DeliveryRecord
	for i := range records {
		r := &records[i]
		if r.OrderID != "IFOOD-20240802-0006" {
			continue
		}
		switch r.Status {
		case "completed":
			original = r
		case "refunded":
			refund = r
		}
	}

	require.NotNil(t, original, "original completed row for IFOOD-20240802-0006 must parse")
	require.NotNil(t, refund, "refund row for IFOOD-20240802-0006 must parse")

	require.Equal(t, int64(6225), original.SubtotalCents)
	require.Nil(t, original.RefundDate)

	require.Equal(t, mustDate(t, "2024-08-02"), refund.OrderDate, "refund row's order_date must stay the original order date, not the refund date")
	require.Equal(t, int64(-6225), refund.SubtotalCents)
	require.Equal(t, int64(-1432), refund.CommissionCents)
	require.Equal(t, int64(-4793), refund.NetPayoutCents)
	require.NotNil(t, refund.RefundDate)
	require.Equal(t, mustDate(t, "2024-08-09"), *refund.RefundDate, "refund_date must be parsed as the settlement date, one week after order_date")
}

// --- Irregularity #3: missing day (opening/README.md) ---
// delivery_platform_export.csv has zero rows for 2024-08-10.
func TestParseDeliveryExport_MissingDayHasNoRows(t *testing.T) {
	records, err := ParseDeliveryExport(openDataFile(t, openingDeliveryFile), openingDeliveryFile)
	require.NoError(t, err)

	for _, r := range records {
		require.NotEqual(t, mustDate(t, "2024-08-10"), r.OrderDate, "delivery export must have zero rows for 2024-08-10 per opening/README.md")
	}
}

// --- Irregularity #4: inconsistent date format across files ---
// delivery_platform_export.csv is ISO YYYY-MM-DD; pos_export.csv is
// DD/MM/YYYY. This must resolve to the same calendar date across both
// files, including the deliberately ambiguous case (day <= 12 and month <=
// 12, e.g. "01/08/2026") where a naive parse could silently swap day/month.
func TestParsePOSExport_InconsistentDateFormatResolvedCorrectly(t *testing.T) {
	records, err := ParsePOSExport(openDataFile(t, openingPOSFile), openingPOSFile)
	require.NoError(t, err)

	byID := make(map[string]POSRecord, len(records))
	for _, r := range records {
		byID[r.OrderID] = r
	}

	// POS-1054..1059 are dated 10/08/2024 in the opening window: they must
	// land on the same missing-delivery calendar day (irregularity #3) that
	// the delivery/cost-sheet ISO exports call 2024-08-10.
	for _, id := range []string{"POS-1054", "POS-1055", "POS-1056", "POS-1057", "POS-1058", "POS-1059"} {
		r, ok := byID[id]
		require.Truef(t, ok, "expected POS record %s", id)
		require.Equal(t, mustDate(t, "2024-08-10"), r.OrderDate, "%s date must resolve to 2024-08-10", id)
	}

	// POS-1000 is dated "01/08/2024" — genuinely ambiguous (day=1,month=8
	// vs. day=8,month=1). The documented DD/MM default must resolve this to
	// 2024-08-01 (August), not 2024-01-08 (January).
	r, ok := byID["POS-1000"]
	require.True(t, ok, "expected POS record POS-1000")
	require.Equal(t, mustDate(t, "2024-08-01"), r.OrderDate, "ambiguous DD/MM date must default to day-first, per the documented assumption")
	require.Equal(t, int64(15250), r.GrossCents)
}

// --- Per-file (not per-row) date format resolution ---
// opening/README.md irregularity #4 documents the DD/MM-vs-MM/DD
// difference as systematic PER FILE, not a row-by-row toss-up. A file's
// format must be established once, from its own unambiguous rows (any row
// with one part > 12), and a later row that unambiguously contradicts that
// established format must be rejected rather than silently reinterpreted
// under its own (different) unambiguous reading.
func TestParsePOSExport_RowDisagreeingWithFilesEstablishedFormatIsRejected(t *testing.T) {
	// Row 1's date "25/03/2026" is unambiguous (25 > 12): day=25, month=3 ->
	// DD/MM/YYYY. That establishes this file's format as DD/MM.
	// Row 2's date "03/25/2026" is ALSO unambiguous (25 > 12, but in the
	// second slot): the only way to read it validly is month=03, day=25 ->
	// MM/DD/YYYY, which contradicts the DD/MM format row 1 established.
	csvData := `order_id,order_date,gross_amount
POS-1,25/03/2026,10.00
POS-2,03/25/2026,12.00
`
	_, err := ParsePOSExport(strings.NewReader(csvData), "synthetic.csv")
	require.Error(t, err, "a row whose only valid reading contradicts the file's established date format must be rejected, not silently reinterpreted")
	require.Contains(t, err.Error(), "row 3")
}

// A file where every row agrees on one format (all DD/MM here) must still
// parse cleanly — this is the non-regression companion to the disagreement
// test above, confirming per-file detection doesn't reject consistent data.
func TestParsePOSExport_ConsistentFileFormatAllRowsAgree(t *testing.T) {
	csvData := `order_id,order_date,gross_amount
POS-1,25/03/2026,10.00
POS-2,01/08/2026,12.00
POS-3,17/01/2026,9.50
`
	records, err := ParsePOSExport(strings.NewReader(csvData), "synthetic.csv")
	require.NoError(t, err)
	require.Len(t, records, 3)

	byID := make(map[string]POSRecord, len(records))
	for _, r := range records {
		byID[r.OrderID] = r
	}
	require.Equal(t, mustDate(t, "2026-03-25"), byID["POS-1"].OrderDate)
	// POS-2's "01/08/2026" is ambiguous on its own, but the file's format
	// was already established as DD/MM by POS-1 and POS-3, so it must
	// resolve to day=1, month=8 (August 1st), consistent with the file.
	require.Equal(t, mustDate(t, "2026-08-01"), byID["POS-2"].OrderDate)
	require.Equal(t, mustDate(t, "2026-01-17"), byID["POS-3"].OrderDate)
}

func TestParseCostSheet_ParsesAllInvoices(t *testing.T) {
	records, err := ParseCostSheet(openDataFile(t, openingCostFile), openingCostFile)
	require.NoError(t, err)
	require.Len(t, records, 14, "opening/README.md documents 14 invoices")

	var total int64
	for _, r := range records {
		total += r.AmountCents
	}
	require.Equal(t, int64(500275), total, "opening/README.md's independently-verified supplier cost total is 5,002.75")
}

// Regression test for a reported HIGH-severity defect: a header-only CSV
// (no data rows at all) parsed to zero records with NO error, since
// readAllRows' "is empty" guard only fires on a truly empty/zero-byte file
// (len(rows) == 0) — a header-only file parses to len(rows) == 1. Left
// unfixed, this let a 0-row upload preview and commit successfully, which
// HandleCommitCostSheet writes as a full REPLACE of the live cost-sheet
// file (FR-008): confirming it would wipe the entire multi-year cost
// history and re-derive every margin to zero input cost. spec.md's own
// Edge Cases section states this must be refused ("header row only").
func TestParseCostSheet_HeaderOnlyFileIsRefused(t *testing.T) {
	csvData := "invoice_id,invoice_date,supplier,category,amount,notes\n"
	_, err := ParseCostSheet(strings.NewReader(csvData), "header_only.csv")
	require.Error(t, err, "a header-only cost sheet must be refused, not accepted as a valid 0-row upload")
	require.Contains(t, err.Error(), "no data rows found")
	require.NotContains(t, err.Error(), "is empty",
		"a header-only file is not the same failure as a zero-byte file — the message must name the real problem")
}

// A file whose only "data" rows are blank lines is the same failure mode as
// header-only: zero actual invoice records.
func TestParseCostSheet_HeaderPlusBlankLinesIsRefused(t *testing.T) {
	csvData := "invoice_id,invoice_date,supplier,category,amount,notes\n\n,,,,,\n"
	_, err := ParseCostSheet(strings.NewReader(csvData), "blank_rows.csv")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no data rows found")
}

// Regression test for a reported HIGH-severity defect: a CSV whose bytes
// start with the UTF-8 byte-order-mark (EF BB BF) — exactly what Microsoft
// Excel on Windows produces by default when saving as "CSV UTF-8" — failed
// with "required column \"invoice_id\" not found", pointing the user at a
// column plainly visible in the file. The 3 BOM bytes were glued onto the
// first header cell, so it never matched any known invoice_id alias.
func TestParseCostSheet_BOMPrefixedFileParsesSuccessfully(t *testing.T) {
	bomPrefixed := "\xEF\xBB\xBFinvoice_id,invoice_date,supplier,category,amount,notes\n" +
		"INV-1,2026-08-02,Acme Foods,produce,125.50,Weekly delivery\n"
	records, err := ParseCostSheet(strings.NewReader(bomPrefixed), "bom.csv")
	require.NoError(t, err, "a leading UTF-8 BOM must be stripped, not treated as part of the header")
	require.Len(t, records, 1)
	require.Equal(t, "INV-1", records[0].InvoiceID)
	require.Equal(t, "Acme Foods", records[0].Supplier)
	require.Equal(t, int64(12550), records[0].AmountCents)
}

// Real-file compatibility (research.md): ingestion must tolerate realistic
// column-name variance, not just the opening window's exact headers. This
// constructs an in-memory delivery export using differently named, reordered,
// and differently-cased columns and confirms parsing still succeeds.
func TestParseDeliveryExport_ToleratesRealisticColumnNameVariance(t *testing.T) {
	csvData := `Order ID,Platform,Order Date,Gross Amount,Commission %,Commission Fee,Payout,Order Status
ORD-1,iFood,2026-08-01,50.00,23,11.50,38.50,Completed
`
	records, err := ParseDeliveryExport(strings.NewReader(csvData), "synthetic.csv")
	require.NoError(t, err)
	require.Len(t, records, 1)

	r := records[0]
	require.Equal(t, "ORD-1", r.OrderID)
	require.Equal(t, "iFood", r.Platform)
	require.Equal(t, mustDate(t, "2026-08-01"), r.OrderDate)
	require.Equal(t, int64(5000), r.SubtotalCents)
	require.Equal(t, int64(2300), r.CommissionRateBps)
	require.Equal(t, int64(1150), r.CommissionCents)
	require.Equal(t, int64(3850), r.NetPayoutCents)
	require.Equal(t, "completed", r.Status)
}

// Principle II (refuse rather than guess) applies to ingestion too: a
// required column that isn't present under any known alias must error
// rather than silently produce zero-valued fields.
func TestParseDeliveryExport_MissingRequiredColumnErrors(t *testing.T) {
	csvData := "platform,order_date,subtotal,commission_rate_pct,commission_amount,net_payout,status\niFood,2026-08-01,10.00,23,2.30,7.70,completed\n"
	_, err := ParseDeliveryExport(strings.NewReader(csvData), "synthetic.csv")
	require.Error(t, err, "missing order_id column must be a hard error, not a silently blank field")
	require.Equal(t, 1, strings.Count(err.Error(), "ingest:"),
		"the \"ingest:\" prefix must appear exactly once, not double-wrapped: %q", err.Error())
}

func TestParseDeliveryExport_UnrecognizedDateFormatErrors(t *testing.T) {
	csvData := "platform,order_id,order_date,subtotal,commission_rate_pct,commission_amount,net_payout,status\niFood,ORD-1,Aug 1 2026,10.00,23,2.30,7.70,completed\n"
	_, err := ParseDeliveryExport(strings.NewReader(csvData), "synthetic.csv")
	require.Error(t, err, "an unrecognized date format must be a hard error, not a silent misparse")
	require.Equal(t, 1, strings.Count(err.Error(), "ingest:"),
		"the \"ingest:\" prefix must appear exactly once, not double-wrapped: %q", err.Error())
}

// Regression test for a reported defect: a bad cost-sheet date produced
// "ingest: costs_bad.csv row 2: invoice_date: ingest: unrecognized date
// format..." — "ingest:" appearing twice because date.go's own error
// already carried the prefix before ParseCostSheet's row-level wrap added
// it again. Covers ParseCostSheet specifically since that's the reported
// call site, in addition to the ParseDeliveryExport coverage above.
func TestParseCostSheet_UnrecognizedDateFormatIsNotDoublePrefixed(t *testing.T) {
	csvData := "invoice_id,invoice_date,supplier,amount\nINV-1,not-a-date,Acme Foods,100.00\n"
	_, err := ParseCostSheet(strings.NewReader(csvData), "costs_bad.csv")
	require.Error(t, err)
	require.Equal(t, 1, strings.Count(err.Error(), "ingest:"),
		"the \"ingest:\" prefix must appear exactly once, not double-wrapped: %q", err.Error())
	require.Contains(t, err.Error(), "costs_bad.csv row 2: invoice_date:")
	require.Contains(t, err.Error(), "unrecognized date format")
}

// Regression test for a reported defect: a bad cost-sheet amount ("abc")
// produced "ingest: bad-amt.csv row 2: amount: money: invalid value \"abc\":
// strconv.ParseInt: parsing \"abc00\": invalid syntax" — an internal
// strconv trace plus the cents-padded string ("abc00", not the "abc" the
// owner actually typed) reaching the UI directly. Same bug class, same
// call-site pattern, as TestParseCostSheet_UnrecognizedDateFormatIsNotDoublePrefixed
// above, just on the amount-parsing path (internal/money) instead of dates.
func TestParseCostSheet_MalformedAmountErrorIsCleanAndHumanReadable(t *testing.T) {
	csvData := "invoice_id,invoice_date,supplier,amount\nINV-1,2026-08-02,Acme Foods,abc\n"
	_, err := ParseCostSheet(strings.NewReader(csvData), "bad-amt.csv")
	require.Error(t, err)
	require.Equal(t, 1, strings.Count(err.Error(), "ingest:"),
		"the \"ingest:\" prefix must appear exactly once, not double-wrapped: %q", err.Error())
	require.Contains(t, err.Error(), "bad-amt.csv row 2: amount:")
	require.Contains(t, err.Error(), `"abc"`, "must name the value the owner actually typed")
	require.NotContains(t, err.Error(), "strconv", "must never leak Go's internal parser trace to the user")
	require.NotContains(t, err.Error(), "abc00", "must never leak the internal cents-padded digit string")
}

// TestParseCostSheet_NegativeAmountIsRefused is a QA-round-5 finding: a
// negative amount ("-50.00") parsed successfully with no validation at
// all, silently REDUCING input_costs and inflating margin with no
// discrepancy flag anywhere to explain why. Unlike a delivery-platform
// subtotal/commission/net_payout — where a negative value is a documented,
// legitimate refund reversal row (cmd/gendata/opening/README.md
// irregularity #2) — this ingestion contract has no concept of a supplier
// credit/return, so a negative cost-sheet amount is always either a sign
// error or unmodeled data, and Constitution Principle II says to refuse
// rather than let it quietly produce a confidently wrong margin figure.
// Mirrors POST /api/promotions' existing "spend must not be negative"
// check (internal/httpapi/promotions_create.go) on the identical class of
// input.
func TestParseCostSheet_NegativeAmountIsRefused(t *testing.T) {
	csvData := "invoice_id,invoice_date,supplier,amount\nINV-1,2026-08-02,Acme Foods,-50.00\n"
	_, err := ParseCostSheet(strings.NewReader(csvData), "negative-amt.csv")
	require.Error(t, err)
	require.Contains(t, err.Error(), "negative-amt.csv row 2: amount:")
	require.Contains(t, err.Error(), "must not be negative")
}

// TestParseCostSheet_ZeroAmountIsAccepted proves the negative-amount
// refusal above is scoped to strictly-negative values, not to zero: a real
// invoice line can legitimately be exactly "0.00" (e.g. a free sample, or
// a line item fully covered by a separate credit applied elsewhere on the
// same invoice), and that must keep parsing the same way it always has.
func TestParseCostSheet_ZeroAmountIsAccepted(t *testing.T) {
	csvData := "invoice_id,invoice_date,supplier,amount\nINV-1,2026-08-02,Acme Foods,0.00\n"
	recs, err := ParseCostSheet(strings.NewReader(csvData), "zero-amt.csv")
	require.NoError(t, err)
	require.Len(t, recs, 1)
	require.Equal(t, int64(0), recs[0].AmountCents)
}

func TestParsePOSExport_TableDriven(t *testing.T) {
	tests := []struct {
		name    string
		csv     string
		wantErr bool
		check   func(t *testing.T, records []POSRecord)
	}{
		{
			name: "basic row parses",
			csv:  "order_id,order_date,order_time,channel,gross_amount,payment_method,status\nPOS-1,15/08/2026,12:00,dine_in,20.00,cash,completed\n",
			check: func(t *testing.T, records []POSRecord) {
				require.Len(t, records, 1)
				require.Equal(t, mustDate(t, "2026-08-15"), records[0].OrderDate)
				require.Equal(t, int64(2000), records[0].GrossCents)
			},
		},
		{
			name:    "malformed amount errors",
			csv:     "order_id,order_date,order_time,channel,gross_amount,payment_method,status\nPOS-1,15/08/2026,12:00,dine_in,not-a-number,cash,completed\n",
			wantErr: true,
		},
		{
			name: "blank trailing line ignored",
			csv:  "order_id,order_date,order_time,channel,gross_amount,payment_method,status\nPOS-1,15/08/2026,12:00,dine_in,20.00,cash,completed\n\n",
			check: func(t *testing.T, records []POSRecord) {
				require.Len(t, records, 1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			records, err := ParsePOSExport(strings.NewReader(tt.csv), "synthetic.csv")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			tt.check(t, records)
		})
	}
}
