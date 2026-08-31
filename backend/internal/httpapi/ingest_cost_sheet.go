package httpapi

// specs/007-cost-sheet-upload: letting the owner upload their supplier cost
// sheet through the web UI, rather than requiring a developer to run
// `go run ./cmd/server -ingest <dir>` on their behalf — the one input
// source this product cannot obtain any other way (delivery-platform/POS
// exports arrive as feeds; the cost sheet is something the owner personally
// produces on an irregular, owner-driven cadence).
//
// Zero model involvement anywhere in this file: parsing/validation reuses
// internal/ingest.ParseCostSheet UNCHANGED (Constitution Principle II —
// no second, UI-specific validation implementation that could disagree with
// the CLI path), and the commit path reuses internal/pipeline.RunIngestionPipeline
// UNCHANGED (the exact same ingest -> reconcile -> persist flow -ingest
// already runs). This file adds transport (multipart upload, JSON response
// shapes) and exactly one new piece of logic: the live-data directory
// bookkeeping in internal/livedata.
//
// Three endpoints:
//   - POST /api/ingest/cost-sheet/preview — parse + validate only, never
//     persists anything (spec FR-005).
//   - POST /api/ingest/cost-sheet/commit — re-validates the SAME way from
//     scratch (spec FR-007 — a prior preview call is never trusted), then
//     writes into internal/livedata.Dir and re-runs the real pipeline.
//   - GET /api/ingest/cost-sheet/template — a static, downloadable example
//     CSV (spec FR-006).
//
// 2026-08-30: the commit endpoint can now, on the request's explicit
// opt-in, also pull the SIMULATED platform revenue for the calendar range
// the uploaded invoices cover and commit both through one pipeline run, so
// that uploading a day's costs produces a real combined reconciliation
// instead of a cost-only one the owner then has to go and complete from a
// different tab. Two things about that are deliberate and are argued for at
// their own call sites: it is an opt-in rather than automatic
// (wantsConnectorSync), and the composition lives in this handler rather
// than inside internal/pipeline (HandleCommitCostSheet).

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/answercache"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/livedata"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/money"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/pipeline"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/platformconnector"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// maxCostSheetUploadBytes bounds the multipart body this feature will read.
// A hand-kept supplier cost sheet is a handful of KB (the dataset's own
// hand-authored opening invoices are ~1KB for 14 invoices); 5MB is generous
// headroom for a much larger real restaurant's invoice history while still
// being a real, enforced bound — Constitution's "timeouts/explicit caps"
// hard-limit posture applied to upload size, the one dimension of this
// feature a malicious or broken client could otherwise abuse unboundedly.
const maxCostSheetUploadBytes = 5 << 20

// liveCostSheetFilename is the FIXED name a committed upload is written
// under inside livedata.Dir — matching the dataset's own cost-sheet name so
// internal/pipeline.RunIngestionPipeline's filename-keyword matching
// (findSourceFiles: "cost"/"supplier"/"invoice") finds it exactly the way
// it finds the generated data/live copy today. This is deliberately
// NEVER the multipart upload's own original filename (spec FR-011): the
// destination path is built from zero request-derived strings, so path
// traversal via a crafted filename is not a runtime check to get right — it
// is a code shape that cannot occur.
const liveCostSheetFilename = "supplier_cost_sheet.csv"

// afterCommitWriteForTests is a test-only seam, always nil in production —
// see its call site in HandleCommitCostSheet for what it exists to prove.
var afterCommitWriteForTests func()

// ingestMu serializes every request path that writes into livedata.Dir and
// then re-runs the reconciliation pipeline against it.
//
// It was originally a mutex scoped to HandleCommitCostSheet's own closure,
// when the cost-sheet commit was the only such path and could therefore
// only ever race with itself. specs/010-platform-connector-proxy added a
// second one: POST /api/connectors/sync writes no file but does re-run the
// same pipeline over the same directory, and its response reports a
// before/after margin snapshot read around that run. A cost-sheet commit
// interleaving with a connector sync would produce exactly the failure the
// closure-scoped mutex was introduced to close — each request truthfully
// reporting its own inputs while the persisted numbers reflect the other's
// — one endpoint wider. A package-level lock is the smallest change that
// makes the guarantee hold across both, and it stays correct if a third
// write path is ever added, which a per-handler lock would not.
var ingestMu sync.Mutex

// CostSheetRowView is one parsed invoice row, rendered for the preview
// response. Amount is a decimal string (internal/money.FormatCents),
// matching every other money field this API returns — never a float.
type CostSheetRowView struct {
	InvoiceID    string           `json:"invoice_id"`
	InvoiceDate  string           `json:"invoice_date"`
	Supplier     string           `json:"supplier"`
	Category     string           `json:"category"`
	Amount       string           `json:"amount"`
	Notes        string           `json:"notes"`
	SourceRowRef SourceRowRefView `json:"source_row_ref"`
}

// SourceRowRefView renders ingest.SourceRowRef for the API — provenance
// (Constitution Principle IV) survives all the way to what the owner sees in
// the preview table, not just to what's persisted.
type SourceRowRefView struct {
	File string `json:"file"`
	Row  int    `json:"row"`
}

// PreviewCostSheetResponse is POST /api/ingest/cost-sheet/preview's success
// body.
type PreviewCostSheetResponse struct {
	RowCount    int                `json:"row_count"`
	TotalAmount string             `json:"total_amount"`
	Rows        []CostSheetRowView `json:"rows"`
}

// MarginSnapshotView is one side (before or after) of a commit's effect on
// the product's persisted margin totals.
type MarginSnapshotView struct {
	Days int `json:"days"`
	// Margin is a decimal string, or null when HasData was false — a real
	// absence (no reconciliation persisted yet), never a fabricated "0.00"
	// (the same "real zero vs. real absence" discipline
	// compare_platform_economics already applies to effective_rate).
	Margin *string `json:"margin"`
}

// CommitCostSheetResponse is POST /api/ingest/cost-sheet/commit's success
// body (spec FR-009).
type CommitCostSheetResponse struct {
	RowsCommitted int `json:"rows_committed"`

	// CoversFrom and CoversTo are the inclusive calendar range the uploaded
	// invoices span — the minimum and maximum invoice_date across every
	// parsed row. Reported unconditionally, not only when a sync ran,
	// because it is the range this upload just replaced the cost sheet for,
	// and the owner has no other way to see what the file they picked
	// actually covered.
	CoversFrom string `json:"covers_from"`
	CoversTo   string `json:"covers_to"`

	// ConnectorSync is present only when the request asked for the matching
	// simulated platform revenue to be pulled in with the upload
	// (sync_connectors=true). null means the box was not ticked and no
	// simulated revenue was fetched or persisted — a real absence, not an
	// empty sync.
	ConnectorSync *ConnectorSyncSummaryView `json:"connector_sync"`

	Before MarginSnapshotView `json:"before"`
	After  MarginSnapshotView `json:"after"`
}

// costSheetDateRange is the inclusive calendar range a parsed cost sheet
// covers: the minimum and maximum invoice_date across its rows.
//
// A cost sheet is not one date. ingest.ParseCostSheet's own contract is one
// invoice per row, each with its own invoice_date on a supplier's own
// irregular cadence (cmd/gendata/opening/README.md: "produce ~every 3 days,
// protein weekly"), so a real upload can be a single day's invoices or a
// month of them. Deriving the range from the rows rather than asking the
// client for it means the sync covers exactly what the file covers, and
// cannot be pointed somewhere else by a caller.
//
// records must be non-empty — ParseCostSheet already refuses a file with no
// data rows, and every caller here runs after that refusal.
func costSheetDateRange(records []ingest.CostInvoiceRecord) (from, to time.Time) {
	from, to = records[0].InvoiceDate, records[0].InvoiceDate
	for _, rec := range records[1:] {
		if rec.InvoiceDate.Before(from) {
			from = rec.InvoiceDate
		}
		if rec.InvoiceDate.After(to) {
			to = rec.InvoiceDate
		}
	}
	return from, to
}

// mergeCostSheetUpload combines a newly-uploaded cost sheet with whatever is
// already committed on disk, so that uploading an invoice updates ONLY the
// invoice(s) the new file actually names and leaves every other
// previously-committed invoice untouched.
//
// Why this exists: before it did, HandleCommitCostSheet wrote the uploaded
// bytes to destPath VERBATIM, which is a wholesale replace — the file the
// pipeline re-reads for every persisted day, not just the ones the owner
// just invoiced. A single real invoice, uploaded exactly as intended,
// silently zeroed input_costs for every other day ever committed: reproduced
// live on 2026-08-31, one $150 test invoice for one date took a 759-day,
// $1,078,340.64 canonical dataset to $2,361,921.90. The commit button's own
// copy ("Replace cost sheet") is honest about what the OLD code did — it was
// never meant to mean "replace the entire multi-year history."
//
// The merge key is InvoiceID, not the calendar date it was first fixed to.
// A date-keyed merge fixed the worst of the incident above (the blast
// radius shrank from "the whole file" to "the whole day") but kept the same
// destructive shape one level down: adversarial testing found that
// uploading ONE new invoice for a day that already had five committed
// invoices deleted the other four, silently, with a 200 and no discrepancy
// flag — the most ordinary real workflow (a late invoice for an
// already-invoiced day) destroyed $9,669.94 of real data in one live repro.
// Keying on InvoiceID instead means an upload only ever touches the exact
// invoices it names: a new ID is added, an ID that already exists is
// replaced (which is also what makes "I fixed a typo'd date on an invoice"
// work correctly — the old row, wherever its date was, is removed and the
// corrected one takes its place, instead of both surviving as a silent
// double-count).
//
// existingPath may not exist yet (a server's very first commit) — that is
// not an error, it just means there is nothing to merge against.
func mergeCostSheetUpload(existingPath string, newRecords []ingest.CostInvoiceRecord, newContent []byte) ([]byte, error) {
	// InvoiceID must be a real, unique identity before it can be trusted as
	// a merge key — a blank or duplicated one would make every row sharing
	// it collide. Refuse rather than guess which one is authoritative.
	seenInUpload := make(map[string]int, len(newRecords))
	for i, rec := range newRecords {
		if strings.TrimSpace(rec.InvoiceID) == "" {
			return nil, fmt.Errorf("row %d: invoice_id is blank — every invoice needs a stable id so a later correction can find and replace it", rec.Ref.Row)
		}
		if prior, dup := seenInUpload[rec.InvoiceID]; dup {
			return nil, fmt.Errorf("invoice_id %q appears twice in this upload (rows %d and %d) — refusing rather than guessing which one is correct", rec.InvoiceID, prior, rec.Ref.Row)
		}
		seenInUpload[rec.InvoiceID] = i + 1
	}

	existingBytes, err := os.ReadFile(existingPath)
	if errors.Is(err, os.ErrNotExist) {
		return newContent, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading existing cost sheet to merge against: %w", err)
	}

	existingRecords, err := ingest.ParseCostSheet(bytes.NewReader(existingBytes), liveCostSheetFilename)
	if err != nil {
		// The file on disk is supposed to be exactly what a prior commit
		// wrote — if it no longer parses, merging against it would risk
		// silently dropping rows a human can't see. Refuse rather than guess.
		return nil, fmt.Errorf("existing cost sheet on file no longer parses, cannot safely merge: %w", err)
	}

	replacedInvoiceIDs := make(map[string]struct{}, len(newRecords))
	for _, rec := range newRecords {
		replacedInvoiceIDs[rec.InvoiceID] = struct{}{}
	}

	merged := make([]ingest.CostInvoiceRecord, 0, len(existingRecords)+len(newRecords))
	for _, rec := range existingRecords {
		if _, replaced := replacedInvoiceIDs[rec.InvoiceID]; replaced {
			continue
		}
		merged = append(merged, rec)
	}
	merged = append(merged, newRecords...)
	sort.Slice(merged, func(i, j int) bool {
		if !merged[i].InvoiceDate.Equal(merged[j].InvoiceDate) {
			return merged[i].InvoiceDate.Before(merged[j].InvoiceDate)
		}
		return merged[i].InvoiceID < merged[j].InvoiceID
	})

	return encodeCostSheetCSV(merged)
}

// encodeCostSheetCSV renders records back into the same column shape
// ingest.ParseCostSheet reads (invoice_id,invoice_date,supplier,category,
// amount,notes), using encoding/csv so a notes field containing a comma or
// quote is escaped correctly rather than hand-formatted.
func encodeCostSheetCSV(records []ingest.CostInvoiceRecord) ([]byte, error) {
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	if err := cw.Write([]string{"invoice_id", "invoice_date", "supplier", "category", "amount", "notes"}); err != nil {
		return nil, err
	}
	for _, rec := range records {
		row := []string{
			rec.InvoiceID,
			rec.InvoiceDate.Format("2006-01-02"),
			rec.Supplier,
			rec.Category,
			money.FormatCents(rec.AmountCents),
			rec.Notes,
		}
		if err := cw.Write(row); err != nil {
			return nil, err
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeCostSheetAtomically replaces destPath with content via write-temp,
// fsync, rename — never a direct, in-place os.WriteFile. destPath is the
// sole, git-ignored, unversioned source of truth every commit merges
// against (mergeCostSheetUpload); a process interrupted mid-write (a crash,
// an OOM kill, a container restart) mid-way through a direct WriteFile
// would leave a truncated file on disk, and the NEXT commit's merge would
// then refuse against admittedly-corrupt content it can no longer tell
// apart from a real one — refusing is the right call once that's happened,
// but the atomic swap here means it never has to. A rename within the same
// directory is atomic on every OS this runs on; there is no window where a
// reader (or the next commit) can observe a partially-written file.
func writeCostSheetAtomically(destPath string, content []byte) error {
	dir := filepath.Dir(destPath)
	tmp, err := os.CreateTemp(dir, ".supplier_cost_sheet-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	// If a later step fails, the temp file must not linger — but a
	// successful Rename below moves it into place, so a no-op Remove after
	// that (file no longer at tmpPath) is expected, not an error worth
	// surfacing.
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("syncing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return fmt.Errorf("setting permissions on temp file: %w", err)
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("renaming temp file into place: %w", err)
	}
	return nil
}

// wantsConnectorSync reads the commit request's opt-in flag.
//
// # Why this is an opt-in and not automatic
//
// The product owner's ask was that uploading a cost sheet should not
// require a second manual trip to the Connected Platforms tab. It should
// not — and after this change it does not. But "does not require a second
// trip" and "happens without being asked" are different things, and the
// difference matters more here than it usually would, because the revenue
// being pulled in is SIMULATED.
//
// internal/platformconnector's package doc states that fact five separate
// times on the way to a number, deliberately and redundantly, on the
// reasoning that a disclosure which lives in exactly one place is a
// disclosure that can be cropped out of a screenshot. A cost-sheet upload
// that silently reached out and injected simulated iFood, Just Eat
// Takeaway and POS revenue into the owner's margin would defeat all five
// at once: the owner never visited the tab that carries the warning, never
// saw the notice, never pressed a button whose own label says "simulated".
// The numbers would simply be different afterward, and nothing in the
// interaction would have said why. That is the product inventing data
// behind the owner's back, which is the specific failure this codebase
// spends the most words guarding against.
//
// So: opt-in at the API, default-on in the UI. The two are not in tension,
// they are the same decision applied at two layers.
//
//   - At the API (this function), the flag is absent-means-false. A curl,
//     a script, or any future integration gets exactly the behaviour it had
//     before this change unless it explicitly asks otherwise. An API has no
//     banner to read.
//
//   - In the browser (CostSheetTab.tsx), the checkbox is pre-ticked, sits
//     in the preview panel directly above the commit button, names the
//     exact date range it will pull, and says "simulated" in its own label.
//     Pre-ticked because it is what the owner asked for and re-ticking it
//     on every upload would be its own kind of friction; visible and
//     adjacent to the confirm button because consent has to be current, and
//     because the owner must be able to say no without leaving the page.
//
// The result reaches them either way: the commit response reports the sync
// it ran, so even a client that ignored the flag can see what landed.
func wantsConnectorSync(r *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(r.FormValue("sync_connectors"))) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

// HandlePreviewCostSheet implements POST /api/ingest/cost-sheet/preview.
// Pure in-memory parsing — no file write, no livedata involvement, no
// pipeline run (spec FR-005: preview is read-only).
func HandlePreviewCostSheet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
		return
	}

	file, header, err := readUploadedCostSheet(w, r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_upload", err.Error())
		return
	}
	defer file.Close()

	records, err := ingest.ParseCostSheet(file, header.Filename)
	if err != nil {
		// The exact, specific ParseCostSheet error (row number, field name,
		// missing column) — never a generic "upload failed" (spec FR-003).
		writeJSONError(w, http.StatusUnprocessableEntity, "invalid_cost_sheet", err.Error())
		return
	}

	rows, total := renderCostSheetRows(records)
	writeJSON(w, http.StatusOK, PreviewCostSheetResponse{
		RowCount:    len(rows),
		TotalAmount: money.FormatCents(total),
		Rows:        rows,
	})
}

// HandleCommitCostSheet implements POST /api/ingest/cost-sheet/commit.
// store must be the concrete *storage.Queries (not just storage.Querier):
// internal/pipeline.RunIngestionPipeline's signature requires it, the same
// requirement the -ingest CLI flag already has in cmd/server/main.go. cache
// is cleared before the pipeline runs, mirroring main.go's own "new source
// data can change any cached answer, clear at the START of ingestion"
// rationale — this is the one place besides the CLI flags that changes
// persisted reconciliation data, so it must uphold the same invalidation
// discipline.
//
// ingestMu (below) serializes the write-then-reconcile critical section
// across concurrent requests. Found live: two tabs (or a double-submit)
// committing close together could otherwise interleave — request A writes
// its validated bytes to the fixed livedata path, request B overwrites that
// same path with a *different* file before A's pipeline run reads it back,
// and A's response then reports A's own row count and before/after margin
// snapshot while the database was actually just reconciled against B's
// content. That is a confidently wrong report of what got persisted — worse
// than a refusal (CLAUDE.md's own stated bar) — and it was reachable with no
// error, no conflict signal, nothing. A single in-process mutex closes it:
// this is a single-process server (cmd/server/main.go), so no cross-process
// locking is needed for a prototype of this shape.
// Since 2026-08-30 this handler also composes the connector sync: when the
// request sets sync_connectors (see wantsConnectorSync for the
// automatic-vs-opt-in reasoning), it fetches the simulated platform revenue
// for the same calendar range the uploaded invoices cover and commits both
// through ONE pipeline run, so the reconciliation the owner is shown
// afterward is a real combined one rather than a cost-only figure they then
// have to go and correct from another tab.
//
// The composition lives here, in the handler layer, and not in
// internal/pipeline. internal/pipeline already accepts a ConnectorOverlay
// and internal/platformconnector already produces one; entangling the two
// packages so a cost-sheet ingest could reach into a connector fetch would
// make the deterministic core depend on the integration layer that feeds
// it, which is backwards, and would put "did the user tick a box" inside a
// package whose job is arithmetic. Orchestrating two existing pipelines is
// exactly what this layer is for (CLAUDE.md: internal/httpapi is "request
// shaping, orchestration, and rendering ... no arithmetic, no domain
// rules").
//
// Ordering is deliberate and load-bearing. The fetch happens BEFORE
// ingestMu is taken and before a single byte is written, so a range the
// connector refuses — over the 31-day cap, or an upstream failure —
// refuses the whole request with the connector's own specific message and
// leaves the cost sheet on file untouched. The alternative (commit the
// cost sheet, then report that the sync failed) would half-perform a
// combined action the owner asked for as one thing, and would do it in the
// direction that changes financial numbers.
func HandleCommitCostSheet(proxy *platformconnector.Proxy, store *storage.Queries, cache *answercache.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}

		file, header, err := readUploadedCostSheet(w, r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_upload", err.Error())
			return
		}
		defer file.Close()

		content, err := io.ReadAll(file)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_upload", fmt.Sprintf("reading uploaded file: %v", err))
			return
		}

		// Re-validate the SAME way, from the request's own bytes, from
		// scratch — spec FR-007. A prior /preview call's verdict is never
		// consulted or trusted here; a client claiming "it was fine before"
		// buys nothing.
		records, err := ingest.ParseCostSheet(bytes.NewReader(content), header.Filename)
		if err != nil {
			writeJSONError(w, http.StatusUnprocessableEntity, "invalid_cost_sheet", err.Error())
			return
		}

		coversFrom, coversTo := costSheetDateRange(records)

		// The connector fetch, if asked for, runs here: after validation,
		// before the lock, before any write. See this handler's doc comment
		// for why a connector refusal must refuse the whole request rather
		// than half-committing it.
		var (
			overlay     *pipeline.ConnectorOverlay
			syncSummary *ConnectorSyncSummaryView
		)
		if wantsConnectorSync(r) {
			if proxy == nil {
				writeJSONError(w, http.StatusInternalServerError, "connectors_unavailable",
					"this server was started without the platform connectors, so simulated revenue cannot be pulled in with this upload — re-send without sync_connectors to commit the cost sheet on its own")
				return
			}
			result, err := proxy.FetchRange(r.Context(), coversFrom, coversTo, proxy.Platforms())
			if err != nil {
				// Verbatim, the same treatment ParseCostSheet's errors get:
				// the connector's refusals already name the cap and the fix
				// ("covers 35 days, more than the 31-day limit ... sync a
				// shorter range"). Nothing has been written at this point,
				// which is what makes "your cost sheet was not changed" a
				// true statement rather than a hopeful one.
				writeJSONError(w, http.StatusUnprocessableEntity, "connector_fetch_failed",
					fmt.Sprintf("%s — the cost sheet was not committed; upload it again without \"also pull in simulated platform revenue\" to commit it on its own", ownerFacing(err)))
				return
			}
			overlay = connectorOverlayFor(result, proxy.Platforms())
			summary := summarizeConnectorSync(result)
			syncSummary = &summary
		}

		// From here on, this request touches the shared livedata file and the
		// shared persisted reconciliation state — see ingestMu's doc comment
		// above for the exact interleaving this closes.
		ingestMu.Lock()
		defer ingestMu.Unlock()

		ctx := r.Context()

		before, err := loadMarginSnapshot(ctx, store)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "query_failed", fmt.Sprintf("reading pre-commit margin totals: %v", err))
			return
		}

		if err := livedata.EnsureReady(); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "live_data_not_ready", err.Error())
			return
		}

		destPath := filepath.Join(livedata.Dir, liveCostSheetFilename)
		merged, err := mergeCostSheetUpload(destPath, records, content)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "merge_failed", fmt.Sprintf("merging %s with the existing cost sheet: %v", liveCostSheetFilename, err))
			return
		}
		if err := writeCostSheetAtomically(destPath, merged); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "write_failed", fmt.Sprintf("writing %s: %v", liveCostSheetFilename, err))
			return
		}

		// A seam for ingest_cost_sheet_test.go ONLY (nil in production, and in
		// every other test in this package): the one point in this critical
		// section where a test can deterministically pause a request that has
		// already written its file but not yet run the pipeline against it,
		// to prove a second, concurrent commit cannot write over it before
		// this one reads it back — see commitMu's doc comment above and
		// TestHandleCommitCostSheet_SerializesConcurrentCommits.
		if afterCommitWriteForTests != nil {
			afterCommitWriteForTests()
		}

		if cache != nil {
			if err := cache.Clear(ctx); err != nil {
				writeJSONError(w, http.StatusInternalServerError, "cache_clear_failed", err.Error())
				return
			}
		}

		// ONE pipeline run, carrying both the newly written cost sheet and
		// (when asked for) the connector overlay for the dates it covers.
		// Two sequential runs would persist an intermediate state in which
		// the new costs sat against the OLD revenue — a reconciliation that
		// was never true of anything, briefly readable by any concurrent
		// request and durably readable if the second run then failed. A nil
		// overlay makes this exactly the call it was before.
		if err := pipeline.RunIngestionPipelineWithConnectorOverlay(livedata.Dir, store, overlay); err != nil {
			// The file itself already passed validation above — a failure
			// here is an operational failure of the pipeline run, not a
			// rejection of the upload, so it is a 500, not a 422.
			writeJSONError(w, http.StatusInternalServerError, "pipeline_failed", err.Error())
			return
		}

		after, err := loadMarginSnapshot(ctx, store)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "query_failed", fmt.Sprintf("reading post-commit margin totals: %v", err))
			return
		}

		writeJSON(w, http.StatusOK, CommitCostSheetResponse{
			RowsCommitted: len(records),
			CoversFrom:    coversFrom.Format(dateLayout),
			CoversTo:      coversTo.Format(dateLayout),
			ConnectorSync: syncSummary,
			Before:        toMarginSnapshotView(before),
			After:         toMarginSnapshotView(after),
		})
	}
}

// HandleCostSheetTemplate implements GET /api/ingest/cost-sheet/template
// (spec FR-006). The example rows are fabricated, clearly-labeled data
// ("Example ..."), deliberately NOT copied from the real dataset — a
// downloadable "template" that turned out to secretly be real dataset rows
// would blur the line this whole feature exists to keep bright.
func HandleCostSheetTemplate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="cost_sheet_template.csv"`)
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, costSheetTemplateCSV)
}

const costSheetTemplateCSV = `invoice_id,invoice_date,supplier,category,amount,notes
INV-TEMPLATE-001,2026-01-15,Example Produce Co.,produce,250.00,Weekly produce delivery
INV-TEMPLATE-002,2026-01-16,Example Beverage Distributors,beverage,180.50,"Kegs, soft drinks, mixers"
`

// readUploadedCostSheet parses a multipart/form-data body (field name
// "file") and returns the uploaded file, bounded by
// maxCostSheetUploadBytes so an unbounded upload cannot exhaust server
// memory (Constitution's "explicit caps" hard limit, applied here).
func readUploadedCostSheet(w http.ResponseWriter, r *http.Request) (multipart.File, *multipart.FileHeader, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxCostSheetUploadBytes)
	if err := r.ParseMultipartForm(maxCostSheetUploadBytes); err != nil {
		return nil, nil, fmt.Errorf("parsing multipart upload (expected multipart/form-data, field \"file\", under %d bytes): %w", maxCostSheetUploadBytes, err)
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return nil, nil, fmt.Errorf("reading uploaded file (expected a multipart field named \"file\"): %w", err)
	}
	return file, header, nil
}

// renderCostSheetRows converts parsed ingest.CostInvoiceRecords into the
// API's CostSheetRowView shape, also returning the total amount in cents
// (never separately recomputed by the frontend — one sum, computed here).
func renderCostSheetRows(records []ingest.CostInvoiceRecord) ([]CostSheetRowView, int64) {
	rows := make([]CostSheetRowView, 0, len(records))
	var total int64
	for _, rec := range records {
		total += rec.AmountCents
		rows = append(rows, CostSheetRowView{
			InvoiceID:   rec.InvoiceID,
			InvoiceDate: rec.InvoiceDate.Format(dateLayout),
			Supplier:    rec.Supplier,
			Category:    rec.Category,
			Amount:      money.FormatCents(rec.AmountCents),
			Notes:       rec.Notes,
			SourceRowRef: SourceRowRefView{
				File: rec.Ref.File,
				Row:  rec.Ref.Row,
			},
		})
	}
	return rows, total
}

// marginSnapshot is the internal (cents-based) form of MarginSnapshotView.
type marginSnapshot struct {
	HasData     bool
	Days        int
	MarginCents int64
}

// loadMarginSnapshot reads the product's currently-persisted total margin
// across the real data date range. When storage.LoadDataDateRange errors —
// which it does specifically when no daily_reconciliation rows exist yet, a
// genuinely first-ever commit — that is reported as HasData: false, nil
// error: "no prior data" is an expected, valid state here, not a query
// failure (spec Acceptance Scenario US1.3). A failure of the SECOND query
// (listing rows in an already-known-good range) is a real error and is
// returned as one.
func loadMarginSnapshot(ctx context.Context, q storage.Querier) (marginSnapshot, error) {
	start, end, rangeErr := storage.LoadDataDateRange(ctx, q)
	if rangeErr != nil {
		// Only the EXPECTED, sentinel failure (no rows yet — a server's
		// very first commit) degrades to "no prior data". Any other error
		// (a connection drop, a malformed query) is a real failure and
		// must propagate as one: found live, a transient DB hiccup here
		// was previously served as a confidently wrong "days: 0, margin:
		// null" statement about the owner's history, on the one endpoint
		// that changes financial numbers.
		if errors.Is(rangeErr, storage.ErrNoReconciliationDataYet) {
			return marginSnapshot{HasData: false}, nil
		}
		return marginSnapshot{}, rangeErr
	}

	days, err := storage.LoadDailyReconciliationsInPeriod(ctx, q, start, end)
	if err != nil {
		return marginSnapshot{}, err
	}

	var total int64
	for _, d := range days {
		total += d.MarginCents
	}
	return marginSnapshot{HasData: true, Days: len(days), MarginCents: total}, nil
}

// toMarginSnapshotView renders a marginSnapshot for the API.
func toMarginSnapshotView(s marginSnapshot) MarginSnapshotView {
	view := MarginSnapshotView{Days: s.Days}
	if s.HasData {
		formatted := money.FormatCents(s.MarginCents)
		view.Margin = &formatted
	}
	return view
}
