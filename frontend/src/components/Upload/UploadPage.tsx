import { useRef, useState, type DragEvent } from 'react'
import {
  AlertTriangle,
  CheckCircle2,
  Download,
  FileSpreadsheet,
  Loader2,
  UploadCloud,
} from 'lucide-react'

import { Button } from '@/components/ui/button'
import DataGrid from '@/components/Charts/DataGrid'
import { Chip, PageContainer, PageHeader, Panel, PanelHeader } from '@/components/ui/page'
import { API_BASE, ApiError, postMultipart } from '@/lib/api'

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

interface CommitCostSheetApi {
  rows_committed: number
  before: MarginSnapshotApi
  after: MarginSnapshotApi
}

type Stage =
  | { name: 'idle' }
  | { name: 'previewing'; file: File }
  | { name: 'preview_error'; file: File; message: string }
  | { name: 'previewed'; file: File; preview: PreviewCostSheetApi }
  | { name: 'committing'; file: File; preview: PreviewCostSheetApi }
  | { name: 'commit_error'; file: File; preview: PreviewCostSheetApi; message: string }
  | { name: 'committed'; result: CommitCostSheetApi }

function errorMessage(caught: unknown): string {
  if (caught instanceof ApiError) return caught.message
  if (caught instanceof Error) return caught.message
  return String(caught)
}

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
 * `/upload` — "Upload cost sheet". A file picker (click-to-browse and
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
export default function UploadPage() {
  const [stage, setStage] = useState<Stage>({ name: 'idle' })
  const [dragActive, setDragActive] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  async function handleFile(file: File) {
    setStage({ name: 'previewing', file })
    try {
      const preview = await postMultipart<PreviewCostSheetApi>(
        '/api/ingest/cost-sheet/preview',
        file,
      )
      setStage({ name: 'previewed', file, preview })
    } catch (caught) {
      setStage({ name: 'preview_error', file, message: errorMessage(caught) })
    }
  }

  async function handleCommit() {
    if (stage.name !== 'previewed') return
    const { file, preview } = stage
    setStage({ name: 'committing', file, preview })
    try {
      const result = await postMultipart<CommitCostSheetApi>(
        '/api/ingest/cost-sheet/commit',
        file,
      )
      setStage({ name: 'committed', result })
    } catch (caught) {
      setStage({ name: 'commit_error', file, preview, message: errorMessage(caught) })
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

  return (
    <PageContainer className="flex flex-col gap-5">
      <PageHeader
        eyebrow="Input costs"
        title="Upload cost sheet"
        meta={<Chip icon={FileSpreadsheet}>Supplier cost sheet CSV</Chip>}
        actions={
          <Button variant="outline" size="sm" asChild>
            <a href={`${API_BASE}/api/ingest/cost-sheet/template`} download>
              <Download aria-hidden="true" />
              Download template
            </a>
          </Button>
        }
      />

      <Panel className="p-5 sm:p-6">
        <PanelHeader
          eyebrow="Step 1"
          title="Choose a supplier cost sheet"
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
          <p className="text-sm font-medium text-foreground">
            Click to browse, or drag a CSV file here
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
                <Button size="sm" onClick={() => void handleCommit()} disabled={isBusy}>
                  {stage.name === 'committing' ? (
                    <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
                  ) : null}
                  Confirm & Ingest
                </Button>
              </div>
            }
          />
          <p className="mt-1 text-xs text-muted-foreground">
            Nothing has been saved yet — {preview.row_count} row
            {preview.row_count === 1 ? '' : 's'} parsed, totalling{' '}
            {formatUsd(preview.total_amount)}. Review before confirming.
          </p>

          <DataGrid
            className="mt-4"
            title="Parsed cost sheet rows"
            columns={['Invoice ID', 'Date', 'Supplier', 'Category', 'Amount', 'Notes']}
            rows={toTableRows(preview.rows)}
          />
        </Panel>
      ) : null}

      {stage.name === 'committed' ? (
        <Panel className="p-5 sm:p-6" role="status">
          <PanelHeader eyebrow="Done" title="Cost sheet ingested" />
          <p className="mt-1 flex items-center gap-1.5 text-sm text-success-text">
            <CheckCircle2 className="size-4 shrink-0" aria-hidden="true" />
            {stage.result.rows_committed} row{stage.result.rows_committed === 1 ? '' : 's'}{' '}
            committed and the full reconciliation pipeline re-ran.
          </p>
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
    </PageContainer>
  )
}
