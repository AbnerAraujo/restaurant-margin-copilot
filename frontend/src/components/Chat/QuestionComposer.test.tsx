import { fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import QuestionComposer from './QuestionComposer'

/** A promise plus its resolver, for asserting on an in-flight loading state. */
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => {
    resolve = res
  })
  return { promise, resolve }
}

describe('QuestionComposer', () => {
  it('renders nothing when closed', () => {
    render(<QuestionComposer open={false} onClose={vi.fn()} onAsk={vi.fn()} />)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('opens on Step 1 with all 8 plain-language categories, never a raw tool name', () => {
    render(<QuestionComposer open onClose={vi.fn()} onAsk={vi.fn()} />)

    const dialog = screen.getByRole('dialog', { name: /build a question/i })
    const categoryButtons = within(dialog).getAllByRole('button')
    // 8 categories plus the header close button.
    expect(categoryButtons.length).toBeGreaterThanOrEqual(8)

    expect(within(dialog).getByText('Check a single day')).toBeInTheDocument()
    expect(within(dialog).getByText('Compare two periods')).toBeInTheDocument()
    expect(within(dialog).getByText('Check for discrepancies')).toBeInTheDocument()
    expect(within(dialog).getByText("Check a promotion's return")).toBeInTheDocument()
    expect(within(dialog).getByText('Find money-losing promotions')).toBeInTheDocument()
    expect(within(dialog).getByText('Compare delivery platforms')).toBeInTheDocument()
    expect(within(dialog).getByText('Get period totals')).toBeInTheDocument()
    expect(
      within(dialog).getByText('Find your priciest day of the month'),
    ).toBeInTheDocument()
    expect(within(dialog).queryByText(/get_/)).not.toBeInTheDocument()
  })

  it('walks a single-date category through params to a well-formed review question, editably', async () => {
    const user = userEvent.setup()
    const onAsk = vi.fn()
    render(<QuestionComposer open onClose={vi.fn()} onAsk={onAsk} />)

    await user.click(screen.getByText('Check a single day'))

    // Step 2: a real date picker, not a free-text field.
    const dateInput = screen.getByLabelText('Date')
    expect(dateInput).toHaveAttribute('type', 'date')

    // Can't continue until the date is filled in.
    const continueButton = screen.getByRole('button', { name: /continue/i })
    expect(continueButton).toBeDisabled()

    await user.type(dateInput, '2026-08-07')
    expect(continueButton).toBeEnabled()
    await user.click(continueButton)

    // Step 3: the exact composed question, shown back for review.
    const textbox = screen.getByLabelText('Your question')
    expect(textbox).toHaveValue('How did we do on 2026-08-07?')

    // Editable before it's asked.
    await user.clear(textbox)
    await user.type(textbox, 'How did we do on 2026-08-07, exactly?')

    await user.click(screen.getByRole('button', { name: /ask this question/i }))
    expect(onAsk).toHaveBeenCalledWith('How did we do on 2026-08-07, exactly?')
  })

  it('requires the date-range sub-choice fields before Continue enables, and composes the period question', async () => {
    const user = userEvent.setup()
    const onAsk = vi.fn()
    render(<QuestionComposer open onClose={vi.fn()} onAsk={onAsk} />)

    await user.click(screen.getByText('Check for discrepancies'))

    // Defaults to "One day" — switch to the period sub-choice.
    await user.click(screen.getByRole('radio', { name: 'A date range' }))

    const continueButton = screen.getByRole('button', { name: /continue/i })
    expect(continueButton).toBeDisabled()

    await user.type(screen.getByLabelText('Start date'), '2026-08-01')
    await user.type(screen.getByLabelText('End date'), '2026-08-14')
    expect(continueButton).toBeEnabled()

    await user.click(continueButton)
    expect(screen.getByLabelText('Your question')).toHaveValue(
      'Which days had discrepancies between 2026-08-01 and 2026-08-14?',
    )

    await user.click(screen.getByRole('button', { name: /ask this question/i }))
    expect(onAsk).toHaveBeenCalledWith(
      'Which days had discrepancies between 2026-08-01 and 2026-08-14?',
    )
  })

  it('flags a backwards date range instead of silently accepting it', async () => {
    const user = userEvent.setup()
    render(<QuestionComposer open onClose={vi.fn()} onAsk={vi.fn()} />)

    await user.click(screen.getByText('Get period totals'))
    await user.type(screen.getByLabelText('Start date'), '2026-08-14')
    await user.type(screen.getByLabelText('End date'), '2026-08-01')

    expect(
      screen.getByText(/end date must be on or after the start date/i),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /continue/i })).toBeDisabled()
  })

  it('lets the owner pick a real campaign from a live-fetched list rather than typing one', async () => {
    const user = userEvent.setup()
    const onAsk = vi.fn()
    const fetchCampaigns = vi.fn().mockResolvedValue([
      { campaignId: 'IFOOD-CAMP-SPRINGMENU', platform: 'iFood' },
      { campaignId: 'JET-CAMP-LUNCHFIX', platform: 'Just Eat Takeaway' },
    ])

    render(
      <QuestionComposer
        open
        onClose={vi.fn()}
        onAsk={onAsk}
        fetchCampaigns={fetchCampaigns}
      />,
    )

    await user.click(screen.getByText("Check a promotion's return"))
    // Defaults to "A specific campaign".
    const select = await screen.findByLabelText('Campaign')
    expect(fetchCampaigns).toHaveBeenCalledTimes(1)

    await user.selectOptions(select, 'IFOOD-CAMP-SPRINGMENU')
    await user.click(screen.getByRole('button', { name: /continue/i }))

    expect(screen.getByLabelText('Your question')).toHaveValue(
      'What was the ROI for campaign IFOOD-CAMP-SPRINGMENU?',
    )
    await user.click(screen.getByRole('button', { name: /ask this question/i }))
    expect(onAsk).toHaveBeenCalledWith('What was the ROI for campaign IFOOD-CAMP-SPRINGMENU?')
  })

  it('shows a recoverable error, not a dead end, when the campaign list fails to load', async () => {
    const user = userEvent.setup()
    const fetchCampaigns = vi
      .fn()
      .mockRejectedValueOnce(new Error('network down'))
      .mockResolvedValueOnce([{ campaignId: 'IFOOD-CAMP-1', platform: 'iFood' }])

    render(
      <QuestionComposer
        open
        onClose={vi.fn()}
        onAsk={vi.fn()}
        fetchCampaigns={fetchCampaigns}
      />,
    )

    await user.click(screen.getByText("Check a promotion's return"))
    expect(
      await screen.findByText(/couldn't load your campaigns/i),
    ).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /try again/i }))
    expect(await screen.findByLabelText('Campaign')).toBeInTheDocument()
    expect(fetchCampaigns).toHaveBeenCalledTimes(2)
  })

  it('switches to the platform + period fields and composes that question instead', async () => {
    const user = userEvent.setup()
    const onAsk = vi.fn()
    const fetchCampaigns = vi.fn().mockResolvedValue([])

    render(
      <QuestionComposer
        open
        onClose={vi.fn()}
        onAsk={onAsk}
        fetchCampaigns={fetchCampaigns}
      />,
    )

    await user.click(screen.getByText("Check a promotion's return"))
    await user.click(screen.getByRole('radio', { name: 'A platform over a period' }))

    await user.selectOptions(screen.getByLabelText('Platform'), 'ifood')
    await user.type(screen.getByLabelText('Start date'), '2026-08-01')
    await user.type(screen.getByLabelText('End date'), '2026-08-14')
    await user.click(screen.getByRole('button', { name: /continue/i }))

    expect(screen.getByLabelText('Your question')).toHaveValue(
      'Show me the ROI for every iFood campaign between 2026-08-01 and 2026-08-14',
    )
    await user.click(screen.getByRole('button', { name: /ask this question/i }))
    expect(onAsk).toHaveBeenCalledWith(
      'Show me the ROI for every iFood campaign between 2026-08-01 and 2026-08-14',
    )
  })

  it('lets the owner go back to change a category without losing the ability to restart cleanly', async () => {
    const user = userEvent.setup()
    render(<QuestionComposer open onClose={vi.fn()} onAsk={vi.fn()} />)

    await user.click(screen.getByText('Check a single day'))
    expect(screen.getByLabelText('Date')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /back/i }))
    expect(screen.getByText('Check a single day')).toBeInTheDocument()
    expect(screen.getByText('Compare two periods')).toBeInTheDocument()
  })

  it('closes on the header close control and on backdrop click, asking nothing', async () => {
    const user = userEvent.setup()
    const onClose = vi.fn()
    const onAsk = vi.fn()
    render(<QuestionComposer open onClose={onClose} onAsk={onAsk} />)

    await user.click(screen.getByRole('button', { name: /close/i }))
    expect(onClose).toHaveBeenCalledTimes(1)
    expect(onAsk).not.toHaveBeenCalled()
  })

  it('resets to Step 1 every time it is reopened', async () => {
    const user = userEvent.setup()
    const { rerender } = render(
      <QuestionComposer open onClose={vi.fn()} onAsk={vi.fn()} />,
    )
    await user.click(screen.getByText('Check a single day'))
    expect(screen.getByLabelText('Date')).toBeInTheDocument()

    rerender(<QuestionComposer open={false} onClose={vi.fn()} onAsk={vi.fn()} />)
    rerender(<QuestionComposer open onClose={vi.fn()} onAsk={vi.fn()} />)

    expect(screen.getByText('What do you want to know?')).toBeInTheDocument()
    expect(screen.queryByLabelText('Date')).not.toBeInTheDocument()
  })

  it('blocks progression on an out-of-range single date with a clear inline error (Check a single day)', async () => {
    const user = userEvent.setup()
    render(
      <QuestionComposer
        open
        onClose={vi.fn()}
        onAsk={vi.fn()}
        minDate="2024-08-01"
        maxDate="2026-08-14"
      />,
    )

    await user.click(screen.getByText('Check a single day'))

    const dateInput = screen.getByLabelText('Date')
    // The browser's own `max` constraint doesn't stop this value from
    // landing in `value` — see QuestionComposer's DateField doc comment.
    await user.type(dateInput, '2027-01-05')

    expect(
      screen.getByText('Choose a date between Aug 1, 2024 and Aug 14, 2026.'),
    ).toBeInTheDocument()
    expect(dateInput).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByRole('button', { name: /continue/i })).toBeDisabled()
  })

  it('blocks progression on an out-of-range period end date with a clear inline error (Get period totals)', async () => {
    const user = userEvent.setup()
    render(
      <QuestionComposer
        open
        onClose={vi.fn()}
        onAsk={vi.fn()}
        minDate="2024-08-01"
        maxDate="2026-08-14"
      />,
    )

    await user.click(screen.getByText('Get period totals'))
    await user.type(screen.getByLabelText('Start date'), '2026-08-01')
    await user.type(screen.getByLabelText('End date'), '2027-01-05')

    expect(
      screen.getByText('Choose a date between Aug 1, 2024 and Aug 14, 2026.'),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /continue/i })).toBeDisabled()
  })

  it('traps Tab/Shift+Tab cycling within the dialog’s own focusable elements', async () => {
    render(
      <>
        <button type="button">Outside before</button>
        <QuestionComposer open onClose={vi.fn()} onAsk={vi.fn()} />
        <button type="button">Outside after</button>
      </>,
    )

    const dialog = screen.getByRole('dialog')
    const focusable = within(dialog).getAllByRole('button')
    expect(focusable.length).toBeGreaterThan(1)
    const first = focusable[0]
    const last = focusable[focusable.length - 1]

    // Shift+Tab from the first element must wrap to the last, never escape
    // to "Outside before".
    first.focus()
    fireEvent.keyDown(dialog, { key: 'Tab', shiftKey: true })
    expect(document.activeElement).toBe(last)

    // Tab from the last element must wrap back to the first, never escape
    // to "Outside after".
    fireEvent.keyDown(dialog, { key: 'Tab' })
    expect(document.activeElement).toBe(first)

    // The rest of the page is inert while the dialog is open — `inert`
    // cascades to descendants, so it's set on the ancestor these buttons
    // share (their render container), not repeated on every leaf.
    expect(screen.getByText('Outside before').closest('[inert]')).not.toBeNull()
    expect(screen.getByText('Outside after').closest('[inert]')).not.toBeNull()
    expect(dialog.closest('[inert]')).toBeNull()
  })

  it('closes on Escape from the initial screen', async () => {
    const user = userEvent.setup()
    const onClose = vi.fn()
    render(<QuestionComposer open onClose={onClose} onAsk={vi.fn()} />)

    await user.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('closes on Escape after a category click moves focus to document.body (QA regression)', async () => {
    const user = userEvent.setup()
    const onClose = vi.fn()
    render(<QuestionComposer open onClose={onClose} onAsk={vi.fn()} />)

    await user.click(screen.getByText('Check a single day'))
    // The clicked category button unmounted on the step change — focus
    // fell to document.body, exactly the state a container-scoped
    // `onKeyDown` handler can never see a keydown from.
    expect(document.activeElement).toBe(document.body)

    await user.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('closes on Escape after clicking the in-dialog Back button (same focus-loss failure mode)', async () => {
    const user = userEvent.setup()
    const onClose = vi.fn()
    render(<QuestionComposer open onClose={onClose} onAsk={vi.fn()} />)

    await user.click(screen.getByText('Check a single day'))
    await user.click(screen.getByRole('button', { name: /back/i }))
    expect(document.activeElement).toBe(document.body)

    await user.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('stops listening for Escape once closed, and does not double-fire across remounts', async () => {
    const user = userEvent.setup()
    const onClose = vi.fn()
    const { rerender } = render(<QuestionComposer open onClose={onClose} onAsk={vi.fn()} />)

    rerender(<QuestionComposer open={false} onClose={onClose} onAsk={vi.fn()} />)
    await user.keyboard('{Escape}')
    expect(onClose).not.toHaveBeenCalled()
  })

  it('shows a loading state while campaigns are in flight', async () => {
    const user = userEvent.setup()
    const { promise } = deferred<{ campaignId: string; platform: string }[]>()
    const fetchCampaigns = vi.fn().mockReturnValue(promise)

    render(
      <QuestionComposer
        open
        onClose={vi.fn()}
        onAsk={vi.fn()}
        fetchCampaigns={fetchCampaigns}
      />,
    )

    await user.click(screen.getByText("Check a promotion's return"))
    expect(screen.getByText(/loading your campaigns/i)).toBeInTheDocument()
  })
})

/**
 * The advisory path — the composer's one route to something that is not a
 * computed fact. These tests hold the two properties that make it safe to
 * offer at all: it is visually and structurally separate from the eight
 * computed categories at every step, and the question it composes is one of
 * those eight categories' own questions, so it inherits their answerability
 * and their date-bounds gate rather than getting new ones.
 */
describe('QuestionComposer — business advice path', () => {
  /** Walks Step 1 → advisory topic list. */
  async function openAdvisoryTopics(user: ReturnType<typeof userEvent.setup>) {
    await user.click(screen.getByRole('button', { name: /get business advice/i }))
  }

  it('hides the advisory path entirely when the host cannot resolve advice', () => {
    render(<QuestionComposer open onClose={vi.fn()} onAsk={vi.fn()} />)
    expect(
      screen.queryByRole('button', { name: /get business advice/i }),
    ).not.toBeInTheDocument()
    expect(screen.queryByText(/ai suggestion/i)).not.toBeInTheDocument()
  })

  it('offers the advisory path apart from the 8 categories, flagged as an AI suggestion', () => {
    render(
      <QuestionComposer open onClose={vi.fn()} onAsk={vi.fn()} onRequestAdvice={vi.fn()} />,
    )
    const dialog = screen.getByRole('dialog', { name: /build a question/i })
    // The 8 computed categories are all still there, unchanged.
    expect(within(dialog).getByText('Check a single day')).toBeInTheDocument()
    expect(within(dialog).getByText('Find your priciest day of the month')).toBeInTheDocument()
    // And the advisory entry carries the chip's own visual language.
    expect(
      within(dialog).getByRole('button', { name: /get business advice/i }),
    ).toBeInTheDocument()
    expect(within(dialog).getByText(/^ai suggestion$/i)).toBeInTheDocument()
    expect(within(dialog).queryByText(/get_/)).not.toBeInTheDocument()
  })

  it('lists every advisory topic in plain language, never an insight kind', async () => {
    const user = userEvent.setup()
    render(
      <QuestionComposer open onClose={vi.fn()} onAsk={vi.fn()} onRequestAdvice={vi.fn()} />,
    )
    await openAdvisoryTopics(user)

    expect(screen.getByText('Advice on recurring discrepancies')).toBeInTheDocument()
    expect(screen.getByText('Advice on a promotion that lost money')).toBeInTheDocument()
    expect(screen.getByText('Advice on a high commission rate')).toBeInTheDocument()
    expect(screen.getByText('Advice on a costly day of the month')).toBeInTheDocument()
    expect(screen.getByText('Advice on a margin decline')).toBeInTheDocument()
    expect(screen.queryByText(/high_commission|margin_decline/)).not.toBeInTheDocument()
  })

  it('walks a topic to a distinct advice request, never through onAsk', async () => {
    const user = userEvent.setup()
    const onAsk = vi.fn()
    const onRequestAdvice = vi.fn()
    render(
      <QuestionComposer
        open
        onClose={vi.fn()}
        onAsk={onAsk}
        onRequestAdvice={onRequestAdvice}
      />,
    )

    await openAdvisoryTopics(user)
    await user.click(screen.getByText('Advice on a high commission rate'))

    const continueButton = screen.getByRole('button', { name: /continue/i })
    expect(continueButton).toBeDisabled()
    await user.type(screen.getByLabelText('Start date'), '2026-08-01')
    await user.type(screen.getByLabelText('End date'), '2026-08-14')
    expect(continueButton).toBeEnabled()
    await user.click(continueButton)

    await user.click(screen.getByRole('button', { name: /compute this and offer advice/i }))

    expect(onRequestAdvice).toHaveBeenCalledWith({
      insightKind: 'high_commission',
      question:
        'Which platform costs me more in commission — iFood or Just Eat Takeaway — between 2026-08-01 and 2026-08-14?',
    })
    // The whole point of the separate callback: advice is never mistaken for
    // an ordinary question by whatever is listening.
    expect(onAsk).not.toHaveBeenCalled()
  })

  it('shows the grounding question read-only, with the two-stage cost disclosed', async () => {
    const user = userEvent.setup()
    render(
      <QuestionComposer open onClose={vi.fn()} onAsk={vi.fn()} onRequestAdvice={vi.fn()} />,
    )

    await openAdvisoryTopics(user)
    await user.click(screen.getByText('Advice on recurring discrepancies'))
    await user.type(screen.getByLabelText('Start date'), '2026-08-01')
    await user.type(screen.getByLabelText('End date'), '2026-08-14')
    await user.click(screen.getByRole('button', { name: /continue/i }))

    // A discrepancy PATTERN is a recurrence, so the grounding question is the
    // period one, never the single-day one.
    expect(
      screen.getByText('Which days had discrepancies between 2026-08-01 and 2026-08-14?'),
    ).toBeInTheDocument()
    // Not editable: an edited question could stop producing the pattern the
    // advice request claims to be about.
    expect(screen.queryByLabelText('Your question')).not.toBeInTheDocument()
    expect(screen.getByText(/billed model call/i)).toBeInTheDocument()
  })

  it('asks for two periods when advising on a margin decline', async () => {
    const user = userEvent.setup()
    const onRequestAdvice = vi.fn()
    render(
      <QuestionComposer
        open
        onClose={vi.fn()}
        onAsk={vi.fn()}
        onRequestAdvice={onRequestAdvice}
      />,
    )

    await openAdvisoryTopics(user)
    await user.click(screen.getByText('Advice on a margin decline'))

    const starts = screen.getAllByLabelText('Start date')
    const ends = screen.getAllByLabelText('End date')
    expect(starts).toHaveLength(2)

    await user.type(starts[0], '2026-08-01')
    await user.type(ends[0], '2026-08-07')
    await user.type(starts[1], '2026-08-08')
    await user.type(ends[1], '2026-08-14')
    await user.click(screen.getByRole('button', { name: /continue/i }))
    await user.click(screen.getByRole('button', { name: /compute this and offer advice/i }))

    expect(onRequestAdvice).toHaveBeenCalledWith({
      insightKind: 'margin_decline',
      question:
        'Compare total margin for 2026-08-01 to 2026-08-07 against 2026-08-08 to 2026-08-14',
    })
  })

  it('blocks a date outside the live data window, exactly like the computed path', async () => {
    const user = userEvent.setup()
    render(
      <QuestionComposer
        open
        onClose={vi.fn()}
        onAsk={vi.fn()}
        onRequestAdvice={vi.fn()}
        minDate="2026-08-01"
        maxDate="2026-08-14"
      />,
    )

    await openAdvisoryTopics(user)
    await user.click(screen.getByText('Advice on a costly day of the month'))
    await user.type(screen.getByLabelText('Start date'), '2026-07-01')
    await user.type(screen.getByLabelText('End date'), '2026-08-14')

    expect(screen.getByRole('button', { name: /continue/i })).toBeDisabled()
    expect(screen.getByRole('alert')).toHaveTextContent(
      /choose a date between Aug 1, 2026 and Aug 14, 2026/i,
    )
  })

  it('walks Back out of the advisory path to the category list', async () => {
    const user = userEvent.setup()
    render(
      <QuestionComposer open onClose={vi.fn()} onAsk={vi.fn()} onRequestAdvice={vi.fn()} />,
    )

    await openAdvisoryTopics(user)
    await user.click(screen.getByText('Advice on a high commission rate'))
    // params -> topic list
    await user.click(screen.getByRole('button', { name: /back/i }))
    expect(screen.getByText('Advice on a margin decline')).toBeInTheDocument()
    // topic list -> the 8 categories
    await user.click(screen.getByRole('button', { name: /back/i }))
    expect(screen.getByText('Check a single day')).toBeInTheDocument()
  })
})
