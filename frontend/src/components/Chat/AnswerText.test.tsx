import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import AnswerText from './AnswerText'

describe('AnswerText', () => {
  it('renders bold markdown as emphasis rather than literal asterisks', () => {
    render(<AnswerText text="Margin was **$328.82** on Saturday." />)

    const bold = screen.getByText('$328.82')
    expect(bold.tagName).toBe('STRONG')
    expect(screen.queryByText(/\*\*/)).not.toBeInTheDocument()
  })

  it('renders inline code and italics', () => {
    render(<AnswerText text="Computed by `get_daily_summary`, *not* estimated." />)

    expect(screen.getByText('get_daily_summary').tagName).toBe('CODE')
    expect(screen.getByText('not').tagName).toBe('EM')
  })

  it('breaks a single-line bulleted list into separate bullets', () => {
    const { container } = render(
      <AnswerText text="Two days flagged: - Aug 3: duplicate order - Aug 8: missing source" />,
    )
    expect(container.querySelectorAll('p').length).toBeGreaterThanOrEqual(3)
    expect(screen.getByText(/Aug 3: duplicate order/)).toBeInTheDocument()
    expect(screen.getByText(/Aug 8: missing source/)).toBeInTheDocument()
  })

  it('leaves plain text exactly as written', () => {
    render(<AnswerText text="No discrepancies were flagged for this date." />)
    expect(
      screen.getByText('No discrepancies were flagged for this date.'),
    ).toBeInTheDocument()
  })

  it('renders unmatched markers verbatim instead of swallowing them', () => {
    render(<AnswerText text="A lone * asterisk and ** double stay put." />)
    expect(
      screen.getByText(/A lone \* asterisk and \*\* double stay put\./),
    ).toBeInTheDocument()
  })
})
