package httpapi

// Unit-level tests for POST /api/ingest/cost-sheet/preview and GET
// /api/ingest/cost-sheet/template — no database required (unlike
// promotions_create_test.go's live-Postgres tests): preview is pure
// in-memory parsing (spec FR-005), and template is a static file, so both
// exercise real behavior without touching storage.Querier at all.
//
// The commit endpoint's happy path (write + full pipeline re-run) is
// deliberately NOT exercised here: it mutates internal/livedata.Dir and
// persists real DailyReconciliation rows via the real pipeline, which would
// either require a live database (making this an integration test in a file
// that's otherwise a fast unit test) or a fake store elaborate enough to be
// its own maintenance burden. That path is verified live instead, per
// specs/007-cost-sheet-upload/plan.md's "Manual/live verification" section —
// this file DOES cover commit's pre-write validation refusal, which runs
// before any database or filesystem interaction and needs no store at all.

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// multipartCostSheetRequest builds a multipart/form-data POST request with
// the given CSV content in a "file" field, matching how a real browser
// upload (or the frontend's FormData-based postMultipart) shapes the body.
func multipartCostSheetRequest(t *testing.T, method, path, filename, csvContent string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = io.WriteString(part, csvContent)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	req := httptest.NewRequest(method, path, &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

const validCostSheetCSV = `invoice_id,invoice_date,supplier,category,amount,notes
INV-TEST-001,2026-08-01,Test Produce Co.,produce,100.00,First test invoice
INV-TEST-002,2026-08-02,Test Beverage Co.,beverage,50.25,"Second, test invoice"
`

// TestHandlePreviewCostSheet_AcceptsAValidUpload is spec Acceptance Scenario
// US1.1 / FR-004: a well-formed cost sheet previews every row with its
// recognized fields, and nothing is persisted (this test uses no database
// connection at all, proving persistence cannot have happened).
func TestHandlePreviewCostSheet_AcceptsAValidUpload(t *testing.T) {
	req := multipartCostSheetRequest(t, http.MethodPost, "/api/ingest/cost-sheet/preview", "test_cost_sheet.csv", validCostSheetCSV)
	rec := httptest.NewRecorder()

	HandlePreviewCostSheet(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp PreviewCostSheetResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 2, resp.RowCount)
	require.Len(t, resp.Rows, 2)
	require.Equal(t, "150.25", resp.TotalAmount)

	require.Equal(t, "INV-TEST-001", resp.Rows[0].InvoiceID)
	require.Equal(t, "2026-08-01", resp.Rows[0].InvoiceDate)
	require.Equal(t, "Test Produce Co.", resp.Rows[0].Supplier)
	require.Equal(t, "produce", resp.Rows[0].Category)
	require.Equal(t, "100.00", resp.Rows[0].Amount)
	require.Equal(t, "First test invoice", resp.Rows[0].Notes)
	require.Equal(t, "test_cost_sheet.csv", resp.Rows[0].SourceRowRef.File)
	require.Equal(t, 2, resp.Rows[0].SourceRowRef.Row) // header occupies row 1
}

// TestHandlePreviewCostSheet_AcceptsHeaderAliases proves this endpoint
// really does reuse ingest.ParseCostSheet's own tolerant column matching
// (plan.md's "no new validation logic" claim), not a stricter re-implementation.
func TestHandlePreviewCostSheet_AcceptsHeaderAliases(t *testing.T) {
	csvWithAliases := "invoice_number,date,vendor,type,total,comment\n" +
		"INV-ALIAS-1,2026-08-01,Alias Supplier,protein,75.00,via aliased headers\n"

	req := multipartCostSheetRequest(t, http.MethodPost, "/api/ingest/cost-sheet/preview", "aliased.csv", csvWithAliases)
	rec := httptest.NewRecorder()

	HandlePreviewCostSheet(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp PreviewCostSheetResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.RowCount)
	require.Equal(t, "INV-ALIAS-1", resp.Rows[0].InvoiceID)
	require.Equal(t, "75.00", resp.Rows[0].Amount)
}

// TestHandlePreviewCostSheet_AcceptsABOMPrefixedUpload is a regression test
// for a reported HIGH-severity defect: a CSV whose bytes start with the
// UTF-8 byte-order-mark (EF BB BF) — what Microsoft Excel on Windows
// produces by default when saving as "CSV UTF-8" — was rejected with
// "required column \"invoice_id\" not found", pointing the owner at a
// column plainly visible in the file. The BOM must be stripped before
// header matching, at the ingest.ParseCostSheet layer this handler calls
// unchanged.
func TestHandlePreviewCostSheet_AcceptsABOMPrefixedUpload(t *testing.T) {
	bomPrefixed := "\xEF\xBB\xBF" + validCostSheetCSV
	req := multipartCostSheetRequest(t, http.MethodPost, "/api/ingest/cost-sheet/preview", "bom.csv", bomPrefixed)
	rec := httptest.NewRecorder()

	HandlePreviewCostSheet(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp PreviewCostSheetResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 2, resp.RowCount)
	require.Equal(t, "INV-TEST-001", resp.Rows[0].InvoiceID)
}

// TestHandlePreviewCostSheet_RejectsAHeaderOnlyUpload is a regression test
// for a reported HIGH-severity defect: a CSV with only the header row (zero
// data rows) previewed successfully with row_count: 0, total $0.00, and
// Confirm & Ingest enabled — since ParseCostSheet's old "is empty" guard
// only fired on a truly empty/zero-byte file. Confirming that preview would
// have made HandleCommitCostSheet REPLACE the entire live cost-sheet file
// (FR-008) with a bare header, wiping the whole multi-year cost history.
// spec.md's own Edge Cases section says this exact case ("header row
// only") must be refused.
func TestHandlePreviewCostSheet_RejectsAHeaderOnlyUpload(t *testing.T) {
	headerOnly := "invoice_id,invoice_date,supplier,category,amount,notes\n"
	req := multipartCostSheetRequest(t, http.MethodPost, "/api/ingest/cost-sheet/preview", "header_only.csv", headerOnly)
	rec := httptest.NewRecorder()

	HandlePreviewCostSheet(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code,
		"a header-only file must be refused, not previewed as a valid 0-row upload")
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "invalid_cost_sheet", body["error"])
	require.Contains(t, body["detail"], "no data rows found")
}

// TestHandlePreviewCostSheet_RejectsAMissingRequiredColumn is spec
// Acceptance Scenario US2.1 / FR-003: a specific, real error naming the
// missing column — never a generic failure message.
func TestHandlePreviewCostSheet_RejectsAMissingRequiredColumn(t *testing.T) {
	missingAmount := "invoice_id,invoice_date,supplier,category,notes\n" +
		"INV-BAD-1,2026-08-01,Some Supplier,produce,no amount column at all\n"

	req := multipartCostSheetRequest(t, http.MethodPost, "/api/ingest/cost-sheet/preview", "missing_amount.csv", missingAmount)
	rec := httptest.NewRecorder()

	HandlePreviewCostSheet(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "invalid_cost_sheet", body["error"])
	require.Contains(t, body["detail"], "amount")
}

// TestHandlePreviewCostSheet_RejectsAMalformedAmountWithTheRowNumber is spec
// Acceptance Scenario US2.2: the error names the specific row.
func TestHandlePreviewCostSheet_RejectsAMalformedAmountWithTheRowNumber(t *testing.T) {
	badAmount := "invoice_id,invoice_date,supplier,category,amount,notes\n" +
		"INV-OK-1,2026-08-01,Supplier A,produce,100.00,fine\n" +
		"INV-OK-2,2026-08-02,Supplier B,protein,not-a-number,bad amount here\n"

	req := multipartCostSheetRequest(t, http.MethodPost, "/api/ingest/cost-sheet/preview", "bad_amount.csv", badAmount)
	rec := httptest.NewRecorder()

	HandlePreviewCostSheet(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Contains(t, body["detail"], "row 3") // header=1, INV-OK-1=2, INV-OK-2=3
	require.Contains(t, body["detail"], "amount")
}

// TestHandlePreviewCostSheet_RejectsAnEmptyFile matches ParseCostSheet's own
// "is empty" refusal (spec Edge Cases).
func TestHandlePreviewCostSheet_RejectsAnEmptyFile(t *testing.T) {
	req := multipartCostSheetRequest(t, http.MethodPost, "/api/ingest/cost-sheet/preview", "empty.csv", "")
	rec := httptest.NewRecorder()

	HandlePreviewCostSheet(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Contains(t, body["detail"], "empty")
}

// TestHandlePreviewCostSheet_RejectsWrongMethod covers the plain method
// guard every handler in this package carries.
func TestHandlePreviewCostSheet_RejectsWrongMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/ingest/cost-sheet/preview", nil)
	rec := httptest.NewRecorder()

	HandlePreviewCostSheet(rec, req)

	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

// TestHandleCommitCostSheet_RejectsAMalformedUploadBeforeTouchingStoreOrDisk
// proves FR-007's re-validation happens BEFORE any write or pipeline run:
// passing a nil store/cache would panic if the handler ever reached the
// database or livedata step for a malformed file, so this test doubles as
// proof that validation is genuinely first.
func TestHandleCommitCostSheet_RejectsAMalformedUploadBeforeTouchingStoreOrDisk(t *testing.T) {
	missingSupplier := "invoice_id,invoice_date,category,amount,notes\n" +
		"INV-BAD-1,2026-08-01,produce,100.00,no supplier column\n"

	req := multipartCostSheetRequest(t, http.MethodPost, "/api/ingest/cost-sheet/commit", "missing_supplier.csv", missingSupplier)
	rec := httptest.NewRecorder()

	handler := HandleCommitCostSheet(nil, nil)
	handler(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "invalid_cost_sheet", body["error"])
	require.Contains(t, body["detail"], "supplier")
}

// TestHandleCommitCostSheet_RejectsAHeaderOnlyUploadBeforeTouchingStoreOrDisk
// is the commit-side companion to
// TestHandlePreviewCostSheet_RejectsAHeaderOnlyUpload: a header-only file
// passed to /commit must be refused by the same FR-007 re-validation, before
// os.WriteFile ever REPLACES the live cost-sheet file. Passing a nil
// store/cache would panic if the handler reached the database or livedata
// step, so this doubles as proof the refusal happens first — a 0-row commit
// must never get anywhere near overwriting the multi-year cost history.
func TestHandleCommitCostSheet_RejectsAHeaderOnlyUploadBeforeTouchingStoreOrDisk(t *testing.T) {
	headerOnly := "invoice_id,invoice_date,supplier,category,amount,notes\n"

	req := multipartCostSheetRequest(t, http.MethodPost, "/api/ingest/cost-sheet/commit", "header_only.csv", headerOnly)
	rec := httptest.NewRecorder()

	handler := HandleCommitCostSheet(nil, nil)
	handler(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "invalid_cost_sheet", body["error"])
	require.Contains(t, body["detail"], "no data rows found")
}

// TestHandleCostSheetTemplate_ServesAParsableCSV is spec FR-006/SC-004: the
// downloaded template is a genuinely valid input, not a display-only
// convenience file — round-tripping it through the real preview handler
// must succeed.
func TestHandleCostSheetTemplate_ServesAParsableCSV(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/ingest/cost-sheet/template", nil)
	rec := httptest.NewRecorder()

	HandleCostSheetTemplate(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "text/csv", rec.Header().Get("Content-Type"))
	require.Contains(t, rec.Header().Get("Content-Disposition"), "cost_sheet_template.csv")

	templateContent := rec.Body.String()
	previewReq := multipartCostSheetRequest(t, http.MethodPost, "/api/ingest/cost-sheet/preview", "cost_sheet_template.csv", templateContent)
	previewRec := httptest.NewRecorder()
	HandlePreviewCostSheet(previewRec, previewReq)

	require.Equal(t, http.StatusOK, previewRec.Code, "the downloaded template must preview successfully when uploaded back unmodified")
	var resp PreviewCostSheetResponse
	require.NoError(t, json.Unmarshal(previewRec.Body.Bytes(), &resp))
	require.GreaterOrEqual(t, resp.RowCount, 1)
}

func TestHandleCostSheetTemplate_RejectsWrongMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/ingest/cost-sheet/template", nil)
	rec := httptest.NewRecorder()

	HandleCostSheetTemplate(rec, req)

	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}
