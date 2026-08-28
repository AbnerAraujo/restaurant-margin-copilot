import { Fragment, type ReactNode } from 'react'

/**
 * Renders the narration model's answer text.
 *
 * Sonnet writes markdown whether or not it is asked to, and every answer was
 * being shown raw — "**Margin: $328.82**" with the asterisks visible. On a
 * financial figure that reads as a rendering fault, which is a bad look for a
 * product whose whole claim is that its numbers are trustworthy.
 *
 * A deliberately tiny subset — bold, italic, inline code, and dash bullets —
 * parsed into React elements. No markdown library and no
 * `dangerouslySetInnerHTML`: this text comes from a model, so it is untrusted
 * input, and the only safe way to render untrusted text is as text. Anything
 * this parser doesn't recognise falls through and is displayed verbatim
 * rather than swallowed.
 */

// Italic requires a non-space immediately inside both markers, so a stray
// "* " in prose ("a lone * asterisk and ** double") is left alone instead of
// being read as emphasis spanning half the sentence.
const INLINE_PATTERN = /(\*\*[^*]+\*\*|`[^`]+`|\*(?!\s)[^*\n]+(?<!\s)\*)/g

function renderInline(text: string, keyPrefix: string): ReactNode[] {
  return text.split(INLINE_PATTERN).map((part, index) => {
    const key = `${keyPrefix}-${index}`
    if (part.startsWith('**') && part.endsWith('**') && part.length > 4) {
      return (
        <strong key={key} className="font-semibold">
          {part.slice(2, -2)}
        </strong>
      )
    }
    if (part.startsWith('`') && part.endsWith('`') && part.length > 2) {
      return (
        <code
          key={key}
          className="rounded bg-muted px-1 py-0.5 font-mono text-[0.9em]"
        >
          {part.slice(1, -1)}
        </code>
      )
    }
    if (part.startsWith('*') && part.endsWith('*') && part.length > 2) {
      return <em key={key}>{part.slice(1, -1)}</em>
    }
    return <Fragment key={key}>{part}</Fragment>
  })
}

/**
 * Splits into blocks on newlines, and additionally on " - " runs — the model
 * frequently emits a whole bulleted list on one line, which would otherwise
 * render as an unreadable wall.
 */
function toBlocks(text: string): string[] {
  return text
    .split(/\n+/)
    .flatMap((line) =>
      line.includes(' - ') ? line.split(/\s+-\s+(?=\S)/) : [line],
    )
    .map((block) => block.trim())
    .filter(Boolean)
}

export default function AnswerText({ text }: { text: string }) {
  const blocks = toBlocks(text)
  const bulletCount = blocks.filter((block) => /^[-*•]\s+/.test(block)).length

  return (
    // max-w-prose-measure (68ch) is the readability band. Without it, an
    // answer rendered in the full-width /ask panel ran to roughly 150
    // characters per line, which is well past the point where the eye loses
    // its place returning to the next line. The narration text itself is
    // untouched — this constrains the column, never the words.
    <div className="max-w-prose-measure space-y-2 text-sm leading-relaxed text-foreground">
      {blocks.map((block, index) => {
        const isBullet = /^[-*•]\s+/.test(block)
        // A single stray bullet is more likely a dash in prose than a list.
        if (isBullet && bulletCount > 1) {
          return (
            <p key={index} className="flex gap-2 pl-1">
              <span aria-hidden="true" className="text-muted-foreground">
                •
              </span>
              <span>{renderInline(block.replace(/^[-*•]\s+/, ''), `b${index}`)}</span>
            </p>
          )
        }
        return <p key={index}>{renderInline(block, `p${index}`)}</p>
      })}
    </div>
  )
}
