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

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/answercache"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/ingest"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/livedata"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/money"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/pipeline"
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
	RowsCommitted int                `json:"rows_committed"`
	Before        MarginSnapshotView `json:"before"`
	After         MarginSnapshotView `json:"after"`
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
func HandleCommitCostSheet(store *storage.Queries, cache *answercache.Cache) http.HandlerFunc {
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
		if err := os.WriteFile(destPath, content, 0o644); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "write_failed", fmt.Sprintf("writing %s: %v", liveCostSheetFilename, err))
			return
		}

		if cache != nil {
			if err := cache.Clear(ctx); err != nil {
				writeJSONError(w, http.StatusInternalServerError, "cache_clear_failed", err.Error())
				return
			}
		}

		if err := pipeline.RunIngestionPipeline(livedata.Dir, store); err != nil {
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
			Before:        toMarginSnapshotView(before),
			After:         toMarginSnapshotView(after),
		})
	}
}

// HandleCostSheetTemplate implements GET /api/ingest/cost-sheet/template
// (spec FR-006). The example rows are fabricated, clearly-labeled data
// ("Example ..."), deliberately NOT copied from backend/fixtures/ — a
// downloadable "template" that turned out to secretly be real fixture rows
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
		return marginSnapshot{HasData: false}, nil
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
