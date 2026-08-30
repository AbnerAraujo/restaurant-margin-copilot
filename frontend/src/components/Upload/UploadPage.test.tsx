import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import UploadPage from './UploadPage'

function stubFetchOnce(ok: boolean, body: unknown, status = ok ? 200 : 422) {
  const mockFetch = vi.fn().mockResolvedValueOnce({
    ok,
    status,
    json: async () => body,
    text: async () => JSON.stringify(body),
  })
  vi.stubGlobal('fetch', mockFetch)
  return mockFetch
}

function testFile(content = 'invoice_id,invoice_date,supplier,category,amount,notes\n') {
  return new File([content], 'cost_sheet.csv', { type: 'text/csv' })
}

async function selectFile(file: File) {
  const input = screen.getByLabelText(/choose a supplier cost sheet csv file/i)
  const user = userEvent.setup()
  await user.upload(input, file)
}

describe('UploadPage', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('shows the backend real, specific validation error on a malformed upload', async () => {
    stubFetchOnce(false, {
      error: 'invalid_cost_sheet',
      detail: 'ingest: cost_sheet.csv row 3: amount: money: invalid value "n/a"',
    })
    render(<UploadPage />)

    await selectFile(testFile())

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/row 3/i)
    expect(alert).toHaveTextContent(/amount/i)
    // Never a generic message standing in for the real one.
    expect(alert).not.toHaveTextContent(/something went wrong/i)
  })

  it('renders every parsed row in the preview table for a valid upload, without persisting anything', async () => {
    stubFetchOnce(true, {
      row_count: 2,
      total_amount: '150.25',
      rows: [
        {
          invoice_id: 'INV-TEST-001',
          invoice_date: '2026-08-01',
          supplier: 'Test Produce Co.',
          category: 'produce',
          amount: '100.00',
          notes: 'First test invoice',
          source_row_ref: { file: 'cost_sheet.csv', row: 2 },
        },
        {
          invoice_id: 'INV-TEST-002',
          invoice_date: '2026-08-02',
          supplier: 'Test Beverage Co.',
          category: 'beverage',
          amount: '50.25',
          notes: '',
          source_row_ref: { file: 'cost_sheet.csv', row: 3 },
        },
      ],
    })
    render(<UploadPage />)

    await selectFile(testFile())

    expect(await screen.findByText('INV-TEST-001')).toBeInTheDocument()
    expect(screen.getByText('INV-TEST-002')).toBeInTheDocument()
    expect(screen.getByText('$100.00')).toBeInTheDocument()
    expect(screen.getByText(/150.25/)).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: /confirm & ingest/i }),
    ).toBeInTheDocument()
    // Preview alone must never claim anything was saved.
    expect(screen.queryByText(/ingested/i)).not.toBeInTheDocument()
  })

  it('re-validates and reports the before/after margin effect on commit', async () => {
    render(<UploadPage />)

    stubFetchOnce(true, {
      row_count: 1,
      total_amount: '100.00',
      rows: [
        {
          invoice_id: 'INV-TEST-001',
          invoice_date: '2026-08-01',
          supplier: 'Test Produce Co.',
          category: 'produce',
          amount: '100.00',
          notes: '',
          source_row_ref: { file: 'cost_sheet.csv', row: 2 },
        },
      ],
    })
    await selectFile(testFile())
    await screen.findByText('INV-TEST-001')

    stubFetchOnce(true, {
      rows_committed: 1,
      before: { days: 14, margin: '1000.00' },
      after: { days: 14, margin: '950.00' },
    })
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /confirm & ingest/i }))

    expect(await screen.findByText(/cost sheet ingested/i)).toBeInTheDocument()
    expect(screen.getByText(/\$1,000\.00 across 14 days/)).toBeInTheDocument()
    expect(screen.getByText(/\$950\.00 across 14 days/)).toBeInTheDocument()
  })

  it('reports "no prior data" honestly rather than a fabricated zero on a first-ever commit', async () => {
    render(<UploadPage />)

    stubFetchOnce(true, {
      row_count: 1,
      total_amount: '100.00',
      rows: [
        {
          invoice_id: 'INV-TEST-001',
          invoice_date: '2026-08-01',
          supplier: 'Test Produce Co.',
          category: 'produce',
          amount: '100.00',
          notes: '',
          source_row_ref: { file: 'cost_sheet.csv', row: 2 },
        },
      ],
    })
    await selectFile(testFile())
    await screen.findByText('INV-TEST-001')

    stubFetchOnce(true, {
      rows_committed: 1,
      before: { days: 0, margin: null },
      after: { days: 14, margin: '950.00' },
    })
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /confirm & ingest/i }))

    expect(await screen.findByText(/cost sheet ingested/i)).toBeInTheDocument()
    expect(screen.getByText(/no prior data/i)).toBeInTheDocument()
    expect(screen.queryByText(/\$0\.00/)).not.toBeInTheDocument()
  })

  // Defense in depth for a fixed HIGH-severity defect: the backend now
  // refuses a 0-data-row upload outright (ingest.ParseCostSheet's "no data
  // rows found" check), so this response shape shouldn't occur in practice
  // — but this page must never let it look like an ordinary preview with
  // Confirm & Ingest quietly enabled underneath it, in case some future
  // parser path ever produces row_count: 0 without erroring.
  it('disables Confirm & Ingest and warns when a preview somehow carries zero rows', async () => {
    stubFetchOnce(true, {
      row_count: 0,
      total_amount: '0.00',
      rows: [],
    })
    render(<UploadPage />)

    await selectFile(testFile())

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/no data rows/i)
    expect(screen.getByRole('button', { name: /confirm & ingest/i })).toBeDisabled()
    // Never the ordinary "nothing has been saved yet, N rows parsed" copy.
    expect(screen.queryByText(/rows? parsed/i)).not.toBeInTheDocument()
  })

  it('offers a template download link pointing at the real backend endpoint', () => {
    render(<UploadPage />)

    const link = screen.getByRole('link', { name: /download template/i })
    expect(link).toHaveAttribute(
      'href',
      expect.stringContaining('/api/ingest/cost-sheet/template'),
    )
  })
})
