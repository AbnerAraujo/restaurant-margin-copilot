import { cn } from '@/lib/utils'
import type { ExampleQuestion } from './exampleQuestions'

export interface SuggestionChipsProps {
  questions: ExampleQuestion[]
  onSelect: (question: string) => void
  /** Accessible name for the group — each placement says why it's here. */
  label: string
  /** Shows the backing MCP tool under each chip. Off in dense placements. */
  showTool?: boolean
  className?: string
}

/**
 * A row of one-tap example questions. The single presentation for capability
 * guidance everywhere it appears — empty state, after a refusal, and the
 * persistent "Ideas" rail — so a returning user meets the same affordance in
 * the same visual language rather than three lookalike mechanisms.
 */
export default function SuggestionChips({
  questions,
  onSelect,
  label,
  showTool = false,
  className,
}: SuggestionChipsProps) {
  if (questions.length === 0) return null

  return (
    <ul
      aria-label={label}
      className={cn('flex flex-wrap gap-1.5', className)}
    >
      {questions.map((question) => (
        <li key={question.text}>
          <button
            type="button"
            onClick={() => onSelect(question.text)}
            className="group flex max-w-full flex-col items-start gap-0.5 rounded-lg border border-border
              bg-card px-2.5 py-1.5 text-left text-xs font-medium text-foreground shadow-sm
              transition-colors hover:border-primary/40 hover:bg-primary/5
              focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
          >
            <span className="line-clamp-2">{question.text}</span>
            {showTool ? (
              <span className="font-mono text-[10px] font-normal text-muted-foreground">
                {question.tool}
              </span>
            ) : null}
          </button>
        </li>
      ))}
    </ul>
  )
}
