import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import ReportPage from './ReportPage'

describe('ReportPage', () => {
  it('renders both the margin trend and promotion ROI charts', () => {
    render(<ReportPage />)

    expect(
      screen.getByRole('heading', { name: '14-Day Margin Trend' }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('heading', { name: 'Promotion ROI' }),
    ).toBeInTheDocument()
  })

  it('renders a page title', () => {
    render(<ReportPage />)

    expect(
      screen.getByRole('heading', { level: 1, name: 'Reports' }),
    ).toBeInTheDocument()
  })
})
