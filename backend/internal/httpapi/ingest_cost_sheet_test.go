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
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/livedata"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/platformconnector"
)

// multipartCostSheetRequest builds a multipart/form-data POST request with
// the given CSV content in a "file" field, matching how a real browser
// upload (or the frontend's FormData-based postMultipart) shapes the body.
func multipartCostSheetRequest(t *testing.T, method, path, filename, csvContent string) *http.Request {
	t.Helper()
	return multipartCostSheetRequestWithFields(t, method, path, filename, csvContent, nil)
}

// multipartCostSheetRequestWithFields is the same, plus the extra plain
// form fields a real browser upload can carry alongside the file — today
// just "sync_connectors" (see wantsConnectorSync).
func multipartCostSheetRequestWithFields(t *testing.T, method, path, filename, csvContent string, fields map[string]string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = io.WriteString(part, csvContent)
	require.NoError(t, err)
	for name, value := range fields {
		require.NoError(t, w.WriteField(name, value))
	}
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

	handler := HandleCommitCostSheet(nil, nil, nil)
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

	handler := HandleCommitCostSheet(nil, nil, nil)
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

// withReadyLiveDataDir makes livedata.EnsureReady() pass for the duration of
// the calling test, backing up whatever real dataset already sits at
// livedata.Dir (this machine's own `cmd/gendata` output, if any) and
// restoring it verbatim afterward via t.Cleanup — mirroring
// livedata_test.go's own withMissingDir backup/restore pattern, just in the
// opposite direction. A lone seed.csv is enough to satisfy EnsureReady (it
// only checks for >=1 CSV) without findSourceFiles ever mistaking it for a
// delivery/POS/cost-sheet source (its name contains none of those keywords).
func withReadyLiveDataDir(t *testing.T) {
	t.Helper()
	backup := livedata.Dir + ".commit-race-test-backup"
	if _, err := os.Stat(livedata.Dir); err == nil {
		require.NoError(t, os.RemoveAll(backup))
		require.NoError(t, os.Rename(livedata.Dir, backup))
		t.Cleanup(func() {
			require.NoError(t, os.RemoveAll(livedata.Dir))
			require.NoError(t, os.Rename(backup, livedata.Dir))
		})
	} else {
		t.Cleanup(func() {
			require.NoError(t, os.RemoveAll(livedata.Dir))
		})
	}
	require.NoError(t, os.MkdirAll(livedata.Dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(livedata.Dir, "seed.csv"), []byte("placeholder\n"), 0o644))
}

// TestHandleCommitCostSheet_SerializesConcurrentCommits is a regression test
// for a real, live-reproducible race: two commits close together used to be
// able to interleave their write-then-reconcile steps, so one request could
// end up running the FULL reconciliation pipeline against the OTHER
// request's file while reporting its own row count and margin snapshot —
// a confidently wrong report of what actually got persisted (CLAUDE.md: "a
// confidently wrong margin figure is worse than a refusal"). commitMu now
// serializes the whole write -> pipeline-run -> re-read section.
//
// This test proves mutual exclusion directly rather than hoping to catch a
// timing-dependent corruption after the fact: request A is paused (via the
// afterCommitWriteForTests seam) immediately after writing its own file,
// still holding commitMu. While A is paused, request B is started
// concurrently and must be unable to make ANY progress — specifically, it
// must not have overwritten the live file yet. Only once A is allowed to
// finish (and release the lock) does B get to run at all, so the file on
// disk ends the test as B's content, uncontested, and A's own pipeline run
// is proven to have seen only its own file.
func TestHandleCommitCostSheet_SerializesConcurrentCommits(t *testing.T) {
	_, q := httpapiConnectOrSkip(t)
	withReadyLiveDataDir(t)

	const fileA = "invoice_id,invoice_date,supplier,category,amount,notes\n" +
		"INV-RACE-A,2026-01-05,Race Supplier A,produce,10.00,file A\n"
	const fileB = "invoice_id,invoice_date,supplier,category,amount,notes\n" +
		"INV-RACE-B1,2026-01-06,Race Supplier B,produce,20.00,file B row 1\n" +
		"INV-RACE-B2,2026-01-07,Race Supplier B,produce,30.00,file B row 2\n"

	reachedHook := make(chan []byte, 1)
	proceed := make(chan struct{})
	var callNum int32

	afterCommitWriteForTests = func() {
		if atomic.AddInt32(&callNum, 1) == 1 {
			// This is request A: capture what's actually on disk right now
			// (proving it's still A's own content, not a peer's) and pause
			// here, still holding commitMu, until the test says to continue.
			onDisk, err := os.ReadFile(filepath.Join(livedata.Dir, liveCostSheetFilename))
			require.NoError(t, err)
			reachedHook <- onDisk
			<-proceed
		}
	}
	t.Cleanup(func() { afterCommitWriteForTests = nil })

	handler := HandleCommitCostSheet(nil, q, nil)
	destPath := filepath.Join(livedata.Dir, liveCostSheetFilename)

	doneA := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := multipartCostSheetRequest(t, http.MethodPost, "/api/ingest/cost-sheet/commit", "a.csv", fileA)
		rec := httptest.NewRecorder()
		handler(rec, req)
		doneA <- rec
	}()

	var onDiskDuringA []byte
	select {
	case onDiskDuringA = <-reachedHook:
	case <-time.After(5 * time.Second):
		t.Fatal("request A never reached the post-write hook — commitMu or the seam is wired wrong")
	}
	require.Equal(t, fileA, string(onDiskDuringA), "the file on disk while A is paused must still be A's own upload")

	doneB := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := multipartCostSheetRequest(t, http.MethodPost, "/api/ingest/cost-sheet/commit", "b.csv", fileB)
		rec := httptest.NewRecorder()
		handler(rec, req)
		doneB <- rec
	}()

	// B must be blocked on commitMu.Lock() — give it a real window to prove
	// it, then confirm neither B finished nor B's write landed.
	select {
	case <-doneB:
		t.Fatal("request B completed while A was still mid-commit — commitMu did not serialize them")
	case <-time.After(200 * time.Millisecond):
	}
	stillOnDisk, err := os.ReadFile(destPath)
	require.NoError(t, err)
	require.Equal(t, fileA, string(stillOnDisk), "B must not have written over A's file while A was still mid-commit")

	close(proceed) // let A finish; only then can B acquire the lock

	var recA, recB *httptest.ResponseRecorder
	select {
	case recA = <-doneA:
	case <-time.After(10 * time.Second):
		t.Fatal("request A never finished after being unpaused")
	}
	select {
	case recB = <-doneB:
	case <-time.After(10 * time.Second):
		t.Fatal("request B never finished after A released commitMu")
	}

	require.Equal(t, http.StatusOK, recA.Code, "request A: %s", recA.Body.String())
	require.Equal(t, http.StatusOK, recB.Code, "request B: %s", recB.Body.String())

	var respA, respB CommitCostSheetResponse
	require.NoError(t, json.Unmarshal(recA.Body.Bytes(), &respA))
	require.NoError(t, json.Unmarshal(recB.Body.Bytes(), &respB))
	require.Equal(t, 1, respA.RowsCommitted)
	require.Equal(t, 2, respB.RowsCommitted)

	finalOnDisk, err := os.ReadFile(destPath)
	require.NoError(t, err)
	require.Equal(t, fileB, string(finalOnDisk), "B ran strictly after A released commitMu, so B's file must be what's left on disk")
}

// --- The upload-triggers-sync composition (2026-08-30) ----------------------

// TestCostSheetDateRange_SpansEveryRow proves the range a commit hands the
// connector is derived from the FILE, across all of its rows, not from the
// first row and not from anything the client sent. A cost sheet is one
// invoice per row on a supplier's own irregular cadence, so a real upload
// can be one day or a month of them.
func TestCostSheetDateRange_SpansEveryRow(t *testing.T) {
	// Deliberately out of chronological order, and with the widest dates in
	// the middle: a min/max scan is correct, "first and last row" is not.
	csv := "invoice_id,invoice_date,supplier,amount\n" +
		"INV-1,2026-08-14,S,10.00\n" +
		"INV-2,2026-08-03,S,10.00\n" +
		"INV-3,2026-08-27,S,10.00\n" +
		"INV-4,2026-08-09,S,10.00\n"

	records, err := ingest.ParseCostSheet(strings.NewReader(csv), "range.csv")
	require.NoError(t, err)

	from, to := costSheetDateRange(records)
	require.Equal(t, "2026-08-03", from.Format(dateLayout))
	require.Equal(t, "2026-08-27", to.Format(dateLayout))
}

// TestCostSheetDateRange_HandlesASingleDaySheet: the degenerate case is a
// real one (an owner uploading today's three invoices), and it must produce
// a one-day range, not an empty or inverted one — the connector refuses an
// inverted range outright.
func TestCostSheetDateRange_HandlesASingleDaySheet(t *testing.T) {
	csv := "invoice_id,invoice_date,supplier,amount\ninv,2026-08-20,S,10.00\n"
	records, err := ingest.ParseCostSheet(strings.NewReader(csv), "one_day.csv")
	require.NoError(t, err)

	from, to := costSheetDateRange(records)
	require.Equal(t, from, to)
	require.Equal(t, "2026-08-20", from.Format(dateLayout))
}

// TestWantsConnectorSync_IsOffUnlessAsked is the API half of the
// automatic-vs-opt-in decision recorded on wantsConnectorSync: an absent
// flag must mean "do not pull in simulated revenue", so every client that
// existed before this change — a curl, a script, the evaluation harness —
// keeps behaving exactly as it did. Silently injecting simulated numbers
// into a caller that never mentioned connectors is the failure this guards.
func TestWantsConnectorSync_IsOffUnlessAsked(t *testing.T) {
	off := []map[string]string{
		nil,
		{"sync_connectors": ""},
		{"sync_connectors": "false"},
		{"sync_connectors": "0"},
		{"sync_connectors": "maybe"},
	}
	for _, fields := range off {
		req := multipartCostSheetRequestWithFields(t, http.MethodPost, "/api/ingest/cost-sheet/commit", "c.csv", validCostSheetCSV, fields)
		require.NoError(t, req.ParseMultipartForm(maxCostSheetUploadBytes))
		require.False(t, wantsConnectorSync(req), "fields %v must not opt in", fields)
	}

	for _, value := range []string{"true", "TRUE", "1", "yes", "on", " true "} {
		req := multipartCostSheetRequestWithFields(t, http.MethodPost, "/api/ingest/cost-sheet/commit", "c.csv", validCostSheetCSV,
			map[string]string{"sync_connectors": value})
		require.NoError(t, req.ParseMultipartForm(maxCostSheetUploadBytes))
		require.True(t, wantsConnectorSync(req), "value %q should opt in", value)
	}
}

// TestHandleCommitCostSheet_RefusesAnOverWideConnectorRangeBeforeTouchingDisk
// is the atomicity guarantee for the combined action.
//
// The owner asked for one thing ("commit these costs AND pull in the
// revenue for the days they cover"). If the connector cannot serve the
// range — here, a cost sheet spanning more than the 31-day sync cap — the
// honest answer is to refuse the whole request and say why, not to commit
// half of it and mention the other half failed in a field nobody reads.
// Passing a nil store and a nil cache proves the refusal happens before any
// database or filesystem work: reaching either would panic.
func TestHandleCommitCostSheet_RefusesAnOverWideConnectorRangeBeforeTouchingDisk(t *testing.T) {
	wide := "invoice_id,invoice_date,supplier,amount\n" +
		"INV-1,2026-06-01,S,100.00\n" +
		"INV-2,2026-08-01,S,100.00\n"

	req := multipartCostSheetRequestWithFields(t, http.MethodPost, "/api/ingest/cost-sheet/commit", "wide.csv", wide,
		map[string]string{"sync_connectors": "true"})
	rec := httptest.NewRecorder()

	HandleCommitCostSheet(platformconnector.NewSimulatedProxy(), nil, nil)(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "connector_fetch_failed", body["error"])
	require.Contains(t, body["detail"], "31-day limit")
	require.Contains(t, body["detail"], "the cost sheet was not committed",
		"a refusal must say what did NOT happen, or the owner has to guess whether their costs landed")
	require.NotContains(t, body["detail"], "platformconnector:",
		"the internal package prefix must not reach the owner")
}

// TestHandleCommitCostSheet_RefusesTheSyncWhenNoConnectorsAreWired: a
// server built without the proxy must refuse an explicit sync request
// rather than quietly committing the cost sheet alone and reporting
// connector_sync: null, which a client would read as "I did not ask".
func TestHandleCommitCostSheet_RefusesTheSyncWhenNoConnectorsAreWired(t *testing.T) {
	req := multipartCostSheetRequestWithFields(t, http.MethodPost, "/api/ingest/cost-sheet/commit", "c.csv", validCostSheetCSV,
		map[string]string{"sync_connectors": "true"})
	rec := httptest.NewRecorder()

	HandleCommitCostSheet(nil, nil, nil)(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "connectors_unavailable", body["error"])
}
