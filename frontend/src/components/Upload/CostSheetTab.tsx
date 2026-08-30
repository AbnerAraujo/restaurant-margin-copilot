import { useRef, useState, type DragEvent } from 'react'
import {
  AlertTriangle,
  CheckCircle2,
  Download,
  FlaskConical,
  Loader2,
  UploadCloud,
} from 'lucide-react'

import { Button } from '@/components/ui/button'
import DataGrid from '@/components/Charts/DataGrid'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { Panel, PanelHeader } from '@/components/ui/page'
import { API_BASE, postMultipart } from '@/lib/api'
import { explainRequestFailure } from '@/lib/requestFailure'
import { useUnsavedChangesGuard } from '@/lib/useUnsavedChangesGuard'

// ---------------------------------------------------------------------------
// specs/007-cost-sheet-upload: the owner uploading a corrected/new supplier
// cost sheet through the web UI, with a real preview before anything is
// committed, and a downloadable template — replacing "ask a developer to run
// -ingest" as the only way to update input costs. Zero model involvement:
// both endpoints this page calls are pure deterministic Go (internal/ingest,
// internal/pipeline), reused unchanged from the CLI ingestion path.
// ---------------------------------------------------------------------------

interface SourceRowRefApi {
  file: string
  row: number
}

interface CostSheetRowApi {
  invoice_id: string
  invoice_date: string
  supplier: string
  category: string
  amount: string
  notes: string
  source_row_ref: SourceRowRefApi
}

interface PreviewCostSheetApi {
  row_count: number
  total_amount: string
  rows: CostSheetRowApi[]
}

interface MarginSnapshotApi {
  days: number
  /** null means no reconciliation was persisted yet — a real absence, never a fabricated "$0.00". */
  margin: string | null
}

interface ConnectorDedupDecisionApi {
  kind: string
  detail: string
}

/** What the connector sync that rode along with this upload actually did.
 *  Absent (null) when the box was unticked — a real "not asked for", never
 *  an empty sync. */
interface ConnectorSyncSummaryApi {
  simulated: boolean
  notice: string
  from: string
  to: string
  days_affected: number
  orders_synced: number
  refunds_synced: number
  tickets_synced: number
  duplicates_removed: number
  unresolved_overlaps: number
  dedup: ConnectorDedupDecisionApi[]
}

interface CommitCostSheetApi {
  rows_committed: number
  covers_from: string
  covers_to: string
  connector_sync: ConnectorSyncSummaryApi | null
  before: MarginSnapshotApi
  after: MarginSnapshotApi
}

/** The inclusive calendar range the previewed invoices cover — the same
 *  min/max scan the backend runs over the same rows (`costSheetDateRange`).
 *  Computed here only to NAME the range in the opt-in label before the
 *  commit; the backend never trusts it, and re-derives its own from the
 *  file's bytes at commit time. */
function coveredRange(preview: PreviewCostSheetApi): { from: string; to: string } | null {
  const dates = preview.rows.map((row) => row.invoice_date).filter(Boolean).sort()
  if (dates.length === 0) return null
  return { from: dates[0], to: dates[dates.length - 1] }
}

function describeRange(range: { from: string; to: string }): string {
  return range.from === range.to ? range.from : `${range.from} to ${range.to}`
}

type Stage =
  | { name: 'idle' }
  | { name: 'previewing'; file: File }
  | { name: 'preview_error'; file: File; message: string }
  | { name: 'previewed'; file: File; preview: PreviewCostSheetApi }
  | { name: 'committing'; file: File; preview: PreviewCostSheetApi }
  | { name: 'commit_error'; file: File; preview: PreviewCostSheetApi; message: string }
  | { name: 'committed'; result: CommitCostSheetApi }

function formatUsd(decimal: string): string {
  return Number(decimal).toLocaleString('en-US', {
    style: 'currency',
    currency: 'USD',
  })
}

function toTableRows(rows: CostSheetRowApi[]): string[][] {
  return rows.map((row) => [
    row.invoice_id,
    row.invoice_date,
    row.supplier,
    row.category || '—',
    formatUsd(row.amount),
    row.notes || '—',
  ])
}

function renderMargin(snapshot: MarginSnapshotApi): string {
  if (snapshot.margin === null) return 'No prior data'
  return `${formatUsd(snapshot.margin)} across ${snapshot.days} day${snapshot.days === 1 ? '' : 's'}`
}

/**
 * The supplier-cost-sheet half of `/upload`. A file picker (click-to-browse and
 * drag-and-drop both call the same `handleFile`), a template download, a
 * specific row-referenced validation-error display, a preview table, and a
 * commit step that re-states the real before/after margin effect.
 *
 * The `File` object selected at pick-time is held in state and reused
 * unchanged for the commit call (never re-read from the `<input>`) — the
 * same bytes that were previewed are the bytes committed, from this page's
 * point of view. The backend still re-validates those bytes from scratch at
 * commit time regardless (FR-007) — this page's discipline here is about
 * correctness of intent, not a substitute for that server-side guarantee.
 */
export default function CostSheetTab() {
  const [stage, setStage] = useState<Stage>({ name: 'idle' })
  const [dragActive, setDragActive] = useState(false)
  // Pre-ticked, and deliberately so. The owner's ask was that uploading a
  // cost sheet should not require a second trip to Connected Platforms, so
  // the default has to be the thing they asked for. What it must NOT be is
  // invisible: the control sits in the preview panel directly above the
  // commit button, names the exact dates it will pull, and says "simulated"
  // in its own label — so nobody reaches "Replace cost sheet" without having
  // been told what else that button does. The backend defaults the other
  // way (absent flag means no sync) precisely because an API has no label to
  // read; see wantsConnectorSync's doc comment in ingest_cost_sheet.go.
  const [syncConnectors, setSyncConnectors] = useState(true)
  const inputRef = useRef<HTMLInputElement>(null)
  // Found live: a fast double-click on "Replace cost sheet" fires two
  // synchronous click events before React's re-render disables the button
  // (setState from an event handler is batched — the handler's SECOND
  // synchronous invocation still closes over the pre-update `stage`, so
  // `stage.name !== 'previewed'` alone let both calls through and posted
  // the commit twice). A ref, unlike state, mutates synchronously and is
  // visible to that second invocation immediately, closing the exact
  // window `disabled={isBusy}` cannot close on its own.
  const committingRef = useRef(false)

  async function handleFile(file: File) {
    setStage({ name: 'previewing', file })
    try {
      const preview = await postMultipart<PreviewCostSheetApi>(
        '/api/ingest/cost-sheet/preview',
        file,
      )
      setStage({ name: 'previewed', file, preview })
    } catch (caught) {
      setStage({ name: 'preview_error', file, message: explainRequestFailure(caught) })
    }
  }

  async function handleCommit() {
    if (stage.name !== 'previewed') return
    if (committingRef.current) return
    committingRef.current = true
    const { file, preview } = stage
    setStage({ name: 'committing', file, preview })
    try {
      const result = await postMultipart<CommitCostSheetApi>(
        '/api/ingest/cost-sheet/commit',
        file,
        { sync_connectors: String(syncConnectors) },
      )
      setStage({ name: 'committed', result })
    } catch (caught) {
      setStage({ name: 'commit_error', file, preview, message: explainRequestFailure(caught) })
    } finally {
      committingRef.current = false
    }
  }

  function handleDrop(event: DragEvent<HTMLDivElement>) {
    event.preventDefault()
    setDragActive(false)
    const file = event.dataTransfer.files?.[0]
    if (file) void handleFile(file)
  }

  function reset() {
    setStage({ name: 'idle' })
    if (inputRef.current) inputRef.current.value = ''
  }

  const currentFile = 'file' in stage ? stage.file : null
  const preview = 'preview' in stage ? stage.preview : null
  const isBusy = stage.name === 'previewing' || stage.name === 'committing'
  const errorText =
    stage.name === 'preview_error' || stage.name === 'commit_error' ? stage.message : null
  // Defense in depth: the backend now refuses a 0-data-row file outright
  // (ingest.ParseCostSheet's "no data rows found" check), so `preview` here
  // should never actually carry row_count 0. This guard exists so that if
  // it ever does — a future parser change, a file shaped in some way this
  // page hasn't anticipated — the UI never lets a 0-row preview look like
  // an ordinary one with the commit button quietly enabled underneath it.
  const previewHasNoRows = preview !== null && preview.row_count === 0
  const range = preview === null ? null : coveredRange(preview)

  // "Meaningful in-progress content" for this page: anything staged past a
  // blank picker and short of an actual commit — a file mid-preview, a
  // successfully previewed table sitting on the commit button, or either
  // one's error state. `idle` (nothing picked yet) and `committed` (the
  // work is already saved) must never trigger the guard — see
  // UploadPage.test.tsx.
  const hasUnsavedChanges = stage.name !== 'idle' && stage.name !== 'committed'
  const { isBlocked, confirmDiscard, cancelDiscard } = useUnsavedChangesGuard(hasUnsavedChanges)

  // QA-found: the dialog below used to show "Nothing has been committed
  // yet" unconditionally, including while `stage.name === 'committing'` —
  // at that exact moment the commit POST is already in flight, and this
  // page has no way to cancel it (lib/api.ts's postMultipart takes no
  // AbortSignal, and even if it did, the request may already have reached
  // the server). Leaving then does NOT undo anything; it just means this
  // tab stops waiting for a result that may still land. Saying "nothing
  // has been committed yet" at that moment is a real, specific lie about
  // financial-data state (CLAUDE.md: a confidently wrong report is worse
  // than a refusal), not just an over-cautious warning.
  const isCommitInFlight = stage.name === 'committing'

  return (
    <div className="flex flex-col gap-5">
      <Panel className="p-5 sm:p-6">
        <PanelHeader
          eyebrow="Step 1"
          title="Choose a supplier cost sheet"
          actions={
            <Button variant="outline" size="sm" asChild>
              <a href={`${API_BASE}/api/ingest/cost-sheet/template`} download>
                <Download aria-hidden="true" />
                Download template
              </a>
            </Button>
          }
        />
        <p className="mt-1 text-xs text-muted-foreground">
          Accepts the same file the CLI <code className="font-mono">-ingest</code> flag
          reads: headers <code className="font-mono">invoice_id, invoice_date, supplier,
          category, amount, notes</code> (or the recognized aliases — see the template).
        </p>

        <div
          role="button"
          tabIndex={0}
          onClick={() => inputRef.current?.click()}
          onKeyDown={(event) => {
            if (event.key === 'Enter' || event.key === ' ') inputRef.current?.click()
          }}
          onDragOver={(event) => {
            event.preventDefault()
            setDragActive(true)
          }}
          onDragLeave={() => setDragActive(false)}
          onDrop={handleDrop}
          className={`mt-4 flex cursor-pointer flex-col items-center justify-center gap-2 rounded-lg border-2 border-dashed p-8 text-center transition-colors ${
            dragActive
              ? 'border-primary bg-primary/5'
              : 'border-border hover:border-primary/50 hover:bg-accent/40'
          }`}
        >
          <UploadCloud className="size-8 text-muted-foreground" aria-hidden="true" />
          {/* Was "Click to browse": this target is reachable by keyboard
              (Enter/Space, see onKeyDown above) and by touch, so naming the
              mouse was both wrong and the only instruction on offer. */}
          <p className="text-sm font-medium text-foreground">
            Choose a file, or drag a CSV here
          </p>
          {currentFile ? (
            <p className="text-xs text-muted-foreground">Selected: {currentFile.name}</p>
          ) : null}
          <input
            ref={inputRef}
            type="file"
            accept=".csv,text/csv"
            className="sr-only"
            aria-label="Choose a supplier cost sheet CSV file"
            onChange={(event) => {
              const file = event.target.files?.[0]
              if (file) void handleFile(file)
            }}
          />
        </div>

        {stage.name === 'previewing' ? (
          <p className="mt-3 flex items-center gap-1.5 text-xs text-muted-foreground">
            <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
            Validating…
          </p>
        ) : null}

        {errorText ? (
          <p role="alert" className="mt-3 flex items-start gap-1.5 text-xs text-destructive-text">
            <AlertTriangle className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
            <span className="font-mono">{errorText}</span>
          </p>
        ) : null}
      </Panel>

      {preview ? (
        <Panel className="p-5 sm:p-6">
          <PanelHeader
            eyebrow="Step 2"
            title="Preview"
            actions={
              <div className="flex items-center gap-2">
                <Button variant="outline" size="sm" onClick={reset} disabled={isBusy}>
                  Choose a different file
                </Button>
                <Button
                  size="sm"
                  onClick={() => void handleCommit()}
                  disabled={isBusy || previewHasNoRows}
                >
                  {stage.name === 'committing' ? (
                    <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
                  ) : null}
                  {/* Was "Confirm & Ingest": Title Case, an ampersand, and a
                      bare verb that never said what would be confirmed. The
                      outcome this button actually produces is that the cost
                      sheet on file is replaced by this file — which is what
                      the warning above it already warns about, so the two now
                      use the same words (ux-writing: a confirm restates the
                      action AND the object). */}
                  Replace cost sheet
                </Button>
              </div>
            }
          />
          {previewHasNoRows ? (
            <p role="alert" className="mt-1 flex items-start gap-1.5 text-xs text-destructive-text">
              <AlertTriangle className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
              <span>
                This file has no data rows — nothing to ingest. Confirming would replace the
                current cost sheet with an empty one. Choose a different file.
              </span>
            </p>
          ) : (
            <p className="mt-1 text-xs text-muted-foreground">
              Nothing has been saved yet — {preview.row_count} row
              {preview.row_count === 1 ? '' : 's'} parsed, totalling{' '}
              {formatUsd(preview.total_amount)}. Review the rows below, then
              replace the cost sheet on file with them.
            </p>
          )}

          {range && !previewHasNoRows ? (
            <div className="mt-4 rounded-lg border border-warning/25 bg-warning/10 p-3">
              <label
                htmlFor="cost-sheet-sync-connectors"
                className="flex cursor-pointer items-start gap-2.5 text-xs"
              >
                <input
                  id="cost-sheet-sync-connectors"
                  type="checkbox"
                  checked={syncConnectors}
                  disabled={isBusy}
                  onChange={(event) => setSyncConnectors(event.target.checked)}
                  className="mt-0.5 size-3.5 shrink-0 rounded-sm border-input accent-primary focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
                />
                <span>
                  <span className="flex items-center gap-1.5 font-medium text-foreground">
                    <FlaskConical className="size-3.5 shrink-0 text-warning-text" aria-hidden="true" />
                    Also pull in simulated platform revenue for {describeRange(range)}
                  </span>
                  {/* The disclosure is here, next to the control, and not
                      only in the Connected Platforms tab — an owner who
                      never opens that tab must still be told what this
                      box does before they tick past it. */}
                  <span className="mt-1 block leading-relaxed text-muted-foreground">
                    Your costs alone would reconcile against whatever revenue is already on file
                    for those days. Leaving this ticked also pulls iFood, Just Eat Takeaway and POS
                    orders for the same dates, so the margin you see afterwards is a complete one.
                    Those three connections are <strong>simulated</strong> — no real account is
                    connected, and the orders are generated locally for demonstration. Untick to
                    replace the cost sheet on its own.
                  </span>
                </span>
              </label>
            </div>
          ) : null}

          <DataGrid
            className="mt-4"
            title="Parsed cost sheet rows"
            columns={['Invoice ID', 'Date', 'Supplier', 'Category', 'Amount', 'Notes']}
            rows={toTableRows(preview.rows)}
            // Column header filters (Excel/Sheets-style), additive to
            // nothing else on this page — there is no other filter surface
            // here to compose with. A real supplier cost sheet can run to
            // dozens of line items across several suppliers and categories,
            // and this is a review-before-commit step: an owner spot-checking
            // one supplier's or one category's rows before replacing the
            // whole file on file benefits from narrowing to just those rows.
            // Invoice ID gets a text filter (each one is close to unique, so
            // a checklist wouldn't scale); Supplier/Category get a checklist
            // (both genuinely categorical, options drawn from this file's own
            // rows, never hardcoded).
            columnFilters={{ 0: 'text', 2: 'categorical', 3: 'categorical' }}
            filterEmptyLabel="No cost sheet rows match these filters."
          />
        </Panel>
      ) : null}

      {stage.name === 'committed' ? (
        <Panel className="p-5 sm:p-6" role="status">
          <PanelHeader eyebrow="Done" title="Cost sheet ingested" />
          <p className="mt-1 flex items-center gap-1.5 text-sm text-success-text">
            <CheckCircle2 className="size-4 shrink-0" aria-hidden="true" />
            {stage.result.rows_committed} row{stage.result.rows_committed === 1 ? '' : 's'}{' '}
            covering {describeRange({ from: stage.result.covers_from, to: stage.result.covers_to })}{' '}
            committed and the full reconciliation pipeline re-ran.
          </p>

          {/* Restated after the fact, not only before it. The owner ticked a
              box a moment ago; what they need now is what actually landed —
              including, and especially, the overlaps the matcher refused to
              resolve, which are possible double-counts sitting inside the
              margin figures directly below this. */}
          {stage.result.connector_sync ? (
            <div className="mt-3 rounded-md border border-warning/25 bg-warning/10 p-2.5 text-xs">
              <p className="flex items-start gap-1.5 text-foreground">
                <FlaskConical className="mt-0.5 size-3.5 shrink-0 text-warning-text" aria-hidden="true" />
                <span>
                  {stage.result.connector_sync.orders_synced} simulated delivery order
                  {stage.result.connector_sync.orders_synced === 1 ? '' : 's'} and{' '}
                  {stage.result.connector_sync.tickets_synced} POS ticket
                  {stage.result.connector_sync.tickets_synced === 1 ? '' : 's'} were pulled in for{' '}
                  {describeRange(stage.result.connector_sync)} and reconciled together with these
                  costs. {stage.result.connector_sync.notice}
                </span>
              </p>
              {stage.result.connector_sync.duplicates_removed > 0 ? (
                <p className="mt-1.5 pl-5 text-muted-foreground">
                  {stage.result.connector_sync.duplicates_removed} POS ticket
                  {stage.result.connector_sync.duplicates_removed === 1 ? '' : 's'} matched a
                  delivery order and {stage.result.connector_sync.duplicates_removed === 1 ? 'was' : 'were'}{' '}
                  counted once rather than twice.
                </p>
              ) : null}
              {stage.result.connector_sync.unresolved_overlaps > 0 ? (
                <p role="note" className="mt-1.5 flex items-start gap-1.5 pl-5 text-warning-text">
                  <AlertTriangle className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
                  <span>
                    {stage.result.connector_sync.unresolved_overlaps} overlap
                    {stage.result.connector_sync.unresolved_overlaps === 1 ? '' : 's'} could not be
                    resolved and {stage.result.connector_sync.unresolved_overlaps === 1 ? 'was' : 'were'}{' '}
                    left in rather than guessed at, so those days may count an order twice. Each one
                    is flagged on the day it affected.
                  </span>
                </p>
              ) : null}
            </div>
          ) : (
            <p className="mt-3 text-xs text-muted-foreground">
              Simulated platform revenue was not pulled in — these days reconcile against whatever
              revenue was already on file for them.
            </p>
          )}
          <dl className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div className="rounded-md border border-border bg-muted/30 p-3">
              <dt className="text-micro font-medium uppercase tracking-wider text-muted-foreground">
                Margin before
              </dt>
              <dd className="mt-1 text-sm font-medium text-foreground">
                {renderMargin(stage.result.before)}
              </dd>
            </div>
            <div className="rounded-md border border-border bg-muted/30 p-3">
              <dt className="text-micro font-medium uppercase tracking-wider text-muted-foreground">
                Margin after
              </dt>
              <dd className="mt-1 text-sm font-medium text-foreground">
                {renderMargin(stage.result.after)}
              </dd>
            </div>
          </dl>
          <Button className="mt-4" variant="outline" size="sm" onClick={reset}>
            Upload another file
          </Button>
        </Panel>
      ) : null}

      <ConfirmDialog
        open={isBlocked}
        title={isCommitInFlight ? 'Leave while replacing the cost sheet?' : 'Discard this cost sheet preview?'}
        description={
          isCommitInFlight
            ? "This replace request has already been sent and can't be cancelled from here — leaving now won't undo it. If it succeeds, the cost sheet will still be replaced; you just won't see the before/after confirmation. Check Today's Close afterward if you want to be sure."
            : "Nothing has been committed yet. Leaving this page now discards the staged file and its preview — you'd need to re-pick it and re-run the preview from scratch."
        }
        confirmLabel={isCommitInFlight ? 'Leave anyway' : 'Discard preview'}
        onConfirm={confirmDiscard}
        onCancel={cancelDiscard}
      />
    </div>
  )
}
