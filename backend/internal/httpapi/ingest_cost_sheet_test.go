package httpapi

// Unit-level tests for POST /api/ingest/cost-sheet/preview and GET
// /api/ingest/cost-sheet/template — no database required (unlike
// promotions_create_test.go's live-Postgres tests): preview is pure
// in-memory parsing (spec FR-005), and template is a static file, so both
// exercise real behavior without touching storage.Querier at all.
//
// The commit endpoint's pre-write validation refusal is covered here too,
// which runs before any database or filesystem interaction and needs no
// store at all. TestHandleCommitCostSheet_SerializesConcurrentCommits is the
// one exception: it DOES exercise the real commit happy path against a live
// Postgres, because the concurrency behavior it proves (commitMu actually
// serializing two overlapping commits) cannot be verified any other way.
// That test cleans up its own daily_reconciliation rows in t.Cleanup — any
// future test doing the same must do likewise, since this is otherwise a
// DB-free file and a stray committed row here would silently corrupt
// reconciliation totals for anyone running the suite against a shared
// Postgres instance.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/livedata"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/platformconnector"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// queryFailingOnDataRange is a minimal storage.Querier fake: it answers
// GetDataDateRange with a real, non-sentinel failure (a dropped connection,
// a malformed query) and panics if anything else is called, since
// loadMarginSnapshot must short-circuit on this error before reaching any
// other query.
type queryFailingOnDataRange struct {
	storage.Querier
}

func (queryFailingOnDataRange) GetDataDateRange(context.Context) (storage.GetDataDateRangeRow, error) {
	return storage.GetDataDateRangeRow{}, errors.New("connection reset by peer")
}

// TestLoadMarginSnapshot_RealQueryFailureIsNotMistakenForNoData pins the
// fix for a live finding: a real storage.LoadDataDateRange failure (a
// transient DB hiccup, not "no rows yet") was previously collapsed into
// marginSnapshot{HasData: false}, nil — a confidently wrong "you have no
// prior history" statement about the owner's data, served with a 200, on
// the one endpoint that changes financial numbers. The genuinely-no-data
// case (ErrNoReconciliationDataYet, a server's very first commit) must
// still degrade quietly; only a REAL failure must now propagate as one.
func TestLoadMarginSnapshot_RealQueryFailureIsNotMistakenForNoData(t *testing.T) {
	_, err := loadMarginSnapshot(context.Background(), queryFailingOnDataRange{})
	require.Error(t, err, "a real query failure must propagate as an error, never silently become HasData:false")
	require.NotErrorIs(t, err, storage.ErrNoReconciliationDataYet)
}

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
// finish (and release the lock) does B get to run at all, so A's own
// pipeline run is proven to have seen only its own file, uncontested — and
// the file on disk ends the test holding BOTH commits' rows, merged rather
// than one wholesale-replacing the other (mergeCostSheetUpload, added
// 2026-08-31 to fix a real bug: a single realistic invoice upload used to
// zero out every OTHER previously-committed day's costs).
func TestHandleCommitCostSheet_SerializesConcurrentCommits(t *testing.T) {
	conn, q := httpapiConnectOrSkip(t)
	withReadyLiveDataDir(t)

	// 1999 dates, matching internal/mcptools/reconciliation_tools_test.go's
	// own sentinelDate convention: this test commits through the REAL
	// handler and the real pipeline against a live Postgres, so its dates
	// must fall outside the canonical dataset's real range (2024-08-01
	// onward). The original version of this test used 2026-01-05..07 — real
	// in-range dates — and a backend-regression pass (2026-08-30) found this
	// permanently drifted the canonical $1,078,340.64 total to
	// $1,081,910.28 when run against any shared Postgres, silently, because
	// nothing restored the pre-existing canonical row for those days. A
	// cleanup-DELETE alone would have been WORSE, not better: it deletes the
	// row outright rather than restoring the original canonical value,
	// leaving a hole in the dataset instead of a wrong-but-present one.
	// Genuinely out-of-range dates make the whole class of failure
	// impossible rather than relying on cleanup to catch it after the fact.
	const fileA = "invoice_id,invoice_date,supplier,category,amount,notes\n" +
		"INV-RACE-A,1999-01-05,Race Supplier A,produce,10.00,file A\n"
	const fileB = "invoice_id,invoice_date,supplier,category,amount,notes\n" +
		"INV-RACE-B1,1999-01-06,Race Supplier B,produce,20.00,file B row 1\n" +
		"INV-RACE-B2,1999-01-07,Race Supplier B,produce,30.00,file B row 2\n"

	// Defensive cleanup, matching deleteDay's own belt-and-suspenders
	// discipline elsewhere: nothing legitimate should ever exist on these
	// dates, but this keeps a repeated run against a persistent (non-ephemeral)
	// Postgres from accumulating stale race-test rows across runs.
	t.Cleanup(func() {
		_, err := conn.Exec(context.Background(),
			"DELETE FROM daily_reconciliation WHERE date IN ($1, $2, $3)",
			"1999-01-05", "1999-01-06", "1999-01-07")
		if err != nil {
			t.Logf("cleanup: failed to delete race-test reconciliation rows: %v", err)
		}
	})

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

	// A and B cover different calendar dates (1999-01-05 vs 1999-01-06/07),
	// so a correct merge keeps both — this is no longer "whichever commit
	// ran last wins" (see mergeCostSheetUpload's own doc comment for why a
	// wholesale replace was the actual live bug this project shipped and
	// fixed on 2026-08-31). The concurrency property this test exists to
	// prove is unchanged: B could not observe or overwrite A's row while A
	// was still mid-commit (checked above) — the two rows coexisting here is
	// the CORRECT outcome of that serialization, not evidence it failed.
	finalOnDisk, err := os.ReadFile(destPath)
	require.NoError(t, err)
	wantMerged := "invoice_id,invoice_date,supplier,category,amount,notes\n" +
		"INV-RACE-A,1999-01-05,Race Supplier A,produce,10.00,file A\n" +
		"INV-RACE-B1,1999-01-06,Race Supplier B,produce,20.00,file B row 1\n" +
		"INV-RACE-B2,1999-01-07,Race Supplier B,produce,30.00,file B row 2\n"
	require.Equal(t, wantMerged, string(finalOnDisk), "A's row and B's rows are on different dates, so a correct merge keeps all three, sorted by date")
}

// TestHandleCommitCostSheet_MergesByDateRatherThanReplacingEverything is the
// direct regression test for the bug itself, reproduced live on 2026-08-31:
// a single realistic cost-sheet upload — one invoice, one day — used to
// REPLACE the entire committed cost history rather than update just the
// day(s) it covers, taking a 759-day, $1,078,340.64 canonical dataset to
// $2,361,921.90 by zeroing every other day's input_costs. Proves three
// things a fix here must get right: (1) uploading day 1 alone persists it,
// (2) uploading day 2 afterward does not erase day 1, (3) re-uploading day 1
// with DIFFERENT content replaces only day 1's own rows, never touching day
// 2's.
func TestHandleCommitCostSheet_MergesByDateRatherThanReplacingEverything(t *testing.T) {
	conn, q := httpapiConnectOrSkip(t)
	withReadyLiveDataDir(t)
	t.Cleanup(func() {
		_, err := conn.Exec(context.Background(),
			"DELETE FROM daily_reconciliation WHERE date IN ($1, $2)",
			"1999-02-10", "1999-02-11")
		if err != nil {
			t.Logf("cleanup: failed to delete merge-test reconciliation rows: %v", err)
		}
	})

	handler := HandleCommitCostSheet(nil, q, nil)
	destPath := filepath.Join(livedata.Dir, liveCostSheetFilename)
	commit := func(csv string) CommitCostSheetResponse {
		t.Helper()
		req := multipartCostSheetRequest(t, http.MethodPost, "/api/ingest/cost-sheet/commit", "upload.csv", csv)
		rec := httptest.NewRecorder()
		handler(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "commit: %s", rec.Body.String())
		var resp CommitCostSheetResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		return resp
	}

	const day1Original = "invoice_id,invoice_date,supplier,category,amount,notes\n" +
		"INV-MERGE-1,1999-02-10,Supplier One,produce,50.00,day 1 original\n"
	resp1 := commit(day1Original)
	require.Equal(t, 1, resp1.RowsCommitted)
	onDiskAfterDay1, err := os.ReadFile(destPath)
	require.NoError(t, err)
	require.Equal(t, day1Original, string(onDiskAfterDay1), "the very first commit has nothing to merge against")

	const day2 = "invoice_id,invoice_date,supplier,category,amount,notes\n" +
		"INV-MERGE-2,1999-02-11,Supplier Two,protein,75.00,day 2\n"
	resp2 := commit(day2)
	// If day 2's commit had wholesale-replaced the file instead of merging,
	// re-reconciling the WHOLE dataset from a file now missing day 1's real
	// cost would move total margin by far more than day 2's own $75 — this
	// is the same class of check that caught the live bug (a $1.28M swing
	// from one missing invoice), just at test scale.
	beforeMargin, err := strconv.ParseFloat(*resp2.Before.Margin, 64)
	require.NoError(t, err)
	afterMargin, err := strconv.ParseFloat(*resp2.After.Margin, 64)
	require.NoError(t, err)
	require.InDelta(t, -75.0, afterMargin-beforeMargin, 0.001,
		"day 2's commit must change total margin by exactly day 2's own $75 cost, proving day 1's cost was not silently dropped")
	onDiskAfterDay2, err := os.ReadFile(destPath)
	require.NoError(t, err)
	require.Contains(t, string(onDiskAfterDay2), "INV-MERGE-1", "day 2's commit must not erase day 1's row")
	require.Contains(t, string(onDiskAfterDay2), "INV-MERGE-2")

	// Same invoice_id as day 1's original commit: a real correction to an
	// existing invoice keeps its own identity, it doesn't become a new one
	// (see mergeCostSheetUpload's doc comment — merging by InvoiceID is
	// exactly what makes this the row that gets replaced, not a second row
	// added alongside the original).
	const day1Revised = "invoice_id,invoice_date,supplier,category,amount,notes\n" +
		"INV-MERGE-1,1999-02-10,Supplier One,produce,999.99,day 1 revised\n"
	resp3 := commit(day1Revised)
	beforeMargin3, err := strconv.ParseFloat(*resp3.Before.Margin, 64)
	require.NoError(t, err)
	afterMargin3, err := strconv.ParseFloat(*resp3.After.Margin, 64)
	require.NoError(t, err)
	// Day 1's cost moved from $50.00 to $999.99 (a $949.99 increase), and
	// day 2 must be untouched — so the ONLY change in total margin is that
	// one delta, not day 2's cost vanishing on top of it.
	require.InDelta(t, -949.99, afterMargin3-beforeMargin3, 0.001,
		"re-uploading day 1 must change total margin by exactly day 1's own cost delta, proving day 2's cost survived untouched")
	finalOnDisk2, err := os.ReadFile(destPath)
	require.NoError(t, err)
	require.NotContains(t, string(finalOnDisk2), "day 1 original", "re-uploading INV-MERGE-1 must replace its OWN prior content, not add to it")
	require.Contains(t, string(finalOnDisk2), "INV-MERGE-1,1999-02-10,Supplier One,produce,999.99,day 1 revised", "day 1's revised content must be present, under the SAME invoice id")
	require.Contains(t, string(finalOnDisk2), "INV-MERGE-2", "day 2, untouched by this commit, must still be present")
}

// TestHandleCommitCostSheet_NewInvoiceForAnAlreadyInvoicedDayKeepsTheOthers
// is the direct regression test for a bug found in adversarial testing on
// 2026-08-31: merging by calendar date (this feature's first fix) still let
// the most ordinary real workflow — a late-arriving invoice for a day that
// already had other invoices on file — silently delete every OTHER invoice
// already committed for that same day, with a 200 and no discrepancy flag.
// Merging by InvoiceID instead means an upload only ever replaces the exact
// invoice(s) it names.
func TestHandleCommitCostSheet_NewInvoiceForAnAlreadyInvoicedDayKeepsTheOthers(t *testing.T) {
	conn, q := httpapiConnectOrSkip(t)
	withReadyLiveDataDir(t)
	t.Cleanup(func() {
		_, err := conn.Exec(context.Background(),
			"DELETE FROM daily_reconciliation WHERE date = $1", "1999-03-15")
		if err != nil {
			t.Logf("cleanup: failed to delete same-day-invoice test reconciliation row: %v", err)
		}
	})

	handler := HandleCommitCostSheet(nil, q, nil)
	destPath := filepath.Join(livedata.Dir, liveCostSheetFilename)
	commit := func(csv string) CommitCostSheetResponse {
		t.Helper()
		req := multipartCostSheetRequest(t, http.MethodPost, "/api/ingest/cost-sheet/commit", "upload.csv", csv)
		rec := httptest.NewRecorder()
		handler(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "commit: %s", rec.Body.String())
		var resp CommitCostSheetResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		return resp
	}

	const fiveInvoicesOneDay = "invoice_id,invoice_date,supplier,category,amount,notes\n" +
		"INV-SAMEDAY-1,1999-03-15,Produce Co,produce,100.00,\n" +
		"INV-SAMEDAY-2,1999-03-15,Meat Co,protein,200.00,\n" +
		"INV-SAMEDAY-3,1999-03-15,Beverage Co,beverage,50.00,\n" +
		"INV-SAMEDAY-4,1999-03-15,Packaging Co,packaging,25.00,\n" +
		"INV-SAMEDAY-5,1999-03-15,Cleaning Co,supplies,10.00,\n"
	resp1 := commit(fiveInvoicesOneDay)
	require.Equal(t, 5, resp1.RowsCommitted)

	// A late-arriving sixth invoice for the SAME day — the exact real
	// workflow that deleted $9,669.94 of real invoices in the live repro.
	const lateInvoice = "invoice_id,invoice_date,supplier,category,amount,notes\n" +
		"INV-SAMEDAY-6,1999-03-15,Late Supplier,produce,15.00,late-arriving invoice\n"
	resp2 := commit(lateInvoice)
	beforeMargin, err := strconv.ParseFloat(*resp2.Before.Margin, 64)
	require.NoError(t, err)
	afterMargin, err := strconv.ParseFloat(*resp2.After.Margin, 64)
	require.NoError(t, err)
	require.InDelta(t, -15.0, afterMargin-beforeMargin, 0.001,
		"the late invoice must change margin by exactly its own $15 cost -- the other five invoices already on file for this day must survive untouched")

	onDisk, err := os.ReadFile(destPath)
	require.NoError(t, err)
	for _, id := range []string{"INV-SAMEDAY-1", "INV-SAMEDAY-2", "INV-SAMEDAY-3", "INV-SAMEDAY-4", "INV-SAMEDAY-5", "INV-SAMEDAY-6"} {
		require.Contains(t, string(onDisk), id, "all six invoices for this day must be present -- none deleted by the late arrival")
	}
}

// --- The upload-triggers-sync composition (2026-08-30) ----------------------

// TestCostSheetDateRange_SpansEveryRow proves the range a commit hands the
// connector is derived from the FILE, across all of its rows, not from the
// first row and not from anything the client sent. A cost sheet is one
// invoice per row on a supplier's own irregular cadence, so a real upload
// can be one day or a month of them.
// TestWriteCostSheetAtomically_ReplacesExistingContentCleanly proves the
// happy path: an existing file's content is fully replaced, and no stray
// temp file is left behind in the directory afterward.
func TestWriteCostSheetAtomically_ReplacesExistingContentCleanly(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "supplier_cost_sheet.csv")
	require.NoError(t, os.WriteFile(dest, []byte("old content"), 0o644))

	require.NoError(t, writeCostSheetAtomically(dest, []byte("new content")))

	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Equal(t, "new content", string(got))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "no temp file should survive a successful write: %v", entries)
}

// TestWriteCostSheetAtomically_CreatesANewFile proves the first-commit
// case: destPath doesn't exist yet, and the atomic write must still
// succeed (create-temp + rename works into a path with nothing there yet).
func TestWriteCostSheetAtomically_CreatesANewFile(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "supplier_cost_sheet.csv")

	require.NoError(t, writeCostSheetAtomically(dest, []byte("first commit")))

	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Equal(t, "first commit", string(got))
}

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
