import { useEffect, useState } from 'react'
import { AlertTriangle, CheckCircle2, FlaskConical, Loader2, RefreshCw } from 'lucide-react'

import { Button } from '@/components/ui/button'
import DataGrid from '@/components/Charts/DataGrid'
import { Input } from '@/components/ui/input'
import { Chip, Panel, PanelHeader } from '@/components/ui/page'
import { getJson, postJson } from '@/lib/api'
import { explainRequestFailure } from '@/lib/requestFailure'

// ---------------------------------------------------------------------------
// specs/010-platform-connector-proxy and specs/012-pos-connector-dedup:
// pulling revenue from iFood, Just Eat Takeaway and the in-house POS instead
// of waiting for a CSV export from each one.
//
// All three connections are SIMULATED. This project has no partner-API
// credentials for either platform and no POS terminal to poll, so the backend
// stands in three mock APIs with genuinely different wire formats and
// normalizes them through one proxy into the same record types the CSV path
// produces.
//
// That fact is stated four separate times on the way to a number: in the tab
// label this component renders inside, in the notice below (which is the first
// thing in the panel and cannot be dismissed), on every source row, and in
// the sync button's own words. The redundancy is deliberate — a cropped
// screenshot, a scrolled-past banner, or a glance at just the results table
// each still carry the disclosure. Zero model involvement in this flow: the
// endpoints it calls are deterministic Go end to end.
//
// The second thing this surface has to be honest about is deduplication. A POS
// that takes delivery orders through an integration records them as its own
// tickets, so a combined sync can see the same order twice. The backend
// resolves what it can and REFUSES to guess at the rest, and both outcomes are
// shown here — the removals as a count, the unresolved overlaps as a warning
// naming what to check. Reporting only the removals would let this panel claim
// a clean result the product did not achieve.
// ---------------------------------------------------------------------------

interface ConnectorPlatformApi {
  platform: string
  name: string
  simulated: boolean
  wire_format: string
  commission_rate_pct: string
  endpoint: string
}

interface ConnectorPlatformsApi {
  simulated: boolean
  notice: string
  platforms: ConnectorPlatformApi[]
}

interface ConnectorDayTotalsApi {
  platform: string
  platform_name: string
  date: string
  order_count: number
  refund_count: number
  gross_sales: string
  refunds: string
  commissions: string
  duplicates_removed: number
  unresolved_overlaps: number
}

interface ConnectorDedupDecisionApi {
  kind: string
  resolved: boolean
  date: string
  platform: string
  pos_order_id: string
  platform_order_id?: string
  detail: string
}

interface ConnectorSyncPreviewApi {
  simulated: boolean
  notice: string
  from: string
  to: string
  order_count: number
  gross_sales: string
  refunds: string
  commissions: string
  duplicates_removed: number
  unresolved_overlaps: number
  dedup: ConnectorDedupDecisionApi[]
  days: ConnectorDayTotalsApi[]
}

interface MarginSnapshotApi {
  days: number
  /** null means no reconciliation was persisted yet — a real absence, never a fabricated "$0.00". */
  margin: string | null
}

interface ConnectorSyncApi {
  simulated: boolean
  from: string
  to: string
  days_affected: number
  orders_synced: number
  refunds_synced: number
  tickets_synced: number
  duplicates_removed: number
  unresolved_overlaps: number
  dedup: ConnectorDedupDecisionApi[]
  before: MarginSnapshotApi
  after: MarginSnapshotApi
}

type Stage =
  | { name: 'idle' }
  | { name: 'previewing' }
  | { name: 'preview_error'; message: string }
  | { name: 'previewed'; preview: ConnectorSyncPreviewApi }
  | { name: 'syncing'; preview: ConnectorSyncPreviewApi }
  | { name: 'sync_error'; preview: ConnectorSyncPreviewApi; message: string }
  | { name: 'synced'; result: ConnectorSyncApi }

function formatUsd(decimal: string): string {
  return Number(decimal).toLocaleString('en-US', { style: 'currency', currency: 'USD' })
}

function renderMargin(snapshot: MarginSnapshotApi): string {
  if (snapshot.margin === null) return 'No prior data'
  return `${formatUsd(snapshot.margin)} across ${snapshot.days} day${snapshot.days === 1 ? '' : 's'}`
}

function toTableRows(days: ConnectorDayTotalsApi[]): string[][] {
  return days.map((day) => [
    day.date,
    day.platform_name,
    String(day.order_count),
    formatUsd(day.gross_sales),
    day.refund_count === 0 ? '—' : `${formatUsd(day.refunds)} (${day.refund_count})`,
    // The POS charges no commission at all, so an em dash rather than
    // "$0.00" — a zero here would read as a platform that happens to be
    // free, which is a different and wrong claim.
    day.platform === 'pos' ? '—' : formatUsd(day.commissions),
    dedupCell(day),
  ])
}

/** One cell summarizing what deduplication did to this source on this day.
 * The unresolved count is shown alongside the removals rather than hidden,
 * because an overlap the backend declined to resolve is a possible
 * double-count sitting inside the gross figure in the same row. */
function dedupCell(day: ConnectorDayTotalsApi): string {
  const parts = []
  if (day.duplicates_removed > 0) parts.push(`${day.duplicates_removed} removed`)
  if (day.unresolved_overlaps > 0) parts.push(`${day.unresolved_overlaps} unresolved`)
  return parts.length === 0 ? '—' : parts.join(', ')
}

/** The default range: the seven days ending today, in the owner's own clock. */
function defaultRange(): { from: string; to: string } {
  const today = new Date()
  const start = new Date(today)
  start.setDate(start.getDate() - 6)
  const iso = (d: Date) => d.toISOString().slice(0, 10)
  return { from: iso(start), to: iso(today) }
}

/**
 * What deduplication did, and — the half that matters more — what it
 * declined to do.
 *
 * The removals are the good news and are stated plainly. The unresolved
 * overlaps are shown as a warning, not folded into the same sentence,
 * because they mean something different in kind: the gross figure beside
 * them may still count an order twice, and the owner is the only one who
 * can settle it. A panel that reported "12 duplicates removed" and stayed
 * silent about the 4 it could not place would be claiming a clean close it
 * did not achieve.
 */
function DedupSummary({
  removed,
  unresolved,
  decisions,
}: {
  removed: number
  unresolved: number
  decisions: ConnectorDedupDecisionApi[]
}) {
  if (!removed && !unresolved) return null

  // Tolerant of a missing list: the counts are the load-bearing part, and a
  // response that reported "4 unresolved" without the itemization must still
  // show the warning rather than crash the panel that carries it.
  const unresolvedDetails = (decisions ?? [])
    .filter((d) => d.kind.startsWith('unresolved_'))
    .map((d) => d.detail)

  return (
    <div className="mt-3 flex flex-col gap-2">
      {removed > 0 ? (
        <p className="flex items-start gap-1.5 text-xs text-muted-foreground">
          <CheckCircle2 className="mt-0.5 size-3.5 shrink-0 text-success-text" aria-hidden="true" />
          <span>
            {removed} POS ticket{removed === 1 ? '' : 's'} matched an order that also came through
            a delivery platform, so {removed === 1 ? 'it is' : 'they are'} counted once rather
            than twice. The platform&apos;s record is the one kept, so its commission is still
            charged against your margin.
          </span>
        </p>
      ) : null}

      {unresolved > 0 ? (
        <div
          role="note"
          className="rounded-md border border-warning/25 bg-warning/10 p-2.5 text-xs"
        >
          <p className="flex items-start gap-1.5 text-warning-text">
            <AlertTriangle className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
            <span>
              {unresolved} POS ticket{unresolved === 1 ? '' : 's'} look
              {unresolved === 1 ? 's' : ''} like {unresolved === 1 ? 'it' : 'they'} came through a
              delivery platform, but the evidence didn&apos;t single out which order.{' '}
              {unresolved === 1 ? 'It was' : 'They were'} left in rather than guessed at, so these
              days may count {unresolved === 1 ? 'that order' : 'those orders'} twice.
            </span>
          </p>
          <ul className="mt-1.5 flex list-disc flex-col gap-1 pl-8 text-muted-foreground">
            {unresolvedDetails.slice(0, 3).map((detail) => (
              <li key={detail}>{detail}</li>
            ))}
            {unresolvedDetails.length > 3 ? (
              <li>and {unresolvedDetails.length - 3} more, each flagged on its own day.</li>
            ) : null}
          </ul>
        </div>
      ) : null}
    </div>
  )
}

export default function ConnectedPlatformsTab() {
  const initialRange = defaultRange()
  const [from, setFrom] = useState(initialRange.from)
  const [to, setTo] = useState(initialRange.to)
  const [platforms, setPlatforms] = useState<ConnectorPlatformApi[] | null>(null)
  const [platformsError, setPlatformsError] = useState<string | null>(null)
  const [stage, setStage] = useState<Stage>({ name: 'idle' })

  useEffect(() => {
    let cancelled = false
    getJson<ConnectorPlatformsApi>('/api/connectors/platforms')
      .then((data) => {
        if (!cancelled) setPlatforms(data.platforms)
      })
      .catch((caught: unknown) => {
        if (!cancelled) setPlatformsError(explainRequestFailure(caught))
      })
    return () => {
      cancelled = true
    }
  }, [])

  async function handlePreview() {
    setStage({ name: 'previewing' })
    try {
      const preview = await postJson<ConnectorSyncPreviewApi>('/api/connectors/sync/preview', {
        from,
        to,
      })
      setStage({ name: 'previewed', preview })
    } catch (caught) {
      setStage({ name: 'preview_error', message: explainRequestFailure(caught) })
    }
  }

  async function handleSync() {
    if (stage.name !== 'previewed') return
    const { preview } = stage
    setStage({ name: 'syncing', preview })
    try {
      const result = await postJson<ConnectorSyncApi>('/api/connectors/sync', { from, to })
      setStage({ name: 'synced', result })
    } catch (caught) {
      setStage({ name: 'sync_error', preview, message: explainRequestFailure(caught) })
    }
  }

  const preview = 'preview' in stage ? stage.preview : null
  const isBusy = stage.name === 'previewing' || stage.name === 'syncing'
  const errorText =
    stage.name === 'preview_error' || stage.name === 'sync_error' ? stage.message : null
  // A range the platforms reported nothing for is not a boring success. A
  // sync is authoritative for the range it covers, so committing an empty
  // one would REMOVE the delivery revenue currently on file for those days
  // and lower margin, with only the existing missing-delivery-source flag
  // to explain it. Say so, and refuse to make it a one-click action.
  const previewIsEmpty = preview !== null && preview.order_count === 0

  return (
    <div className="flex flex-col gap-5">
      {/*
        First element in the panel, above every control, with no dismiss
        affordance anywhere. role="note" rather than "alert": this is a
        standing condition of the feature, not an event, and an alert would
        be announced again on every re-render.
      */}
      <Panel tone="muted" className="p-5 sm:p-6" role="note" aria-labelledby="connector-simulation-heading">
        <div className="flex items-start gap-2.5">
          <FlaskConical className="mt-0.5 size-4 shrink-0 text-warning-text" aria-hidden="true" />
          <div>
            <h2 id="connector-simulation-heading" className="text-sm font-semibold text-foreground">
              These connections are simulated
            </h2>
            <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
              No real iFood account, Just Eat Takeaway account or POS terminal is connected.
              This prototype has no partner-API access to any of them, so the orders below are
              generated locally to show how the connector works. Anything you sync from here
              reaches your margin as simulated revenue, and it stays labeled that way
              everywhere it appears.
            </p>
          </div>
        </div>
      </Panel>

      <Panel className="p-5 sm:p-6">
        <PanelHeader eyebrow="Step 1" title="Choose what to pull" />

        {platformsError ? (
          <p role="alert" className="mt-3 flex items-start gap-1.5 text-xs text-destructive-text">
            <AlertTriangle className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
            <span>
              We couldn&apos;t load the connector list. {platformsError} Check that the backend is
              running, then reload this page.
            </span>
          </p>
        ) : null}

        <ul className="mt-4 flex flex-col gap-2">
          {(platforms ?? []).map((platform) => (
            <li
              key={platform.platform}
              className="rounded-lg border border-border bg-muted/30 p-3"
            >
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-sm font-medium text-foreground">{platform.name}</span>
                {/* Per-row, not only in the banner above: a screenshot
                    cropped to this list still discloses. */}
                <Chip tone="warning" icon={FlaskConical}>
                  Simulated connection
                </Chip>
                {platform.commission_rate_pct ? (
                  <Chip>{platform.commission_rate_pct}% commission</Chip>
                ) : (
                  <Chip>No commission</Chip>
                )}
              </div>
              <p className="mt-1.5 text-xs text-muted-foreground">
                Normalized from: {platform.wire_format}
              </p>
              <p className="mt-0.5 font-mono text-micro text-muted-foreground">
                {platform.endpoint}
              </p>
            </li>
          ))}
        </ul>

        <div className="mt-4 flex flex-wrap items-center gap-x-3 gap-y-2">
          <div className="flex items-center gap-1.5">
            <label htmlFor="connector-from" className="text-xs font-medium text-muted-foreground">
              From
            </label>
            <Input
              id="connector-from"
              type="date"
              className="h-8 w-auto"
              value={from}
              max={to}
              onChange={(event) => setFrom(event.target.value)}
            />
          </div>
          <div className="flex items-center gap-1.5">
            <label htmlFor="connector-to" className="text-xs font-medium text-muted-foreground">
              To
            </label>
            <Input
              id="connector-to"
              type="date"
              className="h-8 w-auto"
              value={to}
              min={from}
              onChange={(event) => setTo(event.target.value)}
            />
          </div>
          <Button size="sm" onClick={() => void handlePreview()} disabled={isBusy}>
            {stage.name === 'previewing' ? (
              <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
            ) : null}
            Preview orders
          </Button>
        </div>
        <p className="mt-2 text-xs text-muted-foreground">
          Up to 31 days at a time. Nothing is saved until you sync.
        </p>

        {stage.name === 'previewing' ? (
          <p className="mt-3 flex items-center gap-1.5 text-xs text-muted-foreground">
            <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
            Fetching orders…
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
              <Button
                size="sm"
                onClick={() => void handleSync()}
                disabled={isBusy || previewIsEmpty}
              >
                {stage.name === 'syncing' ? (
                  <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
                ) : null}
                {/* The button itself says "simulated" — the last place a
                    person looks before the numbers change. */}
                Sync simulated orders
              </Button>
            }
          />
          {previewIsEmpty ? (
            <p role="alert" className="mt-1 flex items-start gap-1.5 text-xs text-destructive-text">
              <AlertTriangle className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
              <span>
                The platforms reported no orders between {preview.from} and {preview.to}. Syncing
                would clear the delivery revenue currently on file for those days and leave them
                empty. Choose a different range.
              </span>
            </p>
          ) : (
            <p className="mt-1 text-xs text-muted-foreground">
              Nothing has been saved yet — {preview.order_count} simulated order
              {preview.order_count === 1 ? '' : 's'} from {preview.from} to {preview.to}, totalling{' '}
              {formatUsd(preview.gross_sales)} in gross sales and{' '}
              {formatUsd(preview.commissions)} in commission. Review the days below, then sync
              them into your close.
            </p>
          )}

          <DedupSummary
            removed={preview.duplicates_removed}
            unresolved={preview.unresolved_overlaps}
            decisions={preview.dedup}
          />

          <DataGrid
            className="mt-4"
            title="Simulated orders by source and day"
            columns={['Date', 'Source', 'Orders', 'Gross sales', 'Refunds', 'Commission', 'Duplicates']}
            rows={toTableRows(preview.days)}
          />
        </Panel>
      ) : null}

      {stage.name === 'synced' ? (
        <Panel className="p-5 sm:p-6" role="status">
          <PanelHeader eyebrow="Done" title="Simulated orders synced" />
          <p className="mt-1 flex items-center gap-1.5 text-sm text-success-text">
            <CheckCircle2 className="size-4 shrink-0" aria-hidden="true" />
            {stage.result.orders_synced} simulated delivery order
            {stage.result.orders_synced === 1 ? '' : 's'} and {stage.result.tickets_synced} POS
            ticket{stage.result.tickets_synced === 1 ? '' : 's'} replaced the revenue on file for{' '}
            {stage.result.from} to {stage.result.to}, and the full reconciliation re-ran.
          </p>

          <DedupSummary
            removed={stage.result.duplicates_removed}
            unresolved={stage.result.unresolved_overlaps}
            decisions={stage.result.dedup}
          />
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
          <p className="mt-3 text-xs text-muted-foreground">
            Those days now read from the simulated connectors. Every row carries a{' '}
            <code className="font-mono">simulated://</code> source, so nothing here can be
            mistaken for a real platform settlement — and every duplicate resolved or left
            unresolved above is recorded as a discrepancy flag on the day it affected, where you
            can find it again after this panel is gone.
          </p>
          <Button
            className="mt-4"
            variant="outline"
            size="sm"
            onClick={() => setStage({ name: 'idle' })}
          >
            <RefreshCw aria-hidden="true" />
            Sync another range
          </Button>
        </Panel>
      ) : null}
    </div>
  )
}
