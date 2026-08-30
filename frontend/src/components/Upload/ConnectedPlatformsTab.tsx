import { useEffect, useState } from 'react'
import { AlertTriangle, CheckCircle2, FlaskConical, Loader2, RefreshCw } from 'lucide-react'

import { Button } from '@/components/ui/button'
import DataGrid from '@/components/Charts/DataGrid'
import { Input } from '@/components/ui/input'
import { Chip, Panel, PanelHeader } from '@/components/ui/page'
import { getJson, postJson } from '@/lib/api'
import { explainRequestFailure } from '@/lib/requestFailure'

// ---------------------------------------------------------------------------
// specs/010-platform-connector-proxy: pulling delivery revenue from iFood and
// Just Eat Takeaway instead of waiting for a CSV export from each merchant
// portal.
//
// Both connections are SIMULATED. This project has no partner-API credentials
// for either platform, so the backend stands in two mock APIs with genuinely
// different wire formats and normalizes them through one proxy into the same
// record type the CSV path produces.
//
// That fact is stated four separate times on the way to a number: in the tab
// label this component renders inside, in the notice below (which is the first
// thing in the panel and cannot be dismissed), on every platform row, and in
// the sync button's own words. The redundancy is deliberate — a cropped
// screenshot, a scrolled-past banner, or a glance at just the results table
// each still carry the disclosure. Zero model involvement in this flow: the
// endpoints it calls are deterministic Go end to end.
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
    formatUsd(day.commissions),
  ])
}

/** The default range: the seven days ending today, in the owner's own clock. */
function defaultRange(): { from: string; to: string } {
  const today = new Date()
  const start = new Date(today)
  start.setDate(start.getDate() - 6)
  const iso = (d: Date) => d.toISOString().slice(0, 10)
  return { from: iso(start), to: iso(today) }
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
              No real iFood or Just Eat Takeaway account is connected. This prototype has no
              partner-API access to either platform, so the orders below are generated locally
              to show how the connector works. Anything you sync from here reaches your margin
              as simulated revenue, and it stays labeled that way everywhere it appears.
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
                <Chip>{platform.commission_rate_pct}% commission</Chip>
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

          <DataGrid
            className="mt-4"
            title="Simulated orders by platform and day"
            columns={['Date', 'Platform', 'Orders', 'Gross sales', 'Refunds', 'Commission']}
            rows={toTableRows(preview.days)}
          />
        </Panel>
      ) : null}

      {stage.name === 'synced' ? (
        <Panel className="p-5 sm:p-6" role="status">
          <PanelHeader eyebrow="Done" title="Simulated orders synced" />
          <p className="mt-1 flex items-center gap-1.5 text-sm text-success-text">
            <CheckCircle2 className="size-4 shrink-0" aria-hidden="true" />
            {stage.result.orders_synced} simulated order
            {stage.result.orders_synced === 1 ? '' : 's'} replaced the delivery revenue on file for{' '}
            {stage.result.from} to {stage.result.to}, and the full reconciliation re-ran.
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
          <p className="mt-3 text-xs text-muted-foreground">
            Those days now read from the simulated connectors. Every row carries a{' '}
            <code className="font-mono">simulated://</code> source, so nothing here can be
            mistaken for a real platform settlement.
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
