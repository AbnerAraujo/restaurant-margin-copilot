/**
 * `/ask` — "Ask about your margin", per redesign-spec.md §1.
 *
 * STUB: the real page mounts the existing `ChatPanel` full-width. This
 * placeholder exists only so the shell's routing can be built and tested
 * independently of that work.
 */
export default function AskPage() {
  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-4">
      <h1 className="text-2xl font-semibold tracking-tight text-foreground">
        Ask about your margin
      </h1>
      <p className="text-sm text-muted-foreground">The chat panel goes here.</p>
    </div>
  )
}
